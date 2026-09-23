package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func releaseConfig() object {
	return object{
		"thresholds": object{"session5h": float64(92), "weeklyAll": float64(89), "weeklyFable": float64(95)},
		"credits":    object{},
		"budget":     object{},
		"usage":      object{},
		"wait":       object{"earlyResetPollMinutes": float64(5)},
	}
}

func statusReading(at int64, five, fiveReset, week, weekReset float64) {
	statusReadingFrom("rel", at, five, fiveReset, week, weekReset)
}

func statusReadingFrom(sid string, at int64, five, fiveReset, week, weekReset float64) {
	recordStatusline(object{
		"session_id": sid,
		"rate_limits": object{
			"five_hour": object{"used_percentage": five, "resets_at": fiveReset},
			"seven_day": object{"used_percentage": week, "resets_at": weekReset},
		},
	}, at, true)
}

func limitsBody(five, fiveReset, week, weekReset float64) string {
	stamp := func(epoch float64) string { return time.Unix(int64(epoch), 0).UTC().Format(time.RFC3339) }
	return `{"limits":[{"kind":"session","utilization":` + formatNumber(five) + `,"resets_at":"` + stamp(fiveReset) + `"},{"kind":"weekly_all","utilization":` + formatNumber(week) + `,"resets_at":"` + stamp(weekReset) + `"}]}`
}

func limitsServer(t *testing.T, bodies ...string) *atomic.Int64 {
	t.Helper()
	served := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		index := int(served.Add(1)) - 1
		if index >= len(bodies) {
			index = len(bodies) - 1
		}
		_, _ = io.WriteString(w, bodies[index])
	}))
	t.Cleanup(server.Close)
	t.Setenv("NOCTIS_USAGE_URL", server.URL)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "lab")
	return served
}

func pauseRecord(hit string, used, threshold float64, startedAt, until float64) object {
	return object{
		"kind": "batch", "window": "five_hour", "label": "5h", "used": used, "threshold": threshold, "hit": hit,
		"startedAt": startedAt, "until": until, "resumeAt": until, "inHook": true,
	}
}

func inHookRelease(cfg, record object) string {
	return earlyRelease(cfg, "rel", record, false, false)
}

func TestABurstPauseHoldsUntilTheWindowReallyResets(t *testing.T) {
	sandboxFiles(t)
	cfg := releaseConfig()
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReading(now-600, 54, reset, 20, weekReset)
	statusReading(now-120, 75, reset, 20, weekReset)
	if plan := evaluate(cfg, currentUsage(now), "claude-opus-5", 0, false).wait; plan == nil || plan.hit != "burst" {
		t.Fatalf("the setup should pause on a burst, got %v", plan)
	}
	record := pauseRecord("burst", 75, 92, float64(now-60), reset)
	if got := inHookRelease(cfg, record); got != "" {
		t.Fatalf("a burst pause ended (%s) before any new reading arrived", got)
	}
	statusReading(now, 75.5, reset, 20, weekReset)
	if got := inHookRelease(cfg, record); got != "" {
		t.Fatalf("a burst pause ended (%s) at the first fresh reading although the window never reset", got)
	}
	statusReading(now, 3, float64(now+5*3600), 20, weekReset)
	if got := inHookRelease(cfg, record); got != "reset" {
		t.Fatalf("a burst pause did not end as a reset when the window really reset: %q", got)
	}
}

func TestACompactionPauseHoldsUntilTheWindowReallyResets(t *testing.T) {
	sandboxFiles(t)
	cfg := releaseConfig()
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReading(now-120, 87, reset, 20, weekReset)
	record := pauseRecord("compaction", 87, 92, float64(now-60), reset)
	statusReading(now, 81, reset, 20, weekReset)
	if got := inHookRelease(cfg, record); got != "" {
		t.Fatalf("a compaction pause ended (%s) on a reading that is no reset of the window", got)
	}
}

