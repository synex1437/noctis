//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sentNotices() []string {
	logged, _ := os.ReadFile(files.log)
	notices := []string{}
	for _, line := range strings.Split(string(logged), "\n") {
		if _, body, found := strings.Cut(line, "] notify: "); found {
			notices = append(notices, body)
		}
	}
	return notices
}

func TestARelaunchThatCannotStartSendsOneNoticeWithTheFolderAndTheCommand(t *testing.T) {
	takeoverSandbox(t)
	relaunchConfig(object{"mode": "headless", "prompt": "carry on"})
	sid := "moved-project"
	parkForRelaunch(t, sid, "batch", "headless")
	moved := filepath.Join(t.TempDir(), "project renamed overnight")
	updateState(func(state object) { getMap(getMap(state, "waits"), sid)["cwd"] = moved })

	resumeWait(sid, "")

	notices := sentNotices()
	if len(notices) != 1 || !strings.Contains(notices[0], moved) || !strings.Contains(notices[0], hostResumeCommand("claude", sid)) {
		t.Fatalf("a relaunch whose project folder is gone sent %d notice(s), want one that names the folder and the command to run there: %q", len(notices), notices)
	}
}

func TestARelaunchWhoseToolIsMissingNamesThatTool(t *testing.T) {
	calls := takeoverSandbox(t)
	previousHost := activeHost
	t.Cleanup(func() { activeHost = previousHost })
	activeHost = "codex"
	t.Setenv("PATH", filepath.Dir(calls))
	relaunchConfig(object{"mode": "headless", "prompt": "carry on"})
	sid := "thr_missing"
	parkForRelaunch(t, sid, "batch", "headless")
	cwd := getString(waitOf(sid), "cwd")

	resumeWait(sid, "")

	notices := sentNotices()
	if len(notices) != 1 || !strings.Contains(notices[0], "codex") || strings.Contains(notices[0], "claude command") || strings.Contains(notices[0], "claude --resume") ||
		!strings.Contains(notices[0], cwd) || !strings.Contains(notices[0], hostResumeCommand("codex", sid)) {
		t.Fatalf("a Codex relaunch with no codex on PATH should send one notice naming codex, the folder and codex's resume command: %q", notices)
	}
}
