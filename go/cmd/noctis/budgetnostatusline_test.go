package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// e8EndpointOnlySandbox is a daily budget of 10 points whose usage comes from the usage endpoint
// alone (fable.json), the way a session on a host without a status line sees it: nothing writes
// usage.json.
func e8EndpointOnlySandbox(t *testing.T, hardStop bool) object {
	t.Helper()
	cfg, _, _ := limitSandbox(t, object{"budget": object{"dailyWeeklyPercent": float64(10), "hardStop": hardStop}}, 20, 10)
	if err := os.Remove(files.usage); err != nil {
		t.Fatalf("the sandbox kept a status-line reading: %v", err)
	}
	return cfg
}

func TestTheDailyBudgetCountsTheDayWhenNoStatusLineRuns(t *testing.T) {
	for _, hardStop := range []bool{true, false} {
		t.Run(fmt.Sprintf("hardStop=%t", hardStop), func(t *testing.T) {
			cfg := e8EndpointOnlySandbox(t, hardStop)
			now := nowSec()
			weekReset := float64(now + 3*86400)
			e8EndpointReading := func(weekly float64) {
				mustWriteJSON(files.fable, object{"fetchedAt": float64(now),
					"five_hour": object{"used": float64(20), "resetsAt": float64(now + 3600)},
					"seven_day": object{"used": weekly, "resetsAt": weekReset}})
			}
			input := object{"session_id": "e8-endpoint-only", "cwd": t.TempDir()}
			spent := T("notice.budget", "30", "10")

			e8EndpointReading(10)
			first := decide(cfg, readState(), input, now, decideOptions{})
			if first.wait != nil || strings.Contains(first.notice, spent) {
				t.Fatalf("the day's first reading, with nothing used today, already stopped or warned: wait %+v, notice %q", first.wait, first.notice)
			}
			if day := getMap(readState(), "budgetDay"); getString(day, "day") != localDay(now) || numberOr(day, "startUsed", -1) != 10 || numberOr(day, "weekResetsAt", -1) != weekReset {
				t.Fatalf("the day did not start from the endpoint's weekly reading of 10%%: budgetDay %v", day)
			}

			e8EndpointReading(40)
			second := decide(cfg, readState(), input, now, decideOptions{})
			if hardStop {
				if second.wait == nil || second.wait.hit != "budget" {
					t.Fatalf("30 points of the weekly quota used today, 10 allowed, and hardStop did not pause: %+v", second.wait)
				}
				return
			}
			if !strings.Contains(second.notice, spent) {
				t.Fatalf("30 points of the weekly quota used today, 10 allowed, and nothing said so: notice %q", second.notice)
			}
		})
	}
}
