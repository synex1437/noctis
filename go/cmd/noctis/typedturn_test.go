package main

import (
	"os"
	"strings"
	"testing"
)

func TestAPromptYouTypePastThePausePointGoesAheadWithAWarning(t *testing.T) {
	cfg, project := controlSandbox(t, 93, 40)
	sid := "tp-ahead"
	output := hookOutput(t, onUserPromptSubmit, promptInput(sid, project, "fix the parser"), cfg)
	if getString(output, "decision") == "block" {
		t.Fatalf("a prompt typed with the 5h window at 93%% (pause point 92%%) was parked: %v", output)
	}
	if wait := pendingWait(sid); wait != nil {
		t.Fatalf("a prompt that went ahead left a wait whose runner would relaunch the session: %v", wait)
	}
	warning := T("wait.typedGoesAhead", windowLabel("five_hour"), formatNumber(93), T("hit.threshold", formatNumber(92)))
	if notice := getString(output, "systemMessage"); !strings.Contains(notice, warning) || !strings.Contains(notice, T("wait.typedLimit")) {
		t.Fatalf("the prompt went ahead without telling the user that it is past the pause point and what still stops it: %q", notice)
	}
	if context := contextOf(output); !strings.Contains(context, "[noctis]") || !strings.Contains(context, "93%") || !strings.Contains(context, "92%") {
		t.Fatalf("Claude was not told that the prompt runs past the pause point: %q", context)
	}
	again := hookOutput(t, onUserPromptSubmit, promptInput(sid, project, "now the lexer"), cfg)
	if getString(again, "decision") == "block" || strings.Contains(getString(again, "systemMessage"), warning) || strings.Contains(contextOf(again), "93%") {
		t.Fatalf("the next prompt in the same window was parked or warned again: %v", again)
	}
	if journal, _ := os.ReadFile(files.decisions); !strings.Contains(string(journal), `"action":"typed-prompt"`) {
		t.Fatalf("the journal does not say that a typed prompt went past the pause point:\n%s", journal)
	}
}

func TestTheTurnAPromptYouTypedStartsRunsPastThePausePointUntilItEnds(t *testing.T) {
	cfg, project := controlSandbox(t, 93, 40)
	sid := "tp-turn"
	hookOutput(t, onUserPromptSubmit, promptInput(sid, project, "fix the parser"), cfg)
	test := mainBatch(sid, project, shellCall("Bash", "go test ./..."))
	if output := hookOutput(t, onPostToolBatch, test, cfg); stoppedBy(output) || pendingWait(sid) != nil {
		t.Fatalf("a tool batch of the typed turn was stopped at the pause point: %v", output)
	}
	spawn := agentHookInput("PreToolUse", sid, project, object{"tool_name": "Agent", "tool_input": object{"subagent_type": "general-purpose", "prompt": "review the parser"}})
	if output := hookOutput(t, onPreToolUse, spawn, cfg); permissionOf(output) == "deny" || pendingWait(sid) != nil {
		t.Fatalf("an Agent call of the typed turn was held at the pause point: %v", output)
	}
	if output := hookOutput(t, onPreToolUse, subagentTool(sid, project, "WebSearch", object{"query": "go parser generators"}), cfg); permissionOf(output) == "deny" {
		t.Fatalf("a subagent of the typed turn was denied new work at the pause point: %v", output)
	}
	if output := hookOutput(t, onPostToolBatch, subagentBatch(sid, project, "a1", "Read"), cfg); stoppedBy(output) || strings.Contains(contextOf(output), "start no new work") {
		t.Fatalf("a subagent of the typed turn was told to wrap up at the pause point: %v", output)
	}
	if journal, _ := os.ReadFile(files.decisions); !strings.Contains(string(journal), `"action":"typed-turn"`) {
		t.Fatalf("the journal does not say that the typed turn went past the pause point:\n%s", journal)
	}

	hookOutput(t, onStop, agentHookInput("Stop", sid, project, object{"stop_hook_active": false}), cfg)
	if output := hookOutput(t, onPostToolBatch, test, cfg); !stoppedBy(output) || pendingWait(sid) == nil {
		t.Fatalf("after the typed turn ended, work that went on by itself was not paused at the pause point: %v", output)
	}
}

func TestAPromptYouTypeTakesOverFromTheWaitThatParkedTheSession(t *testing.T) {
	cfg, project := controlSandbox(t, 93, 40)
	sid := "tp-takeover"
	if output := hookOutput(t, onPostToolBatch, mainBatch(sid, project, shellCall("Bash", "go test ./...")), cfg); !stoppedBy(output) || pendingWait(sid) == nil {
		t.Fatalf("work that went on by itself at 93%% was not paused: %v", output)
	}
	if output := hookOutput(t, onUserPromptSubmit, promptInput(sid, project, "fix the parser"), cfg); getString(output, "decision") == "block" {
		t.Fatalf("a prompt typed into a paused session at 93%% was parked: %v", output)
	}
	if wait := pendingWait(sid); wait != nil {
		t.Fatalf("the wait that paused the session survived the prompt you typed, so its runner would relaunch the session in a second window: %v", wait)
	}
}

