package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func touchedRepo(t *testing.T) string {
	t.Helper()
	repo := workspaceRepo(t)
	for i := 0; i < 50; i++ {
		writeRepoFile(t, repo, fmt.Sprintf("f%02d.txt", i), fmt.Sprintf("file %d\n", i))
	}
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "fifty files")
	later := time.Now().Add(2 * time.Minute)
	for i := 0; i < 50; i++ {
		if err := os.Chtimes(filepath.Join(repo, fmt.Sprintf("f%02d.txt", i)), later, later); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

func indexLeftAlone(t *testing.T, repo, what string, run func()) {
	t.Helper()
	index := filepath.Join(repo, ".git", "index")
	past := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(index, past, past); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	run()
	info, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(past) || !bytes.Equal(before, after) {
		t.Fatalf("%s rewrote .git/index, so it took .git/index.lock: a git add or commit by another session or agent at that moment fails with \"File exists\"", what)
	}
}

func TestNoctisGitStatusLeavesTheIndexAlone(t *testing.T) {
	repo := touchedRepo(t)
	indexLeftAlone(t, repo, "the git status of a checkpoint", func() {
		if _, ok := gitStatusUncached(repo); !ok {
			t.Fatal("git status failed")
		}
	})
}

func TestAGitSnapshotLeavesTheIndexAlone(t *testing.T) {
	repo := touchedRepo(t)
	writeRepoFile(t, repo, "f07.txt", "changed by claude before the pause\n")
	writeRepoFile(t, repo, "f08.txt", "staged by claude before the pause\n")
	gitIn(t, repo, "add", "f08.txt")
	scratch := t.TempDir()
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, scratch)
	}
	var ref, hash string
	indexLeftAlone(t, repo, "a git snapshot", func() {
		ref, hash = gitSnapshot(repo, "locks")
	})
	if ref == "" || hash == "" {
		t.Fatal("no snapshot was taken")
	}
	if left, _ := os.ReadDir(scratch); len(left) > 0 {
		t.Fatalf("the snapshot left %d file(s) behind in the temporary folder, the first %q", len(left), left[0].Name())
	}
	if got := gitIn(t, repo, "show", hash+":f07.txt"); got != "changed by claude before the pause\n" {
		t.Fatalf("the snapshot lost the unstaged change: %q", got)
	}
	if got := gitIn(t, repo, "show", hash+"^2:f08.txt"); got != "staged by claude before the pause\n" {
		t.Fatalf("the snapshot's index commit lost the staged change: %q", got)
	}
	if status := gitIn(t, repo, "status", "--short"); !strings.Contains(status, " M f07.txt") || !strings.Contains(status, "M  f08.txt") {
		t.Fatalf("taking the snapshot changed the working tree or the index: %q", status)
	}
}

func TestAGitSnapshotWorksWhileAnotherGitCommandHoldsTheIndexLock(t *testing.T) {
	repo := touchedRepo(t)
	writeRepoFile(t, repo, "f07.txt", "changed by claude before the pause\n")
	lock := filepath.Join(repo, ".git", "index.lock")
	if err := os.WriteFile(lock, []byte("held by another git command"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref, hash := gitSnapshot(repo, "held")
	if ref == "" || hash == "" {
		t.Fatal("a checkpoint taken while another git command held .git/index.lock got no snapshot")
	}
	if content, err := os.ReadFile(lock); err != nil || string(content) != "held by another git command" {
		t.Fatalf("the other command's index.lock was touched: %q %v", content, err)
	}
	if got := gitIn(t, repo, "show", hash+":f07.txt"); got != "changed by claude before the pause\n" {
		t.Fatalf("the snapshot lost the unstaged change: %q", got)
	}
}

func TestAGitSnapshotInAWorktreeKeepsThatWorktreesStagedChanges(t *testing.T) {
	repo := touchedRepo(t)
	worktree := filepath.Join(t.TempDir(), "feature")
	gitIn(t, repo, "worktree", "add", "-q", "-b", "feature", worktree)
	writeRepoFile(t, worktree, "f09.txt", "staged in the worktree\n")
	gitIn(t, worktree, "add", "f09.txt")
	writeRepoFile(t, worktree, "f10.txt", "changed in the worktree\n")
	ref, hash := gitSnapshot(worktree, "worktree")
	if ref == "" || hash == "" {
		t.Fatal("no snapshot was taken in a worktree")
	}
	if got := gitIn(t, worktree, "show", hash+"^2:f09.txt"); got != "staged in the worktree\n" {
		t.Fatalf("the snapshot's index commit is not the worktree's index: %q", got)
	}
	if got := gitIn(t, worktree, "show", hash+":f10.txt"); got != "changed in the worktree\n" {
		t.Fatalf("the snapshot lost the worktree's unstaged change: %q", got)
	}
	if status := gitIn(t, repo, "status", "--short"); status != "" {
		t.Fatalf("a snapshot in the worktree touched the main checkout: %q", status)
	}
}

func TestAGitSnapshotCutOffAtItsTimeLimitLeavesNoTemporaryIndex(t *testing.T) {
	repo := workspaceRepo(t)
	marker := filepath.Join(t.TempDir(), "filter-finished")
	gitIn(t, repo, "config", "filter.slow.clean", fmt.Sprintf("sleep 3; echo finished > '%s'; cat", filepath.ToSlash(marker)))
	writeRepoFile(t, repo, ".git/info/attributes", "*.txt filter=slow\n")
	writeRepoFile(t, repo, "a.txt", "changed by claude before the pause\n")
	scratch := t.TempDir()
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, scratch)
	}
	limit := gitSnapshotTimeout
	gitSnapshotTimeout = time.Second
	t.Cleanup(func() { gitSnapshotTimeout = limit })
	started := time.Now()
	if ref, hash := gitSnapshot(repo, "cutoff"); ref != "" || hash != "" {
		t.Fatalf("a snapshot whose clean filter takes 3 seconds finished within its 1-second limit: %s %s", ref, hash)
	}
	time.Sleep(time.Until(started.Add(5 * time.Second)))
	if left, _ := os.ReadDir(scratch); len(left) > 0 {
		names := []string{}
		for _, entry := range left {
			names = append(names, entry.Name())
		}
		t.Fatalf("a snapshot cut off at its time limit left %s in the temporary folder", strings.Join(names, ", "))
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a snapshot cut off at its time limit left the git update-index it started running: its clean filter finished 2 seconds after noctis gave up")
	}
}
