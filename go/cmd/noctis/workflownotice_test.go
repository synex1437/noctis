package main

import (
	"strconv"
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

var singleTasksOverAllOfSomething = []string{
	"upgrade all packages",
	"update all tests",
	"refactor all functions in utils.go",
	"document all functions in this file",
	"rename all files in this folder to kebab-case",
	"update the copyright year in all files",
	"tüm testleri güncelle",
	"bütün testleri güncelle",
	"bu dosyadaki tüm fonksiyonları belgele",
}

var fanOutsOverEveryUnitOrACount = []string{
	"migrate every component under src/components to TypeScript",
	"migrate every page from Next.js 13 to 14",
	"port every service to node.js 22",
	"rewrite every page from vue.js to svelte",
	"migrate all components to TypeScript",
	"convert all JavaScript files to TypeScript",
	"tüm bileşen dosyalarını TypeScript'a taşı",
	"tüm servisleri denetle",
	"review these 30 PRs",
	"review the 12 open pull requests",
	"migrate these 15 microservices to Go",
	"port the 20 remaining Python scripts to Go",
	"migrate the 50 cron jobs to systemd timers",
	"rename the logger codebase-wide",
	"repo genelinde logger'ı yeniden adlandır",
	"review each module for missing error handling",
	"review all 40 endpoints for missing auth checks",
	"migrate all 12 packages to ESM",
	"document all 30 functions in utils",
	"rename all 25 files to kebab-case",
	"migrate dozens of components to hooks",
	"rename the logger across the whole codebase",
	"her servisi yeni loglama kütüphanesine taşı",
	"25 dosyayı TypeScript'a taşı",
}

func TestASingleTaskOverAllItsFilesOrTestsBringsNoWorkflowSuggestion(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, object{"workflow": object{"suggest": true}}, 20)
	for _, prompt := range singleTasksOverAllOfSomething {
		if notice := fanOutNotice(hookOutput(t, onUserPromptSubmit, promptInput("s1", project, prompt), cfg)); notice != "" {
			t.Errorf("%q brought a workflow suggestion: %q", prompt, notice)
		}
	}
	trustQueueFile(writeQueueFile(t, project, "# q\n- [ ] update all tests\n- [ ] fix typo\n"), true)
	if output := hookOutput(t, onStop, stopInput("s1", project), cfg); !strings.Contains(getString(output, "reason"), "Queue continues") || fanOutNotice(output) != "" {
		t.Errorf("the queue item %q brought a workflow suggestion: %v", "update all tests", output)
	}
}

func TestANumberThatCountsNoFilesOrModulesBringsNoWorkflowSuggestion(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, object{"workflow": object{"suggest": true}}, 20)
	for _, prompt := range []string{
		"review PR 1234",
		"upgrade to React 19",
		"migrate the database to Postgres 16",
		"review hundreds of lines of logs for the crash",
	} {
		if notice := fanOutNotice(hookOutput(t, onUserPromptSubmit, promptInput("s1", project, prompt), cfg)); notice != "" {
			t.Errorf("%q brought a workflow suggestion: %q", prompt, notice)
		}
	}
}

func TestAPromptAboutOneFileBringsNoWorkflowSuggestion(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, object{"workflow": object{"suggest": true}}, 20)
	for _, prompt := range []string{
		"refactor every function in utils.go",
		"review each function in auth.go",
		"refactor all handlers in server.go",
		"document every function in this file",
		"rename all variables in this file",
		"bu dosyadaki her fonksiyonu belgele",
		"utils.go'daki tüm fonksiyonları TypeScript'e dönüştür",
		"review each function in src/auth/UserService.java",
	} {
		if notice := fanOutNotice(hookOutput(t, onUserPromptSubmit, promptInput("s1", project, prompt), cfg)); notice != "" {
			t.Errorf("%q brought a workflow suggestion: %q", prompt, notice)
		}
	}
}

func TestAQuestionOrAChoreForEveryDayBringsNoWorkflowSuggestion(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, object{"workflow": object{"suggest": true}}, 20)
	for _, prompt := range []string{
		"what is our codebase-wide logging convention",
		"son 10 test neden kırmızı",
		"bu 12 dosyada ne değişti",
		"proje genelinde kullanılan lint kuralları neler",
		"her gün testleri güncelle",
		"her ay yedek dosyalarını taşı",
	} {
		if notice := fanOutNotice(hookOutput(t, onUserPromptSubmit, promptInput("s1", project, prompt), cfg)); notice != "" {
			t.Errorf("%q brought a workflow suggestion: %q", prompt, notice)
		}
	}
}

