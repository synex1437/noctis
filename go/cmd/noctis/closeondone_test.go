package main

import (
	"os"
	"strings"
	"testing"
)

func TestAnIssueImportedAndTickedBeforeTheFirstStopIsClosed(t *testing.T) {
	cfg, project := queueTrustSandbox(t, true)
	calls := fakeGhCLI(t, `[{"number":12,"title":"Backend crash","labels":[]},{"number":13,"title":"Backend leak","labels":[]}]`)
	queuePath := writeQueueFile(t, project, "# q\n- [x] #14 Old item already done\n")
	queueImportOutput(t, "queue", "import", "--cwd", project)
	trustQueueFile(queuePath, true)
	tickIssueItem(t, queuePath, "#12 Backend crash")
	stopHookOutput(t, stopInput("cd1", project), cfg)
	closes := ghLoggedCloses(t, calls, 1)
	if len(closes) != 1 || !strings.HasPrefix(closes[0], "gh issue close 12 ") {
		t.Fatalf("#12 was imported open and ticked before the session's first stop; that stop ran %q, want one issue close 12 (and never #14, ticked before noctis saw it)", closes)
	}
}

func TestAnIssueTickedBeforeTheFirstStopAfterTrustIsClosed(t *testing.T) {
	cfg, project := queueTrustSandbox(t, true)
	calls := fakeGhCLI(t, "[]")
	queuePath := writeQueueFile(t, project, "# q\n- [ ] #12 Backend crash\n- [x] #14 Old item already done\n")
	queueImportOutput(t, "queue", "trust", "--file", queuePath)
	tickIssueItem(t, queuePath, "#12 Backend crash")
	stopHookOutput(t, stopInput("cd2", project), cfg)
	if closes := ghLoggedCloses(t, calls, 1); len(closes) != 1 || !strings.HasPrefix(closes[0], "gh issue close 12 ") {
		t.Fatalf("#12 was open when the file was trusted and ticked before the first stop; that stop ran %q, want one issue close 12", closes)
	}
}

func TestAnIssueTickedBeforeTheFirstStopOfASessionIsClosed(t *testing.T) {
	cfg, project := queueTrustSandbox(t, true)
	calls := fakeGhCLI(t, "[]")
	queuePath := writeQueueFile(t, project, "# q\n- [ ] #12 Backend crash\n- [ ] #13 Backend leak\n")
	trustQueueFile(queuePath, true)
	hookOutput(t, onSessionStart, object{"hook_event_name": "SessionStart", "source": "startup", "session_id": "cd3", "cwd": project}, cfg)
	tickIssueItem(t, queuePath, "#12 Backend crash")
	stopHookOutput(t, stopInput("cd3", project), cfg)
	if closes := ghLoggedCloses(t, calls, 1); len(closes) != 1 || !strings.HasPrefix(closes[0], "gh issue close 12 ") {
		t.Fatalf("#12 was open when the session started and ticked before its first stop; that stop ran %q, want one issue close 12", closes)
	}
}

func TestAnIssueInAChecklistFromThePromptIsClosedWhenTicked(t *testing.T) {
	cfg, project := queueTrustSandbox(t, true)
	calls := fakeGhCLI(t, "[]")
	prompt := listPromptForAutoQueue() + "- #12 fix the login redirect loop on the settings page\n"
	hookOutput(t, onUserPromptSubmit, promptInput("cd4", project, prompt), cfg)
	checklist := getString(getMap(getMap(readState(), "autoQueues"), "cd4"), "path")
	if checklist == "" {
		t.Fatal("the list prompt did not become a checklist")
	}
	content, err := os.ReadFile(checklist)
	if err != nil {
		t.Fatal(err)
	}
	ticked := strings.Replace(string(content), "- [ ] #12 fix", "- [x] #12 fix", 1)
	if ticked == string(content) {
		t.Fatalf("the checklist has no #12 item:\n%s", content)
	}
	if err := os.WriteFile(checklist, []byte(ticked), 0o600); err != nil {
		t.Fatal(err)
	}
	stopHookOutput(t, stopInput("cd4", project), cfg)
	if closes := ghLoggedCloses(t, calls, 1); len(closes) != 1 || !strings.HasPrefix(closes[0], "gh issue close 12 ") {
		t.Fatalf("#12 came open in the prompt's list and was ticked before the first stop; that stop ran %q, want one issue close 12", closes)
	}
}
