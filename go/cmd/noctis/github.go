package main

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const issueRepoPattern = `(?:\w[\w-]*(?:\.[\w-]+)+/)?\w[\w.-]*/[\w.-]+`

var (
	issueRef      = lazyRegexp(`(?i)^(?:\(p\d\)\s*)?(` + issueRepoPattern + `)?#(\d+)\b`)
	issueRepoName = lazyRegexp(`^` + issueRepoPattern + `$`)

	priorityLabel = lazyRegexp(`(?i)^(?:priorit(?:y|ies)|prio|importance|sev(?:erity)?)?[/:_ -]*p([0-9])$`)

	priorityAnchor = lazyRegexp(`(?i)(priorit|prio|importance|severity|sev)`)
	labelWords     = lazyRegexp(`[^a-z0-9]+`)
)

func priorityFromLabels(labels []string) string {
	for _, label := range labels {
		if found := priorityLabel.FindStringSubmatch(strings.TrimSpace(label)); found != nil {
			return "(P" + found[1] + ") "
		}
	}

	for _, label := range labels {
		lower := strings.ToLower(strings.TrimSpace(label))
		words := labelWords.Split(lower, -1)
		meaningful := 0
		for _, word := range words {
			if word != "" {
				meaningful++
			}
		}
		if meaningful > 1 && !priorityAnchor.MatchString(lower) {
			continue
		}
		for _, word := range words {
			switch word {
			case "critical", "urgent", "blocker", "p0":
				return "(P0) "
			case "high", "important":
				return "(P1) "
			case "medium", "normal":
				return "(P3) "
			case "low", "minor":
				return "(P7) "
			}
		}
	}
	return ""
}

type issueID struct {
	repo   string
	number string
}

func (issue issueID) String() string { return issue.repo + "#" + issue.number }

func (issue issueID) ref() string {
	if issue.repo == "" {
		return issue.number
	}
	return strings.TrimPrefix(strings.ToLower(issue.repo), "github.com/") + "#" + issue.number
}

func itemIssue(text string) (issueID, bool) {
	found := issueRef.FindStringSubmatch(text)
	if found == nil {
		return issueID{}, false
	}
	return issueID{repo: found[1], number: found[2]}, true
}

func (issue issueID) trustedHost() bool {
	if strings.Count(issue.repo, "/") < 2 {
		return true
	}
	host, _, _ := strings.Cut(strings.ToLower(issue.repo), "/")
	return host == "github.com" || host == strings.ToLower(strings.TrimSpace(os.Getenv("GH_HOST")))
}

type bareIssueItem struct {
	line int
	hash int
	text string
}

func importedIssueTitle(issue object) string {
	return strings.TrimSpace(strings.ReplaceAll(getString(issue, "title"), "\n", " "))
}

func disarmedTitle(title string) string {
	title = queuePriority.ReplaceAllString(title, "[P$1]")
	title = queueAfter.ReplaceAllString(title, "[after $1]")
	return queueTag.ReplaceAllString(title, "$1")
}

func titledAs(text, title string) bool {
	tail, found := strings.CutPrefix(text, title)
	if !found || (tail != "" && tail[0] != ' ' && tail[0] != '\t') {
		return false
	}
	for _, annotation := range []*lazyRe{queueAfter, queuePriority, queueTag} {
		tail = annotation.ReplaceAllString(tail, "")
	}
	return strings.TrimSpace(tail) == ""
}

func importRepo(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", true
	}
	if rest, scp := strings.CutPrefix(value, "git@"); scp && !strings.Contains(value, "://") {
		value = "ssh://git@" + strings.Replace(rest, ":", "/", 1)
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return "", false
		}
		value = strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.") + "/" + strings.TrimSuffix(strings.Trim(parsed.Path, "/"), ".git")
	}
	return value, issueRepoName.MatchString(value)
}

