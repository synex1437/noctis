package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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

func queueFile(cfg object, dirs ...string) string {
	queue := section(cfg, "queue")
	if !getBool(queue, "enabled", true) {
		return ""
	}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		for _, raw := range getList(queue, "files") {
			name, _ := raw.(string)
			if name == "" {
				continue
			}
			file := filepath.Join(dir, name)
			if info := statSafe(file); info == nil || !info.Mode().IsRegular() {
				continue
			}
			if resolved, err := filepath.EvalSymlinks(file); err != nil || !linkedWithin(dir, resolved) {
				reportLinkedOutQueue(file, dir, resolved)
				continue
			}
			return file
		}
	}
	return ""
}

func reportLinkedOutQueue(file, dir, resolved string) {
	key := "queue:link:" + dir + " -> " + orDefault(resolved, file)
	first := false
	if getMap(readState(), "notified")[key] == nil {
		updateState(func(next object) {
			if notified := stateMap(next, "notified"); notified[key] == nil {
				notified[key], first = float64(nowSec()), true
			}
		})
	}
	report := logInfo
	if first {
		report = warn
	}
	report("queue file %s leads out of %s (to %s); it is not used as a queue", file, dir, resolved)
}

func linkedWithin(root, resolved string) bool {
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(base, resolved)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func queueDirs(input object) []string {
	return sessionDirs(claudeProjectDir(), getString(input, "cwd"))
}

func claudeProjectDir() string {
	project := os.Getenv("CLAUDE_PROJECT_DIR")
	if activeHost != "claude" || project == "" || !filepath.IsAbs(project) {
		return ""
	}
	return filepath.Clean(project)
}

func sessionDirs(project, cwd string) []string {
	if project == "" {
		return []string{cwd}
	}
	if cwd == "" || filepath.Clean(cwd) == project {
		return []string{project}
	}
	return []string{project, cwd}
}

func recordProjectDir(record object) {
	if project := claudeProjectDir(); project != "" {
		record["projectDir"] = project
	}
}

type queueView struct {
	total   int
	blocked int
	items   []string
	plain   bool
}

func parseQueueEntries(content string) ([]queueEntry, bool) {
	lines := strings.Split(strings.TrimPrefix(content, "\uFEFF"), "\n")
	hasBoxes, fenced := false, false
	for _, line := range lines {
		if queueFence(line) {
			fenced = !fenced
			continue
		}
		if !fenced && queueItemPattern.MatchString(strings.TrimRight(line, "\r")) {
			hasBoxes = true
			break
		}
	}
	type draft struct {
		checked bool
		text    strings.Builder
	}
	drafts := []*draft{}
	open := false
	fenced = false
	for _, line := range lines {
		if queueFence(line) {
			fenced, open = !fenced, false
			continue
		}
		if fenced {
			continue
		}
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))
		var text string
		var checked, ok bool
		if hasBoxes {
			text, checked, ok = queueLineParts(line)
		} else {
			text, checked, ok = queueBulletParts(line)
		}
		switch {
		case ok:
			item := &draft{checked: checked}
			item.text.WriteString(text)
			drafts = append(drafts, item)
			open = true
		case trimmed == "" || queueHeading.MatchString(trimmed):
			open = false

		case open && hasBoxes && queueBulletPattern.MatchString(trimmed):
			open = false
		case open:
			last := drafts[len(drafts)-1]
			last.text.WriteByte(' ')
			last.text.WriteString(trimmed)
		}
	}
	entries := make([]queueEntry, 0, len(drafts))
	for index, item := range drafts {
		entries = append(entries, newQueueEntry(index+1, item.text.String(), item.checked))
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

func queueFence(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

func queueLineParts(line string) (text string, checked, ok bool) {
	match := queueItemPattern.FindStringSubmatch(strings.TrimRight(line, "\r"))
	if match == nil {
		return "", false, false
	}
	return strings.TrimSpace(match[3]), match[1] != "", true
}

func queueBulletParts(line string) (text string, checked, ok bool) {
	match := queueBulletPattern.FindStringSubmatch(strings.TrimRight(line, "\r"))
	if match == nil {
		return "", false, false
	}
	text = strings.TrimSpace(match[1])
	return text, queueDoneMarker.MatchString(text), true
}

func parseQueueLine(line string, ordinal int) (queueEntry, bool) {
	text, checked, ok := queueLineParts(line)
	if !ok {
		return queueEntry{}, false
	}
	return newQueueEntry(ordinal, text, checked), true
}

func parseQueueBullet(line string, ordinal int) (queueEntry, bool) {
	text, checked, ok := queueBulletParts(line)
	if !ok {
		return queueEntry{}, false
	}
	return newQueueEntry(ordinal, text, checked), true
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
	doneByOrdinal := map[int]bool{}
	carriers, waitingOn := map[string]int{}, map[string]int{}
	marks := func(entry queueEntry) []string {
		keys := []string{}
		for tag := range entry.tags {
			keys = append(keys, "#"+tag)
		}
		if issue, found := itemIssue(entry.text); found {
			keys = append(keys, "issue:"+strings.ToLower(issue.ref()))
		}
		return keys
	}
	for _, entry := range entries {
		for _, key := range marks(entry) {
			carriers[key]++
			if !entry.checked {
				waitingOn[key]++
			}
		}
		doneByOrdinal[entry.ordinal] = entry.checked
	}
	satisfied := func(self queueEntry, reference string) bool {
		key := ""
		if at := strings.LastIndexByte(reference, '#'); at >= 0 {
			number := reference[at+1:]
			if _, err := strconv.Atoi(number); err == nil {
				key = "issue:" + strings.ToLower(issueID{repo: reference[:at], number: number}.ref())
			} else if at == 0 {
				key = reference
			}
		}
		if key != "" {
			others, waiting := carriers[key], waitingOn[key]
			if slices.Contains(marks(self), key) {
				others--
				if !self.checked {
					waiting--
				}
			}
			return others <= 0 || waiting <= 0
		}
		ordinal, err := strconv.Atoi(reference)
		if err != nil || ordinal < 1 || ordinal > len(entries) || ordinal == self.ordinal {
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
			if !satisfied(entry, reference) {
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

func dropFinishedTasks(items object) {
	for id, raw := range items {
		if getString(toObject(raw), "status") == "completed" {
			delete(items, id)
		}
	}
}

func dropOldestTasks(items object) {
	if len(items) <= openTaskLimit {
		return
	}
	keys := sortedKeys(items)
	sort.SliceStable(keys, func(a, b int) bool {
		return numberOr(toObject(items[keys[a]]), "at", 0) < numberOr(toObject(items[keys[b]]), "at", 0)
	})
	for _, id := range keys[:len(items)-openTaskLimit] {
		delete(items, id)
	}
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

type gitStatusCacheEntry struct {
	raw     string
	ok      bool
	takenAt time.Time
}

var gitStatusCache = map[string]gitStatusCacheEntry{}

func gitStatusRaw(cwd string) (string, bool) {
	if cached, seen := gitStatusCache[cwd]; seen && time.Since(cached.takenAt) < gitStatusCacheTTL {
		return cached.raw, cached.ok
	}
	raw, ok := gitStatusUncached(cwd)
	gitStatusCache[cwd] = gitStatusCacheEntry{raw: raw, ok: ok, takenAt: time.Now()}
	return raw, ok
}

func gitStatusUncached(cwd string) (string, bool) {
	if !insideGitRepo(cwd) {
		return "", false
	}
	command := exec.Command("git", "-c", "status.relativePaths=true", "-c", "color.status=false", "-c", "status.branch=false", "status", "--short")
	command.Dir = cwd
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
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
	digest := sha1.New()
	fmt.Fprintf(digest, "%s\x00%s", gitHead(cwd), raw)
	budget := treeStatLimit
	folders := []string{}
	for _, name := range statusPaths(raw) {
		if budget <= 0 {
			break
		}
		budget--
		if hashPathState(digest, cwd, name) {
			folders = append(folders, name)
		}
	}
	for _, name := range folders {
		if budget <= 0 {
			break
		}
		budget = hashFolderState(digest, cwd, name, budget)
	}
	return hex.EncodeToString(digest.Sum(nil)[:8])
}

func gitHead(cwd string) string {
	command := exec.Command("git", "rev-parse", "-q", "--verify", "HEAD")
	command.Dir = cwd
	output, err := runWithTimeout(command, 3*time.Second)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func statusPaths(raw string) []string {
	paths := []string{}
	for _, line := range strings.Split(raw, "\n") {
		if len(line) < 4 {
			continue
		}
		rest := line[3:]
		if strings.ContainsAny(line[:2], "RC") {
			if from, to, ok := splitStatusRename(rest); ok {
				paths = append(paths, from, to)
				continue
			}
		}
		paths = append(paths, unquoteStatusPath(rest))
	}
	return paths
}

func splitStatusRename(rest string) (string, string, bool) {
	end := strings.Index(rest, " -> ")
	if strings.HasPrefix(rest, `"`) {
		end = -1
		for i := 1; i < len(rest); i++ {
			if rest[i] == '\\' {
				i++
				continue
			}
			if rest[i] == '"' {
				end = i + 1
				break
			}
		}
	}
	if end < 0 || !strings.HasPrefix(rest[end:], " -> ") {
		return "", "", false
	}
	return unquoteStatusPath(rest[:end]), unquoteStatusPath(rest[end+len(" -> "):]), true
}

func unquoteStatusPath(field string) string {
	if len(field) >= 2 && field[0] == '"' && field[len(field)-1] == '"' {
		if unquoted, err := strconv.Unquote(field); err == nil {
			return unquoted
		}
	}
	return field
}

func hashPathState(digest io.Writer, cwd, name string) bool {
	info, err := os.Lstat(filepath.Join(cwd, filepath.FromSlash(name)))
	if err != nil {
		fmt.Fprintf(digest, "\x00%s\x00gone", name)
		return false
	}
	writePathState(digest, name, info)
	return info.IsDir()
}

func hashFolderState(digest io.Writer, cwd, name string, budget int) int {
	path := filepath.Join(cwd, filepath.FromSlash(name))
	_ = filepath.WalkDir(path, func(entryPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entryPath == path {
			return nil
		}
		if budget <= 0 {
			return filepath.SkipAll
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		budget--
		if entryInfo, infoErr := entry.Info(); infoErr == nil {
			relative, _ := filepath.Rel(cwd, entryPath)
			writePathState(digest, filepath.ToSlash(relative), entryInfo)
		}
		return nil
	})
	return budget
}

func writePathState(digest io.Writer, name string, info os.FileInfo) {
	fmt.Fprintf(digest, "\x00%s\x00%d\x00%d\x00%d", name, info.Size(), info.ModTime().UnixNano(), info.Mode())
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
	return runUntil(command, timeout, func() { _ = command.Process.Kill() })
}

func runTreeWithTimeout(command *exec.Cmd, timeout time.Duration) ([]byte, error) {
	isolateTree(command)
	return runUntil(command, timeout, func() { killTree(command.Process) })
}

func runUntil(command *exec.Cmd, timeout time.Duration, stop func()) ([]byte, error) {
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
		stop()
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
		if candidate := sessionQueueFile(cfg, sid, queueDirs(input)...); queueTrusted(cfg, candidate) {
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

var gitSnapshotTimeout = 15 * time.Second

func gitSnapshot(cwd, sid string) (ref, hash string) {
	if !insideGitRepo(cwd) {
		return "", ""
	}
	index, ok := gitIndexCopy(cwd)
	if !ok {
		return "", ""
	}
	defer func() {
		os.Remove(index)
		os.Remove(index + ".lock")
	}()
	create := exec.Command("git", "stash", "create", "noctis checkpoint "+sid)
	create.Dir = cwd
	create.Env = append(os.Environ(), "GIT_INDEX_FILE="+index)
	output, err := runTreeWithTimeout(create, gitSnapshotTimeout)
	if err != nil {
		removeIndexLeftovers(index)
	}
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

func removeIndexLeftovers(index string) {
	entries, err := os.ReadDir(filepath.Dir(index))
	if err != nil {
		return
	}
	prefix := filepath.Base(index) + ".stash."
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			os.Remove(filepath.Join(filepath.Dir(index), entry.Name()))
		}
	}
}

func gitIndexCopy(cwd string) (string, bool) {
	locate := exec.Command("git", "rev-parse", "--git-path", "index")
	locate.Dir = cwd
	output, err := runWithTimeout(locate, 5*time.Second)
	if err != nil {
		return "", false
	}
	source := strings.TrimSpace(string(output))
	if !filepath.IsAbs(source) {
		source = filepath.Join(cwd, source)
	}
	content, err := readFileShared(source)
	if err != nil {
		warn("git snapshot skipped: the index at %s cannot be read (%v)", source, err)
		return "", false
	}
	copied, err := os.CreateTemp("", "noctis-index-*")
	if err != nil {
		warn("git snapshot skipped: no temporary copy of the index (%v)", err)
		return "", false
	}
	_, writeErr := copied.Write(content)
	if closeErr := copied.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		os.Remove(copied.Name())
		warn("git snapshot skipped: no temporary copy of the index (%v)", writeErr)
		return "", false
	}
	return copied.Name(), true
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

func handOverCheckpoint(sid, receiver string) {
	updateState(func(state object) {
		if entry := getMap(getMap(state, "checkpoints"), sid); entry != nil {
			entry["consumed"] = true
			entry["handedTo"] = receiver
		}
	})
}

func checkpointHandedTo(state object, receiver, path string) bool {
	for _, raw := range getMap(state, "checkpoints") {
		if entry := toObject(raw); entry != nil && getString(entry, "handedTo") == receiver && getString(entry, "path") == path {
			return true
		}
	}
	return false
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
		if pid, ok := getNumber(scheduled, "pid"); ok && mayBeOurHelper(int(pid)) {
			return true
		}
	}
	return false
}

func mayBeOurHelper(pid int) bool {
	if !processAlive(pid) {
		return false
	}
	name := processName(pid)
	return name == "" || ownHelperProcess(name)
}

func checkpointUsable(entry object, cwd string, now int64) bool {
	return entry != nil &&
		!getBool(entry, "consumed", false) &&
		(cwd == "" || getString(entry, "cwd") == cwd) &&
		float64(now)-numberOr(entry, "at", 0) <= checkpointTTLSeconds
}

func latestCheckpointFor(state object, cwd string, now int64) (string, object) {
	return latestCheckpointWhere(state, cwd, now, func(string) bool { return true })
}

func checkpointForNewSession(state object, cwd string, now int64) (string, object) {
	return latestCheckpointWhere(state, cwd, now, func(sid string) bool {
		return !waitLive(getMap(getMap(state, "waits"), sid), now) && getMap(getMap(state, "handedOff"), sid) == nil
	})
}

func retireOwnCheckpoint(state object, sid string) {
	if entry := getMap(getMap(state, "checkpoints"), sid); entry != nil && !getBool(entry, "consumed", false) {
		consumeCheckpoint(sid)
	}
}

func latestCheckpointWhere(state object, cwd string, now int64, eligible func(sid string) bool) (string, object) {
	bestSid := ""
	var best object
	for sid, raw := range getMap(state, "checkpoints") {
		entry := toObject(raw)
		if !checkpointUsable(entry, cwd, now) || !eligible(sid) {
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

func scheduledWithoutTask(scheduled object) bool {
	switch getString(scheduled, "method") {
	case "sleeper", "manual", "systemd", "launchd":
		return true
	default:
		return false
	}
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

func cancelRunnerKeepingTask(sid string, state object, keepTask bool) {
	scheduled := getMap(getMap(getMap(state, "waits"), sid), "scheduled")
	cancelScheduledKeepingTask(sid, scheduled, keepTask)
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
	cancelScheduledKeepingTask(sid, scheduled, false)
}

func cancelScheduledKeepingTask(sid string, scheduled object, keepTask bool) {
	if isWindows && !keepTask && scheduled != nil && !scheduledWithoutTask(scheduled) {
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
	name := processName(int(pid))
	if name == "" {
		warn("%s %d cannot be identified; left alone", what, int(pid))
		return
	}
	if !ownHelperProcess(name) {
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
		`@"%SystemRoot%\System32\chcp.com" 65001>nul`,
		fmt.Sprintf(`"%s" %%*`, executable),
		"exit /b %errorlevel%",
		"",
	}, "\r\n")
	existing, _ := readFileShared(files.runnerLauncher)
	if string(existing) != content {
		if err := writeRunnerLauncher([]byte(content)); err != nil {
			fail("runner launcher not written (%s): %v", files.runnerLauncher, err)
		}
	}
	return files.runnerLauncher
}

func writeRunnerLauncher(content []byte) error {
	staging := fmt.Sprintf("%s.%d.tmp", files.runnerLauncher, os.Getpid())
	if err := os.WriteFile(staging, content, 0o644); err != nil {
		return err
	}
	var err error
	for attempt := 0; attempt < 8; attempt++ {
		if err = renameAtomic(staging, files.runnerLauncher); err == nil {
			return nil
		}
		time.Sleep(time.Duration(10+attempt*10) * time.Millisecond)
	}
	writeErr := os.WriteFile(files.runnerLauncher, content, 0o644)
	if removeErr := os.Remove(staging); removeErr != nil {
		warn("temp file left behind: %s", staging)
	}
	if writeErr != nil {
		return writeErr
	}
	warn("atomic rename of %s failed after retries (%v); wrote in place", filepath.Base(files.runnerLauncher), err)
	return nil
}

func scheduleWindowsTask(name string, at float64, commandArgs []string, wake bool) shellResult {
	return runPowershell(windowsTaskScript(name, at, commandArgs, wake), 45*time.Second)
}

func windowsShellArgument(line string) string {
	return `/d /s /c "` + line + `"`
}

func windowsTaskArgument(launcher string, commandArgs []string) string {
	return windowsShellArgument(`"` + launcher + `" ` + strings.Join(commandArgs, " "))
}

func windowsTaskScript(name string, at float64, commandArgs []string, wake bool) string {
	launcher := ensureRunnerLauncher()
	argument := windowsTaskArgument(launcher, commandArgs)
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

func scheduleLockFile(sid string) string {
	return filepath.Join(files.guardDir, "schedule-"+hashKey(sid)+".lock")
}

func scheduleRunner(cfg object, sid string, atEpoch float64) object {
	var scheduled object
	withFileLock(scheduleLockFile(sid), func() {
		scheduled = scheduleRunnerLocked(cfg, sid, atEpoch, 0)
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

func scheduleRunnerLocked(cfg object, sid string, atEpoch, rearms float64) object {
	now := nowSec()
	at := math.Max(atEpoch, float64(now+15))
	nativeAllowed := os.Getenv("NOCTIS_NO_TASKS") == ""
	backend := schedulerBackend()
	replacingTask := nativeAllowed && backend == "task"
	cancelRunnerKeepingTask(sid, readState(), replacingTask)
	var scheduled object
	if replacingTask {
		result := scheduleWindowsTask(taskName(sid), at, runnerArgs("resume", sid, `"`+files.configDir+`"`), getBool(section(cfg, "alarm"), "wakePc", true))
		if result.ok {
			scheduled = object{"method": "task", "taskName": taskName(sid), "at": at, "watcherPid": float64(startResetWatcher(cfg, sid, at))}
		} else {
			removeScheduledTask(taskName(sid))
			warn("task scheduling failed, falling back to sleeper: %s", orDefault(result.err, result.stderr))
		}
	} else if nativeAllowed {
		if native, ok := scheduleNative(backend, sid, at, runnerArgs("resume", sid, files.configDir), getBool(section(cfg, "alarm"), "wakePc", true)); ok {
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
			fail("no scheduler available for %s; resume manually with %s after %s", sid, hostResumeCommand(currentHost().id, sid), localISO(at))
		}
	}
	if rearms > 0 {
		scheduled["rearms"] = rearms
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

func prepareWait(sid string, record object, cfg object) {
	clearQuietMarker(sid)
	if cfg != nil && getString(record, "launchMode") == "" {
		if mode := inferLaunchMode(cfg, sid); mode != "" {
			record["launchMode"] = mode
		}
	}
	record["configDirEnv"] = os.Getenv(claudeConfigEnv)
	recordProjectDir(record)
}

func registerWait(sid string, record object, cfg object) bool {
	prepareWait(sid, record, cfg)
	var replaced object
	written := false
	updateState(func(state object) {
		waits := stateMap(state, "waits")
		replaced, written = getMap(waits, sid), true
		waits[sid] = record
	})
	if !written || getMap(getMap(readState(), "waits"), sid) == nil {
		fail("wait for %s could not be stored; not pausing the session", sid)
		cancelScheduled(sid, getMap(record, "scheduled"))
		return false
	}
	if previous := getMap(replaced, "scheduled"); !sameSchedule(previous, getMap(record, "scheduled")) {
		cancelScheduled(sid, previous)
	}
	return true
}

const (
	joinFreshSeconds = 120
	joinSlackSeconds = 2
)

func sameReset(current object, window string, until float64) bool {
	return current != nil && getString(current, "window") == window && math.Abs(numberOr(current, "until", 0)-until) <= joinSlackSeconds
}

func joinableWait(current object, window string, until float64) bool {
	return sameReset(current, window, until) && float64(nowSec())-numberOr(current, "startedAt", 0) < joinFreshSeconds
}

func sameWait(record object, startedAt float64, holder string) bool {
	return record != nil && numberOr(record, "startedAt", -1) == startedAt && getString(record, "holder") == holder
}

func claimWait(kind, sid string, record object, cfg object) (object, bool) {
	prepareWait(sid, record, cfg)
	var current object
	joined := false
	updateState(func(state object) {
		waits := stateMap(state, "waits")
		current = getMap(waits, sid)
		joined = kind != "prompt" && joinableWait(current, getString(record, "window"), numberOr(record, "until", 0))
		if !joined {
			waits[sid] = record
		}
	})
	if joined {
		return current, true
	}
	stored := getMap(getMap(readState(), "waits"), sid)
	if !sameWait(stored, numberOr(current, "startedAt", -1), getString(current, "holder")) {
		cancelScheduled(sid, getMap(current, "scheduled"))
	}
	if stored == nil {
		fail("wait for %s could not be stored; not pausing the session", sid)
		return nil, false
	}
	return record, false
}

type waitHold struct {
	watch     *waitWatch
	owned     bool
	cancelled bool
	stop      string
}

func holdWait(kind, sid string, cfg object, wait *waitPlan, resumeAt float64, held object, owned bool) waitHold {
	for {
		startedAt, holder, heldResumeAt := numberOr(held, "startedAt", -1), getString(held, "holder"), numberOr(held, "resumeAt", resumeAt)
		epoch := math.Max(resumeAt, heldResumeAt)
		watch := newWaitWatch(cfg, sid, owned)
		watch.startedAt = startedAt
		reached := sleepUntilEvery(epoch, watch.tickSeconds(), func() bool {
			if current := getMap(getMap(readState(), "waits"), sid); current != nil && !sameWait(current, startedAt, holder) {
				return true
			}
			return watch.tick()
		})
		ended := reached || watch.early || watch.dataBack
		var current, scheduled object
		updateState(func(next object) {
			waits := stateMap(next, "waits")
			current = getMap(waits, sid)
			mine := sameWait(current, startedAt, holder)
			if !owned || (current != nil && !mine) || (mine && !ended) {
				return
			}
			if mine {
				scheduled = getMap(current, "scheduled")
				delete(waits, sid)
			}
			if entry := getMap(getMap(next, "checkpoints"), sid); entry != nil {
				entry["consumed"] = true
			}
		})
		mine := sameWait(current, startedAt, holder)
		if mine && !ended {
			continue
		}
		if current == nil || mine {
			cancelScheduled(sid, scheduled)
			hold := waitHold{watch: watch, owned: owned}
			if current == nil && !ended {
				if owned {
					hold.cancelled = true
				} else if float64(nowSec()) >= heldResumeAt {
					sleepUntil(epoch, nil)
				}
			}
			return hold
		}
		if !sameReset(current, wait.window, wait.until) || !getBool(current, "inHook", false) {
			journal(sid, kind, "wait-replaced", hitLabel(wait), object{"window": getString(current, "window")})
			logInfo("in-hook wait for %s was replaced by a %s pause it cannot wait out; stopping", sid, getString(current, "window"))
			return waitHold{watch: watch, stop: savedNotice(cfg, orDefault(getString(current, "label"), wait.label), formatNumber(numberOr(current, "used", wait.used)), formatTime(numberOr(current, "resumeAt", resumeAt)), "")}
		}
		journal(sid, kind, "join-wait", hitLabel(wait), object{"window": wait.window})
		held, owned = current, false
	}
}

func joinWait(kind, sid string, cfg object, wait *waitPlan, current object, resumeAt float64, inHook bool, now int64) waitOutcome {
	journal(sid, kind, "join-wait", hitLabel(wait), object{"window": wait.window})
	if !inHook || !getBool(current, "inHook", false) {
		return waitOutcome{stop: savedNotice(cfg, wait.label, formatNumber(wait.used), formatTime(numberOr(current, "resumeAt", resumeAt)), "")}
	}
	if hold := holdWait(kind, sid, cfg, wait, resumeAt, current, false); hold.stop != "" {
		return waitOutcome{stop: hold.stop}
	}
	return waitOutcome{notice: T("wait.resumed", wait.label, formatNumber(wait.used), durationText(float64(nowSec()-now)))}
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
	case isWindows && files.notifyScript == "":
		if float64(nowSec())-numberOr(getMap(readState(), "notified"), "notify:untrusted", 0) > 86400 {
			updateState(func(next object) { stateMap(next, "notified")["notify:untrusted"] = float64(nowSec()) })
			warn("no toast: %s is not the plugin folder of this binary, so its scripts\\notify.ps1 is not run", files.pluginRoot)
		}
	case isWindows:
		child = exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", files.notifyScript, "-Title", title, "-Body", body)
	case runtime.GOOS == "darwin":
		clean := func(text string) string { return strings.NewReplacer(`"`, "'", `\`, "'").Replace(text) }
		child = exec.Command("osascript", "-e", fmt.Sprintf(`display notification "%s" with title "%s" sound name "Glass"`, clean(body), clean(title)))
	default:
		child = exec.Command("notify-send", title, body)
	}
	if child != nil {
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
	fallback := getString(models, "fallback")
	replaced := settingsModel()
	if replaced != fallback {
		setSettingsModel(fallback)
	}
	previousEffort := setSettingsEffort(getString(getMap(section(cfg, "roles"), "fallback"), "effort"))
	updateState(func(state object) {
		switched := object{"at": float64(now), "from": orDefault(replaced, getString(models, "primary")), "to": fallback, "fableResetsAt": fableResetsAt}
		if earlier := getMap(state, "modelSwitched"); earlier != nil && replaced == fallback {
			switched["from"] = orDefault(getString(earlier, "from"), getString(switched, "from"))
			if previousEffort == "" {
				previousEffort = getString(earlier, "effortWas")
			}
		}
		if previousEffort != "" {
			switched["effortWas"] = previousEffort
		}
		state["modelSwitched"] = switched
	})
}

func isHandoffSession(sid string) bool {
	return os.Getenv(handoffEnv) == sid
}

func hookSleeping(wait object) bool {
	return getBool(wait, "inHook", false) || numberOr(wait, "waking", 0) > 0
}

func holderAlive(wait object) bool {
	id, _, _ := strings.Cut(getString(wait, "holder"), "-")
	pid, err := strconv.Atoi(id)
	if err != nil || !processAlive(pid) {
		return false
	}
	if float64(nowSec())-numberOr(wait, "heartbeat", 0) < heartbeatFreshSeconds {
		return true
	}
	return pid != os.Getpid() && ownHelperProcess(processName(pid))
}

func releaseInterruptedWait(sid string, state object) {
	clearDeadHandoffs(state)
	dropInterruptedWait(sid, state)
	rearmStrandedWaits(state)
}

func dropInterruptedWait(sid string, state object) {
	wait := getMap(getMap(state, "waits"), sid)
	if wait == nil || getMap(getMap(state, "handedOff"), sid) != nil {
		return
	}
	if !hookSleeping(wait) {
		return
	}
	if holderAlive(wait) {
		logInfo("in-hook wait for %s is still held by live hook %s; left alone", sid, getString(wait, "holder"))
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

const (
	runnerOverdueSeconds = 600
	strandedRearmLimit   = 3
)

func rescheduleStrandedWaits(state object) {
	clearDeadHandoffs(state)
	rearmStrandedWaits(state)
}

func rearmStrandedWaits(state object) {
	thorough := strings.EqualFold(activeEvent, "SessionStart")
	var cfg object
	for sid, raw := range getMap(state, "waits") {
		record := toObject(raw)
		if record == nil || getMap(getMap(state, "handedOff"), sid) != nil || strandedBecause(sid, record, nowSec(), thorough) == "" {
			continue
		}
		if cfg == nil {
			cfg = loadConfig()
		}
		rearmStrandedWait(cfg, sid, record, thorough)
	}
}

func strandedBecause(sid string, record object, now int64, thorough bool) string {
	if numberOr(record, "resumeAt", 0) <= 0 || float64(now)-numberOr(record, "startedAt", 0) < schedulingGraceSeconds {
		return ""
	}
	if hookSleeping(record) && float64(now)-math.Max(numberOr(record, "heartbeat", 0), numberOr(record, "waking", 0)) < heartbeatFreshSeconds {
		return ""
	}
	scheduled := getMap(record, "scheduled")
	if scheduled == nil {
		if hookSleeping(record) {
			return "its hook died before scheduling a runner"
		}
		return "no runner was scheduled"
	}
	if numberOr(scheduled, "rearms", 0) >= strandedRearmLimit {
		return ""
	}
	at := numberOr(scheduled, "at", 0)
	overdue := at > 0 && float64(now)-at > runnerOverdueSeconds
	switch getString(scheduled, "method") {
	case "sleeper":
		pid, ok := getNumber(scheduled, "pid")
		switch {
		case !ok || pid <= 0:
		case !processAlive(int(pid)):
			return "its sleeper is gone"
		case thorough:
			if name := processName(int(pid)); name != "" && !ownHelperProcess(name) {
				return fmt.Sprintf("its sleeper pid now runs %q", name)
			}
		}
	case "systemd":
		if overdue {
			return "its runner is overdue"
		}
	case "launchd":
		label := getString(scheduled, "label")
		if !launchdJobOf(sid, label) {
			label = launchdLabel(sid)
		}
		if overdue && statSafe(launchdPlist(label)) != nil {
			return "its runner is overdue"
		}
	case "task":
		if overdue && thorough {
			return "its runner is overdue"
		}
	}
	return ""
}

func rearmStrandedWait(cfg object, sid string, seen object, thorough bool) {
	withFileLock(scheduleLockFile(sid), func() {
		state := readState()
		record := getMap(getMap(state, "waits"), sid)
		if getMap(getMap(state, "handedOff"), sid) != nil || !sameWait(record, numberOr(seen, "startedAt", -1), getString(seen, "holder")) {
			return
		}
		reason := strandedBecause(sid, record, nowSec(), thorough)
		if reason == "" {
			return
		}
		scheduled := getMap(record, "scheduled")
		resumeAt := numberOr(record, "resumeAt", 0)
		at := resumeAt
		if getBool(record, "inHook", false) {
			at += math.Max(0, numberOr(section(cfg, "wait"), "builtinGraceSeconds", 0))
		}
		if numberOr(record, "waking", 0) > 0 {
			at += math.Max(60, numberOr(section(cfg, "wake"), "graceSeconds", 300))
		}
		journal(sid, "repair", "reschedule", reason, object{"resumeAt": resumeAt})
		warn("wait for %s: %s; rescheduling", sid, reason)
		scheduleRunnerLocked(cfg, sid, math.Max(at, numberOr(scheduled, "at", 0)), numberOr(scheduled, "rearms", 0)+1)
	})
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
	case "ceiling":
		if wait.used < wait.threshold {
			return T("hit.ceilingSoon")
		}
		return T("hit.ceiling")
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

const (
	hookBudgetSlackSeconds = 30
	minInHookBudgetSeconds = 90
)

var hookBudgets = map[string]map[string]float64{
	"claude": {
		"SessionStart": 20, "SessionEnd": 10, "UserPromptSubmit": 21600, "PreToolUse": 21600, "PostToolBatch": 21600, "StopFailure": 21600,
		"Notification": 20, "PostModelSwitch": 10, "TaskCreated": 10, "TaskCompleted": 10, "PostToolUse": 15, "Stop": 21600,
		"PermissionRequest": 10,
	},
	"codex":       {"SessionStart": 20, "SessionEnd": 3, "UserPromptSubmit": 21600, "PreToolUse": 20, "PostToolUse": 21600, "Stop": 60},
	"droid":       {"SessionStart": 20, "SessionEnd": 10, "UserPromptSubmit": 21600, "PreToolUse": 20, "PostToolUse": 21600, "Stop": 60},
	"antigravity": {"PreToolUse": 20, "PreInvocation": 21600, "PostInvocation": 21600, "Stop": 60},
	"copilot":     {"sessionStart": 20, "sessionEnd": 10, "userPromptSubmitted": 21600, "preToolUse": 20, "agentStop": 60, "errorOccurred": 20},
}

func hookBudget(host, event string) (float64, bool) {
	for name, seconds := range hookBudgets[host] {
		if strings.EqualFold(name, event) {
			return seconds, true
		}
	}
	return 0, false
}

func waitsInHook(waitCfg object, learnedCap, remaining float64) bool {
	limit := math.Max(1, numberOr(waitCfg, "maxInHookMinutes", 0)) * 60
	if learnedCap > 0 {
		limit = math.Min(limit, math.Max(60, learnedCap-60))
	}
	if budget, known := hookBudget(activeHost, activeEvent); known {
		if budget < minInHookBudgetSeconds {
			return false
		}
		limit = math.Min(limit, budget-hookBudgetSlackSeconds)
	}
	return remaining <= limit
}

func enforceWait(kind string, input object, cfg object, result decision) waitOutcome {
	now := nowSec()
	sid := sessionKey(input)
	wait := result.wait
	waitCfg := section(cfg, "wait")
	resumeAt := wait.until + math.Max(0, numberOr(waitCfg, "resetMarginSeconds", 0))
	learnedCap := numberOr(readState(), "hookCapSeconds", 0)
	inHook := waitsInHook(waitCfg, learnedCap, resumeAt-float64(now))
	if current := getMap(getMap(readState(), "waits"), sid); kind != "prompt" && joinableWait(current, wait.window, wait.until) {
		return joinWait(kind, sid, cfg, wait, current, resumeAt, inHook, now)
	}
	reasonLine := T("wait.reason", wait.label, formatNumber(wait.used), hitLabel(wait), formatTime(wait.until))
	checkpoint := buildCheckpoint(input, reasonLine, result.model, cfg)
	queuedPrompt := ""
	if kind == "prompt" {
		queuedPrompt = truncateText(getString(input, "prompt"), 4000)
	}
	record := object{
		"kind": kind, "window": wait.window, "label": wait.label, "used": wait.used, "threshold": wait.threshold, "hit": wait.hit, "cause": wait.cause,
		"until": wait.until, "resumeAt": resumeAt, "inHook": inHook, "cwd": getString(input, "cwd"),
		"transcript": getString(input, "transcript_path"), "checkpoint": checkpoint, "queuedPrompt": queuedPrompt,
		"startedAt": float64(now), "heartbeat": float64(now), "permissionMode": permissionModeOf(input),
		"holder": strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36),
	}
	recordTree(cfg, record, getString(input, "cwd"))
	runnerAt := resumeAt
	if inHook {
		runnerAt += math.Max(0, numberOr(waitCfg, "builtinGraceSeconds", 0))
	}

	stored, joined := claimWait(kind, sid, record, cfg)
	if stored == nil {
		journal(sid, kind, "pause-failed", reasonLine, object{"window": wait.window, "used": wait.used})
		return waitOutcome{notice: T("wait.notStored", pluginName)}
	}
	if joined {
		return joinWait(kind, sid, cfg, wait, stored, resumeAt, inHook, now)
	}
	scheduled := scheduleRunner(cfg, sid, runnerAt)
	record["scheduled"] = scheduled
	updateState(func(next object) {
		if current := getMap(getMap(next, "waits"), sid); sameWait(current, float64(now), getString(record, "holder")) {
			current["scheduled"] = scheduled
		}
	})
	journal(sid, kind, "pause", reasonLine, object{"inHook": inHook, "resumeAt": resumeAt, "hit": wait.hit, "window": wait.window, "used": wait.used})
	logInfo("wait (%s) for %s: %s; inHook=%t; checkpoint=%s", kind, sid, reasonLine, inHook, orDefault(checkpoint, "none"))
	if inHook {
		hold := holdWait(kind, sid, cfg, wait, resumeAt, record, true)
		watch := hold.watch
		switch {
		case hold.stop != "":
			return waitOutcome{stop: hold.stop}
		case hold.cancelled:
			journal(sid, kind, "wait-cancelled", hitLabel(wait), nil)
			logInfo("in-hook wait for %s cancelled; continuing", sid)
			return waitOutcome{notice: T("wait.cancelled", wait.label)}
		case !hold.owned:
			return waitOutcome{notice: T("wait.resumed", wait.label, formatNumber(wait.used), durationText(float64(nowSec()-now)))}
		case watch.dataBack:
			journal(sid, kind, "data-back", hitLabel(wait), object{"waited": float64(nowSec() - now)})
			notify(cfg, pluginName, T("wait.dataReady", wait.label, T("wait.readyTail")))
			logInfo("in-hook wait for %s ended: fresh %s data shows room", sid, wait.window)
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
	return waitOutcome{stop: savedStop(cfg, kind, wait, resumeAt, suffix)}
}

func savedStop(cfg object, kind string, wait *waitPlan, resumeAt float64, suffix string) string {
	stop := savedNotice(cfg, wait.label, formatNumber(wait.used), formatTime(resumeAt), suffix)
	if kind == "prompt" && wait.hit != "ceiling" {
		stop += " " + T("wait.savedHint")
	}
	return stop
}

func savedNotice(cfg object, label, used, at, suffix string) string {
	if getString(section(cfg, "resume"), "mode") == "none" {
		return T("wait.savedManual", label, used, at)
	}
	return T("wait.saved", label, used, at, suffix)
}

func blindWindow(cfg object, snapshot usageView) (key string, threshold float64, guarded bool) {
	fiveLimit, fiveGuarded := stopPoint(cfg, "session5h")
	weekLimit, weekGuarded := stopPoint(cfg, "weeklyAll")
	switch {
	case fiveGuarded && snapshot.fiveHour != nil && snapshot.fiveHour.used >= fiveLimit-nearEdgeBand:
		return "five_hour", fiveLimit, true
	case weekGuarded && snapshot.sevenDay != nil && snapshot.sevenDay.used >= weekLimit-nearEdgeBand:
		return "seven_day", weekLimit, true
	}
	return "seven_day", weekLimit, false
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
	if !registerWait(sid, record, cfg) {
		return T("wait.notStored", pluginName)
	}
	scheduleRunner(cfg, sid, float64(now+20))
	return T("scoped.savedRelaunch", label, formatNumber(fableUsed), fallback)
}

func maybeRevertDefaultModel(cfg object, state object, usage usageView, now int64) string {
	switched := getMap(state, "modelSwitched")
	if switched == nil || !getBool(section(cfg, "fable"), "revertOnReset", true) {
		return ""
	}
	cleared := false
	if limit, guarded := scopedThresholdEnabled(cfg); guarded && usage.fable != nil {
		cleared = usage.fable.used < limit/2
	} else {
		cleared = float64(now) > numberOr(switched, "fableResetsAt", 0)
	}
	if !cleared {
		return ""
	}
	models := section(cfg, "models")
	target := orDefault(getString(switched, "from"), getString(models, "primary"))
	if settingsModel() == orDefault(getString(switched, "to"), getString(models, "fallback")) {
		setSettingsModel(target)
	}
	setSettingsEffort(getString(switched, "effortWas"))
	updateState(func(next object) { next["modelSwitched"] = nil })
	logInfo("fable window cleared: default model reverted to %s", target)
	return T("scoped.reverted", scopedLabel(cfg), target)
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
	fableLimit, fableGuarded := scopedThresholdEnabled(cfg)
	fableEdge := fableCandidate && fableGuarded && snapshot.fable != nil && snapshot.fable.used >= fableLimit-nearEdgeBand
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
			key, threshold, guarded := blindWindow(cfg, snapshot)
			label := windowLabel(key)
			win := snapshot.byKey(key)
			blind := evaluate(cfg, usage, model, contextPercent, hasContext)
			blind.usageStale, blind.contextPercent, blind.hasContext = true, contextPercent, hasContext
			if blind.wait == nil && win != nil && guarded {
				blind.wait = &waitPlan{window: key, label: label, used: win.used, threshold: threshold, until: win.resetsAt, hit: "blind"}
			}
			used := 0.0
			if win != nil {
				used = win.used
			}
			if blind.wait != nil {
				fail("no usage data for %d min while at %s%% (%s); pausing until data or reset", int((float64(nowSec())-snapshot.updatedAt)/60+0.5), formatNumber(used), label)
			}
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
