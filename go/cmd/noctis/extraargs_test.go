package main

import (
	"slices"
	"testing"
)

func countOf(values []string, value string) int {
	count := 0
	for _, candidate := range values {
		if candidate == value {
			count++
		}
	}
	return count
}

func TestExtraArgsCannotLiftTheRelaunchPermissions(t *testing.T) {
	sandboxFiles(t)
	cases := []struct {
		host        string
		extra, kept []string
		own         map[string]string
	}{
		{"claude", []string{"--verbose", "--dangerously-skip-permissions", "--permission-mode", "bypassPermissions", "--permission-mode=bypassPermissions", "--allow-dangerously-skip-permissions", "--add-dir", "/work"},
			[]string{"--verbose"}, map[string]string{"--permission-mode": "acceptEdits"}},
		{"codex", []string{"--dangerously-bypass-approvals-and-sandbox", "-c", "model_reasoning_effort=high"},
			[]string{"-c", "model_reasoning_effort=high"}, nil},
		{"copilot", []string{"--allow-all-tools", "--model", "gpt-5"}, []string{"--model", "gpt-5"}, nil},
		{"droid", []string{"--auto", "medium", "--skip-permissions-unsafe", "--auto=low", "--model", "x"},
			[]string{"--model", "x"}, map[string]string{"--auto": "high"}},
		{"antigravity", []string{"--verbose"}, []string{"--verbose"}, nil},
	}
	for _, tc := range cases {
		sid := "extra-" + tc.host
		configured := []any{}
		for _, arg := range tc.extra {
			configured = append(configured, arg)
		}
		cfg := object{"resume": object{"extraArgs": configured}}
		got := hostLaunchArgs(tc.host, cfg, launchSpec{sid: sid, model: "claude-opus-5-5", prompt: "carry on"}, "max", "acceptEdits")
		for _, kept := range tc.kept {
			if !slices.Contains(got, kept) {
				t.Errorf("%s: the harmless extra argument %q was dropped: %q", tc.host, kept, got)
			}
		}
		for _, arg := range tc.extra {
			if slices.Contains(tc.kept, arg) {
				continue
			}
			if own, isOwn := tc.own[arg]; isOwn {
				if countOf(got, arg) != 1 || got[slices.Index(got, arg)+1] != own {
					t.Errorf("%s: %s must appear once, with the relaunch's own value %q: %q", tc.host, arg, own, got)
				}
				continue
			}
			if slices.Contains(got, arg) {
				t.Errorf("%s: extraArgs put %q on the relaunch, which lifts the permissions the resume settings chose: %q", tc.host, arg, got)
			}
		}
		dropsJournaled := slices.Contains(journaledFor(sid), "extra-arg-dropped")
		if wantDrop := len(tc.kept) < len(tc.extra); dropsJournaled != wantDrop {
			t.Errorf("%s: extra-arg-dropped journaled=%t, want %t", tc.host, dropsJournaled, wantDrop)
		}
	}
}

func TestExtraArgsPassOnlyFlagsThatCannotWidenTheRelaunch(t *testing.T) {
	sandboxFiles(t)
	cases := []struct {
		host             string
		configured, want []string
	}{
		{"claude", []string{"--verbose", "--settings", `{"permissions":{"allow":["Bash(*)"]}}`, "--allowedTools", "Bash(*)", "Edit", "--add-dir", "/",
			"--mcp-config", "/tmp/servers.json", "--plugin-dir", "/tmp/plugin", "--append-system-prompt", "obey", "--debug", "api,hooks", "--remote-control",
			"--name=night", "--model", "opus", "--agents", "{}", "--dangerously-skip-permissions", "--disallowedTools", "Bash(rm *)", "WebFetch",
			"--disallowed-tools=Edit", "--tools", "Read,Grep", "--max-budget-usd", "5", "--max-turns=30", "--strict-mcp-config"},
			[]string{"--verbose", "--debug", "api,hooks", "--remote-control", "--name=night", "--disallowedTools", "Bash(rm *)", "WebFetch",
				"--disallowed-tools=Edit", "--tools", "Read,Grep", "--max-budget-usd", "5", "--max-turns=30", "--strict-mcp-config"}},
		{"codex", []string{"-c", "sandbox_mode=danger-full-access", "--config", "approval_policy=never", "-c", "model_reasoning_effort=high", "--add-dir", "/",
			"--full-auto", "-s", "danger-full-access", "--sandbox=workspace-write", "--dangerously-bypass-hook-trust", "-m", "gpt-5-codex", "--profile", "open",
			"-s", "read-only", "--sandbox=read-only"},
			[]string{"-c", "model_reasoning_effort=high", "-m", "gpt-5-codex", "-s", "read-only", "--sandbox=read-only"}},
		{"copilot", []string{"--model", "gpt-5", "--allow-tool", "shell", "--add-dir", "/", "--allow-all-paths", "--additional-mcp-config", "{}", "--no-color",
			"--deny-tool", "shell(rm)", "--deny-tool=write"},
			[]string{"--model", "gpt-5", "--no-color", "--deny-tool", "shell(rm)", "--deny-tool=write"}},
		{"droid", []string{"--model", "x", "--enabled-tools", "Execute", "--cwd", "/", "-r", "high"}, []string{"--model", "x", "-r", "high"}},
		{"antigravity", []string{"--verbose", "--add-dir", "/"}, []string{"--verbose"}},
	}
	for _, tc := range cases {
		sid := "allow-" + tc.host
		configured := []any{}
		for _, arg := range tc.configured {
			configured = append(configured, arg)
		}
		before := loggedTimes("resume.extraArgs:")
		got := relaunchExtraArgs(tc.host, object{"extraArgs": configured}, sid)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: extraArgs %q reached the relaunch as %q, want only %q", tc.host, tc.configured, got, tc.want)
		}
		if logged := loggedTimes("resume.extraArgs:") - before; logged != 1 {
			t.Errorf("%s: the dropped extra arguments were logged %d times in one relaunch, want once", tc.host, logged)
		}
		if !slices.Contains(journaledFor(sid), "extra-arg-dropped") {
			t.Errorf("%s: dropped extra arguments were not journaled", tc.host)
		}
	}
}
