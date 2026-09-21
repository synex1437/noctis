package main

import (
	"math"
	"os"
	"time"
)

const (
	sleepTickSeconds    = 15
	sleepFarSeconds     = 1800
	sleepFarTickSeconds = 60
	earlyResetDrop      = 10
	earlyResetMaxAgeMul = 2
)

func earlyResetPollSeconds(cfg object) float64 {
	minutes := numberOr(section(cfg, "wait"), "earlyResetPollMinutes", 5)
	if minutes <= 0 {
		return 0
	}
	return math.Max(3, minutes*60)
}

func windowClearedAt(usage usageView, windowKey string, threshold, startedAt, maxAge, until float64) bool {
	if usage.updatedAt < startedAt {
		return false
	}
	var win *window
	switch windowKey {
	case "five_hour":
		win = usage.fiveHour
	case "seven_day":
		win = usage.sevenDay
	case "fable":
		win = usage.fable
	default:
		return false
	}
	if win == nil {

		fresh := usage.hasAny && maxAge > 0 && float64(nowSec())-usage.updatedAt <= maxAge
		another := usage.fiveHour != nil || usage.sevenDay != nil || usage.fable != nil
		return fresh && another && until > 0 && float64(nowSec()) >= until
	}
	if maxAge > 0 && win.staleness > maxAge {
		return false
	}
	return win.used <= threshold-earlyResetDrop
}

func earlyResetSeen(cfg object, record object, poll bool) bool {
	now := nowSec()
	pollEvery := earlyResetPollSeconds(cfg)
	if poll && pollEvery > 0 && currentHost().limits {
		refreshFable(cfg, now, "early-reset", math.Max(1, pollEvery-2), false)
	}
	threshold := numberOr(record, "threshold", 0)
	if threshold <= 0 {
		threshold = numberOr(record, "used", 100)
	}
	maxAge := float64(0)
	if pollEvery > 0 {
		maxAge = pollEvery*earlyResetMaxAgeMul + 60
	}
	return windowClearedAt(currentUsage(now), getString(record, "window"), threshold, numberOr(record, "startedAt", float64(now)), maxAge, numberOr(record, "until", 0))
}

func sleepUntil(epoch float64, onTick func() bool) bool {
	return sleepUntilEvery(epoch, sleepTickSeconds, onTick)
}

func sleepUntilEvery(epoch, tickSeconds float64, onTick func() bool) bool {
	return sleepUntilPaced(epoch, func(float64) float64 { return tickSeconds }, onTick)
}

func sleepUntilPaced(epoch float64, pace func(remaining float64) float64, onTick func() bool) bool {
	for {
		remaining := epoch - float64(nowSec())
		if remaining <= 0 {
			return true
		}
		if onTick != nil && onTick() {
			return false
		}
		step := math.Max(1, pace(remaining))
		time.Sleep(time.Duration(math.Min(remaining, step)*1000) * time.Millisecond)
	}
}

func (w *waitWatch) tickSeconds() float64 {
	if w.pollEvery > 0 && w.pollEvery < sleepTickSeconds {
		return w.pollEvery
	}
	return sleepTickSeconds
}

func (w *waitWatch) tickPace(remaining float64) float64 {
	base := w.tickSeconds()
	if remaining <= sleepFarSeconds {
		return base
	}
	relaxed := float64(sleepFarTickSeconds)
	if w.pollEvery > 0 && w.pollEvery < relaxed {
		relaxed = w.pollEvery
	}
	if relaxed < base {
		return base
	}
	return relaxed
}

type waitWatch struct {
	sid       string
	cfg       object
	pollEvery float64
	lastPoll  float64
	startedAt float64
	cancelled bool
	early     bool
	heartbeat bool
}

func newWaitWatch(cfg object, sid string, heartbeat bool) *waitWatch {
	watch := &waitWatch{sid: sid, cfg: cfg, pollEvery: earlyResetPollSeconds(cfg), lastPoll: float64(nowSec()), heartbeat: heartbeat}

	if record := getMap(getMap(readState(), "waits"), sid); record != nil {
		watch.startedAt = numberOr(record, "startedAt", 0)
	}
	return watch
}

func (w *waitWatch) tick() bool {
	now := float64(nowSec())
	record := getMap(getMap(readState(), "waits"), w.sid)
	if record == nil || (w.startedAt > 0 && numberOr(record, "startedAt", -1) != w.startedAt) {
		w.cancelled = true
		return true
	}
	if w.heartbeat {
		updateState(func(state object) {
			if current := getMap(getMap(state, "waits"), w.sid); current != nil {
				current["heartbeat"] = now
			}
		})
	}
	if w.pollEvery <= 0 {
		return false
	}

	poll := now-w.lastPoll >= w.pollEvery
	if poll {
		w.lastPoll = now
	}
	if earlyResetSeen(w.cfg, record, poll) {
		w.early = true
		return true
	}
	return false
}

func startResetWatcher(cfg object, sid string, at float64) int {
	if earlyResetPollSeconds(cfg) <= 0 || !currentHost().limits || os.Getenv("NOCTIS_NO_WATCHER") != "" {
		return 0
	}
	return detachedSelf(runnerArgs("sleeper", sid, files.configDir, "--at", formatNumber(at), "--watch"))
}

func repairOrphanWaits() {
	rescheduleStrandedWaits(readState())
}

func triggerEarlyResumes(cfg object) {
	if os.Getenv("NOCTIS_NO_EARLY_TRIGGER") != "" {
		return
	}
	state := readState()
	for sid, raw := range getMap(state, "waits") {
		record := toObject(raw)
		if record == nil || hookSleeping(record) || numberOr(record, "earlyTriggeredAt", 0) > 0 {
			continue
		}
		if getMap(getMap(state, "handedOff"), sid) != nil {
			continue
		}
		if !earlyResetSeen(cfg, record, false) {
			continue
		}
		claimed := false
		updateState(func(next object) {
			if current := getMap(getMap(next, "waits"), sid); current != nil && numberOr(current, "earlyTriggeredAt", 0) == 0 {
				current["earlyTriggeredAt"] = float64(nowSec())
				claimed = true
			}
		})
		if !claimed {
			continue
		}
		journal(sid, "statusline", "early-reset", getString(record, "label"), nil)
		logInfo("early reset seen for %s (%s); resuming ahead of schedule", sid, getString(record, "label"))
		detachedSelf(runnerArgs("resume", sid, files.configDir))
	}
}
