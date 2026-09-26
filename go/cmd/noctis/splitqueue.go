package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

const (
	splitQueueMaxChars = 6000
	splitQueueMaxWords = 1000
)

var clauseBreak = lazyRegexp(`[.!;:,]\s+|(?i:\s+(?:and|then|plus|ve|sonra|ardından|und|dann|et|puis|y|luego|e|poi|en|daarna|i|potem|и|затем)\s+)`)

var verbFinalImperatives = lazyWordSet(verbFinalImperativeWords)

var requestWords = lazyWordSet(`i we you want need would like can could will to just should must let's lets us me kindly go ahead`)

var nounFollowers = lazyWordSet(`is are was were has have had will would can could should must does did do seems keeps gets got needs
	isn't aren't wasn't weren't doesn't didn't won't can't`)

func cleanWord(word string) string {
	return strings.ToLower(strings.Trim(word, ",.:;!()\"'“”‘’«»"))
}

func jobClause(clause string) bool {
	words := strings.Fields(clause)
	if len(words) == 0 {
		return false
	}
	for index, raw := range words {
		word := cleanWord(raw)
		if imperativeWords(word) {
			return index+1 == len(words) || !nounFollowers(cleanWord(words[index+1]))
		}
		if !leadIns(word) && !requestWords(word) {
			break
		}
	}
	last := cleanWord(words[len(words)-1])
	return verbFinalImperatives(last) || verbFinalImperatives(strings.TrimSuffix(strings.TrimSuffix(last, "in"), "iniz"))
}

func severalJobsLikely(prompt string) bool {
	text := strings.TrimSpace(codeFence.ReplaceAllString(prompt, " "))
	if size := len([]rune(text)); size < autoQueueMinChars || size > splitQueueMaxChars || endsQuestion(text) || hasBugReportMarker(text) {
		return false
	}
	jobs, logLines := 0, 0
	for _, line := range strings.Split(text, "\n") {
		if logLikeLine.MatchString(line) {
			logLines++
		}
		for _, clause := range clauseBreak.Split(line, -1) {
			if jobClause(clause) {
				jobs++
			}
		}
	}
	return logLines < 2 && jobs >= autoQueueMinItems && len(jobWordDigests(prompt)) <= splitQueueMaxWords
}

func jobWords(text string) []string {
	words, word := []string{}, []rune{}
	flush := func() {
		if len(word) > 0 {
			words = append(words, strings.ToLower(string(word)))
			word = word[:0]
		}
	}
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana):
			flush()
			words = append(words, string(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r):
			word = append(word, r)
		default:
			flush()
		}
	}
	flush()
	return words
}

func jobWordDigest(word string) string {
	sum := sha256.Sum256([]byte(word))
	return hex.EncodeToString(sum[:6])
}

func jobWordDigests(text string) []any {
	seen, digests := map[string]bool{}, []any{}
	for _, word := range jobWords(text) {
		if digest := jobWordDigest(word); !seen[digest] {
			seen[digest] = true
			digests = append(digests, digest)
		}
	}
	return digests
}

func startSplitQueue(sid, cwd, prompt string, now int64) string {
	lines := []string{"# " + pluginName + " — jobs from the prompt at " + localISO(float64(now)), ""}
	path := writeSessionQueue(sid, object{"cwd": cwd, "at": float64(now), "items": float64(0), "words": jobWordDigests(prompt)}, lines)
	if path == "" {
		return ""
	}
	journal(sid, "UserPromptSubmit", "split-queue", "jobs left to Claude to list", nil)
	logInfo("prompt of %s may hold several jobs; Claude lists them in %s", sid, path)
	return path
}

func splitQueueDirective(path string) string {
	return fmt.Sprintf(`[noctis] This prompt may ask for several separate jobs. If it does, first write them to %s, one "- [ ] " line per job in the order to do them, each job in the user's own words copied from the prompt: noctis refuses a line with a word the prompt does not have, so add no job and no word of your own. Then work through them, mark each "- [x]" the moment it is done, and do not stop or ask for confirmation between jobs; the session continues until every job is ticked. If the prompt is one job, leave that file as it is.`, path)
}

