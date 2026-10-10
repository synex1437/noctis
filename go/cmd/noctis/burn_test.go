package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func burnConfig(burn object) object {
	cfg := testConfig()
	cfg["thresholds"] = object{"session5h": float64(92), "weeklyAll": float64(95), "weeklyFable": float64(97)}
	if burn != nil {
		cfg["burn"] = burn
	}
	return cfg
}

func weekReadings(t *testing.T, now int64, weekReset float64, hoursAgoAndUsed ...float64) {
	t.Helper()
	for i := 0; i+1 < len(hoursAgoAndUsed); i += 2 {
		at := now - int64(hoursAgoAndUsed[i]*3600)
		statusReadingFrom("burn", at, 10, float64(at+3600), hoursAgoAndUsed[i+1], weekReset)
	}
}

func TestAWeekBurningFastStopsNewSubagentsLongBeforeItsReset(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	weekReset := float64(now + 5*86400)
	weekReadings(t, now, weekReset, 3, 30, 2, 33, 1, 36, 0, 39)

	view := weeklyBurn(burnConfig(nil), currentUsage(now), now)

	if !view.known || !view.early || !view.stop {
		t.Fatalf("9 points in 3 hours with 5 days to the reset gave no stop verdict: %+v", view)
	}
	if math.Abs(view.perHour-3) > 0.01 || math.Abs(view.runOut-56.0/3*3600) > 60 {
		t.Fatalf("the pace is %v points an hour and the run-out %v s; want 3 an hour and 56/3 hours", view.perHour, view.runOut)
	}
	if text := burnStatusText(view); !strings.Contains(text, "no new subagents") {
		t.Fatalf("the status line says %q while new subagents are refused", text)
	}
}

func TestAWeekThatRunsOutJustBeforeItsResetWarnsWithoutStoppingSubagents(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	weekReset := float64(now + 3*86400)
	weekReadings(t, now, weekReset, 3, 40, 2, 41, 1, 42, 0, 43)

	view := weeklyBurn(burnConfig(nil), currentUsage(now), now)

	if !view.early || view.stop {
		t.Fatalf("1 point an hour at 43%% with 72 h left runs out in 52 h: want a warning and no stop, got %+v", view)
	}
	if text := burnStatusText(view); !strings.Contains(text, "weekly threshold") || strings.Contains(text, "subagents") {
		t.Fatalf("the status line says %q for a warning", text)
	}
}

func TestAPaceThatLastsTheWeekGivesNoVerdict(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	weekReset := float64(now + 86400)
	weekReadings(t, now, weekReset, 3, 60, 0, 63)

	if view := weeklyBurn(burnConfig(nil), currentUsage(now), now); !view.known || view.early || view.stop || burnStatusText(view) != "" {
		t.Fatalf("1 point an hour at 63%% reaches 95%% in 32 h, after the reset 24 h away: want no verdict, got %+v", view)
	}
}

func TestTheBurnNeedsHalfAnHourOfReadingsAndTwoPointsOfRise(t *testing.T) {
	cases := []struct {
		name     string
		readings []float64
	}{
		{"ten points in twenty minutes", []float64{1.0 / 3, 30, 0, 40}},
		{"one point in three hours", []float64{3, 30, 0, 31}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sandboxFiles(t)
			now := nowSec()
			weekReadings(t, now, float64(now+5*86400), c.readings...)
			if view := weeklyBurn(burnConfig(nil), currentUsage(now), now); view.early || view.stop {
				t.Fatalf("a verdict from too little: %+v", view)
			}
		})
	}
}

func TestTheBurnLooksBackOnlyAsFarAsItsLookback(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	weekReset := float64(now + 5*86400)
	weekReadings(t, now, weekReset, 10, 20, 5, 50, 0, 51)

	view := weeklyBurn(burnConfig(nil), currentUsage(now), now)

	if !view.known || view.early || view.stop {
		t.Fatalf("30 points went 5 hours ago and 1 since; the 3-hour pace is a third of a point an hour, yet the verdict is %+v", view)
	}
	if view = weeklyBurn(burnConfig(object{"lookbackHours": float64(12)}), currentUsage(now), now); !view.stop {
		t.Fatalf("with a 12-hour lookback the 31 points of the last 10 hours stop new subagents, but the verdict is %+v", view)
	}
}

