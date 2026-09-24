package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func leanSwitchOf(t *testing.T, account string) (string, bool) {
	t.Helper()
	value, present := getMap(readJSON(filepath.Join(account, "settings.json")), "env")["CLAUDE_CODE_ENABLE_FUNCTION_HOOKS"]
	text, _ := value.(string)
	return text, present
}

func leanRecordOf(t *testing.T, account string) object {
	t.Helper()
	return getMap(readJSON(filepath.Join(account, pluginName, "config.json")), "managedFunctionHooks")
}

func TestSetupTurnsOnClaudeCodesFunctionHooksForLeanCompactionAndRecordsIt(t *testing.T) {
	box := newCLIBox(t)

	run := box.run(t, "setup", "--config-dir", box.account, "--profile", "economy", "--permissions", "keep")

	box.configured(t, run, box.account, "economy")
	if value, _ := leanSwitchOf(t, box.account); value != "1" {
		t.Fatalf("setup left env.CLAUDE_CODE_ENABLE_FUNCTION_HOOKS at %q, want 1:\n%s", value, run)
	}
	if record := leanRecordOf(t, box.account); record == nil || getString(record, "set") != "1" || record["previous"] != nil {
		t.Fatalf("setup did not record the switch it wrote for the undo: %v", record)
	}
	if !strings.Contains(run.stdout, "CLAUDE_CODE_ENABLE_FUNCTION_HOOKS") {
		t.Fatalf("setup did not say it turned the function hooks on:\n%s", run)
	}
	if getString(getMap(readJSON(filepath.Join(box.account, "settings.json")), "env"), "CLAUDE_CODE_EFFORT_LEVEL") == "" {
		t.Fatalf("the effort level went missing next to the switch:\n%s", run)
	}

	uninstall := box.run(t, "install", "--uninstall", "--config-dir", box.account, "--host", "claude")

	if uninstall.code != 0 {
		t.Fatalf("uninstall failed:\n%s", uninstall)
	}
	if value, present := leanSwitchOf(t, box.account); present {
		t.Fatalf("uninstall left the switch setup wrote at %q:\n%s", value, uninstall)
	}
}

func TestSetupLeavesAFunctionHooksSwitchItDidNotWrite(t *testing.T) {
	for _, own := range []string{"true", "0"} {
		box := newCLIBox(t)
		settings := readJSON(filepath.Join(box.account, "settings.json"))
		getMap(settings, "env")["CLAUDE_CODE_ENABLE_FUNCTION_HOOKS"] = own
		cliWrite(t, filepath.Join(box.account, "settings.json"), marshalPretty(settings))

		run := box.run(t, "setup", "--config-dir", box.account, "--profile", "economy", "--permissions", "keep")

		box.configured(t, run, box.account, "economy")
		if value, _ := leanSwitchOf(t, box.account); value != own {
			t.Fatalf("setup changed a switch the person set to %q into %q:\n%s", own, value, run)
		}
		if record := leanRecordOf(t, box.account); record != nil {
			t.Fatalf("setup recorded a switch it did not write (%q): %v", own, record)
		}
		uninstall := box.run(t, "install", "--uninstall", "--config-dir", box.account, "--host", "claude")
		if value, _ := leanSwitchOf(t, box.account); uninstall.code != 0 || value != own {
			t.Fatalf("uninstall touched a switch setup never wrote (%q → %q):\n%s", own, value, uninstall)
		}
	}
}

func TestSetupWithNoLeanTurnsLeanCompactionOffAndTakesBackOnlyItsOwnSwitch(t *testing.T) {
	box := newCLIBox(t)
	first := box.run(t, "setup", "--config-dir", box.account, "--profile", "economy", "--permissions", "keep")
	box.configured(t, first, box.account, "economy")

	run := box.run(t, "setup", "--config-dir", box.account, "--profile", "economy", "--permissions", "keep", "--no-lean")

	box.configured(t, run, box.account, "economy")
	if value, present := leanSwitchOf(t, box.account); present {
		t.Fatalf("--no-lean left the switch setup wrote at %q:\n%s", value, run)
	}
	if record := leanRecordOf(t, box.account); record != nil {
		t.Fatalf("--no-lean kept an undo record for a switch that is gone: %v", record)
	}
	if lean := getMap(readJSON(filepath.Join(box.account, pluginName, "config.json")), "compaction")["lean"]; lean != false {
		t.Fatalf("--no-lean left compaction.lean at %v:\n%s", lean, run)
	}

	again := box.run(t, "setup", "--config-dir", box.account, "--profile", "economy", "--permissions", "keep")

	box.configured(t, again, box.account, "economy")
	if value, present := leanSwitchOf(t, box.account); present {
		t.Fatalf("a later setup turned the switch back on (%q) after --no-lean:\n%s", value, again)
	}
}

