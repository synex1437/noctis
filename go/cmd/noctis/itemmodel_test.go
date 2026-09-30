package main

import (
	"strings"
	"testing"
)

func TestItemModelReadsTheModelTagOfAnItem(t *testing.T) {
	for text, want := range map[string]string{
		"design the schema (opus)":               "opus",
		"fix the typo in the README (Sonnet)":    "sonnet",
		"(HAIKU) rename the variables":           "haiku",
		"write the proof (fable) first":          "fable",
		"charge the first customer (after #pay)": "",
		"compare opus and sonnet":                "",
		"read the (opus-4) notes":                "",
		"check in with the client (human)":       "",
	} {
		if got := itemModel(text); got != want {
			t.Errorf("itemModel(%q) = %q, want %q", text, got, want)
		}
	}
}

// modelQueueSandbox is deferSandbox with the item a session takes next tagged for model.
func modelQueueSandbox(t *testing.T, model string) (object, string, string) {
	t.Helper()
	cfg, project, frontend, _ := deferSandbox(t)
	path := writeQueueFile(t, project, strings.Replace(deferQueue, "migrate the users table", "migrate the users table ("+model+")", 1))
	trustQueueFile(path, true)
	return cfg, project, frontend
}

// runsOn has the status line say that the session sid runs on model, with tokens in its context.
func runsOn(sid, model string, tokens float64) {
	now := float64(nowSec())
	mustWriteJSON(files.usage, object{
		"updatedAt": now,
		"five_hour": object{"used": float64(10), "resetsAt": now + 3600},
		"seven_day": object{"used": float64(20), "resetsAt": now + 3*86400},
		"sessions":  object{sid: object{"model": model, "updatedAt": now, "contextTokens": tokens}},
	})
}

// journaledEntry is the last entry of the decision journal for sid with action.
func journaledEntry(sid, action string) object {
	var last object
	for _, line := range tailFileLines(files.decisions, 1000) {
		var entry object
		if jsonUnmarshalObject([]byte(line), &entry) == nil && getString(entry, "sid") == sid && getString(entry, "action") == action {
			last = entry
		}
	}
	return last
}

func TestTheStopHookHandsAnItemTaggedForAnotherModelToASubagentOfIt(t *testing.T) {
	cfg, _, frontend := modelQueueSandbox(t, "sonnet")
	runsOn("im1", "claude-opus-5-5", 20000)
	reason := getString(stopHookOutput(t, stopInput("im1", frontend), cfg), "reason")
	if want := `The next item is tagged (sonnet) and this session runs on opus: hand it to a general-purpose subagent with model "sonnet"`; !strings.Contains(reason, want) {
		t.Fatalf("the continuation does not hand the (sonnet) item to a sonnet subagent:\n%s", reason)
	}
	if model := getString(journaledEntry("im1", "continue-queue"), "itemModel"); model != "sonnet" {
		t.Fatalf("the continuation is journaled with itemModel %q, want sonnet", model)
	}
	runsOn("im1", "claude-sonnet-5-5", 20000)
	if reason := getString(stopHookOutput(t, stopInput("im1", frontend), cfg), "reason"); strings.Contains(reason, "is tagged (sonnet)") {
		t.Fatalf("a session on sonnet is told to hand the (sonnet) item to a subagent:\n%s", reason)
	}
}

func TestTheModelNoteTakesThePlaceOfTheLargeContextNote(t *testing.T) {
	cfg, _, frontend := modelQueueSandbox(t, "haiku")
	runsOn("im2", "claude-opus-5-5", 150000)
	reason := getString(stopHookOutput(t, stopInput("im2", frontend), cfg), "reason")
	if !strings.Contains(reason, `subagent with model "haiku"`) || strings.Contains(reason, "context already holds") {
		t.Fatalf("with a large context the continuation should name the item's model once, and not a subagent of the session's own:\n%s", reason)
	}
}

func TestAnItemTaggedForTheSessionsOwnModelKeepsTheLargeContextNote(t *testing.T) {
	cfg, _, frontend := modelQueueSandbox(t, "opus")
	runsOn("im3", "claude-opus-5-5", 150000)
	reason := getString(stopHookOutput(t, stopInput("im3", frontend), cfg), "reason")
	if strings.Contains(reason, "is tagged (opus)") || !strings.Contains(reason, "context already holds") {
		t.Fatalf("an item tagged for the session's own model should leave the large context note as it is:\n%s", reason)
	}
}

func TestTheQueueDirectiveSaysWhatAModelTagMeans(t *testing.T) {
	cfg, project, _, _ := deferSandbox(t)
	start := object{"hook_event_name": "SessionStart", "source": "startup", "session_id": "im4", "cwd": project}
	if context := getString(getMap(hookOutput(t, onSessionStart, start, cfg), "hookSpecificOutput"), "additionalContext"); strings.Contains(context, "is for that model") {
		t.Fatalf("a queue without model tags is told what they mean: %q", context)
	}
	path := writeQueueFile(t, project, strings.Replace(deferQueue, "migrate the users table", "migrate the users table (Opus)", 1))
	trustQueueFile(path, true)
	if context := getString(getMap(hookOutput(t, onSessionStart, start, cfg), "hookSpecificOutput"), "additionalContext"); !strings.Contains(context, "An item tagged (opus), (sonnet), (haiku) or (fable) is for that model") {
		t.Fatalf("the queue directive does not say what a model tag means: %q", context)
	}
}
