package main

import (
	"strings"
	"testing"
	"time"
)

func journalLine(at float64, sid, action, reason string, facts object) string {
	entry := object{"at": at, "sid": sid, "event": "Stop", "action": action, "reason": reason}
	for key, value := range facts {
		entry[key] = value
	}
	return string(marshalCompact(entry))
}

func statsJournal(t *testing.T) (noon float64) {
	t.Helper()
	sandboxFiles(t)
	year, month, day := time.Unix(nowSec(), 0).AddDate(0, 0, -2).Date()
	noon = float64(time.Date(year, month, day, 12, 0, 0, 0, time.Local).Unix())
	writeLines(t, files.decisions+".1",
		journalLine(noon-6*86400, "s1", "pause", "5h 93%", object{"window": "five_hour", "hit": "threshold", "resumeAt": noon - 6*86400 + 3600}),
		journalLine(noon-86400, "s1", "pause", "daily budget", object{"window": "seven_day", "hit": "budget", "resumeAt": noon - 86400 + 7200}))
	writeLines(t, files.decisions,
		journalLine(noon+10, "s1", "pause", "5h 93%", object{"window": "five_hour", "hit": "threshold", "resumeAt": noon + 10 + 3600}),
		journalLine(noon+20, "s1", "pause", "Fable 97%", object{"window": "fable", "hit": "threshold", "resumeAt": noon + 20 + 1800}),
		journalLine(noon+30, "s1", "pause", "burst projection", object{"window": "five_hour", "hit": "burst", "resumeAt": noon + 30 + 600}),
		journalLine(noon+35, "s1", "pause", "usage limit reached", object{"window": "seven_day", "hit": "ceiling", "resumeAt": noon + 35 + 900}),
		journalLine(noon+38, "s1", "pause", "threshold", object{"window": "unknown", "hit": "threshold", "resumeAt": noon + 28}),
		journalLine(noon+40, "s1", "would-pause", "5h 93%", object{"observe": true}),
		journalLine(noon+50, "s1", "reset-confirmed", "5h window reset", object{"ahead": float64(80)}),
		journalLine(noon+60, "s1", "reset-confirmed", "5h window reset", object{"ahead": float64(40)}),
		journalLine(noon+70, "s1", "early-reset", "5h window cleared", nil),
		journalLine(noon+80, "s1", "data-back", "usage data is back", nil),
		journalLine(noon+90, "s1", "schedule-resume", "5h", object{"resumeAt": noon + 3690}),
		journalLine(noon+100, "s2", "schedule-resume", "weekly", object{"resumeAt": noon + 7300}),
		journalLine(noon+110, "s1", "verify-queue", "passed", object{"command": "go test ./pkg", "tier": "each", "seconds": float64(12)}),
		journalLine(noon+120, "s1", "verify-queue", "passed", object{"command": "make check", "seconds": float64(30)}),
		journalLine(noon+130, "s1", "verify-queue", "skipped: the working tree is as it was when it last passed", object{"command": "go test ./pkg", "tier": "each", "skipped": true}),
		journalLine(noon+140, "s1", "verify-queue", "cut short: timed out after 600 s", object{"command": "make check", "seconds": float64(600)}),
		journalLine(noon+150, "s1", "verify-queue", "exited with code 1; the fix goes once to claude-opus-5-5", object{"command": "make check", "seconds": float64(20), "failures": float64(2), "escalated": true, "escalateTo": "claude-opus-5-5"}),
		journalLine(noon+160, "s2", "verify-queue", "exited with code 1", object{"command": "make check", "seconds": float64(5), "failures": float64(1)}),
		journalLine(noon+170, "s2", "hold-queue", "exited with code 1", object{"command": "make check", "seconds": float64(8), "failures": float64(3)}),
		journalLine(noon+180, "s1", "continue-queue", "3 open, working tree changed", object{"forced": float64(1), "treeMoved": true}),
		journalLine(noon+190, "s1", "continue-queue", "3 open", object{"forced": float64(2), "escalated": true, "escalateTo": "claude-opus-5-5"}),
		journalLine(noon+200, "s1", "continue-queue", "3 open", object{"forced": float64(3), "setAside": true}),
		journalLine(noon+210, "s1", "continue-queue", "2 open", object{"forced": float64(4)}),
		journalLine(noon+220, "s1", "continue-queue", "2 open", object{"forced": float64(5), "escalated": false, "escalateTo": "claude-opus-5-5"}),
		journalLine(noon+230, "s1", "allow-stop", "queue not progressing", object{"open": float64(2)}),
		journalLine(noon+240, "s1", "allow-stop", "daily continue limit", object{"open": float64(2), "today": float64(600)}),
		journalLine(noon+250, "s1", "allow-stop", "daily continue limit", object{"open": float64(2), "today": float64(600)}),
		journalLine(noon+260, "s2", "allow-stop", "daily continue limit", object{"open": float64(4), "today": float64(600)}),
		journalLine(noon+270, "s2", "allow-stop", "queue finished", nil),
		"not json",
		"",
		journalLine(noon+86400, "s1", "allow-stop", "daily continue limit", object{"open": float64(2), "today": float64(600)}))
	return noon
}

