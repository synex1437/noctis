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

func TestASetupWithoutPermissionsLeavesTheModeAnEarlierSetupChose(t *testing.T) {
	sandboxFiles(t)
	recordClaudeWithAuto(t)
	cases := []struct {
		name     string
		original string
		first    []string
		byHand   func(permissions object)
		want     string
	}{
		{"auto from the first setup, set to default by hand", "plan", nil, func(p object) { p["defaultMode"] = "default" }, "default"},
		{"auto from the first setup, removed by hand", "plan", nil, func(p object) { delete(p, "defaultMode") }, ""},
		{"acceptEdits chosen with --permissions", "", []string{"--permissions", "acceptEdits"}, nil, "acceptEdits"},
		{"keep chosen on the first setup", "plan", []string{"--permissions", "keep"}, nil, "plan"},
		{"keep chosen on the first setup with no mode set", "", []string{"--permissions", "keep"}, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			permissions := object{"allow": []any{"Bash(npm test)"}}
			if c.original != "" {
				permissions["defaultMode"] = c.original
			}
			account := recordAccount(t, object{"permissions": permissions})
			settingsFile := filepath.Join(account, "settings.json")
			recordSetup(t, account, "max", c.first...)
			if c.byHand != nil {
				settings := readJSON(settingsFile)
				c.byHand(getMap(settings, "permissions"))
				mustWriteJSON(settingsFile, settings)
			}

			recordSetup(t, account, "low")

			got, present := recordValue(readJSON(settingsFile), "permissions", "defaultMode")
			if c.want == "" && present {
				t.Fatalf("a setup without --permissions wrote defaultMode %q where an earlier setup's choice left none", got)
			}
			if c.want != "" && got != c.want {
				t.Fatalf("a setup without --permissions made defaultMode %q; an earlier setup's choice left %q", got, c.want)
			}
			if effort, _ := recordValue(readJSON(settingsFile), "env", "CLAUDE_CODE_EFFORT_LEVEL"); effort != "low" {
				t.Fatalf("the second setup did not set its effort: %q", effort)
			}
		})
	}
}

func TestTheFirstSetupSwitchesToAutoAndAnExplicitModeStillSwitches(t *testing.T) {
	sandboxFiles(t)
	recordClaudeWithAuto(t)
	account := recordAccount(t, object{"permissions": object{"defaultMode": "plan"}})
	settingsFile := filepath.Join(account, "settings.json")

	recordSetup(t, account, "max")
	if got, _ := recordValue(readJSON(settingsFile), "permissions", "defaultMode"); got != "auto" {
		t.Fatalf("the first setup without --permissions left defaultMode %q, want auto", got)
	}
	settings := readJSON(settingsFile)
	getMap(settings, "permissions")["defaultMode"] = "default"
	mustWriteJSON(settingsFile, settings)
	recordSetup(t, account, "max", "--permissions", "acceptEdits")
	if got, _ := recordValue(readJSON(settingsFile), "permissions", "defaultMode"); got != "acceptEdits" {
		t.Fatalf("--permissions acceptEdits made defaultMode %q", got)
	}
	recordSetup(t, account, "max", "--permissions", "auto")
	if got, _ := recordValue(readJSON(settingsFile), "permissions", "defaultMode"); got != "auto" {
		t.Fatalf("--permissions auto made defaultMode %q", got)
	}
}

func TestAProfileSwitchLeavesThePermissionModeTheUserChose(t *testing.T) {
	recordClaudeWithAuto(t)
	root := cliPluginTree(t)
	account := t.TempDir()
	settingsFile := filepath.Join(account, "settings.json")
	cliWrite(t, settingsFile, []byte(`{"permissions": {"defaultMode": "plan"}}`))
	env := cliAccountEnv(root, account)

	first := runNoctisCLI(t, env, "setup", "--config-dir", account, "--profile", "noctis")
	if mode, _ := recordValue(readJSON(settingsFile), "permissions", "defaultMode"); first.code != 0 || mode != "auto" {
		t.Fatalf("the first setup should switch to auto, got %q:\n%s", mode, first)
	}
	settings := readJSON(settingsFile)
	getMap(settings, "permissions")["defaultMode"] = "default"
	mustWriteJSON(settingsFile, settings)

	economy := runNoctisCLI(t, env, "setup", "--config-dir", account, "--profile", "economy")

	settings = readJSON(settingsFile)
	if mode, _ := recordValue(settings, "permissions", "defaultMode"); economy.code != 0 || mode != "default" {
		t.Fatalf("the user set defaultMode back to default after setup; a profile switch made it %q:\n%s", mode, economy)
	}
	if effort, _ := recordValue(settings, "env", "CLAUDE_CODE_EFFORT_LEVEL"); effort != "low" {
		t.Fatalf("the profile switch did not set the economy effort: %q", effort)
	}

	switched := runNoctisCLI(t, env, "setup", "--config-dir", account, "--profile", "economy", "--permissions", "auto")
	if mode, _ := recordValue(readJSON(settingsFile), "permissions", "defaultMode"); switched.code != 0 || mode != "auto" {
		t.Fatalf("--permissions auto should still switch the mode, got %q:\n%s", mode, switched)
	}
	uninstall := runNoctisCLI(t, env, "install", "--uninstall", "--config-dir", account, "--host", "claude")
	if mode, _ := recordValue(readJSON(settingsFile), "permissions", "defaultMode"); uninstall.code != 0 || mode != "default" {
		t.Fatalf("uninstall should give back the mode --permissions auto replaced, got %q:\n%s", mode, uninstall)
	}
}
