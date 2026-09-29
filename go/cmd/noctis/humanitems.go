package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// An item marked (human) or (insan) is the user's own work: signing up for an account, a payment, a
// review only a person can give. Claude never takes one, the items that wait for it stay blocked
// until the user ticks it, and once only such items are left the session stops and the user hears
// which are theirs, without the queue ending.

// humanItemsRule tells Claude how to treat the queue's (human) items, or is "" when it has none.
func humanItemsRule(view queueView) string {
	if view.human == 0 {
		return ""
	}
	return fmt.Sprintf(" %d item(s) marked (human) are the user's own work: skip them, and never tick, edit or remove them unless the user tells you in a prompt that one is done; the items that wait for them become eligible once they are ticked.", view.human)
}

// humanItemNames names a view's (human) items for Claude (note) and for the user (notice).
func humanItemNames(view queueView) (note, notice string) {
	shown := []string{}
	for _, text := range view.humanItems[:min(len(view.humanItems), queueUnmatchedNamed)] {
		shown = append(shown, `"`+truncateText(text, 80)+`"`)
	}
	note = strings.Join(shown, "; ")
	notice = note
	if more := view.human - len(shown); more > 0 {
		note, notice = fmt.Sprintf("%s and %d more", note, more), T("queue.unmatchedMore", note, more)
	}
	return note, notice
}

// stopForWaitingItems lets the session stop when every open item left is the user's, is deferred,
// or waits for one of those. The user hears it once per set of open items and deferrals; the queue
// stays, so ticking an item, or taking a deferred one up again, and typing a prompt goes on with
// the items that waited for it. When a deferral with an end holds an item that could go on then,
// the hook waits for it if it may wait that long (waitForDeferral); otherwise a wake resumes the
// session then (armQueueWake).
func stopForWaitingItems(cfg, state, input object, sid, queuePath, content, label string, view queueView, now int64) {
	if waitForDeferral(cfg, state, input, sid, queuePath, label, view, now) {
		return
	}
	key := fmt.Sprintf("waiting:%s:%s:%s:%s", sid, queuePath, openEntriesDigest(content), deferralDigest(view))
	told := getMap(state, "notified")[key] != nil
	updateState(func(next object) {
		delete(stateMap(next, "stopGuard"), sid)
		stateMap(next, "notified")[key] = float64(now)
	})
	journal(sid, "Stop", "allow-stop", "queue waits on the user", object{"open": view.total, "human": view.human, "deferred": view.deferred})
	logInfo("queue %s of %s waits on %d item(s) marked (human) and %d deferred, %d open item(s) blocked; stop allowed", queuePath, sid, view.human, view.deferred, view.blocked)
	wake := armQueueWake(cfg, input, sid, queuePath, label, view.freeAt, "")
	if told || observing {
		return
	}
	messages := []string{}
	if view.human > 0 {
		_, names := humanItemNames(view)
		if view.total == 0 {
			notify(cfg, pluginName, T("queue.humanNotify", view.human, label, names))
			messages = append(messages, T("queue.humanMessage", view.human, label, names))
		} else {
			notify(cfg, pluginName, T("queue.humanBlockedNotify", view.total, label, view.human, names))
			messages = append(messages, T("queue.humanBlockedMessage", view.total, view.human, names))
		}
	}
	if view.deferred > 0 {
		_, names := deferredNames(view)
		notify(cfg, pluginName, T("queue.deferredNotify", view.deferred, label, names))
		message := T("queue.deferredMessage", view.deferred, label, names, pluginName)
		switch {
		case wake <= 0:
		case getString(section(cfg, "resume"), "mode") == "none":
			message += " " + T("queue.deferredWakeTell", formatTime(wake))
		default:
			message += " " + T("queue.deferredWake", formatTime(wake))
		}
		messages = append(messages, message)
	}
	emit(object{"systemMessage": joinNotices(messages...)})
}

// noteUserTurn keeps whether the user typed the prompt of the turn that runs now: only then may
// Claude tick an item marked (human), because the user said it is done. noctis's own prompts, a
// queue continuation and a turn Claude Code wrote end it, so a session that runs unattended never
// ticks the user's items. It writes the state only when the answer changes.
func noteUserTurn(state object, sid, prompt string, now int64) {
	typed := !promptFromPlugin && !queueContinuationPrompt(prompt) && agentWrittenTurn(prompt) == ""
	if typed == (getMap(getMap(state, "userTurns"), sid) != nil) {
		return
	}
	updateState(func(next object) {
		if typed {
			stateMap(next, "userTurns")[sid] = object{"at": float64(now)}
		} else {
			delete(stateMap(next, "userTurns"), sid)
		}
	})
}

// openHumanItems counts the open (human) items of a queue by their text.
func openHumanItems(content string) map[string]int {
	entries, _ := parseQueueEntries(content)
	open := map[string]int{}
	for _, entry := range entries {
		if entry.human && !entry.checked {
			open[entry.text]++
		}
	}
	return open
}

// mayWriteQueueFile tells cheaply whether file can be a queue file: one named like the queue files,
// or a checklist in noctis's queues folder. Any other write goes ahead without a look at the state.
func mayWriteQueueFile(cfg object, file, cwd string) bool {
	if file == "" {
		return false
	}
	name := filepath.Base(file)
	for _, queue := range queueFileNames(cfg) {
		if strings.EqualFold(name, filepath.Base(queue)) {
			return true
		}
	}
	return writesInto(file, cwd, autoQueueDir())
}

// refuseHumanTicks refuses a file tool's write that would tick, change or remove an open (human)
// item of the queue that drives the session, unless the user typed the prompt of this turn.
func refuseHumanTicks(input, cfg object) bool {
	file, cwd := getString(getMap(input, "tool_input"), "file_path"), getString(input, "cwd")
	if !mayWriteQueueFile(cfg, file, cwd) {
		return false
	}
	sid := sessionKey(input)
	state := peekState()
	// While the user has noctis paused no queue drives the session, and a turn the user typed may
	// tick what the user says is done.
	if getMap(getMap(state, "userTurns"), sid) != nil || numberOr(state, "disabledUntil", 0) > float64(nowSec()) {
		return false
	}
	path := drivenQueueFile(cfg, state, input, sid)
	if path == "" || !writesTo(file, cwd, path) {
		return false
	}
	current, _ := readQueueText(path)
	before := openHumanItems(current)
	if len(before) == 0 {
		return false
	}
	after := openHumanItems(editedText(input, current))
	touched := []string{}
	for text, count := range before {
		if after[text] < count {
			touched = append(touched, text)
		}
	}
	if len(touched) == 0 {
		return false
	}
	applySessionLocale(cfg, state, sid)
	view := queueView{human: len(touched), humanItems: touched}
	note, notice := humanItemNames(view)
	if observed(sid, "PreToolUse", "deny-human-tick", note, nil) {
		return false
	}
	journal(sid, "PreToolUse", "deny-human-tick", truncateText(note, 200), nil)
	logInfo("denied a %s of %s that ticks or changes %d item(s) marked (human) in %s", getString(input, "tool_name"), sid, len(touched), path)
	emit(object{
		"systemMessage":      T("queue.humanTickByModel", filepath.Base(path), notice),
		"hookSpecificOutput": object{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": fmt.Sprintf("Items marked (human) in %s are the user's own work: only the user ticks them, in the file or by telling you in a prompt that one is done. This write would tick, change or remove %s. Leave them exactly as they are, do not change them another way, and go on with the other items; the items that wait for them become eligible once the user ticks them.", path, note)},
	})
	return true
}