func TestAGapWithoutReadingsSpreadsItsRiseOverTheGap(t *testing.T) {
	cases := []struct {
		name     string
		weekLeft float64
		readings []float64
		stop     bool
	}{
		{"five points in the fifteen hours since the last reading", 3, []float64{15, 40, 0, 45}, false},
		{"six points in the two hours after a day without readings", 5, []float64{23, 5, 2, 6, 1, 9, 0, 12}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sandboxFiles(t)
			now := nowSec()
			weekReadings(t, now, float64(now)+c.weekLeft*86400, c.readings...)
			view := weeklyBurn(burnConfig(nil), currentUsage(now), now)
			if !view.known || view.stop != c.stop || view.early != c.stop {
				t.Fatalf("want a stop verdict %v, got %+v", c.stop, view)
			}
		})
	}
}

func TestANewWeekStartsItsPaceAfresh(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	weekReadings(t, now, float64(now+600), 3, 30, 1, 90)
	weekReadings(t, now, float64(now+7*86400), 0.25, 1, 0, 3)

	if view := weeklyBurn(burnConfig(nil), currentUsage(now), now); view.known || view.early || view.stop {
		t.Fatalf("the last week's readings set the new week's pace: %+v", view)
	}
}

func TestTheUsageEndpointsReadingsFeedThePace(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	weekReset := float64(now + 5*86400)
	history := object{}
	for _, sample := range [][2]float64{{3, 30}, {2, 33}, {1, 36}, {0, 39}} {
		appendHistory(history, "seven_day", sample[1], weekReset, now-int64(sample[0]*3600))
	}
	mustWriteJSON(files.fable, object{"fetchedAt": float64(now), "seven_day": object{"used": float64(39), "resetsAt": weekReset}, "history": history})

	if view := weeklyBurn(burnConfig(nil), currentUsage(now), now); !view.stop {
		t.Fatalf("9 points in 3 hours read from the usage endpoint gave no stop verdict: %+v", view)
	}
}

func TestTheBurnAlarmCanBeTurnedOffOrLoosened(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	weekReadings(t, now, float64(now+5*86400), 3, 30, 0, 39)
	usage := currentUsage(now)

	if view := weeklyBurn(burnConfig(object{"alarm": false}), usage, now); view.early || view.stop || burnStatusText(view) != "" {
		t.Fatalf("burn.alarm false still gives a verdict: %+v", view)
	}
	if view := weeklyBurn(burnConfig(object{"stopSubagents": float64(0)}), usage, now); !view.early || view.stop {
		t.Fatalf("burn.stopSubagents 0 should warn and never stop: %+v", view)
	}
	if view := weeklyBurn(burnConfig(object{"stopSubagents": float64(0.1)}), usage, now); view.stop {
		t.Fatalf("a run-out in 19 h of 120 is past a tenth of the time left, yet subagents stop: %+v", view)
	}
}

func TestABurnClaimsNoRefusalOfSubagentsWhileNothingRefusesThem(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	weekReadings(t, now, float64(now+5*86400), 3, 30, 2, 33, 1, 36, 0, 39)
	usage := currentUsage(now)
	guardOff := burnConfig(nil)
	guardOff["subagents"] = object{"guard": false}

	verdicts := []struct {
		name string
		view burnView
	}{
		{"subagents.guard false", burnVerdict(guardOff, object{}, usage, now)},
		{"noctis off", burnVerdict(burnConfig(nil), object{"disabledUntil": float64(now + 3600)}, usage, now)},
	}
	for _, verdict := range verdicts {
		if !verdict.view.early || verdict.view.stop {
			t.Fatalf("%s: nothing refuses subagents, yet the verdict is %+v", verdict.name, verdict.view)
		}
		if text := burnStatusText(verdict.view); !strings.Contains(text, "weekly threshold") || strings.Contains(text, "subagents") {
			t.Fatalf("%s: the status line says %q", verdict.name, text)
		}
		if notices, _ := planNotices(burnConfig(nil), object{"notified": object{}}, usage, &decision{burn: verdict.view}, "b", now, true); len(notices) != 1 || !strings.Contains(notices[0], "burning fast") || strings.Contains(notices[0], "refuses") {
			t.Fatalf("%s: the notice says %v", verdict.name, notices)
		}
	}
	if view := burnVerdict(burnConfig(nil), object{"disabledUntil": float64(now - 1)}, usage, now); !view.stop {
		t.Fatalf("once noctis is back on, the burn stops new subagents again, yet the verdict is %+v", view)
	}
}

