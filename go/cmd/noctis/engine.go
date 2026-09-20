package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const bulletMarker = `[-*+•]`

var (
	queueItemPattern = lazyRegexp(`(?i)^\s*(?:(?:` + bulletMarker + `|\(?\d+[.)])?\s*\[\s*([xX✓✔]?)\s*\]|(TODO)[:\s])\s*(.+)$`)

	queueBulletPattern = lazyRegexp(`^\s*(?:` + bulletMarker + `|\(?\d+[.)])\s+(\S.*)$`)
	queueDoneMarker    = lazyRegexp(`(?i)^~~.*~~$|^\s*(?:✓|✔|\[done\]|\(done\)|\(tamam\)|\(bitti\))|(?:\(done\)|\(tamam\)|\(bitti\)|✓|✔)\s*$`)
	queueHeading       = lazyRegexp(`^\s*#{1,6}\s`)
	queuePriority      = lazyRegexp(`(?i)\(p([0-9])\)`)
	queueAfter         = lazyRegexp(`(?i)\(after\s+([^)]+)\)`)
	queueTag           = lazyRegexp(`#([A-Za-z][\w-]*)`)
)

var (
	emitted = false

	miswired = false
)

func miswiredNotice() string {
	if !miswired {
		return ""
	}
	now := nowSec()
	if float64(now)-numberOr(getMap(readState(), "notified"), "miswired", 0) < 86400 {
		return ""
	}
	updateState(func(next object) { stateMap(next, "notified")["miswired"] = float64(now) })
	warn("invoked with no command argument: the host's hook wiring passes no arguments")
	return T("hook.miswired", pluginName)
}

func emit(output object) {
	if notice := miswiredNotice(); notice != "" {
		output["systemMessage"] = joinNotices(getString(output, "systemMessage"), notice)
	}
	if command == "hook" && activeHost != "claude" {
		output = translateOutput(activeHost, activeEvent, output)
		if output == nil {
			return
		}
	}
	emitted = true
	os.Stdout.Write(marshalCompact(output))
}

type transcriptSummary struct {
	lastUserPrompt    string
	lastAssistantText string
	lastAssistantRaw  string
	files             []string
	commands          []string
	todos             []object
	compactSummary    string
}

