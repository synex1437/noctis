package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestARelaunchKeepsTheSessionsOwnPermissionModeWithTheShippedConfig(t *testing.T) {
	sandboxFiles(t)
	recordClaudeWithAuto(t)
	cfg := shippedDefaults(t)
	for _, mode := range []string{"default", "plan", "acceptEdits", "auto"} {
		if got := supportedPermissionMode(cfg, claudeExecutable(), mode); got != mode {
			t.Fatalf("a session that ran in %s mode is relaunched in %s mode with the shipped config", mode, got)
		}
	}
}

func setupKeepAccount(t *testing.T, relaunchMode string) (string, string) {
	t.Helper()
	sandboxFiles(t)
	recordClaudeWithAuto(t)
	account := recordAccount(t, object{"permissions": object{"defaultMode": "default"}})
	config := cloneObject(shippedDefaults(t))
	resume := cloneObject(section(config, "resume"))
	resume["permissionMode"] = relaunchMode
	config["resume"] = resume
	configFile := filepath.Join(account, pluginName, "config.json")
	ensureDir(filepath.Dir(configFile))
	mustWriteJSON(configFile, config)
	return account, recordSetup(t, account, "max", "--permissions", "keep")
}

func TestSetupWithPermissionsKeepAlsoRelaunchesInTheSessionsOwnMode(t *testing.T) {
	account, output := setupKeepAccount(t, "auto")
	if got := getString(section(readJSON(filepath.Join(account, pluginName, "config.json")), "resume"), "permissionMode"); got != "inherit" {
		t.Fatalf("setup --permissions keep left settings.json alone but relaunches still run in %q mode", got)
	}
	if !strings.Contains(output, "resume.permissionMode=inherit") {
		t.Fatalf("setup did not say that relaunches now keep the session's mode:\n%s", output)
	}
	if got, _ := recordValue(readJSON(filepath.Join(account, "settings.json")), "permissions", "defaultMode"); got != "default" {
		t.Fatalf("setup --permissions keep changed defaultMode to %q", got)
	}
}

func TestSetupWithPermissionsKeepLeavesARelaunchModeTheUserChose(t *testing.T) {
	account, _ := setupKeepAccount(t, "acceptEdits")
	if got := getString(section(readJSON(filepath.Join(account, pluginName, "config.json")), "resume"), "permissionMode"); got != "acceptEdits" {
		t.Fatalf("setup --permissions keep replaced the relaunch mode the user chose with %q", got)
	}
}

const (
	claudeModesWithDontAsk    = `"acceptEdits", "auto", "bypassPermissions", "manual", "dontAsk", "plan"`
	claudeModesWithoutDontAsk = `"acceptEdits", "bypassPermissions", "default", "plan"`
)

func claudeListingModes(t *testing.T, modes string) string {
	t.Helper()
	bin := t.TempDir()
	if isWindows {
		return writeScript(t, filepath.Join(bin, "claude.cmd"), "@echo --permission-mode "+modes+"\r\n")
	}
	return writeScript(t, filepath.Join(bin, "claude"), "#!/bin/sh\nprintf '%s\\n' '--permission-mode <mode> (choices: "+modes+")'\n")
}

func TestASessionThatRanInDontAskIsRelaunchedInDontAsk(t *testing.T) {
	sandboxFiles(t)
	cfg := shippedDefaults(t)
	withDontAsk, withoutDontAsk := claudeListingModes(t, claudeModesWithDontAsk), claudeListingModes(t, claudeModesWithoutDontAsk)
	mode := permissionModeOf(object{"permission_mode": "dontAsk"})
	if got := supportedPermissionMode(cfg, withDontAsk, mode); got != "dontAsk" {
		t.Errorf("a session that ran in dontAsk mode (recorded as %q) is relaunched in %s mode by a claude that lists dontAsk", mode, got)
	}
	if got := supportedPermissionMode(cfg, withoutDontAsk, mode); got != "default" {
		t.Errorf("a session that ran in dontAsk mode is relaunched in %s mode by a claude that does not list dontAsk; want default", got)
	}
	chosen := object{"resume": object{"permissionMode": "dontAsk"}}
	if got := supportedPermissionMode(chosen, withDontAsk, ""); got != "dontAsk" {
		t.Errorf("resume.permissionMode dontAsk relaunches in %s mode by a claude that lists dontAsk", got)
	}
	if got := supportedPermissionMode(chosen, withoutDontAsk, ""); got != "default" {
		t.Errorf("resume.permissionMode dontAsk relaunches in %s mode by a claude that does not list dontAsk; want default", got)
	}
}

func TestASessionInAModeNoctisCannotReadIsRelaunchedInDefaultMode(t *testing.T) {
	sandboxFiles(t)
	cfg := shippedDefaults(t)
	claude := claudeListingModes(t, claudeModesWithDontAsk)
	inputs := []struct {
		name  string
		input object
	}{
		{"no permission_mode", object{}},
		{"a mode noctis does not know", object{"permission_mode": "strict"}},
		{"a number for a mode", object{"permission_mode": float64(3)}},
	}
	for _, tc := range inputs {
		if got := supportedPermissionMode(cfg, claude, permissionModeOf(tc.input)); got != "default" {
			t.Errorf("a session whose hook input carried %s is relaunched in %s mode; want default", tc.name, got)
		}
	}
	if got := supportedPermissionMode(object{"resume": object{"permissionMode": "strict"}}, claude, ""); got != "default" {
		t.Errorf("resume.permissionMode strict, a mode noctis does not know, relaunches in %s mode; want default", got)
	}
}

func TestABypassPermissionsSessionIsStillRelaunchedInAcceptEdits(t *testing.T) {
	sandboxFiles(t)
	claude := claudeListingModes(t, claudeModesWithDontAsk)
	if got := supportedPermissionMode(shippedDefaults(t), claude, permissionModeOf(object{"permission_mode": "bypassPermissions"})); got != "acceptEdits" {
		t.Fatalf("a session that ran in bypassPermissions mode is relaunched in %s mode; want acceptEdits", got)
	}
	if got := supportedPermissionMode(object{"resume": object{"permissionMode": "bypassPermissions"}}, claude, ""); got != "acceptEdits" {
		t.Fatalf("resume.permissionMode bypassPermissions relaunches in %s mode; want acceptEdits", got)
	}
}
