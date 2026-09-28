package main

import (
	"regexp"
	"testing"
)

func u7FlatBucket(used float64) object {
	return object{"utilization": used, "resets_at": soonRFC3339()}
}

func TestTheOlderUsageAnswerReadsTheScopedModelsOwnWeeklyBucket(t *testing.T) {
	other := regexp.MustCompile("(?i)u7model")
	payload := object{
		"five_hour":         u7FlatBucket(12),
		"seven_day":         u7FlatBucket(30),
		"seven_day_fable":   u7FlatBucket(99),
		"seven_day_u7model": u7FlatBucket(10),
	}
	if win := getMap(parseUsagePayload(payload, other), "fable"); win == nil || numberOr(win, "used", -1) != 10 {
		t.Fatalf("with the scoped rule on another model, the scoped window is not that model's bucket at 10%%: %v", win)
	}
	if win := getMap(parseUsagePayload(payload, scopedPatternForTest()), "fable"); win == nil || numberOr(win, "used", -1) != 99 {
		t.Fatalf("with the scoped rule on Fable, the scoped window is not Fable's bucket at 99%%: %v", win)
	}

	delete(payload, "seven_day_u7model")
	if win := getMap(parseUsagePayload(payload, other), "fable"); win != nil {
		t.Fatalf("with the scoped rule on another model and no bucket of it in the answer, Fable's bucket became the scoped window: %v", win)
	}
	if !knownUsageShape(object{"seven_day_u7model": nil}) {
		t.Fatalf("an older answer that carries only one model's weekly bucket is taken for a shape noctis does not know")
	}
}
