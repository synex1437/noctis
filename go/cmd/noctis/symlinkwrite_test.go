package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func linkedFile(t *testing.T, link, content string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "dotfiles", filepath.Base(link))
	cliWrite(t, target, []byte(content))
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("this machine cannot create a symlink: %v", err)
	}
	return target
}

func fileMode(t *testing.T, file string) os.FileMode {
	t.Helper()
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func stillLinked(t *testing.T, link, target string) {
	t.Helper()
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s was a symlink to %s and is now a regular file", link, target)
	}
	if resolved, err := filepath.EvalSymlinks(link); err != nil || !resolvesTo(resolved, target) {
		t.Fatalf("%s no longer points at %s: %q %v", link, target, resolved, err)
	}
}

func noTempFilesBeside(t *testing.T, files ...string) {
	t.Helper()
	for _, file := range files {
		entries, _ := os.ReadDir(filepath.Dir(file))
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".tmp") {
				t.Fatalf("a temp file was left beside %s: %s", file, entry.Name())
			}
		}
	}
}

func TestAModelSwitchWritesThroughASymlinkedSettingsFile(t *testing.T) {
	sandboxFiles(t)
	target := linkedFile(t, files.settings, `{"model": "fable", "env": {"KEEP": "1"}}`)
	mode := fileMode(t, target)

	if !setSettingsModel("opus") {
		t.Fatal("the model switch reported a failure")
	}

	stillLinked(t, files.settings, target)
	written := readJSON(target)
	if getString(written, "model") != "opus" || getString(getMap(written, "env"), "KEEP") != "1" {
		t.Fatalf("the file the link points at was not updated: %v", written)
	}
	if got := fileMode(t, target); got != mode {
		t.Fatalf("the linked settings file went from mode %v to %v", mode, got)
	}
	noTempFilesBeside(t, files.settings, target)
}

func TestSetupWritesThroughSymlinkedSettingsAndConfigFiles(t *testing.T) {
	sandboxFiles(t)
	previous := args
	t.Cleanup(func() { args = previous })
	args = parseArgs([]string{"setup", "--permissions", "keep"})
	account := t.TempDir()
	settingsFile := filepath.Join(account, "settings.json")
	configFile := filepath.Join(account, pluginName, "config.json")
	settingsTarget := linkedFile(t, settingsFile, `{"permissions": {"allow": ["Bash(npm test)"]}}`)
	configTarget := linkedFile(t, configFile, `{"thresholds": {"session5h": 80}}`)
	settingsMode, configMode := fileMode(t, settingsTarget), fileMode(t, configTarget)
	defaults := shippedDefaults(t)
	config := readJSON(configFile)

	var err error
	capturedStdout(t, func() {
		err = wireSettings(account, filepath.Join(account, "bin", binaryFileName()), config, configFile, defaults, true)
	})

	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	stillLinked(t, settingsFile, settingsTarget)
	stillLinked(t, configFile, configTarget)
	if command := getString(getMap(readJSON(settingsTarget), "statusLine"), "command"); !strings.Contains(command, "statusline") {
		t.Fatalf("the linked settings file did not get the status line: %v", readJSON(settingsTarget))
	}
	if getMap(readJSON(configTarget), "managedEffort") == nil || numberOr(getMap(readJSON(configTarget), "thresholds"), "session5h", 0) != 80 {
		t.Fatalf("the linked config file did not get what setup records: %v", readJSON(configTarget))
	}
	if got := fileMode(t, settingsTarget); got != settingsMode {
		t.Fatalf("the linked settings file went from mode %v to %v", settingsMode, got)
	}
	if got := fileMode(t, configTarget); got != configMode {
		t.Fatalf("the linked config file went from mode %v to %v", configMode, got)
	}
	noTempFilesBeside(t, settingsFile, settingsTarget, configFile, configTarget)

	capturedStdout(t, func() { err = uninstallFrom(account) })

	if err != nil {
		t.Fatalf("uninstall failed: %v", err)
	}
	stillLinked(t, settingsFile, settingsTarget)
	stillLinked(t, configFile, configTarget)
	if restored := readJSON(settingsTarget); getMap(restored, "statusLine") != nil || len(getList(getMap(restored, "permissions"), "allow")) != 1 {
		t.Fatalf("uninstall did not put the linked settings file back: %v", restored)
	}
	if got := fileMode(t, settingsTarget); got != settingsMode {
		t.Fatalf("uninstall moved the linked settings file from mode %v to %v", settingsMode, got)
	}
	noTempFilesBeside(t, settingsFile, settingsTarget, configFile, configTarget)
}

func TestAHostInstallWritesThroughASymlinkedHookFile(t *testing.T) {
	sandboxFiles(t)
	codexHome := t.TempDir()
	hooksFile := filepath.Join(codexHome, "hooks.json")
	target := linkedFile(t, hooksFile, `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "my-linter"}]}]}}`)
	mode := fileMode(t, target)

	if _, err := wireHostHooks("codex", filepath.Join(codexHome, pluginName, "plugin", "bin", binaryFileName()), codexHome); err != nil {
		t.Fatalf("wiring the codex hooks failed: %v", err)
	}

	stillLinked(t, hooksFile, target)
	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "my-linter") || !strings.Contains(string(written), "hook --host codex") {
		t.Fatalf("the linked hook file does not hold both the existing hook and noctis's: %s", written)
	}
	if got := fileMode(t, target); got != mode {
		t.Fatalf("the linked hook file went from mode %v to %v", mode, got)
	}
	noTempFilesBeside(t, hooksFile, target)
}

func TestAnAtomicWriteKeepsTheModeOfTheFileItReplaces(t *testing.T) {
	sandboxFiles(t)
	file := filepath.Join(t.TempDir(), "hooks.json")
	cliWrite(t, file, []byte(`{}`))
	if err := os.Chmod(file, 0o644); err != nil {
		t.Fatal(err)
	}
	mode := fileMode(t, file)
	fresh := filepath.Join(t.TempDir(), "new.json")

	if err := writeJSONAtomic(file, object{"a": float64(1)}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(fresh, object{"a": float64(1)}); err != nil {
		t.Fatal(err)
	}

	if got := fileMode(t, file); got != mode {
		t.Fatalf("a %v file became %v when it was rewritten", mode, got)
	}
	if want := fileMode(t, fresh); !isWindows && want != 0o600 {
		t.Fatalf("a new file should stay private (0600), got %v", want)
	}
}

func TestALinkWhoseFileIsNotThereYetGetsItWritten(t *testing.T) {
	sandboxFiles(t)
	target := filepath.Join(t.TempDir(), "dotfiles", "settings.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, files.settings); err != nil {
		t.Skipf("this machine cannot create a symlink: %v", err)
	}

	if !setSettingsModel("opus") {
		t.Fatal("the model switch reported a failure")
	}

	stillLinked(t, files.settings, target)
	if model := getString(readJSON(target), "model"); model != "opus" {
		t.Fatalf("the file the link points at holds model %q", model)
	}
}
