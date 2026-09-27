package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestTheStepsOverTheChecklistLimitAreNamedAndKeptInTheFile(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	steps := []string{}
	for index := 1; index <= autoQueueMaxItems+5; index++ {
		steps = append(steps, fmt.Sprintf("add unit test number %d for the invoice parser module", index))
	}
	prompt := "Please do all of these in order for the release:\n1. " + strings.Join(steps, "\n1. ") + "\n"
	output := startQueue(t, cfg, "over-limit", project, prompt)
	record := getMap(getMap(readState(), "autoQueues"), "over-limit")
	if numberOr(record, "items", 0) != autoQueueMaxItems {
		t.Fatalf("the checklist does not hold the first %d steps: %v", autoQueueMaxItems, record)
	}
	if message := getString(output, "systemMessage"); !strings.Contains(message, "the other 5") || !strings.Contains(message, fmt.Sprint(len(steps))) {
		t.Errorf("the user is not told that 5 of the %d steps were left out: %q", len(steps), message)
	}
	if context := contextOf(output); !strings.Contains(context, "the other 5") {
		t.Errorf("Claude is not told that 5 steps were left out: %q", context)
	}
	content, err := os.ReadFile(getString(record, "path"))
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps[autoQueueMaxItems:] {
		if !strings.Contains(string(content), "\n- "+step+"\n") {
			t.Errorf("the checklist file does not keep %q in sight:\n%s", step, content)
		}
	}
	if view := queueSnapshotOf(getString(record, "path"), string(content)); view.total != autoQueueMaxItems {
		t.Errorf("the steps left out drive the session: %d open, want %d", view.total, autoQueueMaxItems)
	}
	if reason := journaledReason("over-limit", "auto-queue"); !strings.Contains(reason, "5") {
		t.Errorf("noctis why does not say that 5 steps were left out: %q", reason)
	}
}

func TestAFinishedChecklistNamesTheStepsOverTheLimitAgain(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	steps := []string{}
	for index := 1; index <= autoQueueMaxItems+2; index++ {
		steps = append(steps, fmt.Sprintf("write the migration script number %d for the orders table", index))
	}
	startQueue(t, cfg, "over-limit-done", project, "Please do all of these in order for the release:\n- "+strings.Join(steps, "\n- ")+"\n")
	path := getString(getMap(getMap(readState(), "autoQueues"), "over-limit-done"), "path")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(content), "- [ ] ", "- [x] ")), 0o600); err != nil {
		t.Fatal(err)
	}
	message := getString(stopHookOutput(t, stopInput("over-limit-done", project), cfg), "systemMessage")
	for _, step := range steps[autoQueueMaxItems:] {
		if !strings.Contains(message, step) {
			t.Errorf("the finished checklist does not name %q, which is still open: %q", step, message)
		}
	}
	if getMap(getMap(readState(), "autoQueues"), "over-limit-done") != nil {
		t.Error("the finished checklist is still driven")
	}
}
