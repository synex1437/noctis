package main

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	queueCheckTailLines = 40
	queueCheckTailBytes = 8192
)

// checkNoise matches what differs between two runs of a check that fail the same way: durations, clock
// times and dates, memory addresses and temporary folders.
var checkNoise = lazyRegexp(`\d+(\.\d+)?\s?(ns|µs|us|ms|s|sec|secs|seconds)\b|\d{4}-\d{2}-\d{2}[T ]?|\b\d{1,2}:\d{2}(:\d{2})?(\.\d+)?\b|0x[0-9a-fA-F]+|(/tmp/|/var/folders/|\\Temp\\)\S*`)

// checkFailure is a digest of how a check failed and of the end of its output, with what differs from
// run to run left out, or "" when the check printed nothing to compare.
func checkFailure(outcome, tail string) string {
	if strings.TrimSpace(tail) == "" {
		return ""
	}
	return queueItemDigest(outcome + "\n" + checkNoise.ReplaceAllString(tail, "~"))
}

var (
	queueVerifyPattern     = lazyRegexp("(?i)^noctis-verify:[ \t]*`([^`]+)`$")
	queueVerifyEachPattern = lazyRegexp("(?i)^noctis-verify-each:[ \t]*`([^`]+)`$")
)

const (
	fullQueueCheck = "full"
	eachQueueCheck = "each"
)

func queueVerifyLine(content string) string {
	return queueCommandLine(content, queueVerifyPattern)
}

func queueVerifyEachLine(content string) string {
	return queueCommandLine(content, queueVerifyEachPattern)
}

func queueCommandLine(content string, pattern *lazyRe) string {
	fenced := false
	for _, raw := range strings.Split(strings.TrimPrefix(content, "\uFEFF"), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case queueFence(line):
			fenced = !fenced
		case fenced:
		default:
			if match := pattern.FindStringSubmatch(line); match != nil {
				return strings.TrimSpace(match[1])
			}
		}
	}
	return ""
}

func fileCheckCommands(cfg object, path, content string) (full, each string) {
	if path == "" || isAutoQueue(path) || !getBool(section(cfg, "queue"), "fileVerify", true) {
		return "", ""
	}
	if content == "" {
		content, _ = readQueueText(path)
	}
	full, each = queueVerifyLine(content), queueVerifyEachLine(content)
	if full == "" && each == "" {
		return "", ""
	}
	if trusted, _, _ := queueTrustRecordGap(path, content); !trusted {
		return "", ""
	}
	return full, each
}

func configuredCheckCommand(cfg object, key string) string {
	command, _ := section(cfg, "queue")[key].(string)
	return strings.TrimSpace(command)
}

func queueCheckCommandsOf(cfg object, path, content string) (full, each string) {
	full, each = fileCheckCommands(cfg, path, content)
	if full == "" {
		full = configuredCheckCommand(cfg, "verifyCommand")
	}
	if each == "" {
		each = configuredCheckCommand(cfg, "verifyEachCommand")
	}
	if full == "" || full == each {
		return each, ""
	}
	return full, each
}

func checkOrigin(command string, fileCommands ...string) string {
	switch {
	case command == "":
		return ""
	case slices.Contains(fileCommands, command):
		return "file"
	}
	return "config"
}

func checkCommandSetting(cfg object, command string) string {
	if configuredCheckCommand(cfg, "verifyCommand") == command {
		return "queue.verifyCommand"
	}
	return "queue.verifyEachCommand"
}

func queueCheckCommandOf(cfg object, path, content string) string {
	full, _ := queueCheckCommandsOf(cfg, path, content)
	return full
}

func queueCheckCommand(cfg object, path string) string {
	return queueCheckCommandOf(cfg, path, "")
}

func queueCheckSeconds(cfg object) float64 {
	seconds, ok := getNumber(section(cfg, "queue"), "verifyTimeoutSeconds")
	if !ok || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 1 {
		return 900
	}
	return math.Floor(math.Min(seconds, 86400))
}

func queueCheckAttempts(cfg object) float64 {
	attempts, ok := getNumber(section(cfg, "queue"), "verifyAttempts")
	if !ok || math.IsNaN(attempts) || math.IsInf(attempts, 0) || attempts < 1 {
		return 2
	}
	return math.Floor(attempts)
}

func queueCheckRecord(state object, path string) object {
	return getMap(getMap(state, "queueVerify"), queueTrustKey(path))
}

func queueHeld(cfg, state object, path string) bool {
	return numberOr(queueCheckRecord(state, path), "held", 0) > 0 && queueCheckCommand(cfg, path) != ""
}

