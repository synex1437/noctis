package main

import (
	"slices"
	"strings"
	"testing"
)

// e4StopFailureWait is the wait a StopFailure stores for a five-hour wall; wokeAt, when set, is the
// time its same-session wake woke the session in place (exit 2).
func e4StopFailureWait(project string, wokeAt float64) object {
	now := float64(nowSec())
	wait := object{"kind": "stopfailure", "window": "five_hour", "label": "5h", "used": float64(100), "until": now - 60, "resumeAt": now + 240,
		"inHook": false, "cwd": project, "checkpoint": "", "queuedPrompt": "", "startedAt": now - 3600}
	if wokeAt > 0 {
		wait["wakeAttemptedAt"] = wokeAt
	}
	return wait
}

func TestTheStopOfTheTurnASameSessionWakeWokeDrivesTheQueueAgain(t *testing.T) {
	cfg, project, _ := projectQueueSandbox(t)
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	sid := "e4-woken"
	updateState(func(state object) { stateMap(state, "waits")[sid] = e4StopFailureWait(project, float64(nowSec()-30)) })
	output := stopHookOutput(t, stopInput(sid, project), cfg)
	if getString(output, "decision") != "block" || !strings.Contains(getString(output, "reason"), "Queue continues: 2 open in TASKS.md") {
		t.Fatalf("the stop of the turn a same-session wake woke did not go on with the trusted queue: %v", output)
	}
	if wait := pendingWait(sid); wait != nil {
		t.Fatalf("the wait of the wake that took stays stored, so every later stop skips the queue: %v", wait)
	}
	if by := getString(getMap(getMap(readState(), "continuedBy"), sid), "by"); by != "wake" {
		t.Fatalf("the wait was not retired as continued by the wake, as the runner retires it: %q", by)
	}
	if actions := journaledFor(sid); !slices.Contains(actions, "wake-took") {
		t.Fatalf("noctis why does not show that the wake took: %v", actions)
	}
}

func TestAStopBeforeTheSameSessionWakeLeavesTheWaitAndTheQueueAlone(t *testing.T) {
	cfg, project, _ := projectQueueSandbox(t)
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	sid := "e4-not-woken"
	updateState(func(state object) { stateMap(state, "waits")[sid] = e4StopFailureWait(project, 0) })
	if output := stopHookOutput(t, stopInput(sid, project), cfg); output != nil {
		t.Fatalf("a stop while the session waits for its reset drove the queue: %v", output)
	}
	if pendingWait(sid) == nil {
		t.Fatal("a stop before the wake dropped the wait that is to resume the session")
	}
}
