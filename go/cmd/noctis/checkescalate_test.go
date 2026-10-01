package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const checkLastRuleWords = "If the command fails again, the queue is held until it passes."

func failingCheckSandbox(t *testing.T, sid, model string) (object, string, string, string) {
	t.Helper()
	command := shellFor("echo run >> runs.txt; cat fixed.txt", "echo run>> runs.txt & type fixed.txt")
	cfg, project, frontend := queueCheckSandbox(t, command)
	runsOn(sid, model, 20000)
	tickFirstQueueItem(t, project)
	return cfg, project, frontend, command
}

func TestAFailingCheckGoesOnceToAStrongerModelBeforeTheQueueIsHeld(t *testing.T) {
	cfg, project, frontend, command := failingCheckSandbox(t, "ce1", "claude-sonnet-5-5")
	first := stopHookOutput(t, stopInput("ce1", frontend), cfg)
	if reason := getString(first, "reason"); !strings.Contains(reason, "Queue check failed:") || strings.Contains(reason, checkLastRuleWords) {
		t.Fatalf("the first failure, with a stronger model still to come, warned that the next one holds the queue: %v", first)
	}
	second := stopHookOutput(t, stopAgain("ce1", frontend), cfg)
	reason := getString(second, "reason")
	if getString(second, "decision") != "block" || !strings.Contains(reason, "Queue check failed again: `"+command+"`") || !strings.Contains(reason, `Hand the fix once to the noctis:worker subagent (subagent_type "noctis:worker", model "opus") with a brief that stands on its own`) {
		t.Fatalf("the second failure did not hand the fix to Opus: %v", second)
	}
	if !strings.Contains(reason, checkLastRuleWords) || !strings.Contains(reason, "its output verbatim") {
		t.Fatalf("the hand-off does not ask for a brief with the output or say what the next failure does:\n%s", reason)
	}
	if want := T("queue.escalateCheck", command, 2, "Opus"); getString(second, "systemMessage") != want {
		t.Fatalf("the user was told %q, want %q", getString(second, "systemMessage"), want)
	}
	if queueHeld(cfg, readState(), filepath.Join(project, "TASKS.md")) {
		t.Fatal("the queue is held although its check went to a stronger model")
	}
	if entry := journaledEntry("ce1", "verify-queue"); !getBool(entry, "escalated", false) || getString(entry, "escalateTo") != "opus" {
		t.Fatalf("the hand-off is journaled as %v", entry)
	}
	if count := escalationsToday(readState(), nowSec()); count != 1 {
		t.Fatalf("the day's count of escalations is %v, want 1", count)
	}
	third := stopHookOutput(t, stopAgain("ce1", frontend), cfg)
	if getString(third, "decision") == "block" || getString(third, "systemMessage") != T("queue.heldMessage", command, 3, "TASKS.md") {
		t.Fatalf("the failure after the stronger model's turn did not hold the queue: %v", third)
	}
	if runs := queueCheckRuns(project); runs != 3 {
		t.Fatalf("the check ran %d times over three stops, want 3", runs)
	}
}

func TestACheckTheStrongerModelFixesStartsTheCountAgain(t *testing.T) {
	cfg, project, frontend, _ := failingCheckSandbox(t, "ce2", "claude-sonnet-5-5")
	stopHookOutput(t, stopInput("ce2", frontend), cfg)
	if second := stopHookOutput(t, stopAgain("ce2", frontend), cfg); !strings.Contains(getString(second, "reason"), "Hand the fix once to") {
		t.Fatalf("the second failure did not go to a stronger model: %v", second)
	}
	if err := os.WriteFile(filepath.Join(project, "fixed.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	continues(t, stopHookOutput(t, stopAgain("ce2", frontend), cfg), 1)
	if record := queueCheckRecord(readState(), filepath.Join(project, "TASKS.md")); numberOr(record, "failures", 0) != 0 || numberOr(record, "escalated", 0) != 0 {
		t.Fatalf("the pass after the stronger model's fix left %v", record)
	}
}

func TestAFailingCheckIsHeldAsBeforeWhenNoStrongerModelTakesIt(t *testing.T) {
	for _, setting := range []struct {
		name  string
		model string
		apply func(t *testing.T, cfg object)
	}{
		{"queue.escalate off", "claude-sonnet-5-5", func(t *testing.T, cfg object) { section(cfg, "queue")["escalate"] = "off" }},
		{"the day had enough", "claude-sonnet-5-5", func(t *testing.T, cfg object) {
			updateState(func(state object) { state["escalateDay"] = object{"day": localDay(nowSec()), "count": float64(5)} })
		}},
		{"a session on Opus at max", "claude-opus-5-5", func(t *testing.T, cfg object) { t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "max") }},
		{"no room under the stop block cap", "claude-sonnet-5-5", func(t *testing.T, cfg object) { t.Setenv("CLAUDE_CODE_STOP_HOOK_BLOCK_CAP", "2") }},
	} {
		t.Run(setting.name, func(t *testing.T) {
			cfg, _, frontend, command := failingCheckSandbox(t, "ce3", setting.model)
			setting.apply(t, cfg)
			if first := stopHookOutput(t, stopInput("ce3", frontend), cfg); !strings.Contains(getString(first, "reason"), checkLastRuleWords) {
				t.Fatalf("the send-back before the hold does not say that the next failure holds the queue: %v", first)
			}
			second := stopHookOutput(t, stopAgain("ce3", frontend), cfg)
			if getString(second, "decision") == "block" || getString(second, "systemMessage") != T("queue.heldMessage", command, 2, "TASKS.md") {
				t.Fatalf("the second failure did not hold the queue as before: %v", second)
			}
		})
	}
}
