package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestObserveModeLeavesTheCheckpointItDoesNotDeliver(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 20, 40)
	sid := "h6-paused"
	checkpoint := buildCheckpoint(agentHookInput("PostToolBatch", sid, project, nil), "paused", "opus", cfg)
	previous := observing
	t.Cleanup(func() { observing = previous })
	observing = true
	for _, source := range []string{"startup", "clear"} {
		watched := "h6-watched-" + source
		if start := hookOutput(t, onSessionStart, agentHookInput("SessionStart", watched, project, object{"source": source}), cfg); start != nil {
			t.Fatalf("observe mode handed a new session (%s) the resume note: %v", source, start)
		}
		if !slices.Contains(journaledFor(watched), "would-inject-context") {
			t.Fatalf("observe mode did not journal the resume note it would hand over at %s: %v", source, journaledFor(watched))
		}
		if record := checkpointRecord(sid); checkpointSpent(sid) || getString(record, "handedTo") != "" {
			t.Fatalf("observe mode used up the checkpoint it did not deliver at %s: %v", source, record)
		}
	}
	observing = false
	if !handedCheckpointTo(t, cfg, project, "h6-next", checkpoint) {
		t.Fatal("once noctis acts again, the next new session in the folder was not handed the checkpoint")
	}
	if record := checkpointRecord(sid); !checkpointSpent(sid) || getString(record, "handedTo") != "h6-next" {
		t.Fatalf("the checkpoint handed to the next new session is not recorded as handed to it: %v", record)
	}
}

func TestObserveModeLeavesTheNoticesItDoesNotShow(t *testing.T) {
	cfg, project, fiveReset := limitSandbox(t, nil, 95, 40)
	trustQueueFile(writeQueueFile(t, project, "# q\n- [ ] migrate the users table\n"), true)
	issues := selfCheckIssues(cfg)
	if len(issues) == 0 {
		t.Fatal("setup: the self-check finds nothing to report, so this test proves nothing")
	}
	previous := observing
	t.Cleanup(func() { observing = previous })
	observing = true
	if start := hookOutput(t, onSessionStart, agentHookInput("SessionStart", "ob-watched", project, object{"source": "startup"}), cfg); start != nil {
		t.Fatalf("observe mode showed the startup notices: %v", start)
	}
	observing = false
	message := getString(hookOutput(t, onSessionStart, agentHookInput("SessionStart", "ob-enforced", project, object{"source": "startup"}), cfg), "systemMessage")
	for _, notice := range []string{
		T("selfcheck.message", pluginName, strings.Join(issues, "; ")),
		T("session.alreadyOver", windowLabel("five_hour"), "95", formatTime(fiveReset)),
		T("queue.modeNotice", "TASKS.md", 1, pluginName),
	} {
		if !strings.Contains(message, notice) {
			t.Errorf("observe mode used up a startup notice it never showed: back in enforce mode, the next session lacks %q in %q", notice, message)
		}
	}
}

func TestObserveModeLeavesTheUpdateNoticesItDoesNotShow(t *testing.T) {
	for _, update := range []string{"published", "downloaded"} {
		t.Run(update, func(t *testing.T) {
			cfg, project, _ := limitSandbox(t, object{"update": object{"check": true}}, 20, 40)
			t.Setenv("NOCTIS_UPDATE_URL", "https://example.invalid/plugin.json")
			release := object{"checkedAt": float64(nowSec())}
			notice := T("update.available", pluginName, "99.0.0", pluginVersion, "/plugin update "+pluginName)
			if update == "published" {
				release["latest"] = "99.0.0"
			} else {
				cache := filepath.Join(t.TempDir(), "plugins", "cache", "market", pluginName)
				files.pluginRoot = filepath.Join(cache, pluginVersion)
				ensureDir(files.pluginRoot)
				ensureDir(filepath.Join(cache, "99.0.0"))
				notice = T("update.downloaded", pluginName, "99.0.0", pluginVersion)
			}
			mustWriteJSON(files.release, release)
			previous := observing
			t.Cleanup(func() { observing = previous })
			observing = true
			if start := hookOutput(t, onSessionStart, agentHookInput("SessionStart", "ob-watched", project, object{"source": "startup"}), cfg); start != nil {
				t.Fatalf("observe mode showed the startup notices: %v", start)
			}
			observing = false
			if message := getString(hookOutput(t, onSessionStart, agentHookInput("SessionStart", "ob-enforced", project, object{"source": "startup"}), cfg), "systemMessage"); !strings.Contains(message, notice) {
				t.Errorf("observe mode used up the notice of the %s version it never showed: back in enforce mode, the next session lacks %q in %q", update, notice, message)
			}
		})
	}
}
