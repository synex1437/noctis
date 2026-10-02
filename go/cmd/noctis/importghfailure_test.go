package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ghListComplaint = "GraphQL: Could not resolve to a Repository with the name 'acme/apii'."

func fakeGhThatCannotList(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	name, script := "gh", "#!/bin/sh\nif [ \"$1\" = issue ] && [ \"$2\" = list ]; then echo \""+ghListComplaint+"\" >&2; exit 1; fi\necho owner\n"
	if isWindows {
		name, script = "gh.cmd", "@echo off\r\nif \"%~1\"==\"issue\" if \"%~2\"==\"list\" (echo "+ghListComplaint+" 1>&2& exit /b 1)\r\necho owner\r\n"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestQueueImportSaysWhyGhCouldNotListTheIssues(t *testing.T) {
	fakeGhThatCannotList(t)
	project := t.TempDir()
	run := runNoctisCLI(t, nil, "queue", "import", "--cwd", project, "--repo", "acme/apii")
	if run.code != 1 || !strings.Contains(run.stderr, ghListComplaint) {
		t.Fatalf("gh issue list failed with %q, but queue import said (exit %d):\n%s", ghListComplaint, run.code, run.stderr)
	}
	if strings.Contains(run.stderr, "installed and logged in") {
		t.Errorf("gh is installed and gave its reason, yet queue import asks whether it is installed and logged in:\n%s", run.stderr)
	}
	if _, err := os.Stat(filepath.Join(project, "TASKS.md")); err == nil {
		t.Errorf("queue import wrote TASKS.md although gh listed nothing")
	}
}

func TestQueueImportAsksWhetherGhIsInstalledWhenItIsNotFound(t *testing.T) {
	run := runNoctisCLI(t, map[string]string{"PATH": t.TempDir()}, "queue", "import", "--cwd", t.TempDir())
	if run.code != 1 || !strings.Contains(run.stderr, "installed and logged in") {
		t.Fatalf("with no gh on the PATH queue import said (exit %d):\n%s", run.code, run.stderr)
	}
}