func TestWhileNoctisIsOffTheBurnIsToldWithoutARefusal(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	weekReadings(t, now, float64(now+5*86400), 3, 30, 2, 33, 1, 36, 0, 39)
	updateState(func(state object) { state["disabledUntil"] = float64(now + 3600) })

	result := decide(burnConfig(nil), readState(), object{"session_id": "off", "cwd": t.TempDir()}, now, decideOptions{noProbe: true})

	if result.burn.stop || !strings.Contains(result.notice, "burning fast") || strings.Contains(result.notice, "refuses") {
		t.Fatalf("while noctis is off the verdict is %+v and the notice %q", result.burn, result.notice)
	}
	if notified := getMap(readState(), "notified"); notified[burnMark("stop", result.burn)] != nil || notified[burnMark("early", result.burn)] == nil {
		t.Fatalf("while noctis is off the marks told are %v; want the warning's and not the stop's", notified)
	}
}

func TestTheStatusLineClaimsNoRefusalOfSubagentsWhileNoctisIsOff(t *testing.T) {
	now := nowSec()
	weekReset := float64(now + 5*86400)
	statusline := func(paused bool) string {
		t.Helper()
		box := newLeanBox(t)
		sandboxFilesIn(t, filepath.Join(box.account, pluginName))
		weekReadings(t, now, weekReset, 3, 30, 2, 33, 1, 36, 0, 39)
		if paused {
			updateState(func(state object) { state["disabledUntil"] = float64(now + 3600) })
		}
		payload, _ := json.Marshal(object{"session_id": "burn", "model": object{"id": "claude-opus-5-5", "display_name": "Opus 5.5"}, "cwd": forwardSlashes(box.home),
			"rate_limits": object{"five_hour": object{"used_percentage": 10.0, "resets_at": float64(now + 3600)}, "seven_day": object{"used_percentage": 39.0, "resets_at": weekReset}}})
		run := box.run(t, string(payload), "statusline")
		if run.code != 0 {
			t.Fatalf("the status line failed:\n%s", run)
		}
		return run.stdout
	}

	if line := statusline(false); !strings.Contains(line, "no new subagents") {
		t.Fatalf("a week burning 3 points an hour with 5 days to its reset shows %q", line)
	}
	if line := statusline(true); !strings.Contains(line, "⏸") || !strings.Contains(line, "⌛ weekly threshold") || strings.Contains(line, "subagents") {
		t.Fatalf("while noctis is off the status line shows %q", line)
	}
}

