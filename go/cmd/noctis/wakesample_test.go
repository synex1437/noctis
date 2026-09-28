package main

import (
	"strconv"
	"testing"
)

// e5SleptTenMinutes is a wait a hook has slept on for ten minutes of the hour it meant to: a
// same-session wake (waking, no holder, fresh heartbeat) or, with holder set, a pause held in the hook.
func e5SleptTenMinutes(sid, holder string) object {
	now := float64(nowSec())
	wait := sleepingWait(sid, holder, now-5)
	wait["startedAt"], wait["resumeAt"] = now-600, now+3000
	if holder == "" {
		wait["kind"], wait["inHook"], wait["waking"] = "stopfailure", false, now-600
	}
	return wait
}

func TestTakingOverASameSessionWakeTeachesNoHookTimeoutCap(t *testing.T) {
	sandboxFiles(t)
	withFakeScheduler(t, nil)
	// Two sessions taken over ten minutes into their wakes: the same time twice, which from pauses
	// held in the hook would read as a hook timeout cap.
	for _, sid := range []string{"e5-wake-1", "e5-wake-2"} {
		updateState(func(state object) { stateMap(state, "waits")[sid] = e5SleptTenMinutes(sid, "") })
		releaseInterruptedWait(sid, readState())
		if pendingWait(sid) != nil {
			t.Fatalf("a prompt in the session did not let go of the same-session wake it took over from (%s)", sid)
		}
	}
	state := readState()
	if samples := getList(state, "interruptedWaits"); len(samples) != 0 {
		t.Fatalf("a same-session wake the person took over from was counted as an interrupted in-hook wait: %v", samples)
	}
	if learned := numberOr(state, "hookCapSeconds", 0); learned != 0 {
		t.Fatalf("two same-session wakes the person took over from taught a hook timeout cap of %v s, which then sends every longer pause to the runner", learned)
	}
	for _, sid := range []string{"e5-hook-1", "e5-hook-2"} {
		updateState(func(state object) {
			stateMap(state, "waits")[sid] = e5SleptTenMinutes(sid, strconv.Itoa(deadPid(t))+"-e5")
		})
		releaseInterruptedWait(sid, readState())
	}
	if learned := numberOr(readState(), "hookCapSeconds", 0); learned < 590 || learned > 600 {
		t.Fatalf("two pauses held in the hook that were both cut off after ten minutes taught a cap of %v s, want about 600", learned)
	}
}
