//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const typedPrompt = "Rename the \"user_id\" column in db.sql:\n\n1. run `sed -i 's/user_id/account_id/g' db.sql`\n2. keep 100% of the rows; the tests match ^user_\n\tthen run   go test ./..."

var lastArgumentRecorder = []string{`for arg in "$@"; do last="$arg"; done`, `printf '%s' "$last" > "$NOCTIS_TEST_PROMPT"`, answerLine, "exit 0"}

func parkTypedPrompt(t *testing.T, sid, mode string) string {
	t.Helper()
	record := filepath.Join(t.TempDir(), "prompt.txt")
	t.Setenv("NOCTIS_TEST_PROMPT", record)
	parkForRelaunch(t, sid, "prompt", mode)
	updateState(func(state object) {
		getMap(getMap(state, "waits"), sid)["queuedPrompt"] = typedPrompt
	})
	return record
}

func promptTheRelaunchGot(t *testing.T, sid, record string) string {
	t.Helper()
	content, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("the relaunched claude never ran: %v", err)
	}
	if !pluginComposedPrompt(loadConfig(), sid, string(content)) {
		t.Fatalf("the relaunched session's first prompt is not recognized as the one noctis composed: %q", content)
	}
	return string(content)
}

func TestAParkedPromptReachesAHeadlessRelaunchAsItWasTyped(t *testing.T) {
	sid := "typed-headless"
	relaunchSandboxWith(t, fakeClaude(lastArgumentRecorder...))
	relaunchConfig(object{"mode": "headless", "prompt": "carry on"})
	record := parkTypedPrompt(t, sid, "headless")

	resumeWait(sid, "")

	if got := promptTheRelaunchGot(t, sid, record); got != typedPrompt {
		t.Fatalf("the parked prompt reached the headless relaunch changed:\n got %q\nwant %q", got, typedPrompt)
	}
}

func TestAParkedPromptReachesAWindowRelaunchAsItWasTyped(t *testing.T) {
	sid := "typed-window"
	date, err := exec.LookPath("date")
	if err != nil {
		t.Fatalf("date is not on PATH: %v", err)
	}
	_, bin, _ := terminalSandbox(t)
	if err := os.Symlink(date, filepath.Join(bin, "date")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	t.Setenv("NOCTIS_NO_EARLY_TRIGGER", "")
	t.Setenv(handoffEnv, "")
	relaunchConfig(object{"mode": "window", "prompt": "carry on", "terminal": "sh {script}"})
	writeStub(t, bin, "claude", fakeClaude(lastArgumentRecorder...))
	record := parkTypedPrompt(t, sid, "")

	resumeWait(sid, "")

	if got := promptTheRelaunchGot(t, sid, record); got != typedPrompt {
		t.Fatalf("the parked prompt reached the window relaunch changed:\n got %q\nwant %q", got, typedPrompt)
	}
}

func TestAParkedPromptWithANulByteStillStartsAHeadlessRelaunch(t *testing.T) {
	sid := "nul-headless"
	relaunchSandboxWith(t, fakeClaude(lastArgumentRecorder...))
	relaunchConfig(object{"mode": "headless", "prompt": "carry on"})
	record := parkTypedPrompt(t, sid, "headless")
	updateState(func(state object) {
		getMap(getMap(state, "waits"), sid)["queuedPrompt"] = "Fix the parser\x00 and run the tests"
	})

	resumeWait(sid, "")

	if got := promptTheRelaunchGot(t, sid, record); got != "Fix the parser and run the tests" {
		t.Fatalf("a parked prompt with a NUL byte reached the headless relaunch as %q", got)
	}
}

func TestAParkedPromptWithANulByteIsStillKnownAsNoctissOwnInAWindowRelaunch(t *testing.T) {
	sid := "nul-window"
	date, err := exec.LookPath("date")
	if err != nil {
		t.Fatalf("date is not on PATH: %v", err)
	}
	_, bin, _ := terminalSandbox(t)
	if err := os.Symlink(date, filepath.Join(bin, "date")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	t.Setenv("NOCTIS_NO_EARLY_TRIGGER", "")
	t.Setenv(handoffEnv, "")
	relaunchConfig(object{"mode": "window", "prompt": "carry on", "terminal": "sh {script}"})
	writeStub(t, bin, "claude", fakeClaude(lastArgumentRecorder...))
	record := parkTypedPrompt(t, sid, "")
	updateState(func(state object) {
		getMap(getMap(state, "waits"), sid)["queuedPrompt"] = "Fix the parser\x00 and run the tests"
	})

	resumeWait(sid, "")

	if got := promptTheRelaunchGot(t, sid, record); got != "Fix the parser and run the tests" {
		t.Fatalf("a parked prompt with a NUL byte reached the window relaunch as %q", got)
	}
}