func recordedCheckCommand(cfg, record object, path, content string) string {
	full, each := queueCheckCommandsOf(cfg, path, content)
	if each != "" && getString(record, "tier") == eachQueueCheck {
		return each
	}
	return full
}

func queueHeldBack(cfg, state object, sid, path string) bool {
	record := queueCheckRecord(state, path)
	if !queueHeld(cfg, state, path) || numberOr(record, "rerun", 0) > 0 {
		return false
	}
	if getMap(getMap(state, "stopGuard"), sid) != nil {
		updateState(func(next object) { delete(stateMap(next, "stopGuard"), sid) })
	}
	command := recordedCheckCommand(cfg, record, path, "")
	journal(sid, "Stop", "allow-stop", "queue held", object{"command": command})
	logInfo("queue %s held until %q passes; stop allowed for %s", path, command, sid)
	return true
}

func rearmQueueCheck(cfg, input object, sid string) {
	if len(getMap(readState(), "queueVerify")) == 0 {
		return
	}
	path := drivenQueueFile(cfg, nil, input, sid)
	if path == "" || !queueHeld(cfg, readState(), path) {
		return
	}
	key := queueTrustKey(path)
	updateState(func(next object) {
		if record := getMap(getMap(next, "queueVerify"), key); record != nil {
			record["rerun"] = float64(nowSec())
			record["at"] = float64(nowSec())
		}
	})
	logInfo("queue %s held: %q runs again at the next stop of %s", path, recordedCheckCommand(cfg, queueCheckRecord(readState(), path), path, ""), sid)
}

func queueTicks(content string) []any {
	ticked, seen := []any{}, map[string]bool{}
	entries, _ := parseQueueEntries(content)
	for _, entry := range entries {
		if digest := queueItemDigest(entry.text); entry.checked && !seen[digest] {
			seen[digest] = true
			ticked = append(ticked, digest)
		}
	}
	return ticked
}

func queueCheckDue(record object, ticked []any, ending bool) bool {
	if numberOr(record, "failures", 0) > 0 || numberOr(record, "rerun", 0) > 0 {
		return true
	}
	checked, eachChecked := digestSet(getList(record, "ticked")), digestSet(getList(record, "eachTicked"))
	waitingForFullCheck := false
	for _, digest := range ticked {
		text, _ := digest.(string)
		if !checked[text] && !eachChecked[text] {
			return true
		}
		waitingForFullCheck = waitingForFullCheck || !checked[text]
	}
	return ending && waitingForFullCheck
}

func queueChecksPending(record object, ticked []any) []any {
	checked := digestSet(getList(record, "ticked"))
	pending := []any{}
	for _, digest := range ticked {
		if text, _ := digest.(string); !checked[text] {
			pending = append(pending, digest)
		}
	}
	return pending
}

func queueCheckTier(cfg, record object, ticked []any, hasEach, ending bool) string {
	switch {
	case !hasEach || ending:
		return fullQueueCheck
	case numberOr(record, "failures", 0) > 0:
		if getString(record, "tier") == eachQueueCheck {
			return eachQueueCheck
		}
		return fullQueueCheck
	}
	if every := queueFullCheckEvery(cfg); every > 0 && float64(len(queueChecksPending(record, ticked))) >= every {
		return fullQueueCheck
	}
	return eachQueueCheck
}

func queueFullCheckEvery(cfg object) float64 {
	return numberOr(section(cfg, "queue"), "verifyFullEvery", 5)
}

func markEachTier(fields object, tier string) {
	if tier == eachQueueCheck {
		fields["tier"] = eachQueueCheck
	}
}

func queueCheckTree(cfg object, folder, path string) string {
	if !getBool(section(cfg, "queue"), "verifySkipUnchanged", true) {
		return ""
	}
	return worktreeFingerprint(folder, path)
}

func queueCheckPassedOn(record object, tier, tree string) bool {
	if numberOr(record, "failures", 0) > 0 || numberOr(record, "rerun", 0) > 0 {
		return false
	}
	if tier == eachQueueCheck {
		return getString(record, "eachTree") == tree
	}
	return getString(record, "tree") == tree
}

func queueCheckPass(record object, tier string, ticked []any, tree string, now float64) object {
	if tier == fullQueueCheck {
		passed := object{"ticked": ticked, "at": now}
		if tree != "" {
			passed["tree"] = tree
		}
		return passed
	}
	checked := getList(record, "ticked")
	if checked == nil {
		checked = []any{}
	}
	passed := object{"ticked": checked, "eachTicked": queueChecksPending(record, ticked), "at": now}
	if tree != "" {
		passed["eachTree"] = tree
	}
	if fullTree := getString(record, "tree"); fullTree != "" {
		passed["tree"] = fullTree
	}
	return passed
}

