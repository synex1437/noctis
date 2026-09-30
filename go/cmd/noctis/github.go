package main

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

const issueRepoPattern = `(?:\w[\w-]*(?:\.[\w-]+)+/)?\w[\w.-]*/[\w.-]+`

const (
	closeOnDoneAttempts = 3
	closeClaimSeconds   = 120
)

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

// importedIssueTitle keeps a title on one line and visible: every control
// character (a lone CR too), line or paragraph separator and invisible format
// character (a bidi override, a zero-width space) becomes a space, so the item
// in the file reads in every editor as it does to noctis.
func importedIssueTitle(issue object) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return ' '
		}
		return r
	}, getString(issue, "title")))
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
			if action == "status" && args.present["json"] {
				fmt.Println(`{"file":""}`)
				return
			}
			fmt.Println(T("queue.trustNone", strings.Join(queueFileNames(cfg), ", ")))
			if action == "verify" {
				os.Exit(1)
			}
			return
		}
	} else if !filepath.IsAbs(target) {
		target = filepath.Join(cwd, target)
	}
	if absolute, err := filepath.Abs(target); err == nil {
		target = absolute
	}
	if info := statSafe(target); action != "untrust" && (info == nil || !info.Mode().IsRegular()) {
		fmt.Fprintln(os.Stderr, T("queue.fileMissing", target))
		os.Exit(1)
	}
	switch action {
	case "defer", "undefer":
		runQueueDefer(target, action)
	case "note":
		runQueueNote(target)
	case "verify":
		if status := verifyQueueNow(cfg, cwd, target); status != 0 {
			os.Exit(status)
		}
	case "trust":
		_, changed, _ := queueTrustGap(cfg, target)
		trustQueueFile(target, true)
		rememberOpenIssues(cfg, target)
		fmt.Println(T("queue.trustGranted", target))
		if len(changed) > 0 {
			fmt.Println(T("queue.trustIncludes", len(changed)))
			printChangedItems(changed)
		}
		view := queueSnapshot(target)
		printUnmatchedReferences(target, view)
		printEmptyLines(target, view)
		printHumanItems(target, view)
		printDeferrals(target, view)
		printQueueCheck(cfg, target)
	case "untrust":
		trustQueueFile(target, false)
		fmt.Println(T("queue.trustRevoked", target))
	default:
		view := queueSnapshot(target)
		if args.present["json"] {
			fmt.Println(string(marshalCompact(queueStatusFacts(cfg, target, view))))
			return
		}
		trusted, changed, legacy := queueTrustGap(cfg, target)
		switch {
		case trusted:
			fmt.Println(T("queue.trustGranted", target))
		case legacy:
			fmt.Println(T("queue.trustLegacy", filepath.Base(target), pluginName))
		case len(changed) > 0:
			fmt.Println(T("queue.trustChanged", filepath.Base(target), len(changed), pluginName))
			printChangedItems(changed)
		default:
			fmt.Println(T("queue.trustAsk", filepath.Base(target), view.total, pluginName))
		}
		if trusted {
			printQueueProgress(target, view)
			printQueuePace(cfg, target, view)
			printSetups(target)
		}
		printUnmatchedReferences(target, view)
		printEmptyLines(target, view)
		printHumanItems(target, view)
		printDeferrals(target, view)
		printQueueNotes(target)
		printQueueCheck(cfg, target)
		printQueueHold(cfg, target)
	}
}

// printQueueCheck names the command that checks the queue at target between items and where it comes
// from: a file's own runs without a permission prompt, so trusting the file is trusting it.
func printQueueCheck(cfg object, target string) {
	content, _ := readQueueText(target)
	line, command := queueVerifyLine(content), queueCheckCommandOf(cfg, target, content)
	switch {
	case line != "" && command == line:
		fmt.Println(T("queue.verifyFromFile", printableItem(command)))
	case line != "":
		fmt.Println(T("queue.verifyWaits", printableItem(line)))
		if command != "" {
			fmt.Println(T("queue.verifyFromConfig", printableItem(command)))
		}
	case command != "":
		fmt.Println(T("queue.verifyFromConfig", printableItem(command)))
	}
}

