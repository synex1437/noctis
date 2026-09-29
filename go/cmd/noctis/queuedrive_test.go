package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// headlessProject is a project with a trusted TASKS.md, seen from a claude -p run a script started
// in it.
func headlessProject(t *testing.T) (object, string) {
	t.Helper()
	cfg, project, _ := projectQueueSandbox(t)
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "sdk-cli")
	t.Setenv(handoffEnv, "")
	t.Setenv(queueEnv, "")
	return cfg, project
}

func withQueueHeadless(t *testing.T, cfg object) object {
	t.Helper()
	sectionOf := getMap(cfg, "queue")
	copied := object{}
	for key, value := range sectionOf {
		copied[key] = value
	}
	copied["headless"] = true
	next := object{}
	for key, value := range cfg {
		next[key] = value
	}
	next["queue"] = copied
	return next
}

func TestAHeadlessRunIsNotDrivenByTheProjectsQueue(t *testing.T) {
	cfg, project := headlessProject(t)
	if output := stopHookOutput(t, stopInput("hl1", project), cfg); output != nil {
		t.Fatalf("a claude -p run a script started in the project was held at its stop to work through TASKS.md: %v", output)
	}
	if guard := getMap(getMap(readState(), "stopGuard"), "hl1"); guard != nil {
		t.Fatalf("the headless run started a continuation count: %v", guard)
	}
}

func TestAHeadlessRunGetsNoQueueDirective(t *testing.T) {
	cfg, project := headlessProject(t)
	output := hookOutput(t, onSessionStart, object{"hook_event_name": "SessionStart", "source": "startup", "session_id": "hl2", "cwd": project}, cfg)
	if context := getString(getMap(output, "hookSpecificOutput"), "additionalContext"); strings.Contains(context, "TASKS.md") {
		t.Fatalf("a claude -p run was told to work through the project's TASKS.md: %q", context)
	}
}

func TestAHeadlessRunMakesNoChecklistOfItsPrompt(t *testing.T) {
	cfg, _ := headlessProject(t)
	elsewhere := t.TempDir()
	hookOutput(t, onUserPromptSubmit, promptInput("hl3", elsewhere, listPromptForAutoQueue()), cfg)
	if record := getMap(getMap(readState(), "autoQueues"), "hl3"); record != nil {
		t.Fatalf("the list in a script's claude -p prompt became a checklist that keeps the run going: %v", record)
	}
}

func TestQueueHeadlessLetsTheQueueDriveAHeadlessRun(t *testing.T) {
	cfg, project := headlessProject(t)
	cfg = withQueueHeadless(t, cfg)
	output := stopHookOutput(t, stopInput("hl4", project), cfg)
	if getString(output, "decision") != "block" || !strings.Contains(getString(output, "reason"), "migrate the users table") {
		t.Fatalf("with queue.headless on, the headless run was not driven by TASKS.md: %v", output)
	}
}

func TestNoctisQueueOnLetsTheQueueDriveAHeadlessRun(t *testing.T) {
	cfg, project := headlessProject(t)
	t.Setenv(queueEnv, "on")
	if output := stopHookOutput(t, stopInput("hl5", project), cfg); getString(output, "decision") != "block" {
		t.Fatalf("with %s=on the headless run was not driven by TASKS.md: %v", queueEnv, output)
	}
}

func TestNoctisQueueOffKeepsTheQueueOutOfAnInteractiveSession(t *testing.T) {
	cfg, project := headlessProject(t)
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	if output := stopHookOutput(t, stopInput("hl6", project), cfg); getString(output, "decision") != "block" {
		t.Fatalf("an interactive session was not driven by the trusted TASKS.md: %v", output)
	}
	t.Setenv(queueEnv, "off")
	if output := stopHookOutput(t, stopInput("hl7", project), cfg); output != nil {
		t.Fatalf("with %s=off the session was still held at its stop: %v", queueEnv, output)
	}
	output := startQueue(t, cfg, "hl8", project, "/noctis:start TASKS.md")
	if getString(output, "decision") != "block" || !strings.Contains(getString(output, "reason"), queueEnv+"=off") {
		t.Fatalf("with %s=off, /noctis:start did not say why it starts nothing: %v", queueEnv, output)
	}
}

func TestACloudSessionIsNotAHeadlessRun(t *testing.T) {
	cfg, project := headlessProject(t)
	t.Setenv("CLAUDE_CODE_REMOTE", "true")
	if output := stopHookOutput(t, stopInput("hl9", project), cfg); getString(output, "decision") != "block" {
		t.Fatalf("a cloud session with the sdk entrypoint was treated as a script's run and not driven: %v", output)
	}
}

