package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// h7SpawnFromASubagent is an Agent or Task call a subagent makes to start a subagent of its own
// (Claude Code allows that up to CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH): its input carries the
// calling subagent's agent_id.
func h7SpawnFromASubagent(cwd, tool string, toolInput object) object {
	return agentHookInput("PreToolUse", "s1", cwd, object{"agent_id": "h7-agent", "agent_type": "general-purpose", "tool_name": tool, "tool_input": toolInput})
}

func TestASubagentStartedByASubagentGetsThePinnedModelAndTheFallback(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, nil, 97)
	for _, tool := range []string{"Agent", "Task"} {
		explicit := hookOutput(t, onPreToolUse, h7SpawnFromASubagent(project, tool, object{"subagent_type": "general-purpose", "model": "fable", "prompt": "rewrite the parser"}), cfg)
		if permissionOf(explicit) != "allow" || spawnedModel(explicit) != "opus" {
			t.Errorf("with the Fable bucket at 97%% a subagent's %s call started a Fable subagent: %v", tool, explicit)
		}
		if updated := getMap(getMap(explicit, "hookSpecificOutput"), "updatedInput"); getString(updated, "prompt") != "rewrite the parser" || getString(updated, "subagent_type") != "general-purpose" {
			t.Errorf("moving the subagent's %s call to the fallback dropped the rest of its input: %v", tool, updated)
		}
		if plan := hookOutput(t, onPreToolUse, h7SpawnFromASubagent(project, tool, object{"subagent_type": "Plan", "prompt": "plan the parser rewrite"}), cfg); spawnedModel(plan) != "opus" {
			t.Errorf("with the Fable bucket at 97%% a subagent's %s call ran a Plan subagent, pinned to Fable, on Fable: %v", tool, plan)
		}
		if explore := hookOutput(t, onPreToolUse, h7SpawnFromASubagent(project, tool, object{"subagent_type": "Explore", "prompt": "map the parser"}), cfg); spawnedModel(explore) != "haiku" {
			t.Errorf("a subagent's %s call for an Explore subagent did not get the router's pin: %v", tool, explore)
		}
	}
	if output := hookOutput(t, onPreToolUse, h7SpawnFromASubagent(project, "Agent", object{"subagent_type": "general-purpose", "model": "sonnet", "prompt": "x"}), cfg); output != nil {
		t.Errorf("a subagent's call for a model outside the Fable quota was touched: %v", output)
	}
	if journal, _ := os.ReadFile(files.decisions); !strings.Contains(string(journal), `"action":"subagent-fallback"`) || !strings.Contains(string(journal), "Plan: fable → opus") {
		t.Errorf("the move of a subagent's subagent to the fallback was not journaled: %s", journal)
	}
	writeFableBucket(50, float64(nowSec()), float64(nowSec()+2*86400))
	if plan := hookOutput(t, onPreToolUse, h7SpawnFromASubagent(project, "Agent", object{"subagent_type": "Plan", "prompt": "plan it"}), cfg); spawnedModel(plan) != "fable" {
		t.Errorf("with the Fable bucket at 50%% a subagent's Plan subagent did not run on its Fable pin: %v", plan)
	}
}

func TestASubagentsSpawnIsDeniedPastThePausePointAndLeftAloneWhileTheGuardIsPaused(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, nil, 97)
	plan := h7SpawnFromASubagent(project, "Agent", object{"subagent_type": "Plan", "prompt": "plan the parser rewrite"})
	writeUsage(93, 10, 0)
	if output := hookOutput(t, onPreToolUse, plan, cfg); permissionOf(output) != "deny" || spawnedModel(output) != "" {
		t.Fatalf("past the 5h pause point a subagent's Agent call was not denied: %v", output)
	}
	writeUsage(20, 10, 0)
	now := nowSec()
	updateState(func(state object) { state["disabledUntil"] = float64(now + 3600) })
	if output := hookOutput(t, onPreToolUse, plan, cfg); output != nil {
		t.Fatalf("while the guard is paused a subagent's Agent call was changed: %v", output)
	}
}

func TestObserveModeOnlyJournalsTheModelOfASubagentsSubagent(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, nil, 97)
	defer func(previous bool) { observing = previous }(observing)
	observing = true
	if output := hookOutput(t, onPreToolUse, h7SpawnFromASubagent(project, "Agent", object{"subagent_type": "Plan", "prompt": "plan the parser rewrite"}), cfg); output != nil {
		t.Fatalf("observe mode changed the model of a subagent's Plan subagent: %v", output)
	}
	if !slices.Contains(journaledFor("s1"), "would-subagent-fallback") {
		t.Fatalf("observe mode journaled nothing for a subagent's Plan subagent: %v", journaledFor("s1"))
	}
}