func joinText(content []any) string {
	parts := []string{}
	for _, raw := range content {
		block, _ := raw.(object)
		if getString(block, "type") != "text" {
			continue
		}
		if text, ok := block["text"].(string); ok {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func summarizeTranscript(file string) transcriptSummary {
	summary := transcriptSummary{}
	if file == "" {
		return summary
	}
	lines, ok := tailLines(file, transcriptTailBytes)
	if !ok {
		if _, err := os.Stat(file); err != nil {
			warn("transcript unreadable: %v", err)
		}
		return summary
	}
	touched := []string{}
	seen := map[string]bool{}
	for _, line := range lines {
		entry, parsed := parseTranscriptLine(line)
		if !parsed || entry.sidechain || entry.isMeta {
			continue
		}
		if entry.entryType == "user" {
			hasToolResult := false
			for _, raw := range entry.content {
				if getString(toObject(raw), "type") == "tool_result" {
					hasToolResult = true
				}
			}
			if hasToolResult {
				continue
			}
			prompt := joinText(entry.content)
			if prompt == "" {
				continue
			}
			if entry.isCompact || compactPrefixPat.MatchString(prompt) {
				summary.compactSummary = prompt
			} else {
				summary.lastUserPrompt = prompt
			}
			continue
		}
		if entry.entryType != "assistant" {
			continue
		}
		if reply := joinText(entry.content); reply != "" {
			summary.lastAssistantText = reply
		}
		for _, raw := range entry.content {
			block := toObject(raw)
			if getString(block, "type") != "tool_use" {
				continue
			}
			input := getMap(block, "input")
			if input == nil {
				continue
			}
			name := getString(block, "name")
			switch {
			case fileTools[name] && getString(input, "file_path") != "":
				target := getString(input, "file_path")
				if !seen[target] {
					seen[target] = true
					touched = append(touched, target)
				}
			case name == "Bash" && getString(input, "command") != "":
				summary.commands = append(summary.commands, truncateText(getString(input, "command"), 200))
			case name == "TodoWrite":
				todos := []object{}
				for _, rawTodo := range getList(input, "todos") {
					todo := toObject(rawTodo)
					todos = append(todos, object{"content": truncateText(getString(todo, "content"), 200), "status": getString(todo, "status")})
				}
				summary.todos = todos
			}
		}
	}
	if len(touched) > 25 {
		touched = touched[len(touched)-25:]
	}
	summary.files = touched
	if len(summary.commands) > 6 {
		summary.commands = summary.commands[len(summary.commands)-6:]
	}
	summary.lastUserPrompt = truncateText(summary.lastUserPrompt, 900)
	raw := summary.lastAssistantText
	if runes := []rune(raw); len(runes) > 8000 {
		raw = string(runes[len(runes)-8000:])
	}
	summary.lastAssistantRaw = raw
	summary.lastAssistantText = truncateText(summary.lastAssistantText, 1200)
	summary.compactSummary = truncateText(summary.compactSummary, 1500)
	return summary
}

func toObject(raw any) object {
	value, _ := raw.(object)
	return value
}

func queueFileNames(cfg object) []string {
	names := []string{}
	for _, raw := range getList(section(cfg, "queue"), "files") {
		if name, _ := raw.(string); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func queueFile(cfg object, cwd string) string {
	queue := section(cfg, "queue")
	if cwd == "" || !getBool(queue, "enabled", true) {
		return ""
	}
	for _, raw := range getList(queue, "files") {
		name, _ := raw.(string)
		if name == "" {
			continue
		}
		file := filepath.Join(cwd, name)
		if info := statSafe(file); info != nil && info.Mode().IsRegular() {
			return file
		}
	}
	return ""
}

type queueView struct {
	total   int
	blocked int
	items   []string
	plain   bool
}

func parseQueueEntries(content string) ([]queueEntry, bool) {
	lines := strings.Split(content, "\n")
	hasBoxes := false
	for _, line := range lines {
		if queueItemPattern.MatchString(strings.TrimRight(line, "\r")) {
			hasBoxes = true
			break
		}
	}
	entries := []queueEntry{}
	open := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))
		var entry queueEntry
		var ok bool
		if hasBoxes {
			entry, ok = parseQueueLine(line, len(entries)+1)
		} else {
			entry, ok = parseQueueBullet(line, len(entries)+1)
		}
		switch {
		case ok:
			entries = append(entries, entry)
			open = true
		case trimmed == "" || queueHeading.MatchString(trimmed) || strings.HasPrefix(trimmed, "```"):
			open = false

		case open && hasBoxes && queueBulletPattern.MatchString(trimmed):
			open = false
		case open:
			last := &entries[len(entries)-1]
			*last = newQueueEntry(last.ordinal, last.text+" "+trimmed, last.checked)
		}
	}
	return entries, !hasBoxes && len(entries) > 0
}

type queueEntry struct {
	ordinal  int
	text     string
	checked  bool
	priority int
	tags     map[string]bool
	after    []string
}

func parseQueueLine(line string, ordinal int) (queueEntry, bool) {
	match := queueItemPattern.FindStringSubmatch(strings.TrimRight(line, "\r"))
	if match == nil {
		return queueEntry{}, false
	}
	return newQueueEntry(ordinal, match[3], match[1] != ""), true
}

func parseQueueBullet(line string, ordinal int) (queueEntry, bool) {
	match := queueBulletPattern.FindStringSubmatch(strings.TrimRight(line, "\r"))
	if match == nil {
		return queueEntry{}, false
	}
	text := strings.TrimSpace(match[1])
	return newQueueEntry(ordinal, text, queueDoneMarker.MatchString(text)), true
}

func newQueueEntry(ordinal int, text string, checked bool) queueEntry {
	entry := queueEntry{ordinal: ordinal, text: strings.TrimSpace(text), checked: checked, priority: 5, tags: map[string]bool{}}
	if found := queuePriority.FindStringSubmatch(entry.text); found != nil {
		entry.priority = int(found[1][0] - '0')
	}
	for _, found := range queueAfter.FindAllStringSubmatch(entry.text, -1) {
		for _, reference := range strings.Split(found[1], ",") {
			if reference = strings.TrimSpace(reference); reference != "" {
				entry.after = append(entry.after, strings.ToLower(reference))
			}
		}
	}
	for _, found := range queueTag.FindAllStringSubmatch(queueAfter.ReplaceAllString(entry.text, ""), -1) {
		entry.tags[strings.ToLower(found[1])] = true
	}
	return entry
}

func queueSnapshot(file string) queueView {
	info := statSafe(file)
	if info == nil {
		return queueView{}
	}
	handle, err := os.Open(file)
	if err != nil {
		warn("queue file unreadable: %v", err)
		return queueView{}
	}
	defer handle.Close()
	length := info.Size()
	if length > queueMaxBytes {
		warn("queue file %s is %d KB; only the first %d KB are read", filepath.Base(file), length/1024, queueMaxBytes/1024)
		length = queueMaxBytes
	}
	buffer := make([]byte, length)
	read, _ := handle.ReadAt(buffer, 0)
	content := string(buffer[:read])
	if int64(read) == queueMaxBytes {
		if cut := strings.LastIndex(content, "\n"); cut > 0 {
			content = content[:cut]
		}
	}
	entries, plain := parseQueueEntries(content)
	view := queueView{plain: plain}
	doneByTag := map[string]bool{}
	doneByOrdinal := map[int]bool{}

	knownTags := map[string]bool{}
	openByTag := map[string]bool{}
	for _, entry := range entries {
		for tag := range entry.tags {
			knownTags[tag] = true
			if !entry.checked {
				openByTag[tag] = true
			}
		}
		doneByOrdinal[entry.ordinal] = entry.checked
	}
	for tag := range knownTags {
		doneByTag[tag] = !openByTag[tag]
	}
	satisfied := func(reference string) bool {
		if strings.HasPrefix(reference, "#") {
			tag := strings.TrimPrefix(reference, "#")
			return !knownTags[tag] || doneByTag[tag]
		}
		ordinal, err := strconv.Atoi(reference)
		if err != nil || ordinal < 1 || ordinal > len(entries) {
			return true
		}
		return doneByOrdinal[ordinal]
	}
	eligible := []queueEntry{}
	for _, entry := range entries {
		if entry.checked {
			continue
		}
		view.total++
		ready := true
		for _, reference := range entry.after {
			if !satisfied(reference) {
				ready = false
				break
			}
		}
		if ready {
			eligible = append(eligible, entry)
		} else {
			view.blocked++
		}
	}
	sort.SliceStable(eligible, func(a, b int) bool { return eligible[a].priority < eligible[b].priority })
	for _, entry := range eligible {
		if len(view.items) >= queueMaxItems {
			break
		}
		view.items = append(view.items, truncateText(entry.text, 160))
	}
	return view
}

func openTasks(state object, sid string) []object {
	entry := getMap(getMap(state, "tasks"), sid)
	items := getMap(entry, "items")
	result := []object{}
	for _, key := range sortedKeys(items) {
		task := toObject(items[key])
		if task != nil && getString(task, "status") != "completed" {
			result = append(result, task)
		}
	}
	return result
}

func insideGitRepo(cwd string) bool {
	if cwd == "" {
		return false
	}
	dir, _ := filepath.Abs(cwd)
	for depth := 0; depth < 6; depth++ {
		if statSafe(filepath.Join(dir, ".git")) != nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return false
}

func gitStatusRaw(cwd string) (string, bool) {
	if !insideGitRepo(cwd) {
		return "", false
	}
	command := exec.Command("git", "status", "--short")
	command.Dir = cwd
	output, err := runWithTimeout(command, 3*time.Second)
	if err != nil {
		return "", false
	}
	return strings.ReplaceAll(string(output), "\r\n", "\n"), true
}

func treeFingerprint(cwd string) string {
	raw, ok := gitStatusRaw(cwd)
	if !ok {
		return ""
	}
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:8])
}

func workspaceChanged(record object) bool {
	before := getString(record, "tree")
	if before == "" {
		return false
	}
	after := treeFingerprint(getString(record, "cwd"))
	return after != "" && after != before
}

func gitStatus(cwd string) []string {
	output, ok := gitStatusRaw(cwd)
	if !ok {
		return nil
	}
	lines := []string{}
	for _, line := range strings.Split(output, "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > gitStatusLines {
		lines = lines[:gitStatusLines]
	}
	return lines
}

func runWithTimeout(command *exec.Cmd, timeout time.Duration) ([]byte, error) {
	var buffer bytes.Buffer
	command.Stdout = &buffer

	command.WaitDelay = 500 * time.Millisecond
	if err := command.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if errors.Is(err, exec.ErrWaitDelay) {
			err = nil
		}
		return buffer.Bytes(), err
	case <-time.After(timeout):
		_ = command.Process.Kill()
		<-done
		return buffer.Bytes(), fmt.Errorf("timeout")
	}
}

