package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func workspaceRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is not installed")
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	writeRepoFile(t, repo, "a.txt", "one\n")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "init")
	return repo
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-c", "user.name=noctis", "-c", "user.email=noctis@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func writeRepoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func freshTreeFingerprint(t *testing.T, cwd string) string {
	t.Helper()
	gitStatusCache = map[string]gitStatusCacheEntry{}
	fingerprint := treeFingerprint(cwd)
	if fingerprint == "" {
		t.Fatalf("no fingerprint for the repository at %s", cwd)
	}
	return fingerprint
}

func treeChangedSince(cwd, fingerprint string) bool {
	gitStatusCache = map[string]gitStatusCacheEntry{}
	return workspaceChanged(object{"tree": fingerprint, "cwd": cwd})
}

func TestTheWorkspaceGuardSeesAnotherEditToAFileThatWasAlreadyDirty(t *testing.T) {
	repo := workspaceRepo(t)
	writeRepoFile(t, repo, "a.txt", "claude edited this before the pause\n")
	before := freshTreeFingerprint(t, repo)
	if treeChangedSince(repo, before) {
		t.Fatal("the guard reported a change although nothing was touched after the fingerprint")
	}
	writeRepoFile(t, repo, "a.txt", "claude edited this before the pause\nand the user edited it again while the session waited\n")
	if !treeChangedSince(repo, before) {
		t.Fatal("a file that was already modified at the pause was edited again while the session waited, and the guard did not notice: git status still reads \" M a.txt\"")
	}
}

func TestTheWorkspaceGuardSeesACommit(t *testing.T) {
	repo := workspaceRepo(t)
	before := freshTreeFingerprint(t, repo)
	if treeChangedSince(repo, before) {
		t.Fatal("the guard reported a change on a clean tree nobody touched")
	}
	writeRepoFile(t, repo, "b.txt", "written and committed while the session waited\n")
	gitIn(t, repo, "add", "b.txt")
	gitIn(t, repo, "commit", "-q", "-m", "while it waited")
	if !treeChangedSince(repo, before) {
		t.Fatal("new code was committed while the session waited, and the guard did not notice: git status is empty before and after")
	}
}

func TestTheWorkspaceGuardSeesAPull(t *testing.T) {
	repo := workspaceRepo(t)
	upstream := filepath.Join(t.TempDir(), "upstream")
	gitIn(t, repo, "clone", "-q", repo, upstream)
	writeRepoFile(t, upstream, "a.txt", "one\nchanged upstream\n")
	gitIn(t, upstream, "commit", "-q", "-am", "upstream change")
	before := freshTreeFingerprint(t, repo)
	gitIn(t, repo, "pull", "-q", "--ff-only", upstream, "HEAD")
	if !treeChangedSince(repo, before) {
		t.Fatal("a pull brought new code in while the session waited, and the guard did not notice: git status is empty before and after")
	}
}

func TestTheWorkspaceGuardSeesAnEditInsideAnUntrackedFolder(t *testing.T) {
	repo := workspaceRepo(t)
	writeRepoFile(t, repo, "newdir/draft.txt", "claude wrote this before the pause\n")
	before := freshTreeFingerprint(t, repo)
	if treeChangedSince(repo, before) {
		t.Fatal("the guard reported a change although nothing was touched after the fingerprint")
	}
	writeRepoFile(t, repo, "newdir/draft.txt", "claude wrote this before the pause\nthe user rewrote it while the session waited\n")
	if !treeChangedSince(repo, before) {
		t.Fatal("a file in a folder git lists only as \"?? newdir/\" was edited while the session waited, and the guard did not notice")
	}
}

func TestTheWorkspaceGuardFollowsQuotedAndRenamedPathsFromASubfolder(t *testing.T) {
	repo := workspaceRepo(t)
	writeRepoFile(t, repo, "a b.txt", "spaced\n")
	writeRepoFile(t, repo, "über.txt", "umlaut\n")
	writeRepoFile(t, repo, "plain.txt", "plain\n")
	writeRepoFile(t, repo, "sub/x.txt", "inside\n")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "more")
	gitIn(t, repo, "mv", "plain.txt", "re named.txt")
	writeRepoFile(t, repo, "re named.txt", "plain, renamed and edited by claude\n")
	writeRepoFile(t, repo, "a b.txt", "spaced, edited by claude\n")
	writeRepoFile(t, repo, "über.txt", "umlaut, edited by claude\n")
	session := filepath.Join(repo, "sub")
	before := freshTreeFingerprint(t, session)
	if treeChangedSince(session, before) {
		t.Fatal("the guard reported a change although nothing was touched after the fingerprint")
	}
	for _, edit := range []struct{ name, content string }{
		{"a b.txt", "spaced, edited by claude and then by the user\n"},
		{"über.txt", "umlaut, edited by claude and then by the user\n"},
		{"re named.txt", "plain, renamed by claude and edited by the user\n"},
	} {
		before = freshTreeFingerprint(t, session)
		writeRepoFile(t, repo, edit.name, edit.content)
		if !treeChangedSince(session, before) {
			t.Fatalf("%q, which git status lists quoted and relative to the session's folder, was edited again while the session waited, and the guard did not notice", edit.name)
		}
	}
}

