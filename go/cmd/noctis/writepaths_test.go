package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOneGuardPassResolvesAWritePathOnce(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "notes.md")
	symlinkOrSkip(t, filepath.Join(dir, "first.md"), link)
	forget := memoizeWritePaths()
	first := resolvedWritePath(link)
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, filepath.Join(dir, "second.md"), link)
	if again := resolvedWritePath(link); again != first {
		t.Errorf("one guard pass resolved the same path twice: %s, then %s", first, again)
	}
	forget()
	if after := resolvedWritePath(link); after == first || after != resolveWritePath(link) {
		t.Errorf("after the guard pass the path resolves to %s, want the link's new target", after)
	}
}

func TestALinkMovedBetweenTwoEditsIsJudgedByItsNewTarget(t *testing.T) {
	cfg, _, frontend, test := testGuardSandbox(t)
	elsewhere := filepath.Join(t.TempDir(), "users_test.go")
	if err := os.WriteFile(elsewhere, []byte("package users\n\nfunc TestMigrate(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "linked_test.go")
	symlinkOrSkip(t, elsewhere, link)
	if output := hookOutput(t, onPreToolUse, testFileEdit("wp1", frontend, link), cfg); output != nil {
		t.Fatalf("an edit through a link to a test outside the project was refused: %v", output)
	}
	if writePathMemo != nil {
		t.Fatal("the paths resolved for one edit were kept after its hook")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, test, link)
	if output := hookOutput(t, onPreToolUse, testFileEdit("wp1", frontend, link), cfg); !deniedTool(output) {
		t.Fatalf("an edit through the same link, moved onto the project's test, went through: %v", output)
	}
}