func buildCheckpoint(input object, reasonLine, model string, cfg object) string {
	sid := sessionKey(input)
	cwd := getString(input, "cwd")
	summary := summarizeTranscript(getString(input, "transcript_path"))
	tracked := openTasks(readState(), sid)
	if len(tracked) > queueMaxItems {
		tracked = tracked[:queueMaxItems]
	}
	queuePath := ""

	if cfg != nil {
		if candidate := queueFileFor(cfg, cwd, sessionKey(input)); queueTrusted(cfg, candidate) {
			queuePath = candidate
		}
	}
	var queue *queueView
	if queuePath != "" {
		view := queueSnapshot(queuePath)
		queue = &view
	}
	changes := gitStatus(cwd)
	if model == "" {
		model = "?"
	}
	lines := []string{
		T("checkpoint.title", pluginName),
		"",
		T("checkpoint.session", sid),
		T("checkpoint.time", time.Now().Format("02.01.2006 15:04:05")),
		T("checkpoint.project", cwd),
		T("checkpoint.model", model),
		T("checkpoint.reason", reasonLine),
		"",
	}
	if summary.compactSummary != "" {
		lines = append(lines, T("checkpoint.compact"), summary.compactSummary, "")
	}
	lines = append(lines, T("checkpoint.lastPrompt"), orDefault(summary.lastUserPrompt, T("checkpoint.notFound")), "", T("checkpoint.lastReply"), orDefault(summary.lastAssistantText, T("checkpoint.notFound")), "", T("checkpoint.files"))
	if len(summary.files) == 0 {
		lines = append(lines, T("checkpoint.none"))
	}
	for _, file := range summary.files {
		lines = append(lines, "- "+file)
	}
	if len(changes) > 0 {
		lines = append(lines, "", T("checkpoint.git"))
		for _, line := range changes {
			lines = append(lines, "- "+line)
		}
	}
	lines = append(lines, "", T("checkpoint.commands"))
	if len(summary.commands) == 0 {
		lines = append(lines, T("checkpoint.none"))
	}
	for _, cmd := range summary.commands {
		lines = append(lines, "- `"+cmd+"`")
	}
	lines = append(lines, "", T("checkpoint.todos"))
	if len(summary.todos) == 0 {
		lines = append(lines, T("checkpoint.noTodos"))
	}
	for _, todo := range summary.todos {
		mark := " "
		if getString(todo, "status") == "completed" {
			mark = "x"
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s", mark, getString(todo, "content")))
	}
	if len(tracked) > 0 {
		lines = append(lines, "", T("checkpoint.tasks"))
		for _, task := range tracked {
			lines = append(lines, "- "+getString(task, "subject"))
		}
	}
	if queue != nil {
		lines = append(lines, "", T("checkpoint.queue", filepath.Base(queuePath), queue.total))
		if len(queue.items) == 0 {
			lines = append(lines, T("checkpoint.queueEmpty"))
		}
		for _, item := range queue.items {
			lines = append(lines, "- [ ] "+item)
		}
	}
	if runs := workflowRuns(readState(), sid); len(runs) > 0 {
		lines = append(lines, "", T("checkpoint.workflows"))
		for _, run := range runs {
			lines = append(lines, "- "+run)
		}
		lines = append(lines, T("checkpoint.workflowsHint"))
	}
	snapshotRef := ""
	if cfg != nil && getBool(section(cfg, "checkpoint"), "gitSnapshot", false) {
		if ref, hash := gitSnapshot(cwd, sid); ref != "" {
			snapshotRef = ref
			lines = append(lines, "", T("checkpoint.snapshot"), "- "+ref+" → git stash apply "+hash, "")
		}
	}
	lines = append(lines, "", T("checkpoint.resume"), "claude --resume "+sid, "")
	ensureDir(files.checkpoints)
	file := filepath.Join(files.checkpoints, sid+".md")
	if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		fail("checkpoint failed: %v", err)
		return ""
	}
	updateState(func(state object) {
		entry := object{"path": file, "cwd": cwd, "at": float64(nowSec()), "consumed": false}
		if snapshotRef != "" {
			entry["snapshot"] = snapshotRef
		}
		stateMap(state, "checkpoints")[sid] = entry
	})
	return file
}

func gitSnapshot(cwd, sid string) (ref, hash string) {
	if !insideGitRepo(cwd) {
		return "", ""
	}
	create := exec.Command("git", "stash", "create", "noctis checkpoint "+sid)
	create.Dir = cwd
	output, err := runWithTimeout(create, 15*time.Second)
	hash = strings.TrimSpace(string(output))
	if err != nil || hash == "" {
		return "", ""
	}
	ref = fmt.Sprintf("refs/noctis/%s/%d", sid, nowSec())
	pin := exec.Command("git", "update-ref", ref, hash)
	pin.Dir = cwd
	if _, err := runWithTimeout(pin, 5*time.Second); err != nil {
		warn("git snapshot ref failed: %v", err)
		return "", ""
	}
	logInfo("git snapshot %s = %s", ref, hash[:8])
	return ref, hash
}

