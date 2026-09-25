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

func queueCheckCommand(cfg object) string {
	command, _ := section(cfg, "queue")["verifyCommand"].(string)
	return strings.TrimSpace(command)
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
	return queueCheckCommand(cfg) != "" && numberOr(queueCheckRecord(state, path), "held", 0) > 0
}

func queueHeldBack(cfg, state object, sid, path string) bool {
	if !queueHeld(cfg, state, path) || numberOr(queueCheckRecord(state, path), "rerun", 0) > 0 {
		return false
	}
	if getMap(getMap(state, "stopGuard"), sid) != nil {
		updateState(func(next object) { delete(stateMap(next, "stopGuard"), sid) })
	}
	journal(sid, "Stop", "allow-stop", "queue held", object{"command": queueCheckCommand(cfg)})
	logInfo("queue %s held until %q passes; stop allowed for %s", path, queueCheckCommand(cfg), sid)
	return true
}

func rearmQueueCheck(cfg, input object, sid string) {
	if queueCheckCommand(cfg) == "" {
		return
	}
	path := sessionQueueFile(cfg, sid, queueDirs(input)...)
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
	logInfo("queue %s held: %q runs again at the next stop of %s", path, queueCheckCommand(cfg), sid)
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
	if queueCheckCommand(cfg) == "" || observing {
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

func queueCheckReserve(kind string, cfg object) float64 {
	if kind != "stop" || queueCheckCommand(cfg) == "" {
		return 0
	}
	return queueCheckSeconds(cfg)
}

func queueCheckLimit(cfg object, started int64) float64 {
	limit, elapsed := queueCheckSeconds(cfg), float64(nowSec()-started)
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

func runQueueCheck(line, folder string, limit float64) (string, string) {
	command := platformShell(line)
	command.Dir = folder
	tail := &tailBuffer{}
	command.Stdout, command.Stderr = tail, tail
	isolateTree(command)
	err := waitUntil(command, time.Duration(limit*float64(time.Second)), func() { killTree(command.Process) })
	var exit *exec.ExitError
	switch {
	case err == nil:
		return "", tail.text()
	case errors.Is(err, errTimedOut):
		return fmt.Sprintf("did not finish within %s s and was stopped", formatNumber(limit)), tail.text()
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
		return fmt.Sprintf("exited with status %d", exit.ExitCode()), tail.text()
	}
	return "failed: " + err.Error(), tail.text()
}

func gateQueue(cfg, input object, sid, path, content, label string, started int64) object {
	command := queueCheckCommand(cfg)
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
	outcome, tail := runQueueCheck(command, folder, queueCheckLimit(cfg, started))
	facts := object{"command": command, "seconds": math.Round(time.Since(began).Seconds())}
	now, key := float64(nowSec()), queueTrustKey(path)
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
	if failures < queueCheckAttempts(cfg) {
		updateState(func(next object) { stateMap(next, "queueVerify")[key] = stored })
		journal(sid, "Stop", "verify-queue", outcome, facts)
		logInfo("queue check %q for %s %s (%s in a row); Claude is sent back to fix it", command, sid, outcome, formatNumber(failures))
		output := "It printed nothing."
		if tail != "" {
			output = "The end of its output:\n" + tail
		}
		return object{"decision": "block", "reason": fmt.Sprintf("[noctis] Queue check failed: `%s` %s (run in %s). Fix the failure before you start another item, and leave the item you were on unticked until the command passes (untick it if you already marked it done); it runs again when you stop. Do not ask for confirmation; decide yourself. %s", command, outcome, folder, output)}
	}
	stored["held"] = now
	updateState(func(next object) {
		stateMap(next, "queueVerify")[key] = stored
		delete(stateMap(next, "stopGuard"), sid)
	})
	journal(sid, "Stop", "hold-queue", outcome, facts)
	fail("queue check %q failed %s time(s) in a row for %s (%s); %s held until it passes, stop allowed", command, formatNumber(failures), sid, outcome, path)
	shown := truncateText(command, 120)
	notify(cfg, pluginName, T("queue.heldNotify", shown, int(failures), label))
	return object{"systemMessage": T("queue.heldMessage", shown, int(failures), label)}
}
