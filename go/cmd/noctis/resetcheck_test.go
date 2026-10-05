package main

import (
	"testing"
	"time"
)

func weeklyOnlyBody(week, weekReset float64) string {
	stamp := time.Unix(int64(weekReset), 0).UTC().Format(time.RFC3339)
	return `{"limits":[{"kind":"weekly_all","utilization":` + formatNumber(week) + `,"resets_at":"` + stamp + `"}]}`
}

func pausedPastItsReset(t *testing.T, sid, kind string, until float64) object {
	t.Helper()
	sandboxFiles(t)
	cfg := releaseConfig()
	cfg["fable"] = object{"source": "oauth"}
	updateState(func(state object) {
		stateMap(state, "waits")[sid] = object{"kind": kind, "window": "five_hour", "label": "5h", "used": float64(100), "threshold": float64(92), "hit": "threshold",
			"startedAt": until - 600, "until": until, "resumeAt": until + 90}
	})
	return cfg
}

func TestTheTickLandsOnEachCheckAfterTheReset(t *testing.T) {
	now := float64(nowSec())
	for _, check := range []struct {
		name   string
		record object
		want   float64
	}{
		{"before the first check", object{"kind": "batch", "until": now - 5}, 5},
		{"before the second check", object{"kind": "batch", "until": now - 25}, 5},
		{"before the last check", object{"kind": "batch", "until": now - 55}, 5},
		{"after the last check", object{"kind": "batch", "until": now - 65}, sleepTickSeconds},
		{"a limit Claude Code resumes from itself", object{"kind": "stopfailure", "until": now - 5}, sleepTickSeconds},
		{"no known reset", object{"kind": "batch"}, sleepTickSeconds},
	} {
		watch := &waitWatch{pollEvery: 300, record: check.record}
		for waiter, pace := range map[string]float64{"a waiting hook": watch.steadyPace(85), "a sleeper": watch.tickPace(85)} {
			if pace > check.want || pace < check.want-1 {
				t.Errorf("%s, %s: the tick is %vs, want %vs", check.name, waiter, pace, check.want)
			}
		}
	}
}

func TestAWaitReadsTheUsageAtTheFirstCheckAfterItsReset(t *testing.T) {
	for _, kind := range []string{"batch", "stopfailure"} {
		t.Run(kind, func(t *testing.T) {
			now := nowSec()
			cfg := pausedPastItsReset(t, "rc1", kind, float64(now-10))
			served := limitsServer(t, weeklyOnlyBody(20, float64(now+3*86400)))
			watch := newWaitWatch(cfg, "rc1", false)
			watch.lastPoll = float64(now - 20)
			released := watch.tick()
			switch {
			case kind == "stopfailure" && (released || served.Load() != 0):
				t.Fatalf("a wait that Claude Code resumes from itself read the usage %d time(s) and ended (%v) ten seconds after the reset, ahead of that resume", served.Load(), released)
			case kind == "batch" && (!released || !watch.early || served.Load() != 1):
				t.Fatalf("ten seconds after the reset the wait read the usage %d time(s), ended=%v early=%v; want one reading that ends it", served.Load(), released, watch.early)
			}
		})
	}
}

func TestAResetTheUsageDoesNotShowYetIsReadAgainAtTheNextCheck(t *testing.T) {
	now := nowSec()
	until := float64(now - 10)
	cfg := pausedPastItsReset(t, "rc2", "batch", until)
	weekReset := float64(now + 3*86400)
	served := limitsServer(t, limitsBody(100, until, 20, weekReset), weeklyOnlyBody(20, weekReset))
	watch := newWaitWatch(cfg, "rc2", false)
	watch.lastPoll = float64(now - 20)
	if watch.tick() || served.Load() != 1 {
		t.Fatalf("a reading ten seconds after the reset that still shows the window full at its old reset ended the wait (%d reading(s))", served.Load())
	}
	if watch.tick() || served.Load() != 1 {
		t.Fatalf("the wait read the usage again before its next check: %d reading(s)", served.Load())
	}
	previousOffset := timeOffset
	t.Cleanup(func() { timeOffset = previousOffset })
	timeOffset += 20
	if !watch.tick() || !watch.early || served.Load() != 2 {
		t.Fatalf("thirty seconds after the reset, with the window gone from the reading, the wait did not end: %d reading(s), early=%v", served.Load(), watch.early)
	}
}

func TestAWaitThatStartsAfterACheckMakesThatCheckAtOnce(t *testing.T) {
	now := nowSec()
	cfg := pausedPastItsReset(t, "rc4", "batch", float64(now-12))
	served := limitsServer(t, weeklyOnlyBody(20, float64(now+3*86400)))
	watch := newWaitWatch(cfg, "rc4", false)
	if !watch.tick() || !watch.early || served.Load() != 1 {
		t.Fatalf("a wait that started two seconds after the check ten seconds past its reset read the usage %d time(s) at its first tick, ended=%v; want the reading of that check at once, not at the next check eighteen seconds on", served.Load(), watch.early)
	}
}

func TestAnInHookWaitEndsAtTheFirstCheckThatSeesTheReset(t *testing.T) {
	dir := sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	cfg := inHookConfig()
	waitCfg := section(cfg, "wait")
	waitCfg["earlyResetPollMinutes"], waitCfg["resetMarginSeconds"] = float64(5), float64(90)
	now := nowSec()
	served := limitsServer(t, weeklyOnlyBody(20, float64(now+3*86400)))
	plan := &waitPlan{window: "five_hour", label: "5h", used: 100, threshold: 92, until: float64(now - 8), hit: "threshold"}
	outcome := enforceWait("batch", object{"session_id": "rc3", "cwd": dir}, cfg, decision{wait: plan, model: "claude-opus-5"})
	waited := float64(nowSec() - now)
	if served.Load() != 1 || waited > 10 {
		t.Fatalf("the wait read the usage %d time(s) and ran %vs; want one reading at the check ten seconds after the reset, long before its margin ran out", served.Load(), waited)
	}
	if entry := journaledEntry("rc3", "reset-confirmed"); entry == nil || numberOr(entry, "ahead", 0) < 70 || hasAction(journalActions(), "early-reset") {
		t.Fatalf("the resume after the confirmed reset is journaled as %v (%v)", entry, journalActions())
	}
	if outcome.stop != "" || outcome.notice != resumedNotice(plan, waited) {
		t.Fatalf("the session was not let go with the plain resume notice: %+v", outcome)
	}
}
