package main

import (
	"strings"
	"testing"
)

func TestAPauseForAnotherReasonThanItsPausePointSaysWhy(t *testing.T) {
	cfg, project, _ := limitSandbox(t, object{"budget": object{"dailyWeeklyPercent": float64(5), "hardStop": true},
		"wait": object{"maxInHookMinutes": float64(1), "resetMarginSeconds": float64(90)}}, 20, 41)
	week := getMap(readJSON(files.usage), "seven_day")
	updateState(func(state object) {
		state["budgetDay"] = object{"day": localDay(nowSec()), "weekResetsAt": numberOr(week, "resetsAt", 0), "startUsed": float64(30)}
	})
	output := hookOutput(t, onUserPromptSubmit, promptInput("why-budget", project, "go on with the parser tests"), cfg)
	reason := getString(output, "reason")
	if getString(output, "decision") != "block" || !strings.Contains(reason, T("wait.pauseReason", T("hit.budget"))) {
		t.Fatalf("a stop for the daily budget reads like the weekly limit at 41%%: %v", output)
	}
	if strings.Index(reason, T("hit.budget")) > strings.Index(reason, T("wait.savedHint")) {
		t.Errorf("the reason comes after the way out instead of with the pause: %q", reason)
	}

	now := nowSec()
	burst := &waitPlan{window: "five_hour", label: windowLabel("five_hour"), used: 88, threshold: 92, until: float64(now + 3600), hit: "burst"}
	if stop := savedStop(cfg, "batch", burst, burst.until, ""); !strings.Contains(stop, T("wait.pauseReason", T("hit.burst"))) {
		t.Errorf("a pause at 88%% under a 92%% pause point does not say it came from the burst projection: %q", stop)
	}
	for _, hit := range []string{"compaction", "blind", "projection", "ceiling"} {
		other := &waitPlan{window: "five_hour", label: windowLabel("five_hour"), used: 90, threshold: 92, until: float64(now + 3600), hit: hit}
		want := T("hit." + hit)
		if hit == "ceiling" {
			want = T("hit.ceilingSoon")
		}
		if stop := savedStop(cfg, "prompt", other, other.until, ""); !strings.Contains(stop, T("wait.pauseReason", want)) {
			t.Errorf("a %s pause does not say why: %q", hit, stop)
		}
	}
	reached := &waitPlan{window: "five_hour", label: windowLabel("five_hour"), used: 100, threshold: 100, until: float64(now + 3600), hit: "ceiling"}
	if stop := savedStop(cfg, "prompt", reached, reached.until, ""); !strings.Contains(stop, T("wait.pauseReason", T("hit.ceiling"))) {
		t.Errorf("a pause at the paid-credit ceiling does not say the ceiling was reached: %q", stop)
	}
	if joined := joinWait("batch", "why-join", cfg, burst, object{"resumeAt": burst.until}, burst.until, false, now); !strings.Contains(joined.stop, T("wait.pauseReason", T("hit.burst"))) {
		t.Errorf("a session that joins a burst pause is not told why: %q", joined.stop)
	}
	updateState(func(state object) {
		stateMap(state, "waits")["why-replaced"] = object{"window": "seven_day", "label": windowLabel("seven_day"), "used": float64(41), "hit": "budget",
			"threshold": float64(89), "until": float64(now + 86400), "resumeAt": float64(now + 86400), "inHook": false, "startedAt": float64(now), "holder": "other"}
	})
	replaced := holdWait("batch", "why-replaced", cfg, burst, burst.until, object{"startedAt": float64(now - 60), "holder": "gone", "resumeAt": burst.until}, false)
	if !strings.Contains(replaced.stop, T("wait.pauseReason", T("hit.budget"))) {
		t.Errorf("a held wait replaced by a daily-budget pause does not say why that pause came: %q", replaced.stop)
	}
	projection := &waitPlan{window: "seven_day", label: windowLabel("seven_day"), used: 80, threshold: 89, until: float64(now + 86400), hit: "projection"}
	if notice := alreadyOverNotice(projection); !strings.Contains(notice, T("wait.pauseReason", T("hit.projection"))) {
		t.Errorf("the session-start notice for a projected pause does not say why: %q", notice)
	}
	plain := &waitPlan{window: "five_hour", label: windowLabel("five_hour"), used: 93, threshold: 92, until: float64(now + 3600), hit: "threshold"}
	if stop, want := savedStop(cfg, "batch", plain, plain.until, ""), savedNotice(cfg, plain.label, formatNumber(93), formatTime(plain.until), ""); stop != want {
		t.Errorf("a pause at its pause point changed its wording: %q, want %q", stop, want)
	}
	if notice, want := alreadyOverNotice(plain), T("session.alreadyOver", plain.label, formatNumber(93), formatTime(plain.until))+T("session.pauseHint"); notice != want {
		t.Errorf("the session-start notice at the pause point changed its wording: %q, want %q", notice, want)
	}

	why := " " + T("wait.pauseReason", T("hit.burst"))
	early := limitCfgWith(t, cfg, object{"resetMarginSeconds": float64(0)})
	held := &waitPlan{window: "five_hour", label: windowLabel("five_hour"), used: 88, threshold: 92, until: float64(nowSec() + 2), hit: "burst"}
	outcome := enforceWait("batch", agentHookInput("PostToolBatch", "why-early", project, nil), early, decision{wait: held, usage: currentUsage(nowSec())})
	if want := T("wait.earlyReset", held.label, durationText(0)) + why; outcome.stop != "" || outcome.notice != want {
		t.Errorf("a burst wait held in the hook and ended by an early reset: %+v, want the notice %q", outcome, want)
	}
	full := limitCfgWith(t, cfg, object{"resetMarginSeconds": float64(0), "earlyResetPollMinutes": float64(0)})
	held = &waitPlan{window: "five_hour", label: windowLabel("five_hour"), used: 88, threshold: 92, until: float64(nowSec() + 2), hit: "burst"}
	outcome = enforceWait("batch", agentHookInput("PostToolBatch", "why-full", project, nil), full, decision{wait: held, usage: currentUsage(nowSec())})
	if want := T("wait.resumed", held.label, formatNumber(88), durationText(0)) + why; outcome.stop != "" || outcome.notice != want {
		t.Errorf("a burst wait held in the hook to its end: %+v, want the notice %q", outcome, want)
	}
}

func limitCfgWith(t *testing.T, cfg object, wait object) object {
	t.Helper()
	next := cloneObject(cfg)
	merged := cloneObject(section(cfg, "wait"))
	for key, value := range wait {
		merged[key] = value
	}
	next["wait"] = merged
	return next
}
