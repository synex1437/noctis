package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func transcriptStamp(epoch float64) string {
	return time.UnixMilli(int64(epoch * 1000)).UTC().Format("2006-01-02T15:04:05.000Z")
}

func userPromptLine(at float64, text string) string {
	return string(marshalCompact(object{"type": "user", "timestamp": transcriptStamp(at), "message": object{"role": "user", "content": text}}))
}

func apiErrorLine(at float64) string {
	return string(marshalCompact(object{"type": "assistant", "timestamp": transcriptStamp(at), "isApiErrorMessage": true,
		"message": object{"role": "assistant", "content": []any{object{"type": "text", "text": "API Error: 529 overloaded_error"}}}}))
}

func toolResultLine(at float64) string {
	return string(marshalCompact(object{"type": "user", "timestamp": transcriptStamp(at),
		"message": object{"role": "user", "content": []any{object{"type": "tool_result", "tool_use_id": "toolu_1", "content": "ok"}, object{"type": "text", "text": "<system-reminder>the file changed</system-reminder>"}}}}))
}

func writeTranscriptAt(t *testing.T, dir string, lines []string, modified float64) string {
	t.Helper()
	transcript := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(transcript, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	moment := time.UnixMilli(int64(modified * 1000))
	if err := os.Chtimes(transcript, moment, moment); err != nil {
		t.Fatal(err)
	}
	return transcript
}

func TestTheFailureItselfDoesNotCountAsTheSessionContinuing(t *testing.T) {
	calls := takeoverSandbox(t)
	now := float64(nowSec())
	failedAt := now - 600
	cases := []struct {
		name     string
		lines    []string
		modified float64
		launches int
	}{
		{"only the API error was written after the failure", []string{userPromptLine(failedAt-100, "fix the parser"), apiErrorLine(failedAt + 0.4)}, failedAt + 0.4, 1},
		{"the user typed a prompt after the failure", []string{userPromptLine(failedAt-100, "fix the parser"), apiErrorLine(failedAt + 0.4), userPromptLine(failedAt+60, "go on")}, failedAt + 60, 0},
		{"a tool result carrying a reminder was written after the failure", []string{userPromptLine(failedAt-100, "fix the parser"), apiErrorLine(failedAt + 0.4), toolResultLine(failedAt + 2)}, failedAt + 2, 1},
		{"the transcript is not JSON and has not changed since the failure settled", []string{"not json"}, failedAt + 1, 1},
	}
	for index, tc := range cases {
		sid := fmt.Sprintf("sf%d", index+1)
		cwd := t.TempDir()
		transcript := writeTranscriptAt(t, cwd, tc.lines, tc.modified)
		updateState(func(state object) {
			stateMap(state, "waits")[sid] = object{"kind": "stopfailure", "window": "unknown", "label": "overloaded", "overload": true, "attempt": float64(1), "used": nil, "threshold": nil,
				"until": failedAt, "startedAt": failedAt, "resumeAt": failedAt + 30, "cwd": cwd, "transcript": transcript, "launchMode": "headless"}
		})
		statusReadingFrom(sid, nowSec(), 3, now+18000, 20, now+3*86400)

		resumeWait(sid, "")

		if got := launchesOf(calls, sid); got != tc.launches {
			t.Fatalf("%s: the runner relaunched the session %d time(s), want %d (journal %v)", tc.name, got, tc.launches, journaledFor(sid))
		}
	}
}

func TestWhatTheSessionWroteInTheSecondItFailedDoesNotCountAsItGoingOn(t *testing.T) {
	answer := func(at float64) string {
		return string(marshalCompact(object{"type": "assistant", "timestamp": transcriptStamp(at),
			"message": object{"role": "assistant", "content": []any{object{"type": "text", "text": "The parser is fixed; running the tests next."}}}}))
	}
	prompt := func(at float64) string { return userPromptLine(at, "now run the tests") }
	cases := []struct {
		name    string
		failure object
		written func(float64) string
		window  string
	}{
		{"an overload right after an answer", claudeFailure("overloaded", "", "API Error: Repeated 529 Overloaded errors"), answer, "unknown"},
		{"a failure the limits do not explain, on the prompt", claudeFailure("rate_limit", "", "API Error: Request rejected (429)"), prompt, "unknown"},
		{"Fable's limit, on the prompt", claudeFailure("rate_limit", "", "API Error: Fable limit reached"), prompt, "fable"},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := takeoverSandbox(t)
			previousHost, previousOffset := activeHost, timeOffset
			t.Cleanup(func() { activeHost, timeOffset = previousHost, previousOffset })
			activeHost = "claude"
			t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
			config := releaseConfig()
			config["models"] = object{"primary": "claude-fable-5", "fallback": "claude-opus-5"}
			config["resume"] = object{"mode": "window", "prompt": "carry on"}
			mustWriteJSON(files.config, config)
			sid := fmt.Sprintf("same-second%d", index+1)
			cwd := t.TempDir()
			second := float64(nowSec())
			if tc.window == "fable" {
				mustWriteJSON(files.settings, object{"model": "claude-fable-5"})
				writeFableBucket(99, second, second+3*86400)
			}
			lines := []string{userPromptLine(second-60, "fix the parser"), tc.written(second + 0.001), apiErrorLine(second + 0.002)}
			failure := agentHookInput("StopFailure", sid, cwd, tc.failure)
			failure["transcript_path"] = writeTranscriptAt(t, cwd, lines, second+0.002)

			hookOutput(t, onStopFailure, failure, loadConfig())
			wait := pendingWait(sid)
			if wait == nil || getString(wait, "window") != tc.window {
				t.Fatalf("the failure was not parked as a %s retry: %v", tc.window, wait)
			}
			timeOffset += int64(numberOr(wait, "resumeAt", 0)) - nowSec() + 5
			now := float64(nowSec())
			statusReadingFrom(sid, nowSec(), 3, now+18000, 20, now+3*86400)
			resumeWait(sid, "")

			if got := launchesOf(calls, sid); got != 1 {
				t.Fatalf("what the session wrote in the second it failed, before the failure, was taken for the session going on after it: the retry relaunched it %d time(s), want 1, so the session stays stopped (journal %v)", got, journaledFor(sid))
			}
		})
	}
}
