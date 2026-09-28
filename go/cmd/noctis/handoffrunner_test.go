package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"
)

// r2HandOff stores a hand-off of sid to the runner pid, taken ten minutes ago (past the grace a
// young hand-off gets), with the runner's start time when started is not empty.
func r2HandOff(sid string, pid int, started string) object {
	now := float64(nowSec())
	record := object{"at": now - 600, "model": "claude-opus-5", "mode": "headless", "pid": float64(pid), "waitStartedAt": now - 7200}
	if started != "" {
		record["started"] = started
	}
	updateState(func(state object) { stateMap(state, "handedOff")[sid] = cloneObject(record) })
	return record
}

func TestAHandOffHoldsTheSessionOnlyWhileItsPidStillNamesTheRunner(t *testing.T) {
	relaunchSandbox(t)
	runner, stranger := idleRunner(t), idleWindow(t)
	cases := []struct {
		name    string
		pid     int
		started string
		holds   bool
	}{
		{"the runner still runs", runner.pid, processStarted(runner.pid), true},
		{"the runner died and its pid went to a process started at another time", runner.pid, "1", false},
		{"a hand-off an earlier version wrote, whose pid runs noctis", runner.pid, "", true},
		{"a hand-off an earlier version wrote, whose pid runs another program", stranger.pid, "", false},
	}
	records := map[string]object{}
	for index, tc := range cases {
		sid := fmt.Sprintf("hr%d", index+1)
		records[sid] = r2HandOff(sid, tc.pid, tc.started)
	}

	stale := staleHandoffs(readState())

	for index, tc := range cases {
		sid := fmt.Sprintf("hr%d", index+1)
		if held := !slices.Contains(stale, sid); held != tc.holds {
			t.Errorf("%s: the repair pass kept the hand-off: %v, want %v (stale: %v)", tc.name, held, tc.holds, stale)
		}
		if held := liveRunner(records[sid]) == tc.pid; held != tc.holds {
			t.Errorf("%s: a runner found pid %d still holding the session: %v, want %v", tc.name, tc.pid, held, tc.holds)
		}
	}
}

func TestAPromptIsNotRefusedForAHandOffWhoseRunnerPidNowNamesAnotherProcess(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 20, 40)
	t.Setenv(handoffEnv, "")
	runner := idleRunner(t)
	sid := "hr-prompt"
	input := object{"hook_event_name": "UserPromptSubmit", "session_id": sid, "cwd": project, "transcript_path": filepath.Join(project, "transcript.jsonl"), "prompt": "carry on with the migration"}

	r2HandOff(sid, runner.pid, processStarted(runner.pid))
	if output := hookOutput(t, onUserPromptSubmit, input, cfg); getString(output, "decision") != "block" {
		t.Fatalf("a prompt in the old window went through while the runner that relaunched the session still runs: %v", output)
	}

	r2HandOff(sid, runner.pid, "1")
	if output := hookOutput(t, onUserPromptSubmit, input, cfg); getString(output, "decision") == "block" {
		t.Fatalf("the runner died without releasing the hand-off and its pid went to another process, yet every prompt in the session is refused: %v", output)
	}
	if handoff := getMap(getMap(readState(), "handedOff"), sid); handoff != nil {
		t.Fatalf("the hand-off of a runner whose pid now names another process was kept: %v", handoff)
	}
	if entries := journaledCount(sid, "handoff-gone"); entries != 1 {
		t.Fatalf("dropping the hand-off was journaled %d time(s) for noctis why, want once", entries)
	}
}

func TestAWaitHandedToARunnerWhosePidWasReusedIsRearmed(t *testing.T) {
	dir, _ := strandedSandbox(t)
	runner := idleRunner(t)
	sid := "hr-rearm"
	now := float64(nowSec())
	storeWaits(map[string]object{sid: parkedWait(dir, now-600, nil)})
	r2HandOff(sid, runner.pid, "1")

	repairOrphanWaits()

	state := readState()
	if handoff := getMap(getMap(state, "handedOff"), sid); handoff != nil {
		t.Fatalf("the hand-off of a runner whose pid now names another process was kept: %v", handoff)
	}
	if scheduled := getMap(getMap(getMap(state, "waits"), sid), "scheduled"); scheduled == nil || journaledCount(sid, "reschedule") != 1 {
		t.Fatalf("the wait the dead runner had claimed was not re-armed, so the session is never relaunched: %v", getMap(getMap(state, "waits"), sid))
	}
}
