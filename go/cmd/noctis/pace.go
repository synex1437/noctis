package main

import (
	"fmt"
	"math"
	"path/filepath"
)

// The pace of a queue file: whenever a session it drives stops with another number of items done
// than noctis noted last, the Stop hook notes the time, the items done and how much of the weekly
// limit is used then. From the items ticked within paceGapSeconds of the note before, noctis works
// out the time an item takes and its share of the weekly limit, and from those how much of the
// limit the items left need and when they may be done. noctis queue status and the daily digest say
// it, so a user who leaves a long job to run sees whether this week's limit covers it.

const (
	// paceNotes is how many notes a queue file keeps: its pace is that of its last items.
	paceNotes = 25
	// paceGapSeconds leaves out an item ticked longer than this after the note before: that time and
	// that use hold idle hours or other work. A wait for the five-hour limit fits in it.
	paceGapSeconds = 6 * 3600
	// paceMinItems is how many items the pace needs before it says anything.
	paceMinItems = 3
	// paceSameWindowSeconds: two readings whose weekly resets are this close are of one window.
	paceSameWindowSeconds = 3600
	// paceMaxLeft bounds the items the estimate of the finish goes through.
	paceMaxLeft = 10000
)

// paceNote is one note of a queue file: when, how many items were done, and the weekly window's use
// and reset then (used is -1 without a reading).
type paceNote struct {
	at, done, used, resetsAt float64
}

func paceNotesOf(record object) []paceNote {
	notes := []paceNote{}
	for _, raw := range getList(record, "notes") {
		values, ok := raw.([]any)
		if !ok || len(values) != 4 {
			continue
		}
		numbers := [4]float64{}
		valid := true
		for index, value := range values {
			numbers[index], ok = toNumber(value)
			valid = valid && ok
		}
		if valid {
			notes = append(notes, paceNote{at: numbers[0], done: numbers[1], used: numbers[2], resetsAt: numbers[3]})
		}
	}
	return notes
}

func noteQueuePace(cfg, state object, sid, path, content string, now int64) {
	if isAutoQueue(path) {
		return
	}
	key, done := queueTrustKey(path), float64(queueDoneCount(content))
	if notes := paceNotesOf(getMap(getMap(state, "queuePace"), key)); len(notes) > 0 && notes[len(notes)-1].done == done {
		return
	}
	used, resetsAt := -1.0, 0.0
	if week := currentUsage(now).sevenDay; week != nil {
		used, resetsAt = week.used, week.resetsAt
	}
	setup, facts := sessionSetup(cfg, state, sid), tickFactsOf(state, sid, path)
	sentBack := untickedForAFailedCheck(cfg, queueCheckRecord(state, path), path, content)
	updateState(func(next object) {
		paces := stateMap(next, "queuePace")
		kept := paceNotesOf(toObject(paces[key]))
		if len(kept) > 0 && kept[len(kept)-1].done == done {
			return
		}
		if len(kept) > 0 && kept[len(kept)-1].done > done {
			if sentBack {
				return
			}
			kept = nil
		}
		latest := paceNote{at: float64(now), done: done, used: used, resetsAt: resetsAt}
		if len(kept) > 0 {
			noteFinished(next, path, content, kept[len(kept)-1], latest, setup, now)
			noteTicks(next, path, content, kept[len(kept)-1], latest, setup, facts, now)
		}
		kept = append(kept, latest)
		notes := []any{}
		for _, note := range kept[max(0, len(kept)-paceNotes):] {
			notes = append(notes, []any{note.at, note.done, note.used, note.resetsAt})
		}
		paces[key] = object{"path": path, "at": float64(now), "notes": notes}
	})
}

// queuePace is the pace of a queue file and what it says about the items left.
type queuePace struct {
	// items were ticked within paceGapSeconds of the note before; weeklyItems of them between two
	// readings of one weekly window.
	items, weeklyItems int
	secondsPerItem     float64
	// weeklyPerItem is the share of the weekly limit, in percent, an item takes; -1 when unknown.
	weeklyPerItem float64
	left          int
	// weeklyNeeded is the share the items left need and weeklyRoom the share left before the weekly
	// pause point now; -1 when unknown.
	weeklyNeeded, weeklyRoom float64
	finishAt                 float64
}

// paceOf works out the pace of the queue file at path from its notes, and when its left items may
// be done.
func paceOf(cfg, state object, path string, left int, usage usageView, now int64) queuePace {
	pace := queuePace{left: left, weeklyPerItem: -1, weeklyNeeded: -1, weeklyRoom: -1}
	notes := paceNotesOf(getMap(getMap(state, "queuePace"), queueTrustKey(path)))
	seconds, used := 0.0, 0.0
	for index := 1; index < len(notes); index++ {
		before, after := notes[index-1], notes[index]
		ticked, gap := int(after.done-before.done), after.at-before.at
		if ticked <= 0 || gap <= 0 || gap > paceGapSeconds {
			continue
		}
		pace.items += ticked
		seconds += gap
		if before.used >= 0 && after.used >= 0 && math.Abs(after.resetsAt-before.resetsAt) < paceSameWindowSeconds {
			pace.weeklyItems += ticked
			used += math.Max(0, after.used-before.used)
		}
	}
	if pace.items < paceMinItems {
		return pace
	}
	pace.secondsPerItem = seconds / float64(pace.items)
	if pace.weeklyItems >= paceMinItems && used > 0 {
		pace.weeklyPerItem = used / float64(pace.weeklyItems)
		pace.weeklyNeeded = pace.weeklyPerItem * float64(left)
	}
	if week := usage.sevenDay; week != nil {
		pace.weeklyRoom = math.Max(0, weeklyPausePoint(cfg)-week.used)
	}
	pace.finishAt = paceFinish(cfg, pace, usage, now)
	return pace
}

