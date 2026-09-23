package main

import (
	"os"
	"strings"
	"testing"
)

func apiReading(at int64, five, fiveReset, week, weekReset float64) {
	mustWriteJSON(files.fable, object{
		"fetchedAt": float64(at),
		"five_hour": object{"used": five, "resetsAt": fiveReset},
		"seven_day": object{"used": week, "resetsAt": weekReset},
	})
}

func TestAStatusLineWithoutLimitsLeavesNewerApiDataInCharge(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReading(now-1800, 70, reset, 20, weekReset)
	apiReading(now-600, 95, reset, 21, weekReset)
	recordStatusline(object{"session_id": "fresh", "model": object{"id": "claude-opus-5-5"}}, now, true)
	usageFile := readJSON(files.usage)
	if got := numberOr(usageFile, "updatedAt", 0); got != float64(now-1800) {
		t.Fatalf("a render without rate_limits moved usage.json updatedAt to %v, want the last reading at %v", got, now-1800)
	}
	if getMap(getMap(usageFile, "sessions"), "fresh") == nil {
		t.Fatal("the render's session was not recorded")
	}
	usage := currentUsage(now)
	if usage.fiveHour == nil || usage.fiveHour.used != 95 || usage.fiveHour.staleness != 600 {
		t.Fatalf("the five-hour window reads %+v, want the API's 95 %% with staleness 600", usage.fiveHour)
	}
}

func TestAStatusLineWithAnUnusableWindowKeepsTheOldStampAndWarnsOnce(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReading(now-1800, 70, reset, 20, weekReset)
	apiReading(now-600, 95, reset, 21, weekReset)
	for tick := int64(0); tick < 3; tick++ {
		recordStatusline(object{"session_id": "ms", "rate_limits": object{"five_hour": object{"used_percentage": float64(40), "resets_at": reset * 1000}}}, now+tick, true)
	}
	if got := numberOr(readJSON(files.usage), "updatedAt", 0); got != float64(now-1800) {
		t.Fatalf("renders whose only window was unusable moved usage.json updatedAt to %v, want %v", got, now-1800)
	}
	if usage := currentUsage(now + 2); usage.fiveHour == nil || usage.fiveHour.used != 95 {
		t.Fatalf("the five-hour window reads %+v, want the API's 95 %%", usage.fiveHour)
	}
	content, _ := os.ReadFile(files.errors)
	if count := strings.Count(string(content), "five_hour"); count != 1 {
		t.Fatalf("three renders with the same unusable window wrote %d warnings about it, want 1:\n%s", count, content)
	}
}

func TestAStatusLineWithoutLimitsCannotFakeAnEarlyReset(t *testing.T) {
	sandboxFiles(t)
	cfg := releaseConfig()
	now := nowSec()
	reset, weekReset := float64(now+3*3600), float64(now+3*86400)
	statusReadingFrom("A", now-7200, 50, reset, 20, weekReset)
	apiReading(now-90, 93, reset, 21, weekReset)
	record := pauseRecord("threshold", 93, 92, float64(now-80), reset)
	recordStatusline(object{"session_id": "B"}, now, true)
	if got := inHookRelease(cfg, record); got != "" {
		t.Fatalf("a new session's first render, which carried no usage, made a two-hour-old 50 %% reading look fresh and ended the pause early (%s)", got)
	}
}

func TestABareStatusLineOnAFreshAccountIsNoUsageData(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	recordStatusline(object{"session_id": "new"}, now, true)
	if currentUsage(now).hasAny {
		t.Fatal("a render without rate_limits on an account with no usage data made the guard believe it has data")
	}
}