func TestEveryEachAllACountOrTheWholeCodebaseStillBringsAWorkflowSuggestion(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, object{"workflow": object{"suggest": true}}, 20)
	for _, prompt := range fanOutsOverEveryUnitOrACount {
		if notice := fanOutNotice(hookOutput(t, onUserPromptSubmit, promptInput("s1", project, prompt), cfg)); !strings.Contains(notice, "looks like a fan-out task") {
			t.Errorf("%q no longer brings a workflow suggestion: %q", prompt, notice)
		}
	}
}

func TestAPromptThatAsksForAWorkflowItselfBringsNoWorkflowSuggestion(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, object{"workflow": object{"suggest": true}}, 20)
	for _, prompt := range []string{
		"iş akışı ile tüm servisleri yeni API'ye taşı",
		"İş akışı kullanarak tüm servisleri yeni API'ye taşı",
		"tüm servisleri yeni API'ye taşı, bir iş akışı kur",
		"tüm servisleri iş akışıyla yeni API'ye taşı",
		"/deep-research review every file in the repo",
		"use a workflow to migrate every endpoint",
		"ultracode: migrate every endpoint",
	} {
		if notice := fanOutNotice(hookOutput(t, onUserPromptSubmit, promptInput("s1", project, prompt), cfg)); notice != "" {
			t.Errorf("%q, which asks for a workflow itself, brought a workflow suggestion: %q", prompt, notice)
		}
	}
}

func TestTheQueueWeighsTheWholeOfALongItemForAWorkflowSuggestion(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, object{"workflow": object{"suggest": true}}, 20)
	fanOut := "after the release branch is cut and the changelog, the version bump and the docs site rebuild are merged and the staging smoke run is green, migrate every component under src/components to TypeScript"
	oneFile := "migrate every function in the payments helper to the new error type, keeping the wrapped causes and the retry hints that the checkout flow reads as they are now, all of it inside src/payments/helpers.go"
	for _, item := range []string{fanOut, oneFile} {
		trustQueueFile(writeQueueFile(t, project, "# q\n- [ ] "+item+"\n- [ ] fix typo\n"), true)
		if notice := fanOutNotice(hookOutput(t, onStop, stopInput("s1", project), cfg)); (notice != "") != (item == fanOut) {
			t.Errorf("for the queue item of %d characters %q the workflow suggestion is %q", len([]rune(item)), item, notice)
		}
	}
}

func TestATurnClaudeCodeWritesItselfIsNeitherRoutedNorGivenAWorkflowSuggestion(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, object{"workflow": object{"suggest": true}, "router": object{"enabled": true}}, 20)
	research := "research the best mechanical keyboards of 2026 on https://example.com/reviews"
	fanOut := "migrate every component under src/components to TypeScript"
	routed := func(sid string) bool {
		return getMap(getMap(readState(), "routes"), sid) != nil || journaledAction(sid, "route")
	}
	hookOutput(t, onUserPromptSubmit, promptInput("typed-research", project, research), cfg)
	if !routed("typed-research") {
		t.Fatalf("the research prompt %q typed by the user was not routed", research)
	}
	if notice := fanOutNotice(hookOutput(t, onUserPromptSubmit, promptInput("typed-fan-out", project, fanOut), cfg)); notice == "" {
		t.Fatalf("the fan-out prompt %q typed by the user brought no workflow suggestion", fanOut)
	}
	for index, prompt := range []string{
		"[Subagent hand-back] I looked into it: " + research,
		"Another Claude session sent a message: " + research,
		"[SYSTEM NOTIFICATION] " + research,
		"[Subagent hand-back] Next, " + fanOut,
		"Another Claude session sent a message: " + fanOut,
	} {
		sid := "agent-turn-" + strconv.Itoa(index)
		output := hookOutput(t, onUserPromptSubmit, promptInput(sid, project, prompt), cfg)
		if routed(sid) {
			t.Errorf("the turn Claude Code wrote %q was routed: %v", prompt, output)
		}
		if notice := fanOutNotice(output); notice != "" {
			t.Errorf("the turn Claude Code wrote %q brought a workflow suggestion: %q", prompt, notice)
		}
	}
}
