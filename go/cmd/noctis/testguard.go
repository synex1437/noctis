package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// A failing check between queue items is to be fixed in the code it checks. A run nobody watches can
// instead make it pass by weakening, skipping or deleting a test, or by loosening the check's own
// configuration, and the failure stays. So while a queue drives the session, the first change Claude
// makes to an existing test or check file on an item that is not about tests is refused once, with a
// word on why; the same change made again goes through and is recorded for the daily digest.

// testGuardEdits is how many of the changes that went through the record of a queue file keeps.
const testGuardEdits = 20

// testGuardCreated is how many of the test files Claude created on a queue the record keeps.
const testGuardCreated = 50

// testFolders are the folders whose files are tests, test data or snapshots.
var testFolders = map[string]bool{"test": true, "tests": true, "__tests__": true, "spec": true, "specs": true, "testdata": true, "__snapshots__": true, "e2e": true, "fixtures": true, "cypress": true}

// testFileName matches the name of a test file, or of the configuration of a test runner, a linter or
// a CI service.
var testFileName = lazyRegexp(`^(.+_test\.(go|py|rb|exs|rs|dart)|test_.+\.py|.+_spec\.rb|.+\.(test|spec)\.[cm]?[jt]sx?|.+(Test|Tests|Spec)\.(java|kt|scala|groovy|cs|fs|swift|php)|.+[a-z0-9]IT\.(java|kt|scala|groovy)|conftest\.py|pytest\.ini|tox\.ini|\.coveragerc|(jest|vitest|playwright|cypress|karma|mocha|wdio)\.conf(ig)?\.[cm]?[jt]s|\.mocharc(\.[a-z]+)?|\.nycrc(\.[a-z]+)?|phpunit\.xml(\.dist)?|codecov\.ya?ml|\.golangci\.(ya?ml|toml|json)|\.eslintrc(\.[a-z]+)?|eslint\.config\.[cm]?[jt]s|ruff\.toml|\.flake8|\.pylintrc|\.rubocop\.yml|\.gitlab-ci\.yml|\.travis\.yml|azure-pipelines\.ya?ml|Jenkinsfile|bitbucket-pipelines\.yml)$`)

// testItemWords matches an item about tests, checks or CI, whose work may well change them.
var testItemWords = lazyRegexp(`(?i)(\btest|\bspecs?\b|\be2e\b|\bci\b|coverage|fixture|snapshot|\blint|\bflak(y|iness)\b|\bmock(s|ing)?\b|jest|vitest|mocha|pytest|rspec|junit|phpunit|playwright|cypress|тест|テスト|测试|測試|테스트|prueba|اختبار|\btes-?nya\b|\btes unit\b|\bunit tes\b|\bmengetes\b|\bngetes\b|pengetesan|\b(?:meng|peng|di|ter)?uji)`)

// isCheckFile tells whether path names a test or check file: by its name, by a test folder on the
// way to it, or as a CI service's configuration.
func isCheckFile(path string) bool {
	path = filepath.ToSlash(path)
	parts := strings.Split(path, "/")
	if testFileName.MatchString(parts[len(parts)-1]) {
		return true
	}
	for index, part := range parts[:len(parts)-1] {
		if testFolders[part] || part == ".circleci" || part == ".github" && index+1 < len(parts)-1 && parts[index+1] == "workflows" {
			return true
		}
	}
	return false
}

