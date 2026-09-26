package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func contextReading(sid string, window object) {
	recordStatusline(object{"session_id": sid, "context_window": window}, nowSec(), true)
}

func bigContext() object {
	return object{"context_window_size": float64(200000), "used_percentage": float64(71),
		"current_usage": object{"input_tokens": float64(1200), "output_tokens": float64(900), "cache_creation_input_tokens": float64(3000), "cache_read_input_tokens": float64(138000)}}
}

func TestTheStatusLineKeepsHowManyTokensTheContextHolds(t *testing.T) {
	sandboxFiles(t)
	cases := []struct {
		name   string
		window object
		want   float64
		known  bool
	}{
		{"the last request's input", bigContext(), 142200, true},
		{"the used share of the window", object{"context_window_size": float64(1000000), "used_percentage": float64(15)}, 150000, true},
		{"before the first request", object{"context_window_size": float64(200000), "current_usage": nil}, 0, false},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sid := "ctx" + string(rune('1'+index))
			contextReading(sid, tc.window)
			got, known := getNumber(getMap(getMap(readJSON(files.usage), "sessions"), sid), "contextTokens")
			if known != tc.known || got != tc.want {
				t.Fatalf("contextTokens = %v (recorded %t), want %v (recorded %t)", got, known, tc.want, tc.known)
			}
		})
	}
}

func TestAQueueItemGoesToAFreshSubagentOnceTheContextIsBig(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	writeJobFile(t, project, "deneme.md", "- add a login page\n- add a logout button\n")
	startedChecklist(t, cfg, "fc1", project, "deneme.md")
	contextReading("fc1", bigContext())
	output := stopHookOutput(t, stopInput("fc1", project), cfg)
	reason := getString(output, "reason")
	if getString(output, "decision") != "block" || !strings.Contains(reason, "add a login page") {
		t.Fatalf("the Stop hook did not continue the queue: %v", output)
	}
	if !strings.Contains(reason, "fresh general-purpose subagent") || !strings.Contains(reason, "142k tokens") {
		t.Fatalf("with 142k tokens of context the next item was not handed to a fresh subagent: %q", reason)
	}
}

func TestASmallContextOrTheSwitchOffKeepsQueueItemsInTheSession(t *testing.T) {
	cases := []struct {
		name   string
		window object
		above  any
	}{
		{"40k tokens of context", object{"context_window_size": float64(200000), "used_percentage": float64(20)}, nil},
		{"no context reading yet", nil, nil},
		{"queue.subagentAboveTokens 0", bigContext(), float64(0)},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, project := queueTrustSandbox(t, false)
			if tc.above != nil {
				section(cfg, "queue")["subagentAboveTokens"] = tc.above
			}
			sid := "fcs" + string(rune('1'+index))
			writeJobFile(t, project, "deneme.md", "- add a login page\n- add a logout button\n")
			startedChecklist(t, cfg, sid, project, "deneme.md")
			if tc.window != nil {
				contextReading(sid, tc.window)
			}
			output := stopHookOutput(t, stopInput(sid, project), cfg)
			if reason := getString(output, "reason"); !strings.Contains(reason, "add a login page") || strings.Contains(reason, "fresh general-purpose subagent") {
				t.Fatalf("want the item continued in the session itself, got %q", reason)
			}
		})
	}
}

func TestAFreshSessionIsNotHandedAnotherSessionsCheckpoint(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	other := filepath.Join(t.TempDir(), "other-session.md")
	if err := os.WriteFile(other, []byte("# checkpoint of another session\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updateState(func(state object) {
		stateMap(state, "checkpoints")["other-session"] = object{"path": other, "cwd": project, "at": float64(nowSec() - 60), "consumed": false}
		stateMap(state, "freshStarts")["fresh-one"] = object{"from": "paused-one", "at": float64(nowSec() - 5)}
	})
	transcript := filepath.Join(t.TempDir(), "fresh-one.jsonl")
	start := func(sid, transcript string) string {
		output := hookOutput(t, onSessionStart, object{"hook_event_name": "SessionStart", "session_id": sid, "source": "startup", "cwd": project, "transcript_path": transcript}, cfg)
		return getString(getMap(output, "hookSpecificOutput"), "additionalContext")
	}
	if context := start("fresh-one", transcript); strings.Contains(context, other) {
		t.Fatalf("the fresh session the runner started with its own handoff note was handed another session's checkpoint too: %q", context)
	}
	state := readState()
	if getBool(getMap(getMap(state, "checkpoints"), "other-session"), "consumed", false) {
		t.Fatal("the fresh session used up another session's checkpoint")
	}
	if got := getString(getMap(getMap(state, "freshStarts"), "fresh-one"), "transcript"); got != transcript {
		t.Fatalf("the fresh session's transcript was not noted for the runner that watches it: %q", got)
	}
	if context := start("new-one", filepath.Join(t.TempDir(), "new-one.jsonl")); !strings.Contains(context, other) {
		t.Fatalf("a session the user opened was not handed the checkpoint any more: %q", context)
	}
}
