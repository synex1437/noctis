package main

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The daily digest is one message a day through the alarm webhook, at alarm.digestAt: how far each
// queue got since the last one, the item it takes next, how its check stands, what waits on the
// user, the sessions that wait, and how much of the limits is used, so a user who left a long job to
// run sees all of it on the phone. A runner, scheduled the way a pause's relaunch is, sends it and
// sets up the next day's (fireDigest); a session that starts or stops starts that runner when none
// is set for the time asked or the one set is late (maybeDigest).

const (
	digestKey = "digest"
	// digestQueueCount is how many queue files a digest reports on, the most recently driven first.
	digestQueueCount = 4
	// digestRecentDays is how recently a trusted queue file must have been driven or trusted for
	// the digest to report on it.
	digestRecentDays = 7
	// digestNamedItems is how many items one list in the digest names.
	digestNamedItems = 3
	// digestMaxChars keeps the message within what every preset takes in one piece: Discord's 2000
	// characters is the least.
	digestMaxChars = 1900
	// digestLateSeconds is how late a digest may be before a session that starts or stops sends it.
	digestLateSeconds = 600
	// digestRetrySeconds is how long a session waits before it starts the runner again.
	digestRetrySeconds = 600
	// digestStaleSeconds is how old a usage reading may be before the digest says when it is from.
	digestStaleSeconds = 3600
)

// digestFetchWait bounds the wait for a fresh usage reading before the runner sends the digest.
var digestFetchWait = 15 * time.Second

// startDigestRunner starts noctis digest-run --now, detached: tests replace it.
var startDigestRunner = func() {
	detachedSelf(digestRunnerArgs(files.configDir, "--now"))
}

func digestFile() string {
	return filepath.Join(files.guardDir, "digest.json")
}

func digestRunnerArgs(account string, extra ...string) []string {
	arguments := []string{"digest-run", "--account", account}
	if host := currentHost().id; host != "claude" {
		arguments = append(arguments, "--host", host)
	}
	return append(arguments, extra...)
}

// digestTime is alarm.digestAt as HH:MM while the daily digest is on: a time of day, the alarm on
// and a webhook to send it to. problem says why it is off when alarm.digestAt asks for one.
func digestTime(cfg object) (clock, problem string) {
	alarm := section(cfg, "alarm")
	value := strings.TrimSpace(getString(alarm, "digestAt"))
	if value == "" {
		return "", ""
	}
	parsed, err := time.Parse("15:04", value)
	switch {
	case err != nil:
		return "", T("digest.badTime", value)
	case !getBool(alarm, "enabled", true):
		return "", T("digest.alarmOff")
	case webhookSettings(cfg).target == "":
		return "", T("digest.noWebhook")
	}
	return parsed.Format("15:04"), ""
}

// nextDigestAt is the first moment after now at clock, local time.
func nextDigestAt(now int64, clock string) float64 {
	parsed, _ := time.Parse("15:04", clock)
	moment := time.Unix(now, 0)
	for day := 0; ; day++ {
		at := time.Date(moment.Year(), moment.Month(), moment.Day()+day, parsed.Hour(), parsed.Minute(), 0, 0, moment.Location())
		if at.After(moment) {
			return float64(at.Unix())
		}
	}
}

// maybeDigest starts the digest runner from a hook when alarm.digestAt names a time no runner is
// set for, the runner set is over digestLateSeconds late, or the digest was turned off while its
// runner is still set. It starts it at most every digestRetrySeconds.
func maybeDigest(cfg, state object, now int64) {
	clock, _ := digestTime(cfg)
	record := getMap(state, "digest")
	if observing || (clock == "" && getString(record, "time") == "") {
		return
	}
	at := numberOr(record, "at", 0)
	if clock == getString(record, "time") && at > float64(now-digestLateSeconds) {
		return
	}
	if float64(now)-numberOr(record, "started", 0) < digestRetrySeconds {
		return
	}
	updateState(func(next object) { stateMap(next, "digest")["started"] = float64(now) })
	journal("", "digest", "start-runner", clock, object{"at": at})
	startDigestRunner()
}

