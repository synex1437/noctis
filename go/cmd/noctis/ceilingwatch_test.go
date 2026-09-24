package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func unguardedConfig() object {
	return object{
		"thresholds": object{"session5h": nil, "weeklyAll": nil, "weeklyFable": nil},
		"credits":    object{"allowPaid": false, "ceiling": float64(100), "fanOutHeadroom": float64(25)},
		"budget":     object{},
		"fable":      object{"source": "oauth"},
	}
}

func fiveHourAt(win window) usageView {
	now := float64(nowSec())
	if win.resetsAt == 0 {
		win.resetsAt = now + 3600
	}
	return usageView{hasAny: true, updatedAt: now, fiveHour: &win, sevenDay: &window{used: 20, resetsAt: now + 3*86400}}
}

func TestTheCeilingIsWatchedLikeAThreshold(t *testing.T) {
	cfg := unguardedConfig()
	bursting := fiveHourAt(window{used: 97, burst: 6, projected: 97})
	if plan := evaluate(cfg, bursting, "claude-opus-5-5", 0, false).wait; plan == nil || plan.hit != "ceiling" || plan.window != "five_hour" {
		t.Fatalf("with every threshold off, a 5-hour window at 97%% whose last reading jumped 6 points was left running into the paid-credit ceiling: %+v", plan)
	}
	stale := fiveHourAt(window{used: 97, staleness: 600, projected: 107})
	if plan := evaluate(cfg, stale, "claude-opus-5-5", 0, false).wait; plan == nil || plan.hit != "ceiling" {
		t.Fatalf("with every threshold off, a ten-minute-old 97%% reading that projects to 107%% was left running into the paid-credit ceiling: %+v", plan)
	}
	if plan := evaluate(cfg, fiveHourAt(window{used: 97, projected: 97}), "claude-opus-5-5", 0, false).wait; plan != nil {
		t.Fatalf("a fresh, flat 97%% reading was stopped, though nothing says the next turn crosses 100%%: %+v", plan)
	}
	if label := hitLabel(evaluate(cfg, bursting, "claude-opus-5-5", 0, false).wait); label != T("hit.ceilingSoon") {
		t.Fatalf("a stop at 97%%, before the ceiling, was worded %q", label)
	}
	if label := hitLabel(evaluate(cfg, fiveHourAt(window{used: 100, projected: 100}), "claude-opus-5-5", 0, false).wait); label != T("hit.ceiling") {
		t.Fatalf("a stop at 100%% lost its wording: %q", label)
	}
	if !nearEdge(cfg, fiveHourAt(window{used: 93, projected: 93})) {
		t.Fatal("with every threshold off, a window at 93%, 7 points from the paid-credit ceiling, was not polled like one near its edge")
	}
	if nearEdge(cfg, fiveHourAt(window{used: 91, projected: 91})) {
		t.Fatal("a window at 91%, 9 points from the ceiling, was taken as near the edge")
	}
	if seconds := edgePollSeconds(cfg, fiveHourAt(window{used: 98.5, projected: 98.5})); seconds > nearEdgePollClose {
		t.Fatalf("1.5 points from the paid-credit ceiling the next poll is %v s away", seconds)
	}
	cfg["credits"] = object{"allowPaid": true}
	for _, usage := range []usageView{bursting, stale} {
		if plan := evaluate(cfg, usage, "claude-opus-5-5", 0, false).wait; plan != nil {
			t.Fatalf("credits.allowPaid is on, so the overflow is the user's to pay, but the session was stopped: %+v", plan)
		}
	}
	if nearEdge(cfg, fiveHourAt(window{used: 97, projected: 97})) {
		t.Fatal("with paid credits allowed and every threshold off there is no edge to poll for")
	}
}

func TestAPausedGuardChecksStaleUsageNearTheCeiling(t *testing.T) {
	sandboxFiles(t)
	cfg := unguardedConfig()
	now := nowSec()
	reset, weekReset := float64(now+2*3600), float64(now+3*86400)
	paused := object{"disabledUntil": float64(now + 3600)}
	mustWriteJSON(files.fable, object{"fetchedAt": float64(now - 1800), "five_hour": object{"used": float64(97), "resetsAt": reset}, "seven_day": object{"used": float64(20), "resetsAt": weekReset}})
	served := limitsServer(t, limitsBody(100, reset, 20, weekReset))
	if guardPaused(cfg, paused, now) {
		t.Fatal("the guard stayed paused on a half-hour-old 97% reading while the account had reached 100%: the paid-credit ceiling was never checked again")
	}
	if got := served.Load(); got != 1 {
		t.Fatalf("the paused guard asked the usage endpoint %d times near the ceiling, want 1", got)
	}

	sandboxFiles(t)
	served = limitsServer(t, limitsBody(100, reset, 20, weekReset))
	mustWriteJSON(files.fable, object{"fetchedAt": float64(now - 1800), "five_hour": object{"used": float64(40), "resetsAt": reset}, "seven_day": object{"used": float64(20), "resetsAt": weekReset}})
	statusReading(now, 50, reset, 20, weekReset)
	if !guardPaused(cfg, paused, now) {
		t.Fatal("far from the ceiling a paused guard stopped holding")
	}
	if got := served.Load(); got != 0 {
		t.Fatalf("far from the ceiling a paused guard asked the usage endpoint %d times", got)
	}
}

