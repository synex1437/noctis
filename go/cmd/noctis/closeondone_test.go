package main

import (
	"os"
	"path/filepath"
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

func issueStatus(queuePath, ref string) string {
	return getString(getMap(getMap(readState(), "githubSeen"), queuePath+"#"+ref), "status")
}

func TestACloseGhRefusedIsTriedAgainAtTheNextStop(t *testing.T) {
	cfg, project := queueTrustSandbox(t, true)
	calls := fakeGhCLI(t, "[]")
	failing := filepath.Join(t.TempDir(), "fail")
	t.Setenv("NOCTIS_TEST_GH_FAIL", failing)
	queuePath := writeQueueFile(t, project, "# q\n- [ ] #12 Backend crash\n")
	syncDoneIssues(cfg, queuePath, project)
	tickIssueItem(t, queuePath, "#12 Backend crash")
	if err := os.WriteFile(failing, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	syncDoneIssues(cfg, queuePath, project)
	if status := issueStatus(queuePath, "12"); status == "closed" || len(ghLoggedCalls(calls, "close")) != 1 {
		t.Fatalf("gh refused to close #12, yet it is recorded as %q after %d close call(s)", status, len(ghLoggedCalls(calls, "close")))
	}
	if err := os.Remove(failing); err != nil {
		t.Fatal(err)
	}
	syncDoneIssues(cfg, queuePath, project)
	syncDoneIssues(cfg, queuePath, project)
	if closes := ghLoggedCalls(calls, "close"); len(closes) != 2 || issueStatus(queuePath, "12") != "closed" {
		t.Fatalf("after the refused close the next stops ran %d close call(s) in all and left #12 %q; want one more call and closed", len(closes), issueStatus(queuePath, "12"))
	}
}

func TestACloseThatKeepsFailingIsGivenUpAndLogged(t *testing.T) {
	cfg, project := queueTrustSandbox(t, true)
	calls := fakeGhCLI(t, "[]")
	failing := filepath.Join(t.TempDir(), "fail")
	t.Setenv("NOCTIS_TEST_GH_FAIL", failing)
	if err := os.WriteFile(failing, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	queuePath := writeQueueFile(t, project, "# q\n- [ ] #12 Backend crash\n")
	syncDoneIssues(cfg, queuePath, project)
	tickIssueItem(t, queuePath, "#12 Backend crash")
	for stop := 0; stop < 5; stop++ {
		syncDoneIssues(cfg, queuePath, project)
	}
	if closes := ghLoggedCalls(calls, "close"); len(closes) != 3 || issueStatus(queuePath, "12") != "failed" {
		t.Fatalf("five stops over a close gh keeps refusing ran %d close call(s) and left #12 %q; want 3 calls, then failed", len(closes), issueStatus(queuePath, "12"))
	}
	if errors := string(readFileOrEmpty(files.errors)); !strings.Contains(errors, "#12") || !strings.Contains(errors, "api.github.com") {
		t.Fatalf("the refused closes are not in errors.log with gh's own words:\n%s", errors)
	}
}