func weeklyPausePoint(cfg object) float64 {
	if stop, guarded := stopPoint(cfg, "weeklyAll"); guarded {
		return stop
	}
	return 100
}

// paceFinish is when the items left may be done at this pace: one after the other, each taking its
// time and its share of the weekly limit, and after the weekly reset when the next one would pass
// the pause point.
func paceFinish(cfg object, pace queuePace, usage usageView, now int64) float64 {
	at := float64(now)
	week := usage.sevenDay
	pausePoint := weeklyPausePoint(cfg)
	if week == nil || pace.weeklyPerItem <= 0 || pace.weeklyPerItem > pausePoint {
		return at + pace.secondsPerItem*float64(pace.left)
	}
	used, resetsAt := week.used, week.resetsAt
	for item := 0; item < min(pace.left, paceMaxLeft); item++ {
		for at >= resetsAt {
			used, resetsAt = 0, resetsAt+7*86400
		}
		if used+pace.weeklyPerItem > pausePoint {
			at, used, resetsAt = resetsAt, 0, resetsAt+7*86400
		}
		at += pace.secondsPerItem
		used += pace.weeklyPerItem
	}
	return at
}

// paceLines says what an item of the queue takes and when its items left may be done, or nothing
// while the pace is not known.
func paceLines(pace queuePace) []string {
	if pace.items < paceMinItems {
		return nil
	}
	// An item ticked within a minute of the one before still reads as a minute.
	took := durationText(math.Max(60, pace.secondsPerItem))
	lines := []string{T("queue.paceTime", took, pace.items)}
	if pace.weeklyPerItem > 0 {
		lines[0] = T("queue.pace", took, pace.items, int(math.Round(100/pace.weeklyPerItem)))
	}
	switch {
	case pace.left == 0:
	case pace.weeklyNeeded >= 0 && pace.weeklyRoom >= 0:
		lines = append(lines, T("queue.paceLeft", pace.left, T("badge.percent", int(math.Max(1, math.Round(pace.weeklyNeeded)))), T("badge.percent", int(math.Round(pace.weeklyRoom))), formatTime(pace.finishAt)))
	default:
		lines = append(lines, T("queue.paceLeftTime", pace.left, formatTime(pace.finishAt)))
	}
	return lines
}

// paceFacts is the pace for noctis queue status --json, or nil while it is not known.
func paceFacts(pace queuePace) any {
	if pace.items < paceMinItems {
		return nil
	}
	facts := object{"items": float64(pace.items), "secondsPerItem": math.Round(pace.secondsPerItem), "left": float64(pace.left), "finishAt": math.Round(pace.finishAt)}
	if pace.weeklyPerItem > 0 {
		facts["weeklyPerItem"] = math.Round(pace.weeklyPerItem*100) / 100
		facts["weeklyNeeded"] = math.Round(pace.weeklyNeeded*10) / 10
	}
	if pace.weeklyRoom >= 0 {
		facts["weeklyRoom"] = math.Round(pace.weeklyRoom*10) / 10
	}
	return facts
}

// queuePaceNow is the pace of the queue file at path as it stands now.
func queuePaceNow(cfg, state object, path string, view queueView) queuePace {
	now := nowSec()
	return paceOf(cfg, state, path, view.total+view.deferred, currentUsage(now), now)
}

// printQueuePace prints the pace of the queue file at target for noctis queue status, or when it will
// be known once a session noted some of it.
func printQueuePace(cfg object, target string, view queueView) {
	state := readState()
	pace := queuePaceNow(cfg, state, target, view)
	lines := paceLines(pace)
	if len(lines) == 0 && len(paceNotesOf(getMap(getMap(state, "queuePace"), queueTrustKey(target)))) > 0 {
		lines = []string{T("queue.paceSoon", paceMinItems, paceGapSeconds/3600, pace.items)}
	}
	for _, line := range lines {
		fmt.Println("  " + line)
	}
}

// queuePaceStatus is the pace of each queue file the digest reports on, for noctis status: a line
// naming the file and its pace and setup lines under it, for the files with either.
func queuePaceStatus(cfg, state object, usage usageView, now int64) []string {
	lines := []string{}
	for _, path := range digestQueueFiles(state, now) {
		content, _ := readQueueText(path)
		view := queueSnapshotOf(path, content)
		pace := append(paceLines(paceOf(cfg, state, path, view.total+view.deferred, usage, now)), setupLines(state, path)...)
		if text := unverifiedText(cfg, state, path, content); text != "" {
			pace = append(pace, text)
		}
		if text := queueHoldText(cfg, state, path); text != "" {
			pace = append(pace, text)
		}
		if len(pace) == 0 {
			continue
		}
		lines = append(lines, T("status.queue", filepath.Base(path)))
		for _, line := range pace {
			lines = append(lines, "  "+line)
		}
	}
	return lines
}
