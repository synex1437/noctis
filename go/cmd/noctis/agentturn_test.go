package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// m1ReportBody is the final report of an audit subagent: finding titles, the
// fields of each finding, the checks it ran and the next steps it suggests.
const m1ReportBody = `Audit of the queue code is done: two findings, both reproduced on the current build.

- Plan-only prompts still get a checklist, and the Stop hook then drives the forbidden work
- Where: heldback.go:167-189 englishNegationEnd and heldback.go:420 listReferenceWithin
- What happens: a prompt that asks only for an estimate got a 4-step job and three blocked stops
- Fix direction: accept the particle after the negation and widen the list-reference window
- The forbid scan reads the list items, so a rule inside one item holds the whole job back
- Where: heldback.go:843-855 heldBackWork runs englishForbids over the items as well
- Fix direction: run the forbid scan over the words around the list only
- Status: both confirmed with a black-box run, and a unit test for each is ready

Checks:
- gofmt -l ./cmd/noctis printed nothing: pass
- go vet ./... reported no problems: pass
- go test -run HeldBack ./cmd/noctis: 14 passed, 0 failed

Suggested next steps:
- add a regression test for every plan-only prompt in the finding
- widen the list-reference window in englishTalkLead to the whole clause
- run the forbid scan over the prose only and keep the item text out of it
`

// m1AgentTurns are user turns Claude Code writes itself, each carrying the
// report: a subagent's hand-back as the finding shows it, a message from
// another session, a background task's notice, a system notice and a Stop
// hook's feedback.
func m1AgentTurns() map[string]string {
	return map[string]string{
		"m1-handback":      "Another Claude session sent a message: <\\agent-message from=\"queue-audit\">[Subagent hand-back] " + m1ReportBody + "</agent-message>",
		"m1-message":       "Another Claude session sent a message:\n<agent-message from=\"queue-audit\">\n" + m1ReportBody + "</agent-message>",
		"m1-handback-only": "[Subagent hand-back]\n" + m1ReportBody,
		"m1-task":          "<task-notification>\n<task-id>bq71</task-id>\n<status>completed</status>\n<summary>Agent \"queue-audit\" completed</summary>\n<result>\n" + m1ReportBody + "</result>\n</task-notification>",
		"m1-system":        "[SYSTEM NOTIFICATION - NOT USER INPUT] The background agent queue-audit finished.\n" + m1ReportBody,
		"m1-stop-feedback": "Stop hook feedback:\n" + m1ReportBody,
	}
}

func m1NamesAMarker(reason string) bool {
	for _, marker := range []string{"Another Claude session sent a message:", "<agent-message", `<\agent-message`, "[Subagent hand-back]", "<task-notification>", "[SYSTEM NOTIFICATION", "Stop hook feedback:"} {
		if strings.Contains(reason, marker) {
			return true
		}
	}
	return false
}

func TestASubagentReportHandedBackAsAPromptGetsNoChecklist(t *testing.T) {
	if items := autoQueueItems(m1ReportBody); len(items) < autoQueueMinItems {
		t.Fatalf("the report does not read as a job without the words Claude Code wraps it in, so it tests nothing: %q", items)
	}
	cfg, project := queueTrustSandbox(t, false)
	for sid, prompt := range m1AgentTurns() {
		output := startQueue(t, cfg, sid, project, prompt)
		if record := getMap(getMap(readState(), "autoQueues"), sid); record != nil {
			t.Errorf("%s: a report Claude Code handed to the session became the user's checklist of %v items", sid, record["items"])
		}
		if context := contextOf(output); strings.Contains(context, "multi-step job") || strings.Contains(context, "several separate jobs") {
			t.Errorf("%s: Claude was told to work through another agent's report as a job: %q", sid, context)
		}
		if message := getString(output, "systemMessage"); strings.Contains(message, "step job") {
			t.Errorf("%s: the user was told the report is a job: %q", sid, message)
		}
		if stop := stopHookOutput(t, activeStop(sid, project), cfg); getString(stop, "decision") == "block" {
			t.Errorf("%s: the Stop hook drove Claude through the lines of another agent's report: %v", sid, stop)
		}
		if reason := journaledReason(sid, "no-auto-queue"); !m1NamesAMarker(reason) {
			t.Errorf("%s: noctis why does not say that the prompt came from Claude Code, not the user: %q", sid, reason)
		}
	}
}

