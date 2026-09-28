package main

import "testing"

// u9SkewedReading leaves a usage endpoint reading fetched at fetchedAt that measured the local
// clock 10 minutes behind the server's, and whose fetches have failed since.
func u9SkewedReading(fetchedAt int64, reset, weekReset float64) {
	mustWriteJSON(files.fable, object{"fetchedAt": float64(fetchedAt), "clockOffset": float64(600),
		"five_hour": object{"used": float64(40), "resetsAt": reset}, "seven_day": object{"used": float64(20), "resetsAt": weekReset},
		"error": "http-503", "backoffUntil": float64(nowSec() + 120)})
}

func TestAClockOffsetMeasuredLongAgoNoLongerMovesTheResetTimes(t *testing.T) {
	sandboxFiles(t)
	cfg := releaseConfig()
	now := nowSec()
	reset, weekReset := float64(now+3600), float64(now+3*86400)
	statusReading(now, 93, reset, 20, weekReset)

	u9SkewedReading(now-60, reset, weekReset)
	if usage := currentUsage(now); usage.fiveHour == nil || usage.fiveHour.resetsAt != reset-600 || usage.clockOffset != 600 {
		t.Fatalf("a minute after the fetch that measured the clock 10 minutes behind, the reset is not moved 10 minutes earlier: %+v (offset %v)", usage.fiveHour, usage.clockOffset)
	}

	for _, fetchedAt := range []int64{now - 2*3600, now + 2*3600} {
		u9SkewedReading(fetchedAt, reset, weekReset)
		usage := currentUsage(now)
		if usage.fiveHour == nil || usage.fiveHour.resetsAt != reset || usage.clockOffset != 0 {
			t.Fatalf("the clock offset was measured by a fetch at %d, %d s from now, and still moves the reset: %+v (offset %v)", fetchedAt, fetchedAt-now, usage.fiveHour, usage.clockOffset)
		}
		if plan := evaluate(cfg, usage, "", 0, false).wait; plan == nil || plan.until != reset {
			t.Fatalf("at 93%% the pause should last until the reset at %v, not until an old clock offset says: %+v", reset, plan)
		}
	}
}