func dropGitSnapshot(cwd, ref string) {
	if cwd == "" || ref == "" {
		return
	}
	drop := exec.Command("git", "update-ref", "-d", ref)
	drop.Dir = cwd
	_, _ = runWithTimeout(drop, 5*time.Second)
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func consumeCheckpoint(sid string) {
	updateState(func(state object) {
		if entry := getMap(getMap(state, "checkpoints"), sid); entry != nil {
			entry["consumed"] = true
		}
	})
}

func waitLive(wait object, now int64) bool {
	if wait == nil {
		return false
	}
	reference := numberOr(wait, "resumeAt", numberOr(wait, "startedAt", float64(now)))
	if float64(now)-reference < waitStaleSeconds {
		return true
	}
	scheduled := getMap(wait, "scheduled")
	if getString(scheduled, "method") == "sleeper" {
		if pid, ok := getNumber(scheduled, "pid"); ok && processAlive(int(pid)) {
			return true
		}
	}
	return false
}

func checkpointUsable(entry object, cwd string, now int64) bool {
	return entry != nil &&
		!getBool(entry, "consumed", false) &&
		(cwd == "" || getString(entry, "cwd") == cwd) &&
		float64(now)-numberOr(entry, "at", 0) <= checkpointTTLSeconds
}

func latestCheckpointFor(state object, cwd string, now int64) (string, object) {
	bestSid := ""
	var best object
	for sid, raw := range getMap(state, "checkpoints") {
		entry := toObject(raw)
		if !checkpointUsable(entry, cwd, now) {
			continue
		}
		if best == nil || numberOr(entry, "at", 0) > numberOr(best, "at", 0) {
			best, bestSid = entry, sid
		}
	}
	return bestSid, best
}

type shellResult struct {
	ok     bool
	stderr string
	err    string
}

func runPowershell(script string, timeout time.Duration) shellResult {
	command := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	_, err := runWithTimeout(command, timeout)
	if err != nil {
		return shellResult{stderr: strings.TrimSpace(stderr.String()), err: err.Error()}
	}
	return shellResult{ok: true}
}

func psQuote(text string) string {
	return "'" + strings.ReplaceAll(text, "'", "''") + "'"
}

func taskName(sid string) string {
	return "Noctis-" + hashKey(files.configDir+"|"+sid)
}

func detachedSelf(argsList []string) int {
	executable, err := os.Executable()
	if err != nil {
		fail("detached spawn failed: %v", err)
		return 0
	}
	child := exec.Command(executable, argsList...)
	child.Dir = files.guardDir
	child.Env = os.Environ()
	child.Stdin, child.Stdout, child.Stderr = nil, nil, nil
	configureDetached(child)
	if err := child.Start(); err != nil {
		fail("detached spawn failed: %v", err)
		return 0
	}
	pid := child.Process.Pid
	_ = child.Process.Release()
	return pid
}

func removeScheduledTask(name string) {
	if !isWindows {
		return
	}
	runPowershell("Unregister-ScheduledTask -TaskName "+psQuote(name)+" -Confirm:$false -ErrorAction SilentlyContinue", 30*time.Second)
}

func cancelRunner(sid string, state object) {
	cancelRunnerExcept(sid, state, nil)
}

func cancelRunnerExcept(sid string, state object, keep object) {
	wait := getMap(getMap(state, "waits"), sid)
	scheduled := getMap(wait, "scheduled")
	if sameSchedule(scheduled, keep) {
		return
	}
	cancelScheduled(sid, scheduled)
}

func cancelScheduled(sid string, scheduled object) {
	if isWindows {
		removeScheduledTask(taskName(sid))
	}
	cancelNative(sid, scheduled)
	if getString(scheduled, "method") == "sleeper" {
		killDetached(scheduled, "pid", "sleeper")
	}
	killDetached(scheduled, "watcherPid", "reset watcher")
}

func sameSchedule(left, right object) bool {
	if left == nil || right == nil {
		return false
	}
	method := getString(left, "method")
	if method == "" || method != getString(right, "method") {
		return false
	}
	for _, field := range []string{"pid", "unit", "label", "taskName", "watcherPid"} {
		if getString(left, field) != getString(right, field) {
			return false
		}
		if numberOr(left, field, math.NaN()) != numberOr(right, field, math.NaN()) {

			if _, leftHas := getNumber(left, field); leftHas {
				return false
			}
			if _, rightHas := getNumber(right, field); rightHas {
				return false
			}
		}
	}
	return true
}

func killDetached(scheduled object, key, what string) {
	pid, ok := getNumber(scheduled, key)
	if !ok || int(pid) == 0 || int(pid) == os.Getpid() {
		return
	}
	if !processAlive(int(pid)) {
		logInfo("%s %d already gone", what, int(pid))
		return
	}
	if name := processName(int(pid)); name != "" && !ownHelperProcess(name) {
		warn("%s %d is now %q, not ours; left alone", what, int(pid), name)
		return
	}
	if process, err := os.FindProcess(int(pid)); err == nil {
		if err := process.Kill(); err != nil {
			logInfo("%s %d already gone", what, int(pid))
		}
	}
}

func ensureRunnerLauncher() string {
	executable, _ := os.Executable()
	content := strings.Join([]string{
		"@echo off",
		fmt.Sprintf(`"%s" %%*`, executable),
		"exit /b %errorlevel%",
		"",
	}, "\r\n")
	existing, _ := os.ReadFile(files.runnerLauncher)
	if string(existing) != content {
		_ = os.WriteFile(files.runnerLauncher, []byte(content), 0o644)
	}
	return files.runnerLauncher
}

func scheduleWindowsTask(name string, at float64, commandArgs []string, wake bool) shellResult {
	return runPowershell(windowsTaskScript(name, at, commandArgs, wake), 45*time.Second)
}

func windowsTaskScript(name string, at float64, commandArgs []string, wake bool) string {
	launcher := ensureRunnerLauncher()
	argument := fmt.Sprintf(`/d /c "%s" %s`, launcher, strings.Join(commandArgs, " "))
	wakeFlag := ""
	if wake {
		wakeFlag = "-WakeToRun "
	}
	return strings.Join([]string{
		"$ErrorActionPreference = 'Stop'",
		"$action = New-ScheduledTaskAction -Execute 'cmd.exe' -Argument " + psQuote(argument) + " -WorkingDirectory " + psQuote(files.guardDir),
		"$trigger = New-ScheduledTaskTrigger -Once -At ([datetime]" + psQuote(localISO(at)) + ")",
		"$settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries " + wakeFlag + "-ExecutionTimeLimit (New-TimeSpan -Days 7) -MultipleInstances IgnoreNew",
		"Register-ScheduledTask -TaskName " + psQuote(name) + " -Action $action -Trigger $trigger -Settings $settings -Force | Out-Null",
	}, "; ")
}

func scheduleRunner(cfg object, sid string, atEpoch float64) object {
	var scheduled object
	withFileLock(files.scheduleLock, func() {
		scheduled = scheduleRunnerLocked(cfg, sid, atEpoch)
	})
	return scheduled
}

func runnerArgs(command, sid, account string, extra ...string) []string {
	arguments := []string{command, "--sid", sid, "--account", account}
	if host := currentHost().id; host != "claude" {
		arguments = append(arguments, "--host", host)
	}
	return append(arguments, extra...)
}

func scheduleRunnerLocked(cfg object, sid string, atEpoch float64) object {
	now := nowSec()
	at := math.Max(atEpoch, float64(now+15))
	cancelRunner(sid, readState())
	var scheduled object
	nativeAllowed := os.Getenv("NOCTIS_NO_TASKS") == ""
	if isWindows && nativeAllowed {
		result := scheduleWindowsTask(taskName(sid), at, runnerArgs("resume", sid, `"`+files.configDir+`"`), getBool(section(cfg, "alarm"), "wakePc", true))
		if result.ok {
			scheduled = object{"method": "task", "taskName": taskName(sid), "at": at, "watcherPid": float64(startResetWatcher(cfg, sid, at))}
		} else {
			warn("task scheduling failed, falling back to sleeper: %s", orDefault(result.err, result.stderr))
		}
	} else if nativeAllowed {
		if native, ok := scheduleNative(sid, at, runnerArgs("resume", sid, files.configDir), getBool(section(cfg, "alarm"), "wakePc", true)); ok {
			scheduled = native
			scheduled["watcherPid"] = float64(startResetWatcher(cfg, sid, at))
		}
	}
	if scheduled == nil && os.Getenv("NOCTIS_NO_SCHEDULE") != "" {
		scheduled = object{"method": "manual", "at": at}
	}
	if scheduled == nil {
		pid := detachedSelf(runnerArgs("sleeper", sid, files.configDir, "--at", formatNumber(at)))
		if pid > 0 {
			scheduled = object{"method": "sleeper", "pid": float64(pid), "at": at}
		} else {
			scheduled = object{"method": "manual", "at": at}
			fail("no scheduler available for %s; resume manually with claude --resume %s after %s", sid, sid, localISO(at))
		}
	}
	updateState(func(state object) {
		if wait := getMap(getMap(state, "waits"), sid); wait != nil {
			wait["scheduled"] = scheduled
		}
	})
	logInfo("runner scheduled for %s at %s via %s", sid, localISO(at), getString(scheduled, "method"))
	return scheduled
}

func inferLaunchMode(cfg object, sid string) string {
	mode := getString(section(cfg, "resume"), "mode")
	if mode == "none" {
		return "none"
	}
	if getMap(getMap(readJSON(files.usage), "sessions"), sid) != nil {
		return mode
	}
	return "headless"
}

func registerWait(sid string, record object, cfg object) bool {
	clearQuietMarker(sid)

	cancelRunnerExcept(sid, readState(), getMap(record, "scheduled"))
	if cfg != nil && getString(record, "launchMode") == "" {
		if mode := inferLaunchMode(cfg, sid); mode != "" {
			record["launchMode"] = mode
		}
	}
	updateState(func(state object) {
		stateMap(state, "waits")[sid] = record
	})
	if getMap(getMap(readState(), "waits"), sid) == nil {
		fail("wait for %s could not be stored; not pausing the session", sid)
		return false
	}
	return true
}

func clearWait(sid string, state object) {
	if state == nil {
		state = readState()
	}
	cancelRunner(sid, state)
	updateState(func(next object) {
		delete(stateMap(next, "waits"), sid)
	})
}

func clearWaitAndConsume(sid string, state object) {
	if state == nil {
		state = readState()
	}
	cancelRunner(sid, state)
	updateState(func(next object) {
		delete(stateMap(next, "waits"), sid)
		if entry := getMap(getMap(next, "checkpoints"), sid); entry != nil {
			entry["consumed"] = true
		}
	})
}

func notify(cfg object, title, body string) {
	alarm := section(cfg, "alarm")
	if !getBool(alarm, "enabled", true) {
		return
	}
	var child *exec.Cmd
	switch {
	case isWindows:
		child = exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", files.notifyScript, "-Title", title, "-Body", body)
	case runtime.GOOS == "darwin":
		clean := func(text string) string { return strings.NewReplacer(`"`, "'", `\`, "'").Replace(text) }
		child = exec.Command("osascript", "-e", fmt.Sprintf(`display notification "%s" with title "%s" sound name "Glass"`, clean(body), clean(title)))
	default:
		child = exec.Command("notify-send", title, body)
	}
	configureDetached(child)
	if err := child.Start(); err != nil {

		if notifierMissing(err) {
			once := "notify:" + filepath.Base(child.Path)
			if float64(nowSec())-numberOr(getMap(readState(), "notified"), once, 0) > 86400 {
				updateState(func(next object) { stateMap(next, "notified")[once] = float64(nowSec()) })
				warn("no desktop notifier here (%v); alarms are silent, waits are unaffected", err)
			}
		} else {
			warn("notify failed: %v", err)
		}
	} else {
		_ = child.Process.Release()
	}
	if webhookSettings(cfg).target != "" {
		if command == "webhook" {
			deliverWebhook(cfg, title, body)
		} else {
			detachedSelf([]string{"webhook", "--account", files.configDir, "--title", title, "--body", body})
		}
	}
	logInfo("notify: %s — %s", title, body)
}

func persistModelSwitch(cfg object, fableResetsAt float64, now int64) {
	models := section(cfg, "models")
	if settingsModel() != getString(models, "fallback") {
		setSettingsModel(getString(models, "fallback"))
	}
	updateState(func(state object) {
		state["modelSwitched"] = object{"at": float64(now), "from": getString(models, "primary"), "to": getString(models, "fallback"), "fableResetsAt": fableResetsAt}
	})
}

func isHandoffSession(sid string) bool {
	return os.Getenv(handoffEnv) == sid
}

func hookSleeping(wait object) bool {
	return getBool(wait, "inHook", false) || numberOr(wait, "waking", 0) > 0
}

func releaseInterruptedWait(sid string, state object) {
	rescheduleStrandedWaits(state)
	wait := getMap(getMap(state, "waits"), sid)
	if wait == nil || getMap(getMap(state, "handedOff"), sid) != nil {
		return
	}
	if !hookSleeping(wait) {
		return
	}
	now := float64(nowSec())
	heartbeat := numberOr(wait, "heartbeat", now)
	startedAt := numberOr(wait, "startedAt", now)
	slept := math.Max(0, math.Min(now, heartbeat)-startedAt)
	intended := numberOr(wait, "resumeAt", now) - startedAt
	clearWait(sid, state)
	if slept > 120 && intended-slept > 120 {
		updateState(func(next object) {
			samples := getList(next, "interruptedWaits")
			samples = append(samples, slept)
			if len(samples) > interruptedSamples {
				samples = samples[len(samples)-interruptedSamples:]
			}
			next["interruptedWaits"] = samples
			if len(samples) >= 2 {

				low, high := math.Inf(1), 0.0
				for _, raw := range samples {
					if value, ok := toNumber(raw); ok {
						low, high = math.Min(low, value), math.Max(high, value)
					}
				}
				current := numberOr(next, "hookCapSeconds", 0)
				if low >= 300 && high/low < 1.1 && (current == 0 || low < current) {
					next["hookCapSeconds"] = low
					warn("in-hook waits keep ending after ~%d min; assuming a hook timeout cap and using the runner for longer waits", int(low/60+0.5))
				}
			}
		})
	}
	logInfo("in-hook wait for %s was interrupted after %ds of %ds; runner cancelled", sid, int(slept), int(intended))
}

func clearDeadHandoffs(state object) {
	stale := []string{}
	for sid, raw := range getMap(state, "handedOff") {
		record := toObject(raw)
		if record == nil {
			stale = append(stale, sid)
			continue
		}
		pid := int(numberOr(record, "pid", 0))
		if pid <= 0 || pid == os.Getpid() || processAlive(pid) {
			continue
		}

		if float64(nowSec())-numberOr(record, "at", 0) < handoffGraceSeconds {
			continue
		}
		stale = append(stale, sid)
	}
	if len(stale) == 0 {
		return
	}
	updateState(func(next object) {
		for _, sid := range stale {
			delete(stateMap(next, "handedOff"), sid)
		}
	})
	for _, sid := range stale {
		delete(getMap(state, "handedOff"), sid)
		journal(sid, "repair", "handoff-gone", "the relaunch process is no longer running", nil)
		warn("hand-off of %s pointed at a process that is gone; the session is free again", sid)
	}
}

func rescheduleStrandedWaits(state object) {
	clearDeadHandoffs(state)
	for sid, raw := range getMap(state, "waits") {
		record := toObject(raw)
		if record == nil || getBool(record, "inHook", false) || getMap(record, "scheduled") != nil {
			continue
		}
		if getMap(getMap(state, "handedOff"), sid) != nil {
			continue
		}
		resumeAt := numberOr(record, "resumeAt", 0)
		if resumeAt <= 0 {
			continue
		}
		journal(sid, "repair", "reschedule", "wait had no runner", object{"resumeAt": resumeAt})
		warn("wait for %s had no runner (an interrupted hook); rescheduling", sid)
		scheduleRunner(loadConfig(), sid, resumeAt)
	}
}

type waitOutcome struct {
	stop    string
	notice  string
	context string
}

func workspaceGuardOn(cfg object) bool {
	return getBool(section(cfg, "wait"), "workspaceGuard", true)
}

func recordTree(cfg object, record object, cwd string) {
	if workspaceGuardOn(cfg) {
		record["tree"] = treeFingerprint(cwd)
	}
}

func hitLabel(wait *waitPlan) string {
	switch wait.hit {
	case "burst", "compaction", "blind", "projection", "budget":
		return T("hit." + wait.hit)
	}
	return T("hit.threshold", formatNumber(wait.threshold))
}

func permissionModeOf(input object) string {
	mode := getString(input, "permission_mode")
	if knownPermModes[mode] {
		return mode
	}
	return ""
}

func enforceWait(kind string, input object, cfg object, result decision) waitOutcome {
	now := nowSec()
	sid := sessionKey(input)
	wait := result.wait
	waitCfg := section(cfg, "wait")
	resumeAt := wait.until + math.Max(0, numberOr(waitCfg, "resetMarginSeconds", 0))
	learnedCap := numberOr(readState(), "hookCapSeconds", 0)
	inHookLimit := math.Max(1, numberOr(waitCfg, "maxInHookMinutes", 0)) * 60
	if learnedCap > 0 {
		inHookLimit = math.Min(inHookLimit, math.Max(60, learnedCap-60))
	}
	inHook := resumeAt-float64(now) <= inHookLimit
	if current := getMap(getMap(readState(), "waits"), sid); kind != "prompt" && current != nil && getString(current, "window") == wait.window && numberOr(current, "until", 0) == wait.until && float64(now)-numberOr(current, "startedAt", 0) < 120 {

		journal(sid, kind, "join-wait", hitLabel(wait), object{"window": wait.window})
		if inHook && getBool(current, "inHook", false) {
			joined := newWaitWatch(cfg, sid, false)
			sleepUntilEvery(numberOr(current, "resumeAt", resumeAt), joined.tickSeconds(), joined.tick)
			return waitOutcome{notice: T("wait.resumed", wait.label, formatNumber(wait.used), durationText(float64(nowSec()-now)))}
		}
		return waitOutcome{stop: T("wait.saved", wait.label, formatNumber(wait.used), formatTime(numberOr(current, "resumeAt", resumeAt)), "")}
	}
	reasonLine := T("wait.reason", wait.label, formatNumber(wait.used), hitLabel(wait), formatTime(wait.until))
	checkpoint := buildCheckpoint(input, reasonLine, result.model, cfg)
	queuedPrompt := ""
	if kind == "prompt" {
		queuedPrompt = truncateText(getString(input, "prompt"), 4000)
	}
	record := object{
		"kind": kind, "window": wait.window, "label": wait.label, "used": wait.used, "threshold": wait.threshold, "hit": wait.hit,
		"until": wait.until, "resumeAt": resumeAt, "inHook": inHook, "cwd": getString(input, "cwd"),
		"transcript": getString(input, "transcript_path"), "checkpoint": checkpoint, "queuedPrompt": queuedPrompt,
		"startedAt": float64(now), "heartbeat": float64(now), "permissionMode": permissionModeOf(input),
	}
	recordTree(cfg, record, getString(input, "cwd"))
	runnerAt := resumeAt
	if inHook {
		runnerAt += math.Max(0, numberOr(waitCfg, "builtinGraceSeconds", 0))
	}

	if !registerWait(sid, record, cfg) {
		journal(sid, kind, "pause-failed", reasonLine, object{"window": wait.window, "used": wait.used})
		return waitOutcome{notice: T("wait.notStored", pluginName)}
	}
	scheduled := scheduleRunner(cfg, sid, runnerAt)
	record["scheduled"] = scheduled
	updateState(func(next object) {
		if current := getMap(getMap(next, "waits"), sid); current != nil {
			current["scheduled"] = scheduled
		}
	})
	journal(sid, kind, "pause", reasonLine, object{"inHook": inHook, "resumeAt": resumeAt, "hit": wait.hit, "window": wait.window, "used": wait.used})
	logInfo("wait (%s) for %s: %s; inHook=%t; checkpoint=%s", kind, sid, reasonLine, inHook, orDefault(checkpoint, "none"))
	if inHook {
		watch := newWaitWatch(cfg, sid, true)
		sleepUntilEvery(resumeAt, watch.tickSeconds(), watch.tick)
		clearWaitAndConsume(sid, nil)
		switch {
		case watch.cancelled:
			journal(sid, kind, "wait-cancelled", hitLabel(wait), nil)
			logInfo("in-hook wait for %s cancelled; continuing", sid)
			return waitOutcome{notice: T("wait.cancelled", wait.label)}
		case watch.early && (wait.until <= 0 || float64(nowSec()) < wait.until):
			journal(sid, kind, "early-reset", hitLabel(wait), object{"waited": float64(nowSec() - now)})
			notify(cfg, pluginName, T("wait.earlyResetNotify", wait.label))
			logInfo("in-hook wait for %s ended early: %s reset ahead of schedule", sid, wait.window)
		default:
			notify(cfg, pluginName, T("wait.notifyReset", wait.label))
			logInfo("in-hook wait finished for %s", sid)
		}
		if slept := float64(nowSec() - now); learnedCap > 0 && slept > learnedCap {
			updateState(func(next object) {
				next["hookCapSeconds"] = float64(0)
				next["interruptedWaits"] = []any{}
			})
			logInfo("an in-hook wait of %ds completed past the learned cap; cap forgotten", int(slept))
		}
		outcome := waitOutcome{notice: T("wait.resumed", wait.label, formatNumber(wait.used), durationText(float64(nowSec()-now)))}
		if watch.early && (wait.until <= 0 || float64(nowSec()) < wait.until) {
			outcome.notice = T("wait.earlyReset", wait.label, durationText(float64(nowSec()-now)))
		}
		if workspaceChanged(record) {
			journal(sid, kind, "workspace-changed", "tree differs from the checkpoint", nil)
			warn("workspace changed while %s waited; the session was told to re-check", sid)
			outcome.notice += " " + T("workspace.changed")
			outcome.context = T("workspace.context")
		}
		return outcome
	}
	suffix := ""
	if getString(scheduled, "method") == "sleeper" {
		suffix = T("wait.sleeperSuffix")
	}
	stop := T("wait.saved", wait.label, formatNumber(wait.used), formatTime(resumeAt), suffix)
	if kind == "prompt" {
		stop += " " + T("wait.savedHint")
	}
	return waitOutcome{stop: stop}
}

func handleFableHit(kind string, input object, cfg object, result decision) string {
	now := nowSec()
	sid := sessionKey(input)
	models := section(cfg, "models")
	fallback := getString(models, "fallback")
	label := scopedLabel(cfg)
	fableUsed := result.usage.fable.used
	persistModelSwitch(cfg, result.usage.fable.resetsAt, now)
	journal(sid, kind, "switch-model", label+" "+formatNumber(fableUsed)+"%", object{"to": fallback, "scoped": fableUsed})
	logInfo("fable threshold hit (%s) for %s: %%%s", kind, sid, formatNumber(fableUsed))
	if kind == "prompt" {
		return T("scoped.promptBlock", label, formatNumber(fableUsed), fallback, fallback)
	}
	checkpoint := buildCheckpoint(input, T("scoped.reason", label, formatNumber(fableUsed), fallback), result.model, cfg)
	if !getBool(section(cfg, "fable"), "autoRelaunch", true) || getString(section(cfg, "resume"), "mode") == "none" {
		return T("scoped.savedManual", label, formatNumber(fableUsed), fallback, fallback)
	}
	record := object{
		"kind": "fable", "window": "fable", "label": label, "used": fableUsed, "threshold": scopedThreshold(cfg),
		"until": float64(now), "resumeAt": float64(now + 20), "inHook": false, "cwd": getString(input, "cwd"),
		"transcript": getString(input, "transcript_path"), "checkpoint": checkpoint, "modelOverride": fallback,
		"queuedPrompt": "", "startedAt": float64(now), "permissionMode": permissionModeOf(input),
	}
	recordTree(cfg, record, getString(input, "cwd"))
	record["scheduled"] = scheduleRunner(cfg, sid, float64(now+20))
	if !registerWait(sid, record, cfg) {
		cancelRunner(sid, readState())
		return T("wait.notStored", pluginName)
	}
	return T("scoped.savedRelaunch", label, formatNumber(fableUsed), fallback)
}

func maybeRevertDefaultModel(cfg object, state object, usage usageView, now int64) string {
	switched := getMap(state, "modelSwitched")
	if switched == nil || !getBool(section(cfg, "fable"), "revertOnReset", true) {
		return ""
	}
	cleared := false
	if usage.fable != nil {
		cleared = usage.fable.used < scopedThreshold(cfg)/2
	} else {
		cleared = float64(now) > numberOr(switched, "fableResetsAt", 0)
	}
	if !cleared {
		return ""
	}
	models := section(cfg, "models")
	if settingsModel() == getString(models, "fallback") {
		setSettingsModel(getString(models, "primary"))
	}
	updateState(func(next object) { next["modelSwitched"] = nil })
	logInfo("fable window cleared: default model reverted to primary")
	return T("scoped.reverted", scopedLabel(cfg), getString(models, "primary"))
}

func recordHookPulse(state object, now int64) {
	if float64(now)-numberOr(state, "lastHookAt", 0) < hookPulseInterval {
		return
	}
	sweepStaleLocks()
	sweepQuietMarkers(now)
	updateState(func(next object) { next["lastHookAt"] = float64(now) })
}

type decideOptions struct {
	force   bool
	noProbe bool
}

func decide(cfg object, state object, input object, now int64, options decideOptions) decision {
	recordHookPulse(state, now)
	sid := sessionKey(input)
	usageFile := readJSON(files.usage)
	model := resolveSessionModel(cfg, state, usageFile, sid)
	sessionInfo := getMap(getMap(usageFile, "sessions"), sid)
	contextPercent, hasContext := getNumber(sessionInfo, "context")
	usageCfg := section(cfg, "usage")
	staleSeconds := usageStaleSeconds(cfg)
	if sessionInfo == nil {

		staleSeconds = 60
	}

	usageAt := numberOr(usageFile, "updatedAt", 0)
	usageStale := usageAt == 0 || float64(now)-usageAt > staleSeconds
	fableCandidate := scopedModelPattern(cfg).MatchString(model)
	snapshot := currentUsage(now)
	edge := nearEdge(cfg, snapshot)
	fableEdge := fableCandidate && snapshot.fable != nil && snapshot.fable.used >= scopedThreshold(cfg)-nearEdgeBand
	usage := snapshot
	refreshError := ""
	if fableCandidate || usageStale || options.force || edge {
		reason := "stale-usage-fallback"
		maxAge := -1.0
		switch {
		case options.force:
			reason, maxAge = "forced", 30
		case edge:
			reason, maxAge = "near-edge", edgePollSeconds(cfg, snapshot)
		case fableEdge:
			reason, maxAge = "fable-edge", nearEdgePollNormal
		case fableCandidate:
			reason = "fable-session"
		}
		before := numberOr(readJSON(files.fable), "fetchedAt", 0)
		after := refreshFable(cfg, now, reason, maxAge, options.force)
		if numberOr(after, "fetchedAt", 0) != before {
			usage = currentUsage(now)
		} else {
			refreshError = liveRefreshError(after, now)
		}
	}
	fetchedOnce := numberOr(readJSON(files.fable), "fetchedAt", 0) > 0
	permanentError := refreshError == "no-token" || strings.HasPrefix(refreshError, "http-40")
	if edge && refreshError != "" && fetchedOnce && !permanentError && !options.noProbe && float64(now)-snapshot.updatedAt > blindAfterSeconds {
		warn("near the limit without fresh data (%s); probing before allowing more work", refreshError)
		rounds := int(math.Max(1, numberOr(usageCfg, "blindProbeRounds", 5)))
		interval := math.Max(1, numberOr(usageCfg, "blindProbeSeconds", 60))
		probedFrom := numberOr(readJSON(files.fable), "fetchedAt", 0)
		for round := 1; round <= rounds; round++ {
			sleepUntil(float64(nowSec())+interval, nil)
			probe := refreshFable(cfg, nowSec(), "blind-probe", 0, true)

			if fetched := numberOr(probe, "fetchedAt", 0); fetched > probedFrom {
				usage = currentUsage(nowSec())
				refreshError = liveRefreshError(probe, nowSec())
				if refreshError == "" {
					break
				}
				probedFrom = fetched
			}
		}
		if refreshError != "" {
			key, threshold := "seven_day", thresholdOf(cfg, "weeklyAll")
			if snapshot.fiveHour != nil && snapshot.fiveHour.used >= thresholdOf(cfg, "session5h")-nearEdgeBand {
				key, threshold = "five_hour", thresholdOf(cfg, "session5h")
			}
			label := windowLabel(key)
			win := snapshot.byKey(key)
			blind := evaluate(cfg, usage, model, contextPercent, hasContext)
			blind.usageStale, blind.contextPercent, blind.hasContext = true, contextPercent, hasContext
			if blind.wait == nil && win != nil {
				blind.wait = &waitPlan{window: key, label: label, used: win.used, threshold: threshold, until: win.resetsAt, hit: "blind"}
			}
			used := 0.0
			if win != nil {
				used = win.used
			}
			fail("no usage data for %d min while at %s%% (%s); pausing until data or reset", int((float64(nowSec())-snapshot.updatedAt)/60+0.5), formatNumber(used), label)
			return blind
		}
	}
	result := evaluate(cfg, usage, model, contextPercent, hasContext)
	result.usageStale, result.contextPercent, result.hasContext = usageStale, contextPercent, hasContext
	notices, marks := planNotices(cfg, state, usage, &result, sid, now, currentHost().limits)
	if len(marks) > 0 {
		updateState(func(next object) {
			for _, key := range marks {
				stateMap(next, "notified")[key] = float64(now)
			}
		})
	}
	if reverted := maybeRevertDefaultModel(cfg, state, usage, now); reverted != "" {
		notices = append(notices, reverted)
	}
	if len(notices) > 0 {
		result.notice = strings.Join(notices, " ")
	}
	return result
}

func planNotices(cfg, state object, usage usageView, result *decision, sid string, now int64, hostReportsLimits bool) (notices []string, marks []string) {
	notified := getMap(state, "notified")
	if !usage.hasAny && notified[sid] == nil && hostReportsLimits {
		notices = append(notices, T("notice.noUsage", pluginName))
		marks = append(marks, sid)
	}
	if getString(cfg, "configError") != "" && notified[sid+":config"] == nil {
		notices = append(notices, T("notice.configError", pluginName))
		marks = append(marks, sid+":config")
	}
	if used, cap, over := dailyBudgetStatus(cfg, state, usage, now); over {
		budgetKey := sid + ":budget:" + localDay(now)
		switch {
		case getBool(section(cfg, "budget"), "hardStop", false) && result.wait == nil && usage.sevenDay != nil:
			result.wait = &waitPlan{window: "seven_day", label: windowLabel("seven_day"), used: usage.sevenDay.used, threshold: cap, until: nextLocalMidnight(now), hit: "budget"}
			marks = append(marks, budgetKey)
		case notified[budgetKey] == nil:
			notices = append(notices, T("notice.budget", formatNumber(math.Round(used*10)/10), formatNumber(cap)))
			marks = append(marks, budgetKey)
		}
	}
	return notices, marks
}

func localDay(now int64) string {
	return time.Unix(now, 0).Local().Format("2006-01-02")
}

func nextLocalMidnight(now int64) float64 {
	moment := time.Unix(now, 0).Local()
	next := time.Date(moment.Year(), moment.Month(), moment.Day()+1, 0, 0, 0, 0, moment.Location())
	return float64(next.Unix())
}

func dailyBudgetStatus(cfg, state object, usage usageView, now int64) (float64, float64, bool) {
	cap := numberOr(section(cfg, "budget"), "dailyWeeklyPercent", 0)
	if cap <= 0 || usage.sevenDay == nil {
		return 0, 0, false
	}
	day := getMap(state, "budgetDay")
	if getString(day, "day") != localDay(now) || numberOr(day, "weekResetsAt", -1) != usage.sevenDay.resetsAt {
		return 0, cap, false
	}
	usedToday := usage.sevenDay.used - numberOr(day, "startUsed", usage.sevenDay.used)
	return usedToday, cap, usedToday >= cap
}

func recordBudgetDay(usage usageView, now int64) {
	if usage.sevenDay == nil {
		return
	}
	day := localDay(now)
	state := readState()
	current := getMap(state, "budgetDay")
	if getString(current, "day") == day && numberOr(current, "weekResetsAt", -1) == usage.sevenDay.resetsAt {
		return
	}
	startUsed := usage.sevenDay.used
	if numberOr(current, "weekResetsAt", -1) != usage.sevenDay.resetsAt && getString(current, "day") == day {
		startUsed = 0
	}
	updateState(func(next object) {
		next["budgetDay"] = object{"day": day, "weekResetsAt": usage.sevenDay.resetsAt, "startUsed": startUsed}
	})
}

func notifierMissing(err error) bool {
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist)
}
