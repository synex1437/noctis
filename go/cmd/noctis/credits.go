package main

import "math"

func creditsCfg(cfg object) object {
	return section(cfg, "credits")
}

func paidCreditsAllowed(cfg object) bool {
	return getBool(creditsCfg(cfg), "allowPaid", false)
}

func creditCeiling(cfg object) float64 {
	value := numberOr(creditsCfg(cfg), "ceiling", creditCeilingDefault)
	if value < 1 || value > 100 {
		return creditCeilingDefault
	}
	return value
}

func fanOutHeadroom(cfg object) float64 {
	return math.Max(0, numberOr(creditsCfg(cfg), "fanOutHeadroom", fanOutHeadroomDefault))
}

func ceilingHit(cfg object, usage usageView) *waitPlan {
	if paidCreditsAllowed(cfg) {
		return nil
	}
	ceiling := creditCeiling(cfg)
	var worst *waitPlan
	for _, key := range []string{"five_hour", "seven_day"} {
		win := usage.byKey(key)
		if win == nil || win.used < ceiling {
			continue
		}
		if worst == nil || win.resetsAt > worst.until {
			worst = &waitPlan{window: key, label: windowLabel(key), used: win.used, threshold: ceiling, until: win.resetsAt, hit: "ceiling"}
		}
	}
	return worst
}

func guardPaused(cfg, state object, now int64) bool {
	if numberOr(state, "disabledUntil", 0) <= float64(now) {
		return false
	}
	if ceilingHit(cfg, currentUsage(now)) != nil {
		warn("the guard is paused, but usage is at the ceiling: refusing to spend paid credits")
		return false
	}
	return true
}

func headroomLeft(cfg object, usage usageView) (float64, string) {
	room, label := math.Inf(1), ""
	thresholds := section(cfg, "thresholds")
	consider := func(win *window, limit float64, name string) {
		if win == nil {
			return
		}
		if left := limit - win.used; left < room {
			room, label = left, name
		}
	}
	for _, spec := range []struct {
		key, threshold string
	}{{"five_hour", "session5h"}, {"seven_day", "weeklyAll"}} {
		limit := creditCeiling(cfg)
		if validThreshold(thresholds[spec.threshold]) {
			limit = math.Min(limit, thresholdOf(cfg, spec.threshold))
		}
		consider(usage.byKey(spec.key), limit, windowLabel(spec.key))
	}
	if value := scopedThresholdValue(cfg); validThreshold(value) && usage.fable != nil {
		consider(usage.fable, scopedThreshold(cfg), scopedLabel(cfg))
	}
	return room, label
}

func unguardedWindows(cfg object) []string {
	thresholds := section(cfg, "thresholds")
	unguarded := []string{}
	for _, spec := range []struct {
		key   string
		value any
	}{
		{"session5h", thresholds["session5h"]},
		{"weeklyAll", thresholds["weeklyAll"]},
		{"weeklyFable", scopedThresholdValue(cfg)},
	} {
		if !validThreshold(spec.value) {
			unguarded = append(unguarded, spec.key)
		}
	}
	return unguarded
}

func creditsText(cfg object) string {
	if paidCreditsAllowed(cfg) {
		return T("credits.allowed")
	}
	return T("credits.refused", formatNumber(creditCeiling(cfg)), formatNumber(fanOutHeadroom(cfg)))
}
