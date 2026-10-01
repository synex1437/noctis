package main

import (
	"fmt"
	"strings"
)

// After a compaction the summary is all Claude has of the work before it, and a summary can drop or
// misstate what matters most to go on: the item in hand, the files it changed and whether the check
// passed. So while a queue drives the session, the PreCompact hook counts the compactions on the item
// in hand, and the SessionStart hook that follows one hands Claude a short note of those facts, read
// from the queue file, git and noctis's record of the check rather than from the summary.

// compactNoteFiles is how many changed files the note names.
const compactNoteFiles = 12

// onPreCompact counts the compactions on the item in hand of the queue that drives the session. It
// never holds a compaction back.
func onPreCompact(input, cfg object) {
	sid := sessionKey(input)
	path := drivenQueueFile(cfg, peekState(), input, sid)
	if path == "" {
		return
	}
	// The same test as the SessionStart hook's, whose note the count is for.
	if trusted, _, _ := queueTrustGap(cfg, path); !trusted {
		return
	}
	content, _ := readQueueText(path)
	items := queueSnapshotOf(path, content).items
	if len(items) == 0 {
		return
	}
	digest, now, count := queueItemDigest(items[0]), float64(nowSec()), 0.0
	updateState(func(next object) {
		record := stateMap(stateMap(next, "queueCompactions"), queueTrustKey(path))
		if getString(record, "item") != digest {
			record["item"], record["count"] = digest, float64(0)
		}
		count = numberOr(record, "count", 0) + 1
		record["count"], record["at"] = count, now
	})
	trigger := orDefault(getString(input, "trigger"), "?")
	journal(sid, "PreCompact", "count-compaction", fmt.Sprintf("%s compaction %d on %s", trigger, int(count), truncateText(items[0], 80)), object{"count": count})
	logInfo("%s compaction %d of %s on %q", trigger, int(count), sid, truncateText(items[0], 80))
}

// itemCompactions is how many compactions the item in hand of the queue at path has seen.
func itemCompactions(state object, path, item string) int {
	record := getMap(getMap(state, "queueCompactions"), queueTrustKey(path))
	if getString(record, "item") != queueItemDigest(item) {
		return 0
	}
	return int(numberOr(record, "count", 0))
}

func compactRestoreNote(cfg, state, input object, sid, path string) string {
	content, _ := readQueueText(path)
	items := queueSnapshotOf(path, content).items
	if len(items) == 0 {
		return ""
	}
	item := items[0]
	parts := []string{fmt.Sprintf("[noctis] After the compaction: the summary above can drop or misstate details, so here is what noctis knows. The item in hand: %q.", printableItem(truncateText(item, 300)))}
	if raw, ok := gitStatusRaw(queueFolder(cfg, input, sid, path)); ok {
		files := statusPaths(raw)
		shown := []string{}
		for _, file := range files[:min(len(files), compactNoteFiles)] {
			shown = append(shown, truncateText(printableItem(file), 80))
		}
		switch {
		case len(files) == 0:
			parts = append(parts, "No file has changed since the last commit.")
		case len(files) > len(shown):
			parts = append(parts, fmt.Sprintf("Changed since the last commit: %s and %d more.", strings.Join(shown, ", "), len(files)-len(shown)))
		default:
			parts = append(parts, fmt.Sprintf("Changed since the last commit: %s.", strings.Join(shown, ", ")))
		}
	}
	record := queueCheckRecord(state, path)
	command := recordedCheckCommand(cfg, record, path, content)
	switch shown := "`" + truncateText(command, 120) + "`"; {
	case command == "":
		parts = append(parts, "Nothing checks this queue between items, so run the project's tests yourself before you tick the item.")
	case numberOr(record, "failures", 0) > 0:
		parts = append(parts, fmt.Sprintf("The queue's check (%s) failed at its last run: fix that first.", shown))
	case numberOr(record, "retried", 0) > 0:
		parts = append(parts, fmt.Sprintf("The queue's check (%s) was cut short at its last run; it runs again at the next stop.", shown))
	case numberOr(record, "at", 0) > 0:
		parts = append(parts, fmt.Sprintf("noctis runs the queue's check (%s) after you tick the item; it last passed at %s.", shown, formatTime(numberOr(record, "at", 0))))
	default:
		parts = append(parts, fmt.Sprintf("noctis runs the queue's check (%s) after you tick the item.", shown))
	}
	parts = append(parts, "Read a file again before you change it rather than going by the summary.")
	if count := itemCompactions(state, path, item); count >= 2 {
		parts = append(parts, fmt.Sprintf("This item has now gone through %d compactions: hand long test runs, big diffs and wide searches to a subagent, and finish the item in small steps.", count))
	}
	return strings.Join(parts, " ")
}
