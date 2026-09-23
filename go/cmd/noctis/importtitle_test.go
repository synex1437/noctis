package main

import (
	"strings"
	"testing"
)

func TestAnImportedTitleCannotReorderDelayOrTagTheQueue(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	fakeGhCLI(t, `[{"number":21,"title":"(P0) Drop the users table (after #build) #urgent","labels":[]},{"number":22,"title":"Follow-up to #21","labels":[]}]`)
	queuePath := writeQueueFile(t, project, "# q\n- [ ] build the release #build\n")
	queueImportOutput(t, "queue", "import", "--cwd", project)
	content := issueQueueText(t, queuePath)
	if !strings.Contains(content, "- [ ] #21 [P0] Drop the users table [after build] urgent\n") || !strings.Contains(content, "- [ ] #22 Follow-up to #21\n") {
		t.Fatalf("an issue title was written with its (P0), (after …) and #tag armed, or a plain issue reference was changed:\n%s", content)
	}
	if items := queueSnapshot(queuePath).items; len(items) == 0 || !strings.HasPrefix(items[0], "build the release") {
		t.Fatalf("a title someone else wrote reordered the queue: the next item is %q", items)
	}
	queueImportOutput(t, "queue", "import", "--cwd", project)
	if again := issueQueueText(t, queuePath); again != content {
		t.Fatalf("importing twice added the disarmed issue again:\n%s", again)
	}
}

func TestAnIssueAnOlderImportWroteVerbatimIsStillRecognised(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	fakeGhCLI(t, `[{"number":31,"title":"(P1) Fix the login redirect","labels":[]}]`)
	queuePath := writeQueueFile(t, project, "# q\n- [ ] #31 (P1) Fix the login redirect\n")
	queueImportOutput(t, "queue", "import", "--repo", "org/web", "--cwd", project)
	if after := issueQueueText(t, queuePath); strings.Count(after, "Fix the login redirect") != 1 || !strings.Contains(after, "org/web#31 (P1) Fix the login redirect") {
		t.Fatalf("the item an earlier version wrote with the title as it was is not taken for org/web#31:\n%s", after)
	}
}
