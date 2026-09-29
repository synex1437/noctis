package main

import (
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// A deferral with an end (noctis queue defer --until) sets an item aside until then. Once every open
// item left is deferred, marked (human) or waits for one of those, the session stops, and the soonest
// end of a deferral whose item could go on then is when the queue can go on. When the Stop hook may
// wait that long it holds the stop (waitForDeferral) and answers it again once the item is eligible,
// so the queue goes on in the same session. Otherwise, and in case that hook is cut off, a runner set
// for that moment (armQueueWake) resumes the session as noctis resumes a paused one. A prompt in the
// session, or in another one the same queue drives, drops that wake: the queue is in hand again.

// deferralPoll is how often a Stop hook that waits for a deferral looks at the queue and the state.
var deferralPoll = 15 * time.Second

var (
	// endedDeferrals are the items whose deferral ended while this Stop hook waited, and
	// answeringAgain tells that the hook answers the stop a second time, after that wait.
	endedDeferrals []deferredItem
	answeringAgain bool
	// hookWaited is how long this hook had run when it answers the stop again, so a pause it holds
	// or a check it runs after that still fits the hook's time.
	hookWaited float64
)

// queueWakeKey names the runner of a session's queue wake apart from the runner of its pause.
func queueWakeKey(sid string) string {
	return sid + "|queue-wake"
}

// freedNames names deferred items for the user.
func freedNames(items []deferredItem) string {
	shown := []string{}
	for _, item := range items[:min(len(items), queueUnmatchedNamed)] {
		shown = append(shown, `"`+truncateText(item.text, 80)+`"`)
	}
	names := strings.Join(shown, "; ")
	if more := len(items) - len(shown); more > 0 {
		names = T("queue.unmatchedMore", names, more)
	}
	return names
}

// endedDeferralRule tells Claude which deferrals ended while the stop waited, or is "" when none
// did.
func endedDeferralRule() string {
	if len(endedDeferrals) == 0 {
		return ""
	}
	shown := []string{}
	for _, item := range endedDeferrals[:min(len(endedDeferrals), queueUnmatchedNamed)] {
		shown = append(shown, fmt.Sprintf(`"%s" (it waited on: %s)`, truncateText(item.text, 80), truncateText(item.reason, 80)))
	}
	note := strings.Join(shown, "; ")
	if more := len(endedDeferrals) - len(shown); more > 0 {
		note = fmt.Sprintf("%s and %d more", note, more)
	}
	return fmt.Sprintf(" The deferral of these items has ended, so they are eligible again: %s. If what one waited on is still missing, defer it again with %s queue defer instead of stalling on it.", note, pluginName)
}

// endedDeferralNotice tells the user which deferrals ended while the stop waited, or is "".
func endedDeferralNotice() string {
	if len(endedDeferrals) == 0 {
		return ""
	}
	return T("queue.deferralEnded", freedNames(endedDeferrals))
}

// deferralWaitFits tells whether a Stop hook that began at started may wait until end and still
// have the time the queue's check needs after it, as a pause held in the hook must (waitsInHook).
func deferralWaitFits(cfg, state, input object, end float64, started int64) bool {
	return waitsInHook(section(cfg, "wait"), numberOr(state, "hookCapSeconds", 0), end-float64(started), queueCheckReserve("stop", cfg, input))
}

// waitForDeferral holds the stop in the hook until the soonest deferral that holds an item back
// ends, when the hook may wait that long, then answers the stop again: the item is eligible, so the
// queue goes on in this session. A wake armed for just after that moment resumes the session
// instead if the hook is cut off first. It reports whether it answered the stop.
func waitForDeferral(cfg, state, input object, sid, queuePath, label string, view queueView, started int64) bool {
	end, freed := view.freeAt, view.freed
	if end <= 0 || answeringAgain || !deferralWaitFits(cfg, state, input, end, started) {
		return false
	}
	if observed(sid, "Stop", "wait-deferral", freedNames(freed), object{"until": end}) {
		return false
	}
	holder := strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	grace := math.Max(0, numberOr(section(cfg, "wait"), "builtinGraceSeconds", 0))
	backup := armQueueWake(cfg, input, sid, queuePath, label, end+grace, holder) > 0
	updateState(func(next object) { delete(stateMap(next, "stopGuard"), sid) })
	journal(sid, "Stop", "wait-deferral", freedNames(freed), object{"until": end})
	logInfo("queue %s of %s waits in the Stop hook until %s, when the deferral of %d item(s) ends", queuePath, sid, localISO(end), len(freed))
	notify(cfg, pluginName, T("queue.deferralWaitNotify", label, formatTime(end), freedNames(freed)))
	restoreCollector()
	beat := float64(nowSec())
	for float64(nowSec()) < end {
		time.Sleep(min(deferralPoll, time.Duration((end-float64(nowSec()))*float64(time.Second))))
		current := peekState()
		if numberOr(current, "disabledUntil", 0) > float64(nowSec()) {
			// noctis was paused: the stop goes ahead, and the wake resumes the session once the pause
			// is over.
			journal(sid, "Stop", "allow-stop", "noctis was paused while the queue waited on a deferral", nil)
			return true
		}
		if backup && getString(getMap(getMap(current, "queueWakes"), sid), "holder") != holder {
			// A prompt, noctis cancel or a stop of another session took the wake: the queue is not
			// this wait's to go on with.
			journal(sid, "Stop", "allow-stop", "the queue's wake was dropped while it waited on a deferral", nil)
			return true
		}
		if getMap(getMap(current, "handedOff"), sid) != nil {
			dropQueueWake(sid, holder)
			return true
		}
		fresh := queueSnapshot(queuePath)
		if len(fresh.items) > 0 {
			break
		}
		if fresh.freeAt != end {
			if fresh.freeAt <= 0 || !deferralWaitFits(cfg, current, input, fresh.freeAt, started) {
				break
			}
			end, freed = fresh.freeAt, fresh.freed
			if backup {
				backup = armQueueWake(cfg, input, sid, queuePath, label, end+grace, holder) > 0
			}
			logInfo("queue %s of %s now waits until %s, when the deferral of %d item(s) ends", queuePath, sid, localISO(end), len(freed))
		}
		if now := float64(nowSec()); backup && now-beat >= heartbeatFreshSeconds/4 {
			beat = now
			updateState(func(next object) {
				if wake := getMap(getMap(next, "queueWakes"), sid); getString(wake, "holder") == holder {
					wake["heartbeat"] = now
				}
			})
		}
	}
	dropQueueWake(sid, holder)
	deferrals := queueDeferrals(peekState(), queuePath)
	ended := []deferredItem{}
	for _, item := range freed {
		if _, held := deferrals[item.digest]; !held {
			ended = append(ended, item)
		}
	}
	journal(sid, "Stop", "deferral-ended", freedNames(ended), object{"waited": float64(nowSec() - started)})
	logInfo("queue %s of %s: %d deferral(s) ended after %s in the Stop hook; answering the stop again", queuePath, sid, len(ended), durationText(float64(nowSec()-started)))
	endedDeferrals, answeringAgain, hookWaited = ended, true, float64(nowSec()-started)
	defer func() { endedDeferrals, answeringAgain, hookWaited = nil, false, 0 }()
	onStop(input, cfg)
	return true
}

// armQueueWake sets the runner that resumes the session at at, when the soonest deferral that holds
// an item back ends, and keeps in state.json's queueWakes what that takes. holder names the hook that
// waits for that moment itself, if one does: the runner leaves the session to it while it lives. It
// returns when the wake is set for, or 0 when nothing can wake the session: a cloud session starts
// no runner.
func armQueueWake(cfg, input object, sid, queuePath, label string, at float64, holder string) float64 {
	if at <= 0 || observing || cloudSession() {
		return 0
	}
	cwd := getString(input, "cwd")
	record := object{
		"queue": queuePath, "label": label, "at": at, "armedAt": float64(nowSec()), "cwd": cwd,
		"transcript": getString(input, "transcript_path"), "permissionMode": permissionModeOf(input),
		"configDirEnv": os.Getenv(claudeConfigEnv),
	}
	if mode := inferLaunchMode(cfg, sid); mode != "" {
		record["launchMode"] = mode
	}
	if holder != "" {
		record["holder"], record["heartbeat"] = holder, float64(nowSec())
	}
	recordProjectDir(record)
	recordTree(cfg, record, cwd)
	return scheduleQueueWake(cfg, sid, record)
}

// scheduleQueueWake stores record as the session's queue wake and schedules its runner for the
// record's at, keeping the runner already scheduled for that moment.
func scheduleQueueWake(cfg object, sid string, record object) float64 {
	key := queueWakeKey(sid)
	at := math.Max(numberOr(record, "at", 0), float64(nowSec()+15))
	record["at"] = at
	withFileLock(scheduleLockFile(key), func() {
		previous := getMap(getMap(readState(), "queueWakes"), sid)
		scheduled := getMap(previous, "scheduled")
		if numberOr(scheduled, "at", 0) != at || !scheduleLives(scheduled) {
			scheduled = scheduleQueueWakeRunner(cfg, sid, at, scheduled)
		}
		record["scheduled"] = scheduled
		updateState(func(next object) { stateMap(next, "queueWakes")[sid] = record })
	})
	journal(sid, "Stop", "arm-queue-wake", getString(record, "label"), object{"at": at, "method": getString(getMap(record, "scheduled"), "method")})
	return at
}

// scheduleLives tells whether a runner scheduled earlier can still fire: a sleeper must still run,
// and a wake with no runner gets another try.
func scheduleLives(scheduled object) bool {
	switch getString(scheduled, "method") {
	case "", "manual", "cloud":
		return false
	case "sleeper":
		pid, _ := getNumber(scheduled, "pid")
		return pid > 0 && processAlive(int(pid))
	}
	return true
}

// ownSchedule tells whether scheduled started this process, which a cancel must leave to finish.
func ownSchedule(scheduled object) bool {
	switch getString(scheduled, "method") {
	case "systemd":
		return ownSystemdUnit(getString(scheduled, "unit"))
	case "launchd":
		return ownLaunchdJob(getString(scheduled, "label"))
	case "sleeper":
		return int(numberOr(scheduled, "pid", 0)) == os.Getpid()
	}
	return false
}

// scheduleQueueWakeRunner schedules noctis queue-wake for the session at at, as scheduleRunnerLocked
// schedules the runner of a pause, and cancels the runner previous names.
func scheduleQueueWakeRunner(cfg object, sid string, at float64, previous object) object {
	key := queueWakeKey(sid)
	nativeAllowed := os.Getenv("NOCTIS_NO_TASKS") == ""
	backend := ""
	if nativeAllowed {
		backend = schedulerBackend()
	}
	replacingTask := nativeAllowed && backend == "task"
	if !ownSchedule(previous) {
		cancelScheduledKeepingTask(key, previous, replacingTask)
	}
	wake := getBool(section(cfg, "alarm"), "wakePc", true)
	var scheduled object
	if replacingTask {
		keepEnvironmentForRunner(key, slices.Concat(carriedEnvNames, proxyEnvNames))
		if result := scheduleWindowsTask(taskName(key), at, runnerArgs("queue-wake", sid, `"`+files.configDir+`"`), wake); result.ok {
			scheduled = object{"method": "task", "taskName": taskName(key), "at": at}
		} else {
			removeScheduledTask(taskName(key))
			dropProxiesForRunner(key)
			warn("task scheduling for the queue wake of %s failed, falling back to a sleeper: %s", sid, orDefault(result.err, result.stderr))
		}
	} else if nativeAllowed {
		if native, ok := scheduleNative(backend, key, at, runnerArgs("queue-wake", sid, files.configDir), wake); ok {
			scheduled = native
		}
	}
	if scheduled == nil && os.Getenv("NOCTIS_NO_SCHEDULE") != "" {
		scheduled = object{"method": "manual", "at": at}
	}
	if scheduled == nil {
		if pid := detachedSelf(runnerArgs("queue-wake", sid, files.configDir, "--at", formatNumber(at))); pid > 0 {
			scheduled = object{"method": "sleeper", "pid": float64(pid), "at": at}
		} else {
			scheduled = object{"method": "manual", "at": at}
			fail("no scheduler available for the queue wake of %s; its queue goes on when a prompt is typed in it after %s", sid, localISO(at))
		}
	}
	logInfo("queue wake for %s scheduled at %s via %s", sid, localISO(at), getString(scheduled, "method"))
	return scheduled
}

// dropQueueWake drops the session's queue wake and cancels its runner, when holder is "" or holds
// the wake.
func dropQueueWake(sid, holder string) {
	if getMap(getMap(peekState(), "queueWakes"), sid) == nil {
		return
	}
	var scheduled object
	dropped := false
	updateState(func(next object) {
		wakes := stateMap(next, "queueWakes")
		wake := getMap(wakes, sid)
		if wake == nil || (holder != "" && getString(wake, "holder") != holder) {
			return
		}
		scheduled, dropped = getMap(wake, "scheduled"), true
		delete(wakes, sid)
	})
	if dropped && !ownSchedule(scheduled) {
		cancelScheduled(queueWakeKey(sid), scheduled)
	}
}

// dropQueueWakes drops the queue wakes a prompt makes moot: the session's own, which goes on now,
// and those of other sessions the same queue drives, since this one takes that queue up.
func dropQueueWakes(cfg, state, input object, sid string) {
	wakes := getMap(state, "queueWakes")
	if len(wakes) == 0 {
		return
	}
	queuePath, looked := "", false
	for _, other := range sortedKeys(wakes) {
		if other != sid {
			if !looked {
				queuePath, looked = drivenQueueFile(cfg, state, input, sid), true
			}
			if queuePath == "" || getString(toObject(wakes[other]), "queue") != queuePath {
				continue
			}
		}
		dropQueueWake(other, "")
		journal(other, "UserPromptSubmit", "drop-queue-wake", "a prompt took the queue up", object{"by": sid})
	}
}

// runQueueWake is the runner armQueueWake schedules: noctis queue-wake --sid <session> [--at
// <epoch>]. With --at it sleeps until then first, as a sleeper does for a pause.
func runQueueWake() {
	sid := flagString("sid")
	if sid == "" {
		os.Exit(2)
	}
	key := queueWakeKey(sid)
	takeProxiesForRunner(key)
	if isWindows {
		removeScheduledTask(taskName(key))
	}
	if args.present["at"] {
		at, ok := toNumber(flagString("at"))
		if !ok {
			os.Exit(2)
		}
		if !sleepUntil(at, func() bool { return getMap(getMap(peekState(), "queueWakes"), sid) == nil }) {
			return
		}
	}
	fireQueueWake(sid)
	bootOutFinishedLaunchdJob(key)
}

// fireQueueWake resumes the session sid when its queue wake is due and still wanted: nothing else
// went on with the session since the wake was armed, the queue still drives it, and it holds an item
// Claude can take now. The wake becomes a wait of its own kind, resumed as noctis resumes a paused
// session, so the limits, a fresh start and the relaunch go as they do there.
func fireQueueWake(sid string) {
	cfg := loadConfig()
	now := nowSec()
	state := readState()
	record := getMap(getMap(state, "queueWakes"), sid)
	if record == nil {
		logInfo("queue wake %s: nothing to wake (dropped or already done)", sid)
		return
	}
	at := numberOr(record, "at", 0)
	rearm := func(when float64, why string) {
		record["at"] = when
		scheduleQueueWake(cfg, sid, record)
		logInfo("queue wake %s: %s; set again for %s", sid, why, localISO(when))
	}
	switch {
	case float64(now) < at-1:
		logInfo("queue wake %s: set for %s now; this earlier runner leaves it to its own", sid, localISO(at))
		return
	case getString(record, "holder") != "" && holderAlive(record) && numberOr(record, "aliveChecks", 0) < aliveHookMaxChecks:
		record["aliveChecks"] = numberOr(record, "aliveChecks", 0) + 1
		rearm(float64(now+300), "the Stop hook that waits for the deferral still runs")
		return
	case numberOr(state, "disabledUntil", 0) > float64(now):
		rearm(numberOr(state, "disabledUntil", 0)+15, "noctis is paused")
		return
	}
	claimed := false
	updateState(func(next object) {
		wakes := stateMap(next, "queueWakes")
		if current := getMap(wakes, sid); current != nil && numberOr(current, "armedAt", -1) == numberOr(record, "armedAt", -2) && numberOr(current, "at", -1) == at {
			delete(wakes, sid)
			claimed = true
		}
	})
	if !claimed {
		logInfo("queue wake %s: the wake changed while this runner looked at it; its own runner takes it", sid)
		return
	}
	if scheduled := getMap(record, "scheduled"); !ownSchedule(scheduled) {
		cancelScheduled(queueWakeKey(sid), scheduled)
	}
	skip := func(reason string) {
		journal(sid, "queue-wake", "skip", reason, nil)
		logInfo("queue wake %s: %s; not resuming it", sid, reason)
	}
	state = readState()
	if getMap(getMap(state, "waits"), sid) != nil {
		skip("the session has a pause of its own, whose runner takes the queue up")
		return
	}
	if liveRunner(getMap(getMap(state, "handedOff"), sid)) > 0 {
		skip("another runner holds the session")
		return
	}
	if sessionContinuedAfter(record, numberOr(record, "armedAt", 0)+pauseSettleSeconds) {
		skip("the session went on since the wake was set")
		return
	}
	queuePath := getString(record, "queue")
	if queuePath == "" || sessionQueueFile(cfg, sid, sessionDirs(getString(record, "projectDir"), getString(record, "cwd"))...) != queuePath {
		skip("the queue no longer drives the session")
		return
	}
	view, trusted := trustedQueueSnapshot(cfg, queuePath)
	if !trusted || queueHeld(cfg, state, queuePath) {
		skip("the queue is not trusted as it is, or is held")
		return
	}
	if len(view.items) == 0 {
		if view.freeAt > float64(now) {
			delete(record, "holder")
			delete(record, "aliveChecks")
			rearm(view.freeAt, "the deferral it waited for was moved")
			return
		}
		skip("no item of the queue is eligible")
		return
	}
	label := getString(record, "label")
	if getString(section(cfg, "resume"), "mode") == "none" {
		notify(cfg, pluginName, T("wait.queueReady", label, T("wait.readyNone")))
		journal(sid, "queue-wake", "notify", label, object{"eligible": float64(len(view.items))})
		return
	}
	wait := object{
		"kind": "queue", "window": "queue", "label": label, "until": at, "resumeAt": float64(now),
		"startedAt": float64(now), "heartbeat": float64(now), "inHook": false, "storedBy": float64(os.Getpid()),
		"queuedPrompt": fmt.Sprintf("Continue. A deferral in %s ended while this session was stopped, so its item is eligible again: take the queue up where it stopped. If what an item waited on is still missing, defer it again with %s queue defer instead of stalling on it.", label, pluginName),
	}
	for _, name := range []string{"cwd", "transcript", "permissionMode", "launchMode", "configDirEnv", "projectDir", "tree"} {
		if value, present := record[name]; present {
			wait[name] = value
		}
	}
	if tokens, known := sessionContextTokens(sid); known {
		wait["contextTokens"] = tokens
	}
	if _, _, due := freshStartDue(cfg, wait, sid, now); due {
		input := object{"session_id": sid, "cwd": getString(record, "cwd"), "transcript_path": getString(record, "transcript")}
		wait["checkpoint"] = buildCheckpoint(input, T("queue.deferralReason", label), resolveSessionModel(cfg, readState(), readJSON(files.usage), sid), cfg)
	}
	stored := false
	updateState(func(next object) {
		if waits := stateMap(next, "waits"); waits[sid] == nil {
			waits[sid], stored = wait, true
		}
	})
	if !stored {
		skip("the session paused meanwhile")
		return
	}
	journal(sid, "queue-wake", "resume", label, object{"eligible": float64(len(view.items))})
	logInfo("queue wake %s: a deferral in %s ended with %d item(s) eligible; resuming the session", sid, queuePath, len(view.items))
	resumeWait(sid, "queue")
}
