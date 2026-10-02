package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

func TestARunnerThatEndsAPauseWithoutARelaunchSaysWhyInTheJournal(t *testing.T) {
	cases := []struct {
		name  string
		usage bool
		setup func(sid string, wait object)
	}{
		{"Claude Code's own auto-continue resumed the session", true, func(sid string, _ object) {
			updateState(func(state object) {
				stateMap(state, "autoResume")[sid] = object{"type": "quota_auto_resume_fired", "at": float64(nowSec())}
			})
		}},
		{"resume.mode is none", true, func(string, object) {
			config := readJSON(files.config)
			config["resume"] = object{"mode": "none"}
			mustWriteJSON(files.config, config)
		}},
		{"its last try found no usage data", false, func(_ string, wait object) {
			wait["attempts"] = float64(stopFailureMaxAttempts - 1)
		}},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := takeoverSandbox(t)
			previousHost := activeHost
			t.Cleanup(func() { activeHost = previousHost })
			activeHost = "claude"
			t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
			sid := fmt.Sprintf("ended%d", index+1)
			now := float64(nowSec())
			failedAt := now - 600
			cwd := t.TempDir()
			transcript := writeTranscriptAt(t, cwd, []string{userPromptLine(failedAt-100, "fix the parser"), apiErrorLine(failedAt + 0.4)}, failedAt+0.4)
			wait := object{"kind": "stopfailure", "window": "unknown", "label": "overloaded", "overload": true, "attempt": float64(1), "used": nil, "threshold": nil,
				"until": failedAt + 1, "startedAt": failedAt, "resumeAt": failedAt + 30, "cwd": cwd, "transcript": transcript, "launchMode": "headless"}
			tc.setup(sid, wait)
			updateState(func(state object) { stateMap(state, "waits")[sid] = wait })
			if tc.usage {
				statusReadingFrom(sid, nowSec(), 3, now+18000, 20, now+3*86400)
			}

			resumeWait(sid, "")

			if got := launchesOf(calls, sid); got != 0 {
				t.Fatalf("the runner relaunched the session %d time(s), want none", got)
			}
			if left := pendingWait(sid); left != nil {
				t.Fatalf("the pause was kept: %v", left)
			}
			if journal := journaledFor(sid); !slices.Contains(journal, "skip-launch") {
				t.Fatalf("the runner ended the pause without relaunching the session and left no reason in the journal, so noctis why does not say what became of it: %v", journal)
			}
		})
	}
}