func TestAThresholdPauseStillEndsOnAnEarlyReset(t *testing.T) {
	sandboxFiles(t)
	cfg := releaseConfig()
	now := nowSec()
	reset, weekReset := float64(now+900), float64(now+3*86400)
	statusReading(now-120, 93, reset, 20, weekReset)
	record := pauseRecord("threshold", 93, 92, float64(now-60), reset)
	statusReading(now, 83, reset, 20, weekReset)
	if got := inHookRelease(cfg, record); got != "" {
		t.Fatalf("a threshold pause ended (%s) just below the threshold", got)
	}
	statusReading(now, 2, float64(now+18000), 20, weekReset)
	if got := inHookRelease(cfg, record); got != "reset" {
		t.Fatalf("a threshold pause did not end on an early reset: %q", got)
	}
}

func TestABlindPauseEndsWhenFreshDataShowsRoom(t *testing.T) {
	sandboxFiles(t)
	cfg := releaseConfig()
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReading(now-300, 86, reset, 20, weekReset)
	record := pauseRecord("blind", 86, 92, float64(now-60), reset)
	if got := inHookRelease(cfg, record); got != "" {
		t.Fatalf("a blind pause ended (%s) before any data came back", got)
	}
	statusReading(now, 86.5, reset, 20, weekReset)
	if got := inHookRelease(cfg, record); got != "data" {
		t.Fatalf("a blind pause stayed parked although fresh data shows the window below its threshold: %q", got)
	}
	statusReading(now, 93, reset, 20, weekReset)
	if got := inHookRelease(cfg, record); got != "" {
		t.Fatalf("a blind pause ended (%s) on fresh data that is over the threshold", got)
	}
}

func TestABlindPauseStaysWhileFreshDataStillShowsABurst(t *testing.T) {
	sandboxFiles(t)
	cfg := releaseConfig()
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReading(now-900, 60, reset, 20, weekReset)
	statusReading(now-300, 86, reset, 20, weekReset)
	record := pauseRecord("blind", 86, 92, float64(now-60), reset)
	statusReading(now, 86.5, reset, 20, weekReset)
	if got := inHookRelease(cfg, record); got != "" {
		t.Fatalf("a blind pause ended (%s) although the fresh data still shows a burst toward the limit", got)
	}
}

func TestABlindPauseStaysWhileTheOtherWindowIsOverItsThreshold(t *testing.T) {
	sandboxFiles(t)
	cfg := releaseConfig()
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReading(now-300, 86, reset, 20, weekReset)
	record := pauseRecord("blind", 86, 92, float64(now-60), reset)
	statusReading(now, 86.5, reset, 90, weekReset)
	if got := inHookRelease(cfg, record); got != "" {
		t.Fatalf("a blind pause ended (%s) although the weekly window is over its threshold", got)
	}
}

func TestABlindPauseStaysWhileTheDailyBudgetHardStops(t *testing.T) {
	sandboxFiles(t)
	cfg := releaseConfig()
	cfg["budget"] = object{"dailyWeeklyPercent": float64(10), "hardStop": true}
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReading(now-300, 86, reset, 20, weekReset)
	updateState(func(state object) {
		state["budgetDay"] = object{"day": localDay(now), "weekResetsAt": weekReset, "startUsed": float64(5)}
	})
	record := pauseRecord("blind", 86, 92, float64(now-60), reset)
	statusReading(now, 86.5, reset, 20, weekReset)
	if got := inHookRelease(cfg, record); got != "" {
		t.Fatalf("a blind pause ended (%s) although the daily budget still stops the session", got)
	}
}

func TestADataPauseNeedsAReadingThatIsStillFresh(t *testing.T) {
	sandboxFiles(t)
	cfg := releaseConfig()
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReading(now-400, 86, reset, 20, weekReset)
	record := pauseRecord("blind", 86, 92, float64(now-300), reset)
	statusReading(now-200, 86.5, reset, 20, weekReset)
	if got := inHookRelease(cfg, record); got != "" {
		t.Fatalf("a blind pause ended (%s) on a reading that is minutes old", got)
	}
}

func TestAProjectionPauseEndsWhenFreshDataShowsRoom(t *testing.T) {
	sandboxFiles(t)
	cfg := releaseConfig()
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReading(now-900, 85, reset, 20, weekReset)
	record := pauseRecord("projection", 85, 92, float64(now-60), reset)
	statusReading(now, 85.5, reset, 20, weekReset)
	if got := inHookRelease(cfg, record); got != "data" {
		t.Fatalf("a projection pause stayed parked although fresh data shows the window below its threshold: %q", got)
	}
}

