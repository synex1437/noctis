package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func permissionRequest(sid, cwd, tool, file, mode string) object {
	return object{
		"hook_event_name": "PermissionRequest", "session_id": sid, "cwd": cwd, "transcript_path": filepath.Join(cwd, "transcript.jsonl"),
		"permission_mode": mode, "tool_name": tool, "tool_input": object{"file_path": file},
	}
}

func permissionAllowed(output object) bool {
	specific := getMap(output, "hookSpecificOutput")
	return getString(specific, "hookEventName") == "PermissionRequest" && getString(getMap(specific, "decision"), "behavior") == "allow"
}

func checklistFor(t *testing.T, sid, project string) string {
	t.Helper()
	path := startAutoQueue(sid, project, []string{"add input validation to the signup form", "write tests for the payments module", "update the README for the new CLI flags"}, nowSec())
	if path == "" {
		t.Fatal("the checklist from the prompt was not written")
	}
	return path
}

func linkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("this Windows account may not create symbolic links (Developer Mode or an elevated account is needed): %v", err)
		}
		t.Fatal(err)
	}
}

func TestClaudeReadsAndTicksTheChecklistNoctisWroteWithoutAPrompt(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	path := checklistFor(t, "pr1", project)
	for _, tool := range []string{"Read", "Edit", "MultiEdit", "Write"} {
		for _, mode := range []string{"default", "acceptEdits", "auto"} {
			if output := hostHook(t, "claude", permissionRequest("pr1", project, tool, path, mode)); !permissionAllowed(output) {
				t.Fatalf("Claude Code asked to approve %s of the session's own checklist in %s mode and noctis did not answer: %v; the checklist sits under the protected .claude folder, so that prompt stops an unattended job", tool, mode, output)
			}
		}
	}
	if !strings.Contains(string(readFileOrEmpty(files.decisions)), `"action":"allow-queue-file"`) {
		t.Fatal("the approval was not journaled")
	}
}

func TestNoctisAnswersOnlyForTheSessionsOwnQueueFile(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	path := checklistFor(t, "pr2", project)
	other := checklistFor(t, "pr3", project)
	spelled := strings.Join([]string{filepath.Dir(path), "..", filepath.Base(filepath.Dir(path)), filepath.Base(path)}, string(filepath.Separator))
	shell := permissionRequest("pr2", project, "Bash", "", "acceptEdits")
	shell["tool_input"] = object{"command": "rm " + path}
	for _, request := range []struct {
		name  string
		input object
	}{
		{"another session's checklist", permissionRequest("pr2", project, "Edit", other, "acceptEdits")},
		{"Claude Code's settings", permissionRequest("pr2", project, "Edit", files.settings, "acceptEdits")},
		{"the checklist spelled through ..", permissionRequest("pr2", project, "Edit", spelled, "acceptEdits")},
		{"a relative path", permissionRequest("pr2", project, "Edit", filepath.Base(path), "acceptEdits")},
		{"plan mode", permissionRequest("pr2", project, "Edit", path, "plan")},
		{"a shell command naming the checklist", shell},
	} {
		if output := hostHook(t, "claude", request.input); permissionAllowed(output) {
			t.Fatalf("noctis approved %s: %v", request.name, output)
		}
	}
}

