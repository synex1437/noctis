package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSetupWithACodeRoleThatHasNoEffortWritesNoEffortLevel(t *testing.T) {
	for _, c := range []struct {
		code, byHand, want string
	}{
		{"claude-haiku-4-5:high", "", "medium"},
		{"sonnet", "", "medium"},
		{"claude-haiku-4-5:high", "high", "high"},
	} {
		t.Run(c.code+" "+c.byHand, func(t *testing.T) {
			box := newCLIBox(t)
			settingsFile := filepath.Join(box.account, "settings.json")
			first := box.run(t, "setup", "--config-dir", box.account, "--code", "opus:max", "--permissions", "keep")
			box.configured(t, first, box.account, "custom")
			if effort, _ := recordValue(readJSON(settingsFile), "env", "CLAUDE_CODE_EFFORT_LEVEL"); effort != "max" {
				t.Fatalf("the first setup did not write the code role's effort: %q\n%s", effort, first)
			}
			if c.byHand != "" {
				settings := readJSON(settingsFile)
				getMap(settings, "env")["CLAUDE_CODE_EFFORT_LEVEL"] = c.byHand
				cliWrite(t, settingsFile, marshalPretty(settings))
			}

			run := box.run(t, "setup", "--config-dir", box.account, "--code", c.code, "--permissions", "keep")

			box.configured(t, run, box.account, "custom")
			config := readJSON(filepath.Join(box.account, pluginName, "config.json"))
			if effort := getString(section(config, "models"), "effort"); effort != "" {
				t.Errorf("--code %s leaves the code role without an effort, yet models.effort stays %q for the relaunches", c.code, effort)
			}
			if effort, _ := recordValue(readJSON(settingsFile), "env", "CLAUDE_CODE_EFFORT_LEVEL"); effort != c.want {
				t.Errorf("--code %s: env.CLAUDE_CODE_EFFORT_LEVEL is %q, want %q (setup takes back only the level it wrote)", c.code, effort, c.want)
			}
			if strings.Contains(run.stdout, "CLAUDE_CODE_EFFORT_LEVEL=max") || strings.Contains(run.stdout, "config.json expects") {
				t.Errorf("--code %s: the output still names an effort level for the code role:\n%s", c.code, run)
			}

			uninstall := box.run(t, "install", "--uninstall", "--config-dir", box.account, "--host", "claude")

			if effort, _ := recordValue(readJSON(settingsFile), "env", "CLAUDE_CODE_EFFORT_LEVEL"); uninstall.code != 0 || effort != c.want {
				t.Errorf("--code %s: after uninstall env.CLAUDE_CODE_EFFORT_LEVEL is %q, want %q:\n%s", c.code, effort, c.want, uninstall)
			}
		})
	}
}

func TestARelaunchWithoutAnEffortLeavesTheEffortOut(t *testing.T) {
	home := relaunchSandbox(t)
	t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "max")
	launch := launchSpec{sid: "s1", cwd: home, model: "sonnet", prompt: "carry on"}

	if args := hostLaunchArgs("claude", object{"resume": object{}}, launch, "", "acceptEdits"); slices.Contains(args, "--effort") {
		t.Errorf("a relaunch without an effort still passes --effort: %q", args)
	}
	if value, found := envEntry(relaunchEnv(launch, ""), "CLAUDE_CODE_EFFORT_LEVEL"); found {
		t.Errorf("a relaunch without an effort still hands the session CLAUDE_CODE_EFFORT_LEVEL=%q", value)
	}
	script, _ := unixLaunchScript(launch, "/opt/claude/bin/claude", []string{"--resume", "s1"}, "")
	content, err := os.ReadFile(script)
	if err != nil || strings.Contains(string(content), "export CLAUDE_CODE_EFFORT_LEVEL") || !strings.Contains(string(content), "unset CLAUDE_CODE_EFFORT_LEVEL") {
		t.Errorf("the window launcher of a relaunch without an effort should unset CLAUDE_CODE_EFFORT_LEVEL, not export one (%v):\n%s", err, content)
	}
}