// fireDigest is what the digest runner does: it sends the digest that is due, once, and sets up the
// next day's. With the digest turned off it drops what was set up.
func fireDigest(cfg object) {
	clock, problem := digestTime(cfg)
	if clock == "" {
		if dropDigest() && problem != "" {
			warn("daily digest dropped: %s", problem)
		}
		return
	}
	record := getMap(readState(), "digest")
	due := numberOr(record, "at", 0)
	// A scheduler that fires a little early would otherwise have the runner set the same time again
	// and leave, with nothing left to start it.
	if early := due - float64(nowSec()); getString(record, "time") == clock && early > 0 && early <= 120 {
		time.Sleep(time.Duration(early*1000) * time.Millisecond)
	}
	now := nowSec()
	if getString(record, "time") == clock && due > 0 && due <= float64(now) && numberOr(record, "sent", 0) < due {
		claimed := false
		updateState(func(next object) {
			entry := stateMap(next, "digest")
			if numberOr(entry, "at", 0) != due || numberOr(entry, "sent", 0) >= due {
				return
			}
			entry["sent"] = due
			claimed = true
		})
		if claimed {
			sendDigest(cfg, "runner", false)
		}
	}
	armDigest(cfg, clock, nowSec())
}

// armDigest sets up the runner for the next digest at clock, keeping the one set for that time while
// it still stands.
func armDigest(cfg object, clock string, now int64) {
	at := nextDigestAt(now, clock)
	var scheduled object
	withFileLock(scheduleLockFile(digestKey), func() {
		scheduled = getMap(getMap(readState(), "digest"), "scheduled")
		if numberOr(scheduled, "at", 0) != at || !scheduleLives(scheduled) {
			scheduled = scheduleTimedRunner(cfg, digestKey, "the daily digest", at, scheduled, digestRunnerArgs)
			if scheduled == nil {
				scheduled = object{"method": "manual", "at": at}
				warn("no scheduler available for the daily digest; it goes out when a session starts or stops after %s", localISO(at))
			}
		}
		updateState(func(next object) {
			entry := stateMap(next, "digest")
			entry["time"], entry["at"], entry["scheduled"] = clock, at, scheduled
		})
	})
	journal("", "digest", "arm", clock, object{"at": at, "method": getString(scheduled, "method")})
	logInfo("daily digest set for %s via %s", localISO(at), getString(scheduled, "method"))
}

// dropDigest drops the digest's runner and record once alarm.digestAt no longer asks for a digest,
// and says whether there was one. With none it writes nothing, so an account noctis never ran in
// keeps no state.
func dropDigest() bool {
	if len(getMap(peekState(), "digest")) == 0 {
		return false
	}
	var scheduled object
	dropped := false
	updateState(func(next object) {
		entry := getMap(next, "digest")
		if len(entry) == 0 {
			return
		}
		scheduled, dropped = getMap(entry, "scheduled"), true
		next["digest"] = object{}
	})
	if dropped {
		if !ownSchedule(scheduled) {
			cancelScheduled(digestKey, scheduled)
		}
		journal("", "digest", "drop", "alarm.digestAt asks for no digest", nil)
	}
	return dropped
}

// runDigestRunner is the runner armDigest schedules, noctis digest-run: with --at it sleeps until
// then first, as a sleeper does for a pause; with --now a session started it and nothing was
// scheduled for this run.
func runDigestRunner() {
	if !args.present["now"] {
		takeProxiesForRunner(digestKey)
		if isWindows {
			removeScheduledTask(taskName(digestKey))
		}
	}
	cfg := loadedConfig()
	if args.present["at"] {
		at, ok := toNumber(flagString("at"))
		if !ok {
			os.Exit(2)
		}
		if !sleepUntil(at, func() bool { return numberOr(getMap(peekState(), "digest"), "at", 0) != at }) {
			return
		}
		cfg = loadConfig()
	}
	fireDigest(cfg)
	bootOutFinishedLaunchdJob(digestKey)
}

