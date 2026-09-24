package main

import (
	"strings"
	"testing"
)

func TestEveryToolIsToldToResumeWithItsOwnCommand(t *testing.T) {
	sandboxFiles(t)
	previousLocale, previousHost := locale, activeHost
	t.Cleanup(func() { locale, activeHost = previousLocale, previousHost })
	commands := map[string]string{"claude": "claude --resume s-1", "codex": "codex resume s-1", "antigravity": "agy --conversation s-1",
		"droid": "droid exec --session-id s-1", "copilot": "copilot --resume=s-1"}
	for host, want := range commands {
		if got := hostResumeCommand(host, "s-1"); got != want {
			t.Errorf("%s resumes a session with %q, the notices say %q", host, want, got)
		}
	}
	codex := commands["codex"]
	for _, code := range []string{"en", "tr", "de", "fr", "es", "pt", "it", "nl", "pl", "ru", "ja", "zh", "ko", "ar"} {
		locale = code
		for _, text := range []string{
			T("session.checkpoint", pluginName, "10:00", "/tmp/cp.md", codex),
			T("launch.failed", "s-1", "/work/project", codex),
			T("runner.noData", "s-1", codex),
			T("status.launchFailed", "s-1", "10:00", codex),
		} {
			if !strings.Contains(text, codex) || strings.Contains(text, "claude --resume") {
				t.Errorf("%s: %q does not tell a Codex user to run %s", code, text, codex)
			}
		}
	}
	locale, activeHost = "en", "codex"
	state := object{"launchFailures": object{"thr_9": object{"at": float64(nowSec()), "model": "gpt-5"}}}
	if text := describeState(object{}, state, usageView{}, nowSec()); !strings.Contains(text, "codex resume thr_9") || strings.Contains(text, "claude --resume") {
		t.Fatalf("noctis status on a Codex account names the wrong resume command:\n%s", text)
	}
}
