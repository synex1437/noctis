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
			[]string{"--verbose", "--add-dir", "/work"}, map[string]string{"--permission-mode": "acceptEdits"}},
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