func TestNoctissOwnRelaunchOfASessionIsStillDriven(t *testing.T) {
	cfg, project := headlessProject(t)
	t.Setenv(handoffEnv, "hl10")
	if output := stopHookOutput(t, stopInput("hl10", project), cfg); getString(output, "decision") != "block" {
		t.Fatalf("noctis's headless relaunch of a session was not driven by its queue: %v", output)
	}
	updateState(func(state object) {
		stateMap(state, "freshStarts")["hl10-fresh"] = object{"from": "hl10", "at": float64(nowSec())}
	})
	if output := stopHookOutput(t, stopInput("hl10-fresh", project), cfg); getString(output, "decision") != "block" {
		t.Fatalf("the fresh session noctis started in place of a paused one was not driven by the queue: %v", output)
	}
	if output := stopHookOutput(t, stopInput("hl10-child", project), cfg); output != nil {
		t.Fatalf("a claude -p the relaunched session started (it inherits %s) was driven by the queue: %v", handoffEnv, output)
	}
}

func TestAQueueStartedInAHeadlessRunDrivesIt(t *testing.T) {
	cfg, project := headlessProject(t)
	jobs := filepath.Join(project, "jobs.md")
	if err := os.WriteFile(jobs, []byte("- [ ] write the changelog\n- [ ] tag the release\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	startedChecklist(t, cfg, "hl11", project, "jobs.md")
	output := stopHookOutput(t, stopInput("hl11", project), cfg)
	if getString(output, "decision") != "block" || !strings.Contains(getString(output, "reason"), "write the changelog") {
		t.Fatalf("a queue the run started itself with /noctis:start did not drive it: %v", output)
	}
}

func TestAPausedHeadlessRunIsRelaunchedOutOfQueues(t *testing.T) {
	_, project := headlessProject(t)
	cfg, calls := relaunchRecorder(t)
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "sdk-cli")
	t.Setenv(queueEnv, "")
	bin := t.TempDir()
	queueEnvLog := filepath.Join(bin, "queue-env.log")
	t.Setenv("NOCTIS_TEST_QUEUE_ENV", queueEnvLog)
	if isWindows {
		writeScript(t, filepath.Join(bin, "claude.cmd"), "@>>\"%NOCTIS_TEST_CALLS%\" cd\r\n@>>\"%NOCTIS_TEST_CALLS%\" echo %*\r\n@>>\"%NOCTIS_TEST_QUEUE_ENV%\" echo queue=%NOCTIS_QUEUE%\r\n@exit /b 0\r\n")
	} else {
		writeScript(t, filepath.Join(bin, "claude"), "#!/bin/sh\npwd -P >> \"$NOCTIS_TEST_CALLS\"\nprintf '%s\\n' \"$*\" >> \"$NOCTIS_TEST_CALLS\"\nprintf 'queue=%s\\n' \"$NOCTIS_QUEUE\" >> \"$NOCTIS_TEST_QUEUE_ENV\"\n")
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, arguments := parkAndRelaunch(t, calls, "hl12", "a pause of a headless run", func(sid string) {
		enforceWait("batch", object{"session_id": sid, "cwd": project}, cfg, decision{wait: fiveHourPlan(float64(nowSec() + 8*3600)), model: "claude-opus-5"})
	})
	if strings.Contains(arguments, "Task list") || strings.Contains(arguments, "migrate the users table") {
		t.Errorf("the relaunch of a script's claude -p run was told to work through the project's queue: %s", arguments)
	}
	if logged, _ := os.ReadFile(queueEnvLog); !strings.Contains(string(logged), "queue=off") {
		t.Errorf("the relaunch of a headless run did not carry %s=off, so it would be driven by the queue once it runs in a terminal: %q", queueEnv, logged)
	}
}

func TestAPausedInteractiveSessionIsRelaunchedWithItsQueue(t *testing.T) {
	sid := "hl13"
	cfg, _ := headlessProject(t)
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	record := object{}
	prepareWait(sid, record, cfg)
	if getBool(record, "queueOff", false) {
		t.Fatalf("the pause of an interactive session was marked as one no queue drives: %v", record)
	}
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "sdk-cli")
	record = object{}
	prepareWait(sid, record, cfg)
	if !getBool(record, "queueOff", false) {
		t.Fatalf("the pause of a headless run was not marked as one no queue drives: %v", record)
	}
}
