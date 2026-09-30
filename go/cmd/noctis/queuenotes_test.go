package main

import (
	"fmt"
	"strings"
	"testing"
)

const noteText = "use SQLite for the cache: no server to run"

func TestQueueNoteKeepsADecisionAndQueueStatusGivesItBack(t *testing.T) {
	cfg, project, _, _ := deferSandbox(t)
	if printed := queueCommand(t, cfg, project, "note", "use", "SQLite", "for", "the", "cache:", "no", "server", "to", "run"); !strings.Contains(printed, T("queue.noted", "TASKS.md")) {
		t.Fatalf("noctis queue note printed %q", printed)
	}
	status := queueCommand(t, cfg, project, "status")
	if !strings.Contains(status, T("queue.notes", 1)) || !strings.Contains(status, "  "+noteText) {
		t.Fatalf("noctis queue status does not give the decision back:\n%s", status)
	}
	var facts object
	if err := jsonUnmarshal([]byte(queueCommand(t, cfg, project, "status", "--json")), &facts); err != nil {
		t.Fatal(err)
	}
	decisions := getList(facts, "decisions")
	if len(decisions) != 1 || getString(toObject(decisions[0]), "text") != noteText || numberOr(toObject(decisions[0]), "at", 0) == 0 {
		t.Fatalf("noctis queue status --json gives decisions %v", decisions)
	}
}

func TestAQueueFileKeepsItsLastDecisionsAndDropsAFileLeftForAMonth(t *testing.T) {
	cfg, project, _, path := deferSandbox(t)
	mustWriteJSON(queueNotesFile(), object{"old": object{"path": "/gone/TASKS.md", "at": float64(nowSec() - queueTrustTTLSeconds - 60), "notes": []any{[]any{float64(1), "an old decision"}}}})
	for index := 1; index <= queueNotesKept+2; index++ {
		queueCommand(t, cfg, project, "note", fmt.Sprintf("decision %d", index))
	}
	notes := readJSON(queueNotesFile())
	kept := queueNotesOf(notes, path)
	if len(kept) != queueNotesKept || kept[0].text != "decision 3" || kept[len(kept)-1].text != fmt.Sprintf("decision %d", queueNotesKept+2) {
		t.Fatalf("the file keeps %d decisions from %q, want the last %d", len(kept), kept[0].text, queueNotesKept)
	}
	if notes["old"] != nil {
		t.Fatal("the decisions of a file nothing was noted on for a month were kept")
	}
}

func TestTheQueueDirectiveAsksForDecisionsAndGivesTheLastOnes(t *testing.T) {
	cfg, project, _, _ := deferSandbox(t)
	start := object{"hook_event_name": "SessionStart", "source": "startup", "session_id": "qn1", "cwd": project}
	context := getString(getMap(hookOutput(t, onSessionStart, start, cfg), "hookSpecificOutput"), "additionalContext")
	if !strings.Contains(context, pluginName+` queue note "<the decision, and why>" --file '`) || strings.Contains(context, "Decisions noted so far") {
		t.Fatalf("the queue directive does not ask Claude to note its decisions, or names some before any was noted:\n%s", context)
	}
	queueCommand(t, cfg, project, "note", noteText)
	context = getString(getMap(hookOutput(t, onSessionStart, start, cfg), "hookSpecificOutput"), "additionalContext")
	if want := "Decisions noted so far (the last 1 of 1): " + noteText + "."; !strings.Contains(context, want) {
		t.Fatalf("the queue directive of the next session does not give %q:\n%s", want, context)
	}
}

func TestTheDigestNamesTheDecisionsNotedSinceTheLastOne(t *testing.T) {
	cfg, project, _, path, _ := digestSandbox(t, "21:00")
	now := nowSec()
	mustWriteJSON(queueNotesFile(), object{queueTrustKey(path): object{"path": path, "at": float64(now - 60), "notes": []any{
		[]any{float64(now - 7200), "an earlier decision"},
		[]any{float64(now - 60), noteText},
	}}})
	baseline := object{"at": float64(now - 3600), "queues": object{}}
	body := buildDigest(cfg, readState(), currentUsage(now), baseline, now).body
	if want := "- " + T("digest.decided", formatTime(float64(now-3600)), 1, noteText); !strings.Contains(body, want) {
		t.Fatalf("the digest does not say %q:\n%s", want, body)
	}
	if strings.Contains(body, "an earlier decision") {
		t.Fatalf("the digest names a decision the one before already had:\n%s", body)
	}
	first := buildDigest(cfg, readState(), currentUsage(now), nil, now).body
	if want := "- " + T("digest.decided", formatTime(float64(now-86400)), 2, "an earlier decision; "+noteText); !strings.Contains(first, want) {
		t.Fatalf("the first digest does not name the decisions of the last day as %q:\n%s", want, first)
	}
	_ = project
}

func TestAQueueNoteKeepsNoControlCharactersAndStaysBounded(t *testing.T) {
	cfg, project, _, path := deferSandbox(t)
	queueCommand(t, cfg, project, "note", "\x1b[31mred\x1b[0m", strings.Repeat("long ", 100))
	notes := queueNotesOf(readJSON(queueNotesFile()), path)
	if len(notes) != 1 || strings.ContainsRune(notes[0].text, 0x1b) || len([]rune(notes[0].text)) > queueNoteMaxChars+1 {
		t.Fatalf("the note was kept as %+v", notes)
	}
}