func whyStatsOutput(t *testing.T, argv ...string) string {
	t.Helper()
	defer func(previous parsedArgs) { args = previous }(args)
	args = parseArgs(append([]string{"why", "--stats"}, argv...))
	return strings.TrimSpace(capturedStdout(t, runWhy))
}

func inLocale(t *testing.T, code string) {
	t.Helper()
	previous := locale
	t.Cleanup(func() { locale = previous })
	locale = code
}

func TestWhyStatsSumsUpEachKindOfDecisionInTheWindow(t *testing.T) {
	statsJournal(t)
	before := float64(nowSec())
	report := object{}
	if err := jsonUnmarshalObject([]byte(whyStatsOutput(t, "--json")), &report); err != nil {
		t.Fatalf("why --stats --json did not print one JSON object: %v", err)
	}
	to := numberOr(report, "to", 0)
	if to < before || to > before+2 || numberOr(report, "from", 0) != to-7*86400 {
		t.Fatalf("the report spans %v to %v; want the last seven days up to now (%v)", report["from"], report["to"], before)
	}
	delete(report, "from")
	delete(report, "to")
	want := object{"days": float64(7), "decisions": float64(30), "observed": float64(1),
		"pauses": object{"count": float64(6), "plannedSeconds": float64(14100),
			"windows": object{"five_hour": float64(2), "seven_day": float64(2), "fable": float64(1), "unknown": float64(1)},
			"causes":  object{"burst": float64(1), "budget": float64(1), "ceiling": float64(1)}},
		"resumes":   object{"resetConfirmed": float64(2), "secondsEarlier": float64(120), "earlyReset": float64(1), "dataBack": float64(1)},
		"limitHits": float64(2),
		"checks": object{"runs": float64(6), "seconds": float64(675), "passed": float64(2), "failed": float64(3), "cutShort": float64(1),
			"skipped": float64(1), "perItem": float64(1), "handedUp": float64(1), "held": float64(1)},
		"continues": object{"count": float64(5), "treeMoved": float64(1), "handedUp": float64(1), "setAside": float64(1), "letGo": float64(1), "dayLimits": float64(3)},
	}
	if got, wanted := string(marshalCompact(report)), string(marshalCompact(want)); got != wanted {
		t.Fatalf("why --stats --json counted\n%s\nwant\n%s", got, wanted)
	}
}