func ghCommand(cwd string, arguments ...string) ([]byte, error) {
	command := exec.Command("gh", arguments...)
	command.Dir = cwd
	return runWithTimeout(command, 20*time.Second)
}

func runQueueTrust(cfg object, cwd, action string) {
	target := flagString("file")
	if target == "" {

		if target = queueFileFor(cfg, cwd, flagString("sid")); target == "" {
			fmt.Println(T("queue.trustNone", strings.Join(queueFileNames(cfg), ", ")))
			return
		}
	}
	if absolute, err := filepath.Abs(target); err == nil {
		target = absolute
	}
	switch action {
	case "trust":
		trustQueueFile(target, true)
		fmt.Println(T("queue.trustGranted", target))
	case "untrust":
		trustQueueFile(target, false)
		fmt.Println(T("queue.trustRevoked", target))
	default:
		if queueTrusted(cfg, target) {
			fmt.Println(T("queue.trustGranted", target))
			return
		}
		fmt.Println(T("queue.trustAsk", filepath.Base(target), queueSnapshot(target).total, pluginName))
	}
}

func runQueue() {
	action := positional(1)
	if action != "import" && action != "trust" && action != "untrust" && action != "status" {
		fmt.Fprintln(os.Stderr, T("queue.usage"))
		os.Exit(2)
	}
	cwd := flagString("cwd")
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	cfg := loadConfig()
	if action != "import" {
		runQueueTrust(cfg, cwd, action)
		return
	}
	target := flagString("file")
	if target == "" {
		if target = queueFile(cfg, cwd); target == "" {
			target = filepath.Join(cwd, "TASKS.md")
		}
	}
	limit := "200"
	if value, ok := toNumber(flagString("limit")); ok && value >= 1 {
		limit = formatNumber(value)
	}
	repo, valid := importRepo(flagString("repo"))
	if !valid {
		fmt.Fprintln(os.Stderr, T("queue.usage"))
		os.Exit(2)
	}
	arguments := []string{"issue", "list", "--state", "open", "--limit", limit, "--json", "number,title,labels"}
	if repo != "" {
		arguments = append(arguments, "--repo", repo)
	}
	if label := flagString("label"); label != "" {
		arguments = append(arguments, "--label", label)
	}
	output, err := ghCommand(cwd, arguments...)
	if err != nil {
		fmt.Fprintln(os.Stderr, T("queue.ghFailed", err))
		os.Exit(1)
	}
	var issues []any
	if err := jsonUnmarshal(output, &issues); err != nil {
		fmt.Fprintln(os.Stderr, T("queue.ghFailed", err))
		os.Exit(1)
	}
	existing, _ := os.ReadFile(target)
	fileLines := strings.Split(string(existing), "\n")
	known, bare := map[string]bool{}, map[string][]bareIssueItem{}
	for index, line := range fileLines {
		entry, ok := parseQueueLine(line, 0)
		if !ok {
			entry, ok = parseQueueBullet(line, 0)
		}
		issue, found := itemIssue(entry.text)
		if !ok || !found {
			continue
		}
		known[issue.ref()] = true
		if repo == "" || issue.repo != "" {
			continue
		}
		if start := strings.Index(line, entry.text); start >= 0 {
			hash := start + strings.IndexByte(entry.text, '#')
			bare[issue.number] = append(bare[issue.number], bareIssueItem{line: index, hash: hash, text: strings.TrimSpace(line[hash+1+len(issue.number):])})
		}
	}
	lines, qualified := []string{}, 0
	for _, raw := range issues {
		issue := toObject(raw)
		number, ok := getNumber(issue, "number")
		if issue == nil || !ok {
			continue
		}
		id, original := issueID{repo: repo, number: formatNumber(number)}, importedIssueTitle(issue)
		title := disarmedTitle(original)
		if known[id.ref()] {
			continue
		}
		known[id.ref()] = true
		present := false
		for _, item := range bare[id.number] {
			if titledAs(item.text, title) || titledAs(item.text, original) {
				fileLines[item.line] = fileLines[item.line][:item.hash] + repo + fileLines[item.line][item.hash:]
				qualified++
				present = true
			}
		}
		if present {
			continue
		}
		labels := []string{}
		for _, rawLabel := range getList(issue, "labels") {
			labels = append(labels, getString(toObject(rawLabel), "name"))
		}
		lines = append(lines, fmt.Sprintf("- [ ] %s%s %s", priorityFromLabels(labels), id, title))
	}
	content := strings.Join(fileLines, "\n")
	if len(lines) > 0 {
		if content == "" {
			content = "# TASKS\n"
		}
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		if !strings.Contains(content, "## GitHub issues") {
			content += "\n## GitHub issues\n"
		}
		content += strings.Join(lines, "\n") + "\n"
	}
	if content != string(existing) {
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if qualified > 0 {
		logInfo("queue import: %d item(s) in %s that an older import wrote as a bare #N now name %s", qualified, filepath.Base(target), repo)
	}
	if len(lines) == 0 {
		fmt.Println(T("queue.importNone", len(issues), filepath.Base(target)))
		return
	}
	fmt.Println(T("queue.importDone", len(lines), len(issues)-len(lines), filepath.Base(target)))
}

func syncDoneIssues(cfg object, queuePath, cwd string) {
	github := getMap(section(cfg, "queue"), "github")
	if !getBool(github, "closeOnDone", false) {
		return
	}
	content, err := os.ReadFile(queuePath)
	if err != nil {
		return
	}
	open, checked := map[string]bool{}, map[string]issueID{}
	for _, line := range strings.Split(string(content), "\n") {
		entry, ok := parseQueueLine(line, 0)
		issue, found := itemIssue(entry.text)
		if !ok || !found {
			continue
		}
		if entry.checked {
			checked[issue.ref()] = issue
		} else {
			open[issue.ref()] = true
		}
	}
	if len(open) == 0 && len(checked) == 0 {
		return
	}
	prefix := queuePath + "#"
	pending := []issueID{}
	changed := false
	updateState(func(state object) {
		seen := stateMap(state, "githubSeen")
		status := func(key string) string { return getString(getMap(seen, key), "status") }
		for _, ref := range sortedKeys(checked) {
			key := prefix + ref
			if open[ref] {
				continue
			}
			if status(key) == "open" {
				pending = append(pending, checked[ref])
			}
			if status(key) != "closed" {
				seen[key] = object{"status": "closed", "at": float64(nowSec())}
				changed = true
			}
		}
		for ref := range open {
			if key := prefix + ref; seen[key] == nil {
				seen[key] = object{"status": "open", "at": float64(nowSec())}
				changed = true
			}
		}
	})
	if !changed {
		return
	}
	comment := orDefault(getString(github, "comment"), "Completed by Claude Code (noctis queue).")
	for _, issue := range pending {
		if _, err := strconv.Atoi(issue.number); err != nil {
			continue
		}
		if !issue.trustedHost() {
			warn("not closing GitHub issue %s (checked off in %s): its host is neither github.com nor GH_HOST", issue, filepath.Base(queuePath))
			continue
		}
		arguments := []string{"issue", "close", issue.number}
		if issue.repo != "" {
			arguments = append(arguments, "--repo", issue.repo)
		}
		child := exec.Command("gh", append(arguments, "--comment", comment)...)

		child.Dir = filepath.Dir(queuePath)
		if isAutoQueue(queuePath) && cwd != "" {
			child.Dir = cwd
		}
		configureDetached(child)
		if err := child.Start(); err != nil {
			warn("gh issue close %s failed to start: %v", issue, err)
			continue
		}
		_ = child.Process.Release()
		journal("", "Stop", "close-issue", issue.String(), object{"file": filepath.Base(queuePath)})
		logInfo("closing GitHub issue %s (checked off in %s)", issue, filepath.Base(queuePath))
	}
}
