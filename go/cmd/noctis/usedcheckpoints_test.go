package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkpointRecord is the record of sid's checkpoint wherever it is kept: in state.json while it
// waits to be used, in used-checkpoints.json once it was.
func checkpointRecord(sid string) object {
	return toObject(allCheckpoints(readState())[sid])
}

// storedRecords is what is on disk, not a parse noctis kept: the checkpoints in state.json, or
// every record in used-checkpoints.json.
func storedRecords(t *testing.T, file string) object {
	t.Helper()
	content, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return object{}
	}
	if err != nil {
		t.Fatal(err)
	}
	var parsed object
	if err := json.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("%s is not a JSON object: %v\n%s", file, err, content)
	}
	if file == files.state {
		return getMap(parsed, "checkpoints")
	}
	return parsed
}

func plantNote(t *testing.T, sid string) string {
	t.Helper()
	ensureDir(files.checkpoints)
	note := filepath.Join(files.checkpoints, sid+".md")
	if err := os.WriteFile(note, []byte("# checkpoint of "+sid+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return note
}

func plantCheckpoint(t *testing.T, sid, cwd string, at int64) string {
	t.Helper()
	note := plantNote(t, sid)
	updateState(func(state object) {
		stateMap(state, "checkpoints")[sid] = object{"path": note, "cwd": cwd, "at": float64(at), "consumed": false}
	})
	return note
}

func TestAUsedCheckpointMovesToUsedCheckpointsJSON(t *testing.T) {
	sandboxFiles(t)
	project := t.TempDir()
	now := nowSec()
	note := plantCheckpoint(t, "used-one", project, now)
	plantCheckpoint(t, "open-one", project, now)

	result := updateState(func(state object) {
		getMap(getMap(state, "checkpoints"), "used-one")["consumed"] = true
	})
	if !getBool(getMap(getMap(result, "checkpoints"), "used-one"), "consumed", false) {
		t.Fatalf("the state updateState returned lost the checkpoint just marked used: %v", getMap(result, "checkpoints"))
	}
	inState, used := storedRecords(t, files.state), storedRecords(t, usedCheckpointsFile())
	if inState["used-one"] != nil || inState["open-one"] == nil {
		t.Fatalf("state.json should keep the checkpoint still to be used and only it: %v", inState)
	}
	if !getBool(toObject(used["used-one"]), "consumed", false) || used["open-one"] != nil {
		t.Fatalf("used-checkpoints.json should hold the used checkpoint and only it: %v", used)
	}
	if !getBool(checkpointRecord("used-one"), "consumed", false) || checkpointRecord("open-one") == nil || getBool(checkpointRecord("open-one"), "consumed", true) {
		t.Fatalf("a look at every checkpoint lost one: used %v, open %v", checkpointRecord("used-one"), checkpointRecord("open-one"))
	}
	if statSafe(note) == nil {
		t.Fatal("moving a used checkpoint deleted its note, which the session it was handed to may still read")
	}
	if due, want := numberOr(peekState(), "usedCheckpointsDue", 0), float64(now+checkpointTTLSeconds+1); due != want {
		t.Fatalf("state.json should say the used checkpoint expires at %.0f, not %.0f", want, due)
	}

	before, _ := os.ReadFile(usedCheckpointsFile())
	updateState(func(state object) {
		stateMap(state, "sessionLocale")["someone"] = object{"lang": "tr", "at": float64(now)}
	})
	if after, _ := os.ReadFile(usedCheckpointsFile()); !bytes.Equal(before, after) {
		t.Fatalf("a write with no checkpoint to move rewrote used-checkpoints.json:\n%s\n%s", before, after)
	}
}

func TestAHandedOnNoteStaysTheReceiversOnceItsCheckpointMoved(t *testing.T) {
	sandboxFiles(t)
	project := t.TempDir()
	note := plantCheckpoint(t, "giver", project, nowSec()-60)
	handOverCheckpoint("giver", "taker")
	if storedRecords(t, files.state)["giver"] != nil {
		t.Fatal("the checkpoint handed on stayed in state.json")
	}
	if !checkpointHandedTo("taker", note) {
		t.Fatal("the note handed on is no longer the receiver's once its checkpoint moved to used-checkpoints.json")
	}
	if checkpointHandedTo("bystander", note) {
		t.Fatal("the note counts as handed to a session it was not handed to")
	}
	plantCheckpoint(t, "giver", project, nowSec())
	if checkpointHandedTo("taker", note) {
		t.Fatal("a newer checkpoint the giver wrote to the same note counts as handed to the session that got the older one")
	}
}

func TestAStateJSONFromBeforeMovesItsUsedCheckpointsOnTheFirstWrite(t *testing.T) {
	sandboxFiles(t)
	project := t.TempDir()
	now := nowSec()
	usedNote, openNote := plantNote(t, "was-used"), plantNote(t, "still-open")
	// As 7.3 wrote it: indented, with a used checkpoint among the open ones.
	old := marshalPretty(object{
		"checkpoints": object{
			"was-used":   object{"path": usedNote, "cwd": project, "at": float64(now - 3600), "consumed": true, "handedTo": "next-session"},
			"still-open": object{"path": openNote, "cwd": project, "at": float64(now - 60), "consumed": false},
		},
		"sessionLocale": object{"someone": object{"lang": "tr", "at": float64(now)}},
	})
	if err := os.WriteFile(files.state, old, 0o600); err != nil {
		t.Fatal(err)
	}
	if !getBool(checkpointRecord("was-used"), "consumed", false) || !checkpointHandedTo("next-session", usedNote) {
		t.Fatal("a used checkpoint in a state.json from before is not seen until the first write")
	}

	updateState(func(state object) {
		stateMap(state, "sessionLocale")["another"] = object{"lang": "en", "at": float64(now)}
	})
	written, _ := os.ReadFile(files.state)
	if bytes.Contains(written, []byte("\n")) {
		t.Fatalf("state.json is still written indented:\n%s", written)
	}
	if storedRecords(t, files.state)["was-used"] != nil || storedRecords(t, usedCheckpointsFile())["was-used"] == nil {
		t.Fatalf("the used checkpoint did not move on the first write: state.json %v, used-checkpoints.json %v", storedRecords(t, files.state), storedRecords(t, usedCheckpointsFile()))
	}
	if !getBool(checkpointRecord("was-used"), "consumed", false) || !checkpointHandedTo("next-session", usedNote) || getBool(checkpointRecord("still-open"), "consumed", true) {
		t.Fatalf("a checkpoint is seen differently after the move: used %v, open %v", checkpointRecord("was-used"), checkpointRecord("still-open"))
	}
	if sessionLanguage(peekState(), "someone") != "tr" {
		t.Fatal("the rest of the state was lost in the move")
	}
	if backup, _ := os.ReadFile(files.stateBackup); !bytes.Equal(backup, old) {
		t.Fatalf("the backup is not state.json as it was before the write:\n%s", backup)
	}
}

func TestUsedCheckpointsExpireWithTheirNotesOnlyOnceDue(t *testing.T) {
	sandboxFiles(t)
	project := t.TempDir()
	now := nowSec()
	expired, recent := now-checkpointTTLSeconds-10, now-60
	staleNote, recentNote, reusedNote := plantNote(t, "stale"), plantNote(t, "recent"), plantNote(t, "reused")
	outside := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(outside, []byte("# not noctis's\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustWriteJSON(usedCheckpointsFile(), object{
		"stale":    object{"path": staleNote, "cwd": project, "at": float64(expired), "consumed": true},
		"recent":   object{"path": recentNote, "cwd": project, "at": float64(recent), "consumed": true},
		"reused":   object{"path": reusedNote, "cwd": project, "at": float64(expired), "consumed": true},
		"tampered": object{"path": outside, "cwd": project, "at": float64(expired), "consumed": true},
	})
	// "reused" has since written a new checkpoint, still to be used, to the same note.
	open := object{"reused": object{"path": reusedNote, "cwd": project, "at": float64(now), "consumed": false}}

	mustWriteJSON(files.state, object{"checkpoints": open, "usedCheckpointsDue": float64(now + 3600)})
	updateState(func(state object) {
		stateMap(state, "sessionLocale")["someone"] = object{"lang": "tr", "at": float64(now)}
	})
	if kept := storedRecords(t, usedCheckpointsFile()); len(kept) != 4 || statSafe(staleNote) == nil {
		t.Fatalf("used-checkpoints.json was pruned before its oldest record was due: %v", kept)
	}

	mustWriteJSON(files.state, object{"checkpoints": open, "usedCheckpointsDue": float64(expired + checkpointTTLSeconds + 1)})
	updateState(func(state object) {
		stateMap(state, "sessionLocale")["another"] = object{"lang": "en", "at": float64(now)}
	})
	if kept := storedRecords(t, usedCheckpointsFile()); len(kept) != 1 || kept["recent"] == nil {
		t.Fatalf("once due, used-checkpoints.json should keep only the record that has not expired: %v", kept)
	}
	if statSafe(staleNote) != nil {
		t.Error("the note of an expired used checkpoint was left behind")
	}
	if statSafe(reusedNote) == nil {
		t.Error("the note of an expired used checkpoint was deleted though a newer checkpoint of its session, still to be used, names it")
	}
	if statSafe(outside) == nil {
		t.Error("expiring a used checkpoint deleted a file outside the checkpoints folder that its record named")
	}
	if statSafe(recentNote) == nil {
		t.Error("the note of a used checkpoint that has not expired was deleted")
	}
	if due, want := numberOr(peekState(), "usedCheckpointsDue", 0), float64(recent+checkpointTTLSeconds+1); due != want {
		t.Fatalf("state.json should say the next record expires at %.0f, not %.0f", want, due)
	}
}

func TestUsedCheckpointsKeepTheNewerRecordOfASession(t *testing.T) {
	sandboxFiles(t)
	project := t.TempDir()
	now := nowSec()
	note := plantCheckpoint(t, "twice", project, now-600)
	consumeCheckpoint("twice")
	plantCheckpoint(t, "twice", project, now)
	handOverCheckpoint("twice", "receiver")
	if record := toObject(storedRecords(t, usedCheckpointsFile())["twice"]); numberOr(record, "at", 0) != float64(now) || getString(record, "handedTo") != "receiver" {
		t.Fatalf("the newer used checkpoint of a session did not replace the older one: %v", record)
	}
	updateState(func(state object) {
		stateMap(state, "checkpoints")["twice"] = object{"path": note, "cwd": project, "at": float64(now - 300), "consumed": true}
	})
	if record := toObject(storedRecords(t, usedCheckpointsFile())["twice"]); numberOr(record, "at", 0) != float64(now) {
		t.Fatalf("an older used checkpoint replaced the newer one of its session: %v", record)
	}
}

func TestUsedCheckpointsStayInStateJSONWhileTheirFileCannotBeOpened(t *testing.T) {
	sandboxFiles(t)
	project := t.TempDir()
	plantCheckpoint(t, "kept-here", project, nowSec())
	if err := os.MkdirAll(usedCheckpointsFile(), 0o700); err != nil {
		t.Fatal(err)
	}
	consumeCheckpoint("kept-here")
	if !getBool(toObject(storedRecords(t, files.state)["kept-here"]), "consumed", false) {
		t.Fatal("a used checkpoint left state.json though used-checkpoints.json could not be opened, and is lost")
	}
	if !getBool(checkpointRecord("kept-here"), "consumed", false) {
		t.Fatalf("the used checkpoint kept in state.json is not seen: %v", checkpointRecord("kept-here"))
	}
	if logged := string(readFileOrEmpty(files.log)) + string(readFileOrEmpty(files.errors)); !strings.Contains(logged, "used-checkpoints.json could not be opened") {
		t.Fatalf("nothing says why the used checkpoint stayed in state.json:\n%s", logged)
	}

	if err := os.Remove(usedCheckpointsFile()); err != nil {
		t.Fatal(err)
	}
	updateState(func(object) {})
	if storedRecords(t, files.state)["kept-here"] != nil || storedRecords(t, usedCheckpointsFile())["kept-here"] == nil {
		t.Fatal("the used checkpoint did not move once its file could be written")
	}
}

func TestHooksLeaveUsedCheckpointsJSONUnread(t *testing.T) {
	_, project := agentSessionSandbox(t, 20)
	now := nowSec()
	note := plantNote(t, "earlier")
	mustWriteJSON(usedCheckpointsFile(), object{
		"earlier": object{"path": note, "cwd": project, "at": float64(now - 60), "consumed": true, "handedTo": "quiet-1"},
	})
	updateState(func(state object) { state["usedCheckpointsDue"] = float64(now + 3600) })
	for _, payload := range []object{
		agentHookInput("SessionStart", "quiet-1", project, object{"source": "startup"}),
		agentHookInput("UserPromptSubmit", "quiet-1", project, object{"prompt": "fix the login bug"}),
		claudeToolPayload("PreToolUse", "quiet-1", project, "Edit", object{"file_path": filepath.Join(project, "main.go"), "old_string": "a", "new_string": "b"}),
		claudeToolPayload("PreToolUse", "quiet-1", project, "Bash", object{"command": "go test ./..."}),
		permissionRequest("quiet-1", project, "Read", filepath.Join(project, "main.go"), "default"),
		agentHookInput("Stop", "quiet-1", project, object{"stop_hook_active": false}),
	} {
		dropParsed(usedCheckpointsFile())
		hostHook(t, "claude", payload)
		if _, seen := parsedEntry(usedCheckpointsFile()); seen {
			t.Fatalf("a %s hook read used-checkpoints.json, which only the checkpoint command and a Read of a note handed on need", getString(payload, "hook_event_name"))
		}
	}
	if output := hostHook(t, "claude", permissionRequest("quiet-1", project, "Read", note, "default")); !permissionAllowed(output) {
		t.Fatalf("the Read of the note handed on was not allowed: %v", output)
	}
	if _, seen := parsedEntry(usedCheckpointsFile()); !seen {
		t.Fatal("the Read of the note handed on allowed it without looking in used-checkpoints.json")
	}
}

func TestTheCheckpointCommandFindsAUsedCheckpointInItsOwnFile(t *testing.T) {
	note := filepath.Join(t.TempDir(), "note.md")
	cliWrite(t, note, []byte("# a checkpoint already used\n"))
	account, env := cliStateAccount(t, object{"checkpoints": object{}})
	cliWrite(t, filepath.Join(account, pluginName, "used-checkpoints.json"), marshalState(object{
		cliLongSid: object{"path": note, "cwd": t.TempDir(), "at": float64(nowSec()), "consumed": true},
	}))
	run := runNoctisCLI(t, env, "checkpoint", "--sid", "1a2b3c4d")
	if run.code != 0 || cliFirstLine(run.stdout) != "NONE" || !strings.Contains(run.stdout, "(1 checkpoint(s) here are already used up") || strings.Contains(run.stdout, "a checkpoint already used") {
		t.Fatalf("checkpoint --sid of a used checkpoint should say NONE and that one is used up:\n%s", run)
	}
}
