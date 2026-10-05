package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func forgetGitStatuses(t *testing.T) {
	t.Helper()
	gitStatusCache = map[string]gitStatusCacheEntry{}
	t.Cleanup(func() { gitStatusCache = map[string]gitStatusCacheEntry{} })
}

func gitStatusRuns() int {
	gitStatusCacheLock.Lock()
	defer gitStatusCacheLock.Unlock()
	return len(gitStatusRunning)
}

func TestTheStatusAHookReadsIsTheOneStartedAhead(t *testing.T) {
	repo := workspaceRepo(t)
	forgetGitStatuses(t)
	writeRepoFile(t, repo, "before.txt", "made before the run\n")
	gitStatusAhead(repo)()
	if runs := gitStatusRuns(); runs != 0 {
		t.Fatalf("%d git status run(s) still under way after waiting for the one started ahead", runs)
	}
	writeRepoFile(t, repo, "after.txt", "made after the run\n")
	lines := strings.Join(gitStatus(repo), "\n")
	if !strings.Contains(lines, "before.txt") || strings.Contains(lines, "after.txt") {
		t.Fatalf("the status read after a run started ahead is not that run's: %q", lines)
	}
}

func TestAStatusAskedForWhileARunIsUnderWayWaitsForThatRun(t *testing.T) {
	repo := workspaceRepo(t)
	forgetGitStatuses(t)
	var started atomic.Int32
	previous := hideConsole
	hideConsole = func(command *exec.Cmd) {
		if strings.TrimSuffix(filepath.Base(command.Path), ".exe") == "git" {
			started.Add(1)
			// Slow enough that the status is asked for while the run is still under way.
			time.Sleep(50 * time.Millisecond)
		}
		previous(command)
	}
	t.Cleanup(func() { hideConsole = previous })
	settled := gitStatusAhead(repo)
	if _, ok := gitStatusRaw(repo); !ok {
		t.Fatal("no git status in a repository")
	}
	settled()
	if runs := started.Load(); runs != 1 {
		t.Fatalf("git status started %d times for one hook, want once", runs)
	}
}

func TestAFreshStatusIsNotRunAgainAhead(t *testing.T) {
	repo := workspaceRepo(t)
	forgetGitStatuses(t)
	if _, ok := gitStatusRaw(repo); !ok {
		t.Fatal("no git status in a repository")
	}
	writeRepoFile(t, repo, "later.txt", "made after the status was read\n")
	gitStatusAhead(repo)()
	if lines := strings.Join(gitStatus(repo), "\n"); strings.Contains(lines, "later.txt") {
		t.Fatalf("a status read %v ago was run again ahead: %q", gitStatusCacheTTL, lines)
	}
	outside := t.TempDir()
	gitStatusAhead(outside)()
	gitStatusCacheLock.Lock()
	_, ran := gitStatusCache[outside]
	gitStatusCacheLock.Unlock()
	if ran {
		t.Fatal("git status was started ahead in a folder outside a repository")
	}
}

func TestAStopFailureThatGivesUpLeavesNoGitStatusRunning(t *testing.T) {
	sandboxFiles(t)
	repo := workspaceRepo(t)
	forgetGitStatuses(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	cfg := waitEngineConfig()
	cfg["fable"] = object{"source": "off"}
	sid := "gives-up-in-a-repository"
	input := object{"session_id": sid, "cwd": repo, "error_type": "unknown", "error_message": "API Error: Request timed out"}
	writeRepoFile(t, repo, "dirty.txt", "not committed\n")
	for step := 1; step <= stopFailureMaxAttempts; step++ {
		updateState(func(state object) { delete(stateMap(state, "waits"), sid) })
		gitStatusCache = map[string]gitStatusCacheEntry{}
		onStopFailure(input, cfg)
		wait := getMap(getMap(readState(), "waits"), sid)
		if wait == nil {
			t.Fatalf("failure %d in a row was not parked", step)
		}
		if checkpoint, _ := os.ReadFile(getString(wait, "checkpoint")); !strings.Contains(string(checkpoint), "?? dirty.txt") {
			t.Fatalf("the checkpoint of failure %d in a row does not list the uncommitted file: %q", step, checkpoint)
		}
	}
	updateState(func(state object) { delete(stateMap(state, "waits"), sid) })
	// The run started ahead takes longer than the rest of the hook, as it can in a large tree.
	previous := hideConsole
	hideConsole = func(command *exec.Cmd) {
		if strings.TrimSuffix(filepath.Base(command.Path), ".exe") == "git" {
			time.Sleep(150 * time.Millisecond)
		}
		previous(command)
	}
	t.Cleanup(func() { hideConsole = previous })
	gitStatusCache = map[string]gitStatusCacheEntry{}
	onStopFailure(input, cfg)
	if wait := getMap(getMap(readState(), "waits"), sid); wait != nil {
		t.Fatalf("the session was parked for another retry after %d failures: %v", stopFailureMaxAttempts+1, wait)
	}
	if runs := gitStatusRuns(); runs != 0 {
		t.Fatalf("the StopFailure hook ended with %d git status run(s) it started still under way", runs)
	}
}
