package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeJobFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func openJobs(t *testing.T, checklist string) []string {
	t.Helper()
	content, err := os.ReadFile(checklist)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := parseQueueEntries(string(content))
	open := []string{}
	for _, entry := range entries {
		if !entry.checked {
			open = append(open, entry.text)
		}
	}
	return open
}

func startQueue(t *testing.T, cfg object, sid, project, prompt string) object {
	t.Helper()
	return hookOutput(t, onUserPromptSubmit, promptInput(sid, project, prompt), cfg)
}

func startedChecklist(t *testing.T, cfg object, sid, project, file string) string {
	t.Helper()
	output := startQueue(t, cfg, sid, project, "/noctis:start "+file)
	checklist := sessionQueueFile(cfg, sid, project)
	if checklist == "" {
		t.Fatalf("/noctis:start %s started no queue: %v", file, output)
	}
	return checklist
}

func tickAll(t *testing.T, checklist string) {
	t.Helper()
	content, err := os.ReadFile(checklist)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checklist, []byte(strings.ReplaceAll(string(content), "- [ ]", "- [x]")), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStartingAFileQueuesItsJobsAndSaysHowManyItFound(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	source := writeJobFile(t, project, "deneme.md", "# Sprint\n\nWe ship on Friday.\n\n- add a login page\n- add a logout button\n  with a confirmation dialog\n- write tests for both\n- update the README\n- tag the release\n")
	before, _ := os.ReadFile(source)
	output := startQueue(t, cfg, "sq1", project, "/noctis:start deneme.md")
	if getString(output, "decision") == "block" {
		t.Fatalf("/noctis:start deneme.md was refused: %v", output)
	}
	if message := getString(output, "systemMessage"); message != T("queue.startDetected", 5, "deneme.md", pluginName) {
		t.Errorf("/noctis:start does not say how many jobs it found in the file: %q", message)
	}
	checklist := sessionQueueFile(cfg, "sq1", project)
	if checklist == "" || checklist == source {
		t.Fatalf("no checklist of noctis's own drives the session (queue file %q)", checklist)
	}
	want := []string{"add a login page", "add a logout button with a confirmation dialog", "write tests for both", "update the README", "tag the release"}
	if got := openJobs(t, checklist); !slices.Equal(got, want) {
		t.Errorf("the checklist holds %q, not the jobs as the file words them: %q", got, want)
	}
	context := getString(getMap(output, "hookSpecificOutput"), "additionalContext")
	if !strings.Contains(context, checklist) || !strings.Contains(context, source) {
		t.Errorf("Claude is not told where the checklist and the file it came from are:\n%s", context)
	}
	if after, _ := os.ReadFile(source); string(after) != string(before) {
		t.Errorf("starting the queue changed the user's file:\n%s", after)
	}
}

func TestTheJobsOfAStartedFileAreTheOnesItsAuthorWrote(t *testing.T) {
	cases := []struct {
		name, file, content string
		open                []string
		eligible            string
		blocked             int
	}{
		{"checkboxes, the ticked one kept for the item numbers", "TODO.md", "- [x] set up the repository\n- [ ] add a login page\n- [ ] write tests for it (after 2)\n", []string{"add a login page", "write tests for it (after 2)"}, "add a login page", 1},
		{"a numbered list", "plan.md", "Plan for the week\n\n1. add a login page\n2. add a logout button\n3) write tests for both\n", []string{"add a login page", "add a logout button", "write tests for both"}, "add a login page", 0},
		{"one job per line of a text file", "deneme.txt", "Yapılacaklar:\nlogin sayfasını düzelt\ntestleri yaz\n\nreadme dosyasını güncelle\n", []string{"login sayfasını düzelt", "testleri yaz", "readme dosyasını güncelle"}, "login sayfasını düzelt", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, project := queueTrustSandbox(t, false)
			writeJobFile(t, project, tc.file, tc.content)
			output := startQueue(t, cfg, "sq2", project, "/noctis:start "+tc.file)
			if message := getString(output, "systemMessage"); message != T("queue.startDetected", len(tc.open), tc.file, pluginName) {
				t.Fatalf("/noctis:start %s: %q, want %d jobs detected (%v)", tc.file, message, len(tc.open), output)
			}
			checklist := sessionQueueFile(cfg, "sq2", project)
			if got := openJobs(t, checklist); !slices.Equal(got, tc.open) {
				t.Errorf("the checklist holds %q, want %q", got, tc.open)
			}
			if snapshot := queueSnapshot(checklist); len(snapshot.items) == 0 || snapshot.items[0] != tc.eligible || snapshot.blocked != tc.blocked {
				t.Errorf("the checklist's next job is %q with %d waiting on a dependency, want %q and %d: the item numbers (after …) refers to moved", snapshot.items, snapshot.blocked, tc.eligible, tc.blocked)
			}
		})
	}
}

