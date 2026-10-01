package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func progressSandbox(t *testing.T, repository bool) (object, string) {
	t.Helper()
	cfg, project := stopCapSandbox(t, "claude", 3, 3)
	t.Setenv("CLAUDE_CODE_STOP_HOOK_BLOCK_CAP", "")
	if repository {
		gitIn(t, project, "init", "-q")
		writeRepoFile(t, project, "work/start.txt", "start\n")
		gitIn(t, project, "add", "-A")
		gitIn(t, project, "commit", "-q", "-m", "start")
	}
	return cfg, project
}

func stopSteps(t *testing.T, cfg object, sid, project string, stops int, before func(stop int)) []string {
	t.Helper()
	steps := []string{}
	for stop := 0; stop < stops; stop++ {
		if before != nil && stop > 0 {
			before(stop)
		}
		input := stopInput(sid, project)
		input["stop_hook_active"] = stop > 0
		output := stopHookOutput(t, input, cfg)
		reason := getString(output, "reason")
		switch {
		case getString(output, "decision") != "block":
			steps = append(steps, "let go")
		case strings.Contains(reason, "Hand it once"):
			steps = append(steps, "escalate")
		case strings.Contains(reason, "Find the root cause of what holds it up"):
			steps = append(steps, "set aside")
		default:
			steps = append(steps, "continue")
		}
		if getString(output, "decision") != "block" {
			return steps
		}
	}
	return steps
}

func treeMovedContinues(sid string) int {
	moved := 0
	for _, line := range tailFileLines(files.decisions, 1000) {
		var entry object
		if jsonUnmarshalObject([]byte(line), &entry) == nil && getString(entry, "sid") == sid && getString(entry, "action") == "continue-queue" && getBool(entry, "treeMoved", false) {
			moved++
		}
	}
	return moved
}

func TestEditsBetweenStopsHoldTheStuckStepOffForAsManyStopsAsTheIdleLimit(t *testing.T) {
	cfg, project := progressSandbox(t, true)
	steps := stopSteps(t, cfg, "tp1", project, 9, func(stop int) {
		writeRepoFile(t, project, fmt.Sprintf("work/%d.txt", stop), fmt.Sprintf("step %d\n", stop))
	})
	want := []string{"continue", "continue", "continue", "continue", "continue", "escalate", "set aside", "let go"}
	if !slices.Equal(steps, want) {
		t.Fatalf("a session that edits files before every stop without ticking or committing got %v, want %v: the first stop takes the tree as it is, the next three count the changed tree, and from there it is stuck as before", steps, want)
	}
	if moved := treeMovedContinues("tp1"); moved != 3 {
		t.Fatalf("%d continuations are journaled with treeMoved, want 3", moved)
	}
}

func TestStopsWithoutEditsAreStuckAsBefore(t *testing.T) {
	cfg, project := progressSandbox(t, true)
	steps := stopSteps(t, cfg, "tp2", project, 9, nil)
	if want := []string{"continue", "continue", "escalate", "set aside", "let go"}; !slices.Equal(steps, want) {
		t.Fatalf("a session that changes nothing between stops got %v, want %v", steps, want)
	}
	if moved := treeMovedContinues("tp2"); moved != 0 {
		t.Fatalf("%d continuations without a change are journaled with treeMoved", moved)
	}
}

func TestATreeThatReturnsToAnEarlierStateIsNotProgress(t *testing.T) {
	cfg, project := progressSandbox(t, true)
	steps := stopSteps(t, cfg, "tp3", project, 9, func(stop int) {
		writeRepoFile(t, project, "work/flip.txt", []string{"one\n", "two\n"}[stop%2])
	})
	if want := []string{"continue", "continue", "continue", "escalate", "set aside", "let go"}; !slices.Equal(steps, want) {
		t.Fatalf("a session that edits a file back and forth between two contents got %v, want %v: going back to a tree seen before is not progress", steps, want)
	}
}