// runDigest is noctis digest: the digest as it would go out now; with --send it goes out now. Once
// alarm.digestAt names a new time, it also sets up the runner for it, and once it asks for no digest
// it drops the runner set.
func runDigest() {
	cfg := loadedConfig()
	clock, problem := digestTime(cfg)
	switch record := getMap(readState(), "digest"); {
	case clock != "" && getString(record, "time") != clock:
		armDigest(cfg, clock, nowSec())
	case clock == "" && len(record) > 0:
		dropDigest()
	}
	if args.present["send"] {
		sent, reason := sendDigest(cfg, "command", true)
		if !sent {
			fmt.Fprintln(os.Stderr, T("webhook.failed", reason))
			os.Exit(1)
		}
		host := ""
		if parsed, err := url.Parse(webhookSettings(cfg).target); err == nil {
			host = parsed.Host
		}
		fmt.Println(T("webhook.sent", host, webhookSettings(cfg).preset))
		return
	}
	freshUsageForDigest(cfg, 5*time.Second)
	now := nowSec()
	report := buildDigest(cfg, readState(), currentUsage(now), readJSON(digestFile()), now)
	fmt.Println(report.title)
	fmt.Println(report.body)
	fmt.Println()
	record := getMap(readState(), "digest")
	switch {
	case problem != "":
		fmt.Println(T("digest.off", problem))
	case clock == "":
		fmt.Println(T("digest.unset", files.config, pluginName))
	default:
		fmt.Println(T("digest.nextAt", formatTime(numberOr(record, "at", 0)), getString(getMap(record, "scheduled"), "method")))
	}
}

// sendDigest builds the digest and sends it; once it went out, what it saw becomes the baseline of
// the next one. The outcome is kept for noctis status.
func sendDigest(cfg object, by string, ignoreBreaker bool) (bool, string) {
	freshUsageForDigest(cfg, digestFetchWait)
	now := nowSec()
	report := buildDigest(cfg, readState(), currentUsage(now), readJSON(digestFile()), now)
	sent, reason := deliverWebhook(cfg, report.title, report.body, ignoreBreaker)
	updateState(func(next object) {
		entry := stateMap(next, "digest")
		entry["sentAt"] = float64(now)
		if sent {
			delete(entry, "failed")
		} else {
			entry["failed"] = reason
		}
	})
	if !sent {
		journal("", "digest", "failed", reason, nil)
		warn("daily digest not delivered: %s", reason)
		return false, reason
	}
	if err := writeJSONAtomic(digestFile(), object{"at": float64(now), "queues": report.done}); err != nil {
		warn("digest.json not written: %v", err)
	}
	journal("", "digest", "sent", by, object{"chars": float64(len([]rune(report.body)))})
	logInfo("daily digest sent (%s)", by)
	return true, ""
}

// freshUsageForDigest fetches the usage first when noctis fetches it itself and the reading it has
// is stale, waiting for the answer at most wait.
func freshUsageForDigest(cfg object, wait time.Duration) {
	if source := getString(section(cfg, "fable"), "source"); source != "oauth" && source != "codex" {
		return
	}
	cached := orObject(readJSON(files.fable), object{})
	if now := nowSec(); float64(now)-numberOr(cached, "fetchedAt", 0) > usageStaleSeconds(cfg) {
		refreshAside(cfg, cached, now, "digest", wait)
	}
}

// digestReport is one digest: its title and text, and the items it saw done in each queue file,
// which become the baseline of the next digest once this one went out.
type digestReport struct {
	title, body string
	done        object
}

