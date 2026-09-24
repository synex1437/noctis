package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureChainsAStatusLineSetAfterInstallInsteadOfDroppingIt(t *testing.T) {
	cliPluginTree(t)
	cliWrite(t, filepath.Join(files.pluginRoot, "bin", binaryFileName()), []byte("engine"))
	cliWrite(t, files.config, []byte(`{"statusline": {"chainCommand": "old-bar"}}`))
	cliWrite(t, files.settings, []byte(`{"statusLine": {"type": "command", "command": "new-bar --fancy"}}`))

	capturedStdout(t, func() { firstRunSetup(shippedDefaults(t)) })

	if chain := getString(section(readJSON(files.config), "statusline"), "chainCommand"); chain != "new-bar --fancy" {
		t.Fatalf("the status line set after install (new-bar --fancy) was dropped: the chain is %q, so the bar shows the old one and uninstall restores it", chain)
	}
	if command := getString(getMap(readJSON(files.settings), "statusLine"), "command"); !strings.Contains(command, pluginName) {
		t.Fatalf("settings.json does not run noctis as its status line: %q", command)
	}
	capturedStdout(t, func() { firstRunSetup(shippedDefaults(t)) })
	if chain := getString(section(readJSON(files.config), "statusline"), "chainCommand"); chain != "new-bar --fancy" {
		t.Fatalf("the next session start changed the chain again: %q", chain)
	}
	var err error
	capturedStdout(t, func() { err = undoSetupSettings(files.settings, readJSON(files.settings), readJSON(files.config)) })
	if err != nil {
		t.Fatal(err)
	}
	if command := getString(getMap(readJSON(files.settings), "statusLine"), "command"); command != "new-bar --fancy" {
		t.Fatalf("uninstall put back %q instead of the status line the user had set last (new-bar --fancy)", command)
	}
}

func TestSetupChainsAStatusLineSetAfterInstallInsteadOfDroppingIt(t *testing.T) {
	sandboxFiles(t)
	previous := args
	t.Cleanup(func() { args = previous })
	args = parseArgs([]string{"setup", "--permissions", "keep"})
	account := t.TempDir()
	cliWrite(t, filepath.Join(account, "settings.json"), []byte(`{"statusLine": {"type": "command", "command": "new-bar --fancy"}}`))
	configFile := filepath.Join(account, pluginName, "config.json")
	defaults := shippedDefaults(t)
	config := cloneObject(defaults)
	config["statusline"] = object{"chainCommand": "old-bar"}

	var err error
	printed := capturedStdout(t, func() {
		err = wireSettings(account, filepath.Join(account, "bin", binaryFileName()), config, configFile, defaults, false)
	})
	if err != nil {
		t.Fatal(err)
	}
	if chain := getString(section(readJSON(configFile), "statusline"), "chainCommand"); chain != "new-bar --fancy" {
		t.Fatalf("setup dropped the status line set after install (new-bar --fancy): the chain is %q", chain)
	}
	if !strings.Contains(printed, "new-bar --fancy") {
		t.Fatalf("setup did not say which status line it chained:\n%s", printed)
	}
}
