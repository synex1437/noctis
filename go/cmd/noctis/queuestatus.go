package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"
)

// printChangedItems lists the items and lines of a queue file added or changed since its last trust,
// each once.
func printChangedItems(changed []string) {
	shown := map[string]bool{}
	for _, text := range changed {
		if line := printableItem(text); !shown[line] {
			shown[line] = true
			fmt.Println("  " + line)
		}
	}
}

func queueDoneCount(content string) int {
	entries, _ := parseQueueEntries(content)
	done := 0
	for _, entry := range entries {
		if entry.checked && entry.text != "" {
			done++
		}
	}
	return done
}

// printQueueProgress says how far the queue at target is and which item a session takes next.
func printQueueProgress(target string, view queueView) {
	content, _ := readQueueText(target)
	fmt.Println(T("queue.statusCounts", filepath.Base(target), view.total, queueDoneCount(content)))
	if len(view.items) > 0 {
		fmt.Println("  " + T("queue.statusNext", printableItem(view.items[0])))
	}
	if view.blocked > 0 {
		fmt.Println("  " + T("queue.statusBlocked", view.blocked))
	}
}

// printQueueHold says that a failing check holds the queue at target, since when, and how to lift it.
func printQueueHold(cfg object, target string) {
	state := readState()
	if !queueHeld(cfg, state, target) {
		return
	}
	record := queueCheckRecord(state, target)
	fmt.Println(T("queue.statusHeld", formatTime(numberOr(record, "held", 0)), printableItem(truncateText(queueCheckCommand(cfg, target), 120)), int(numberOr(record, "failures", 0))))
}

// queueStatusFacts is noctis queue status --json: what the text form says, for a script that watches
// an unattended run.
func queueStatusFacts(cfg object, target string, view queueView) object {
	content, _ := readQueueText(target)
	trusted, changed, legacy := queueTrustGap(cfg, target)
	facts := object{"file": target, "trusted": trusted, "legacyTrust": legacy, "open": view.total, "done": queueDoneCount(content), "waiting": view.blocked, "human": view.human, "deferred": view.deferred}
	facts["changedSinceTrust"] = orEmpty(changed)
	facts["items"] = orEmpty(view.items)
	if len(view.items) > 0 {
		facts["next"] = view.items[0]
	}
	facts["humanItems"] = orEmpty(view.humanItems)
	deferred := []any{}
	for _, item := range view.deferredItems {
		entry := object{"text": item.text, "reason": item.reason}
		if item.until > 0 {
			entry["until"] = item.until
		}
		deferred = append(deferred, entry)
	}
	facts["deferredItems"] = deferred
	empty := []any{}
	for _, line := range view.empty {
		empty = append(empty, line)
	}
	facts["emptyLines"] = empty
	facts["unmatched"] = orEmpty(view.unmatched)
	state := readState()
	line, command := queueVerifyLine(content), queueCheckCommandOf(cfg, target, content)
	check := object{"command": command, "fileLine": line, "held": queueHeld(cfg, state, target)}
	switch {
	case command == "":
		check["from"] = ""
	case command == line:
		check["from"] = "file"
	default:
		check["from"] = "config"
	}
	record := queueCheckRecord(state, target)
	if held := numberOr(record, "held", 0); held > 0 && check["held"] == true {
		check["heldAt"] = held
	}
	check["failures"] = numberOr(record, "failures", 0)
	facts["check"] = check
	facts["pace"] = paceFacts(queuePaceNow(cfg, state, target, view))
	facts["decisions"] = queueNotesFacts(target)
	return facts
}

func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// verifyQueueNow runs the check of the queue at target now, in the folder the Stop hook runs it in,
// and on a pass records the ticked items as checked and lifts a hold: the user sees a fix work
// without a turn of Claude's, and the next session to start or stop is driven again. A failure
// changes nothing, so it does not count toward queue.verifyAttempts. It returns the exit status of
// noctis queue verify.
func verifyQueueNow(cfg object, cwd, target string) int {
	content, _ := readQueueText(target)
	command := queueCheckCommandOf(cfg, target, content)
	if command == "" {
		fmt.Fprintln(os.Stderr, T("queue.verifyNone", filepath.Base(target)))
		return 1
	}
	folder := queueFolder(cfg, object{"cwd": cwd}, "", target)
	shown := printableItem(truncateText(command, 120))
	began := time.Now()
	outcome, tail := runQueueCheck(command, folder, queueCheckSeconds(cfg))
	if outcome != "" {
		fmt.Println(T("queue.verifyFailed", shown, outcome, folder))
		if tail != "" {
			fmt.Println(tail)
		}
		return 1
	}
	fmt.Println(T("queue.verifyPassed", shown, folder, formatNumber(math.Round(time.Since(began).Seconds()))))
	held, key, ticked := queueHeld(cfg, readState(), target), queueTrustKey(target), queueTicks(content)
	updateState(func(next object) {
		stateMap(next, "queueVerify")[key] = object{"ticked": ticked, "at": float64(nowSec())}
	})
	syncDoneIssues(cfg, target, content, cwd)
	if held {
		fmt.Println(T("queue.verifyReleased", filepath.Base(target)))
	}
	return 0
}
