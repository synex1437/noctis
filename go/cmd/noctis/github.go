package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var (
	issueRef = lazyRegexp(`#(\d+)\b`)

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
	arguments := []string{"issue", "list", "--state", "open", "--limit", limit, "--json", "number,title,labels"}
	if repo := flagString("repo"); repo != "" {
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
	known := map[string]bool{}
	for _, found := range issueRef.FindAllStringSubmatch(string(existing), -1) {
		known[found[1]] = true
	}
	lines := []string{}
	for _, raw := range issues {
		issue := toObject(raw)
		number, ok := getNumber(issue, "number")
		if issue == nil || !ok || known[formatNumber(number)] {
			continue
		}
		labels := []string{}
		for _, rawLabel := range getList(issue, "labels") {
			labels = append(labels, getString(toObject(rawLabel), "name"))
		}
		title := strings.TrimSpace(strings.ReplaceAll(getString(issue, "title"), "\n", " "))
		lines = append(lines, fmt.Sprintf("- [ ] %s#%s %s", priorityFromLabels(labels), formatNumber(number), title))
	}
	if len(lines) == 0 {
		fmt.Println(T("queue.importNone", len(issues), filepath.Base(target)))
		return
	}
	content := string(existing)
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
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
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
	open, checked := map[string]bool{}, map[string]bool{}
	for _, line := range strings.Split(string(content), "\n") {
		entry, ok := parseQueueLine(line, 0)
		if !ok {
			continue
		}
		for _, found := range issueRef.FindAllStringSubmatch(entry.text, -1) {
			if entry.checked {
				checked[found[1]] = true
			} else {
				open[found[1]] = true
			}
		}
	}
	if len(open) == 0 && len(checked) == 0 {
		return
	}
	prefix := queuePath + "#"
	pending := []string{}
	changed := false
	updateState(func(state object) {
		seen := stateMap(state, "githubSeen")
		status := func(key string) string { return getString(getMap(seen, key), "status") }
		for number := range checked {
			key := prefix + number
			if status(key) == "open" {
				pending = append(pending, number)
			}
			if status(key) != "closed" {
				seen[key] = object{"status": "closed", "at": float64(nowSec())}
				changed = true
			}
		}
		for number := range open {
			if key := prefix + number; seen[key] == nil {
				seen[key] = object{"status": "open", "at": float64(nowSec())}
				changed = true
			}
		}
	})
	if !changed {
		return
	}
	comment := orDefault(getString(github, "comment"), "Completed by Claude Code (noctis queue).")
	for _, number := range pending {
		if _, err := strconv.Atoi(number); err != nil {
			continue
		}
		child := exec.Command("gh", "issue", "close", number, "--comment", comment)

		child.Dir = filepath.Dir(queuePath)
		if isAutoQueue(queuePath) && cwd != "" {
			child.Dir = cwd
		}
		configureDetached(child)
		if err := child.Start(); err != nil {
			warn("gh issue close %s failed to start: %v", number, err)
			continue
		}
		_ = child.Process.Release()
		journal("", "Stop", "close-issue", "#"+number, object{"file": filepath.Base(queuePath)})
		logInfo("closing GitHub issue #%s (checked off in %s)", number, filepath.Base(queuePath))
	}
}
