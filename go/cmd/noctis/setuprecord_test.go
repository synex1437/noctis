package main

import (
	"os"
	"path/filepath"
	"testing"
)

func recordClaudeWithAuto(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	if isWindows {
		writeScript(t, filepath.Join(bin, "claude.cmd"), "@echo --permission-mode \"acceptEdits\", \"auto\", \"default\", \"plan\"\r\n")
	} else {
		writeScript(t, filepath.Join(bin, "claude"), "#!/bin/sh\nprintf '%s\\n' '--permission-mode <mode> (choices: \"acceptEdits\", \"auto\", \"default\", \"plan\")'\n")
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func recordSetup(t *testing.T, account, effort string, flags ...string) string {
	t.Helper()
	previous := args
	defer func() { args = previous }()
	args = parseArgs(append([]string{"setup"}, flags...))
	defaults := shippedDefaults(t)
	configFile := filepath.Join(account, pluginName, "config.json")
	config := readJSON(configFile)
	if config == nil {
		config = cloneObject(defaults)
	}
	models := cloneObject(section(config, "models"))
	models["effort"] = effort
	config["models"] = models
	var err error
	output := capturedStdout(t, func() {
		err = wireSettings(account, filepath.Join(account, "bin", binaryFileName()), config, configFile, defaults, false)
	})
	if err != nil {
		t.Fatalf("setup %v failed: %v", flags, err)
	}
	return output
}

func recordUninstall(t *testing.T, account string) (object, string) {
	t.Helper()
	var err error
	output := capturedStdout(t, func() { err = uninstallFrom(account) })
	if err != nil {
		t.Fatalf("uninstall failed: %v", err)
	}
	return readJSON(filepath.Join(account, "settings.json")), output
}

func recordValue(settings object, group, key string) (string, bool) {
	value, present := getMap(settings, group)[key]
	text, _ := value.(string)
	return text, present
}

func recordAccount(t *testing.T, settings object) string {
	t.Helper()
	account := t.TempDir()
	mustWriteJSON(filepath.Join(account, "settings.json"), settings)
	return account
}

func TestARepeatedSetupKeepsTheEffortFoundBeforeTheFirstOne(t *testing.T) {
	sandboxFiles(t)
	for _, original := range []string{"medium", ""} {
		settings := object{"permissions": object{"allow": []any{"Bash(npm test)"}}}
		if original != "" {
			settings["env"] = object{"CLAUDE_CODE_EFFORT_LEVEL": original}
		}
		account := recordAccount(t, settings)

		recordSetup(t, account, "max", "--permissions", "keep")
		recordSetup(t, account, "low", "--permissions", "keep")

		if got, _ := recordValue(readJSON(filepath.Join(account, "settings.json")), "env", "CLAUDE_CODE_EFFORT_LEVEL"); got != "low" {
			t.Fatalf("the second setup did not write its own effort: %q", got)
		}
		after, _ := recordUninstall(t, account)
		got, present := recordValue(after, "env", "CLAUDE_CODE_EFFORT_LEVEL")
		if original == "" && present {
			t.Errorf("no effort before setup, setup with max and then with low, uninstall: effort %q is left", got)
		}
		if original != "" && got != original {
			t.Errorf("effort %s before setup, setup with max and then with low, uninstall: effort is %q", original, got)
		}
	}
}

func TestARepeatedSetupKeepsThePermissionModeFoundBeforeTheFirstOne(t *testing.T) {
	sandboxFiles(t)
	cases := []struct {
		name, original string
		runs           []string
	}{
		{"plan, then acceptEdits, then default", "plan", []string{"acceptEdits", "default"}},
		{"no mode, then acceptEdits, then plan", "", []string{"acceptEdits", "plan"}},
		{"default, then plan, then acceptEdits, then plan", "default", []string{"plan", "acceptEdits", "plan"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			permissions := object{"allow": []any{"Bash(npm test)"}}
			if c.original != "" {
				permissions["defaultMode"] = c.original
			}
			account := recordAccount(t, object{"permissions": permissions})

			for _, mode := range c.runs {
				recordSetup(t, account, "max", "--permissions", mode)
				if got, _ := recordValue(readJSON(filepath.Join(account, "settings.json")), "permissions", "defaultMode"); got != mode {
					t.Fatalf("setup --permissions %s wrote %q", mode, got)
				}
			}
			after, _ := recordUninstall(t, account)

			got, present := recordValue(after, "permissions", "defaultMode")
			if c.original == "" && present {
				t.Errorf("no mode before setup: uninstall left defaultMode %q", got)
			}
			if c.original != "" && got != c.original {
				t.Errorf("defaultMode %s before setup: uninstall left %q", c.original, got)
			}
			if len(getList(getMap(after, "permissions"), "allow")) != 1 {
				t.Errorf("the allow rules were not kept: %v", getMap(after, "permissions"))
			}
		})
	}
}

func TestAValueChangedBetweenTwoSetupsIsTheOneUninstallPutsBack(t *testing.T) {
	sandboxFiles(t)
	account := recordAccount(t, object{"env": object{"CLAUDE_CODE_EFFORT_LEVEL": "medium"}, "permissions": object{"defaultMode": "plan"}})
	settingsFile := filepath.Join(account, "settings.json")

	recordSetup(t, account, "max", "--permissions", "acceptEdits")
	settings := readJSON(settingsFile)
	getMap(settings, "env")["CLAUDE_CODE_EFFORT_LEVEL"] = "high"
	getMap(settings, "permissions")["defaultMode"] = "default"
	mustWriteJSON(settingsFile, settings)
	recordSetup(t, account, "low", "--permissions", "acceptEdits")
	after, _ := recordUninstall(t, account)

	if got, _ := recordValue(after, "env", "CLAUDE_CODE_EFFORT_LEVEL"); got != "high" {
		t.Errorf("the effort set by hand between two setups, which the second one replaced, came back as %q", got)
	}
	if got, _ := recordValue(after, "permissions", "defaultMode"); got != "default" {
		t.Errorf("the mode set by hand between two setups, which the second one replaced, came back as %q", got)
	}
}

func TestAnEffortThatAlreadyMatchedSetupIsRecordedAndSurvivesUninstall(t *testing.T) {
	sandboxFiles(t)
	account := recordAccount(t, object{"env": object{"CLAUDE_CODE_EFFORT_LEVEL": "max"}})

	recordSetup(t, account, "max", "--permissions", "keep")
	record := getMap(readJSON(filepath.Join(account, pluginName, "config.json")), "managedEffort")
	after, _ := recordUninstall(t, account)

	if getString(record, "previous") != "max" || getString(record, "set") != "max" {
		t.Errorf("setup found effort max and wrote max, but recorded %v", record)
	}
	if got, _ := recordValue(after, "env", "CLAUDE_CODE_EFFORT_LEVEL"); got != "max" {
		t.Errorf("the effort max the user had before setup is %q after uninstall", got)
	}
}

func TestAValueChangedAfterTheLastSetupStaysThroughUninstall(t *testing.T) {
	sandboxFiles(t)
	account := recordAccount(t, object{"env": object{"CLAUDE_CODE_EFFORT_LEVEL": "medium"}, "permissions": object{"defaultMode": "plan"}})
	settingsFile := filepath.Join(account, "settings.json")

	recordSetup(t, account, "max", "--permissions", "acceptEdits")
	settings := readJSON(settingsFile)
	getMap(settings, "env")["CLAUDE_CODE_EFFORT_LEVEL"] = "high"
	getMap(settings, "permissions")["defaultMode"] = "default"
	mustWriteJSON(settingsFile, settings)
	after, _ := recordUninstall(t, account)

	if got, _ := recordValue(after, "env", "CLAUDE_CODE_EFFORT_LEVEL"); got != "high" {
		t.Errorf("the effort set by hand after setup became %q on uninstall", got)
	}
	if got, _ := recordValue(after, "permissions", "defaultMode"); got != "default" {
		t.Errorf("the mode set by hand after setup became %q on uninstall", got)
	}
}
