package main

import (
	"os"
	"path/filepath"
	"strings"
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

func TestUninstallLeavesAValueSetupHoldsNoRecordOf(t *testing.T) {
	sandboxFiles(t)
	account := recordAccount(t, object{"env": object{"CLAUDE_CODE_EFFORT_LEVEL": "high"}, "permissions": object{"defaultMode": "acceptEdits"}})
	mustWriteJSON(filepath.Join(account, pluginName, "config.json"), object{"thresholds": object{"session5h": float64(92)}, "managedPermissionMode": "acceptEdits"})

	after, _ := recordUninstall(t, account)

	if got, _ := recordValue(after, "env", "CLAUDE_CODE_EFFORT_LEVEL"); got != "high" {
		t.Errorf("setup holds no record of this effort, yet uninstall made it %q", got)
	}
	if got, _ := recordValue(after, "permissions", "defaultMode"); got != "acceptEdits" {
		t.Errorf("setup recorded no value from before it for this mode (it found acceptEdits already set), yet uninstall made it %q", got)
	}
}

func TestUninstallGivesBackAModeThatAlreadyMatchedWhatSetupWrote(t *testing.T) {
	sandboxFiles(t)
	recordClaudeWithAuto(t)
	cases := []struct {
		name, original string
		runs           [][]string
	}{
		{"acceptEdits before setup, setup --permissions acceptEdits", "acceptEdits", [][]string{{"--permissions", "acceptEdits"}}},
		{"auto before setup, a first setup without --permissions", "auto", [][]string{nil}},
		{"plan, then acceptEdits, then plan again", "plan", [][]string{{"--permissions", "acceptEdits"}, {"--permissions", "plan"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			account := recordAccount(t, object{"permissions": object{"defaultMode": c.original, "allow": []any{"Bash(npm test)"}}})
			for _, flags := range c.runs {
				recordSetup(t, account, "max", flags...)
			}

			after, _ := recordUninstall(t, account)

			if got, _ := recordValue(after, "permissions", "defaultMode"); got != c.original {
				t.Fatalf("defaultMode %s before setup: uninstall left %q", c.original, got)
			}
			if len(getList(getMap(after, "permissions"), "allow")) != 1 {
				t.Fatalf("the allow rules were not kept: %v", getMap(after, "permissions"))
			}
		})
	}
}

func TestUninstallForgetsWhatSetupRecordedSoALaterSetupStartsOver(t *testing.T) {
	sandboxFiles(t)
	recordClaudeWithAuto(t)
	original := object{"model": "sonnet", "env": object{"CLAUDE_CODE_EFFORT_LEVEL": "medium"}, "permissions": object{"defaultMode": "plan"}}
	account := recordAccount(t, original)
	settingsFile := filepath.Join(account, "settings.json")
	configFile := filepath.Join(account, pluginName, "config.json")
	restored := func(settings object) bool {
		effort, _ := recordValue(settings, "env", "CLAUDE_CODE_EFFORT_LEVEL")
		mode, _ := recordValue(settings, "permissions", "defaultMode")
		return getString(settings, "model") == "sonnet" && effort == "medium" && mode == "plan"
	}

	recordSetup(t, account, "max", "--permissions", "keep")
	recordSetup(t, account, "max", "--permissions", "acceptEdits")
	first, _ := recordUninstall(t, account)
	if !restored(first) {
		t.Fatalf("the first uninstall did not put back sonnet, medium and plan: %v", first)
	}
	config := readJSON(configFile)
	for _, key := range []string{"managedModel", "managedEffort", "managedPermissionMode", "managedPermissionPrevious", "managedPermissionKeep"} {
		if _, kept := config[key]; kept {
			t.Errorf("uninstall put settings.json back but kept %s, which describes a setup that is gone: %v", key, config[key])
		}
	}
	if second, _ := recordUninstall(t, account); !restored(second) {
		t.Fatalf("a second uninstall changed what the first one put back: %v", second)
	}

	recordSetup(t, account, "max")
	if mode, _ := recordValue(readJSON(settingsFile), "permissions", "defaultMode"); mode != "auto" {
		t.Fatalf("a setup after an uninstall should start over like a first setup and switch to auto, got %q", mode)
	}
	if again, _ := recordUninstall(t, account); !restored(again) {
		t.Fatalf("the uninstall after a new setup did not put back sonnet, medium and plan: %v", again)
	}
}

func TestAValueSetByHandAfterAnUninstallIsNotTakenForSetups(t *testing.T) {
	sandboxFiles(t)
	account := recordAccount(t, object{})
	settingsFile := filepath.Join(account, "settings.json")
	recordSetup(t, account, "max", "--permissions", "keep")
	recordUninstall(t, account)
	settings := readJSON(settingsFile)
	settings["env"] = object{"CLAUDE_CODE_EFFORT_LEVEL": "max"}
	mustWriteJSON(settingsFile, settings)

	recordSetup(t, account, "max", "--permissions", "keep")
	after, _ := recordUninstall(t, account)

	if got, _ := recordValue(after, "env", "CLAUDE_CODE_EFFORT_LEVEL"); got != "max" {
		t.Fatalf("effort max set by hand after an uninstall matched the old record of the first setup and was removed by the next uninstall: %q", got)
	}
}

func TestAStatusLineSetAfterAnUninstallIsChainedAndPutBackByTheNextOne(t *testing.T) {
	sandboxFiles(t)
	account := recordAccount(t, object{"statusLine": object{"type": "command", "command": "my-old-line"}})
	settingsFile := filepath.Join(account, "settings.json")
	configFile := filepath.Join(account, pluginName, "config.json")
	chained := func() string { return getString(section(readJSON(configFile), "statusline"), "chainCommand") }

	recordSetup(t, account, "max", "--permissions", "keep")
	first, _ := recordUninstall(t, account)
	if got := getString(getMap(first, "statusLine"), "command"); got != "my-old-line" {
		t.Fatalf("the first uninstall put back the status line %q", got)
	}
	if chain := chained(); chain != "" {
		t.Errorf("uninstall put the status line back but kept statusline.chainCommand %q, which describes a setup that is gone", chain)
	}
	settings := readJSON(settingsFile)
	settings["statusLine"] = object{"type": "command", "command": "my-new-line"}
	mustWriteJSON(settingsFile, settings)

	recordSetup(t, account, "max", "--permissions", "keep")
	if chain := chained(); chain != "my-new-line" {
		t.Errorf("the setup after an uninstall chained %q, not the status line it replaced", chain)
	}
	second, _ := recordUninstall(t, account)

	if got := getString(getMap(second, "statusLine"), "command"); got != "my-new-line" {
		t.Fatalf("the status line set by hand after the first uninstall came back as %q", got)
	}
}

func TestAStatusLineRemovedAfterAnUninstallIsNeitherChainedNorPutBackByTheNextOne(t *testing.T) {
	sandboxFiles(t)
	account := recordAccount(t, object{"statusLine": object{"type": "command", "command": "my-old-line"}})
	settingsFile := filepath.Join(account, "settings.json")
	configFile := filepath.Join(account, pluginName, "config.json")

	recordSetup(t, account, "max", "--permissions", "keep")
	recordUninstall(t, account)
	settings := readJSON(settingsFile)
	delete(settings, "statusLine")
	mustWriteJSON(settingsFile, settings)
	recordSetup(t, account, "max", "--permissions", "keep")
	if chain := getString(section(readJSON(configFile), "statusline"), "chainCommand"); chain != "" {
		t.Errorf("my-old-line was removed by hand after the uninstall, yet the next setup chains %q above noctis's status line", chain)
	}
	after, _ := recordUninstall(t, account)

	if line, present := after["statusLine"]; present {
		t.Fatalf("my-old-line was removed by hand after the first uninstall, yet the second one put back %v", line)
	}
}

func recordEnglish(t *testing.T) {
	t.Helper()
	previous := locale
	t.Cleanup(func() { locale = previous })
	locale = "en"
}

func recordClaudeWithoutAuto(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	if isWindows {
		writeScript(t, filepath.Join(bin, "claude.cmd"), "@echo --permission-mode \"acceptEdits\", \"default\", \"plan\"\r\n")
	} else {
		writeScript(t, filepath.Join(bin, "claude"), "#!/bin/sh\nprintf '%s\\n' '--permission-mode <mode> (choices: \"acceptEdits\", \"default\", \"plan\")'\n")
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestSetupNamesThePermissionModeFromBeforeSetupAndHowToGetItBack(t *testing.T) {
	sandboxFiles(t)
	recordEnglish(t)
	recordClaudeWithAuto(t)
	cases := []struct {
		name, original string
		runs           [][]string
		say            []string
		never          []string
	}{
		{"no mode before a first setup", "", [][]string{nil}, []string{"permissions.defaultMode=auto (not set before setup; delete it from settings.json to go back)"}, nil},
		{"plan before --permissions acceptEdits", "plan", [][]string{{"--permissions", "acceptEdits"}}, []string{"permissions.defaultMode=acceptEdits (before setup: plan; noctis setup --permissions plan puts it back)"}, nil},
		{"plan before two runs", "plan", [][]string{{"--permissions", "acceptEdits"}, {"--permissions", "default"}}, []string{"permissions.defaultMode=default (before setup: plan; noctis setup --permissions plan puts it back)"}, nil},
		{"the mode setup writes, already set", "acceptEdits", [][]string{{"--permissions", "acceptEdits"}}, []string{"permissions.defaultMode=acceptEdits (as it was before setup)"}, nil},
		{"a mode setup never writes", "bypassPermissions", [][]string{{"--permissions", "acceptEdits"}}, []string{"permissions.defaultMode=acceptEdits (before setup: bypassPermissions, which setup cannot set; set it again in settings.json to go back)"}, []string{"--permissions bypassPermissions"}},
		{"auto before --permissions plan where Claude Code offers auto", "auto", [][]string{{"--permissions", "plan"}}, []string{"permissions.defaultMode=plan (before setup: auto; noctis setup --permissions auto puts it back)"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			permissions := object{}
			if c.original != "" {
				permissions["defaultMode"] = c.original
			}
			account := recordAccount(t, object{"permissions": permissions})
			output := ""
			for _, flags := range c.runs {
				output = recordSetup(t, account, "max", flags...)
			}

			for _, text := range c.say {
				if !strings.Contains(output, text) {
					t.Errorf("setup does not say %q:\n%s", text, output)
				}
			}
			for _, text := range c.never {
				if strings.Contains(output, text) {
					t.Errorf("setup says %q, a value it would refuse:\n%s", text, output)
				}
			}
		})
	}
}

func TestSetupDoesNotOfferAutoBackWhereClaudeCodeHasNoAuto(t *testing.T) {
	sandboxFiles(t)
	recordEnglish(t)
	recordClaudeWithoutAuto(t)
	account := recordAccount(t, object{"permissions": object{"defaultMode": "auto"}})

	output := recordSetup(t, account, "max", "--permissions", "plan")

	if !strings.Contains(output, "permissions.defaultMode=plan (before setup: auto, which setup cannot set; set it again in settings.json to go back)") || strings.Contains(output, "--permissions auto puts it back") {
		t.Fatalf("setup offers --permissions auto as the way back where Claude Code has no auto mode:\n%s", output)
	}
}

func TestSetupSaysWhenItLeavesThePermissionModeAsItIs(t *testing.T) {
	sandboxFiles(t)
	recordEnglish(t)
	recordClaudeWithAuto(t)
	for _, c := range []struct {
		name   string
		byHand func(permissions object)
		say    string
	}{
		{"set to default by hand", func(p object) { p["defaultMode"] = "default" }, "permissions.defaultMode left at default (setup changes it only on the account's first setup or with --permissions)"},
		{"removed by hand", func(p object) { delete(p, "defaultMode") }, "permissions.defaultMode left unset (setup changes it only on the account's first setup or with --permissions)"},
		{"untouched", func(object) {}, "permissions.defaultMode left at auto (setup changes it only on the account's first setup or with --permissions)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			account := recordAccount(t, object{"permissions": object{"defaultMode": "plan"}})
			settingsFile := filepath.Join(account, "settings.json")
			recordSetup(t, account, "max")
			settings := readJSON(settingsFile)
			c.byHand(getMap(settings, "permissions"))
			mustWriteJSON(settingsFile, settings)

			output := recordSetup(t, account, "low")

			if !strings.Contains(output, c.say) {
				t.Fatalf("setup does not say %q:\n%s", c.say, output)
			}
		})
	}
	account := recordAccount(t, object{"permissions": object{"defaultMode": "plan"}})
	if output := recordSetup(t, account, "max", "--permissions", "keep"); strings.Contains(output, "permissions.defaultMode") {
		t.Fatalf("--permissions keep should leave the permission mode out of the output:\n%s", output)
	}
}

func TestUninstallSaysItSetTheValuesBack(t *testing.T) {
	sandboxFiles(t)
	recordEnglish(t)
	account := recordAccount(t, object{"env": object{"CLAUDE_CODE_EFFORT_LEVEL": "medium"}, "permissions": object{"defaultMode": "plan"}})
	recordSetup(t, account, "max", "--permissions", "acceptEdits")

	_, output := recordUninstall(t, account)

	if !strings.Contains(output, "settings.json: statusLine, effort and permission mode set back to what they were before setup (a value you changed since stays)") || strings.Contains(output, "removed;") {
		t.Fatalf("uninstall does not say it set the values back:\n%s", output)
	}
}
