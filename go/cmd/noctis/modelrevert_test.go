package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func scopedSwitchConfig() object {
	return object{
		"models":     object{"primary": "fable", "fallback": "opus"},
		"roles":      object{"fallback": object{"model": "opus", "effort": "high"}},
		"fable":      object{"revertOnReset": true},
		"thresholds": object{"weeklyFable": float64(95)},
	}
}

func fableWindowCleared(now int64) usageView {
	return usageView{hasAny: true, fable: &window{used: 1, resetsAt: float64(now + 86400)}}
}

func TestTheRevertTakesBackTheModelAndEffortTheSwitchAdded(t *testing.T) {
	cfg := scopedSwitchConfig()
	for name, before := range map[string]object{
		"a model and no effort":  {"model": "fable"},
		"no model and no effort": {"permissions": object{"defaultMode": "acceptEdits"}},
	} {
		t.Run(name, func(t *testing.T) {
			want := T("scoped.reverted", scopedLabel(cfg), "fable")
			if before["model"] == nil {
				want = T("scoped.revertedDefault", scopedLabel(cfg))
			}
			sandboxFiles(t)
			mustWriteJSON(files.settings, cloneObject(before))
			now := nowSec()
			persistModelSwitch(cfg, float64(now+3600), now)
			during := readJSON(files.settings)
			if getString(during, "model") != "opus" || getString(getMap(during, "env"), "CLAUDE_CODE_EFFORT_LEVEL") != "high" {
				t.Fatalf("at the Fable cap settings.json should name the fallback role's model and effort: %v", during)
			}
			note := maybeRevertDefaultModel(cfg, readState(), fableWindowCleared(now), now+20)
			after := readJSON(files.settings)
			if model, had := before["model"]; after["model"] != model || (after["model"] != nil) != had {
				t.Errorf("settings.json had model %v before the switch and %v after the revert", before["model"], after["model"])
			}
			if env, had := after["env"]; had {
				t.Errorf("settings.json had no effort before the switch; after the revert it still has env %v", env)
			}
			if note != want {
				t.Errorf("the revert notice says %q, want %q", note, want)
			}
			if getMap(readState(), "modelSwitched") != nil {
				t.Error("the switch is still recorded after the revert")
			}
		})
	}
}

func TestTheRevertLeavesWhatThePersonPickedDuringTheSwitch(t *testing.T) {
	cfg := scopedSwitchConfig()
	sandboxFiles(t)
	mustWriteJSON(files.settings, object{"model": "fable", "env": object{"CLAUDE_CODE_EFFORT_LEVEL": "max"}})
	now := nowSec()
	persistModelSwitch(cfg, float64(now+3600), now)
	withSettings(func(data object) bool {
		getMap(data, "env")["CLAUDE_CODE_EFFORT_LEVEL"] = "medium"
		return true
	})
	maybeRevertDefaultModel(cfg, readState(), fableWindowCleared(now), now+20)
	after := readJSON(files.settings)
	if got := getString(getMap(after, "env"), "CLAUDE_CODE_EFFORT_LEVEL"); got != "medium" {
		t.Errorf("the effort picked during the switch was replaced by the one from before it: %q", got)
	}
	if got := getString(after, "model"); got != "fable" {
		t.Errorf("the model the switch replaced did not come back: %q", got)
	}
	mustWriteJSON(files.settings, object{"model": "fable", "env": object{"CLAUDE_CODE_EFFORT_LEVEL": "max"}})
	persistModelSwitch(cfg, float64(now+3600), now)
	setSettingsModel("sonnet")
	note := maybeRevertDefaultModel(cfg, readState(), fableWindowCleared(now), now+20)
	if got := settingsModel(); got != "sonnet" || strings.Contains(note, "fable") {
		t.Errorf("a model picked during the switch became %q, and the notice said %q", got, note)
	}
}

