package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const x5ImportIssues = `[{"number":21,"title":"Speed up the parser","labels":[],"author":{"login":"owner"}},{"number":22,"title":"Drop the old flag","labels":[],"author":{"login":"owner"}}]`

func TestQueueImportLeavesAFileWithNothingNewAsItIs(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	fakeGhCLI(t, x5ImportIssues)
	original := "\uFEFF# q\r\n- [ ] #21 Speed up the parser\r\n- [ ] #22 Drop the old flag\r\n"
	queuePath := writeQueueFile(t, project, original)
	old := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := os.Chtimes(queuePath, old, old); err != nil {
		t.Fatal(err)
	}
	queueImportOutput(t, "queue", "import", "--cwd", project)
	if content := issueQueueText(t, queuePath); content != original {
		t.Errorf("an import with nothing new changed the file from %q to %q", original, content)
	}
	if info, err := os.Stat(queuePath); err != nil || !info.ModTime().Equal(old) {
		t.Errorf("an import with nothing new wrote the file again (modified %v)", info.ModTime())
	}
}

func TestQueueImportKeepsTheBOMAndTheLineEndingOfTheFile(t *testing.T) {
	queueTrustSandbox(t, false)
	fakeGhCLI(t, x5ImportIssues)
	for _, file := range []struct{ original, want string }{
		{"\uFEFF# q\r\n- [ ] #21 Speed up the parser\r\n", "\uFEFF# q\r\n- [ ] #21 Speed up the parser\r\n\r\n## GitHub issues\r\n- [ ] #22 Drop the old flag\r\n"},
		{"# q\r\n- [ ] #21 Speed up the parser", "# q\r\n- [ ] #21 Speed up the parser\r\n\r\n## GitHub issues\r\n- [ ] #22 Drop the old flag\r\n"},
		{"\uFEFF", "\uFEFF# TASKS\n\n## GitHub issues\n- [ ] #21 Speed up the parser\n- [ ] #22 Drop the old flag\n"},
		{"# q\n- [ ] #21 Speed up the parser\n", "# q\n- [ ] #21 Speed up the parser\n\n## GitHub issues\n- [ ] #22 Drop the old flag\n"},
	} {
		project := t.TempDir()
		queuePath := writeQueueFile(t, project, file.original)
		queueImportOutput(t, "queue", "import", "--cwd", project)
		if content := issueQueueText(t, queuePath); content != file.want {
			t.Errorf("importing into %q gave %q; want %q", file.original, content, file.want)
		}
	}
}

// identityNow returns the FileInfo of the file at path with its identity read now. On Windows
// os.Stat notes only the path, and os.SameFile reads the identity later from whatever file has the
// name by then, so a FileInfo taken before the file was replaced would match one taken after.
func identityNow(t *testing.T, path string) os.FileInfo {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestQueueImportReplacesTheFileInOneStep(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	fakeGhCLI(t, x5ImportIssues)
	queuePath := writeQueueFile(t, project, "# q\n- [ ] #21 Speed up the parser\n")
	before := identityNow(t, queuePath)
	queueImportOutput(t, "queue", "import", "--cwd", project)
	after := identityNow(t, queuePath)
	if os.SameFile(before, after) {
		t.Errorf("the import rewrote %s in place, so a hook reading it meanwhile can see it cut short; it must write a new file and rename it over the old one", filepath.Base(queuePath))
	}
	if after.Mode().Perm() != before.Mode().Perm() {
		t.Errorf("the import changed the mode of the file from %v to %v", before.Mode().Perm(), after.Mode().Perm())
	}
	noTempFilesBeside(t, queuePath)

	fresh := t.TempDir()
	reference := filepath.Join(fresh, "reference.md")
	if err := os.WriteFile(reference, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	queueImportOutput(t, "queue", "import", "--cwd", fresh)
	if got, want := fileMode(t, filepath.Join(fresh, "TASKS.md")), fileMode(t, reference); got != want {
		t.Errorf("a TASKS.md the import creates is a project file and must get the usual mode %v, got %v", want, got)
	}
	noTempFilesBeside(t, filepath.Join(fresh, "TASKS.md"))
}
