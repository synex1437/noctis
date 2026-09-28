package main

import "testing"

func TestAnIdleSessionsExpiredWindowDoesNotReplaceTheLiveOne(t *testing.T) {
	sandboxFiles(t)
	cfg := releaseConfig()
	now := nowSec()
	live, expired, weekReset := float64(now+3600), float64(now-600), float64(now+3*86400)
	statusReadingFrom("a", now-60, 93, live, 20, weekReset)
	statusReadingFrom("b", now, 97, expired, 20, weekReset)

	usage := currentUsage(now)
	if usage.fiveHour == nil || usage.fiveHour.used != 93 || usage.fiveHour.resetsAt != live {
		t.Fatalf("session b re-rendered a 5-hour window that reset 10 minutes ago, and session a's live reading of 93%% was lost: %+v", usage.fiveHour)
	}
	if plan := evaluate(cfg, usage, "", 0, false).wait; plan == nil || plan.window != "five_hour" {
		t.Fatalf("at 93%% of the live 5-hour window (pause point 92%%) the session was left running: %+v", plan)
	}
	for _, raw := range getList(getMap(readJSON(files.usage), "history"), "five_hour") {
		if numberOr(toObject(raw), "resetsAt", 0) == expired {
			t.Fatalf("the window that has reset went into the history, where it crowds out the live window's samples: %v", raw)
		}
	}
	if updated := numberOr(readJSON(files.usage), "updatedAt", 0); updated != float64(now) {
		t.Fatalf("session b's weekly window is live and was read now, yet the usage file says it was updated at %v", updated)
	}
}

func TestTheLiveReadingWinsOverAWindowThatHasResetFromTheOtherSource(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	live, expired, weekReset := float64(now+3600), float64(now-600), float64(now+3*86400)
	oauthReading(now-30, 93, live, 20, weekReset)
	statusReading(now, 97, expired, 20, weekReset)
	if usage := currentUsage(now); usage.fiveHour == nil || usage.fiveHour.used != 93 {
		t.Fatalf("the usage endpoint read the live 5-hour window at 93%% 30 s ago, and a newer status line showing a window that has reset hid it: %+v", usage.fiveHour)
	}

	sandboxFiles(t)
	statusReading(now-30, 93, live, 20, weekReset)
	oauthReading(now, 97, expired, 20, weekReset)
	if usage := currentUsage(now); usage.fiveHour == nil || usage.fiveHour.used != 93 {
		t.Fatalf("the status line read the live 5-hour window at 93%% 30 s ago, and a newer endpoint reading of a window that has reset hid it: %+v", usage.fiveHour)
	}
}
