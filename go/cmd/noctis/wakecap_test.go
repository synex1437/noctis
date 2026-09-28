package main

import (
	"strings"
	"testing"
	"time"
)

// h8WeeklyWallIn writes usage data with the weekly window at 99 % and its reset seconds away.
func h8WeeklyWallIn(seconds float64) {
	now := float64(nowSec())
	mustWriteJSON(files.usage, object{
		"updatedAt": now,
		"five_hour": object{"used": float64(40), "resetsAt": now + 3600},
		"seven_day": object{"used": float64(99), "resetsAt": now + seconds},
	})
}

// h8StopFailureSleeps runs the StopFailure hook of a weekly wall and tells whether it stayed in the
// hook to wake the session. A hook found sleeping has its wait cleared, so it ends at its next tick
// and the test does not wait for the reset.
func h8StopFailureSleeps(t *testing.T, cfg object, sid, project string) bool {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		onStopFailure(agentHookInput("StopFailure", sid, project, object{"error": "rate_limit", "last_assistant_message": "You've hit your weekly limit · resets Mon 9am"}), cfg)
	}()
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		select {
		case <-done:
			return false
		default:
		}
		if numberOr(pendingWait(sid), "waking", 0) > 0 {
			clearWait(sid, nil)
			select {
			case <-done:
			case <-time.After(40 * time.Second):
				t.Fatal("the StopFailure hook did not end after its wait was cleared")
			}
			return true
		}
	}
	t.Fatal("the StopFailure hook neither returned nor started to sleep within 20 s")
	return false
}

func TestASameSessionWakeDoesNotSleepPastTheStopFailureHooksTimeout(t *testing.T) {
	cfg, project := cloudSandbox(t, t.TempDir(), object{"wake": object{"maxMinutes": float64(400)}})
	// 6 h + a few minutes: inside wake.maxMinutes (400 min), past the hook's 21600 s timeout.
	h8WeeklyWallIn(21500)
	sid := "h8-past-timeout"
	if h8StopFailureSleeps(t, cfg, sid, project) {
		t.Fatal("the StopFailure hook went to sleep for a reset past its own 21600 s timeout, so Claude Code kills it before it wakes the session")
	}
	if wait := pendingWait(sid); getString(wait, "window") != "seven_day" {
		t.Fatalf("the weekly wall was not recognised: %v", wait)
	}
	if reason := journaledReason(sid, "cloud-no-wake"); !strings.Contains(reason, "the StopFailure hook's timeout") {
		t.Fatalf("noctis why does not say that the reset is past the hook's timeout: %q", reason)
	}
}

func TestASameSessionWakeDoesNotSleepPastTheLearnedHookCap(t *testing.T) {
	cfg, project := cloudSandbox(t, t.TempDir(), nil)
	updateState(func(state object) { state["hookCapSeconds"] = float64(3600) })
	// Two hours: inside wake.maxMinutes (330 min), past the 60 min cap noctis learned.
	h8WeeklyWallIn(7000)
	sid := "h8-past-cap"
	if h8StopFailureSleeps(t, cfg, sid, project) {
		t.Fatal("the StopFailure hook went to sleep for a reset past the hook time cap noctis learned")
	}
	if reason := journaledReason(sid, "cloud-no-wake"); !strings.Contains(reason, "the hook time cap noctis learned") {
		t.Fatalf("noctis why does not say that the reset is past the learned cap: %q", reason)
	}
}