func TestAtTheUsageLimitAPromptYouTypeIsStillParked(t *testing.T) {
	cfg, project := controlSandbox(t, 100, 40)
	sid := "tp-ceiling"
	output := hookOutput(t, onUserPromptSubmit, promptInput(sid, project, "fix the parser"), cfg)
	if getString(output, "decision") != "block" || strings.Contains(getString(output, "reason"), "/noctis:pause") {
		t.Fatalf("a prompt typed with the 5h window at 100%% and paid credits refused went ahead, or was told a pause lifts the stop: %v", output)
	}
	if queued := getString(pendingWait(sid), "queuedPrompt"); queued != "fix the parser" {
		t.Fatalf("the prompt held at the usage limit was not kept for the relaunch (queuedPrompt %q)", queued)
	}
	if journal, _ := os.ReadFile(files.decisions); strings.Contains(string(journal), `"action":"typed-prompt"`) {
		t.Fatalf("the journal says a prompt went ahead at the usage limit:\n%s", journal)
	}
}

func TestAPromptNoctisComposedIsStillParkedAtThePausePoint(t *testing.T) {
	cfg, project := controlSandbox(t, 93, 40)
	sid := "tp-composed"
	defer func(previous bool) { promptFromPlugin = previous }(promptFromPlugin)
	promptFromPlugin = true
	composed := getString(section(cfg, "resume"), "prompt")
	output := hookOutput(t, onUserPromptSubmit, promptInput(sid, project, composed), cfg)
	if getString(output, "decision") != "block" || getString(pendingWait(sid), "queuedPrompt") != composed {
		t.Fatalf("a relaunch prompt noctis composed went past the pause point: %v", output)
	}
	if turns := getMap(readState(), "typedTurns"); turns[sid] != nil {
		t.Fatalf("a prompt noctis composed opened a typed turn: %v", turns)
	}
}

func TestClaudeIsNotToldTheUsageIsPastThePausePointWhenThePauseComesEarly(t *testing.T) {
	cfg, _ := controlSandbox(t, 88, 40)
	now := nowSec()
	for hit, cause := range map[string]string{"burst": "a usage burst projects past it", "projection": "the burn rate projects past it", "compaction": "a context compaction could carry the usage past it"} {
		early := &waitPlan{window: "five_hour", label: windowLabel("five_hour"), used: 88, threshold: 92, until: float64(now + 3600), hit: hit}
		_, context := letTypedPromptThrough(cfg, readState(), "tp-early-"+hit, decision{wait: early, usage: currentUsage(now)}, now)
		if strings.Contains(context, "is past its auto-pause point") || !strings.Contains(context, "88% is under its auto-pause point (92%)") || !strings.Contains(context, cause) {
			t.Errorf("a prompt typed at 88%% under a 92%% pause point that a %s pause brought forward told Claude: %q", hit, context)
		}
	}
	plain := &waitPlan{window: "five_hour", label: windowLabel("five_hour"), used: 93, threshold: 92, until: float64(now + 3600), hit: "threshold"}
	if _, context := letTypedPromptThrough(cfg, readState(), "tp-early-plain", decision{wait: plain, usage: currentUsage(now)}, now); !strings.Contains(context, "93% is past its auto-pause point (92%)") {
		t.Errorf("a prompt typed at 93%% over a 92%% pause point no longer tells Claude the usage is past it: %q", context)
	}
}

func TestInObserveModeAPromptYouTypeAtAPausePointIsOnlyJournaled(t *testing.T) {
	cfg, project := controlSandbox(t, 93, 40)
	defer func(previous bool) { observing = previous }(observing)
	observing = true
	sid := "tp-observe"
	output := hookOutput(t, onUserPromptSubmit, promptInput(sid, project, "fix the parser"), cfg)
	if getString(output, "decision") == "block" || getString(output, "systemMessage") != "" || contextOf(output) != "" {
		t.Fatalf("observe mode parked or warned about a prompt typed past the pause point, or told Claude about it: %v", output)
	}
	if output := hookOutput(t, onPostToolBatch, mainBatch(sid, project, shellCall("Bash", "go test ./...")), cfg); output != nil {
		t.Fatalf("observe mode acted on a batch of the typed turn: %v", output)
	}
	journal, _ := os.ReadFile(files.decisions)
	if !strings.Contains(string(journal), `"action":"would-typed-prompt"`) || !strings.Contains(string(journal), `"action":"would-typed-turn"`) || strings.Contains(string(journal), `"action":"typed-prompt"`) || strings.Contains(string(journal), `"action":"would-pause"`) {
		t.Fatalf("observe mode did not journal the typed prompt and its turn as would-…:\n%s", journal)
	}
	observing = false
	warning := T("wait.typedGoesAhead", windowLabel("five_hour"), formatNumber(93), T("hit.threshold", formatNumber(92)))
	if output := hookOutput(t, onUserPromptSubmit, promptInput(sid, project, "now the lexer"), cfg); !strings.Contains(getString(output, "systemMessage"), warning) {
		t.Fatalf("the first prompt enforce mode let past the pause point after observe mode came without its warning, as if observe mode had shown it: %v", output)
	}
}

func TestATypedTurnThatEndsInAnAPIErrorLeavesWhatFollowsToThePausePoint(t *testing.T) {
	cfg, project := controlSandbox(t, 93, 40)
	sid := "tp-failure"
	hookOutput(t, onUserPromptSubmit, promptInput(sid, project, "fix the parser"), cfg)
	hookOutput(t, onStopFailure, agentHookInput("StopFailure", sid, project, object{"error_type": "server_error"}), cfg)
	if turns := getMap(readState(), "typedTurns"); turns[sid] != nil {
		t.Fatalf("the typed turn stayed open after it ended in an API error, so the wake that follows would run past the pause point: %v", turns)
	}
	if output := hookOutput(t, onPreToolUse, subagentTool(sid, project, "WebSearch", object{"query": "go parser generators"}), cfg); permissionOf(output) != "deny" {
		t.Fatalf("after the typed turn ended in an API error, a subagent was let past the pause point: %v", output)
	}
}
