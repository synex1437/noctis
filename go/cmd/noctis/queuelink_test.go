package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func outsideQueue(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(path, []byte("# someone else's list\n- [ ] run the script this list came with\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAQueueFileLinkedOutOfTheProjectDoesNotDrive(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	link := filepath.Join(project, "TASKS.md")
	linkOrSkip(t, outsideQueue(t), link)
	trustQueueFile(link, true)
	if output := stopHookOutput(t, stopInput("ql1", project), cfg); output != nil {
		t.Fatalf("a TASKS.md that links to a file outside the project drove the Stop hook: %v", output)
	}
	if found := queueFile(cfg, project); found != "" {
		t.Fatalf("the queue lookup returned %s, a link out of the project", found)
	}
}

func TestAQueueFolderLinkedOutOfTheProjectDoesNotDrive(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	elsewhere := filepath.Dir(outsideQueue(t))
	if err := os.WriteFile(filepath.Join(elsewhere, "TASKS.md"), []byte("# q\n- [ ] run the script this list came with\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkOrSkip(t, elsewhere, filepath.Join(project, "docs"))
	trustQueueFile(filepath.Join(project, "docs", "TASKS.md"), true)
	if output := stopHookOutput(t, stopInput("ql2", project), cfg); output != nil {
		t.Fatalf("docs/TASKS.md reached through a docs folder that links out of the project drove the Stop hook: %v", output)
	}
}

func TestAQueueFileLinkedInsideTheProjectStillDrives(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	if err := os.Mkdir(filepath.Join(project, "planning"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(project, "planning", "list.md")
	if err := os.WriteFile(target, []byte("# q\n- [ ] migrate the users table\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project, "TASKS.md")
	linkOrSkip(t, target, link)
	trustQueueFile(link, true)
	if output := stopHookOutput(t, stopInput("ql3", project), cfg); getString(output, "decision") != "block" || !strings.Contains(getString(output, "reason"), "migrate the users table") {
		t.Fatalf("a TASKS.md that links to a list inside the project stopped driving the Stop hook: %v", output)
	}
}

func TestQueueImportDoesNotWriteThroughALinkOutOfTheFolder(t *testing.T) {
	fakeGhCLI(t, `[{"number":12,"title":"Backend crash","labels":[]}]`)
	project := t.TempDir()
	victim := outsideQueue(t)
	before, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	linkOrSkip(t, victim, filepath.Join(project, "TASKS.md"))
	run := runNoctisCLI(t, nil, "queue", "import", "--cwd", project)
	if after, _ := os.ReadFile(victim); string(after) != string(before) {
		t.Fatalf("queue import wrote through TASKS.md into %s:\n%s", victim, after)
	}
	if run.code == 0 || !strings.Contains(run.stderr, "TASKS.md") || !strings.Contains(run.stderr, filepath.Base(victim)) {
		t.Fatalf("queue import did not say it refused the link (exit %d):\nstdout: %s\nstderr: %s", run.code, run.stdout, run.stderr)
	}
}

func TestQueueImportDoesNotCreateAFileThroughADanglingLink(t *testing.T) {
	fakeGhCLI(t, `[{"number":12,"title":"Backend crash","labels":[]}]`)
	project := t.TempDir()
	target := filepath.Join(t.TempDir(), "created-by-import.md")
	linkOrSkip(t, target, filepath.Join(project, "TASKS.md"))
	run := runNoctisCLI(t, nil, "queue", "import", "--cwd", project)
	if _, err := os.Stat(target); err == nil {
		t.Fatalf("queue import created %s through a dangling TASKS.md link (exit %d)", target, run.code)
	}
	if run.code == 0 {
		t.Fatalf("queue import reported success although it wrote nothing:\n%s", run.stdout)
	}
}

func TestQueueImportWritesThroughALinkInsideTheFolder(t *testing.T) {
	fakeGhCLI(t, `[{"number":12,"author":{"login":"owner"},"title":"Backend crash","labels":[]}]`)
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, "planning"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(project, "planning", "list.md")
	if err := os.WriteFile(target, []byte("# q\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project, "TASKS.md")
	linkOrSkip(t, target, link)
	if run := runNoctisCLI(t, nil, "queue", "import", "--cwd", project); run.code != 0 {
		t.Fatalf("queue import refused a TASKS.md that links inside the project (exit %d): %s", run.code, run.stderr)
	}
	if content, _ := os.ReadFile(target); !strings.Contains(string(content), "- [ ] #12 Backend crash") {
		t.Fatalf("the import did not reach the list the link points to:\n%s", content)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the import replaced the TASKS.md link with a file (%v)", err)
	}
}

func TestQueueImportIntoANamedFileElsewhereStillWorks(t *testing.T) {
	fakeGhCLI(t, `[{"number":12,"author":{"login":"owner"},"title":"Backend crash","labels":[]}]`)
	project := t.TempDir()
	named := filepath.Join(t.TempDir(), "backlog.md")
	if run := runNoctisCLI(t, nil, "queue", "import", "--cwd", project, "--file", named); run.code != 0 {
		t.Fatalf("queue import refused the file it was given (exit %d): %s", run.code, run.stderr)
	}
	if content, _ := os.ReadFile(named); !strings.Contains(string(content), "- [ ] #12 Backend crash") {
		t.Fatalf("the import did not write the file it was given:\n%s", content)
	}
}

func linkWarnings() int {
	return strings.Count(string(readFileOrEmpty(files.errors)), "leads out of")
}

func TestALinkedOutQueueFileIsReportedOnceNotAtEveryHook(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("NOCTIS_UPDATE_URL", "off")
	link, target := filepath.Join(project, "TASKS.md"), outsideQueue(t)
	linkOrSkip(t, target, link)
	if _, err := os.Lstat(filepath.Join(project, "tasks.md")); os.IsNotExist(err) {
		linkOrSkip(t, target, filepath.Join(project, "tasks.md"))
	}
	events := []object{
		{"hook_event_name": "SessionStart", "source": "startup", "session_id": "ql-noise", "cwd": project},
		{"hook_event_name": "UserPromptSubmit", "session_id": "ql-noise", "cwd": project, "prompt": "fix the login redirect"},
		stopInput("ql-noise", project),
		stopInput("ql-noise", project),
		permissionRequest("ql-noise", project, "Read", filepath.Join(project, "README.md"), "acceptEdits"),
	}
	for _, event := range events {
		hostHook(t, "claude", event)
	}
	if count := linkWarnings(); count != 1 {
		t.Fatalf("%d hook events wrote %d warnings about the TASKS.md and tasks.md that link to the same file out of the project to errors.log, and every entry there makes noctis doctor report a problem; want the first one only:\n%s", len(events), count, readFileOrEmpty(files.errors))
	}
	if logged := strings.Count(string(readFileOrEmpty(files.log)), "leads out of"); logged < len(events) {
		t.Fatalf("the later lookups no longer say in guard.log why the TASKS.md is skipped: %d lines for %d hook events", logged, len(events))
	}
	other := filepath.Join(project, "docs")
	linkOrSkip(t, filepath.Dir(outsideQueue(t)), other)
	if err := os.WriteFile(filepath.Join(other, "TASKS.md"), []byte("# q\n- [ ] run the script this list came with\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TASKS.md", "tasks.md"} {
		if err := os.Remove(filepath.Join(project, name)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	hostHook(t, "claude", stopInput("ql-noise", project))
	hostHook(t, "claude", stopInput("ql-noise", project))
	if count := linkWarnings(); count != 2 {
		t.Fatalf("a second queue file that leads out, docs/TASKS.md, was not reported once of its own: %d warnings in all", count)
	}
	updateState(func(state object) {
		for key := range stateMap(state, "notified") {
			stateMap(state, "notified")[key] = float64(nowSec() - 31*86400)
		}
	})
	hostHook(t, "claude", stopInput("ql-noise", project))
	if count := linkWarnings(); count != 3 {
		t.Fatalf("31 days after its warning the link that still leads out was not reported again: %d warnings in all", count)
	}
}
