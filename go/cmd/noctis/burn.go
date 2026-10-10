package main

import (
	"fmt"
	"math"
)

const (
	paceHistoryKey     = "seven_day_pace"
	paceKeepSeconds    = 24 * 3600
	paceSamplesKept    = 512
	paceWindowSlack    = 6 * 3600
	burnMinSpanSeconds = 1800
	burnMinRise        = 2.0
	burnLookbackHours  = 3.0
	burnStopShare      = 0.5
)

func appendPace(history object, used, resetsAt float64, now int64) {
	kept := []any{}
	cutoff := float64(now) - paceKeepSeconds
	baseline := -1
	list := getList(history, paceHistoryKey)
	for index, raw := range list {
		sample := toObject(raw)
		if sample == nil || math.Abs(numberOr(sample, "resetsAt", 0)-resetsAt) > paceWindowSlack {
			continue
		}
		if numberOr(sample, "at", 0) < cutoff {
			baseline = index
			continue
		}
		kept = append(kept, sample)
	}
	if baseline >= 0 {
		kept = append([]any{list[baseline]}, kept...)
	}
	if last := len(kept) - 1; last < 0 || numberOr(toObject(kept[last]), "used", math.NaN()) != used {
		kept = append(kept, object{"used": used, "resetsAt": resetsAt, "at": float64(now)})
	}
	if len(kept) > paceSamplesKept {
		kept = kept[len(kept)-paceSamplesKept:]
	}
	history[paceHistoryKey] = kept
}

type paceSample struct {
	used, at float64
}

func paceBase(resetsAt, start float64, current paceSample, sources ...object) (paceSample, bool) {
	before, after := paceSample{at: math.Inf(-1)}, paceSample{at: math.Inf(1)}
	for _, source := range sources {
		for _, raw := range getList(getMap(source, "history"), paceHistoryKey) {
			sample := toObject(raw)
			used, okUsed := getNumber(sample, "used")
			at, okAt := getNumber(sample, "at")
			reset, okReset := getNumber(sample, "resetsAt")
			if !okUsed || !okAt || !okReset || math.Abs(reset-resetsAt) > paceWindowSlack {
				continue
			}
			switch {
			case at <= start:
				if at > before.at {
					before = paceSample{used, at}
				}
			case at < after.at:
				after = paceSample{used, at}
			}
		}
	}
	if math.IsInf(before.at, 0) {
		return after, !math.IsInf(after.at, 0)
	}
	if math.IsInf(after.at, 0) {
		after = current
	}
	return paceSample{used: before.used + (after.used-before.used)*(start-before.at)/(after.at-before.at), at: start}, true
}

type burnView struct {
	known    bool
	perHour  float64
	span     float64
	used     float64
	target   float64
	runOut   float64
	resetIn  float64
	resetsAt float64
	early    bool
	stop     bool
}

func burnSettings(cfg object) (alarm bool, lookback, share float64) {
	burn := section(cfg, "burn")
	lookback = math.Min(24, math.Max(1, numberOr(burn, "lookbackHours", burnLookbackHours)))
	share = math.Min(1, math.Max(0, numberOr(burn, "stopSubagents", burnStopShare)))
	return getBool(burn, "alarm", true), lookback, share
}

func weeklyBurn(cfg object, usage usageView, now int64) burnView {
	win := usage.sevenDay
	alarm, lookback, share := burnSettings(cfg)
	if win == nil || !alarm {
		return burnView{}
	}
	target, guarded := stopPoint(cfg, "weeklyAll")
	if !guarded {
		target = 100
	}
	view := burnView{used: win.used, target: target, resetsAt: win.resetsAt, resetIn: win.resetsAt - float64(now)}
	start := float64(now) - lookback*3600
	base, found := paceBase(win.resetsAt+usage.clockOffset, start, paceSample{win.used, float64(now)}, readJSON(files.usage), readJSON(files.fable))
	if !found || view.resetIn <= 0 {
		return view
	}
	from := math.Max(base.at, start)
	view.span = float64(now) - from
	if view.span < burnMinSpanSeconds {
		return view
	}
	view.known = true
	rise := win.used - base.used
	if rise < burnMinRise {
		return view
	}
	view.perHour = rise / view.span * 3600
	left := target - win.used
	if left <= 0 {
		return view
	}
	view.runOut = left / view.perHour * 3600
	view.early = view.runOut < view.resetIn
	view.stop = view.early && share > 0 && view.runOut < share*view.resetIn && getBool(section(cfg, "subagents"), "guard", true)
	return view
}

func burnVerdict(cfg, state object, usage usageView, now int64) burnView {
	view := weeklyBurn(cfg, usage, now)
	view.stop = view.stop && !pauseHolds(cfg, state, usage, now)
	return view
}

func (view burnView) refusing() bool {
	return view.stop && !observing
}

func (view burnView) paceText() string {
	return formatNumber(math.Round(view.perHour*10) / 10)
}

func (view burnView) burnReason(lookback float64, refused string) string {
	return fmt.Sprintf("[noctis] The weekly limit is burning too fast: %s points an hour over the last %s h, so it would reach its pause point (%s%%) in about %s, %s before its reset at %s. %s are refused until the pace drops (each subagent starts with a context of its own and re-reads it on every call). Do this work yourself in this session, reading only what it needs.",
		view.paceText(), formatNumber(lookback), formatNumber(view.target), englishDurationText(view.runOut), englishDurationText(view.resetIn-view.runOut), formatTime(view.resetsAt), refused)
}

func burnMark(level string, view burnView) string {
	return fmt.Sprintf("burn:%s:%d", level, int64(math.Round(view.resetsAt/3600)))
}

func burnNotice(cfg, notified object, view burnView) (string, []string) {
	if !view.early {
		return "", nil
	}
	key, fresh := "notice.burnEarly", []string{burnMark("early", view)}
	if view.refusing() {
		key, fresh = "notice.burnStop", append(fresh, burnMark("stop", view))
	}
	if notified[fresh[len(fresh)-1]] != nil {
		return "", nil
	}
	return burnNoticeText(cfg, key, view), fresh
}

func burnNoticeText(cfg object, key string, view burnView) string {
	_, lookback, _ := burnSettings(cfg)
	return T(key, view.paceText(), formatNumber(lookback), formatNumber(view.target), durationText(view.runOut), durationText(view.resetIn-view.runOut))
}

func burnStatusText(view burnView) string {
	switch {
	case view.refusing():
		return T("statusline.burnStop", durationText(view.runOut))
	case view.early:
		return T("statusline.weekEta", durationText(view.runOut))
	}
	return ""
}