func TestASubagentReportLeavesTheUsersOwnChecklistAsItIs(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	sid := "m1-own"
	startQueue(t, cfg, sid, project, "Please do all of the following today, one after the other, and tell me when you are done."+heldBackItems)
	before := getMap(getMap(readState(), "autoQueues"), sid)
	if numberOr(before, "items", 0) != 4 {
		t.Fatalf("the user's four listed jobs did not become the checklist: %v items", before["items"])
	}
	path := getString(before, "path")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	notice := "<task-notification>\n<task-id>bq72</task-id>\n<status>completed</status>\n<summary>Background command \"go test ./...\" completed (exit code 0)</summary>\n</task-notification>"
	for _, turn := range []string{m1AgentTurns()["m1-handback"], notice} {
		startQueue(t, cfg, sid, project, turn)
		after := getMap(getMap(readState(), "autoQueues"), sid)
		if numberOr(after, "items", 0) != 4 || getString(after, "path") != path {
			t.Fatalf("a turn Claude Code wrote replaced or ended the checklist of the user's own prompt: %v items in %q", after["items"], getString(after, "path"))
		}
		if now, _ := os.ReadFile(path); string(now) != string(content) {
			t.Fatalf("a turn Claude Code wrote rewrote the user's checklist:\n%s", now)
		}
	}
	stop := stopHookOutput(t, stopInput(sid, project), cfg)
	if reason := getString(stop, "reason"); getString(stop, "decision") != "block" || !strings.Contains(reason, "add a login endpoint with rate limiting") {
		t.Errorf("after another agent's report the Stop hook no longer drives the user's own first item: %v", stop)
	}
}

func TestInObserveModeASubagentReportIsJournaledAsNoAutoQueue(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	defer func(previous bool) { observing = previous }(observing)
	observing = true
	sid := "m1-observe"
	startQueue(t, cfg, sid, project, m1AgentTurns()["m1-handback"])
	if record := getMap(getMap(readState(), "autoQueues"), sid); record != nil {
		t.Fatalf("observe mode started a checklist of %v items", record["items"])
	}
	found := false
	for _, line := range tailFileLines(files.decisions, 1000) {
		var entry object
		if jsonUnmarshalObject([]byte(line), &entry) == nil && getString(entry, "sid") == sid && getString(entry, "action") == "no-auto-queue" {
			found = getBool(entry, "observe", false) && m1NamesAMarker(getString(entry, "reason"))
		}
	}
	if !found {
		t.Errorf("observe mode did not journal that the report Claude Code handed over is no job of the user's")
	}
}

func TestTheFieldsOfAFindingAndPassedChecksInAListAreNotSteps(t *testing.T) {
	steps := []string{
		"add a login endpoint with rate limiting to the auth service",
		"rewrite the payment module so it uses the new billing client",
		"remove the legacy logging from the worker and the scheduler",
	}
	fields := []string{
		"Where: src/auth/login.ts:40-88 and src/auth/limits.ts:12",
		"Status: confirmed on staging with the load test from last week",
		"Fix direction: move the limiter in front of the password check",
		"What happens: every failed login hits the database twice",
		"**Severity:** high, since the endpoint is public and unauthenticated",
		"gofmt -l ./cmd/noctis printed nothing: pass",
		"go test ./cmd/noctis after the change: 14 passed",
	}
	listed := append(append([]string{steps[0]}, fields...), steps[1:]...)
	job := promptJobOf("Please do the following for the next release, one after the other, and keep the notes below in mind:" + bullets(listed))
	if !slices.Equal(job.items, steps) {
		t.Errorf("the checklist holds %q, want only the three steps %q", job.items, steps)
	}
	for _, field := range fields {
		if !slices.Contains(job.dropped, cleanItem(field)) {
			t.Errorf("%q is not kept as a listed line that is no step: %q", field, job.dropped)
		}
	}
	turkishSteps := heldBackItemsTrList()[:3]
	turkishFields := []string{
		"Nerede: auth/login.go:40-88 ve auth/limits.go:12 dosyaları",
		"Kök neden: sınırlayıcı parola kontrolünden sonra çalışıyor",
		"Düzeltme yönü: sınırlayıcıyı parola kontrolünün önüne taşı",
	}
	job = promptJobOf("Aşağıdaki işleri sırayla yap ve bitince bana haber ver, aradaki notları da göz önünde tut:" + bullets(append(append([]string{turkishSteps[0]}, turkishFields...), turkishSteps[1:]...)))
	if !slices.Equal(job.items, turkishSteps) {
		t.Errorf("the Turkish checklist holds %q, want only the three steps %q", job.items, turkishSteps)
	}
	if items := autoQueueItems(m1ReportBody); len(items) != 4 {
		t.Errorf("the report's own lines should give its finding title and its three suggested steps, got %d: %q", len(items), items)
	}
}

func TestAListOfTasksWrittenUnderAreaLabelsIsStillAChecklist(t *testing.T) {
	tasks := []string{
		"Login page: the submit button does nothing on older phones",
		"Settings: the dark mode toggle resets after a reload",
		"Checkout: the total ignores the discount code on mobile",
		"Location: ask for the GPS permission only when the map opens",
		"Search results: sort them by relevance instead of by date",
		"Docs: update the README for the new CLI flags",
		"Fix: the retry loop in the uploader never gives up",
	}
	expectItems(t, "area labels", "I found these while testing the release candidate on my phone and laptop this morning. Please fix them all, one after the other:"+bullets(tasks), tasks)
}