func TestAStatusLineWithoutLimitsDoesNotEndAProjectionPause(t *testing.T) {
	sandboxFiles(t)
	cfg := releaseConfig()
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReading(now-900, 70, reset, 20, weekReset)
	record := pauseRecord("projection", 70, 92, float64(now-60), reset)
	recordStatusline(object{"session_id": "rel"}, now, true)
	if got := inHookRelease(cfg, record); got != "" {
		t.Fatalf("a status line that carried no usage ended a projection pause (%s) on the old reading", got)
	}
}

func TestAStatusLineWithoutLimitsDoesNotEndABlindPause(t *testing.T) {
	sandboxFiles(t)
	cfg := releaseConfig()
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReading(now-900, 86, reset, 20, weekReset)
	record := pauseRecord("blind", 86, 92, float64(now-60), reset)
	recordStatusline(object{"session_id": "rel"}, now, true)
	if got := inHookRelease(cfg, record); got != "" {
		t.Fatalf("a status line that carried no usage ended a blind pause (%s) on the old reading", got)
	}
}

func TestAWindowRemembersWhenItWasLastReported(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReading(now-300, 40, reset, 20, weekReset)
	recordStatusline(object{"session_id": "rel"}, now, true)
	if got := currentUsage(now).fiveHour.reportedAt; got != float64(now-300) {
		t.Fatalf("a status line without usage moved the five-hour reading time to %v, want %v", got, now-300)
	}
	if err := os.Remove(files.usage); err != nil {
		t.Fatal(err)
	}
	mustWriteJSON(files.fable, object{"fetchedAt": float64(now - 30), "five_hour": object{"used": float64(41), "resetsAt": reset}})
	if got := currentUsage(now).fiveHour.reportedAt; got != float64(now-30) {
		t.Fatalf("an API reading was dated %v, want its fetch time %v", got, now-30)
	}
}

func TestAWeeklyBlindPauseEndsWhenTheUsageEndpointAnswersAgain(t *testing.T) {
	dir := sandboxFiles(t)
	now := nowSec()
	weekReset := float64(now + 5*86400)
	fiveReset := float64(now + 3*3600)
	limitsServer(t, limitsBody(30, fiveReset, 84.5, weekReset))
	cfg := releaseConfig()
	cfg["fable"] = object{"source": "oauth"}
	mustWriteJSON(files.fable, object{"fetchedAt": float64(now - 3600), "seven_day": object{"used": float64(84), "resetsAt": weekReset}, "five_hour": object{"used": float64(30), "resetsAt": fiveReset}})
	transcript := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	quiet := time.Unix(now-300, 0)
	if err := os.Chtimes(transcript, quiet, quiet); err != nil {
		t.Fatal(err)
	}
	record := object{
		"kind": "batch", "window": "seven_day", "label": "weekly", "used": float64(84), "threshold": float64(89), "hit": "blind",
		"startedAt": float64(now - 120), "until": weekReset, "resumeAt": weekReset, "inHook": false, "transcript": transcript,
	}
	if got := earlyRelease(cfg, "rel", record, true, true); got != "data" {
		t.Fatalf("a parked weekly blind pause stayed parked after the usage endpoint answered with room: %q", got)
	}
}

