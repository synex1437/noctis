package main

import (
	"path/filepath"
	"slices"
	"testing"
)

// h3SessionRuns records, as the status line would, that sid runs on model.
func h3SessionRuns(sid, project, model string) {
	usage := readJSON(files.usage)
	usage["sessions"] = object{sid: object{"model": model, "cwd": project, "transcript": filepath.Join(project, "transcript.jsonl"), "updatedAt": float64(nowSec()), "context": nil}}
	mustWriteJSON(files.usage, usage)
}

func TestModelNotFoundForAnotherModelLeavesTheSettingsModelAlone(t *testing.T) {
	cases := []struct {
		name     string
		settings string
		running  string
		fields   object
	}{
		{"the error names a typo while settings.json names the fallback alias", "opus", "", claudeFailure("model_not_found", "model: claude-fable-9-typo", "There's an issue with the selected model (claude-fable-9-typo). It may not exist or you may not have access to it.")},
		{"the error names a typo while settings.json names a full model id", "claude-opus-5", "", adapterFailure("model_not_found", "model: claude-fable-9-typo")},
		{"the error names no model and the session runs another one", "opus", "claude-fable-5-1", adapterFailure("model_not_found", "")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, project, _ := limitSandbox(t, nil, 20, 40)
			sid := "h3-other-model"
			if c.running != "" {
				h3SessionRuns(sid, project, c.running)
			}
			mustWriteJSON(files.settings, object{"model": c.settings, "effortLevel": "max"})
			hookOutput(t, onStopFailure, agentHookInput("StopFailure", sid, project, c.fields), cfg)
			if got := settingsModel(); got != c.settings {
				t.Fatalf("a model_not_found for a model settings.json does not name changed its model from %q to %q", c.settings, got)
			}
			if !slices.Contains(journaledFor(sid), "model-unavailable") {
				t.Fatalf("the unavailable model was not journaled: %v", journaledFor(sid))
			}
			if wait := pendingWait(sid); wait != nil {
				t.Fatalf("model_not_found scheduled a retry on the model the plan lacks: %v", wait)
			}
		})
	}
}

func TestModelNotFoundForTheSettingsModelStillStepsItDown(t *testing.T) {
	cases := []struct {
		name     string
		settings string
		running  string
		fields   object
		want     string
	}{
		{"an alias whose model the session runs", "fable", "claude-fable-5-1", adapterFailure("model_not_found", ""), "opus"},
		{"the fallback alias the error names by its full id", "opus", "", claudeFailure("model_not_found", "model: claude-opus-5", ""), ""},
		{"a model named nowhere but in settings.json", "claude-gone-9", "", adapterFailure("model_not_found", ""), "opus"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, project, _ := limitSandbox(t, nil, 20, 40)
			sid := "h3-settings-model"
			if c.running != "" {
				h3SessionRuns(sid, project, c.running)
			}
			mustWriteJSON(files.settings, object{"model": c.settings})
			hookOutput(t, onStopFailure, agentHookInput("StopFailure", sid, project, c.fields), cfg)
			if got := settingsModel(); got != c.want {
				t.Fatalf("model_not_found for the model settings.json names (%q) left it at %q, want %q", c.settings, got, c.want)
			}
		})
	}
}
