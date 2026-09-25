package main

import (
	"strings"
	"testing"
)

func TestTheShippedConfigSuggestsNoWorkflow(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, nil, 20)
	output := hookOutput(t, onUserPromptSubmit, fanOutPrompt("s1", project), cfg)
	if text := contextOf(output) + getString(output, "systemMessage"); strings.Contains(text, "fan-out task") || strings.Contains(text, "ultracode") {
		t.Fatalf("with the shipped config a fan-out prompt still brought a workflow suggestion: %v", output)
	}
}

func TestAWorkflowSuggestionTellsThePersonAndGivesClaudeNoInstruction(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, object{"workflow": object{"suggest": true}}, 20)
	output := hookOutput(t, onUserPromptSubmit, fanOutPrompt("s1", project), cfg)
	if context := contextOf(output); strings.Contains(context, "workflow") || strings.Contains(context, "ultracode") {
		t.Fatalf("the workflow suggestion was handed to Claude as an instruction: %q", context)
	}
	if notice := getString(output, "systemMessage"); !strings.Contains(notice, "looks like a fan-out task") || !strings.Contains(notice, "in your own prompt with ultracode") {
		t.Fatalf("the person was not told that the request can run as a workflow if they ask for one: %v", output)
	}
}

func TestTheQueueNeverTellsClaudeToStartAWorkflow(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, object{"workflow": object{"suggest": true}}, 20)
	trustQueueFile(writeQueueFile(t, project, "# q\n- [ ] migrate every component under src/components to TypeScript\n- [ ] fix typo\n"), true)
	output := hookOutput(t, onStop, stopInput("s1", project), cfg)
	if reason := getString(output, "reason"); !strings.Contains(reason, "Queue continues") || strings.Contains(reason, "workflow") || strings.Contains(reason, "ultracode") {
		t.Fatalf("the queue told Claude to start a workflow for its next item: %q", reason)
	}
	if notice := getString(output, "systemMessage"); !strings.Contains(notice, "The next item looks like a fan-out task") {
		t.Fatalf("the person was not told that the next item can run as a workflow: %v", output)
	}
}

func TestRunningAllTestsIsNotTakenForAFanOut(t *testing.T) {
	for _, prompt := range []string{
		"run all tests and fix failures",
		"Review the code and run all tests",
		"fix all failing tests",
		"check that all files are formatted",
		"tüm testleri çalıştır ve hataları düzelt",
	} {
		if looksLikeFanOut(prompt) {
			t.Errorf("%q was taken for a fan-out task", prompt)
		}
	}
	for _, prompt := range []string{
		"migrate every endpoint",
		"Migrate every route handler under src/routes to the new router",
		"Audit every route handler under src/routes for missing auth checks and fix what you find",
		"migrate every component under src/components to TypeScript",
		"review all 40 endpoints for missing auth checks",
		"her endpoint'i taşı",
		"tüm bileşen dosyalarını TypeScript'a taşı ve testleri düzelt",
	} {
		if !looksLikeFanOut(prompt) {
			t.Errorf("%q is no longer taken for a fan-out task", prompt)
		}
	}
}
