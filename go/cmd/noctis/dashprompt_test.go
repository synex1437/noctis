//go:build !windows

package main

import (
	"strings"
	"testing"
)

func TestAPromptThatStartsWithADashIsNeverReadAsAnOption(t *testing.T) {
	for _, text := range []string{"- add tests", "--help me finish the parser", "   -x flag parsing"} {
		if prompt := sanitizePrompt(text); !strings.HasPrefix(prompt, "Continue. -") {
			t.Errorf("sanitizePrompt(%q) = %q, which a CLI would read as an option", text, prompt)
		}
	}
	if prompt := sanitizePrompt("fix the -x flag"); prompt != "fix the -x flag" {
		t.Errorf("a prompt that does not start with a dash was changed to %q", prompt)
	}
	for _, host := range []string{"claude", "codex"} {
		args := hostLaunchArgs(host, object{"resume": object{}}, launchSpec{sid: "s1", model: "claude-opus-5-5", prompt: "carry on"}, "max", "acceptEdits")
		if len(args) < 2 || args[len(args)-2] != "--" || args[len(args)-1] != "carry on" {
			t.Errorf("%s: the prompt does not follow --: %q", host, args)
		}
	}
}

func TestARelaunchWithAPromptThatStartsWithADashRuns(t *testing.T) {
	sid := "dash-prompt"
	script := fakeClaude(
		`last=""; before=""`,
		`for arg in "$@"; do before="$last"; last="$arg"; done`,
		`case "$last" in -*) if [ "$before" != "--" ]; then echo "error: unknown option '$last'" >&2; exit 1; fi ;; esac`,
		answerLine, "exit 0")
	calls := relaunchSandboxWith(t, script)
	relaunchConfig(object{"mode": "headless", "prompt": "- keep going with the parser"})
	parkForRelaunch(t, sid, "batch", "headless")

	resumeWait(sid, "")

	if got := launchesOf(calls, sid); got != 1 {
		t.Fatalf("the runner launched the session %d time(s), want 1", got)
	}
	if wait := waitOf(sid); wait != nil {
		t.Fatalf("claude refused the relaunch prompt as an unknown option and the wait is kept for a retry: %v (journal %v)", wait, journaledFor(sid))
	}
}
