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
	pauseSettleSeconds  = 60
)

var resetCheckOffsets = []float64{10, 30, 60}

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
		resetPassed := until > 0 && float64(nowSec()) >= until
		goneSinceReset := usage.updatedAt >= until && usage.lapsedAt[windowKey] < until
		return fresh && another && resetPassed && goneSinceReset
	}
	if maxAge > 0 && win.staleness > maxAge {
		return false
	}
	return win.used <= threshold-earlyResetDrop
}

func dataPause(record object) bool {
	hit := getString(record, "hit")
	if hit == "ceiling" {
		hit = getString(record, "cause")
	}
	return hit == "blind" || hit == "projection"
}

func quietSincePause(record object) bool {
	info := statSafe(getString(record, "transcript"))
	return info != nil && float64(info.ModTime().UnixMilli())/1000 <= numberOr(record, "startedAt", 0)+pauseSettleSeconds
}

func roomReported(cfg object, sid string, record object, usage usageView, now int64) bool {
	win := usage.byKey(getString(record, "window"))
	if win == nil || win.reportedAt < numberOr(record, "startedAt", float64(now)) || float64(now)-win.reportedAt > blindAfterSeconds {
		return false
	}
	state := readState()
	usageFile := readJSON(files.usage)
	context, hasContext := getNumber(getMap(getMap(usageFile, "sessions"), sid), "context")
	if evaluate(cfg, usage, resolveSessionModel(cfg, state, usageFile, sid), context, hasContext).wait != nil {
		return false
	}
	_, _, over := dailyBudgetStatus(cfg, state, usage, now)
	return !over || !getBool(section(cfg, "budget"), "hardStop", false)
}

func earlyRelease(cfg object, sid string, record object, fetchOlderThan float64, relaunch bool) string {
	if getString(record, "hit") == "relaunch" {
		return ""
	}
	now := nowSec()
	pollEvery := earlyResetPollSeconds(cfg)
	if fetchOlderThan > 0 && pollEvery > 0 && currentHost().limits {
		backoff := numberOr(readJSON(files.fable), "backoffUntil", 0)
		fable := refreshFableWaiting(cfg, now, "early-reset", fetchOlderThan, false, refreshWait)
		if getString(fable, "error") == "token-expired" && numberOr(fable, "backoffUntil", 0) != backoff {
			tellSignInExpired(cfg, record, numberOr(fable, "fetchedAt", 0))
		}
	}
	threshold := numberOr(record, "threshold", 0)
	if threshold <= 0 {
		threshold = numberOr(record, "used", 100)
	}
	maxAge := float64(0)
	if pollEvery > 0 {
		maxAge = pollEvery*earlyResetMaxAgeMul + 60
	}
	usage := currentUsage(now)
	reference := math.Min(threshold, numberOr(record, "used", threshold))
	if windowClearedAt(usage, getString(record, "window"), reference, numberOr(record, "startedAt", float64(now)), maxAge, numberOr(record, "until", 0)) {
		return "reset"
	}
	if dataPause(record) && (!relaunch || quietSincePause(record)) && roomReported(cfg, sid, record, usage, now) {
		return "data"
	}
	return ""
}

func tellSignInExpired(cfg, record object, lastReading float64) {
	if _, tokenState := oauthTokenState(); tokenState != "expired" {
		return
	}
	first := false
	updateState(func(state object) {
		notified := stateMap(state, "notified")
		if told, ok := getNumber(notified, "signInExpired"); ok && told >= lastReading {
			return
		}
		notified["signInExpired"] = float64(nowSec())
		first = true
	})
	if !first {
		return
	}
	logInfo("the Claude sign-in expired during the %s wait: usage cannot be refreshed until Claude Code signs in again", getString(record, "window"))
	notify(cfg, pluginName, T("wait.signInExpired", getString(record, "label"), formatTime(numberOr(record, "resumeAt", 0))))
}

func sleepUntil(epoch float64, onTick func() bool) bool {
	return sleepUntilEvery(epoch, sleepTickSeconds, onTick)
}

func sleepUntilEvery(epoch, tickSeconds float64, onTick func() bool) bool {
	return sleepUntilPaced(epoch, func(float64) float64 { return tickSeconds }, onTick)
}

func sleepUntilPaced(epoch float64, pace func(remaining float64) float64, onTick func() bool) bool {
	restoreCollector()
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
	pace := w.tickSeconds()
	if remaining > sleepFarSeconds {
		relaxed := float64(sleepFarTickSeconds)
		if w.pollEvery > 0 && w.pollEvery < relaxed {
			relaxed = w.pollEvery
		}
		pace = math.Max(pace, relaxed)
	}
	return w.towardResetCheck(pace)
}

func (w *waitWatch) steadyPace(float64) float64 {
	return w.towardResetCheck(w.tickSeconds())
}