func buildDigest(cfg, state object, usage usageView, baseline object, now int64) digestReport {
	lines := []string{digestLimits(cfg, usage, now)}
	if disabled := numberOr(state, "disabledUntil", 0); disabled > float64(now) {
		lines = append(lines, T("digest.paused", formatTime(disabled)))
	}
	lines = append(lines, digestWaits(state, now)...)
	done := object{}
	previous := getMap(baseline, "queues")
	queues := digestQueueFiles(state, now)
	for _, path := range queues {
		key := queueTrustKey(path)
		block, seen := digestQueue(cfg, state, path, getMap(previous, key), numberOr(baseline, "at", 0))
		lines = append(lines, "")
		lines = append(lines, block...)
		done[key] = object{"path": path, "done": seen}
	}
	if len(queues) == 0 {
		lines = append(lines, "", T("digest.noQueues", digestRecentDays))
	}
	return digestReport{title: T("digest.title", pluginName, digestDay(now)), body: truncateText(strings.Join(lines, "\n"), digestMaxChars), done: done}
}

func digestDay(now int64) string {
	moment := time.Unix(now, 0)
	return fmt.Sprintf("%s %02d.%02d", dayName(int(moment.Weekday())), moment.Day(), int(moment.Month()))
}

// digestLimits is the usage badge of the status line, with the time of the reading when it is old.
func digestLimits(cfg object, usage usageView, now int64) string {
	badge := usageBadgeAt(usage, cfg, now)
	if badge == "" {
		return T("digest.limitsNone")
	}
	line := T("digest.limits", badge)
	if usage.updatedAt > 0 && float64(now)-usage.updatedAt > digestStaleSeconds {
		line += T("digest.limitsAge", formatTime(usage.updatedAt))
	}
	return line
}

// digestWaits names the sessions that wait: on a limit, or for a deferral of their queue to end.
func digestWaits(state object, now int64) []string {
	lines := []string{}
	waits := getMap(state, "waits")
	for _, sid := range sortedKeys(waits) {
		wait := toObject(waits[sid])
		if !waitLive(wait, now) {
			continue
		}
		if getString(wait, "window") == "queue" {
			lines = append(lines, T("digest.queueWake", shortSid(sid), getString(wait, "label"), formatTime(numberOr(wait, "resumeAt", 0))))
			continue
		}
		lines = append(lines, T("digest.wait", shortSid(sid), getString(wait, "label"), formatTime(numberOr(wait, "resumeAt", 0))))
	}
	wakes := getMap(state, "queueWakes")
	for _, sid := range sortedKeys(wakes) {
		wake := toObject(wakes[sid])
		lines = append(lines, T("digest.queueWake", shortSid(sid), getString(wake, "label"), formatTime(numberOr(wake, "at", 0))))
	}
	return lines
}

// digestQueueFiles are the queue files a digest reports on: the trusted ones still there that were
// driven, or trusted, within digestRecentDays, the most recently driven first.
func digestQueueFiles(state object, now int64) []string {
	type recent struct {
		path string
		used float64
	}
	found := []recent{}
	for _, raw := range getMap(state, "queueTrust") {
		record := toObject(raw)
		path := getString(record, "path")
		used := math.Max(numberOr(record, "used", 0), numberOr(record, "at", 0))
		if path == "" || float64(now)-used > digestRecentDays*86400 || statSafe(path) == nil {
			continue
		}
		found = append(found, recent{path, used})
	}
	sort.Slice(found, func(a, b int) bool {
		if found[a].used != found[b].used {
			return found[a].used > found[b].used
		}
		return found[a].path < found[b].path
	})
	paths := []string{}
	for _, entry := range found[:min(len(found), digestQueueCount)] {
		paths = append(paths, entry.path)
	}
	return paths
}