func TestWhatTheSessionWroteBeforeTheClockWentBackDoesNotCountAsItGoingOn(t *testing.T) {
	answer := func(at float64) string {
		return string(marshalCompact(object{"type": "assistant", "timestamp": transcriptStamp(at),
			"message": object{"role": "assistant", "content": []any{object{"type": "text", "text": "The parser is fixed; running the tests next."}}}}))
	}
	resumeLater := func(sid string, at float64) {
		timeOffset += int64(at) - nowSec() + 5
		now := float64(nowSec())
		statusReadingFrom(sid, nowSec(), 3, now+18000, 20, now+3*86400)
	}
	relaunchSandboxAt := func(t *testing.T) string {
		calls := takeoverSandbox(t)
		previousHost, previousOffset := activeHost, timeOffset
		t.Cleanup(func() { activeHost, timeOffset = previousHost, previousOffset })
		activeHost = "claude"
		t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
		config := releaseConfig()
		config["resume"] = object{"mode": "window", "prompt": "carry on"}
		config["wait"] = object{"maxInHookMinutes": float64(1)}
		mustWriteJSON(files.config, config)
		return calls
	}

	t.Run("a turn that failed on an overload", func(t *testing.T) {
		calls := relaunchSandboxAt(t)
		sid, cwd := "clock-back-overload", t.TempDir()
		second := float64(nowSec())
		failure := agentHookInput("StopFailure", sid, cwd, claudeFailure("overloaded", "", "API Error: Repeated 529 Overloaded errors"))
		failure["transcript_path"] = writeTranscriptAt(t, cwd, []string{userPromptLine(second-60, "fix the parser"), answer(second + 300), apiErrorLine(second + 0.002)}, second+300)

		hookOutput(t, onStopFailure, failure, loadConfig())
		wait := pendingWait(sid)
		if wait == nil {
			t.Fatalf("the overload parked no retry (journal %v)", journaledFor(sid))
		}
		resumeLater(sid, numberOr(wait, "resumeAt", 0))
		resumeWait(sid, "")

		if got := launchesOf(calls, sid); got != 1 {
			t.Fatalf("an answer written before the clock went back five minutes, stamped after the failure, was taken for the session going on after it: the retry relaunched it %d time(s), want 1, so the session stays stopped (journal %v)", got, journaledFor(sid))
		}
	})

	t.Run("a pause at the limit", func(t *testing.T) {
		calls := relaunchSandboxAt(t)
		sid, cwd := "clock-back-limit", t.TempDir()
		now := float64(nowSec())
		reset := now + 600
		statusReadingFrom(sid, nowSec(), 93, reset, 20, now+3*86400)
		batch := agentHookInput("PostToolBatch", sid, cwd, nil)
		batch["transcript_path"] = writeTranscriptAt(t, cwd, []string{userPromptLine(now-60, "fix the parser"), answer(reset + 120)}, reset+120)
		plan := &waitPlan{window: "five_hour", label: windowLabel("five_hour"), used: 93, threshold: 92, until: reset, hit: "threshold"}

		enforceWait("batch", batch, loadConfig(), decision{wait: plan, usage: currentUsage(nowSec())})
		wait := pendingWait(sid)
		if wait == nil || wait["inHook"] == true {
			t.Fatalf("the pause was not handed to its runner: %v", wait)
		}
		resumeLater(sid, numberOr(wait, "resumeAt", 0))
		resumeWait(sid, "")

		if got := launchesOf(calls, sid); got != 1 {
			t.Fatalf("an answer written before the clock went back past the reset was taken for the session going on after it: the runner relaunched it %d time(s), want 1, so the session stays stopped (journal %v)", got, journaledFor(sid))
		}
	})

	wakes := []struct {
		name, back string
		ahead      float64
	}{
		{"a queue wake", "two minutes", 120},
		{"a queue wake due before what the session wrote", "three hours", 3 * 3600},
	}
	for index, tc := range wakes {
		t.Run(tc.name, func(t *testing.T) {
			cfg, project, frontend, calls := queueWakeRelaunchSandbox(t)
			defer func(previous int64) { timeOffset = previous }(timeOffset)
			section(cfg, "wait")["maxInHookMinutes"] = float64(1)
			deferPayUntil(t, cfg, project, "2h")
			sid, now := fmt.Sprintf("clock-back-wake%d", index+1), float64(nowSec())
			stop := stopInput(sid, frontend)
			stop["transcript_path"] = writeTranscriptAt(t, t.TempDir(), []string{userPromptLine(now-60, "connect the payment provider"), answer(now + tc.ahead)}, now+tc.ahead)

			stopHookOutput(t, stop, cfg)
			wake := getMap(getMap(readState(), "queueWakes"), sid)
			if wake == nil {
				t.Fatalf("a stop on a deferral that ends in two hours set no wake (journal %v)", journaledFor(sid))
			}
			resumeLater(sid, numberOr(wake, "at", 0)+25)
			fireQueueWake(sid)

			if got := launchesOf(calls, sid); got != 1 {
				t.Fatalf("an answer written before the clock went back %s, stamped after the wake was set, was taken for the session going on: the wake relaunched it %d time(s), want 1, so the queue stays idle (journal %v)", tc.back, got, journaledFor(sid))
			}
		})
	}
}

func TestOnlyTheLinesWrittenAfterThePauseWasRecordedAreReadAsWrittenSince(t *testing.T) {
	transcript := filepath.Join(t.TempDir(), "session.jsonl")
	before, after := "{\"n\":1}\n{\"n\":2}\n", "{\"n\":3}\n{\"n\":4}\n"
	padding := strings.Repeat("{\"pad\":\""+strings.Repeat("x", 1000)+"\"}\n", 300)
	cases := []struct {
		name    string
		content string
		size    int
		want    []string
	}{
		{"the size recorded between two lines", before + after, len(before), []string{`{"n":3}`, `{"n":4}`}},
		{"the size recorded in the middle of a line", before + after, len(before) + 3, []string{`{"n":4}`}},
		{"the size recorded at the end", before + after, len(before + after), nil},
		{"no size recorded", before + after, 0, []string{`{"n":1}`, `{"n":2}`, `{"n":3}`, `{"n":4}`}},
		{"a transcript shorter than the size recorded", before + after, len(before+after) + 100, []string{`{"n":1}`, `{"n":2}`, `{"n":3}`, `{"n":4}`}},
		{"the size recorded inside the tail of a longer transcript", before + padding + after, len(before + padding), []string{`{"n":3}`, `{"n":4}`}},
	}
	for _, tc := range cases {
		if err := os.WriteFile(transcript, []byte(tc.content), 0o600); err != nil {
			t.Fatal(err)
		}
		lines, first, ok := transcriptTailSince(object{"transcript": transcript, "transcriptSize": float64(tc.size)})
		if !ok {
			t.Fatalf("%s: the transcript was not read", tc.name)
		}
		var got []string
		for _, line := range lines[first:] {
			if line != "" {
				got = append(got, line)
			}
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: read %q as written since the pause, want %q", tc.name, got, tc.want)
		}
	}
	if err := os.WriteFile(transcript, []byte(before+padding+after), 0o600); err != nil {
		t.Fatal(err)
	}
	if lines, first, _ := transcriptTailSince(object{"transcript": transcript, "transcriptSize": float64(len(before))}); first != 0 || len(lines) < 2 || lines[len(lines)-2] != `{"n":4}` {
		t.Errorf("a size recorded before the tail starts left out lines of the tail: first %d of %d", first, len(lines))
	}
}
