package main

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
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

// queueVerifyPattern is the line that names a queue file's own check command: noctis-verify: `command`,
// on a line of its own.
var queueVerifyPattern = lazyRegexp("(?i)^noctis-verify:[ \t]*`([^`]+)`$")

// queueVerifyLine is the command the first noctis-verify line of content names, outside code fences.
func queueVerifyLine(content string) string {
	fenced := false
	for _, raw := range strings.Split(strings.TrimPrefix(content, "\uFEFF"), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case queueFence(line):
			fenced = !fenced
		case fenced:
		default:
			if match := queueVerifyPattern.FindStringSubmatch(line); match != nil {
				return strings.TrimSpace(match[1])
			}
		}
	}
	return ""
}

// fileCheckCommand is the check command the queue file at path names for itself, or "". It counts
// only while the user's trust (noctis queue trust) covers the file as content reads it, also with
// queue.requireTrust off, since the command runs without a permission prompt; a checklist noctis
// wrote never names one, and queue.fileVerify false turns the lines off. content "" reads the file.
func fileCheckCommand(cfg object, path, content string) string {
	if path == "" || isAutoQueue(path) || !getBool(section(cfg, "queue"), "fileVerify", true) {
		return ""
	}
	if content == "" {
		content, _ = readQueueText(path)
	}
	command := queueVerifyLine(content)
	if command == "" {
		return ""
	}
	if trusted, _, _ := queueTrustRecordGap(path, content); !trusted {
		return ""
	}
	return command
}

