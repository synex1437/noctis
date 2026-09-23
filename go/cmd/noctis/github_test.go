package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeGhCLI(t *testing.T, listing string) string {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "gh-calls.log")
	issues := filepath.Join(dir, "gh-issues.json")
	if err := os.WriteFile(issues, []byte(listing), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOCTIS_TEST_GH_CALLS", calls)
	t.Setenv("NOCTIS_TEST_GH_ISSUES", issues)
	name, script := "gh", "#!/bin/sh\necho \"gh $*\" >> \"$NOCTIS_TEST_GH_CALLS\"\nif [ \"$1\" = issue ] && [ \"$2\" = list ]; then cat \"$NOCTIS_TEST_GH_ISSUES\"; fi\n"
	if isWindows {
		name, script = "gh.cmd", "@echo off\r\n>>\"%NOCTIS_TEST_GH_CALLS%\" echo gh %*\r\nif \"%~1\"==\"issue\" if \"%~2\"==\"list\" type \"%NOCTIS_TEST_GH_ISSUES%\"\r\n"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return calls
}

func ghLoggedCalls(log, verb string) []string {
	data, _ := os.ReadFile(log)
	calls := []string{}
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "gh issue "+verb+" ") {
			calls = append(calls, line)
		}
	}
	return calls
}