func TestObserveModeJournalsTheBurnStopWithoutClaimingIt(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 10, 30)
	if err := os.Remove(files.usage); err != nil {
		t.Fatal(err)
	}
	now := nowSec()
	weekReadings(t, now, float64(now+5*86400), 3, 30, 2, 33, 1, 36, 0, 39)
	defer func(previous bool) { observing = previous }(observing)
	observing = true

	if output := spawnAgent(t, cfg, "burn-observe", project, "general-purpose"); permissionOf(output) == "deny" || getString(output, "systemMessage") != "" {
		t.Fatalf("observe mode refused a subagent in a week burning too fast: %v", output)
	}
	if rows := journalWith("would-deny-subagent-spawn"); len(rows) != 1 || !strings.Contains(rows[0], "weekly burn 3 points/h") {
		t.Fatalf("observe mode journaled %v; want the refusal it would make", rows)
	}
	usage := currentUsage(now)
	view := burnVerdict(cfg, readState(), usage, now)
	if text := burnStatusText(view); !strings.Contains(text, "weekly threshold") || strings.Contains(text, "subagents") {
		t.Fatalf("in observe mode the status line says %q", text)
	}
	if notices, marks := planNotices(cfg, object{"notified": object{}}, usage, &decision{burn: view}, "b", now, true); len(notices) != 1 || !strings.Contains(notices[0], "burning fast") || strings.Contains(notices[0], "refuses") || len(marks) != 1 {
		t.Fatalf("in observe mode the notice says %v and marks %v", notices, marks)
	}
}

func TestTheBurnNoticeComesOncePerWeekAndLevel(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	weekReset := float64(now + 3*86400)
	cfg := burnConfig(nil)
	weekReadings(t, now, weekReset, 3, 40, 0, 43)
	state := object{"notified": object{}}

	result := decision{burn: weeklyBurn(cfg, currentUsage(now), now)}
	notices, marks := planNotices(cfg, state, currentUsage(now), &result, "b1", now, true)
	if len(notices) != 1 || !strings.Contains(notices[0], "burning fast") || len(marks) != 1 {
		t.Fatalf("a week that runs out early told %v and marked %v", notices, marks)
	}
	for _, mark := range marks {
		getMap(state, "notified")[mark] = float64(now)
	}
	if again, _ := planNotices(cfg, state, currentUsage(now), &result, "b2", now, true); len(again) != 0 {
		t.Fatalf("the warning came again in another session of the same week: %v", again)
	}

	later := now + 600
	weekReadings(t, later, weekReset, 0, 52)
	result = decision{burn: weeklyBurn(cfg, currentUsage(later), later)}
	notices, marks = planNotices(cfg, state, currentUsage(later), &result, "b1", later, true)
	if !result.burn.stop || len(notices) != 1 || !strings.Contains(notices[0], "refuses new subagents") || len(marks) != 2 {
		t.Fatalf("a burn that grew to the stop level told %v and marked %v (verdict %+v)", notices, marks, result.burn)
	}
}

func TestATypedPromptShowsTheBurnStopOnce(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 10, 30)
	if err := os.Remove(files.usage); err != nil {
		t.Fatal(err)
	}
	now := nowSec()
	weekReadings(t, now, float64(now+5*86400), 3, 30, 2, 33, 1, 36, 0, 39)

	first := hookOutput(t, onUserPromptSubmit, promptInput("burn", project, "go on with the parser"), cfg)
	if message := getString(first, "systemMessage"); !strings.Contains(message, "refuses new subagents") {
		t.Fatalf("the prompt in a week burning too fast showed %v", first)
	}
	if second := hookOutput(t, onUserPromptSubmit, promptInput("burn", project, "and the lexer"), cfg); strings.Contains(getString(second, "systemMessage"), "burning") {
		t.Fatalf("the burn notice came twice: %v", second)
	}
}

func TestThePaceHistoryKeepsADayAndTheReadingBeforeIt(t *testing.T) {
	now := nowSec()
	reset := float64(now + 4*86400)
	history := object{}
	appendPace(history, 5, reset-7*86400, now-40*3600)
	appendPace(history, 10, reset, now-30*3600)
	appendPace(history, 12, reset, now-26*3600)
	appendPace(history, 20, reset, now-3*3600)
	appendPace(history, 20, reset, now-2*3600)
	appendPace(history, 25, reset, now)

	kept := getList(history, paceHistoryKey)
	got := []float64{}
	for _, raw := range kept {
		got = append(got, numberOr(toObject(raw), "used", -1))
	}
	if len(got) != 3 || got[0] != 12 || got[1] != 20 || got[2] != 25 {
		t.Fatalf("the pace history kept %v; want the last reading before the day (12), then 20 once and 25", got)
	}
}