func TestAWindowAboutToCrossTheCeilingIsHeldLikeOneAtIt(t *testing.T) {
	cfg, project, _ := limitSandbox(t, object{"thresholds": object{"session5h": float64(0), "weeklyAll": float64(0), "weeklyFable": float64(0)}, "wait": object{"maxInHookMinutes": 1}}, 40, 20)
	now := nowSec()
	reset, weekReset := float64(now+7200), float64(now+3*86400)
	statusReading(now-600, 91, reset, 20, weekReset)
	statusReading(now-60, 97, reset, 20, weekReset)
	output := hookOutput(t, onUserPromptSubmit, promptInput("cs-control", project, "/noctis:status"), cfg)
	if reason := getString(output, "reason"); getString(output, "decision") != "block" || !strings.Contains(reason, T("hit.ceilingSoon")) {
		t.Fatalf("with every threshold off and the 5-hour window at 97%% after a 6-point jump, /noctis:status went on toward the paid-credit ceiling: %v", output)
	}
	if guardPaused(cfg, object{"disabledUntil": float64(now + 3600)}, now) {
		t.Fatal("a paused guard kept the session running at 97% after a 6-point jump, toward the paid-credit ceiling")
	}
	reason := subagentLimitReason(&waitPlan{window: "five_hour", label: "5h", used: 97, threshold: 100, until: reset, hit: "ceiling"}, &waitPlan{window: "five_hour", label: "5h", used: 97, threshold: 100, until: reset, hit: "ceiling"})
	if strings.Contains(reason, "at the paid-credit ceiling") || !strings.Contains(reason, "paid-credit ceiling") {
		t.Fatalf("a subagent stopped at 97%%, before the ceiling, is told it is at the ceiling: %s", reason)
	}
}

func TestTheCheckGateCountsAWindowAboutToCrossTheCeiling(t *testing.T) {
	account := t.TempDir()
	guard := filepath.Join(account, pluginName)
	now := float64(nowSec())
	reset := now + 7200
	cliWrite(t, filepath.Join(guard, "config.json"), marshalCompact(object{"thresholds": object{"session5h": float64(0), "weeklyAll": float64(0), "weeklyFable": float64(0)}}))
	cliWrite(t, filepath.Join(guard, "usage.json"), marshalCompact(object{
		"updatedAt": now,
		"five_hour": object{"used": float64(97), "resetsAt": reset, "at": now},
		"seven_day": object{"used": float64(20), "resetsAt": now + 3*86400, "at": now},
		"history":   object{"five_hour": []any{object{"used": float64(91), "resetsAt": reset, "at": now - 600}, object{"used": float64(97), "resetsAt": reset, "at": now - 60}}},
	}))
	run := runNoctisCLI(t, cliAccountEnv(repoRoot(), account), "check", "--json")
	if run.code != 11 || !strings.Contains(run.stdout, T("hit.ceilingSoon")) {
		t.Fatalf("with every threshold off, noctis check called a window at 97%% after a 6-point jump fine, though the hooks stop there before the paid-credit ceiling:\n%s", run)
	}
}

func TestACeilingPauseOnAStaleTrendEndsWhenFreshDataShowsRoom(t *testing.T) {
	dir := sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	cfg := unguardedConfig()
	cfg["wait"] = object{"earlyResetPollMinutes": float64(5), "maxInHookMinutes": float64(1), "workspaceGuard": false}
	cfg["alarm"] = object{"enabled": false}
	now := nowSec()
	reset, weekReset := float64(now+4*3600), float64(now+3*86400)
	statusReading(now-1200, 88, reset, 20, weekReset)
	statusReading(now-900, 93, reset, 20, weekReset)
	stale := evaluate(cfg, currentUsage(now), "claude-opus-5-5", 0, false)
	if stale.wait == nil || stale.wait.hit != "ceiling" || stale.wait.used >= stale.wait.threshold {
		t.Fatalf("with every threshold off, a 15-minute-old 93%% reading that trends past 100%% should stop work before the paid-credit ceiling: %+v", stale.wait)
	}
	enforceWait("batch", object{"session_id": "rel", "cwd": dir}, cfg, stale)
	record := getMap(getMap(readState(), "waits"), "rel")
	if record == nil || getBool(record, "inHook", true) {
		t.Fatalf("the pause before the ceiling was not parked: %v", record)
	}
	statusReading(nowSec(), 93, reset, 20, weekReset)
	if plan := evaluate(cfg, currentUsage(nowSec()), "claude-opus-5-5", 0, false).wait; plan != nil {
		t.Fatalf("a fresh, flat 93%% reading still stops work: %+v", plan)
	}
	if got := earlyRelease(cfg, "rel", record, false, false); got != "data" {
		t.Fatalf("a pause taken before the paid-credit ceiling on a stale trend held on (%q) after a fresh reading showed the window flat at 93%%, so it would last until the reset at %s", got, formatTime(reset))
	}
	if notice := readyNotice(record, "data", nowSec(), ""); notice != T("wait.dataReady", getString(record, "label"), "") {
		t.Fatalf("a session let go on fresh data before the ceiling is told %q", notice)
	}
	for cause, used := range map[string]float64{"burst": 97, "threshold": 100} {
		held := pauseRecord("ceiling", used, 100, numberOr(record, "startedAt", 0), reset)
		held["cause"] = cause
		if got := earlyRelease(cfg, "rel", held, false, false); got != "" {
			t.Fatalf("a stop at the paid-credit ceiling on a %s at %v%% ended (%s) on a fresh reading, before its window reset", cause, used, got)
		}
	}
}

