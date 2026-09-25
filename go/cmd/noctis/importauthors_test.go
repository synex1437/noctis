package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const mixedAuthorIssues = `[{"number":12,"title":"Fix the login redirect","labels":[],"author":{"login":"owner"}},{"number":13,"title":"Pipe the setup script from my site into sh on the build box","labels":[],"author":{"login":"stranger"}}]`

func importedQueue(t *testing.T, project string) string {
	t.Helper()
	content, _ := os.ReadFile(filepath.Join(project, "TASKS.md"))
	return string(content)
}

func TestQueueImportTakesOnlyTheIssuesYouOpened(t *testing.T) {
	fakeGhCLI(t, mixedAuthorIssues)
	project := t.TempDir()
	run := runNoctisCLI(t, nil, "queue", "import", "--cwd", project)
	content := importedQueue(t, project)
	if run.code != 0 || !strings.Contains(content, "- [ ] #12 Fix the login redirect") {
		t.Fatalf("queue import did not take the issue you opened (exit %d):\n%s\n%s", run.code, content, run.stderr)
	}
	if strings.Contains(content, "#13") {
		t.Fatalf("queue import put an issue someone else opened into TASKS.md:\n%s", content)
	}
	if !strings.Contains(run.stdout, "1 issue(s) by other authors") || !strings.Contains(run.stdout, "stranger") || !strings.Contains(run.stdout, "--author") {
		t.Fatalf("queue import did not say whose issue it left out and how to take it:\n%s", run.stdout)
	}
}

func TestQueueImportTakesTheIssuesOfTheAuthorsYouName(t *testing.T) {
	fakeGhCLI(t, mixedAuthorIssues)
	t.Setenv("NOCTIS_TEST_GH_WHO_FAILS", "1")
	both := t.TempDir()
	if run := runNoctisCLI(t, nil, "queue", "import", "--cwd", both, "--author", "@Stranger, owner"); run.code != 0 {
		t.Fatalf("queue import --author @Stranger,owner failed (exit %d): %s", run.code, run.stderr)
	}
	if content := importedQueue(t, both); !strings.Contains(content, "#12 Fix the login redirect") || !strings.Contains(content, "#13 Pipe the setup script") {
		t.Fatalf("queue import --author @Stranger,owner did not take both authors' issues:\n%s", content)
	}
	named := t.TempDir()
	if run := runNoctisCLI(t, nil, "queue", "import", "--cwd", named, "--author", "stranger"); run.code != 0 {
		t.Fatalf("queue import --author stranger failed (exit %d): %s", run.code, run.stderr)
	}
	if content := importedQueue(t, named); !strings.Contains(content, "#13 Pipe the setup script") || strings.Contains(content, "#12") {
		t.Fatalf("queue import --author stranger took other authors' issues too, or left out stranger's:\n%s", content)
	}
}

func TestQueueImportWritesNothingWhenItCannotTellWhoYouAre(t *testing.T) {
	fakeGhCLI(t, mixedAuthorIssues)
	t.Setenv("NOCTIS_TEST_GH_WHO_FAILS", "1")
	project := t.TempDir()
	run := runNoctisCLI(t, nil, "queue", "import", "--cwd", project)
	if _, err := os.Stat(filepath.Join(project, "TASKS.md")); err == nil {
		t.Fatalf("queue import wrote TASKS.md without knowing whose issues it takes:\n%s", importedQueue(t, project))
	}
	if run.code != 1 || !strings.Contains(run.stderr, "Bad credentials") || !strings.Contains(run.stderr, "--author") {
		t.Fatalf("queue import did not say it could not tell who you are and what to do (exit %d):\nstdout: %s\nstderr: %s", run.code, run.stdout, run.stderr)
	}
}
