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