func TestAStartedQueueDrivesTheSessionUntilEveryJobIsTicked(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	writeJobFile(t, project, "deneme.md", "- add a login page\n- add a logout button\n")
	checklist := startedChecklist(t, cfg, "sq3", project, "deneme.md")
	if output := stopHookOutput(t, stopInput("sq3", project), cfg); getString(output, "decision") != "block" || !strings.Contains(getString(output, "reason"), "add a login page") {
		t.Fatalf("the Stop hook let the session stop with both jobs open: %v", output)
	}
	tickAll(t, checklist)
	output := stopHookOutput(t, stopInput("sq3", project), cfg)
	if message := getString(output, "systemMessage"); message != T("queue.doneMessage", "deneme.md") {
		t.Errorf("the finished queue is not named after the file it came from: %q", message)
	}
	if left := sessionQueueFile(cfg, "sq3", project); left != "" {
		t.Errorf("the finished queue still drives the session: %q", left)
	}
}

func TestAStartedQueueOutlivesThePromptsTypedWhileItRuns(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	writeJobFile(t, project, "deneme.md", "- add a login page\n- add a logout button\n")
	checklist := startedChecklist(t, cfg, "sq4", project, "deneme.md")
	startQueue(t, cfg, "sq4", project, "use tabs, not spaces, in the new files")
	if got := sessionQueueFile(cfg, "sq4", project); got != checklist {
		t.Fatalf("a prompt typed while the started queue ran ended it (queue file now %q)", got)
	}
	pad := "Here is some context about the project so that the prompt is long enough for the detector to consider it: " + strings.Repeat("the codebase is a Node service with a Postgres database and a React front end. ", 2)
	startQueue(t, cfg, "sq4", project, pad+"\n- add input validation to the signup form\n- write tests for the payments module\n- update the README for the new CLI flags\n")
	if got := openJobs(t, checklist); !slices.Equal(got, []string{"add a login page", "add a logout button"}) {
		t.Fatalf("a prompt with several steps replaced the jobs the user started: %q", got)
	}
}

func TestAStartedQueueComesBeforeTheProjectQueueFile(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	tasks := writeQueueFile(t, project, "- [ ] migrate the database\n")
	trustQueueFile(tasks, true)
	writeJobFile(t, project, "deneme.md", "- add a login page\n")
	startedChecklist(t, cfg, "sq5", project, "deneme.md")
	if output := stopHookOutput(t, stopInput("sq5", project), cfg); !strings.Contains(getString(output, "reason"), "add a login page") {
		t.Fatalf("the project's TASKS.md drove the session the user had started on deneme.md: %v", output)
	}
}

