package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func dailyLimitSandbox(t *testing.T) (object, string) {
	t.Helper()
	cfg, project := queueTrustSandbox(t, false)
	t.Setenv("CLAUDE_CODE_STOP_HOOK_BLOCK_CAP", "")
	trustQueueFile(writeQueueFile(t, project, "# release\n- [ ] migrate the users table\n- [ ] write the release notes\n"), true)
	return cfg, project
}

func stopsOf(t *testing.T, cfg object, sid, project string, stops int) (int, []string) {
	t.Helper()
	blocked, notices := 0, []string{}
	for stop := 0; stop < stops; stop++ {
		output := stopHookOutput(t, stopInput(sid, project), cfg)
		if getString(output, "decision") == "block" {
			blocked++
			continue
		}
		if message := getString(output, "systemMessage"); message != "" {
			notices = append(notices, message)
		}
	}
	return blocked, notices
}

func journaledDailyLimits(t *testing.T, sid string) int {
	t.Helper()
	content, _ := os.ReadFile(files.decisions)
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		var entry object
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if getString(entry, "sid") == sid && getString(entry, "action") == "allow-stop" && getString(entry, "reason") == "daily continue limit" {
			count++
		}
	}
	return count
}

func TestAQueueThatKeepsGivingUpIsContinuedAtMostSixHundredTimesADay(t *testing.T) {
	cfg, project := dailyLimitSandbox(t)
	blocked, notices := stopsOf(t, cfg, "day1", project, 700)
	cycles := numberOr(getMap(getMap(readState(), "stopGuard"), "day1"), "cycles", 0)
	if blocked != 600 {
		t.Fatalf("a queue that gave up after every 200 continuations (%s give-ups) was continued %d times in one day by the Stop hook; the daily limit is 600", formatNumber(cycles), blocked)
	}
	if cycles != 2 {
		t.Fatalf("the per-session limit no longer gives up after 200 continuations: %s give-ups in 600 continuations", formatNumber(cycles))
	}
	want := T("queue.dayLimitMessage", "600", 2)
	told := 0
	for _, notice := range notices {
		if notice == want {
			told++
		}
	}
	if told != 1 {
		t.Fatalf("the daily limit was announced %d times, want once as %q; notices: %q", told, want, notices)
	}
	if journaled := journaledDailyLimits(t, "day1"); journaled != 700-600-2 {
		t.Fatalf("the stops the daily limit let through were journaled %d times, want %d", journaled, 700-600-2)
	}
}

func TestTheDailyContinueLimitSurvivesAGiveUpAndStartsAgainAtMidnight(t *testing.T) {
	cfg, project := dailyLimitSandbox(t)
	section(cfg, "queue")["maxContinuesPerDay"] = float64(3)
	section(cfg, "queue")["maxContinuesPerSession"] = float64(2)
	blocked, notices := stopsOf(t, cfg, "day2", project, 6)
	if blocked != 3 {
		t.Fatalf("with queue.maxContinuesPerDay 3 and queue.maxContinuesPerSession 2 the Stop hook continued %d of 6 stops; the give-up after the second continuation must not start the daily count again", blocked)
	}
	want := T("queue.dayLimitMessage", "3", 2)
	if len(notices) != 2 || notices[1] != want {
		t.Fatalf("after the give-up notice the daily limit must be announced once as %q, got %q", want, notices)
	}
	if other, _ := stopsOf(t, cfg, "day2-other", project, 1); other != 1 {
		t.Fatal("another session was held to the daily limit of day2")
	}
	defer func(previous int64) { timeOffset = previous }(timeOffset)
	timeOffset += int64(nextLocalMidnight(nowSec())) - nowSec() + 60
	now := float64(nowSec())
	mustWriteJSON(files.usage, object{
		"updatedAt": now,
		"five_hour": object{"used": float64(20), "resetsAt": now + 3600},
		"seven_day": object{"used": float64(10), "resetsAt": now + 86400},
	})
	if next, _ := stopsOf(t, cfg, "day2", project, 1); next != 1 {
		t.Fatal("the day after it reached the daily limit the queue was not continued again")
	}
}

func TestADailyContinueLimitOfZeroLeavesOnlyTheSessionLimits(t *testing.T) {
	cfg, project := dailyLimitSandbox(t)
	section(cfg, "queue")["maxContinuesPerDay"] = float64(0)
	section(cfg, "queue")["maxContinuesPerSession"] = float64(2)
	if blocked, _ := stopsOf(t, cfg, "day3", project, 12); blocked != 8 {
		t.Fatalf("with queue.maxContinuesPerDay 0 the Stop hook continued %d of 12 stops; with only the session limit of 2 it gives up every third stop and continues 8", blocked)
	}
}
