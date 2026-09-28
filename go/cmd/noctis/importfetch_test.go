package main

import (
	"strings"
	"testing"
)

const x4OwnerIssues = `[{"number":12,"title":"Fix the login redirect","labels":[],"author":{"login":"owner"}},{"number":14,"title":"Rename the build box","labels":[],"author":{"login":"owner"}}]`

func TestQueueImportAsksGhForTheIssuesOfEachAuthor(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	calls := fakeGhCLI(t, mixedAuthorIssues)
	queueImportOutput(t, "queue", "import", "--cwd", project)
	lists := ghLoggedCalls(calls, "list")
	if len(lists) != 1 || !strings.Contains(lists[0]+" ", " --author @me ") {
		t.Fatalf("queue import must ask gh for your own open issues, not every author's first --limit: %q", lists)
	}
	queueImportOutput(t, "queue", "import", "--cwd", t.TempDir(), "--author", "@Stranger, owner")
	lists = ghLoggedCalls(calls, "list")[1:]
	if len(lists) != 2 || !strings.Contains(lists[0]+" ", " --author stranger ") || !strings.Contains(lists[1]+" ", " --author owner ") {
		t.Fatalf("queue import --author @Stranger,owner must ask gh once for each author's open issues: %q", lists)
	}
}

func TestQueueImportSaysWhatGhListedRatherThanAllOpenIssues(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	fakeGhCLI(t, "[]")
	if output := strings.TrimSpace(queueImportOutput(t, "queue", "import", "--cwd", project)); output != T("queue.importEmpty", "@me") {
		t.Errorf("with no open issue of yours, queue import said %q; want %q", output, T("queue.importEmpty", "@me"))
	}
	fakeGhCLI(t, x4OwnerIssues)
	writeQueueFile(t, project, "# q\n- [ ] #12 Fix the login redirect\n- [ ] #14 Rename the build box\n")
	if output := strings.TrimSpace(queueImportOutput(t, "queue", "import", "--cwd", project)); output != T("queue.importNone", 2, "owner", "TASKS.md") {
		t.Errorf("with both of your open issues in the file, queue import said %q; want %q", output, T("queue.importNone", 2, "owner", "TASKS.md"))
	}
	output := queueImportOutput(t, "queue", "import", "--cwd", t.TempDir(), "--limit", "2")
	if !strings.Contains(output, T("queue.importLimit", "2", "@me")) {
		t.Errorf("queue import --limit 2 got two issues back and did not say that older ones may not have been listed:\n%s", output)
	}
}
