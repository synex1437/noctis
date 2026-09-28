package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gitSaysHead(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(gitIn(t, dir, "rev-parse", "-q", "--verify", "HEAD"))
}

func headReadFromFiles(t *testing.T, what, cwd string) {
	t.Helper()
	head, ok := gitHeadFromFiles(cwd)
	if !ok {
		t.Fatalf("%s: HEAD was left to git, though noctis reads this layout itself", what)
	}
	if want := gitSaysHead(t, cwd); head != want {
		t.Fatalf("%s: HEAD read from the repository's files is %q, git says %q", what, head, want)
	}
}

func TestHeadIsReadFromTheFilesAsGitGivesItInEveryLayoutNoctisReads(t *testing.T) {
	repo := workspaceRepo(t)
	headReadFromFiles(t, "a branch with a loose ref", repo)
	writeRepoFile(t, repo, "sub/deeper/b.txt", "two\n")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "second")
	headReadFromFiles(t, "a folder below the top of the working tree", filepath.Join(repo, "sub", "deeper"))
	gitIn(t, repo, "pack-refs", "--all")
	headReadFromFiles(t, "a packed branch", repo)
	writeRepoFile(t, repo, "a.txt", "three\n")
	gitIn(t, repo, "commit", "-q", "-am", "third")
	headReadFromFiles(t, "a loose ref newer than the packed one", repo)
	gitIn(t, repo, "checkout", "-q", "--detach", "HEAD~1")
	headReadFromFiles(t, "a detached HEAD", repo)
	gitIn(t, repo, "checkout", "-q", "-")
	headReadFromFiles(t, "a branch checked out again", repo)

	linked := filepath.Join(t.TempDir(), "linked")
	gitIn(t, repo, "worktree", "add", "-q", "-b", "side", linked)
	headReadFromFiles(t, "a linked working tree", linked)
	writeRepoFile(t, linked, "c.txt", "on the side\n")
	gitIn(t, linked, "add", ".")
	gitIn(t, linked, "commit", "-q", "-m", "on the side")
	headReadFromFiles(t, "a commit in a linked working tree", linked)
	headReadFromFiles(t, "the main working tree after a commit in a linked one", repo)
	gitIn(t, repo, "pack-refs", "--all")
	headReadFromFiles(t, "a linked working tree whose branch is packed", linked)
}

func TestHeadIsReadThroughAGitFileThatPointsElsewhere(t *testing.T) {
	repo := workspaceRepo(t)
	store := filepath.Join(t.TempDir(), "store.git")
	if err := os.Rename(filepath.Join(repo, ".git"), store); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(repo, store)
	if err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, repo, ".git", "gitdir: "+filepath.ToSlash(relative)+"\n")
	headReadFromFiles(t, "a .git file with a relative gitdir", repo)
	writeRepoFile(t, repo, ".git", "gitdir: "+store+"\n")
	headReadFromFiles(t, "a .git file with an absolute gitdir", repo)
}

