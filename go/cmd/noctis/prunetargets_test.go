package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func plantOldChecklist(t *testing.T, file string) {
	t.Helper()
	ensureDir(filepath.Dir(file))
	if err := os.WriteFile(file, []byte("- [ ] keep me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	weekAgo := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(file, weekAgo, weekAgo); err != nil {
		t.Fatal(err)
	}
}

func pathOutThroughALink(t *testing.T, folder, name string) (string, string) {
	t.Helper()
	outside := t.TempDir()
	away := filepath.Join(outside, "away")
	ensureDir(away)
	escaped := filepath.Join(outside, name)
	plantOldChecklist(t, escaped)
	ensureDir(folder)
	linkOrSkip(t, away, filepath.Join(folder, "away"))
	return strings.Join([]string{folder, "away", "..", name}, string(filepath.Separator)), escaped
}

func TestPruningNeverDeletesAFileOrGitRefThatIsNotNoctiss(t *testing.T) {
	dir := sandboxFiles(t)
	repo := workspaceRepo(t)
	head := strings.TrimSpace(gitIn(t, repo, "rev-parse", "HEAD"))
	gitIn(t, repo, "branch", "keep")
	gitIn(t, repo, "branch", "work")
	gitIn(t, repo, "update-ref", "refs/noctis/old-session/1700000000", head)
	gitIn(t, repo, "symbolic-ref", "refs/noctis/linked-session/1700000000", "refs/heads/work")
	outside := t.TempDir()
	notes, todo := filepath.Join(outside, "notes.md"), filepath.Join(outside, "todo.md")
	checkpoint, linked, queue := filepath.Join(files.checkpoints, "old-session.md"), filepath.Join(files.checkpoints, "linked-session.md"), filepath.Join(dir, "queues", "old-queue.md")
	for _, file := range []string{notes, todo, checkpoint, linked, queue} {
		plantOldChecklist(t, file)
	}
	expired := float64(time.Now().Add(-8 * 24 * time.Hour).Unix())
	mustWriteJSON(files.state, object{
		"checkpoints": object{
			"old-session":    object{"path": checkpoint, "cwd": repo, "at": expired, "snapshot": "refs/noctis/old-session/1700000000"},
			"linked-session": object{"path": linked, "cwd": repo, "at": expired, "snapshot": "refs/noctis/linked-session/1700000000"},
			"tampered":       object{"path": notes, "cwd": repo, "at": expired, "snapshot": "refs/heads/keep"},
		},
		"autoQueues": object{
			"old-queue":      object{"path": queue, "cwd": repo, "at": expired},
			"tampered-queue": object{"path": todo, "cwd": repo, "at": expired},
		},
	})

	updateState(func(object) {})

	for _, file := range []string{notes, todo} {
		if statSafe(file) == nil {
			t.Errorf("pruning an expired record deleted %s, a file outside the noctis folder that state.json named", file)
		}
	}
	refs := gitIn(t, repo, "for-each-ref", "--format=%(refname)")
	if !strings.Contains(refs, "refs/heads/keep\n") {
		t.Errorf("pruning an expired checkpoint ran `git update-ref -d refs/heads/keep` because state.json named it as the snapshot; refs left: %q", refs)
	}
	if !strings.Contains(refs, "refs/heads/work\n") {
		t.Errorf("pruning an expired checkpoint deleted refs/heads/work, the branch its snapshot ref refs/noctis/linked-session/1700000000 pointed at; refs left: %q", refs)
	}
	for _, file := range []string{checkpoint, linked, queue} {
		if statSafe(file) != nil {
			t.Errorf("the expired record's own file %s was not removed", file)
		}
	}
	if strings.Contains(refs, "refs/noctis/") {
		t.Errorf("the expired checkpoints' own snapshot refs were not removed: %q", refs)
	}
	state := readState()
	if len(getMap(state, "checkpoints")) != 0 || len(getMap(state, "autoQueues")) != 0 {
		t.Errorf("expired records are still in state.json: checkpoints %v, autoQueues %v", getMap(state, "checkpoints"), getMap(state, "autoQueues"))
	}

	t.Run("through a symbolic link in the noctis folder", func(t *testing.T) {
		viaCheckpoints, plans := pathOutThroughALink(t, files.checkpoints, "plans.md")
		viaQueues, ideas := pathOutThroughALink(t, autoQueueDir(), "ideas.md")
		mustWriteJSON(files.state, object{
			"checkpoints": object{"escaping": object{"path": viaCheckpoints, "at": expired}},
			"autoQueues":  object{"escaping-queue": object{"path": viaQueues, "at": expired}},
		})

		updateState(func(object) {})

		for _, file := range []string{plans, ideas} {
			if statSafe(file) == nil {
				t.Errorf("pruning an expired record deleted %s, outside the noctis folder, through a symbolic link and .. in the path state.json named", file)
			}
		}
		state := readState()
		if len(getMap(state, "checkpoints")) != 0 || len(getMap(state, "autoQueues")) != 0 {
			t.Errorf("expired records are still in state.json: checkpoints %v, autoQueues %v", getMap(state, "checkpoints"), getMap(state, "autoQueues"))
		}
	})
}

func TestEndingAnAutoQueueDeletesOnlyAFileInTheQueuesFolder(t *testing.T) {
	dir := sandboxFiles(t)
	outside, own := filepath.Join(t.TempDir(), "draft.md"), filepath.Join(dir, "queues", "own.md")
	for _, file := range []string{outside, own} {
		ensureDir(filepath.Dir(file))
		if err := os.WriteFile(file, []byte("- [ ] keep me\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := float64(nowSec())
	updateState(func(state object) {
		stateMap(state, "autoQueues")["tampered"] = object{"path": outside, "at": now}
		stateMap(state, "autoQueues")["own"] = object{"path": own, "at": now}
	})

	endAutoQueue("tampered", true)
	endAutoQueue("own", true)

	if statSafe(outside) == nil {
		t.Errorf("ending the auto-queue of a session deleted %s, a file outside the queues folder that state.json named", outside)
	}
	if statSafe(own) != nil {
		t.Errorf("ending an auto-queue left its own checklist %s behind", own)
	}
	if queues := getMap(readState(), "autoQueues"); len(queues) != 0 {
		t.Errorf("ended auto-queues are still in state.json: %v", queues)
	}

	t.Run("through a symbolic link in the queues folder", func(t *testing.T) {
		through, escaped := pathOutThroughALink(t, autoQueueDir(), "plans.md")
		updateState(func(state object) {
			stateMap(state, "autoQueues")["escaping"] = object{"path": through, "at": now}
		})

		endAutoQueue("escaping", true)

		if statSafe(escaped) == nil {
			t.Errorf("ending the auto-queue of a session deleted %s, outside the queues folder, through a symbolic link and .. in the path state.json named", escaped)
		}
		if queues := getMap(readState(), "autoQueues"); len(queues) != 0 {
			t.Errorf("the ended auto-queue is still in state.json: %v", queues)
		}
	})
}