func storeEscalatedCheck(key, sid string, record object, now int64) {
	updateState(func(next object) {
		stateMap(next, "queueVerify")[key] = record
		delete(stateMap(next, "userTurns"), sid)
		countEscalation(next, now)
	})
}

func storeQueueCheck(key string, record object) {
	updateState(func(next object) { stateMap(next, "queueVerify")[key] = record })
}

func checkOutputText(tail string) string {
	if tail == "" {
		return "It printed nothing."
	}
	return "The end of its output:\n" + tail
}

func queueUncheckedIssues(cfg, state object, path, content string) map[string]bool {
	unchecked := map[string]bool{}
	if observing || queueCheckCommandOf(cfg, path, content) == "" {
		return unchecked
	}
	passed := digestSet(getList(queueCheckRecord(state, path), "ticked"))
	entries, _ := parseQueueEntries(content)
	for _, entry := range entries {
		if issue, found := itemIssue(entry.text); found && entry.checked && !passed[queueItemDigest(entry.text)] {
			unchecked[issue.ref()] = true
		}
	}
	return unchecked
}

func queueFolder(cfg, input object, sid, path string) string {
	if isAutoQueue(path) {
		if project := claudeProjectDir(); project != "" {
			return project
		}
		return orDefault(getString(getMap(getMap(readState(), "autoQueues"), sid), "cwd"), getString(input, "cwd"))
	}
	for _, dir := range queueDirs(input) {
		for _, name := range queueFileNames(cfg) {
			if dir != "" && filepath.Join(dir, name) == path {
				return dir
			}
		}
	}
	return filepath.Dir(path)
}

// queueCheckReserve is the time a wait in place in the Stop hook leaves free for the queue check.
func queueCheckReserve(kind string, cfg, input object) float64 {
	if kind != "stop" || queueCheckCommand(cfg, drivenQueueFile(cfg, nil, input, sessionKey(input))) == "" {
		return 0
	}
	return queueCheckSeconds(cfg)
}

func queueCheckLimit(cfg object, started int64) float64 {
	limit, elapsed := queueCheckSeconds(cfg), float64(nowSec()-started)+hookWaited
	if learned := numberOr(readState(), "hookCapSeconds", 0); learned > 0 {
		limit = math.Min(limit, learned-60-elapsed)
	}
	if budget, known := hookBudget(activeHost, activeEvent); known {
		limit = math.Min(limit, budget-hookBudgetSlackSeconds-elapsed)
	}
	return math.Max(1, limit)
}

type tailBuffer struct {
	data []byte
}

func (tail *tailBuffer) Write(chunk []byte) (int, error) {
	tail.data = append(tail.data, chunk...)
	if len(tail.data) > 2*queueCheckTailBytes {
		tail.data = tail.data[:copy(tail.data, tail.data[len(tail.data)-queueCheckTailBytes:])]
	}
	return len(chunk), nil
}

func (tail *tailBuffer) text() string {
	data := tail.data
	if len(data) > queueCheckTailBytes {
		data = data[len(data)-queueCheckTailBytes:]
		if cut := bytes.IndexByte(data, '\n'); cut >= 0 {
			data = data[cut+1:]
		}
	}
	text := strings.ToValidUTF8(strings.ReplaceAll(string(data), "\r", ""), "")
	lines := strings.Split(strings.TrimRight(text, "\n\t "), "\n")
	if len(lines) > queueCheckTailLines {
		lines = lines[len(lines)-queueCheckTailLines:]
	}
	return strings.Join(lines, "\n")
}

// runQueueCheck runs the check line in folder for at most limit seconds. It returns how the check
// failed ("" when it passed), the end of its output, and whether it was cut short rather than failed:
// stopped at the limit, killed by a signal, or ended with the status of a timeout (124) or of a kill
// (137, as when the system runs out of memory).
func runQueueCheck(line, folder string, limit float64) (string, string, bool) {
	command := platformShell(line)
	command.Dir = folder
	tail := &tailBuffer{}
	command.Stdout, command.Stderr = tail, tail
	isolateTree(command)
	err := waitUntil(command, time.Duration(limit*float64(time.Second)), func() { killTree(command.Process) })
	var exit *exec.ExitError
	switch {
	case err == nil:
		return "", tail.text(), false
	case errors.Is(err, errTimedOut):
		return fmt.Sprintf("did not finish within %s s and was stopped", formatNumber(limit)), tail.text(), true
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
		status := exit.ExitCode()
		return fmt.Sprintf("exited with status %d", status), tail.text(), status == 124 || status == 137
	case errors.As(err, &exit):
		return "failed: " + err.Error(), tail.text(), true
	}
	return "failed: " + err.Error(), tail.text(), false
}

