package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// s3ForgedHandOver is a used-checkpoints.json record that makes note count as handed to sid, so
// noctis would let sid read another session's checkpoint note without asking.
func s3ForgedHandOver(t *testing.T, note, sid string) string {
	t.Helper()
	forged, err := json.Marshal(object{"s3-other": object{"path": note, "cwd": "/tmp", "at": float64(nowSec()), "consumed": true, "handedTo": sid}})
	if err != nil {
		t.Fatal(err)
	}
	return string(forged)
}

// s3DeniedAsState tells whether noctis denied a file tool call as a write to its own state.
func s3DeniedAsState(t *testing.T, call object) (bool, object) {
	t.Helper()
	run := runHostHook(t, "claude", call, getString(call, "session_id"), 10*time.Second)
	return permissionOf(run.answer) == "deny" && getString(run.answer, "systemMessage") == T("queue.stateFileByModel", pluginName), run.answer
}

func TestClaudeMayNotWriteNoctisUsedCheckpointsFile(t *testing.T) {
	queueTrustSandbox(t, false)
	used := usedCheckpointsFile()
	note := plantNote(t, "s3-other")
	// Before a checkpoint was used the file is not there yet: creating it is a write all the same.
	create := fileToolCall("s3-own", "Write", used)
	getMap(create, "tool_input")["content"] = s3ForgedHandOver(t, note, "s3-own")
	if denied, answer := s3DeniedAsState(t, create); !denied {
		t.Errorf("Claude's Write of a forged hand-over to the missing used-checkpoints.json was not denied: %v", answer)
	}
	plantCheckpoint(t, "s3-giver", t.TempDir(), nowSec()-60)
	handOverCheckpoint("s3-giver", "s3-taker")
	if statSafe(used) == nil {
		t.Fatal("handing a checkpoint over wrote no used-checkpoints.json")
	}
	link := filepath.Join(t.TempDir(), "used-link.json")
	if err := os.Symlink(used, link); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ tool, path string }{
		{"Write", used},
		{"Edit", used},
		{"MultiEdit", used},
		{"Write", link},
	} {
		if denied, answer := s3DeniedAsState(t, fileToolCall("s3-own", tc.tool, tc.path)); !denied {
			t.Errorf("Claude's %s to noctis's used-checkpoints.json (%s) was not denied: %v", tc.tool, filepath.Base(tc.path), answer)
		}
	}
	if checkpointHandedTo("s3-own", note) {
		t.Fatal("another session's note counts as handed to s3-own")
	}
	if !checkpointHandedTo("s3-taker", filepath.Join(files.checkpoints, "s3-giver.md")) {
		t.Fatal("the note noctis handed over is no longer the receiver's")
	}
}
