package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testGuardSandbox is a project whose trusted queue drives the session and that has a test file from
// before the session.
func testGuardSandbox(t *testing.T) (object, string, string, string) {
	t.Helper()
	cfg, project, frontend := queueCheckSandbox(t, "")
	test := filepath.Join(project, "tests", "users_test.go")
	if err := os.MkdirAll(filepath.Dir(test), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(test, []byte("package users\n\nfunc TestMigrate(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg, project, frontend, test
}

func testFileEdit(sid, cwd, file string) object {
	return agentHookInput("PreToolUse", sid, cwd, object{"tool_name": "Edit", "tool_input": object{"file_path": file, "old_string": "func TestMigrate", "new_string": "func testMigrate"}})
}

func journaledAction(sid, action string) bool {
	for _, journaled := range journaledFor(sid) {
		if journaled == action {
			return true
		}
	}
	return false
}

func TestTestAndCheckFilesAreKnownByTheirNameOrFolder(t *testing.T) {
	cases := map[string]bool{
		"internal/users/users_test.go":            true,
		"tests/test_users.py":                     true,
		"app/models/user_spec.rb":                 true,
		"src/users.test.ts":                       true,
		"src/users.spec.mjs":                      true,
		"src/test/java/UserServiceTest.java":      true,
		"Sources/AppTests/UserTests.swift":        true,
		"conftest.py":                             true,
		"jest.config.js":                          true,
		"vitest.config.mts":                       true,
		".eslintrc.json":                          true,
		".golangci.yml":                           true,
		".github/workflows/ci.yml":                true,
		".circleci/config.yml":                    true,
		"spec/fixtures/users.json":                true,
		"src/__snapshots__/app.test.tsx.snap":     true,
		"internal/parse/testdata/input.txt":       true,
		"internal/users/users.go":                 false,
		"src/users.ts":                            false,
		".github/CODEOWNERS":                      false,
		"docs/testing.md":                         false,
		"src/contest.py":                          false,
		"src/latest.go":                           false,
		"Makefile":                                false,
		"package.json":                            false,
		"services/specification/specification.go": false,
		"src/main/java/UserServiceIT.java":        true,
		"src/Audit/AUDIT.php":                     false,
	}
	for path, want := range cases {
		if got := isCheckFile(path); got != want {
			t.Errorf("isCheckFile(%q) = %t, want %t", path, got, want)
		}
	}
}

func TestItemsAboutTestsAreKnownByTheirWords(t *testing.T) {
	cases := map[string]bool{
		"fix the flaky login test":                    true,
		"raise the coverage of the parser":            true,
		"run the e2e suite on the CI":                 true,
		"update the snapshots after the redesign":     true,
		"move the fixtures into one folder":           true,
		"make the linter pass on the new module":      true,
		"replace the mocks with fakes":                true,
		"ödeme testlerini ekle":                       true,
		"ユーザーのテストを追加する":                               true,
		"build the settings page from the mockup":     false,
		"fix the flake8 warnings in the parser":       false,
		"make the error message more specific":        false,
		"migrate the users table":                     false,
		"write the release notes for the new release": false,
	}
	for text, want := range cases {
		if got := testItemWords.MatchString(text); got != want {
			t.Errorf("testItemWords.MatchString(%q) = %t, want %t", text, got, want)
		}
	}
}

func TestAChangeToATestOnAnItemNotAboutTestsIsRefusedOnceThenRecorded(t *testing.T) {
	cfg, project, frontend, test := testGuardSandbox(t)
	first := hookOutput(t, onPreToolUse, testFileEdit("tg1", frontend, test), cfg)
	reason := getString(getMap(first, "hookSpecificOutput"), "permissionDecisionReason")
	if !deniedTool(first) || !strings.Contains(reason, "tests/users_test.go is a test or check file") || !strings.Contains(reason, "(migrate the users table)") || !strings.Contains(reason, "make the same edit again") {
		t.Fatalf("a change to a test on an item not about tests was not refused with a word on why: %v", first)
	}
	if !journaledAction("tg1", "ask-test-edit") {
		t.Fatalf("the refusal is not in the journal: %v", journaledFor("tg1"))
	}
	if again := hookOutput(t, onPreToolUse, testFileEdit("tg1", frontend, test), cfg); again != nil {
		t.Fatalf("the same change made again was refused again: %v", again)
	}
	if !journaledAction("tg1", "test-edit") {
		t.Fatalf("the change that went through is not in the journal: %v", journaledFor("tg1"))
	}
	path := filepath.Join(project, "TASKS.md")
	now := nowSec()
	if files := testEditsSince(readState(), path, float64(now-60)); len(files) != 1 || files[0] != "tests/users_test.go" {
		t.Fatalf("the change that went through is recorded as %q", files)
	}
	from := float64(now) - 86400
	if body, want := buildDigest(cfg, readState(), currentUsage(now), nil, now).body, "- "+T("digest.testEdits", formatTime(from), 1, "tests/users_test.go"); !strings.Contains(body, want) {
		t.Fatalf("the digest does not name the test changed past the refusal (want %q):\n%s", want, body)
	}

	writeQueueFile(t, project, "# q\n- [x] migrate the users table\n- [ ] write the release notes\n")
	if next := hookOutput(t, onPreToolUse, testFileEdit("tg1", frontend, test), cfg); !deniedTool(next) || !strings.Contains(getString(getMap(next, "hookSpecificOutput"), "permissionDecisionReason"), "(write the release notes)") {
		t.Fatalf("on the next item a change to a test was not refused once more: %v", next)
	}
}

func TestTestChangesGoThroughWhereTheyBelong(t *testing.T) {
	cfg, project, frontend, test := testGuardSandbox(t)
	source := filepath.Join(project, "internal", "users.go")
	if output := hookOutput(t, onPreToolUse, testFileEdit("tg2", frontend, source), cfg); output != nil {
		t.Fatalf("a change to a file that is not a test was refused: %v", output)
	}
	outside := filepath.Join(t.TempDir(), "tests", "other_test.go")
	if err := os.MkdirAll(filepath.Dir(outside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("package other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output := hookOutput(t, onPreToolUse, testFileEdit("tg2", frontend, outside), cfg); output != nil {
		t.Fatalf("a change to a test outside the queue's folder was refused: %v", output)
	}

	section(cfg, "queue")["guardTests"] = false
	if output := hookOutput(t, onPreToolUse, testFileEdit("tg2", frontend, test), cfg); output != nil {
		t.Fatalf("with queue.guardTests off a change to a test was refused: %v", output)
	}
	section(cfg, "queue")["guardTests"] = true

	defer func(previous bool) { observing = previous }(observing)
	observing = true
	if output := hookOutput(t, onPreToolUse, testFileEdit("tg2", frontend, test), cfg); output != nil {
		t.Fatalf("observe mode refused a change to a test: %v", output)
	}
	if !journaledAction("tg2", "would-ask-test-edit") {
		t.Fatalf("observe mode did not journal the refusal it would make: %v", journaledFor("tg2"))
	}
	observing = false

	writeQueueFile(t, project, "# q\n- [ ] rename the users columns\n- [ ] write the release notes\n")
	if output := hookOutput(t, onPreToolUse, testFileEdit("tg2", frontend, test), cfg); output != nil {
		t.Fatalf("with items nobody trusted in the queue file a change to a test was refused: %v", output)
	}
	writeQueueFile(t, project, "# q\n- [ ] migrate the users table\n- [ ] write the release notes\n")

	hookOutput(t, onUserPromptSubmit, promptInput("tg3", frontend, "the migrate test is wrong, fix it"), cfg)
	if output := hookOutput(t, onPreToolUse, testFileEdit("tg3", frontend, test), cfg); output != nil {
		t.Fatalf("in a turn the user typed a change to a test was refused: %v", output)
	}

	writeQueueFile(t, project, "# q\n- [ ] add tests for the users table migration\n- [ ] write the release notes\n")
	trustQueueFile(filepath.Join(project, "TASKS.md"), true)
	if output := hookOutput(t, onPreToolUse, testFileEdit("tg4", frontend, test), cfg); output != nil {
		t.Fatalf("on an item about tests a change to a test was refused: %v", output)
	}
	writeQueueFile(t, project, "# q\n- [ ] rename the helper in users_test.go\n- [ ] write the release notes\n")
	trustQueueFile(filepath.Join(project, "TASKS.md"), true)
	if output := hookOutput(t, onPreToolUse, testFileEdit("tg4", frontend, test), cfg); output != nil {
		t.Fatalf("on an item that names the test file a change to it was refused: %v", output)
	}
}

func TestATestClaudeWritesOnTheQueueStaysOpenToIt(t *testing.T) {
	cfg, project, frontend, _ := testGuardSandbox(t)
	created := filepath.Join(project, "tests", "orders_test.go")
	write := agentHookInput("PreToolUse", "tg5", frontend, object{"tool_name": "Write", "tool_input": object{"file_path": created, "content": "package orders\n\nfunc TestOrders(t *testing.T) {}\n"}})
	if output := hookOutput(t, onPreToolUse, write, cfg); deniedTool(output) {
		t.Fatalf("writing a new test was refused: %v", output)
	}
	if err := os.WriteFile(created, []byte("package orders\n\nfunc TestMigrate(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output := hookOutput(t, onPreToolUse, testFileEdit("tg5", frontend, created), cfg); output != nil {
		t.Fatalf("a change to a test Claude wrote on the queue was refused: %v", output)
	}
	if journaledAction("tg5", "ask-test-edit") || len(testEditsSince(readState(), filepath.Join(project, "TASKS.md"), 0)) != 0 {
		t.Fatalf("a test Claude wrote on the queue counts as one it changed past a refusal: %v", journaledFor("tg5"))
	}
}