func gateQueue(cfg, input object, sid, path, content, label string, snapshot queueView, started int64) object {
	full, each := queueCheckCommandsOf(cfg, path, content)
	if full == "" {
		return nil
	}
	record := queueCheckRecord(readState(), path)
	ticked := queueTicks(content)
	ending := len(snapshot.items) == 0
	if !queueCheckDue(record, ticked, ending) {
		return nil
	}
	tier := queueCheckTier(cfg, record, ticked, each != "", ending)
	command := full
	if tier == eachQueueCheck {
		command = each
	}
	if observed(sid, "Stop", "verify-queue", command, nil) {
		return nil
	}
	folder := queueFolder(cfg, input, sid, path)
	now, key := float64(nowSec()), queueTrustKey(path)
	facts := object{"command": command}
	markEachTier(facts, tier)
	tree := queueCheckTree(cfg, folder, path)
	if tree != "" && queueCheckPassedOn(record, tier, tree) {
		storeQueueCheck(key, queueCheckPass(record, tier, ticked, tree, now))
		facts["skipped"] = true
		journal(sid, "Stop", "verify-queue", "skipped: the working tree is as it was when it last passed", facts)
		logInfo("queue check %q for %s skipped: the working tree is as it was when it last passed", command, sid)
		if tier == fullQueueCheck {
			syncDoneIssues(cfg, path, content, getString(input, "cwd"))
		}
		return nil
	}
	began := time.Now()
	outcome, tail, cut := runQueueCheck(command, folder, queueCheckLimit(cfg, started))
	facts["seconds"] = math.Round(time.Since(began).Seconds())
	if cut && numberOr(record, "failures", 0) == 0 && numberOr(record, "held", 0) == 0 && numberOr(record, "retried", 0) == 0 {
		took := time.Since(began).Seconds()
		switch {
		case queueCheckLimit(cfg, started) >= took+1:
			journal(sid, "Stop", "verify-queue", "cut short: "+outcome, facts)
			logInfo("queue check %q for %s %s; it runs once more now, not counted as a failed attempt", command, sid, outcome)
			again := time.Now()
			outcome, tail, cut = runQueueCheck(command, folder, queueCheckLimit(cfg, started))
			facts["seconds"] = math.Round(time.Since(again).Seconds())
		case !ending:
			updateState(func(next object) {
				stored := object{}
				for name, value := range queueCheckRecord(next, path) {
					stored[name] = value
				}
				if numberOr(stored, "at", 0) == 0 {
					stored["at"] = now
				}
				stored["retried"], stored["rerun"] = now, now
				markEachTier(stored, tier)
				stateMap(next, "queueVerify")[key] = stored
			})
			journal(sid, "Stop", "verify-queue", "cut short: "+outcome, facts)
			logInfo("queue check %q for %s %s; it runs once more at the next stop, not counted as a failed attempt", command, sid, outcome)
			return nil
		}
	}
	if outcome == "" {
		storeQueueCheck(key, queueCheckPass(record, tier, ticked, tree, now))
		journal(sid, "Stop", "verify-queue", "passed", facts)
		logInfo("queue check %q passed for %s in %s", command, sid, folder)
		if tier == fullQueueCheck {
			syncDoneIssues(cfg, path, content, getString(input, "cwd"))
		}
		return nil
	}
	failures := numberOr(record, "failures", 0) + 1
	facts["failures"] = failures
	kept := getList(record, "ticked")
	if kept == nil {
		kept = []any{}
	}
	stored := object{"ticked": kept, "failures": failures, "at": now}
	markEachTier(stored, tier)
	if escalated := numberOr(record, "escalated", 0); escalated > 0 {
		stored["escalated"] = escalated
	}
	same := false
	if failed := checkFailure(outcome, tail); failed != "" && !cut {
		stored["failed"] = failed
		same = failures > 1 && failures < queueCheckAttempts(cfg) && getString(record, "failed") == failed
	}
	if same {
		facts["same"] = true
	}
	if !same && failures < queueCheckAttempts(cfg) {
		updateState(func(next object) {
			stateMap(next, "queueVerify")[key] = stored
			delete(stateMap(next, "userTurns"), sid)
		})
		journal(sid, "Stop", "verify-queue", outcome, facts)
		logInfo("queue check %q for %s %s (%s in a row); Claude is sent back to fix it", command, sid, outcome, formatNumber(failures))
		last := ""
		if model, _ := checkFixEscalation(cfg, record, sid, int64(now)); model == "" && failures+1 >= queueCheckAttempts(cfg) {
			last = queueCheckLastRule(path)
		}
		return object{"decision": "block", "reason": fmt.Sprintf("[noctis] Queue check failed: `%s` %s (run in %s). Fix the failure before you start another item, and leave the item you were on unticked until the command passes (untick it if you already marked it done); it runs again when you stop.%s Do not ask for confirmation; decide yourself. %s", command, outcome, folder, last, checkOutputText(tail))}
	}
	if model, agent := checkFixEscalation(cfg, record, sid, int64(now)); model != "" {
		stored["escalated"] = now
		storeEscalatedCheck(key, sid, stored, int64(now))
		facts["escalateTo"], facts["escalated"] = model, true
		journal(sid, "Stop", "verify-queue", outcome+"; the fix goes once to "+model, facts)
		logInfo("queue check %q for %s %s (%s in a row); the fix goes once to %s (%s) before the queue is held", command, sid, outcome, formatNumber(failures), model, agent)
		repeated := ""
		if same {
			repeated = " Its last two runs failed with the same output: the fixes so far changed nothing the check sees."
		}
		reason := fmt.Sprintf("[noctis] Queue check failed again: `%s` %s (run in %s), %s time(s) in a row.%s Hand the fix once to %s It runs again when you stop.%s Do not ask for confirmation; decide yourself. %s", command, outcome, folder, formatNumber(failures), repeated, handOff(escalationAgentText(model, agent), "the command, its output verbatim, what you changed and tried and why it did not work, the files involved"), queueCheckLastRule(path), checkOutputText(tail))
		return object{"decision": "block", "reason": reason, "systemMessage": T("queue.escalateCheck", truncateText(command, 120), int(failures), escalationModelTitle(model, agent))}
	}
	stored["held"] = now
	updateState(func(next object) {
		stateMap(next, "queueVerify")[key] = stored
		delete(stateMap(next, "stopGuard"), sid)
	})
	journal(sid, "Stop", "hold-queue", outcome, facts)
	fail("queue check %q failed %s time(s) in a row for %s (%s); %s held until it passes, stop allowed", command, formatNumber(failures), sid, outcome, path)
	if same {
		logInfo("queue check %q for %s failed with the same output as its last run; held before queue.verifyAttempts (%s)", command, sid, formatNumber(queueCheckAttempts(cfg)))
	}
	shown := truncateText(command, 120)
	if fileFull, fileEach := fileCheckCommands(cfg, path, content); checkOrigin(command, fileFull, fileEach) == "file" {
		notify(cfg, pluginName, T("queue.heldNotifyFile", int(failures), label))
	} else {
		notify(cfg, pluginName, T("queue.heldNotify", int(failures), label, checkCommandSetting(cfg, command)))
	}
	message := T("queue.heldMessage", shown, int(failures), label)
	if same {
		message += " " + T("queue.heldSame")
	}
	return object{"systemMessage": message}
}

