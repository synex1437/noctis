package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func i2PaddedLine(command string) object {
	return object{"type": "command", "command": command, "padding": float64(0)}
}

func TestSetupKeepsTheFieldsOfTheStatusLineItChainsAndUninstallPutsItBackWhole(t *testing.T) {
	sandboxFiles(t)
	account := recordAccount(t, object{"statusLine": i2PaddedLine("my-bar --fancy")})
	settingsFile := filepath.Join(account, "settings.json")

	for _, effort := range []string{"max", "low"} {
		recordSetup(t, account, effort, "--permissions", "keep")
		line := getMap(readJSON(settingsFile), "statusLine")
		if !strings.Contains(getString(line, "command"), pluginName) || line["padding"] != float64(0) {
			t.Fatalf("setup put noctis's status line in place of one with padding 0 and dropped the padding: %v", line)
		}
	}
	after, _ := recordUninstall(t, account)

	if line := getMap(after, "statusLine"); !reflect.DeepEqual(line, i2PaddedLine("my-bar --fancy")) {
		t.Fatalf("uninstall did not put back the whole status line the user had: %v", line)
	}
}

func TestEnsureKeepsTheFieldsOfTheStatusLineItChainsAndUninstallPutsItBackWhole(t *testing.T) {
	cliPluginTree(t)
	cliWrite(t, filepath.Join(files.pluginRoot, "bin", binaryFileName()), []byte("engine"))
	mustWriteJSON(files.settings, object{"statusLine": i2PaddedLine("new-bar")})

	capturedStdout(t, func() { firstRunSetup(shippedDefaults(t)) })

	line := getMap(readJSON(files.settings), "statusLine")
	if !strings.Contains(getString(line, "command"), pluginName) || line["padding"] != float64(0) {
		t.Fatalf("a session start put noctis's status line in place of one with padding 0 and dropped the padding: %v", line)
	}
	var err error
	capturedStdout(t, func() { err = undoSetupSettings(files.settings, readJSON(files.settings), readJSON(files.config)) })
	if err != nil {
		t.Fatal(err)
	}
	if line := getMap(readJSON(files.settings), "statusLine"); !reflect.DeepEqual(line, i2PaddedLine("new-bar")) {
		t.Fatalf("uninstall did not put back the whole status line the user had: %v", line)
	}
}

func TestUninstallRemovesAStatusLineSetupAddedWhereThereWasNone(t *testing.T) {
	sandboxFiles(t)
	account := recordAccount(t, object{"theme": "dark"})
	settingsFile := filepath.Join(account, "settings.json")
	recordSetup(t, account, "max", "--permissions", "keep")
	settings := readJSON(settingsFile)
	getMap(settings, "statusLine")["padding"] = float64(2)
	mustWriteJSON(settingsFile, settings)

	after, _ := recordUninstall(t, account)

	if line, present := after["statusLine"]; present {
		t.Fatalf("there was no status line before setup, yet uninstall left %v", line)
	}
}
