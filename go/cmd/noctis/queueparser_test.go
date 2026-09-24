package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func snapshotOf(t *testing.T, content string) queueView {
	t.Helper()
	file := filepath.Join(t.TempDir(), "TASKS.md")
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return queueSnapshot(file)
}

func TestAnItemThatNamesItselfAsADependencyIsNotBlockedByItself(t *testing.T) {
	view := snapshotOf(t, "- [ ] build the api #api (after #api)\n- [ ] step two (after 2)\n")
	if view.blocked != 0 || len(view.items) != 2 {
		t.Fatalf("an item waiting on its own tag or its own number deadlocked: %+v", view)
	}
	view = snapshotOf(t, "- [ ] migrate the api #api\n- [ ] document the api #api (after #api)\n")
	if view.blocked != 1 || len(view.items) != 1 || !strings.HasPrefix(view.items[0], "migrate") {
		t.Fatalf("an item waiting on a tag another open item carries must still wait: %+v", view)
	}
}

func TestAnAfterOnAnIssueReferenceWaitsForThatIssuesItem(t *testing.T) {
	view := snapshotOf(t, "- [ ] (P1) #12 Fix the login redirect\n- [ ] (P0) deploy (after #12)\n")
	if view.blocked != 1 || len(view.items) != 1 || !strings.Contains(view.items[0], "#12 Fix") {
		t.Fatalf("(after #12) did not wait for the item of issue #12: %+v", view)
	}
	view = snapshotOf(t, "- [x] (P1) #12 Fix the login redirect\n- [ ] (P0) deploy (after #12)\n")
	if view.blocked != 0 || len(view.items) != 1 {
		t.Fatalf("(after #12) kept waiting after the issue's item was ticked: %+v", view)
	}
	view = snapshotOf(t, "- [ ] org/web#31 Fix the header\n- [ ] ship (after org/web#31)\n- [ ] tidy up (after #999)\n")
	if view.blocked != 1 || len(view.items) != 2 {
		t.Fatalf("a qualified issue reference must wait and one that matches nothing must be ignored: %+v", view)
	}
}

func TestChecklistLinesInsideACodeFenceAreNotItems(t *testing.T) {
	view := snapshotOf(t, "# how to write items\n```markdown\n- [ ] an example, not a task\n```\n- [ ] the real task\n")
	if view.total != 1 || len(view.items) != 1 || view.items[0] != "the real task" {
		t.Fatalf("a checklist line inside a code fence was read as a task: %+v", view)
	}
	view = snapshotOf(t, "- write the parser\n~~~\n- [ ] fenced example\n~~~\n- ship it\n")
	if !view.plain || view.total != 2 {
		t.Fatalf("a fenced checkbox made a plain list a checkbox list: %+v", view)
	}
}

func TestAFencedExampleNeverClosesAnIssue(t *testing.T) {
	cfg, project := queueTrustSandbox(t, true)
	calls := fakeGhCLI(t, "[]")
	queuePath := writeQueueFile(t, project, "# q\n```markdown\n- [ ] #12 an example item\n```\n- [ ] #13 the real task\n")
	syncDoneIssues(cfg, queuePath, project)
	writeQueueFile(t, project, "# q\n```markdown\n- [x] #12 an example item\n```\n- [ ] #13 the real task\n")
	syncDoneIssues(cfg, queuePath, project)
	if closes := ghLoggedCloses(t, calls, 0); len(closes) != 0 {
		t.Fatalf("ticking an example inside a code fence closed an issue: %q", closes)
	}
}