func (w *waitWatch) towardResetCheck(pace float64) float64 {
	now := float64(nowSec())
	if next := w.nextResetCheck(now); next > 0 {
		return math.Min(pace, next-now)
	}
	return pace
}

func (w *waitWatch) nextResetCheck(now float64) float64 {
	reset := resetToConfirm(w.record)
	if reset <= 0 {
		return 0
	}
	for _, offset := range resetCheckOffsets {
		if check := reset + offset; check > now {
			return check
		}
	}
	return 0
}

func (w *waitWatch) resetCheckDue(now float64) float64 {
	reset := resetToConfirm(w.record)
	due := 0.0
	for _, offset := range resetCheckOffsets {
		if check := reset + offset; reset > 0 && check <= now && check > w.lastPoll {
			due = check
		}
	}
	return due
}

func resetToConfirm(record object) float64 {
	if builtinResumeFollows(record) {
		return 0
	}
	return numberOr(record, "until", 0)
}

func builtinResumeFollows(record object) bool {
	return getString(record, "kind") == "stopfailure"
}

func resetSeen(record object, resumeAt float64) (string, object) {
	until := numberOr(record, "until", 0)
	if now := float64(nowSec()); until > 0 && now >= until {
		return "reset-confirmed", object{"ahead": math.Max(0, math.Round(resumeAt-now))}
	}
	return "early-reset", nil
}

type waitWatch struct {
	sid       string
	cfg       object
	record    object
	pollEvery float64
	lastPoll  float64
	startedAt float64
	cancelled bool
	early     bool
	dataBack  bool
	relaunch  bool
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
		rescheduleOwnWait(w.sid, w.startedAt, func(current object) { current["heartbeat"] = now })
	}
	if w.pollEvery <= 0 {
		return false
	}
	w.record = record
	fetchOlderThan := 0.0
	if check := w.resetCheckDue(now); check > 0 {
		fetchOlderThan = math.Max(1, now-check)
	} else if now-w.lastPoll >= w.pollEvery {
		fetchOlderThan = math.Max(1, w.pollEvery-2)
	}
	if fetchOlderThan > 0 {
		w.lastPoll = now
	}
	switch earlyRelease(w.cfg, w.sid, record, fetchOlderThan, w.relaunch) {
	case "reset":
		w.early = true
		return true
	case "data":
		w.dataBack = true
		return true
	}
	return false
}

func startResetWatcher(cfg object, sid string, at float64) int {
	if earlyResetPollSeconds(cfg) <= 0 || !currentHost().limits || os.Getenv("NOCTIS_NO_WATCHER") != "" || cloudSession() {
		return 0
	}
	return detachedSelf(runnerArgs("sleeper", sid, files.configDir, "--at", formatNumber(at), "--watch"))
}

// repairOrphanWaits runs on every status line refresh and session start, so it looks at the kept
// parse of state.json, and takes a copy of its own only to clear a dead hand-off from it.
func repairOrphanWaits() {
	state := peekState()
	if stale := staleHandoffs(state); len(stale) > 0 {
		state = readState()
		dropHandoffs(state, stale)
	}
	rearmStrandedWaits(state)
}

func triggerEarlyResumes(cfg object) {
	if os.Getenv("NOCTIS_NO_EARLY_TRIGGER") != "" || cloudSession() {
		return
	}
	state := peekState()
	for sid, raw := range getMap(state, "waits") {
		record := toObject(raw)
		if record == nil || hookSleeping(record) || numberOr(record, "earlyTriggeredAt", 0) > 0 {
			continue
		}
		if getMap(getMap(state, "handedOff"), sid) != nil && !handoffWatchesEarlierWindow(state, sid, numberOr(record, "startedAt", 0)) {
			continue
		}
		reason := earlyRelease(cfg, sid, record, 0, true)
		if reason == "" {
			continue
		}
		claimed := false
		rescheduleOwnWait(sid, numberOr(record, "startedAt", -1), func(current object) {
			if numberOr(current, "earlyTriggeredAt", 0) == 0 {
				current["earlyTriggeredAt"] = float64(nowSec())
				claimed = true
			}
		})
		if !claimed {
			continue
		}
		if reason == "data" {
			journal(sid, "statusline", "data-back", getString(record, "label"), nil)
			logInfo("fresh usage data shows room for %s (%s); resuming ahead of schedule", sid, getString(record, "label"))
		} else {
			action, facts := resetSeen(record, numberOr(record, "resumeAt", 0))
			journal(sid, "statusline", action, getString(record, "label"), facts)
			logInfo("%s seen for %s (%s); resuming before the planned time", action, sid, getString(record, "label"))
		}
		detachedSelf(runnerArgs("resume", sid, files.configDir, "--release", reason))
	}
}