func ghLoggedCloses(t *testing.T, log string, want int) []string {
	t.Helper()
	journaled, _ := os.ReadFile(files.decisions)
	if started := strings.Count(string(journaled), `"action":"close-issue"`); started > want {
		want = started
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		closes := ghLoggedCalls(log, "close")
		if len(closes) >= want || time.Now().After(deadline) {
			return closes
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func queueImportOutput(t *testing.T, argv ...string) string {
	t.Helper()
	previous := args
	defer func() { args = previous }()
	args = parseArgs(argv)
	return capturedStdout(t, runQueue)
}

func issueQueueText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func tickIssueItem(t *testing.T, path, item string) {
	t.Helper()
	content := issueQueueText(t, path)
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "- [ ] ") && strings.Contains(line, item) {
			writeQueueFile(t, filepath.Dir(path), strings.Replace(content, line, "- [x] "+strings.TrimPrefix(line, "- [ ] "), 1))
			return
		}
	}
	t.Fatalf("no open item %q in %s:\n%s", item, filepath.Base(path), content)
}

func TestTickingAnIssueClosesOnlyItsOwnReference(t *testing.T) {
	cfg, project := queueTrustSandbox(t, true)
	calls := fakeGhCLI(t, "[]")
	queuePath := writeQueueFile(t, project, "# q\n- [ ] #12 Follow-up to #45\n")
	syncDoneIssues(cfg, queuePath, project)
	tickIssueItem(t, queuePath, "#12 Follow-up to #45")
	syncDoneIssues(cfg, queuePath, project)
	closes := ghLoggedCloses(t, calls, 1)
	if len(closes) != 1 || !strings.HasPrefix(closes[0], "gh issue close 12 ") {
		t.Fatalf("ticking \"#12 Follow-up to #45\" ran %q; want only issue close 12, since #45 is only named in the title", closes)
	}
	syncDoneIssues(cfg, queuePath, project)
	if closes := ghLoggedCloses(t, calls, 1); len(closes) != 1 {
		t.Fatalf("a second pass over the same ticked item closed again: %q", closes)
	}
}

func TestAnIssueNamedInATickedItemStaysOpenWhileItsOwnItemIsOpen(t *testing.T) {
	cfg, project := queueTrustSandbox(t, true)
	calls := fakeGhCLI(t, "[]")
	queuePath := writeQueueFile(t, project, "# q\n- [ ] #12 Follow-up to #45\n- [ ] #45 Flaky login test\n")
	trustQueueFile(queuePath, true)
	stopHookOutput(t, stopInput("gh1", project), cfg)
	tickIssueItem(t, queuePath, "#12 Follow-up to #45")
	output := stopHookOutput(t, stopInput("gh1", project), cfg)
	if getString(output, "decision") != "block" || !strings.Contains(getString(output, "reason"), "#45 Flaky login test") {
		t.Fatalf("the Stop hook no longer sends the session on to the open #45 item: %v", output)
	}
	closes := ghLoggedCloses(t, calls, 1)
	if len(closes) != 1 || !strings.HasPrefix(closes[0], "gh issue close 12 ") {
		t.Fatalf("ticking #12 ran %q; want only issue close 12, never #45 whose item the session is being sent to", closes)
	}
}

func TestAnIssueStaysOpenUntilEveryItemCarryingItIsTicked(t *testing.T) {
	cfg, project := queueTrustSandbox(t, true)
	calls := fakeGhCLI(t, "[]")
	queuePath := writeQueueFile(t, project, "# q\n- [ ] #12 backend part\n- [ ] #12 frontend part\n")
	syncDoneIssues(cfg, queuePath, project)
	tickIssueItem(t, queuePath, "#12 backend part")
	syncDoneIssues(cfg, queuePath, project)
	if closes := ghLoggedCloses(t, calls, 0); len(closes) != 0 {
		t.Fatalf("#12 was closed while its frontend item was still open: %q", closes)
	}
	tickIssueItem(t, queuePath, "#12 frontend part")
	syncDoneIssues(cfg, queuePath, project)
	if closes := ghLoggedCloses(t, calls, 1); len(closes) != 1 || !strings.HasPrefix(closes[0], "gh issue close 12 ") {
		t.Fatalf("ticking the last item of #12 ran %q; want one issue close 12", closes)
	}
}

func TestImportAddsAnIssueThatIsOnlyMentionedInTheFile(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	fakeGhCLI(t, `[{"number":12,"title":"Follow-up to #45","labels":[]},{"number":45,"title":"Flaky login test","labels":[]},{"number":7,"title":"Ship the release","labels":[]}]`)
	queuePath := writeQueueFile(t, project, "# Release #7 checklist\n- [ ] #12 Follow-up to #45\n")
	queueImportOutput(t, "queue", "import", "--cwd", project)
	content := issueQueueText(t, queuePath)
	for _, want := range []string{"- [ ] #45 Flaky login test\n", "- [ ] #7 Ship the release\n"} {
		if !strings.Contains(content, want) {
			t.Errorf("the import did not add %q; an issue named only in another item or a heading is not tracked:\n%s", strings.TrimSpace(want), content)
		}
	}
	if strings.Count(content, "#12 ") != 1 {
		t.Errorf("the import added #12 again although its item is in the file:\n%s", content)
	}
}

func TestImportKeepsAnIssueTrackedByAnyItemThatStartsWithIt(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	fakeGhCLI(t, `[{"number":14,"title":"Old item","labels":[]},{"number":15,"title":"Plain bullet","labels":[]},{"number":16,"title":"Todo line","labels":[]}]`)
	content := "# q\n- [x] (P2) #14 Old item\n\n## notes\n- #15 Plain bullet\nTODO: #16 Todo line\n"
	queuePath := writeQueueFile(t, project, content)
	output := queueImportOutput(t, "queue", "import", "--cwd", project)
	if after := issueQueueText(t, queuePath); after != content {
		t.Fatalf("the import added issues whose items are already in the file (%s):\n%s", strings.TrimSpace(output), after)
	}
}

func TestTheItemsOwnIssueIsTheReferenceItStartsWith(t *testing.T) {
	cases := []struct{ text, repo, number string }{
		{"(P1) #12 Fix login redirect", "", "12"},
		{"(p7)#13 Write the API docs", "", "13"},
		{"#14 Old item already tracked", "", "14"},
		{"#12 Follow-up to #45", "", "12"},
		{"#12", "", "12"},
		{"org/backend#12 Backend crash", "org/backend", "12"},
		{"(P1) Org/Backend#12 Backend crash", "Org/Backend", "12"},
		{"ghe.example.com/org/backend#12 Backend crash", "ghe.example.com/org/backend", "12"},
		{"org/.github#3 Update the profile", "org/.github", "3"},
	}
	for _, tc := range cases {
		issue, found := itemIssue(tc.text)
		if !found || issue.repo != tc.repo || issue.number != tc.number {
			t.Errorf("itemIssue(%q) = %+v, %v; want repo %q number %q", tc.text, issue, found, tc.repo, tc.number)
		}
	}
	for _, text := range []string{"Follow-up to #45", "Revert #78", "fix login redirect (#12)", "(after #3) #12 later", "#frontend fix the header", "#12abc", "-x/y#12 flag", "src/app/main.go#12 fix", "# 12", ""} {
		if issue, found := itemIssue(text); found {
			t.Errorf("itemIssue(%q) = %+v; an item that does not start with a reference is no issue's item", text, issue)
		}
	}
}

func TestImportRepoAcceptsTheFormsGhTakes(t *testing.T) {
	cases := []struct{ value, want string }{
		{"", ""},
		{"org/backend", "org/backend"},
		{" org/backend ", "org/backend"},
		{"ghe.example.com/org/backend", "ghe.example.com/org/backend"},
		{"https://github.com/org/backend", "github.com/org/backend"},
		{"https://www.GitHub.com/org/backend.git/", "github.com/org/backend"},
		{"git@github.com:org/backend.git", "github.com/org/backend"},
		{"ssh://git@ghe.example.com:22/org/backend.git", "ghe.example.com/org/backend"},
	}
	for _, tc := range cases {
		got, valid := importRepo(tc.value)
		if !valid || got != tc.want {
			t.Errorf("importRepo(%q) = %q, %v; want %q", tc.value, got, valid, tc.want)
			continue
		}
		if issue, found := itemIssue("(P1) " + got + "#12 Title"); got != "" && (!found || issue.repo != got || issue.number != "12") {
			t.Errorf("an item imported from %q does not read back as %s#12: %+v", tc.value, got, issue)
		}
	}
	for _, value := range []string{"org", "-org/backend", "org/backend --label bug", "a/b/c/d", "ghe/org/backend", "../backend", "https://github.com", "https://github.com/org/backend/issues/12"} {
		if got, valid := importRepo(value); valid {
			t.Errorf("importRepo(%q) accepted %q; want the usage text", value, got)
		}
	}
}

func TestARepoImportIsClosedInThatRepository(t *testing.T) {
	cfg, project := queueTrustSandbox(t, true)
	calls := fakeGhCLI(t, `[{"number":12,"title":"Backend crash","labels":[{"name":"P1"}]}]`)
	queuePath := filepath.Join(project, "TASKS.md")
	queueImportOutput(t, "queue", "import", "--repo", "org/backend", "--cwd", project)
	if lists := ghLoggedCalls(calls, "list"); len(lists) != 1 || !strings.Contains(lists[0]+" ", " --repo org/backend ") {
		t.Fatalf("the list call did not name the repository: %q", lists)
	}
	if content := issueQueueText(t, queuePath); !strings.Contains(content, "- [ ] (P1) org/backend#12 Backend crash\n") {
		t.Errorf("an import from org/backend was written without its repository:\n%s", content)
	}
	syncDoneIssues(cfg, queuePath, project)
	tickIssueItem(t, queuePath, "Backend crash")
	syncDoneIssues(cfg, queuePath, project)
	closes := ghLoggedCloses(t, calls, 1)
	if len(closes) != 1 || !strings.HasPrefix(closes[0], "gh issue close 12 ") || !strings.Contains(closes[0]+" ", " --repo org/backend ") {
		t.Fatalf("ticking org/backend#12 ran %q; want issue close 12 with --repo org/backend, not #12 of the checkout", closes)
	}
}

func TestARepoImportIsNotHiddenByTheCheckoutsOwnIssue(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	fakeGhCLI(t, `[{"number":12,"title":"Backend crash","labels":[]},{"number":13,"title":"Backend leak","labels":[]}]`)
	queuePath := writeQueueFile(t, project, "# q\n- [ ] #12 Local crash\n- [x] Org/Backend#13 Backend leak\n")
	queueImportOutput(t, "queue", "import", "--repo", "org/backend", "--cwd", project)
	content := issueQueueText(t, queuePath)
	if !strings.Contains(content, "- [ ] org/backend#12 Backend crash\n") {
		t.Fatalf("the checkout's own #12 kept org/backend#12 out of the import:\n%s", content)
	}
	if strings.Contains(content, "org/backend#13") {
		t.Fatalf("org/backend#13 was imported again although Org/Backend#13 is in the file:\n%s", content)
	}
	for _, repo := range []string{"org/backend", "https://github.com/org/backend"} {
		queueImportOutput(t, "queue", "import", "--repo", repo, "--cwd", project)
		if again := issueQueueText(t, queuePath); again != content {
			t.Fatalf("importing --repo %s again changed the file:\n%s", repo, again)
		}
	}
}

func TestTickingAnIssueOnAHostTheFileNamesRunsNoGh(t *testing.T) {
	cfg, project := queueTrustSandbox(t, true)
	calls := fakeGhCLI(t, "[]")
	t.Setenv("GH_HOST", "")
	queuePath := writeQueueFile(t, project, "# q\n- [ ] evil.example.com/o/r#1 innocuous task\n- [ ] ghe.example.com/org/api#3 API crash\n- [ ] GitHub.com/org/backend#12 Backend crash\n- [ ] write the release notes\n")
	trustQueueFile(queuePath, true)
	stopHookOutput(t, stopInput("gh2", project), cfg)
	for _, item := range []string{"innocuous task", "API crash", "Backend crash"} {
		tickIssueItem(t, queuePath, item)
	}
	if output := stopHookOutput(t, stopInput("gh2", project), cfg); getString(output, "decision") != "block" {
		t.Fatalf("the Stop hook no longer sends the session on to the open item: %v", output)
	}
	closes := ghLoggedCloses(t, calls, 1)
	if len(closes) != 1 || !strings.HasPrefix(closes[0], "gh issue close 12 --repo GitHub.com/org/backend ") {
		t.Fatalf("the Stop hook ran %q; want only the github.com close, never gh sent to a host the queue file names", closes)
	}
	logged, _ := os.ReadFile(files.errors)
	for _, issue := range []string{"evil.example.com/o/r#1", "ghe.example.com/org/api#3"} {
		if !strings.Contains(string(logged), issue) {
			t.Errorf("the skipped close of %s left no warning:\n%s", issue, logged)
		}
	}
}

func TestAnIssueOnGhHostIsClosedThere(t *testing.T) {
	cfg, project := queueTrustSandbox(t, true)
	calls := fakeGhCLI(t, "[]")
	t.Setenv("GH_HOST", "GHE.example.com")
	queuePath := writeQueueFile(t, project, "# q\n- [ ] ghe.example.com/org/api#3 API crash\n- [ ] evil.example.com/o/r#1 innocuous task\n")
	syncDoneIssues(cfg, queuePath, project)
	tickIssueItem(t, queuePath, "API crash")
	tickIssueItem(t, queuePath, "innocuous task")
	syncDoneIssues(cfg, queuePath, project)
	closes := ghLoggedCloses(t, calls, 1)
	if len(closes) != 1 || !strings.HasPrefix(closes[0], "gh issue close 3 --repo ghe.example.com/org/api ") {
		t.Fatalf("with GH_HOST=GHE.example.com close-on-done ran %q; want only issue close 3 on that host", closes)
	}
}

func TestARepoImportQualifiesTheItemsAnOlderImportWroteAsABareNumber(t *testing.T) {
	cfg, project := queueTrustSandbox(t, true)
	calls := fakeGhCLI(t, `[{"number":12,"title":"Backend crash","labels":[{"name":"P1"}]},{"number":13,"title":"Backend leak","labels":[]},{"number":14,"title":"Backend lag","labels":[]},{"number":15,"title":"Backend flake","labels":[]}]`)
	queuePath := writeQueueFile(t, project, "# q\n- [ ] (P1) #12 Backend crash\n- [x] #13 Backend leak #backend (after #12)\n- [ ] #14 Local lag\n- [ ] #15 Backend flake\n- [ ] org/backend#15 Backend flake\n")
	output := queueImportOutput(t, "queue", "import", "--repo", "org/backend", "--cwd", project)
	want := "# q\n- [ ] (P1) org/backend#12 Backend crash\n- [x] org/backend#13 Backend leak #backend (after #12)\n- [ ] #14 Local lag\n- [ ] #15 Backend flake\n- [ ] org/backend#15 Backend flake\n\n## GitHub issues\n- [ ] org/backend#14 Backend lag\n"
	if content := issueQueueText(t, queuePath); content != want {
		t.Fatalf("importing --repo org/backend over items an older import wrote as #12 and #13 gave:\n%s\nwant:\n%s", content, want)
	}
	if strings.TrimSpace(output) != T("queue.importDone", 1, 3, "TASKS.md") {
		t.Errorf("the import reported %q; want the qualified items counted as already present", output)
	}
	queueImportOutput(t, "queue", "import", "--repo", "org/backend", "--cwd", project)
	if again := issueQueueText(t, queuePath); again != want {
		t.Fatalf("importing --repo org/backend again changed the file:\n%s", again)
	}
	syncDoneIssues(cfg, queuePath, project)
	tickIssueItem(t, queuePath, "Backend crash")
	syncDoneIssues(cfg, queuePath, project)
	closes := ghLoggedCloses(t, calls, 1)
	if len(closes) != 1 || !strings.HasPrefix(closes[0], "gh issue close 12 --repo org/backend ") {
		t.Fatalf("ticking the item an older import wrote for org/backend#12 ran %q; want issue close 12 with --repo org/backend, not #12 of the checkout", closes)
	}
}

func TestARepoImportKeepsTheLineEndingOfAQualifiedItem(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	fakeGhCLI(t, `[{"number":12,"title":"Backend crash","labels":[]}]`)
	queuePath := writeQueueFile(t, project, "# q\r\n* [ ] #12 Backend crash\r\n")
	queueImportOutput(t, "queue", "import", "--repo", "https://github.com/org/backend", "--cwd", project)
	if content := issueQueueText(t, queuePath); content != "# q\r\n* [ ] github.com/org/backend#12 Backend crash\r\n" {
		t.Fatalf("importing over a CRLF item an older import wrote as #12 gave %q; want it qualified in place with its line ending kept", content)
	}
}

func TestAnItemIsTitledAsItsIssueOnlyWhenNothingButAnnotationsFollow(t *testing.T) {
	for _, text := range []string{"Backend crash", "Backend crash #backend", "Backend crash (after #11) #api", "Backend crash (P2)", "Backend crash\t#x"} {
		if !titledAs(text, "Backend crash") {
			t.Errorf("titledAs(%q, %q) = false", text, "Backend crash")
		}
	}
	for _, text := range []string{"", "Backend", "Backend crashes", "Backend crash in the importer", "Backend crash #12", "backend crash", "Backend crash: now worse", "Local lag"} {
		if titledAs(text, "Backend crash") {
			t.Errorf("titledAs(%q, %q) = true; the item is not that issue", text, "Backend crash")
		}
	}
}
