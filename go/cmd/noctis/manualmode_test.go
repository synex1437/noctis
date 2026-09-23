//go:build !windows

package main

import (
	"path/filepath"
	"testing"
)

func TestManualPermissionModeIsKeptWhereClaudeKnowsIt(t *testing.T) {
	relaunchSandbox(t)
	bin := t.TempDir()
	current := writeScript(t, filepath.Join(bin, "claude-current"), "#!/bin/sh\nprintf '  --permission-mode <mode>  Permission mode to use for the session\\n      (choices: \"acceptEdits\", \"auto\",\\n      \"bypassPermissions\", \"manual\",\\n      \"dontAsk\", \"plan\")\\n'\n")
	older := writeScript(t, filepath.Join(bin, "claude-older"), "#!/bin/sh\nprintf '  --permission-mode <mode>  (choices: \"acceptEdits\", \"bypassPermissions\", \"default\", \"plan\")\\n'\n")
	cases := []struct {
		claude, requested, inherited, want string
	}{
		{current, "manual", "", "manual"},
		{older, "manual", "", "default"},
		{current, "inherit", "manual", "manual"},
		{older, "inherit", "manual", "default"},
		{current, "auto", "", "auto"},
		{older, "auto", "", "acceptEdits"},
	}
	for _, tc := range cases {
		cfg := object{"resume": object{"permissionMode": tc.requested}}
		if got := supportedPermissionMode(cfg, tc.claude, tc.inherited); got != tc.want {
			t.Errorf("%s asked for %q (inherited %q): relaunched with %q, want %q", filepath.Base(tc.claude), tc.requested, tc.inherited, got, tc.want)
		}
	}
	if mode := permissionModeOf(object{"permission_mode": "manual"}); mode != "manual" {
		t.Errorf("a hook input reporting permission_mode manual was recorded as %q, so inherit would relaunch it with acceptEdits", mode)
	}
}