func TestEditsToTheQueueFileAloneAreNotProgress(t *testing.T) {
	cfg, project := progressSandbox(t, true)
	queuePath := filepath.Join(project, "TASKS.md")
	steps := stopSteps(t, cfg, "tp4", project, 9, func(stop int) {
		file, err := os.OpenFile(queuePath, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(file, "\n")
		file.Close()
	})
	if want := []string{"continue", "continue", "escalate", "set aside", "let go"}; !slices.Equal(steps, want) {
		t.Fatalf("a session that only rewrites the queue file between stops got %v, want %v", steps, want)
	}
}

func TestEditsOutsideAGitRepositoryAreNotSeen(t *testing.T) {
	cfg, project := progressSandbox(t, false)
	steps := stopSteps(t, cfg, "tp5", project, 9, func(stop int) {
		writeRepoFile(t, project, fmt.Sprintf("work/%d.txt", stop), "step\n")
	})
	if want := []string{"continue", "continue", "escalate", "set aside", "let go"}; !slices.Equal(steps, want) {
		t.Fatalf("a session outside a git repository got %v, want %v: without git status there is no tree to compare", steps, want)
	}
}

func TestACommitStartsTheTreeAllowanceAgain(t *testing.T) {
	cfg, project := progressSandbox(t, true)
	steps := stopSteps(t, cfg, "tp6", project, 14, func(stop int) {
		writeRepoFile(t, project, fmt.Sprintf("work/%d.txt", stop), "step\n")
		if stop == 5 {
			gitIn(t, project, "add", "-A")
			gitIn(t, project, "commit", "-q", "-m", "halfway")
		}
	})
	want := append(slices.Repeat([]string{"continue"}, 10), "escalate", "set aside", "let go")
	if !slices.Equal(steps, want) {
		t.Fatalf("a commit at the sixth stop did not start the tree allowance again: %v, want %v", steps, want)
	}
}

func TestWorktreeFingerprintFollowsContentNotTimes(t *testing.T) {
	project := t.TempDir()
	gitIn(t, project, "init", "-q")
	writeRepoFile(t, project, "src/a.txt", "a\n")
	queuePath := writeQueueFile(t, project, "- [ ] one\n")
	gitIn(t, project, "add", "-A")
	gitIn(t, project, "commit", "-q", "-m", "start")
	clean := worktreeFingerprint(project, queuePath)
	if clean == "" {
		t.Fatal("no fingerprint for a clean repository")
	}
	writeRepoFile(t, project, "src/a.txt", "b\n")
	changed := worktreeFingerprint(project, queuePath)
	if changed == clean {
		t.Fatal("a changed file left the fingerprint as it was")
	}
	writeRepoFile(t, project, "src/a.txt", "a\n")
	if back := worktreeFingerprint(project, queuePath); back != clean {
		t.Fatalf("the file written back to its committed content gave %s, want the clean %s", back, clean)
	}
	writeQueueFile(t, project, "- [x] one\n")
	if ticked := worktreeFingerprint(project, queuePath); ticked != clean {
		t.Fatalf("a change to the queue file alone gave %s, want %s", ticked, clean)
	}
	writeRepoFile(t, project, "new/b.txt", "b\n")
	untracked := worktreeFingerprint(project, queuePath)
	writeRepoFile(t, project, "new/b.txt", "c\n")
	if again := worktreeFingerprint(project, queuePath); again == untracked {
		t.Fatal("a change inside an untracked folder left the fingerprint as it was")
	}
	if outside := worktreeFingerprint(t.TempDir(), ""); outside != "" {
		t.Fatalf("a folder outside any repository has the fingerprint %q", outside)
	}
}

func TestAPromptStartsTheTreeAllowanceAgain(t *testing.T) {
	sandboxFiles(t)
	updateState(func(state object) {
		stateMap(state, "stopGuard")["tp7"] = object{"idle": float64(0), "trees": []any{"aa", "bb"}, "treeMoves": float64(1), "at": float64(nowSec())}
	})
	resetIdleGuard(readState(), "tp7")
	if guard := getMap(getMap(readState(), "stopGuard"), "tp7"); guard == nil || guard["trees"] != nil || guard["treeMoves"] != nil {
		t.Fatalf("a prompt left the tree allowance of the stop guard: %v", guard)
	}
}