// digestQueue reports on the queue file at path: how many items are done and open, which were done
// since the last digest (before holds the items that one saw done), the item it takes next, its
// check, and what waits on the user. It returns the lines and the items it sees done now.
func digestQueue(cfg, state object, path string, before object, since float64) ([]string, []any) {
	content, _ := readQueueText(path)
	entries, _ := parseQueueEntries(content)
	view := queueSnapshotOf(path, content)
	seen := map[string]bool{}
	for _, raw := range getList(before, "done") {
		if digest, ok := raw.(string); ok {
			seen[digest] = true
		}
	}
	doneNow, newly := []any{}, []string{}
	for _, entry := range entries {
		if !entry.checked || entry.text == "" {
			continue
		}
		digest := queueItemDigest(entry.text)[:12]
		doneNow = append(doneNow, digest)
		if before != nil && !seen[digest] {
			newly = append(newly, entry.text)
		}
	}
	name, open := filepath.Base(path), view.total+view.human+view.deferred
	lines := []string{T("digest.queue", name, len(doneNow), open)}
	if open == 0 {
		lines[0] = T("digest.queueFinished", name, len(doneNow))
	}
	if before != nil {
		if len(newly) == 0 {
			lines = append(lines, "- "+T("digest.doneNone", formatTime(since)))
		} else {
			lines = append(lines, "- "+T("digest.doneSince", formatTime(since), len(newly), digestNames(newly)))
		}
	}
	if trusted, changed, _ := queueTrustGapOf(cfg, path, content); !trusted {
		lines = append(lines, "- "+T("digest.untrusted", len(changed), pluginName))
	}
	switch {
	case len(view.items) > 0:
		lines = append(lines, "- "+T("digest.next", digestItem(view.items[0])))
	case view.total > 0:
		lines = append(lines, "- "+T("digest.nextBlocked", view.total))
	}
	if line := digestCheck(cfg, state, path, content); line != "" {
		lines = append(lines, "- "+line)
	}
	if view.human > 0 {
		lines = append(lines, "- "+T("digest.human", view.human, digestNames(view.humanItems)))
	}
	if view.deferred > 0 {
		shown := []string{}
		for _, item := range view.deferredItems[:min(len(view.deferredItems), digestNamedItems)] {
			text := digestItem(item.text)
			if item.reason != "" {
				text += " — " + truncateText(printableItem(item.reason), 60)
			}
			if item.until > 0 {
				text += " (" + T("digest.until", formatTime(item.until)) + ")"
			}
			shown = append(shown, text)
		}
		named := strings.Join(shown, "; ")
		if more := view.deferred - len(shown); more > 0 {
			named = T("queue.unmatchedMore", named, more)
		}
		lines = append(lines, "- "+T("digest.deferred", view.deferred, named))
	}
	return lines, doneNow
}

// digestCheck says how the check of the queue at path stands, or "" when it has none.
func digestCheck(cfg, state object, path, content string) string {
	if queueCheckCommandOf(cfg, path, content) == "" {
		return ""
	}
	record := queueCheckRecord(state, path)
	switch {
	case queueHeld(cfg, state, path):
		return T("digest.checkHeld", formatTime(numberOr(record, "held", 0)), pluginName)
	case numberOr(record, "failures", 0) > 0:
		return T("digest.checkFailing", int(numberOr(record, "failures", 0)), formatTime(numberOr(record, "at", 0)))
	case numberOr(record, "at", 0) > 0:
		return T("digest.checkPassed", formatTime(numberOr(record, "at", 0)))
	}
	return T("digest.checkNone")
}

func digestItem(text string) string {
	return truncateText(printableItem(text), 60)
}

// digestNames names the first digestNamedItems of texts and counts the rest.
func digestNames(texts []string) string {
	shown := []string{}
	for _, text := range texts[:min(len(texts), digestNamedItems)] {
		shown = append(shown, digestItem(text))
	}
	named := strings.Join(shown, "; ")
	if more := len(texts) - len(shown); more > 0 {
		return T("queue.unmatchedMore", named, more)
	}
	return named
}

// digestStatus is the line of noctis status on the daily digest, or "" when none is asked for.
func digestStatus(cfg, state object) string {
	clock, problem := digestTime(cfg)
	if problem != "" {
		return T("status.digestOff", problem)
	}
	if clock == "" {
		return ""
	}
	record := getMap(state, "digest")
	line := T("status.digestPending", clock, pluginName)
	if at := numberOr(record, "at", 0); at > 0 && getString(record, "time") == clock {
		line = T("status.digest", clock, formatTime(at), getString(getMap(record, "scheduled"), "method"))
	}
	if failed := getString(record, "failed"); failed != "" {
		line += T("status.digestFailed", formatTime(numberOr(record, "sentAt", 0)), failed)
	}
	return line
}
