package main

import (
	"os"
	"slices"
	"testing"
)

func parkRelaunchOffFable(t *testing.T, sid string) (object, string) {
	t.Helper()
	cwd, now := t.TempDir(), float64(nowSec())
	transcript := writeTranscriptAt(t, cwd, []string{userPromptLine(now-60, "fix the parser")}, now-60)
	handleFableHit("batch", object{"session_id": sid, "cwd": cwd, "transcript_path": transcript}, loadConfig(), fableHitDecision())
	wait := pendingWait(sid)
	if getString(wait, "kind") != "fable" {
		t.Fatalf("setup: Fable's limit parked no relaunch on the fallback model: %v (journal %v)", wait, journaledFor(sid))
	}
	writeFableBucket(99, now, now+3*86400)
	statusReadingFrom(sid, nowSec(), 3, now+18000, 20, now+3*86400)
	hookOutput(t, onPostModelSwitch, object{"session_id": sid, "to_model": getString(wait, "modelOverride")}, loadConfig())
	return wait, cwd
}

func TestAPromptThatGoesOnInTheSessionsOwnWindowTakesOverItsRelaunchOffFable(t *testing.T) {
	for _, paused := range []bool{false, true} {
		name := "noctis on"
		if paused {
			name = "noctis paused"
		}
		t.Run(name, func(t *testing.T) {
			calls := takeoverSandbox(t)
			previousOffset := timeOffset
			t.Cleanup(func() { timeOffset = previousOffset })
			sid := "fable-own-window"
			wait, cwd := parkRelaunchOffFable(t, sid)
			if paused {
				updateState(func(state object) { state["disabledUntil"] = float64(nowSec() + 3600) })
			}

			if output := hookOutput(t, onUserPromptSubmit, promptInput(sid, cwd, "go on with the parser tests"), loadConfig()); getString(output, "decision") == "block" {
				t.Fatalf("the prompt typed in the session's own window after the switch off Fable was refused: %v", output)
			}
			timeOffset += int64(numberOr(wait, "resumeAt", 0)) - nowSec() + 1
			resumeWait(sid, "")

			if got := launchesOf(calls, sid); got != 0 {
				t.Fatalf("the session went on in its own window after the switch off Fable, before its answer reached the transcript, and the runner relaunched it as well: %d launch(es) (journal %v)", got, journaledFor(sid))
			}
			if left := pendingWait(sid); left != nil {
				t.Fatalf("the relaunch on the fallback model stayed parked after the session went on in its own window: %v", left)
			}
			if mark := getMap(getMap(readState(), "continuedBy"), sid); getString(mark, "by") != "session" || numberOr(mark, "startedAt", -1) != numberOr(wait, "startedAt", -2) {
				t.Fatalf("the pause was not marked as continued by the session itself: %v", mark)
			}
			if !slices.Contains(journaledFor(sid), "skip-launch") {
				t.Fatalf("the relaunch was dropped without saying why in the journal: %v", journaledFor(sid))
			}
		})
	}
}

func TestAPromptIsRefusedWhenTheRunnerTookTheSessionOverWhileItWasDecided(t *testing.T) {
	takeoverSandbox(t)
	sid := "fable-runner-first"
	wait, cwd := parkRelaunchOffFable(t, sid)
	at := float64(nowSec())
	var output object

	whileTheStateIsLocked(func() {
		output = hookOutput(t, onUserPromptSubmit, promptInput(sid, cwd, "go on with the parser tests"), loadConfig())
	}, func(state object) {
		stateMap(state, "handedOff")[sid] = object{"at": at, "model": getString(wait, "modelOverride"), "mode": "headless", "pid": float64(os.Getpid()), "waitStartedAt": numberOr(wait, "startedAt", 0)}
		markContinued(state, sid, wait, "runner")
	})

	if want := T("handoff.blocked", formatTime(at), getString(wait, "modelOverride"), sid); getString(output, "decision") != "block" || getString(output, "reason") != want {
		t.Fatalf("the runner relaunched the session while a prompt in its own window was being decided, and the prompt went ahead as well, so two processes drive one session: %v", output)
	}
	if left := pendingWait(sid); !sameWait(left, numberOr(wait, "startedAt", -1), getString(wait, "holder")) {
		t.Fatalf("the refused prompt took the pause from the runner that relaunched the session: %v", left)
	}
	if mark := getMap(getMap(readState(), "continuedBy"), sid); getString(mark, "by") != "runner" {
		t.Fatalf("the refused prompt marked the pause as its own: %v", mark)
	}
}
