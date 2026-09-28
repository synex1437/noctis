package main

import (
	"math"
	"testing"
)

func e2WeekReading(used, resetsAt float64) usageView {
	return usageView{hasAny: true, sevenDay: &window{used: used, resetsAt: resetsAt}}
}

func TestADailyBudgetKeepsItsStartWhenTheWeeklyResetMovesBySeconds(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	cfg := object{"thresholds": object{}, "budget": object{"dailyWeeklyPercent": float64(10)}}
	weekReset := float64(now + 3*86400)
	recordBudgetDay(e2WeekReading(30, weekReset), now)
	for _, shift := range []float64{1, -1, 60, -sameWindowSeconds, sameWindowSeconds} {
		reading := e2WeekReading(32, weekReset+shift)
		recordBudgetDay(reading, now)
		used, _, over := dailyBudgetStatus(cfg, readState(), reading, now)
		if over || math.Abs(used-2) > 0.001 {
			t.Fatalf("with the weekly reset read %v s from the day's first reading, today counts %v%% (over: %v); want the 2%% used since the day began", shift, used, over)
		}
	}

	fresh := e2WeekReading(3, weekReset+7*86400)
	recordBudgetDay(fresh, now)
	if used, _, _ := dailyBudgetStatus(cfg, readState(), fresh, now); math.Abs(used-3) > 0.001 {
		t.Fatalf("after the week reset today, today counts %v%%; want the 3%% of the new week", used)
	}
}

func TestTheUsageEndpointsReadingOfTheSameWeekDoesNotTripTheDailyBudget(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	cfg := object{"thresholds": object{}, "budget": object{"dailyWeeklyPercent": float64(10), "hardStop": true}}
	weekReset := float64(now + 3*86400)
	statusReading(now, 20, float64(now+7200), 30, weekReset)
	recordBudgetDay(currentUsage(now), now)

	mustWriteJSON(files.fable, object{"fetchedAt": float64(now), "seven_day": object{"used": float64(32), "resetsAt": weekReset + 60}})
	usage := currentUsage(now)
	if usage.sevenDay == nil || usage.sevenDay.resetsAt != weekReset+60 {
		t.Fatalf("the endpoint's higher reading of the week was not the one used: %+v", usage.sevenDay)
	}
	recordBudgetDay(usage, now)
	result := decision{}
	notices, _ := planNotices(cfg, readState(), usage, &result, "e2", now, true)
	if result.wait != nil || len(notices) != 0 {
		t.Fatalf("2%% used today under a 10%% daily budget paused the session (%+v) or told it the budget was used up (%v)", result.wait, notices)
	}
}