func TestAChecklistSwappedForALinkIsNotApproved(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	path := checklistFor(t, "pr4", project)
	outside := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(outside, []byte("- [ ] private\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	linkOrSkip(t, outside, path)
	if output := hostHook(t, "claude", permissionRequest("pr4", project, "Edit", path, "acceptEdits")); permissionAllowed(output) {
		t.Fatalf("noctis approved an edit through a link that replaced the checklist: %v", output)
	}
}

func TestATrustedQueueFileUnderDotClaudeIsTickedWithoutAPrompt(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	dir := filepath.Join(project, ".claude")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	queuePath := filepath.Join(dir, "TASKS.md")
	if err := os.WriteFile(queuePath, []byte("# q\n- [ ] migrate the users table\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output := hostHook(t, "claude", permissionRequest("pr5", project, "Edit", queuePath, "acceptEdits")); permissionAllowed(output) {
		t.Fatalf("noctis approved an edit of a .claude/TASKS.md nobody trusted: %v", output)
	}
	trustQueueFile(queuePath, true)
	if output := hostHook(t, "claude", permissionRequest("pr5", project, "Edit", queuePath, "acceptEdits")); !permissionAllowed(output) {
		t.Fatalf("Claude Code asked to approve a tick in the trusted .claude/TASKS.md and noctis did not answer: %v", output)
	}
}

func TestObserveModeOnlyJournalsTheApproval(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	path := checklistFor(t, "pr6", project)
	defer func(previous bool) { observing = previous }(observing)
	observing = true
	if output := hostHook(t, "claude", permissionRequest("pr6", project, "Edit", path, "acceptEdits")); permissionAllowed(output) {
		t.Fatalf("observe mode approved a permission request: %v", output)
	}
	if !strings.Contains(string(readFileOrEmpty(files.decisions)), `"action":"would-allow-queue-file"`) {
		t.Fatal("observe mode did not journal the approval it would have given")
	}
}

func readFileOrEmpty(path string) []byte {
	data, _ := os.ReadFile(path)
	return data
}

func handedResumeNote(t *testing.T, cfg object, project, paused, fresh, source string) string {
	t.Helper()
	t.Setenv("NOCTIS_UPDATE_URL", "off")
	note := buildCheckpoint(object{"session_id": paused, "cwd": project}, "paused at the usage limit", "", cfg)
	if note == "" {
		t.Fatal("no checkpoint was written")
	}
	start := hostHook(t, "claude", object{"hook_event_name": "SessionStart", "source": source, "session_id": fresh, "cwd": project, "transcript_path": filepath.Join(project, fresh+".jsonl")})
	if context := getString(getMap(start, "hookSpecificOutput"), "additionalContext"); !strings.Contains(context, note) {
		t.Fatalf("the session started with %s was not handed the resume note %s: %v", source, note, start)
	}
	return note
}

func TestAFreshSessionReadsTheResumeNoteItWasHandedWithoutAPrompt(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	for _, start := range []struct{ source, paused, fresh string }{
		{"startup", "cp-paused1", "cp-fresh"},
		{"clear", "cp-paused2", "cp-cleared"},
	} {
		note := handedResumeNote(t, cfg, project, start.paused, start.fresh, start.source)
		for _, mode := range []string{"default", "acceptEdits", "auto"} {
			if output := hostHook(t, "claude", permissionRequest(start.fresh, project, "Read", note, mode)); !permissionAllowed(output) {
				t.Fatalf("Claude Code asked to approve the Read of the resume note noctis handed the session at %s, in %s mode, and noctis did not answer: %v; the note sits under the account's .claude folder, so claude -p refuses that Read and the session goes on without the note", start.source, mode, output)
			}
		}
	}
	if !strings.Contains(string(readFileOrEmpty(files.decisions)), `"action":"allow-checkpoint-note"`) {
		t.Fatal("the approval was not journaled")
	}
}

func TestNoctisAnswersOnlyForTheResumeNoteTheSessionWasHanded(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	note := handedResumeNote(t, cfg, project, "cp-paused", "cp-fresh", "startup")
	unhanded := buildCheckpoint(object{"session_id": "cp-away", "cwd": t.TempDir()}, "paused at the usage limit", "", cfg)
	if unhanded == "" {
		t.Fatal("no second checkpoint was written")
	}
	spelled := strings.Join([]string{filepath.Dir(note), "..", filepath.Base(filepath.Dir(note)), filepath.Base(note)}, string(filepath.Separator))
	for _, request := range []struct {
		name  string
		input object
	}{
		{"an Edit of the note", permissionRequest("cp-fresh", project, "Edit", note, "acceptEdits")},
		{"a MultiEdit of the note", permissionRequest("cp-fresh", project, "MultiEdit", note, "acceptEdits")},
		{"a Write over the note", permissionRequest("cp-fresh", project, "Write", note, "acceptEdits")},
		{"a Read of the note by a session it was not handed to", permissionRequest("cp-stranger", project, "Read", note, "acceptEdits")},
		{"a Read of the note by the session that paused", permissionRequest("cp-paused", project, "Read", note, "acceptEdits")},
		{"a Read of a checkpoint no session was handed", permissionRequest("cp-fresh", project, "Read", unhanded, "acceptEdits")},
		{"a Read of the note spelled through ..", permissionRequest("cp-fresh", project, "Read", spelled, "acceptEdits")},
		{"a Read of the note in plan mode", permissionRequest("cp-fresh", project, "Read", note, "plan")},
		{"a Read of Claude Code's settings", permissionRequest("cp-fresh", project, "Read", files.settings, "acceptEdits")},
	} {
		if output := hostHook(t, "claude", request.input); permissionAllowed(output) {
			t.Fatalf("noctis approved %s: %v", request.name, output)
		}
	}
}

func TestAResumeNoteSwappedForALinkIsNotApproved(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	note := handedResumeNote(t, cfg, project, "cp-paused", "cp-fresh", "startup")
	outside := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(outside, []byte("private\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(note); err != nil {
		t.Fatal(err)
	}
	linkOrSkip(t, outside, note)
	if output := hostHook(t, "claude", permissionRequest("cp-fresh", project, "Read", note, "acceptEdits")); permissionAllowed(output) {
		t.Fatalf("noctis approved a Read through a link that replaced the resume note: %v", output)
	}
}

func TestObserveModeOnlyJournalsTheResumeNoteApproval(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	note := handedResumeNote(t, cfg, project, "cp-paused", "cp-fresh", "startup")
	defer func(previous bool) { observing = previous }(observing)
	observing = true
	if output := hostHook(t, "claude", permissionRequest("cp-fresh", project, "Read", note, "acceptEdits")); permissionAllowed(output) {
		t.Fatalf("observe mode approved a permission request: %v", output)
	}
	if !strings.Contains(string(readFileOrEmpty(files.decisions)), `"action":"would-allow-checkpoint-note"`) {
		t.Fatal("observe mode did not journal the approval it would have given")
	}
}