func TestASetupDuringAScopedSwitchIsTheNewBaseline(t *testing.T) {
	root := cliPluginTree(t)
	account := t.TempDir()
	now := nowSec()
	previous, previousArgs := files, args
	t.Cleanup(func() { files, args = previous, previousArgs })
	args = parseArgs(nil)
	files = pathsFor(account, root)
	mustWriteJSON(files.settings, object{"model": "opus", "env": object{"CLAUDE_CODE_EFFORT_LEVEL": "high"}})
	updateState(func(state object) {
		state["modelSwitched"] = object{"at": float64(now - 3600), "from": "fable", "to": "opus", "fableResetsAt": float64(now - 60), "effortWas": "max"}
	})
	run := runNoctisCLI(t, cliAccountEnv(root, account), "setup", "--config-dir", account, "--profile", "code", "--no-ask", "--permissions", "keep")
	if run.code != 0 {
		t.Fatalf("setup failed:\n%s", run)
	}
	chosen := readJSON(filepath.Join(account, "settings.json"))
	if getString(chosen, "model") != "opus" || getString(getMap(chosen, "env"), "CLAUDE_CODE_EFFORT_LEVEL") != "xhigh" {
		t.Fatalf("setup --profile code should leave opus at effort xhigh: %v", chosen)
	}
	note := maybeRevertDefaultModel(loadConfig(), readState(), fableWindowCleared(now), now)
	after := readJSON(files.settings)
	if note != "" || getString(after, "model") != "opus" || getString(getMap(after, "env"), "CLAUDE_CODE_EFFORT_LEVEL") != "xhigh" {
		t.Fatalf("the reset of a switch made before setup undid the setup: notice %q, settings %v", note, after)
	}
}

func switchedThenSetUp(t *testing.T, settings object, first []string, during []string) (usageView, int64) {
	t.Helper()
	root := cliPluginTree(t)
	account := t.TempDir()
	now := nowSec()
	previous, previousArgs := files, args
	t.Cleanup(func() { files, args = previous, previousArgs })
	args = parseArgs(nil)
	files = pathsFor(account, root)
	if settings != nil {
		mustWriteJSON(files.settings, settings)
	}
	env := cliAccountEnv(root, account)
	if run := runNoctisCLI(t, env, append([]string{"setup", "--config-dir", account, "--no-ask", "--permissions", "keep"}, first...)...); run.code != 0 {
		t.Fatalf("setup failed:\n%s", run)
	}
	persistModelSwitch(loadConfig(), float64(now+3600), now)
	if model, _, effort, _ := settingsModelAndEffort(); model != "opus" || effort != "high" {
		t.Fatalf("at the Fable cap settings.json should name the fallback role, opus at effort high: %s at %s", model, effort)
	}
	if run := runNoctisCLI(t, env, append([]string{"setup", "--config-dir", account, "--no-ask", "--permissions", "keep"}, during...)...); run.code != 0 {
		t.Fatalf("setup failed:\n%s", run)
	}
	if model, _, _, _ := settingsModelAndEffort(); model != "opus" {
		t.Fatalf("setup during the switch replaced the fallback model while Fable's cap is out: %s", model)
	}
	return fableWindowCleared(now), now
}

func TestASetupThatKeepsTheSwitchedModelLeavesTheResetItsModel(t *testing.T) {
	fable := []string{"--code", "claude-fable-5-1:max", "--fallback", "opus:high"}
	for _, tc := range []struct {
		name          string
		settings      object
		first, during []string
		model, effort string
	}{
		{"a threshold preset", nil, fable, []string{"--preset", "conservative"}, "claude-fable-5-1", "max"},
		{"a plain re-run", nil, fable, nil, "claude-fable-5-1", "max"},
		{"a new code role", nil, fable, []string{"--code", "sonnet:medium"}, "sonnet", "medium"},
		{"a model setup did not manage before", nil, append([]string{"--no-model"}, fable...), nil, "claude-fable-5-1", "max"},
		{"a Fable model of your own", object{"model": "claude-fable-5-1"}, []string{"--code", "sonnet:medium", "--fallback", "opus:high"}, nil, "claude-fable-5-1", "medium"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleared, now := switchedThenSetUp(t, tc.settings, tc.first, tc.during)
			note := maybeRevertDefaultModel(loadConfig(), readState(), cleared, now)
			model, _, effort, _ := settingsModelAndEffort()
			if model != tc.model || effort != tc.effort || note != T("scoped.reverted", scopedLabel(loadConfig()), tc.model) {
				t.Fatalf("setup kept the fallback model the switch wrote, so the reset should put in %s at effort %s: got %s at %s, notice %q", tc.model, tc.effort, model, effort, note)
			}
		})
	}
}
