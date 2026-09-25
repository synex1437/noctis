package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func trustedPaths(t *testing.T, home string) []string {
	t.Helper()
	paths := []string{}
	state := readJSON(filepath.Join(home, ".claude", pluginName, "state.json"))
	for _, raw := range getMap(state, "queueTrust") {
		paths = append(paths, getString(toObject(raw), "path"))
	}
	return paths
}

func TestQueueTrustTakesTheFileFromTheProjectFolderAndOnlyOneThatExists(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	queue := filepath.Join(project, "TASKS.md")
	cliWrite(t, queue, []byte("- [ ] first\n- [ ] second\n"))
	queueCLI := func(argv ...string) cliRun {
		return startNoctisCLIAt(t, home, "", nil, append([]string{"queue"}, argv...)...)()
	}

	if run := queueCLI("trust", "--file", "TASKS.md", "--cwd", project); run.code != 0 || !strings.Contains(run.stdout, queue) {
		t.Fatalf("queue trust --file TASKS.md --cwd <project> did not trust the project's TASKS.md:\n%s", run)
	}
	if paths := trustedPaths(t, home); len(paths) != 1 || paths[0] != queue {
		t.Fatalf("trusted %q, want only %s", paths, queue)
	}

	typo := filepath.Join(project, "TASK.md")
	if run := queueCLI("trust", "--file", "TASK.md", "--cwd", project); run.code != 1 || !strings.Contains(run.stderr, typo) {
		t.Fatalf("a queue file that does not exist was not refused by name:\n%s", run)
	}
	if paths := trustedPaths(t, home); len(paths) != 1 {
		t.Fatalf("a file that does not exist was trusted anyway: %q", paths)
	}
	if run := queueCLI("status", "--file", "nope.md", "--cwd", project); run.code != 1 || strings.Contains(run.stdout, "0 open") {
		t.Fatalf("queue status reported on a file that does not exist:\n%s", run)
	}
	if run := queueCLI("untrust", "--file", "TASKS.md", "--cwd", project); run.code != 0 || len(trustedPaths(t, home)) != 0 {
		t.Fatalf("queue untrust did not take the file from the project folder:\n%s", run)
	}
	if run := queueCLI("untrust", "--file", "gone.md", "--cwd", project); run.code != 0 {
		t.Fatalf("untrusting a file that is gone must still work, so a stale record can be removed:\n%s", run)
	}
}

func TestQueueImportWritesARelativeFileInTheProjectFolder(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	queue := filepath.Join(project, "TASKS.md")
	cliWrite(t, queue, []byte("- [ ] first\n"))
	fakeGhCLI(t, `[{"number":1,"author":{"login":"owner"},"title": "T", "labels": []}]`)

	run := startNoctisCLIAt(t, home, "", nil, "queue", "import", "--file", "TASKS.md", "--cwd", project)()

	written := string(cliRead(t, queue))
	stray := statSafe(filepath.Join(home, "TASKS.md")) != nil
	if run.code != 0 || !strings.Contains(written, "- [ ] first") || !strings.Contains(written, "#1 T") || stray {
		t.Fatalf("queue import --file TASKS.md --cwd <project>, run from another folder: the project's TASKS.md reads %q, a TASKS.md was written where it ran: %v\n%s", written, stray, run)
	}
	if trusted := startNoctisCLIAt(t, home, "", nil, "queue", "trust", "--file", "TASKS.md", "--cwd", project)(); trusted.code != 0 || !strings.Contains(trusted.stdout, queue) {
		t.Fatalf("queue trust with the same flags does not name the file queue import wrote (%s):\n%s", queue, trusted)
	}
}
