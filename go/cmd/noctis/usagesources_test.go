package main

import "testing"

func oauthReading(fetchedAt int64, five, fiveReset, week, weekReset float64) {
	mustWriteJSON(files.fable, object{
		"fetchedAt": float64(fetchedAt),
		"five_hour": object{"used": five, "resetsAt": fiveReset},
		"seven_day": object{"used": week, "resetsAt": weekReset},
	})
}

func TestTheHigherReadingOfTheSameWindowWins(t *testing.T) {
	sandboxFiles(t)
	cfg := releaseConfig()
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	oauthReading(now-5, 93, reset, 20, weekReset)
	statusReading(now, 85, reset, 20, weekReset)
	usage := currentUsage(now)
	if usage.fiveHour == nil || usage.fiveHour.used != 93 {
		t.Fatalf("the usage endpoint read the 5-hour window at 93%% five seconds ago, and a status line re-rendered its older 85%% for the same window afterwards: the guard decided on %+v", usage.fiveHour)
	}
	if usage.fiveHour.staleness != 5 {
		t.Fatalf("the 93%% reading is five seconds old, the guard took it as %v seconds old", usage.fiveHour.staleness)
	}
	if plan := evaluate(cfg, usage, "claude-opus-5-5", 0, false).wait; plan == nil || plan.window != "five_hour" {
		t.Fatalf("at 93%% of the 5-hour window (pause point 92%%) the session was left running: %+v", plan)
	}
	for _, apart := range []float64{-120, 60, 120} {
		oauthReading(now-5, 93, reset+apart, 20, weekReset)
		if usage := currentUsage(now); usage.fiveHour == nil || usage.fiveHour.used != 93 {
			t.Fatalf("two readings whose reset times are %v s apart belong to one window, and the lower newer one won: %+v", apart, usage.fiveHour)
		}
	}
	oauthReading(now-5, 93, reset+121, 20, weekReset)
	if usage := currentUsage(now); usage.fiveHour == nil || usage.fiveHour.used != 85 {
		t.Fatalf("reset times more than two minutes apart are two windows, and the newer reading must win: %+v", usage.fiveHour)
	}
	statusReading(now, 95, reset, 20, weekReset)
	if usage := currentUsage(now); usage.fiveHour == nil || usage.fiveHour.used != 95 {
		t.Fatalf("the status line read the window higher than the usage endpoint, and its reading was not used: %+v", usage.fiveHour)
	}
}

func TestANewWindowStillReplacesTheOldOne(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	reset, weekReset := float64(now+600), float64(now+3*86400)
	oauthReading(now-5, 93, reset, 20, weekReset)
	statusReading(now, 3, reset+5*3600, 20, weekReset)
	if usage := currentUsage(now); usage.fiveHour == nil || usage.fiveHour.used != 3 || usage.fiveHour.resetsAt != reset+5*3600 {
		t.Fatalf("the status line reports a new 5-hour window, and the older reading of the window before it was kept: %+v", usage.fiveHour)
	}
	statusReading(now-60, 93, reset, 20, weekReset)
	oauthReading(now, 2, reset+5*3600, 20, weekReset)
	if usage := currentUsage(now); usage.fiveHour == nil || usage.fiveHour.used != 2 {
		t.Fatalf("the usage endpoint reports the window reset early, and the older reading of the finished window was kept: %+v", usage.fiveHour)
	}
}

func TestTheNewerOfTwoEqualReadingsIsUsed(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReading(now-400, 80, reset, 20, weekReset)
	oauthReading(now-30, 80, reset, 20, weekReset)
	if usage := currentUsage(now); usage.fiveHour == nil || usage.fiveHour.staleness != 30 {
		t.Fatalf("two equal readings of one window: the one taken 30 s ago must be used, got %+v", usage.fiveHour)
	}
}
