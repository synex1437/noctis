package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestQueueImportDoesNotTurnTheItemsOfAListWithoutCheckboxesIntoText(t *testing.T) {
	fakeGhCLI(t, `[{"number":12,"author":{"login":"owner"},"title":"Backend crash","labels":[]}]`)
	project := t.TempDir()
	original := "# TASKS\n- Fix the login redirect loop on the settings page\n- Add a CSV export to the reports screen\n- Update the install docs for Windows\n"
	queuePath := writeQueueFile(t, project, original)
	run := runNoctisCLI(t, nil, "queue", "import", "--cwd", project)
	if content := issueQueueText(t, queuePath); content != original {
		entries, _ := parseQueueEntries(content)
		t.Fatalf("queue import added a checkbox item to a list without checkboxes, so the queue holds %d item(s) instead of the list's 3 and the issue (exit %d, %q):\n%s", len(entries), run.code, run.stdout, content)
	}
	if want := fmt.Sprintf(catalogFor("en")["queue.importPlain"], "TASKS.md", 3); run.code != 1 || !strings.Contains(run.stderr, want) {
		t.Fatalf("queue import into a list without checkboxes ended with exit %d and\n%s\nwant exit 1 and %q", run.code, run.stderr, want)
	}
}

func TestQueueImportThatAddsNoItemStillWorksOnAListWithoutCheckboxes(t *testing.T) {
	fakeGhCLI(t, `[{"number":12,"author":{"login":"owner"},"title":"Backend crash","labels":[]}]`)
	project := t.TempDir()
	queuePath := writeQueueFile(t, project, "# TASKS\n- Fix the login redirect loop on the settings page\n- #12 Backend crash\n")
	if run := runNoctisCLI(t, nil, "queue", "import", "--cwd", project); run.code != 0 {
		t.Fatalf("an import with nothing new failed on a list without checkboxes (exit %d):\n%s", run.code, run.stderr)
	}
	if run := runNoctisCLI(t, nil, "queue", "import", "--repo", "org/backend", "--cwd", project); run.code != 0 {
		t.Fatalf("an import that only names the repository of a bare item failed on a list without checkboxes (exit %d):\n%s", run.code, run.stderr)
	}
	if content, want := issueQueueText(t, queuePath), "# TASKS\n- Fix the login redirect loop on the settings page\n- org/backend#12 Backend crash\n"; content != want {
		t.Fatalf("the list without checkboxes reads\n%s\nwant\n%s", content, want)
	}
}