func TestARescheduledCeilingPauseDescribesTheStopNowInForce(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	previous := args
	t.Cleanup(func() { args = previous })
	args = parseArgs([]string{"resume", "--sid", "rel"})
	cfg := unguardedConfig()
	cfg["fable"] = object{}
	cfg["wait"] = object{"earlyResetPollMinutes": float64(5)}
	mustWriteJSON(files.config, cfg)
	now := nowSec()
	reset, weekReset := float64(now+4*3600), float64(now+3*86400)
	record := pauseRecord("ceiling", 97, 100, float64(now-1500), reset)
	record["cause"], record["inHook"] = "burst", false
	updateState(func(state object) { stateMap(state, "waits")["rel"] = record })
	statusReading(now-1200, 88, reset, 20, weekReset)
	statusReading(now-900, 93, reset, 20, weekReset)
	runResume()
	stored := getMap(getMap(readState(), "waits"), "rel")
	if stored == nil || getString(stored, "hit") != "ceiling" || getString(stored, "cause") != "projection" {
		t.Fatalf("the runner kept a pause before the ceiling that now rests on a stale trend, not a burst, described as %v", stored)
	}
	statusReading(nowSec(), 93, reset, 20, weekReset)
	if got := earlyRelease(loadConfig(), "rel", stored, false, false); got != "data" {
		t.Fatalf("the rescheduled pause held on (%q) after a fresh reading showed the window flat at 93%%", got)
	}
}

func TestNoUsageDataNearTheCeilingEndsInABlindPause(t *testing.T) {
	dir := sandboxFiles(t)
	probes := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		probes.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	t.Setenv("NOCTIS_USAGE_URL", server.URL)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "lab")
	now := nowSec()
	reset, weekReset := float64(now+2*3600), float64(now+3*86400)
	mustWriteJSON(files.usage, object{"updatedAt": float64(now - 120), "five_hour": object{"used": float64(97), "resetsAt": reset}, "seven_day": object{"used": float64(20), "resetsAt": weekReset}})
	mustWriteJSON(files.fable, object{"fetchedAt": float64(now - 3600), "error": "http-503", "backoffUntil": float64(now + 600)})
	cfg := unguardedConfig()
	cfg["usage"] = object{"blindProbeSeconds": float64(1), "blindProbeRounds": float64(1)}
	for _, thresholds := range []object{
		{"session5h": nil, "weeklyAll": nil, "weeklyFable": nil},
		{"session5h": nil, "weeklyAll": float64(89), "weeklyFable": nil},
	} {
		cfg["thresholds"] = thresholds
		before := probes.Load()
		result := decide(cfg, readState(), object{"session_id": "blind-ceiling", "cwd": dir}, now, decideOptions{})
		if probes.Load() == before {
			t.Fatalf("with thresholds %v the usage endpoint was not probed 3 points from the paid-credit ceiling", thresholds)
		}
		if plan := result.wait; plan == nil || plan.hit != "blind" || plan.window != "five_hour" || plan.threshold != 100 || plan.until != reset {
			t.Fatalf("with thresholds %v, a 5-hour window at 97%% whose data stopped two minutes ago was probed and then %+v: work went on toward the paid-credit ceiling without data, or waited on the wrong window", thresholds, plan)
		}
	}
	cfg["thresholds"] = object{"session5h": nil, "weeklyAll": nil, "weeklyFable": nil}
	cfg["wait"] = object{"earlyResetPollMinutes": float64(5)}
	statusReadingFrom("blind-ceiling", nowSec(), 97.5, reset, 20, weekReset)
	if got := earlyRelease(cfg, "blind-ceiling", pauseRecord("blind", 97, 100, float64(now), reset), false, false); got != "data" {
		t.Fatalf("a blind stop near the paid-credit ceiling held on (%q) after fresh data showed the window flat at 97.5%%", got)
	}
	cfg["credits"] = object{"allowPaid": true}
	before := probes.Load()
	if plan := decide(cfg, readState(), object{"session_id": "blind-ceiling", "cwd": dir}, now, decideOptions{}).wait; plan != nil || probes.Load() != before {
		t.Fatalf("with paid credits allowed and every threshold off there is no stop to guard, yet the endpoint was probed %d times and the session got %+v", probes.Load()-before, plan)
	}
}