func TestTheWorkspaceGuardReadsGitStatusWhateverTheUsersGitConfigSays(t *testing.T) {
	repo := workspaceRepo(t)
	writeRepoFile(t, repo, "sub/x.txt", "inside\n")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "sub")
	gitIn(t, repo, "config", "status.relativePaths", "false")
	gitIn(t, repo, "config", "color.status", "always")
	writeRepoFile(t, repo, "a.txt", "claude edited this before the pause\n")
	writeRepoFile(t, repo, "sub/x.txt", "claude edited this too\n")
	for _, session := range []string{repo, filepath.Join(repo, "sub")} {
		for _, name := range []string{"a.txt", "sub/x.txt"} {
			before := freshTreeFingerprint(t, session)
			writeRepoFile(t, repo, name, "edited again while the session in "+session+" waited\n"+name+"\n")
			if !treeChangedSince(session, before) {
				t.Fatalf("with status.relativePaths off and color.status always, an edit to the already modified %s was not noticed from %s", name, session)
			}
		}
	}
	gitStatusCache = map[string]gitStatusCacheEntry{}
	for _, line := range gitStatus(filepath.Join(repo, "sub")) {
		if strings.Contains(line, "\x1b") {
			t.Fatalf("the checkpoint's git status lines carry colour codes: %q", line)
		}
	}
}

func TestTheWorkspaceGuardSeesAListedFileAfterABigUntrackedFolder(t *testing.T) {
	repo := workspaceRepo(t)
	for i := 0; i <= treeStatLimit; i++ {
		writeRepoFile(t, repo, fmt.Sprintf("aaa/f%04d.txt", i), "generated\n")
	}
	writeRepoFile(t, repo, "zzz.txt", "claude wrote this before the pause\n")
	writeRepoFile(t, repo, "a.txt", "claude edited this before the pause\n")
	before := freshTreeFingerprint(t, repo)
	if treeChangedSince(repo, before) {
		t.Fatal("the guard reported a change although nothing was touched after the fingerprint")
	}
	writeRepoFile(t, repo, "zzz.txt", "claude wrote this before the pause\nthe user rewrote it while the session waited\n")
	if !treeChangedSince(repo, before) {
		t.Fatalf("zzz.txt, which git status lists after an untracked folder of %d files, was edited while the session waited, and the guard did not notice", treeStatLimit+1)
	}
	before = freshTreeFingerprint(t, repo)
	writeRepoFile(t, repo, "aaa/f0000.txt", "generated, then edited by the user while the session waited\n")
	if !treeChangedSince(repo, before) {
		t.Fatal("the first file of the big untracked folder was edited while the session waited, and the guard did not notice")
	}
}

func TestAFetchWhileTheSessionWaitsIsNoWorkspaceChangeWithStatusBranchOn(t *testing.T) {
	upstream := workspaceRepo(t)
	repo := filepath.Join(t.TempDir(), "clone")
	gitIn(t, upstream, "clone", "-q", upstream, repo)
	gitIn(t, repo, "config", "status.branch", "true")
	writeRepoFile(t, repo, "a.txt", "claude edited this before the pause\n")
	before := freshTreeFingerprint(t, repo)
	writeRepoFile(t, upstream, "b.txt", "committed upstream while the session waited\n")
	gitIn(t, upstream, "add", "b.txt")
	gitIn(t, upstream, "commit", "-q", "-m", "upstream")
	gitIn(t, repo, "fetch", "-q")
	if treeChangedSince(repo, before) {
		t.Fatal("with status.branch on, a fetch that only moved the upstream branch was reported as a workspace change: no file, no commit HEAD points at and no listed path changed")
	}
	gitStatusCache = map[string]gitStatusCacheEntry{}
	for _, line := range gitStatus(repo) {
		if strings.HasPrefix(line, "##") {
			t.Fatalf("the checkpoint's git status lines carry the branch line %q as if it were a changed path", line)
		}
	}
	writeRepoFile(t, repo, "a.txt", "claude edited this before the pause\nand the user edited it again\n")
	if !treeChangedSince(repo, before) {
		t.Fatal("with status.branch on, another edit to the already modified a.txt was not noticed")
	}
}
