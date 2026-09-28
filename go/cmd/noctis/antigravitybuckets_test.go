package main

import (
	"math"
	"testing"
)

func u4QuotaWindow(t *testing.T, limits object, key string) (float64, float64) {
	t.Helper()
	win := getMap(limits, key)
	if win == nil {
		t.Fatalf("no %s window in %v", key, limits)
	}
	return math.Round(numberOr(win, "used_percentage", -1)), numberOr(win, "resets_at", 0)
}

func TestAWeeklyAntigravityQuotaNearItsResetStaysTheWeeklyWindow(t *testing.T) {
	now := nowSec()
	limits := antigravityRateLimits(object{"model": object{"id": "U4model 3 (High)"}, "quota": object{
		"u4model-5h":     object{"remaining_fraction": 0.93, "reset_in_seconds": float64(1500)},
		"u4model-weekly": object{"remaining_fraction": 0.1, "reset_in_seconds": float64(3 * 3600)},
	}}, now)
	if used, resetsAt := u4QuotaWindow(t, limits, "five_hour"); used != 7 || resetsAt != float64(now+1500) {
		t.Fatalf("the 5-hour bucket at 7%% was replaced by the weekly one three hours before its reset: %v%% until %v", used, resetsAt)
	}
	if used, _ := u4QuotaWindow(t, limits, "seven_day"); used != 90 {
		t.Fatalf("the weekly bucket at 90%% three hours before its reset is not the weekly window: %v%%", used)
	}
}

func TestAnAntigravityQuotaPlacedByItsResetNeverReplacesOneItsNamePlaced(t *testing.T) {
	now := nowSec()
	limits := antigravityRateLimits(object{"model": object{"id": "U4other 4"}, "quota": object{
		"small-quota":  object{"remaining_fraction": 0.05, "reset_in_seconds": float64(3 * 3600)},
		"u4model-5h":   object{"remaining_fraction": 0.93, "reset_in_seconds": float64(1500)},
		"large-weekly": object{"remaining_fraction": 0.6, "reset_in_seconds": float64(4 * 86400)},
		"large-quota":  object{"remaining_fraction": 0.2, "reset_in_seconds": float64(2 * 86400)},
	}}, now)
	if used, _ := u4QuotaWindow(t, limits, "five_hour"); used != 7 {
		t.Fatalf("a bucket named for the 5-hour window lost it to one that only resets within 5 hours: %v%%", used)
	}
	if used, _ := u4QuotaWindow(t, limits, "seven_day"); used != 40 {
		t.Fatalf("a bucket named for the weekly window lost it to one that only resets later: %v%%", used)
	}

	unnamed := antigravityRateLimits(object{"model": object{"id": "U4other 4"}, "quota": object{
		"small-quota": object{"remaining_fraction": 0.05, "reset_in_seconds": float64(3 * 3600)},
		"large-quota": object{"remaining_fraction": 0.2, "reset_in_seconds": float64(2 * 86400)},
	}}, now)
	if used, _ := u4QuotaWindow(t, unnamed, "five_hour"); used != 95 {
		t.Fatalf("with no bucket named for a window, the one resetting within 5 hours is not the 5-hour window: %v%%", used)
	}
	if used, _ := u4QuotaWindow(t, unnamed, "seven_day"); used != 80 {
		t.Fatalf("with no bucket named for a window, the one resetting in two days is not the weekly window: %v%%", used)
	}
}