// queueCheckCommandOf is the command that checks the queue at path between items: its file's own
// (fileCheckCommand), else queue.verifyCommand.
func queueCheckCommandOf(cfg object, path, content string) string {
	if command := fileCheckCommand(cfg, path, content); command != "" {
		return command
	}
	command, _ := section(cfg, "queue")["verifyCommand"].(string)
	return strings.TrimSpace(command)
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

func queueHeldBack(cfg, state object, sid, path string) bool {
	if !queueHeld(cfg, state, path) || numberOr(queueCheckRecord(state, path), "rerun", 0) > 0 {
		return false
	}
	if getMap(getMap(state, "stopGuard"), sid) != nil {
		updateState(func(next object) { delete(stateMap(next, "stopGuard"), sid) })
	}
	command := queueCheckCommand(cfg, path)
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
	logInfo("queue %s held: %q runs again at the next stop of %s", path, queueCheckCommand(cfg, path), sid)
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

func queueCheckDue(record object, ticked []any) bool {
	if numberOr(record, "failures", 0) > 0 || numberOr(record, "rerun", 0) > 0 {
		return true
	}
	known := map[string]bool{}
	for _, digest := range getList(record, "ticked") {
		if text, ok := digest.(string); ok {
			known[text] = true
		}
	}
	for _, digest := range ticked {
		if text, _ := digest.(string); !known[text] {
			return true
		}
	}
	return false
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

func gateQueue(cfg, input object, sid, path, content, label string, started int64) object {
	command := queueCheckCommandOf(cfg, path, content)
	if command == "" {
		return nil
	}
	record := queueCheckRecord(readState(), path)
	ticked := queueTicks(content)
	if !queueCheckDue(record, ticked) || observed(sid, "Stop", "verify-queue", command, nil) {
		return nil
	}
	folder := queueFolder(cfg, input, sid, path)
	began := time.Now()
	outcome, tail, cut := runQueueCheck(command, folder, queueCheckLimit(cfg, started))
	facts := object{"command": command, "seconds": math.Round(time.Since(began).Seconds())}
	now, key := float64(nowSec()), queueTrustKey(path)
	if cut && numberOr(record, "failures", 0) == 0 && numberOr(record, "held", 0) == 0 && numberOr(record, "retried", 0) == 0 {
		// A check cut short says nothing about the code, so the first run of a streak that is cut short
		// runs once more before it counts as a failed attempt: here when the hook has the time for a
		// second run as long as the first, else at the next stop while Claude goes on with the next
		// item. With no item to go on with, the queue would end unchecked, so the run counts.
		took := time.Since(began).Seconds()
		switch {
		case queueCheckLimit(cfg, started) >= took+1:
			journal(sid, "Stop", "verify-queue", "cut short: "+outcome, facts)
			logInfo("queue check %q for %s %s; it runs once more now, not counted as a failed attempt", command, sid, outcome)
			again := time.Now()
			outcome, tail, cut = runQueueCheck(command, folder, queueCheckLimit(cfg, started))
			facts["seconds"] = math.Round(time.Since(again).Seconds())
		case len(queueSnapshotOf(path, content).items) > 0:
			updateState(func(next object) {
				stored := object{}
				for name, value := range queueCheckRecord(next, path) {
					stored[name] = value
				}
				// at dates the last pass, and keeps a new record from being pruned.
				if numberOr(stored, "at", 0) == 0 {
					stored["at"] = now
				}
				stored["retried"], stored["rerun"] = now, now
				stateMap(next, "queueVerify")[key] = stored
			})
			journal(sid, "Stop", "verify-queue", "cut short: "+outcome, facts)
			logInfo("queue check %q for %s %s; it runs once more at the next stop, not counted as a failed attempt", command, sid, outcome)
			return nil
		}
	}
	if outcome == "" {
		updateState(func(next object) { stateMap(next, "queueVerify")[key] = object{"ticked": ticked, "at": now} })
		journal(sid, "Stop", "verify-queue", "passed", facts)
		logInfo("queue check %q passed for %s in %s", command, sid, folder)
		syncDoneIssues(cfg, path, content, getString(input, "cwd"))
		return nil
	}
	failures := numberOr(record, "failures", 0) + 1
	facts["failures"] = failures
	kept := getList(record, "ticked")
	if kept == nil {
		kept = []any{}
	}
	stored := object{"ticked": kept, "failures": failures, "at": now}
	// A run that fails as the one before it did, output and all, shows the fix in between changed
	// nothing the check sees: the queue is held now rather than after queue.verifyAttempts. A run cut
	// short says nothing of the kind.
	same := false
	if failed := checkFailure(outcome, tail); failed != "" && !cut {
		stored["failed"] = failed
		same = failures > 1 && failures < queueCheckAttempts(cfg) && getString(record, "failed") == failed
	}
	if same {
		facts["same"] = true
	}
	if !same && failures < queueCheckAttempts(cfg) {
		// Claude goes back to work on noctis's word, not the user's: see noteUserTurn.
		updateState(func(next object) {
			stateMap(next, "queueVerify")[key] = stored
			delete(stateMap(next, "userTurns"), sid)
		})
		journal(sid, "Stop", "verify-queue", outcome, facts)
		logInfo("queue check %q for %s %s (%s in a row); Claude is sent back to fix it", command, sid, outcome, formatNumber(failures))
		output := "It printed nothing."
		if tail != "" {
			output = "The end of its output:\n" + tail
		}
		last := ""
		if failures+1 >= queueCheckAttempts(cfg) {
			last = queueCheckLastRule(path)
		}
		return object{"decision": "block", "reason": fmt.Sprintf("[noctis] Queue check failed: `%s` %s (run in %s). Fix the failure before you start another item, and leave the item you were on unticked until the command passes (untick it if you already marked it done); it runs again when you stop.%s Do not ask for confirmation; decide yourself. %s", command, outcome, folder, last, output)}
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
	if fileCheckCommand(cfg, path, content) == command {
		notify(cfg, pluginName, T("queue.heldNotifyFile", int(failures), label))
	} else {
		notify(cfg, pluginName, T("queue.heldNotify", int(failures), label))
	}
	message := T("queue.heldMessage", shown, int(failures), label)
	if same {
		message += " " + T("queue.heldSame")
	}
	return object{"systemMessage": message}
}

// queueUnverified counts the ticked items of the queue at path that passed no check: all of them when
// nothing checks the queue, else those ticked since its check last passed.
func queueUnverified(cfg, state object, path, content string) (unverified, ticked int) {
	passed := map[string]bool{}
	if queueCheckCommandOf(cfg, path, content) != "" {
		passed = digestSet(getList(queueCheckRecord(state, path), "ticked"))
	}
	for _, digest := range queueTicks(content) {
		ticked++
		if text, _ := digest.(string); !passed[text] {
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

// suggestedCheck is the command the project of the queue at path seems to check itself with, when
// nothing checks the queue yet and its file could name one, or "".
func suggestedCheck(cfg object, path, content string) string {
	if isAutoQueue(path) || queueVerifyLine(content) != "" || queueCheckCommandOf(cfg, path, content) != "" || !getBool(section(cfg, "queue"), "fileVerify", true) {
		return ""
	}
	return projectCheckCommand(filepath.Dir(path))
}