func TestHeadIsLeftToGitWhereTheFilesWouldBeAGuess(t *testing.T) {
	leftToGit := func(t *testing.T, what, cwd, want string) {
		t.Helper()
		if head, ok := gitHeadFromFiles(cwd); ok {
			t.Fatalf("%s: HEAD was read from the files as %q instead of being left to git", what, head)
		}
		if head := gitHead(cwd); head != want {
			t.Fatalf("%s: HEAD is %q, git says %q", what, head, want)
		}
	}
	t.Run("a branch with no commit yet", func(t *testing.T) {
		workspaceRepo(t)
		fresh := t.TempDir()
		gitIn(t, fresh, "init", "-q")
		leftToGit(t, "a branch with no commit yet", fresh, "")
	})
	t.Run("a symbolic ref behind HEAD", func(t *testing.T) {
		repo := workspaceRepo(t)
		branch := strings.TrimSpace(gitIn(t, repo, "symbolic-ref", "HEAD"))
		gitIn(t, repo, "symbolic-ref", "refs/heads/alias", branch)
		gitIn(t, repo, "symbolic-ref", "HEAD", "refs/heads/alias")
		leftToGit(t, "HEAD naming a branch that is itself a symbolic ref", repo, gitSaysHead(t, repo))
	})
	t.Run("a reference table", func(t *testing.T) {
		repo := workspaceRepo(t)
		want := gitSaysHead(t, repo)
		if err := os.MkdirAll(filepath.Join(repo, ".git", "reftable"), 0o755); err != nil {
			t.Fatal(err)
		}
		leftToGit(t, "a repository with a reftable folder", repo, want)
	})
	t.Run("a loose ref that cannot be read", func(t *testing.T) {
		repo := workspaceRepo(t)
		gitIn(t, repo, "pack-refs", "--all")
		writeRepoFile(t, repo, "a.txt", "newer than the packed branch\n")
		gitIn(t, repo, "commit", "-q", "-am", "newer")
		branch := strings.TrimSpace(gitIn(t, repo, "symbolic-ref", "HEAD"))
		loose := filepath.Join(repo, ".git", filepath.FromSlash(branch))
		if err := os.Chmod(loose, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(loose, 0o644) })
		if _, err := os.ReadFile(loose); err == nil {
			t.Skip("this user reads a file whatever its mode says")
		}
		// The packed branch is the commit before, and git itself cannot read the loose one.
		leftToGit(t, "a loose ref that cannot be read", repo, "")
	})
	t.Run("git steered by the environment", func(t *testing.T) {
		repo := workspaceRepo(t)
		other := workspaceRepo(t)
		writeRepoFile(t, other, "a.txt", "another history\n")
		gitIn(t, other, "commit", "-q", "-am", "another commit")
		own := gitSaysHead(t, repo)
		t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
		want := gitSaysHead(t, repo)
		if want == own {
			t.Fatal("the test needs GIT_DIR to name another history than the folder's own .git")
		}
		leftToGit(t, "GIT_DIR naming another repository", repo, want)
	})
}

func TestHeadIsReadWithoutStartingGit(t *testing.T) {
	repo := workspaceRepo(t)
	want := gitSaysHead(t, repo)
	t.Setenv("PATH", t.TempDir())
	if head := gitHead(repo); head != want {
		t.Fatalf("with no git to start, HEAD is %q, want %q read from the repository's files", head, want)
	}
}

func TestTheIndexIsFoundInTheFilesWhereGitFindsIt(t *testing.T) {
	repo := workspaceRepo(t)
	writeRepoFile(t, repo, "sub/b.txt", "two\n")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "second")
	linked := filepath.Join(t.TempDir(), "linked")
	gitIn(t, repo, "worktree", "add", "-q", "-b", "side", linked)
	gitSaysIndex := func(cwd string) string {
		said := strings.TrimSpace(gitIn(t, cwd, "rev-parse", "--git-path", "index"))
		if !filepath.IsAbs(said) {
			said = filepath.Join(cwd, said)
		}
		return said
	}
	for _, cwd := range []string{repo, filepath.Join(repo, "sub"), linked} {
		found, ok := gitIndexPath(cwd)
		if !ok {
			t.Fatalf("no index found for %s", cwd)
		}
		foundInfo, err := os.Stat(found)
		if err != nil {
			t.Fatalf("the index found for %s is %s, which does not exist: %v", cwd, found, err)
		}
		wantInfo, err := os.Stat(gitSaysIndex(cwd))
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(foundInfo, wantInfo) {
			t.Fatalf("the index found for %s is %s, git says %s", cwd, found, gitSaysIndex(cwd))
		}
	}
	elsewhere := filepath.Join(t.TempDir(), "other-index")
	t.Setenv("GIT_INDEX_FILE", elsewhere)
	if found, ok := gitIndexPath(repo); !ok || filepath.Clean(found) != filepath.Clean(gitSaysIndex(repo)) {
		t.Fatalf("with GIT_INDEX_FILE set, the index found is %q (ok %t), git says %q", found, ok, gitSaysIndex(repo))
	}
}
