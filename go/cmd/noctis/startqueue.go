package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const startedQueueMaxJobs = 500

var queueRule = lazyRegexp(`^(?:(?:-\s*){3,}|(?:\*\s*){3,}|(?:_\s*){3,})$`)

func queueCommandArgument(prompt string) string {
	fields := strings.Fields(prompt)
	if len(fields) == 0 {
		return ""
	}
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(prompt), fields[0]))
	if len(rest) >= 2 && (rest[0] == '"' || rest[0] == '\'') && rest[len(rest)-1] == rest[0] {
		rest = strings.TrimSpace(rest[1 : len(rest)-1])
	}
	return rest
}

func startedQueueSource(input object, name string) string {
	if strings.HasPrefix(name, "~/") || strings.HasPrefix(name, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			name = filepath.Join(home, name[2:])
		}
	}
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}
	dirs := queueDirs(input)
	for _, dir := range dirs {
		if candidate := filepath.Join(dir, name); dir != "" && statSafe(candidate) != nil {
			return candidate
		}
	}
	return filepath.Join(dirs[0], name)
}

func fileQueueEntries(content string) []queueEntry {
	if entries, _ := parseQueueEntries(content); len(entries) > 0 {
		return entries
	}
	entries := []queueEntry{}
	fenced, front := false, false
	for index, raw := range strings.Split(strings.TrimPrefix(content, "\uFEFF"), "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		switch {
		case index == 0 && line == "---":
			front = true
		case front:
			front = line != "---"
		case queueFence(line):
			fenced = !fenced
		case fenced, line == "", queueHeading.MatchString(line), queueRule.MatchString(line), strings.HasPrefix(line, "<!--"), strings.HasSuffix(line, ":"):
		default:
			entries = append(entries, newQueueEntry(len(entries)+1, line, queueDoneMarker.MatchString(line)))
		}
	}
	return entries
}

func onStartPrompt(input, cfg object, sid, notice string) {
	name := queueCommandArgument(getString(input, "prompt"))
	refuse := func(reason string) {
		journal(sid, "UserPromptSubmit", "refuse-start-queue", truncateText(name, 200), nil)
		logInfo("queue start for %s refused: %s", sid, reason)
		emit(object{"decision": "block", "reason": reason})
	}
	if !getBool(section(cfg, "queue"), "enabled", true) {
		refuse(T("queue.startOff", pluginName))
		return
	}
	if name == "" {
		refuse(T("queue.startUsage", pluginName))
		return
	}
	source := startedQueueSource(input, name)
	label := filepath.Base(source)
	info := statSafe(source)
	if info == nil || !info.Mode().IsRegular() {
		refuse(T("queue.startUnreadable", source))
		return
	}
	if info.Size() > queueMaxBytes {
		refuse(T("queue.startTooBig", label, startedQueueMaxJobs))
		return
	}
	data, err := os.ReadFile(source)
	if err != nil || bytes.IndexByte(data, 0) >= 0 {
		refuse(T("queue.startUnreadable", source))
		return
	}
	entries := fileQueueEntries(string(data))
	open := 0
	for _, entry := range entries {
		if !entry.checked {
			open++
		}
	}
	switch {
	case open == 0:
		refuse(T("queue.startEmpty", label))
		return
	case len(entries) > startedQueueMaxJobs:
		refuse(T("queue.startTooBig", label, startedQueueMaxJobs))
		return
	}
	if observed(sid, "UserPromptSubmit", "start-queue", fmt.Sprintf("%d jobs from %s", open, source), nil) {
		return
	}
	now := nowSec()
	lines := []string{"# " + pluginName + " — jobs from " + source + " (started " + localISO(float64(now)) + ")", ""}
	for _, entry := range entries {
		box := "- [ ] "
		if entry.checked {
			box = "- [x] "
		}
		lines = append(lines, box+entry.text)
	}
	checklist := writeSessionQueue(sid, object{"cwd": getString(input, "cwd"), "at": float64(now), "items": float64(open), "source": source}, lines)
	if checklist == "" {
		return
	}
	rememberOpenIssues(cfg, checklist)
	resetIdleGuard(readState(), sid)
	journal(sid, "UserPromptSubmit", "start-queue", fmt.Sprintf("%d jobs from %s", open, source), nil)
	logInfo("queue for %s started from %s: %d open jobs in %s", sid, source, open, checklist)
	emit(object{
		"systemMessage":      joinNotices(notice, T("queue.startDetected", open, label, pluginName)),
		"hookSpecificOutput": object{"hookEventName": "UserPromptSubmit", "additionalContext": startedQueueDirective(source, checklist, open)},
	})
}

func onStopPrompt(input, cfg object, sid string) {
	record := getMap(getMap(readState(), "autoQueues"), sid)
	path := getString(record, "path")
	if record == nil || statSafe(path) == nil {
		endAutoQueue(sid, false)
		reason := T("queue.stopNone")
		if file := followedQueueFile(cfg, queueDirs(input)...); file != "" && queueNeedsTrust(cfg, file) {
			reason = T("queue.stopProjectFile", filepath.Base(file))
		} else if file != "" {
			reason = T("queue.stopProjectFileOpen", filepath.Base(file))
		}
		emit(object{"decision": "block", "reason": reason})
		return
	}
	total := int(numberOr(record, "items", 0))
	done := max(0, total-queueSnapshot(path).total)
	label := sessionQueueLabel(sid)
	endAutoQueue(sid, true)
	journal(sid, "UserPromptSubmit", "stop-queue", fmt.Sprintf("%d of %d done", done, total), nil)
	logInfo("queue of %s stopped by the user: %d of %d done", sid, done, total)
	emit(object{"decision": "block", "reason": T("queue.stopDone", done, total, label)})
}

func startedQueueDirective(source, path string, count int) string {
	return fmt.Sprintf(`[noctis] The user started a queue from %s: %d open job(s), copied as a checklist to %s. Work through them in order, mark each item "- [x]" in the checklist the moment it is done, and do not stop, summarize or ask for confirmation between jobs; the session continues until every job is ticked. %s stays as the user wrote it: read it for any context a job needs, but tick nothing in it.`, source, count, path, filepath.Base(source))
}

func sessionQueueDirective(sid, path string, count int) string {
	if source := getString(getMap(getMap(readState(), "autoQueues"), sid), "source"); source != "" {
		return startedQueueDirective(source, path, count)
	}
	return autoQueueDirective(path, count)
}

func sessionQueueLabel(sid string) string {
	if source := getString(getMap(getMap(readState(), "autoQueues"), sid), "source"); source != "" {
		return filepath.Base(source)
	}
	return T("queue.autoLabel")
}
