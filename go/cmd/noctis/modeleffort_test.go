package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTheSavedEffortOfAModelGoesUnderItsCanonicalID(t *testing.T) {
	for model, want := range map[string]string{
		"opus":                              "claude-opus-5-5",
		"Opus":                              "claude-opus-5-5",
		"opus[1m]":                          "claude-opus-5-5",
		"sonnet":                            "claude-sonnet-5-5",
		"fable":                             "claude-fable-5-1",
		"claude-opus-5-5[1m]":               "claude-opus-5-5",
		"claude-sonnet-5-5-20260915":        "claude-sonnet-5-5",
		"claude-haiku-4-5-20251001":         "",
		"haiku":                             "",
		"best":                              "",
		"default":                           "",
		"opusplan":                          "",
		"us.anthropic.claude-opus-5-5-v1:0": "",
		"":                                  "",
	} {
		if got := savedEffortModel(model); got != want {
			t.Errorf("savedEffortModel(%q) is %q, want %q", model, got, want)
		}
	}
	dir := t.TempDir()
	for _, c := range []struct{ model, effort, want string }{
		{"opus", "xhigh", "claude-opus-5-5"},
		{"opus", "max", ""},
		{"opus", "", ""},
		{"opusplan", "high", ""},
	} {
		if got := effortModelFor(dir, c.model, c.effort); got != c.want {
			t.Errorf("effortModelFor(%q, %q) is %q, want %q", c.model, c.effort, got, c.want)
		}
	}
}

func savedLevel(settings object, model string) (string, bool) {
	return recordValue(getMap(settings, "modelSettings"), model, "effortLevel")
}

// effortInEffect is the level a session on the model settings.json selects starts at: the variable,
// else the level saved for that model.
func effortInEffect(settings object) string {
	if level := getString(getMap(settings, "env"), "CLAUDE_CODE_EFFORT_LEVEL"); level != "" {
		return level
	}
	level, _ := savedLevel(settings, savedEffortModel(getString(settings, "model")))
	return level
}

func TestSetupSavesALevelBelowMaxForTheModelAndTakesTheVariableAway(t *testing.T) {
	sandboxFiles(t)
	recordEnglish(t)
	account := recordAccount(t, object{"env": object{"CLAUDE_CODE_EFFORT_LEVEL": "medium"}, "permissions": object{"allow": []any{"Bash(npm test)"}}})

	output := recordSetup(t, account, "xhigh", "--permissions", "keep")

	settings := readJSON(filepath.Join(account, "settings.json"))
	if level, _ := savedLevel(settings, "claude-opus-5-5"); level != "xhigh" {
		t.Fatalf("setup saved %q for claude-opus-5-5, want xhigh: %v", level, settings)
	}
	if level, present := recordValue(settings, "env", "CLAUDE_CODE_EFFORT_LEVEL"); present {
		t.Errorf("env.CLAUDE_CODE_EFFORT_LEVEL=%s is left, and it overrides the saved level", level)
	}
	if !strings.Contains(output, "modelSettings.claude-opus-5-5.effortLevel=xhigh") || !strings.Contains(output, "read when a session starts") {
		t.Errorf("the output does not say where the level went:\n%s", output)
	}
	config := readJSON(filepath.Join(account, pluginName, "config.json"))
	if record := getMap(config, "managedEffort"); getString(record, "previous") != "medium" || getString(record, "set") != "" {
		t.Errorf("the variable setup took away is recorded as %v", record)
	}

	after, _ := recordUninstall(t, account)

	if level, _ := recordValue(after, "env", "CLAUDE_CODE_EFFORT_LEVEL"); level != "medium" {
		t.Errorf("effort medium before setup: uninstall made the variable %q", level)
	}
	if saved, present := after["modelSettings"]; present {
		t.Errorf("no modelSettings before setup: uninstall left %v", saved)
	}
}

func TestSetupKeepsWhatAModelSettingsEntryHeld(t *testing.T) {
	sandboxFiles(t)
	account := recordAccount(t, object{"modelSettings": object{
		"claude-opus-5-5":   object{"effortLevel": "low", "note": "mine"},
		"claude-sonnet-5-5": object{"effortLevel": "high"},
	}})

	recordSetup(t, account, "xhigh", "--permissions", "keep")
	settings := readJSON(filepath.Join(account, "settings.json"))
	if level, _ := savedLevel(settings, "claude-opus-5-5"); level != "xhigh" || getString(getMap(getMap(settings, "modelSettings"), "claude-opus-5-5"), "note") != "mine" {
		t.Fatalf("setup did not save xhigh next to what the entry held: %v", getMap(settings, "modelSettings"))
	}
	after, _ := recordUninstall(t, account)

	opus := getMap(getMap(after, "modelSettings"), "claude-opus-5-5")
	if getString(opus, "effortLevel") != "low" || getString(opus, "note") != "mine" {
		t.Errorf("the entry held low before setup: uninstall left %v", opus)
	}
	if level, _ := savedLevel(after, "claude-sonnet-5-5"); level != "high" {
		t.Errorf("setup touched another model's level: %q", level)
	}
}

func TestALevelChangedAfterSetupStaysThroughUninstall(t *testing.T) {
	sandboxFiles(t)
	account := recordAccount(t, object{})
	settingsFile := filepath.Join(account, "settings.json")
	recordSetup(t, account, "xhigh", "--permissions", "keep")
	settings := readJSON(settingsFile)
	getMap(getMap(settings, "modelSettings"), "claude-opus-5-5")["effortLevel"] = "high"
	mustWriteJSON(settingsFile, settings)

	after, _ := recordUninstall(t, account)

	if level, _ := savedLevel(after, "claude-opus-5-5"); level != "high" {
		t.Errorf("the level set by hand after setup became %q on uninstall", level)
	}
}

