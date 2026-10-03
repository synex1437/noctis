package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func speakTurkish(t *testing.T) {
	t.Helper()
	previous := locale
	t.Cleanup(func() { locale = previous })
	locale = "tr"
	if windowLabel("five_hour") == "5h" || windowLabel("seven_day") == "weekly" {
		t.Fatal("setup: the Turkish window labels are the English ones, so this test cannot tell them apart")
	}
}

func TestAPromptPastThePausePointTellsClaudeTheWindowInEnglish(t *testing.T) {
	cfg, project := controlSandbox(t, 93, 40)
	speakTurkish(t)
	typed := hookOutput(t, onUserPromptSubmit, promptInput("cw-typed", project, "fix the parser"), cfg)
	if context := contextOf(typed); !strings.Contains(context, "[noctis] 5h usage 93%") || strings.Contains(context, windowLabel("five_hour")) {
		t.Errorf("Claude was told about the window of a Turkish user's prompt in Turkish: %q", context)
	}
	if notice := getString(typed, "systemMessage"); !strings.Contains(notice, windowLabel("five_hour")) {
		t.Errorf("the user's own notice lost its Turkish window label: %q", notice)
	}
	write := subagentTool("cw-sub", project, "Write", object{"file_path": filepath.Join(project, "src", "x.go"), "content": "x"})
	if reason := reasonOf(hookOutput(t, onPreToolUse, write, cfg)); !strings.Contains(reason, "[noctis] 5h usage is 93%") || strings.Contains(reason, windowLabel("five_hour")) {
		t.Errorf("a subagent was denied in Turkish window terms: %q", reason)
	}
}

func TestTheWarnBandTellsClaudeTheWindowInEnglish(t *testing.T) {
	cfg, project := controlSandbox(t, 40, 93)
	speakTurkish(t)
	output := hookOutput(t, onUserPromptSubmit, promptInput("cw-warn", project, "fix the parser"), cfg)
	if context := contextOf(output); !strings.Contains(context, "[noctis] weekly usage 93% (auto-pause at 95%)") || strings.Contains(context, windowLabel("seven_day")) {
		t.Fatalf("the warn band reached Claude in Turkish window terms: %q", context)
	}
}

func TestTheWorkflowGateAndASubagentStopTellClaudeTheWindowInEnglish(t *testing.T) {
	cfg := creditsConfig()
	cfg["workflow"] = object{"gate": true}
	speakTurkish(t)
	reset := float64(nowSec() + 3600)
	five := &waitPlan{window: "five_hour", label: windowLabel("five_hour"), used: 93, threshold: 92, until: reset, hit: "threshold"}
	ceiling := &waitPlan{window: "seven_day", label: windowLabel("seven_day"), used: 99, threshold: 100, until: reset, hit: "ceiling"}
	scoped := &waitPlan{window: "fable", label: "Fable", used: 97, threshold: 95, until: reset, hit: "threshold"}
	for _, check := range []struct{ what, text, want string }{
		{"the gate at a pause point", gateWorkflowLaunch(cfg, decision{wait: five}, nil), "(5h 93%)"},
		{"the gate past Fable's pause point", gateWorkflowLaunch(cfg, decision{wait: scoped}, nil), "(Fable 97%)"},
		{"the gate in the warn band", gateWorkflowLaunch(cfg, decision{warnWindow: &warnPlan{window: "seven_day", label: windowLabel("seven_day"), used: 86, threshold: 89, resetsAt: reset}}, nil), "weekly usage is 86%"},
		{"the gate short of room", gateWorkflowLaunch(cfg, decision{usage: usageFrom(80, 30, reset)}, nil), "only 12 points of the 5h window"},
		{"a subagent at a pause point", subagentLimitReason(five, nil), "5h usage is 93%"},
		{"a subagent at the ceiling", subagentLimitReason(five, ceiling), "weekly usage is 99%"},
	} {
		if !strings.Contains(check.text, check.want) || strings.Contains(check.text, windowLabel("five_hour")) || strings.Contains(check.text, windowLabel("seven_day")) {
			t.Errorf("%s: Claude was not told %q in English: %q", check.what, check.want, check.text)
		}
	}
}
