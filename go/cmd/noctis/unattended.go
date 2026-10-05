package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

const waitingNoticeGapSeconds = 600

var waitingNoticeKinds = map[string]bool{
	"permission_prompt":        true,
	"worker_permission_prompt": true,
	"elicitation_dialog":       true,
	"elicitation_url_dialog":   true,
}

func drivingQueue(cfg, state, input object, sid string) (string, queueView) {
	if numberOr(state, "disabledUntil", 0) > float64(nowSec()) {
		return "", queueView{}
	}
	path := drivenQueueFile(cfg, state, input, sid)
	if path == "" || queueHeld(cfg, state, path) {
		return "", queueView{}
	}
	if view, trusted := trustedQueueSnapshot(cfg, path); trusted && len(view.items) > 0 {
		return path, view
	}
	return "", queueView{}
}

func refuseQuestionInQueue(input, cfg object) {
	sid := sessionKey(input)
	path, view := drivingQueue(cfg, peekState(), input, sid)
	if path == "" {
		return
	}
	asked := orDefault(oneLine(firstQuestion(input), 200), filepath.Base(path))
	if observed(sid, "PreToolUse", "deny-question", asked, nil) {
		return
	}
	journal(sid, "PreToolUse", "deny-question", asked, object{"file": filepath.Base(path), "open": view.total})
	logInfo("denied a question of %s: the queue %s drives it (%d open)", sid, path, view.total)
	emit(object{"hookSpecificOutput": object{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": questionRefusal(path, view.total)}})
}

func firstQuestion(input object) string {
	for _, raw := range getList(getMap(input, "tool_input"), "questions") {
		if text := getString(toObject(raw), "question"); text != "" {
			return text
		}
	}
	return ""
}

func oneLine(text string, max int) string {
	return truncateText(printableItem(strings.Join(strings.Fields(text), " ")), max)
}

func questionRefusal(path string, open int) string {
	record := fmt.Sprintf(`note it in one line: %s queue note "<the decision, and why>" --file %s`, pluginName, shellQuote(path))
	if isAutoQueue(path) {
		record = "say in your reply what you decided and why"
	}
	return fmt.Sprintf(`[noctis] A queue drives this session (%s: %d open) and runs while the user is away, so a question would hold it until they come back; this one was not shown. Do not ask: decide yourself, take the reading that fits the item and the project best, go on, and %s. If only the user can settle it (an account, a payment, a key, a choice that is theirs alone), run %s queue defer <a unique part of the item's text> --reason "<what it waits on>" --file %s and go on with the next item.`, filepath.Base(path), open, record, pluginName, shellQuote(path))
}

func noticeWaitingForUser(input, cfg object, sid, kind string) {
	state := peekState()
	now := float64(nowSec())
	if numberOr(state, "disabledUntil", 0) > now {
		return
	}
	if !relaunchedByNoctis(state, sid) && numberOr(getMap(getMap(state, "waits"), sid), "wakeAttemptedAt", 0) <= 0 {
		if path, _ := drivingQueue(cfg, state, input, sid); path == "" {
			return
		}
	}
	key, fresh := "waiting:"+sid, false
	updateState(func(next object) {
		notified := stateMap(next, "notified")
		if now-numberOr(notified, key, 0) >= waitingNoticeGapSeconds {
			notified[key], fresh = now, true
		}
	})
	if !fresh {
		return
	}
	detail := oneLine(orDefault(getString(input, "message"), kind), 120)
	if observed(sid, "Notification", "waiting-notice", detail, object{"type": kind}) {
		return
	}
	journal(sid, "Notification", "waiting-notice", detail, object{"type": kind})
	logInfo("%s waits for the user (%s): notified", sid, kind)
	notify(cfg, pluginName, T("notification.waiting", filepath.Base(orDefault(getString(input, "cwd"), "?")), shortSid(sid), detail))
}