func TestWhyStatsTellsTheCountsInWords(t *testing.T) {
	statsJournal(t)
	inLocale(t, "en")
	got := strings.Split(whyStatsOutput(t), "\n")
	if !strings.HasPrefix(got[0], "Decisions from ") || !strings.HasSuffix(got[0], ": 30") {
		t.Fatalf("why --stats opened with %q", got[0])
	}
	got = got[1:]
	want := []string{
		"Observe-mode entries left out (what noctis would have done): 1",
		"Pauses: 6",
		"   planned wait: 3h 55m · 5h: 2 · weekly: 2 · Fable: 1 · unknown: 1 · burst projection: 1 · daily budget: 1 · usage limit reached (paid credits refused): 1",
		"Resumed before the planned time: 4",
		"   after a confirmed reset: 2, 2m saved · window cleared before its reset time: 1 · on fresh usage data: 1",
		"Limit hits that stopped Claude Code: 2",
		"Queue checks run: 6",
		"   11m in all · passed: 2 · failed: 3 · cut short: 1 · skipped on an unchanged tree: 1 · per-item command: 1 · handed to a stronger model: 1 · held the queue: 1",
		"Queue continuations: 5",
		"   on a changed working tree: 1 · handed to a stronger model: 1 · items set aside: 1 · stuck queues let go: 1 · daily limit reached: 3",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("why --stats printed\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	inLocale(t, "tr")
	turkish := whyStatsOutput(t)
	for _, line := range []string{" arasındaki kararlar: 30", "Duraklamalar: 6", "   doğrulanmış sıfırlamadan sonra: 2, 2dk kazanıldı"} {
		if !strings.Contains(turkish, line) {
			t.Fatalf("the Turkish report lacks %q:\n%s", line, turkish)
		}
	}
}

func TestWhyStatsReachesBackAsManyDaysAsAsked(t *testing.T) {
	noon := statsJournal(t)
	for _, days := range [][]string{nil, {"--days", "0"}, {"--days", "-3"}, {"--days", "soon"}, {"--days", "inf"}} {
		report := object{}
		if jsonUnmarshalObject([]byte(whyStatsOutput(t, append(days, "--json")...)), &report) != nil || numberOr(report, "days", 0) != 7 || numberOr(report, "decisions", 0) != 30 {
			t.Fatalf("why --stats %v did not fall back to the last seven days: %v", days, report)
		}
	}
	report := object{}
	if jsonUnmarshalObject([]byte(whyStatsOutput(t, "--days", "10", "--json")), &report) != nil || numberOr(report, "decisions", 0) != 31 || numberOr(report, "from", 0) != noon-6*86400 {
		t.Fatalf("why --stats --days 10 did not reach back to the oldest decision in the rotated journal: %v", report)
	}
	if jsonUnmarshalObject([]byte(whyStatsOutput(t, "--days", "1e9", "--json")), &report) != nil || numberOr(report, "days", 0) != lookbackMaxDays {
		t.Fatalf("why --stats --days 1e9 was not capped at %d days: %v", lookbackMaxDays, report["days"])
	}
}

func TestWhyStatsOnAnEmptyJournalSaysNothingWasDecidedYet(t *testing.T) {
	sandboxFiles(t)
	inLocale(t, "en")
	if got := whyStatsOutput(t); got != T("why.empty") {
		t.Fatalf("why --stats on an empty journal printed %q", got)
	}
	report := object{}
	if jsonUnmarshalObject([]byte(whyStatsOutput(t, "--json")), &report) != nil || numberOr(report, "decisions", -1) != 0 || numberOr(getMap(report, "checks"), "runs", -1) != 0 {
		t.Fatalf("why --stats --json on an empty journal printed %v", report)
	}
	writeLines(t, files.decisions, journalLine(float64(nowSec()), "s1", "would-pause", "5h 93%", object{"observe": true}))
	if got := whyStatsOutput(t); !strings.Contains(got, "Observe-mode entries left out (what noctis would have done): 1") || !strings.Contains(got, "Pauses: 0") {
		t.Fatalf("why --stats on a journal of observe-mode entries only printed\n%s", got)
	}
}