func jobText(line string) string {
	text := strings.TrimSpace(line)
	if match := listItemLine.FindStringSubmatch(text); match != nil {
		text = match[1]
	}
	if match := checkboxPrefix.FindStringSubmatch(text); match != nil {
		text = text[len(match[0]):]
	}
	return strings.Join(strings.Fields(queueUndone(text)), " ")
}

func editedText(input object, current string) string {
	toolInput := getMap(input, "tool_input")
	replace := func(text string, edit object) string {
		old, next := getString(edit, "old_string"), getString(edit, "new_string")
		switch {
		case old == "":
			return next
		case getBool(edit, "replace_all", false):
			return strings.ReplaceAll(text, old, next)
		}
		return strings.Replace(text, old, next, 1)
	}
	switch getString(input, "tool_name") {
	case "Write":
		return getString(toolInput, "content")
	case "Edit":
		return replace(current, toolInput)
	}
	for _, edit := range getList(toolInput, "edits") {
		current = replace(current, toObject(edit))
	}
	return current
}

func writesTo(file, cwd, target string) bool {
	if file == "" || target == "" {
		return false
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(cwd, file)
	}
	targets := []string{filepath.Clean(target), resolvedWritePath(target)}
	for _, candidate := range []string{filepath.Clean(file), resolvedWritePath(file)} {
		if candidate != "" && slices.Contains(targets, candidate) {
			return true
		}
	}
	return false
}

func refuseJobsNotInPrompt(input, cfg object) bool {
	sid := sessionKey(input)
	state := readState()
	record := getMap(getMap(state, "autoQueues"), sid)
	words, path := digestSet(getList(record, "words")), getString(record, "path")
	if len(words) == 0 || !writesTo(getString(getMap(input, "tool_input"), "file_path"), getString(input, "cwd"), path) {
		return false
	}
	current, _ := readQueueText(path)
	next := editedText(input, current)
	known := map[string]bool{}
	for _, line := range strings.Split(current, "\n") {
		known[jobText(line)] = true
	}
	refused, reasons := []string{}, []string{}
	for _, line := range strings.Split(next, "\n") {
		text := jobText(line)
		if text == "" || known[text] {
			continue
		}
		missing := []string{}
		for _, word := range jobWords(text) {
			if !words[jobWordDigest(word)] && !slices.Contains(missing, word) {
				missing = append(missing, word)
			}
		}
		if len(missing) > 0 {
			shown := truncateText(strings.TrimSpace(line), 160)
			refused = append(refused, shown)
			reasons = append(reasons, fmt.Sprintf("%q (%s)", shown, strings.Join(missing, ", ")))
		}
	}
	applySessionLocale(cfg, state, sid)
	if len(refused) > 0 {
		journal(sid, "PreToolUse", "deny-job-not-in-prompt", fmt.Sprintf("%d line(s)", len(refused)), nil)
		logInfo("denied %d checklist line(s) of %s whose words are not in the prompt", len(refused), sid)
		emit(object{
			"systemMessage":      T("queue.jobNotInPrompt", strings.Join(refused, "; ")),
			"hookSpecificOutput": object{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": fmt.Sprintf("%s holds only the jobs the user asked for, each in the user's own words: every word of a job must appear in the prompt. These lines use words the prompt does not have: %s. Write each job with the prompt's own words, or leave it out.", path, strings.Join(reasons, "; "))},
		})
		return true
	}
	added := queueSnapshotOf(path, next).total - queueSnapshotOf(path, current).total
	if insideSubagent(input) || added <= 0 {
		return false
	}
	listed := numberOr(record, "items", 0)
	updateState(func(fresh object) {
		if own := getMap(getMap(fresh, "autoQueues"), sid); own != nil {
			own["items"] = numberOr(own, "items", 0) + float64(added)
		}
	})
	if listed > 0 {
		return false
	}
	journal(sid, "PreToolUse", "split-queue-listed", fmt.Sprintf("%d jobs", added), nil)
	logInfo("Claude listed %d job(s) of the prompt of %s in %s", added, sid, path)
	emit(object{"systemMessage": T("queue.autoNotice", added, pluginName)})
	return true
}