func printHumanItems(target string, view queueView) {
	if view.human > 0 {
		_, names := humanItemNames(view)
		fmt.Println(T("queue.humanStatus", filepath.Base(target), view.human, printableItem(names)))
	}
}

func printEmptyLines(target string, view queueView) {
	if len(view.empty) > 0 {
		fmt.Println(T("queue.emptyLines", filepath.Base(target), len(view.empty), emptyLineList(view)))
	}
}

func printableItem(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, text)
}

func printUnmatchedReferences(target string, view queueView) {
	if len(view.unmatched) == 0 {
		return
	}
	listed := view.unmatched[:min(len(view.unmatched), queueUnmatchedKept)]
	list := printableItem(strings.Join(shortReferences(listed), ", "))
	if more := len(view.unmatched) - len(listed) + view.unmatchedMore; more > 0 {
		list = T("queue.unmatchedMore", list, more)
	}
	fmt.Println(T("queue.unmatched", filepath.Base(target), list))
}

func runQueue() {
	action := positional(1)
	if action != "import" && action != "trust" && action != "untrust" && action != "status" && action != "verify" && action != "defer" && action != "undefer" && action != "note" {
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
	target, folder := flagString("file"), cwd
	if target == "" {
		if target = queueFile(cfg, cwd); target == "" {
			target = filepath.Join(cwd, "TASKS.md")
		}
	} else {
		if !filepath.IsAbs(target) {
			target = filepath.Join(cwd, target)
		}
		folder = filepath.Dir(target)
	}
	destination, leads := importDestination(folder, target)
	if destination == "" {
		fmt.Fprintln(os.Stderr, T("queue.importLink", target, folder, leads))
		os.Exit(1)
	}
	limit := 200.0
	if value, ok := toNumber(flagString("limit")); ok && value >= 1 {
		limit = value
	}
	repo, valid := importRepo(flagString("repo"))
	if !valid {
		fmt.Fprintln(os.Stderr, T("queue.usage"))
		os.Exit(2)
	}
	wanted := importAuthorNames()
	issues, listed, full := []any{}, map[float64]bool{}, []string{}
	for _, author := range wanted {
		arguments := []string{"issue", "list", "--state", "open", "--author", author, "--limit", formatNumber(limit), "--json", "number,title,labels,author,url"}
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
		var page []any
		if err := jsonUnmarshal(output, &page); err != nil {
			fmt.Fprintln(os.Stderr, T("queue.ghFailed", err))
			os.Exit(1)
		}
		if float64(len(page)) >= limit {
			full = append(full, author)
		}
		for _, raw := range page {
			if number, ok := getNumber(toObject(raw), "number"); ok {
				if listed[number] {
					continue
				}
				listed[number] = true
			}
			issues = append(issues, raw)
		}
	}
	authors, named := map[string]bool{}, ""
	if len(issues) > 0 {
		authors, named = importAuthors(cwd, issuesHost(repo, issues), wanted)
	}
	existing, _ := os.ReadFile(destination)
	text, bom := strings.CutPrefix(string(existing), "\uFEFF")
	newline := "\n"
	if end := strings.IndexByte(text, '\n'); end > 0 && text[end-1] == '\r' {
		newline = "\r\n"
	}
	fileLines := strings.Split(text, "\n")
	known, bare := map[string]bool{}, map[string][]bareIssueItem{}
	fenced := false
	for index, line := range fileLines {
		if queueFence(line) {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
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
	lines, qualified, skipped, others := []string{}, 0, 0, []string{}
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
		if author := strings.ToLower(getString(getMap(issue, "author"), "login")); !authors[author] {
			skipped++
			if author != "" && !slices.Contains(others, author) {
				others = append(others, author)
			}
			continue
		}
		labels := []string{}
		for _, rawLabel := range getList(issue, "labels") {
			labels = append(labels, getString(toObject(rawLabel), "name"))
		}
		lines = append(lines, fmt.Sprintf("- [ ] %s%s %s", priorityFromLabels(labels), id, title))
	}
	if len(lines) > 0 {
		cr, added := strings.TrimSuffix(newline, "\n"), []string{}
		for _, line := range lines {
			added = append(added, line+cr)
		}
		if end := len(fileLines) - 1; fileLines[end] != "" {
			fileLines[end] += cr
			fileLines = append(fileLines, "")
		} else if end == 0 {
			fileLines = []string{"# TASKS" + cr, ""}
		}
		at := importSectionEnd(fileLines)
		if at < 0 {
			at, added = len(fileLines)-1, append([]string{cr, "## GitHub issues" + cr}, added...)
		}
		fileLines = slices.Insert(fileLines, at, added...)
	}
	content := strings.Join(fileLines, "\n")
	if bom {
		content = "\uFEFF" + content
	}
	if len(lines) > 0 || qualified > 0 {
		if err := writeImportedQueue(destination, []byte(content)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if absolute, err := filepath.Abs(target); err == nil {
		rememberOpenIssues(cfg, absolute)
	}
	if qualified > 0 {
		logInfo("queue import: %d item(s) in %s that an older import wrote as a bare #N now name %s", qualified, filepath.Base(target), repo)
	}
	switch fetched := len(issues) - skipped; {
	case fetched == 0:
		fmt.Println(T("queue.importEmpty", strings.Join(wanted, ", ")))
	case len(lines) == 0:
		fmt.Println(T("queue.importNone", fetched, named, filepath.Base(target)))
	default:
		fmt.Println(T("queue.importDone", len(lines), fetched-len(lines), filepath.Base(target)))
	}
	for _, author := range full {
		fmt.Println(T("queue.importLimit", formatNumber(limit), author))
	}
	if skipped > 0 {
		if len(others) == 0 {
			others = []string{"?"}
		}
		fmt.Println(T("queue.importSkipped", skipped, strings.Join(others, ", "), named))
	}
	if absolute, err := filepath.Abs(target); err == nil {
		if trusted, changed, legacy := queueTrustGap(cfg, absolute); legacy {
			fmt.Println(T("queue.trustLegacy", filepath.Base(target), pluginName))
		} else if !trusted && len(changed) > 0 {
			fmt.Println(T("queue.importTrust", len(changed), filepath.Base(target)))
		}
	}
}

func importAuthorNames() []string {
	if !args.present["author"] {
		return []string{"@me"}
	}
	names := []string{}
	for _, value := range args.values["author"] {
		for _, login := range strings.Split(value, ",") {
			if login = strings.ToLower(strings.TrimSpace(login)); login != "@me" {
				login = strings.TrimPrefix(login, "@")
			}
			if login != "" && !slices.Contains(names, login) {
				names = append(names, login)
			}
		}
	}
	if len(names) == 0 {
		fmt.Fprintln(os.Stderr, T("queue.usage"))
		os.Exit(2)
	}
	return names
}

func issuesHost(repo string, issues []any) string {
	if strings.Count(repo, "/") >= 2 {
		host, _, _ := strings.Cut(repo, "/")
		return host
	}
	for _, raw := range issues {
		if link, err := url.Parse(getString(toObject(raw), "url")); err == nil && link.Hostname() != "" {
			return strings.ToLower(link.Hostname())
		}
	}
	return ""
}

func importAuthors(cwd, host string, wanted []string) (map[string]bool, string) {
	authors, names := map[string]bool{}, []string{}
	for _, login := range wanted {
		if login == "@me" {
			login = importLogin(cwd, host)
		}
		if !authors[login] {
			authors[login] = true
			names = append(names, login)
		}
	}
	return authors, strings.Join(names, ", ")
}

func importLogin(cwd, host string) string {
	arguments := []string{"api", "user", "--jq", ".login"}
	if host != "" {
		arguments = []string{"api", "--hostname", host, "user", "--jq", ".login"}
	}
	command := exec.Command("gh", arguments...)
	command.Dir = cwd
	var complaint bytes.Buffer
	command.Stderr = &complaint
	output, err := runWithTimeout(command, 20*time.Second)
	login := strings.ToLower(strings.TrimSpace(string(output)))
	if err == nil && login == "" {
		err = errors.New("gh api user printed no login")
	}
	if err != nil {
		if text := strings.TrimSpace(complaint.String()); text != "" {
			err = errors.New(text)
		}
		fmt.Fprintln(os.Stderr, T("queue.importWho", err))
		os.Exit(1)
	}
	return login
}

func queueIssueItems(content string) (map[string]bool, map[string]issueID) {
	open, checked := map[string]bool{}, map[string]issueID{}
	fenced := false
	for _, line := range strings.Split(strings.TrimPrefix(content, "\uFEFF"), "\n") {
		if queueFence(line) {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
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
	return open, checked
}

func rememberOpenIssues(cfg object, queuePath string) {
	if queuePath == "" || !getBool(getMap(section(cfg, "queue"), "github"), "closeOnDone", false) {
		return
	}
	content, err := os.ReadFile(queuePath)
	if err != nil {
		return
	}
	open, _ := queueIssueItems(string(content))
	if len(open) == 0 {
		return
	}
	updateState(func(state object) {
		seen := stateMap(state, "githubSeen")
		for ref := range open {
			if key := queuePath + "#" + ref; seen[key] == nil {
				seen[key] = object{"status": "open", "at": float64(nowSec())}
			}
		}
	})
}

// importSectionEnd returns the line before which new items go when the file
// already has a "## GitHub issues" heading outside a code block: after the last
// item of that section and the lines indented under it, or right after the
// heading while the section holds no item. The section ends at the next heading
// of level one or two. It returns -1 when the file has no such heading.
func importSectionEnd(fileLines []string) int {
	heading, fenced := -1, false
	for index, line := range fileLines {
		if queueFence(line) {
			fenced = !fenced
		} else if !fenced && strings.EqualFold(strings.TrimSpace(line), "## GitHub issues") {
			heading = index
			break
		}
	}
	if heading < 0 {
		return -1
	}
	at, item := heading+1, false
	for index := heading + 1; index < len(fileLines); index++ {
		line := fileLines[index]
		fence := queueFence(line)
		if fence {
			fenced = !fenced
		}
		_, _, listed := queueLineParts(line)
		if !listed {
			_, _, listed = queueBulletParts(line)
		}
		switch unindented := strings.TrimLeft(line, " \t"); {
		case strings.TrimSpace(line) == "":
		case item && unindented != line:
			at = index + 1
		case fence || fenced:
			item = false
		case queueHeading.MatchString(line):
			if len(unindented)-len(strings.TrimLeft(unindented, "#")) <= 2 {
				return at
			}
			item = false
		case listed:
			at, item = index+1, true
		default:
			item = false
		}
	}
	return at
}

// writeImportedQueue replaces the queue file in one step, so a hook that reads it
// meanwhile sees the old list or the new one, never half of it. A file the import
// creates starts out with the usual mode of a project file: the store's writer
// would make it private, as it does noctis's own files.
func writeImportedQueue(file string, content []byte) error {
	if created, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644); err == nil {
		created.Close()
	} else if !errors.Is(err, os.ErrExist) {
		return err
	}
	return writeEncodedAtomic(file, content)
}

func importDestination(folder, target string) (string, string) {
	if _, err := os.Lstat(target); err != nil {
		parent, err := filepath.EvalSymlinks(filepath.Dir(target))
		switch {
		case err != nil:
			return target, ""
		case !linkedWithin(folder, parent):
			return "", parent
		}
		return filepath.Join(parent, filepath.Base(target)), ""
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		leads, _ := os.Readlink(target)
		return "", leads
	}
	if !linkedWithin(folder, resolved) {
		return "", resolved
	}
	return resolved, ""
}

func syncDoneIssues(cfg object, queuePath, content, cwd string) {
	github := getMap(section(cfg, "queue"), "github")
	if !getBool(github, "closeOnDone", false) {
		return
	}
	open, checked := queueIssueItems(content)
	if len(open) == 0 && len(checked) == 0 {
		return
	}
	prefix := queuePath + "#"
	pending, skipped := []issueID{}, []issueID{}
	updateState(func(state object) {
		now := float64(nowSec())
		seen := stateMap(state, "githubSeen")
		unchecked := queueUncheckedIssues(cfg, state, queuePath, content)
		for _, ref := range sortedKeys(checked) {
			key := prefix + ref
			if open[ref] {
				continue
			}
			record := getMap(seen, key)
			status := getString(record, "status")
			switch {
			case record == nil:
				seen[key] = object{"status": "closed", "at": now}
			case unchecked[ref]:
				continue
			case status == "open" || status == "closing" && now-numberOr(record, "at", 0) > closeClaimSeconds:
				if !checked[ref].trustedHost() {
					record["status"], record["at"] = "skipped", now
					skipped = append(skipped, checked[ref])
					continue
				}
				record["status"], record["at"] = "closing", now
				pending = append(pending, checked[ref])
			}
		}
		for ref := range open {
			if key := prefix + ref; seen[key] == nil {
				seen[key] = object{"status": "open", "at": now}
			}
		}
	})
	for _, issue := range skipped {
		warn("not closing GitHub issue %s (checked off in %s): its host is neither github.com nor GH_HOST", issue, filepath.Base(queuePath))
	}
	if len(pending) == 0 {
		return
	}
	comment := orDefault(getString(github, "comment"), "Completed by Claude Code (noctis queue).")
	dir := filepath.Dir(queuePath)
	if isAutoQueue(queuePath) && cwd != "" {
		dir = cwd
	}
	failures := make([]error, len(pending))
	var closing sync.WaitGroup
	for index, issue := range pending {
		closing.Add(1)
		go func() {
			defer closing.Done()
			failures[index] = closeIssue(issue, dir, comment)
		}()
	}
	closing.Wait()
	attempts := make([]float64, len(pending))
	updateState(func(state object) {
		now := float64(nowSec())
		seen := stateMap(state, "githubSeen")
		for index, issue := range pending {
			key := prefix + issue.ref()
			record := getMap(seen, key)
			if record == nil {
				record = object{}
				seen[key] = record
			}
			record["at"] = now
			if failures[index] == nil {
				record["status"] = "closed"
				delete(record, "error")
				continue
			}
			attempts[index] = numberOr(record, "attempts", 0) + 1
			record["attempts"], record["error"], record["status"] = attempts[index], truncateText(failures[index].Error(), 300), "open"
			if attempts[index] >= closeOnDoneAttempts {
				record["status"] = "failed"
			}
		}
	})
	for index, issue := range pending {
		if failures[index] == nil {
			journal("", "Stop", "close-issue", issue.String(), object{"file": filepath.Base(queuePath)})
			logInfo("closed GitHub issue %s (checked off in %s)", issue, filepath.Base(queuePath))
			continue
		}
		journal("", "Stop", "close-issue-failed", issue.String(), object{"file": filepath.Base(queuePath), "attempt": attempts[index], "error": truncateText(failures[index].Error(), 300)})
		if attempts[index] >= closeOnDoneAttempts {
			warn("GitHub issue %s (checked off in %s) could not be closed after %d attempts, giving up: %v", issue, filepath.Base(queuePath), int(attempts[index]), failures[index])
			continue
		}
		warn("GitHub issue %s (checked off in %s) was not closed (attempt %d of %d), trying again at the next stop: %v", issue, filepath.Base(queuePath), int(attempts[index]), closeOnDoneAttempts, failures[index])
	}
}

func closeIssue(issue issueID, dir, comment string) error {
	arguments := []string{"issue", "close", issue.number}
	if issue.repo != "" {
		arguments = append(arguments, "--repo", issue.repo)
	}
	command := exec.Command("gh", append(arguments, "--comment", comment)...)
	command.Dir = dir
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if _, err := runWithTimeout(command, 20*time.Second); err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return fmt.Errorf("%v: %s", err, strings.Join(strings.Fields(detail), " "))
		}
		return err
	}
	return nil
}