func queueUnverified(cfg, state object, path, content string) (unverified, ticked int) {
	passed, passedEach := map[string]bool{}, map[string]bool{}
	if queueCheckCommandOf(cfg, path, content) != "" {
		record := queueCheckRecord(state, path)
		passed, passedEach = digestSet(getList(record, "ticked")), digestSet(getList(record, "eachTicked"))
	}
	for _, digest := range queueTicks(content) {
		ticked++
		if text, _ := digest.(string); !passed[text] && !passedEach[text] {
			unverified++
		}
	}
	return unverified, ticked
}

// unverifiedText says how many ticked items of the queue at path passed no check, or "" when all did.
func unverifiedText(cfg, state object, path, content string) string {
	unverified, ticked := queueUnverified(cfg, state, path, content)
	switch {
	case unverified == 0:
		return ""
	case queueCheckCommandOf(cfg, path, content) == "":
		return T("queue.unverifiedNone", unverified)
	}
	return T("queue.unverifiedWait", unverified, ticked, pluginName)
}

func suggestedCheck(cfg object, path, content string) string {
	if isAutoQueue(path) || queueVerifyLine(content) != "" || queueVerifyEachLine(content) != "" || queueCheckCommandOf(cfg, path, content) != "" || !getBool(section(cfg, "queue"), "fileVerify", true) {
		return ""
	}
	return projectCheckCommand(filepath.Dir(path))
}
