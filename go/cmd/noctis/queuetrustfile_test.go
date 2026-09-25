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

func TestQueueStatusAndTrustNameAnAfterReferenceThatMatchesNoItemAndLeaveFreeTextAlone(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	queue := filepath.Join(project, "TASKS.md")
	cliWrite(t, queue, []byte("- [ ] (P0) fix the login redirect #auth\n- [ ] migrate the users (after #auth, #Atuh)\n- [ ] seed the demo data (after #atuh)\n- [ ] deploy (after 42)\n- [ ] tidy up #tidy (after #tidy, 1)\n- [ ] clean up the logs (after the release)\n- [ ] Login fails (after password reset)\n- [ ] ship it (after (see #12) the review)\n- [ ] order the beans #café\n- [ ] brew (after #café)\n"))
	queueCLI := func(argv ...string) cliRun {
		return startNoctisCLIAt(t, home, "", nil, append([]string{"queue"}, argv...)...)()
	}
	named := func(run cliRun) bool {
		if run.code != 0 || !strings.Contains(run.stdout, "#Atuh, 42, #café.") || strings.Count(run.stdout, "⚠") != 1 || strings.Count(run.stdout, "#Atuh") != 1 {
			return false
		}
		for _, silent := range []string{"#atuh", "#auth", "#tidy", "release", "password", "#12", "review"} {
			if strings.Contains(run.stdout, silent) {
				return false
			}
		}
		return true
	}

	if run := queueCLI("status", "--file", "TASKS.md", "--cwd", project); !named(run) || !strings.Contains(run.stdout, "10 open") {
		t.Fatalf("queue status on a file nobody trusted does not name #Atuh, 42 and #café, the (after …) references no item matches, once each and as written, or names free text in parentheses:\n%s", run)
	}
	if run := queueCLI("trust", "--file", "TASKS.md", "--cwd", project); !named(run) || !strings.Contains(run.stdout, queue) {
		t.Fatalf("queue trust does not name #Atuh, 42 and #café, the (after …) references no item matches, once each:\n%s", run)
	}
	if run := queueCLI("status", "--file", "TASKS.md", "--cwd", project); !named(run) || !strings.Contains(run.stdout, queue) {
		t.Fatalf("queue status on a trusted file does not name #Atuh, 42 and #café, the (after …) references no item matches, once each:\n%s", run)
	}
	cliWrite(t, queue, []byte("- [ ] (P0) fix the login redirect #auth\n- [ ] migrate the users (after #auth)\n- [ ] deploy (after 2)\n"))
	if run := queueCLI("status", "--file", "TASKS.md", "--cwd", project); run.code != 0 || strings.Contains(run.stdout, "⚠") {
		t.Fatalf("queue status warns about (after …) references that all match an item:\n%s", run)
	}
}