func TestStopEndsTheQueueAndSaysHowFarItGot(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	source := writeJobFile(t, project, "deneme.md", "- [x] set up the repository\n- [ ] add a login page\n- [ ] add a logout button\n- [ ] write tests for both\n")
	before, _ := os.ReadFile(source)
	checklist := startedChecklist(t, cfg, "sq6", project, "deneme.md")
	content, _ := os.ReadFile(checklist)
	if err := os.WriteFile(checklist, []byte(strings.Replace(string(content), "- [ ] add a login page", "- [x] add a login page", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	output := startQueue(t, cfg, "sq6", project, "/noctis:stop")
	if getString(output, "decision") != "block" || getString(output, "reason") != T("queue.stopDone", 1, 3, "deneme.md") {
		t.Fatalf("/noctis:stop does not say how many of the three jobs were done: %v", output)
	}
	if left := sessionQueueFile(cfg, "sq6", project); left != "" {
		t.Errorf("the stopped queue still drives the session: %q", left)
	}
	if _, err := os.Stat(checklist); !os.IsNotExist(err) {
		t.Errorf("the stopped queue's checklist was left behind: %v", err)
	}
	if output := stopHookOutput(t, stopInput("sq6", project), cfg); output != nil {
		t.Errorf("the Stop hook still continues the stopped queue: %v", output)
	}
	if after, _ := os.ReadFile(source); string(after) != string(before) {
		t.Errorf("stopping the queue changed the user's file:\n%s", after)
	}
	if output := startQueue(t, cfg, "sq6", project, "/noctis:stop"); getString(output, "reason") != T("queue.stopNone") {
		t.Errorf("/noctis:stop with no queue running: %v", output)
	}
}

func TestStopNamesTheProjectFileThatDrivesTheSession(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	trustQueueFile(writeQueueFile(t, project, "- [ ] migrate the database\n"), true)
	if output := startQueue(t, cfg, "sq7", project, "/noctis:stop"); getString(output, "reason") != T("queue.stopProjectFile", "TASKS.md") {
		t.Errorf("/noctis:stop in a folder whose TASKS.md the user trusted does not say how to stop that: %v", output)
	}
}

func TestStartRefusesAFileItCannotUse(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	if err := os.Mkdir(filepath.Join(project, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJobFile(t, project, "image.md", "- add a login page\x00\x01\x02\n")
	writeJobFile(t, project, "done.md", "- [x] add a login page\n- [x] add a logout button\n")
	writeJobFile(t, project, "empty.txt", "\n\n")
	writeJobFile(t, project, "huge.txt", strings.Repeat("add one more small feature\n", startedQueueMaxJobs+1))
	cases := []struct{ prompt, reason string }{
		{"/noctis:start", T("queue.startUsage", pluginName)},
		{"/noctis:start nothing-here.md", T("queue.startUnreadable", filepath.Join(project, "nothing-here.md"))},
		{"/noctis:start notes", T("queue.startUnreadable", filepath.Join(project, "notes"))},
		{"/noctis:start image.md", T("queue.startUnreadable", filepath.Join(project, "image.md"))},
		{"/noctis:start done.md", T("queue.startEmpty", "done.md")},
		{"/noctis:start empty.txt", T("queue.startEmpty", "empty.txt")},
		{"/noctis:start huge.txt", T("queue.startTooBig", "huge.txt", startedQueueMaxJobs)},
	}
	for _, tc := range cases {
		output := startQueue(t, cfg, "sq8", project, tc.prompt)
		if getString(output, "decision") != "block" || getString(output, "reason") != tc.reason {
			t.Errorf("%q: %v, want it refused with %q", tc.prompt, output, tc.reason)
		}
		if record := getMap(getMap(readState(), "autoQueues"), "sq8"); record != nil {
			t.Fatalf("%q started a queue anyway: %v", tc.prompt, record)
		}
	}
}

func TestWhileNoctisIsPausedStartSaysSoAndStopStillStops(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	writeJobFile(t, project, "deneme.md", "- add a login page\n- add a logout button\n")
	startedChecklist(t, cfg, "sq9", project, "deneme.md")
	until := float64(nowSec() + 3600)
	updateState(func(state object) { state["disabledUntil"] = until })
	output := startQueue(t, cfg, "sq9-other", project, "/noctis:start deneme.md")
	if getString(output, "decision") != "block" || getString(output, "reason") != T("queue.startPaused", formatTime(until), pluginName) {
		t.Errorf("/noctis:start while noctis is paused does not say that it would not drive the queue: %v", output)
	}
	if record := getMap(getMap(readState(), "autoQueues"), "sq9-other"); record != nil {
		t.Errorf("/noctis:start while noctis is paused started a queue: %v", record)
	}
	output = startQueue(t, cfg, "sq9", project, "/noctis:stop")
	if getString(output, "reason") != T("queue.stopDone", 0, 2, "deneme.md") || sessionQueueFile(cfg, "sq9", project) != "" {
		t.Errorf("/noctis:stop while noctis is paused did not stop the queue: %v", output)
	}
}

func TestAtTheCreditCeilingStartIsRefusedLikeTheOtherControlCommands(t *testing.T) {
	cfg, project := controlSandbox(t, 40, 95)
	writeUsage(100, 95, float64(nowSec()+7200))
	writeJobFile(t, project, "deneme.md", "- add a login page\n- add a logout button\n")
	if output := startQueue(t, cfg, "sq10", project, "/noctis:start deneme.md"); getString(output, "decision") != "block" {
		t.Errorf("/noctis:start went past the paid-credit ceiling: %v", output)
	}
	if record := getMap(getMap(readState(), "autoQueues"), "sq10"); record != nil {
		t.Errorf("/noctis:start at the paid-credit ceiling started a queue: %v", record)
	}
}

func TestAResumedSessionIsToldWhereItsStartedQueueCameFrom(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	source := writeJobFile(t, project, "deneme.md", "- add a login page\n- add a logout button\n")
	checklist := startedChecklist(t, cfg, "sq11", project, "deneme.md")
	output := hookOutput(t, onSessionStart, agentHookInput("SessionStart", "sq11", project, object{"source": "resume"}), cfg)
	if context := getString(getMap(output, "hookSpecificOutput"), "additionalContext"); !strings.Contains(context, checklist) || !strings.Contains(context, source) {
		t.Errorf("the resumed session is not told where its checklist and the file it came from are:\n%s", context)
	}
}

func TestClaudeCannotStartOrStopAQueueItself(t *testing.T) {
	for _, name := range []string{"start", "stop"} {
		skill := readShippedSkill(t, name)
		if skill.modelInvocable {
			t.Errorf("skills/%s can be started by Claude itself (a file in the repository can ask it to); it needs disable-model-invocation: true", name)
		}
		if len(skill.allowed) > 0 || len(skill.commands) > 0 {
			t.Errorf("skills/%s pre-approves %q or runs %q; noctis does its work when the command is typed, so the skill needs no tool", name, skill.allowed, skill.commands)
		}
	}
}

func TestStopEndsTheQueueAtTheUsageCeiling(t *testing.T) {
	cfg, project := controlSandbox(t, 40, 20)
	writeJobFile(t, project, "deneme.md", "- add a login page\n- add a logout button\n")
	startedChecklist(t, cfg, "sc1", project, "deneme.md")
	writeUsage(100, 20, float64(nowSec()+7200))
	if output := startQueue(t, cfg, "sc1", project, "/noctis:stop"); getString(output, "reason") != T("queue.stopDone", 0, 2, "deneme.md") {
		t.Errorf("/noctis:stop at the usage ceiling did not say how far the queue got: %v", output)
	}
	if record := getMap(getMap(readState(), "autoQueues"), "sc1"); record != nil {
		t.Fatalf("the queue outlives /noctis:stop at the usage ceiling, so it goes on after the reset: %v", record)
	}
}

func TestStopEndsTheQueueFromTheWindowItsSessionMovedOutOf(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	writeJobFile(t, project, "deneme.md", "- add a login page\n- add a logout button\n")
	startedChecklist(t, cfg, "sc2", project, "deneme.md")
	updateState(func(state object) {
		stateMap(state, "handedOff")["sc2"] = object{"at": float64(nowSec()), "mode": "headless", "pid": float64(os.Getpid())}
	})
	if output := startQueue(t, cfg, "sc2", project, "/noctis:stop"); getString(output, "reason") != T("queue.stopDone", 0, 2, "deneme.md") {
		t.Errorf("/noctis:stop in the window the session moved out of did not say how far the queue got: %v", output)
	}
	if record := getMap(getMap(readState(), "autoQueues"), "sc2"); record != nil {
		t.Fatalf("the queue outlives /noctis:stop typed in the window the session moved out of: %v", record)
	}
}