func TestSetupWithNoLeanLeavesAPersonsOwnSwitch(t *testing.T) {
	box := newCLIBox(t)
	settings := readJSON(filepath.Join(box.account, "settings.json"))
	getMap(settings, "env")["CLAUDE_CODE_ENABLE_FUNCTION_HOOKS"] = "1"
	cliWrite(t, filepath.Join(box.account, "settings.json"), marshalPretty(settings))

	run := box.run(t, "setup", "--config-dir", box.account, "--profile", "economy", "--permissions", "keep", "--no-lean")

	box.configured(t, run, box.account, "economy")
	if value, _ := leanSwitchOf(t, box.account); value != "1" {
		t.Fatalf("--no-lean removed a switch the person set (now %q):\n%s", value, run)
	}
}

func TestDoctorSaysWhenLeanCompactionIsOnButCannotLoad(t *testing.T) {
	leanSandbox(t)
	t.Setenv("NOCTIS_LANG", "en")
	t.Setenv("CLAUDE_CODE_ENABLE_FUNCTION_HOOKS", "")
	mustWriteJSON(files.settings, object{"env": object{}})
	doctor := strings.Join(doctorLines(loadConfig()), "\n")
	if !strings.Contains(doctor, "!!  CLAUDE_CODE_ENABLE_FUNCTION_HOOKS: none") {
		t.Fatalf("the doctor did not flag a missing function hooks switch:\n%s", doctor)
	}
	mustWriteJSON(files.settings, object{"env": object{"CLAUDE_CODE_ENABLE_FUNCTION_HOOKS": "1"}})
	doctor = strings.Join(doctorLines(loadConfig()), "\n")
	if !strings.Contains(doctor, "OK  CLAUDE_CODE_ENABLE_FUNCTION_HOOKS: 1") {
		t.Fatalf("the doctor did not accept the switch:\n%s", doctor)
	}
	mustWriteJSON(files.config, object{"compaction": object{"lean": false}})
	mustWriteJSON(files.settings, object{"env": object{}})
	if doctor = strings.Join(doctorLines(loadConfig()), "\n"); strings.Contains(doctor, "CLAUDE_CODE_ENABLE_FUNCTION_HOOKS") {
		t.Fatalf("the doctor asked for the switch although lean compaction is off:\n%s", doctor)
	}
}

func TestTheDocsSayNoLeanTakesBackOnlyTheSwitchSetupWrote(t *testing.T) {
	for _, doc := range []struct {
		file, from, to string
		says           []string
		never          string
	}{
		{"skills/setup/SKILL.md", "`--no-lean` (", ")", []string{"CLAUDE_CODE_ENABLE_FUNCTION_HOOKS", "setup wrote is taken back", "the user set stays"}, "alone"},
	} {
		raw, err := os.ReadFile(filepath.Join(repoRoot(), doc.file))
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		start := strings.Index(text, doc.from)
		if start < 0 {
			t.Fatalf("%s no longer describes --no-lean at %q", doc.file, doc.from)
		}
		clause := text[start:]
		if end := strings.Index(clause[len(doc.from):], doc.to); end >= 0 {
			clause = clause[:len(doc.from)+end]
		}
		for _, want := range doc.says {
			if !strings.Contains(clause, want) {
				t.Errorf("%s says of --no-lean %q, without %q", doc.file, clause, want)
			}
		}
		if strings.Contains(clause, doc.never) {
			t.Errorf("%s says --no-lean leaves the switch as it is (%q), while setup takes back the one it wrote: %q", doc.file, doc.never, clause)
		}
	}
}

func TestAFunctionHooksSwitchSetByHandAfterAnUninstallIsNotTakenForSetups(t *testing.T) {
	sandboxFiles(t)
	account := recordAccount(t, object{})
	settingsFile := filepath.Join(account, "settings.json")
	recordSetup(t, account, "max", "--permissions", "keep")
	if value, _ := leanSwitchOf(t, account); value != "1" {
		t.Fatalf("setup left the switch at %q, want 1", value)
	}
	recordUninstall(t, account)
	if record := leanRecordOf(t, account); record != nil {
		t.Errorf("uninstall took the switch back but kept the record of it, which describes a setup that is gone: %v", record)
	}
	settings := readJSON(settingsFile)
	settings["env"] = object{"CLAUDE_CODE_ENABLE_FUNCTION_HOOKS": "1"}
	mustWriteJSON(settingsFile, settings)

	recordSetup(t, account, "max", "--permissions", "keep")
	recordUninstall(t, account)

	if value, present := leanSwitchOf(t, account); !present || value != "1" {
		t.Fatalf("a switch set by hand after an uninstall matched the old record of the first setup and was removed by the next uninstall: %q (present %v)", value, present)
	}
}