// refuseTestEdits refuses, once per queue item, a file tool's change to an existing test or check file
// in the folder of the trusted queue that drives the session, while the item it is on is not about
// tests and the user did not type the prompt of this turn.
func refuseTestEdits(input, cfg object) bool {
	file, cwd := getString(getMap(input, "tool_input"), "file_path"), getString(input, "cwd")
	if file == "" || !getBool(section(cfg, "queue"), "guardTests", true) {
		return false
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(cwd, file)
	}
	// Any other write goes ahead without a look at the state.
	if file = filepath.Clean(file); !isCheckFile(file) {
		return false
	}
	sid := sessionKey(input)
	state := peekState()
	if getMap(getMap(state, "userTurns"), sid) != nil || numberOr(state, "disabledUntil", 0) > float64(nowSec()) {
		return false
	}
	path := drivenQueueFile(cfg, state, input, sid)
	if path == "" || !queueTrusted(cfg, path) {
		return false
	}
	rel := pathInside(file, queueFolder(cfg, input, sid, path))
	if rel == "" || !isCheckFile(rel) {
		return false
	}
	key, now := queueTrustKey(path), float64(nowSec())
	if statSafe(file) == nil {
		// A new test is welcome, and so are later changes to it.
		updateState(func(next object) {
			record := stateMap(stateMap(next, "testGuard"), key)
			created := getList(record, "created")
			if !slices.Contains(created, any(rel)) {
				created = append(created, rel)
			}
			record["created"], record["at"] = created[max(0, len(created)-testGuardCreated):], now
		})
		return false
	}
	record := getMap(getMap(state, "testGuard"), key)
	if slices.Contains(getList(record, "created"), any(rel)) {
		return false
	}
	content, _ := readQueueText(path)
	items := queueSnapshotOf(path, content).itemTexts
	if len(items) == 0 {
		return false
	}
	item := items[0]
	if testItemWords.MatchString(item) || strings.Contains(strings.ToLower(item), strings.ToLower(filepath.Base(rel))) {
		return false
	}
	digest := queueItemDigest(item)
	if getString(record, "item") == digest {
		if observed(sid, "PreToolUse", "test-edit", rel, nil) {
			return false
		}
		journal(sid, "PreToolUse", "test-edit", rel, object{"item": truncateText(item, 200)})
		logInfo("%s of %s changes the test or check file %s again while on %q; it goes through", getString(input, "tool_name"), sid, rel, truncateText(item, 80))
		updateState(func(next object) {
			record := stateMap(stateMap(next, "testGuard"), key)
			edits := append(getList(record, "edits"), object{"at": now, "file": rel, "item": truncateText(item, 200)})
			record["edits"], record["at"] = edits[max(0, len(edits)-testGuardEdits):], now
		})
		return false
	}
	if observed(sid, "PreToolUse", "ask-test-edit", rel, nil) {
		return false
	}
	journal(sid, "PreToolUse", "ask-test-edit", rel, object{"item": truncateText(item, 200)})
	logInfo("refused a %s of %s to the test or check file %s while on %q, once", getString(input, "tool_name"), sid, rel, truncateText(item, 80))
	updateState(func(next object) {
		record := stateMap(stateMap(next, "testGuard"), key)
		record["item"], record["at"] = digest, now
	})
	emit(object{"hookSpecificOutput": object{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": fmt.Sprintf("[noctis] %s is a test or check file, and the queue item you are on (%s) is not about tests or checks. When a check fails, fix the code it checks: weakening, skipping or deleting a test, or loosening the check's configuration, makes it pass while the failure stays. If the test or the check is itself wrong, or this item needs the change, make the same edit again: it goes through this time, noctis records it for the user, and you say why in your summary. Do not ask for confirmation; decide yourself.", rel, printableItem(truncateText(item, 120)))}})
	return true
}

// pathInside is file's path inside folder, with forward slashes, as given or with symbolic links
// resolved, or "" when it lies outside.
func pathInside(file, folder string) string {
	for _, pair := range [][2]string{{file, folder}, {resolvedWritePath(file), resolvedWritePath(folder)}} {
		if pair[0] == "" || pair[1] == "" {
			continue
		}
		if rel, err := filepath.Rel(pair[1], pair[0]); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel)
		}
	}
	return ""
}

// testEditsSince are the test and check files whose changes went through after a refusal on the queue
// at path since from, the latest first and each once.
func testEditsSince(state object, path string, from float64) []string {
	files := []string{}
	edits := getList(getMap(getMap(state, "testGuard"), queueTrustKey(path)), "edits")
	for index := len(edits) - 1; index >= 0; index-- {
		edit := toObject(edits[index])
		if file := getString(edit, "file"); numberOr(edit, "at", 0) >= from && file != "" && !slices.Contains(files, file) {
			files = append(files, file)
		}
	}
	return files
}
