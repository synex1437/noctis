package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// settingsRaceClaude puts a claude on PATH whose --help, which setup runs to learn the permission modes,
// replaces settings.json with meanwhile the first time, as a /permissions or /config change made then would.
func settingsRaceClaude(t *testing.T, settingsFile string, meanwhile object) {
	t.Helper()
	bin := t.TempDir()
	next := filepath.Join(bin, "meanwhile.json")
	mustWriteJSON(next, meanwhile)
	if isWindows {
		writeScript(t, filepath.Join(bin, "claude.cmd"), fmt.Sprintf("@move /y \"%s\" \"%s\" >nul 2>&1\r\n@echo --permission-mode \"acceptEdits\", \"auto\", \"default\", \"plan\"\r\n", next, settingsFile))
	} else {
		writeScript(t, filepath.Join(bin, "claude"), fmt.Sprintf("#!/bin/sh\nmv '%s' '%s' 2>/dev/null\nprintf '%%s\\n' '--permission-mode <mode> (choices: \"acceptEdits\", \"auto\", \"default\", \"plan\")'\n", next, settingsFile))
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// settingsRaceHeld runs run while the account's settings lock is held as another noctis process holds
// it; that process then makes change to settings.json and lets the lock go. run must not write
// settings.json before then.
func settingsRaceHeld(t *testing.T, account string, change func(settings object), run func() error) {
	t.Helper()
	settingsFile := filepath.Join(account, "settings.json")
	lock := filepath.Join(account, pluginName, "settings.lock")
	cliWrite(t, lock, []byte(strconv.Itoa(os.Getpid())))
	before := cliRead(t, settingsFile)
	problems := []string{}
	capturedStdout(t, func() {
		finished := make(chan error, 1)
		go func() { finished <- run() }()
		time.Sleep(300 * time.Millisecond)
		if now, _ := os.ReadFile(settingsFile); !bytes.Equal(now, before) {
			problems = append(problems, fmt.Sprintf("settings.json was written while another process held the settings lock:\n%s", now))
		}
		settings := readJSON(settingsFile)
		change(settings)
		if err := os.WriteFile(settingsFile, marshalPretty(settings), 0o600); err != nil {
			problems = append(problems, err.Error())
		}
		if err := os.Remove(lock); err != nil {
			problems = append(problems, err.Error())
		}
		if err := <-finished; err != nil {
			problems = append(problems, err.Error())
		}
	})
	if len(problems) > 0 {
		t.Fatal(strings.Join(problems, "\n"))
	}
}

func TestSetupKeepsWhatIsWrittenToSettingsWhileItAsksClaudeForThePermissionModes(t *testing.T) {
	sandboxFiles(t)
	account := recordAccount(t, object{"theme": "dark", "permissions": object{"allow": []any{"Bash(npm test)"}}})
	settingsFile := filepath.Join(account, "settings.json")
	settingsRaceClaude(t, settingsFile, object{
		"theme":       "light",
		"outputStyle": "Explanatory",
		"env":         object{"MY_TOOL_HOME": "/opt/tool"},
		"permissions": object{"allow": []any{"Bash(npm test)", "Bash(go test ./...)"}},
	})

	recordSetup(t, account, "max")

	settings := readJSON(settingsFile)
	env, permissions := getMap(settings, "env"), getMap(settings, "permissions")
	if getString(settings, "theme") != "light" || getString(settings, "outputStyle") != "Explanatory" || getString(env, "MY_TOOL_HOME") != "/opt/tool" || len(getList(permissions, "allow")) != 2 {
		t.Fatalf("what was written to settings.json while setup ran claude --help is gone: %v", settings)
	}
	if getString(env, "CLAUDE_CODE_EFFORT_LEVEL") != "max" || getString(permissions, "defaultMode") != "auto" || !strings.Contains(getString(getMap(settings, "statusLine"), "command"), "statusline") {
		t.Fatalf("setup's own changes are missing: %v", settings)
	}
}

func TestUninstallKeepsWhatIsWrittenToSettingsAfterItReadThem(t *testing.T) {
	sandboxFiles(t)
	recordClaudeWithAuto(t)
	account := recordAccount(t, object{"theme": "dark"})
	settingsFile := filepath.Join(account, "settings.json")
	recordSetup(t, account, "max")
	read := readJSON(settingsFile)
	meanwhile := readJSON(settingsFile)
	meanwhile["theme"] = "light"
	getMap(meanwhile, "env")["MY_TOOL_HOME"] = "/opt/tool"
	getMap(meanwhile, "permissions")["allow"] = []any{"Bash(npm test)"}
	mustWriteJSON(settingsFile, meanwhile)

	var err error
	capturedStdout(t, func() {
		err = undoSetupSettings(settingsFile, read, readJSON(filepath.Join(account, pluginName, "config.json")))
	})

	want := object{"theme": "light", "env": object{"MY_TOOL_HOME": "/opt/tool"}, "permissions": object{"allow": []any{"Bash(npm test)"}}}
	if settings := readJSON(settingsFile); err != nil || !reflect.DeepEqual(settings, want) {
		t.Fatalf("uninstall should take back only what setup set and keep what was written after it read settings.json (%v): %v", err, settings)
	}
}

func TestSetupAndUninstallWaitForTheSettingsLockAnotherProcessHolds(t *testing.T) {
	sandboxFiles(t)
	recordClaudeWithAuto(t)
	previous := args
	t.Cleanup(func() { args = previous })
	args = parseArgs([]string{"setup"})
	defaults := shippedDefaults(t)
	account := recordAccount(t, object{"theme": "dark"})
	settingsFile := filepath.Join(account, "settings.json")
	configFile := filepath.Join(account, pluginName, "config.json")

	settingsRaceHeld(t, account, func(settings object) { settings["outputStyle"] = "Explanatory" }, func() error {
		return wireSettings(account, filepath.Join(account, "bin", binaryFileName()), cloneObject(defaults), configFile, defaults, false)
	})
	if settings := readJSON(settingsFile); getString(settings, "outputStyle") != "Explanatory" || effortInEffect(settings) != "xhigh" {
		t.Fatalf("setup, held back by the settings lock, lost the holder's change or its own: %v", settings)
	}

	read := readJSON(settingsFile)
	settingsRaceHeld(t, account, func(settings object) { settings["theme"] = "light" }, func() error {
		return undoSetupSettings(settingsFile, read, readJSON(configFile))
	})
	if settings, want := readJSON(settingsFile), (object{"theme": "light", "outputStyle": "Explanatory"}); !reflect.DeepEqual(settings, want) {
		t.Fatalf("uninstall, held back by the settings lock, should leave %v; settings.json holds %v", want, settings)
	}
}

func TestSetupKeepsTheWindowAutocompactSavesWhileItAsksClaudeForThePermissionModes(t *testing.T) {
	low := object{"modelSettings": object{"claude-opus-5-5": object{"effortLevel": "low"}}}
	for _, race := range []struct{ before, meanwhile object }{
		{object{}, object{"modelSettings": object{"claude-opus-5-5": object{"autoCompactWindow": float64(400000)}}}},
		{low, object{"modelSettings": object{"claude-opus-5-5": object{"effortLevel": "low", "autoCompactWindow": float64(400000)}}}},
	} {
		sandboxFiles(t)
		clearCompactionVariables(t)
		account := recordAccount(t, race.before)
		settingsFile := filepath.Join(account, "settings.json")
		settingsRaceClaude(t, settingsFile, race.meanwhile)

		recordSetup(t, account, "high")

		opus := getMap(getMap(readJSON(settingsFile), "modelSettings"), "claude-opus-5-5")
		if want := (object{"effortLevel": "high", "autoCompactWindow": float64(400000)}); !reflect.DeepEqual(opus, want) {
			t.Errorf("from %v, setup should save its level beside the window /autocompact saved for the model while setup ran claude --help: want %v, settings.json holds %v", race.before, want, opus)
		}
	}
}

func TestUninstallKeepsTheWindowAutocompactSavesWhileItWaitsForTheSettingsLock(t *testing.T) {
	low := object{"modelSettings": object{"claude-opus-5-5": object{"effortLevel": "low"}}}
	for _, race := range []struct{ before, want object }{
		{object{}, object{"autoCompactWindow": float64(140000)}},
		{low, object{"effortLevel": "low", "autoCompactWindow": float64(140000)}},
	} {
		sandboxFiles(t)
		clearCompactionVariables(t)
		recordClaudeWithAuto(t)
		account := recordAccount(t, race.before)
		settingsFile := filepath.Join(account, "settings.json")
		recordSetup(t, account, "high")
		read := readJSON(settingsFile)

		settingsRaceHeld(t, account, func(settings object) {
			getMap(getMap(settings, "modelSettings"), "claude-opus-5-5")["autoCompactWindow"] = float64(140000)
		}, func() error {
			return undoSetupSettings(settingsFile, read, readJSON(filepath.Join(account, pluginName, "config.json")))
		})

		if opus := getMap(getMap(readJSON(settingsFile), "modelSettings"), "claude-opus-5-5"); !reflect.DeepEqual(opus, race.want) {
			t.Errorf("from %v, uninstall should take back its level beside the window /autocompact saved for the model while uninstall waited for the settings lock: want %v, settings.json holds %v", race.before, race.want, opus)
		}
	}
}

func TestUninstallLeavesNoStateFolderInAnAccountThatHadNone(t *testing.T) {
	sandboxFiles(t)
	account := recordAccount(t, object{"theme": "dark"})

	after, _ := recordUninstall(t, account)

	if statSafe(filepath.Join(account, pluginName)) != nil || !reflect.DeepEqual(after, object{"theme": "dark"}) {
		t.Fatalf("uninstall in an account without noctis should leave it as it was: settings %v, state folder there: %v", after, statSafe(filepath.Join(account, pluginName)) != nil)
	}
}
