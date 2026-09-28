package main

import (
	"slices"
	"testing"
)

func TestAnEarlyResumeStopsTheSystemdTimerItsPauseWaitedOn(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		ownTimer bool
	}{
		{"resumed early, by the reset watcher", false},
		{"fired by that timer itself", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			calls := takeoverSandbox(t)
			recorded := withFakeScheduler(t, nil)
			sid := "r6-early"
			now := float64(nowSec())
			later := now + 3*86400
			unit := systemdJobUnit(sid, later)
			// A weekly pause whose runner is a systemd timer three days out; the window reset early.
			strandedPause(t, sid, object{"window": "seven_day", "label": "Wk", "until": later, "resumeAt": later,
				"scheduled": object{"method": "systemd", "unit": unit, "at": later}})
			statusReadingFrom(sid, nowSec(), 3, now+18000, 20, now+7*86400)
			t.Setenv(systemdUnitEnv, "")
			if testCase.ownTimer {
				t.Setenv(systemdUnitEnv, unit)
			}

			resumeWait(sid, "reset")

			if launches := launchesOf(calls, sid); launches != 1 {
				t.Fatalf("the runner launched the session %d times; want once (journal %v)", launches, journaledFor(sid))
			}
			stopped := systemctlStops(*recorded)
			if testCase.ownTimer {
				if len(stopped) > 0 {
					t.Fatalf("the runner its own timer started stopped %v; that timer is one-shot and has fired, and the service is the runner itself", stopped)
				}
				return
			}
			if !slices.Contains(stopped, unit+".timer") {
				t.Fatalf("the session was resumed early and its pause is gone, but its timer %s.timer was not stopped (systemctl stops: %v): it fires in three days, and neither cancel nor uninstall can find it", unit, stopped)
			}
		})
	}
}