func TestMaxGoesInTheVariableAndTakesTheSavedLevelBack(t *testing.T) {
	sandboxFiles(t)
	account := recordAccount(t, object{})
	settingsFile := filepath.Join(account, "settings.json")

	recordSetup(t, account, "xhigh", "--permissions", "keep")
	recordSetup(t, account, "max", "--permissions", "keep")

	settings := readJSON(settingsFile)
	if saved, present := settings["modelSettings"]; present {
		t.Errorf("setup at max left the level it saved before: %v", saved)
	}
	if level, _ := recordValue(settings, "env", "CLAUDE_CODE_EFFORT_LEVEL"); level != "max" {
		t.Errorf("setup at max wrote the variable %q", level)
	}
	if record := getMap(readJSON(filepath.Join(account, pluginName, "config.json")), "managedModelEffort"); record != nil {
		t.Errorf("setup at max still records a saved level: %v", record)
	}

	recordSetup(t, account, "high", "--permissions", "keep")

	settings = readJSON(settingsFile)
	if level, _ := savedLevel(settings, "claude-opus-5-5"); level != "high" {
		t.Errorf("setup at high after max saved %q", level)
	}
	if level, present := recordValue(settings, "env", "CLAUDE_CODE_EFFORT_LEVEL"); present {
		t.Errorf("setup at high after max left the variable %q", level)
	}
	after, _ := recordUninstall(t, account)
	if _, present := recordValue(after, "env", "CLAUDE_CODE_EFFORT_LEVEL"); present {
		t.Errorf("no variable before setup: uninstall left one: %v", getMap(after, "env"))
	}
	if saved, present := after["modelSettings"]; present {
		t.Errorf("no modelSettings before setup: uninstall left %v", saved)
	}
}

func TestAnOlderClaudeCodeGetsTheVariable(t *testing.T) {
	sandboxFiles(t)
	account := recordAccount(t, object{})
	mustWriteJSON(filepath.Join(account, pluginName, "usage.json"), object{"version": "2.1.240"})

	recordSetup(t, account, "xhigh", "--permissions", "keep")

	settings := readJSON(filepath.Join(account, "settings.json"))
	if level, _ := recordValue(settings, "env", "CLAUDE_CODE_EFFORT_LEVEL"); level != "xhigh" {
		t.Errorf("Claude Code 2.1.240 saves no level per model, yet the variable is %q", level)
	}
	if saved, present := settings["modelSettings"]; present {
		t.Errorf("Claude Code 2.1.240 reads no modelSettings, yet setup wrote %v", saved)
	}
}

func TestTheDoctorChecksTheLevelWhereSetupSavedIt(t *testing.T) {
	record := object{"claude-opus-5-5": object{"previous": nil, "set": "xhigh"}}
	cfg := object{"models": object{"primary": "opus", "effort": "xhigh"}, "managedModelEffort": record}
	for _, c := range []struct {
		name, settings, state, marker, fix string
		ok                                 bool
	}{
		{"saved", `{"modelSettings": {"claude-opus-5-5": {"effortLevel": "xhigh"}}}`, "", "Effort saved for claude-opus-5-5: xhigh", "", true},
		{"another level", `{"modelSettings": {"claude-opus-5-5": {"effortLevel": "high"}}}`, "", "config.json expects xhigh", "to save xhigh for claude-opus-5-5 in modelSettings", false},
		{"the variable", `{"modelSettings": {"claude-opus-5-5": {"effortLevel": "xhigh"}}, "env": {"CLAUDE_CODE_EFFORT_LEVEL": "max"}}`, "", "CLAUDE_CODE_EFFORT_LEVEL=max overrides", "removes the variable", false},
		{"the switch's variable", `{"modelSettings": {"claude-opus-5-5": {"effortLevel": "xhigh"}}, "env": {"CLAUDE_CODE_EFFORT_LEVEL": "high"}}`, "high", "Effort saved for claude-opus-5-5: xhigh", "", true},
		{"max set by hand", `{"modelSettings": {"claude-opus-5-5": {"effortLevel": "xhigh"}}}`, "", "config.json expects max", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			doctorRemedySandbox(t)
			cliWrite(t, files.settings, []byte(c.settings))
			if c.state != "" {
				mustWriteJSON(files.state, object{"modelSwitched": object{"at": float64(nowSec()), "from": "fable", "to": "opus", "effortSet": c.state}})
			}

			cfg := cfg
			if c.name == "max set by hand" {
				cfg = object{"models": object{"primary": "opus", "effort": "max"}, "managedModelEffort": record}
			}
			lines := doctorRun(t, "claude", cfg)

			if c.ok {
				joined := strings.Join(lines, "\n")
				if !strings.Contains(joined, "OK  "+c.marker) || strings.Contains(joined, "CLAUDE_CODE_EFFORT_LEVEL") {
					t.Errorf("want an OK line %q and nothing about the variable:\n%s", c.marker, joined)
				}
				return
			}
			if _, fix := remedyFor(t, lines, c.marker); fix != "" && !strings.Contains(fix, c.fix) {
				t.Errorf("the remedy for %q is %q, want it to say %q", c.marker, fix, c.fix)
			}
		})
	}
}
