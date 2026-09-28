package main

import (
	"slices"
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
