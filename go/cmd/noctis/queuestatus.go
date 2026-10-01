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

func printQueueHold(cfg object, target string) {
	state := readState()
	if !queueHeld(cfg, state, target) {
		return
	}
	record := queueCheckRecord(state, target)
	fmt.Println(T("queue.statusHeld", formatTime(numberOr(record, "held", 0)), printableItem(truncateText(recordedCheckCommand(cfg, record, target, ""), 120)), int(numberOr(record, "failures", 0))))
}

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
	full, each := queueCheckCommandsOf(cfg, target, content)
	fileFull, fileEach := fileCheckCommands(cfg, target, content)
	check := object{"command": full, "fileLine": queueVerifyLine(content), "from": checkOrigin(full, fileFull, fileEach), "held": queueHeld(cfg, state, target)}
	check["eachCommand"], check["eachFileLine"], check["eachFrom"] = each, queueVerifyEachLine(content), checkOrigin(each, fileEach)
	if each != "" {
		check["fullEvery"] = queueFullCheckEvery(cfg)
	}
	record := queueCheckRecord(state, target)
	if held := numberOr(record, "held", 0); held > 0 && check["held"] == true {
		check["heldAt"] = held
	}
	check["failures"] = numberOr(record, "failures", 0)
	unverified, ticked := queueUnverified(cfg, state, target, content)
	check["unverified"], check["ticked"] = unverified, ticked
	if suggested := suggestedCheck(cfg, target, content); suggested != "" {
		check["suggested"] = suggested
	}
	facts["check"] = check
	facts["pace"] = paceFacts(queuePaceNow(cfg, state, target, view))
	facts["models"] = setupFacts(state, target)
	facts["decisions"] = queueNotesFacts(target)
	return facts
}

func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func verifyQueueNow(cfg object, cwd, target string) int {
	content, _ := readQueueText(target)
	command := queueCheckCommandOf(cfg, target, content)
	if command == "" {
		fmt.Fprintln(os.Stderr, T("queue.verifyNone", filepath.Base(target)))
		return 1
	}
	folder := queueFolder(cfg, object{"cwd": cwd}, "", target)
	shown := printableItem(truncateText(command, 120))
	tree := queueCheckTree(cfg, folder, target)
	began := time.Now()
	outcome, tail, _ := runQueueCheck(command, folder, queueCheckSeconds(cfg))
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
		stateMap(next, "queueVerify")[key] = queueCheckPass(nil, fullQueueCheck, ticked, tree, float64(nowSec()))
	})
	syncDoneIssues(cfg, target, content, cwd)
	if held {
		fmt.Println(T("queue.verifyReleased", filepath.Base(target)))
	}
	return 0
}
