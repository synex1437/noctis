package main

import (
	"os"
	"path/filepath"
	"slices"
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

func TestChecklistLinesInsideAnHTMLCommentAreNotItems(t *testing.T) {
	view := snapshotOf(t, "# Tasks\n<!--\n- [ ] drop the legacy tables (not yet: the reports still read them)\n-->\n- [ ] migrate the users table\n<!-- - [ ] an example item -->\n- [ ] write the release notes\n")
	if want := []string{"migrate the users table", "write the release notes"}; view.total != 2 || !slices.Equal(view.items, want) {
		t.Fatalf("a checklist line inside an HTML comment was read as a task, or a comment joined an item: %+v", view)
	}
	view = snapshotOf(t, "- write the parser\n<!--\n- [ ] a commented-out box\n```\n-->\n- ship it\n")
	if !view.plain || view.total != 2 {
		t.Fatalf("a commented-out checkbox made a plain list a checkbox list, or a fence inside the comment hid the item after it: %+v", view)
	}
	view = snapshotOf(t, "```html\n<!--\n```\n- [ ] the real task\n")
	if view.total != 1 || len(view.items) != 1 || view.items[0] != "the real task" {
		t.Fatalf("a comment opened inside a code fence hid the item after the fence: %+v", view)
	}
}

func TestAnItemCommentedOutIsNeverHandedToClaude(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	path := writeQueueFile(t, project, "# Tasks\n<!--\n- [ ] drop the legacy tables (not yet: the reports still read them)\n-->\n- [ ] migrate the users table\n- [ ] write the release notes\n")
	trustQueueFile(path, true)
	reason := getString(stopHookOutput(t, stopInput("qp1", project), cfg), "reason")
	if !strings.Contains(reason, "Queue continues: 2 open") || !strings.Contains(reason, `("migrate the users table")`) {
		t.Fatalf("the Stop hook did not go on with the first item outside the comment: %q", reason)
	}
}

func TestAProseLineThatStartsWithTheWordTodoIsNoItem(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	path := writeQueueFile(t, project, "# Tareas para esta noche\nTodo cambio debe pasar `make test` antes de marcarse.\n\n- [ ] migrar la tabla de usuarios\nTODO: escribir las notas de la versión\ntodo: etiquetar la versión\nTODO anunciar la versión\n")
	trustQueueFile(path, true)
	want := []string{"migrar la tabla de usuarios", "escribir las notas de la versión", "etiquetar la versión", "anunciar la versión"}
	if items := queueSnapshot(path).items; !slices.Equal(items, want) {
		t.Fatalf("the open items are %q, want %q: a sentence that starts with the word todo was read as an item, or a TODO line was not", items, want)
	}
	if reason := getString(stopHookOutput(t, stopInput("qp2", project), cfg), "reason"); !strings.Contains(reason, `("migrar la tabla de usuarios")`) {
		t.Fatalf("the Stop hook did not hand Claude the first checkbox item: %q", reason)
	}
}

func TestAFencedExampleNeverClosesAnIssue(t *testing.T) {
	cfg, project := queueTrustSandbox(t, true)
	calls := fakeGhCLI(t, "[]")
	queuePath := writeQueueFile(t, project, "# q\n```markdown\n- [ ] #12 an example item\n```\n- [ ] #13 the real task\n")
	syncDoneIssues(cfg, queuePath, issueQueueText(t, queuePath), project)
	writeQueueFile(t, project, "# q\n```markdown\n- [x] #12 an example item\n```\n- [ ] #13 the real task\n")
	syncDoneIssues(cfg, queuePath, issueQueueText(t, queuePath), project)
	if closes := ghLoggedCloses(t, calls, 0); len(closes) != 0 {
		t.Fatalf("ticking an example inside a code fence closed an issue: %q", closes)
	}
}