func TestAParkedDataPauseIsNotRelaunchedOverAContinuedSession(t *testing.T) {
	dir := sandboxFiles(t)
	cfg := releaseConfig()
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReading(now-400, 86, reset, 20, weekReset)
	transcript := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	paused := time.Unix(now-300, 0)
	if err := os.Chtimes(transcript, paused, paused); err != nil {
		t.Fatal(err)
	}
	record := pauseRecord("blind", 86, 92, float64(now-300), reset)
	record["inHook"], record["transcript"] = false, transcript
	statusReading(now, 86.5, reset, 20, weekReset)
	if got := earlyRelease(cfg, "rel", record, false, true); got != "data" {
		t.Fatalf("a parked blind pause of a quiet session stayed parked on fresh data with room: %q", got)
	}
	active := time.Unix(now-10, 0)
	if err := os.Chtimes(transcript, active, active); err != nil {
		t.Fatal(err)
	}
	if got := earlyRelease(cfg, "rel", record, false, true); got != "" {
		t.Fatalf("a relaunch was allowed (%s) although the session went on after the pause", got)
	}
	if got := earlyRelease(cfg, "rel", record, false, false); got != "data" {
		t.Fatalf("the waiting hook itself was kept waiting on fresh data with room: %q", got)
	}
	record["transcript"] = filepath.Join(dir, "missing.jsonl")
	if got := earlyRelease(cfg, "rel", record, false, true); got != "" {
		t.Fatalf("a relaunch was allowed (%s) for a session whose transcript cannot be checked", got)
	}
}

func TestARescheduledWaitDescribesThePauseNowInForce(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	previous := args
	t.Cleanup(func() { args = previous })
	args = parseArgs([]string{"resume", "--sid", "rel"})
	mustWriteJSON(files.config, releaseConfig())
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReading(now-600, 54, reset, 20, weekReset)
	statusReading(now-120, 75, reset, 20, weekReset)
	record := pauseRecord("burst", 75, 92, float64(now-600), reset)
	record["inHook"] = false
	updateState(func(state object) { stateMap(state, "waits")["rel"] = record })
	statusReading(now, 93, reset, 20, weekReset)
	runResume()
	stored := getMap(getMap(readState(), "waits"), "rel")
	if stored == nil {
		t.Fatal("the wait disappeared although the window is over its threshold")
	}
	if getString(stored, "hit") != "threshold" || numberOr(stored, "used", 0) != 93 || numberOr(stored, "threshold", 0) != 92 {
		t.Fatalf("the rescheduled wait still describes the old pause: hit=%v used=%v threshold=%v", stored["hit"], stored["used"], stored["threshold"])
	}
	if numberOr(stored, "startedAt", 0) < float64(now) {
		t.Fatalf("the rescheduled wait kept its old start: %v", stored["startedAt"])
	}
}

func inHookConfig() object {
	cfg := releaseConfig()
	cfg["wait"] = object{"earlyResetPollMinutes": 0.05, "resetMarginSeconds": float64(0), "builtinGraceSeconds": float64(0), "maxInHookMinutes": float64(330), "workspaceGuard": false}
	cfg["alarm"] = object{"enabled": false}
	cfg["fable"] = object{"source": "oauth"}
	return cfg
}

func journalActions() []string {
	actions := []string{}
	for _, line := range tailFileLines(files.decisions, 50) {
		var entry object
		if err := jsonUnmarshalObject([]byte(line), &entry); err == nil {
			actions = append(actions, getString(entry, "action"))
		}
	}
	return actions
}

func hasAction(actions []string, wanted string) bool {
	for _, action := range actions {
		if action == wanted {
			return true
		}
	}
	return false
}

func TestAnInHookBurstPauseOutlastsAFreshReadingAndEndsOnTheRealReset(t *testing.T) {
	dir := sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	cfg := inHookConfig()
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReading(now-600, 54, reset, 20, weekReset)
	statusReading(now-120, 75, reset, 20, weekReset)
	served := limitsServer(t, limitsBody(75.5, reset, 20, weekReset), limitsBody(3, float64(now+5*3600), 20, weekReset))
	plan := &waitPlan{window: "five_hour", label: "5h", used: 75, threshold: 92, until: float64(now + 30), hit: "burst"}
	outcome := enforceWait("batch", object{"session_id": "rel", "cwd": dir}, cfg, decision{wait: plan, model: "claude-opus-5"})
	actions := journalActions()
	if got := served.Load(); got < 2 {
		t.Fatalf("the in-hook burst pause was let go after %d fresh reading(s), before its window reset: %q %v", got, outcome.notice, actions)
	}
	if !hasAction(actions, "early-reset") || hasAction(actions, "data-back") {
		t.Fatalf("the burst pause did not end on the real reset of its window: %v", actions)
	}
	if float64(nowSec()) >= plan.until || outcome.stop != "" || outcome.notice != T("wait.earlyReset", plan.label, durationText(float64(nowSec()-now))) {
		t.Fatalf("the session was not let go as soon as the window reset: %+v", outcome)
	}
	if getMap(getMap(readState(), "waits"), "rel") != nil {
		t.Fatal("the wait record outlived the in-hook release")
	}
}

func TestAnInHookBlindPauseEndsWhenFreshDataShowsRoom(t *testing.T) {
	dir := sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	cfg := inHookConfig()
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReading(now-300, 86, reset, 20, weekReset)
	served := limitsServer(t, limitsBody(86.5, reset, 20, weekReset))
	plan := &waitPlan{window: "five_hour", label: "5h", used: 86, threshold: 92, until: float64(now + 30), hit: "blind"}
	outcome := enforceWait("batch", object{"session_id": "rel", "cwd": dir}, cfg, decision{wait: plan, model: "claude-opus-5"})
	if served.Load() == 0 || float64(nowSec()) >= plan.until {
		t.Fatalf("a blind pause held until its reset although the usage endpoint answered with room (%d reading(s))", served.Load())
	}
	actions := journalActions()
	if !hasAction(actions, "data-back") || hasAction(actions, "early-reset") {
		t.Fatalf("the release was not journaled as data coming back: %v", actions)
	}
	if outcome.stop != "" || outcome.notice != T("wait.resumed", plan.label, formatNumber(plan.used), durationText(float64(nowSec()-now))) {
		t.Fatalf("the session was not let go with the plain resume notice: %+v", outcome)
	}
	if getMap(getMap(readState(), "waits"), "rel") != nil {
		t.Fatal("the wait record outlived the in-hook release")
	}
}

func TestAResumeNoticeSaysWhatEndedThePause(t *testing.T) {
	now := nowSec()
	tail := T("wait.readyTail")
	blind := object{"hit": "blind", "label": "weekly", "until": float64(now + 4*86400)}
	if got, want := readyNotice(blind, "reset", now, tail), T("wait.ready", "weekly", tail); got != want {
		t.Fatalf("a blind pause ended by a real reset of its window was announced as %q, want %q", got, want)
	}
	if got, want := readyNotice(blind, "data", now, tail), T("wait.dataReady", "weekly", tail); got != want {
		t.Fatalf("a blind pause ended by fresh data was announced as %q, want %q", got, want)
	}
	if got, want := readyNotice(blind, "", now, tail), T("wait.dataReady", "weekly", tail); got != want {
		t.Fatalf("a blind pause resumed before its reset without a known cause was announced as %q, want %q", got, want)
	}
	projection := object{"hit": "projection", "label": "5h", "until": float64(now - 30)}
	if got, want := readyNotice(projection, "", now, tail), T("wait.ready", "5h", tail); got != want {
		t.Fatalf("a projection pause resumed after its reset was announced as %q, want %q", got, want)
	}
	threshold := object{"hit": "threshold", "label": "5h", "until": float64(now + 900)}
	if got, want := readyNotice(threshold, "", now, tail), T("wait.ready", "5h", tail); got != want {
		t.Fatalf("a threshold pause was announced as %q, want %q", got, want)
	}
}

func TestOlderNumbersFromAnotherSessionDoNotEndADataPause(t *testing.T) {
	sandboxFiles(t)
	cfg := releaseConfig()
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReading(now-300, 86, reset, 20, weekReset)
	record := pauseRecord("blind", 86, 92, float64(now-60), reset)
	statusReadingFrom("idle", now, 80, reset, 20, weekReset)
	if got := currentUsage(now).fiveHour.reportedAt; got != float64(now-300) {
		t.Fatalf("the kept five-hour reading was dated %v, want the time it was reported, %v", got, now-300)
	}
	if got := inHookRelease(cfg, record); got != "" {
		t.Fatalf("a blind pause ended (%s) on another session's older, lower numbers while the kept reading predates the pause", got)
	}
	statusReadingFrom("idle", now, 86.5, reset, 20, weekReset)
	if got := inHookRelease(cfg, record); got != "data" {
		t.Fatalf("a blind pause stayed parked after another session reported fresh numbers with room: %q", got)
	}
}
