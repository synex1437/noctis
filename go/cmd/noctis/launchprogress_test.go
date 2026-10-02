//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	recordCall     = `printf '%s\n' "$*" >> "$NOCTIS_TEST_CALLS"`
	answerLine     = `printf '{"type":"assistant","timestamp":"%s","message":{"role":"assistant","content":[{"type":"text","text":"Picking up the parser."}]}}\n' "$(date -u +%Y-%m-%dT%H:%M:%S.000Z)" >> "$NOCTIS_TEST_TRANSCRIPT"`
	promptLine     = `printf '{"type":"user","timestamp":"%s","message":{"role":"user","content":"carry on"}}\n' "$(date -u +%Y-%m-%dT%H:%M:%S.999Z)" >> "$NOCTIS_TEST_TRANSCRIPT"`
	apiFailureLine = `printf '{"type":"assistant","timestamp":"%s","isApiErrorMessage":true,"message":{"role":"assistant","content":[{"type":"text","text":"API Error: 529 overloaded_error"}]}}\n' "$(date -u +%Y-%m-%dT%H:%M:%S.999Z)" >> "$NOCTIS_TEST_TRANSCRIPT"`
)

func fakeClaude(lines ...string) string {
	return strings.Join(append(append([]string{"#!/bin/sh", recordCall}, lines...), ""), "\n")
}

func parkForRelaunch(t *testing.T, sid, kind, mode string) {
	t.Helper()
	now := float64(nowSec())
	cwd := t.TempDir()
	transcript := writeTranscriptAt(t, cwd, []string{userPromptLine(now-900, "fix the parser")}, now-900)
	t.Setenv("NOCTIS_TEST_TRANSCRIPT", transcript)
	wait := object{"kind": kind, "window": "five_hour", "label": "5h", "used": float64(95), "threshold": float64(92), "hit": "threshold",
		"startedAt": now - 600, "until": now - 10, "resumeAt": now - 5, "cwd": cwd, "transcript": transcript, "launchMode": mode,
		"inHook": true, "heartbeat": now - 600, "wakeAttemptedAt": now - wakeGraceSeconds(loadConfig()) - 8, "earlyTriggeredAt": now - 4}
	if kind == "stopfailure" {
		wait["window"], wait["label"], wait["overload"], wait["attempt"] = "unknown", "overloaded", true, float64(1)
		wait["until"], wait["startedAt"] = now-600, now-600
	}
	updateState(func(state object) { stateMap(state, "waits")[sid] = wait })
	statusReadingFrom(sid, nowSec(), 3, now+18000, 20, now+3*86400)
}

func relaunchConfig(resume object) {
	config := releaseConfig()
	config["wait"] = object{"earlyResetPollMinutes": float64(5), "heartbeatGraceSeconds": float64(1)}
	config["resume"] = resume
	mustWriteJSON(files.config, config)
}

func relaunchSandboxWith(t *testing.T, script string) string {
	t.Helper()
	calls := takeoverSandbox(t)
	relaunchConfig(object{"mode": "window", "prompt": "carry on"})
	writeScript(t, filepath.Join(filepath.Dir(calls), "claude"), script)
	return calls
}

func waitOf(sid string) object {
	return getMap(getMap(readState(), "waits"), sid)
}

func TestARelaunchThatEndsWithoutAnsweringKeepsTheWaitForARetry(t *testing.T) {
	cases := []struct {
		name   string
		script string
	}{
		{"claude could not resume the session", fakeClaude(`echo "No conversation found with session ID" >&2`, "exit 1")},
		{"all it wrote was the prompt and an API error", fakeClaude(promptLine, apiFailureLine, "exit 1")},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sid := fmt.Sprintf("nothing%d", index+1)
			calls := relaunchSandboxWith(t, tc.script)
			parkForRelaunch(t, sid, "batch", "headless")
			before := float64(nowSec())

			resumeWait(sid, "")

			if got := launchesOf(calls, sid); got != 1 {
				t.Fatalf("the runner launched the session %d time(s), want 1", got)
			}
			wait := waitOf(sid)
			if wait == nil {
				t.Fatalf("a relaunch that ended without the session answering was taken for a resume and the wait was deleted (journal %v)", journaledFor(sid))
			}
			if numberOr(wait, "launchAttempts", 0) != 1 || getString(wait, "hit") != "relaunch" {
				t.Fatalf("the kept wait does not say it retries a relaunch: %v", wait)
			}
			if retry := numberOr(wait, "resumeAt", 0); retry < before+retryDelaySeconds(releaseConfig(), 1) {
				t.Fatalf("the retry is due at %v, before the first step of the retry ladder (%v s after %v)", retry, retryDelaySeconds(releaseConfig(), 1), before)
			}
			if started, until := numberOr(wait, "startedAt", 0), numberOr(wait, "until", 0); started <= before-1 || until != started {
				t.Fatalf("the retry still dates from the old pause (startedAt %v, until %v, relaunch at %v)", started, until, before)
			}
			for _, stale := range []string{"wakeAttemptedAt", "earlyTriggeredAt", "waking"} {
				if _, kept := wait[stale]; kept {
					t.Fatalf("the retry kept %s from the pause it replaced: %v", stale, wait)
				}
			}
			if getBool(wait, "inHook", true) {
				t.Fatal("the retry still claims a hook sleeps on it")
			}
			if !slices.Contains(journaledFor(sid), "launch-no-progress") {
				t.Fatalf("the relaunch that did nothing was not journaled: %v", journaledFor(sid))
			}
			if getMap(getMap(readState(), "launchFailures"), sid) != nil {
				t.Fatal("a relaunch that will be retried was reported as a failure to resume by hand")
			}
		})
	}
}

func TestARelaunchIsDoneWhenTheSessionAnsweredOrExitedCleanly(t *testing.T) {
	cases := []struct {
		name   string
		script string
	}{
		{"it answered and then failed", fakeClaude("sleep 1", answerLine, "exit 1")},
		{"it answered and exited cleanly", fakeClaude("sleep 1", answerLine, "exit 0")},
		{"it exited cleanly without writing", fakeClaude("exit 0")},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sid := fmt.Sprintf("done%d", index+1)
			calls := relaunchSandboxWith(t, tc.script)
			parkForRelaunch(t, sid, "batch", "headless")

			resumeWait(sid, "")

			if got := launchesOf(calls, sid); got != 1 {
				t.Fatalf("the runner launched the session %d time(s), want 1", got)
			}
			if wait := waitOf(sid); wait != nil {
				t.Fatalf("the relaunch was retried: %v", wait)
			}
			if slices.Contains(journaledFor(sid), "launch-no-progress") {
				t.Fatalf("the relaunch was journaled as having done nothing: %v", journaledFor(sid))
			}
		})
	}
}

func TestTheRetryRunnerRelaunchesAgainInsteadOfTakingTheFailedRunForTheSessionContinuing(t *testing.T) {
	for _, kind := range []string{"batch", "stopfailure"} {
		t.Run(kind, func(t *testing.T) {
			sid := "retry-" + kind
			calls := relaunchSandboxWith(t, fakeClaude(promptLine, apiFailureLine, "exit 1"))
			parkForRelaunch(t, sid, kind, "headless")

			resumeWait(sid, "")
			if waitOf(sid) == nil {
				t.Fatal("the first relaunch did nothing and the wait was not kept")
			}
			time.Sleep(1100 * time.Millisecond)
			statusReadingFrom(sid, nowSec(), 3, float64(nowSec()+18000), 20, float64(nowSec()+3*86400))
			if reason := earlyRelease(releaseConfig(), sid, waitOf(sid), 0, true); reason != "" {
				t.Fatalf("a wait that retries a relaunch was released early (%s) by a status line showing room, which it always shows once the window reset", reason)
			}
			updateState(func(state object) {
				getMap(getMap(state, "waits"), sid)["resumeAt"] = float64(nowSec() - 1)
			})

			resumeWait(sid, "")

			if got := launchesOf(calls, sid); got != 2 {
				t.Fatalf("the retry runner launched the session %d time(s) in total, want 2: it read the prompt and error the failed relaunch wrote as the session going on (journal %v)", got, journaledFor(sid))
			}
			if wait := waitOf(sid); wait == nil || numberOr(wait, "launchAttempts", 0) != 2 {
				t.Fatalf("the second relaunch that did nothing was not counted: %v", wait)
			}
		})
	}
}

func TestAnAnswerInTheSecondAFailedRelaunchEndedKeepsItsRetryFromRelaunchingTheSessionAgain(t *testing.T) {
	for _, kind := range []string{"batch", "stopfailure"} {
		t.Run(kind, func(t *testing.T) {
			sid := "late-answer-" + kind
			calls := relaunchSandboxWith(t, fakeClaude(promptLine, apiFailureLine, "exit 1"))
			previousOffset := timeOffset
			t.Cleanup(func() { timeOffset = previousOffset })
			parkForRelaunch(t, sid, kind, "headless")

			resumeWait(sid, "")
			wait := waitOf(sid)
			if wait == nil || getString(wait, "hit") != "relaunch" {
				t.Fatalf("the first relaunch did nothing and its retry was not parked: %v", wait)
			}
			answer := marshalCompact(object{"type": "assistant", "timestamp": transcriptStamp(numberOr(wait, "until", 0) - 0.001),
				"message": object{"role": "assistant", "content": []any{object{"type": "text", "text": "Picking up the parser."}}}})
			transcript, err := os.OpenFile(getString(wait, "transcript"), os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			_, err = transcript.Write(append(answer, '\n'))
			transcript.Close()
			if err != nil {
				t.Fatal(err)
			}
			timeOffset += int64(numberOr(wait, "resumeAt", 0)) - nowSec() + 1
			now := float64(nowSec())
			statusReadingFrom(sid, nowSec(), 3, now+18000, 20, now+3*86400)

			resumeWait(sid, "")

			if got := launchesOf(calls, sid); got != 1 {
				t.Fatalf("the session answered in the second its failed relaunch ended, after the runner had looked, and the retry relaunched it again: %d launch(es) in total, want 1 (journal %v)", got, journaledFor(sid))
			}
			if left := waitOf(sid); left != nil {
				t.Fatalf("the retry kept the pause of a session that went on: %v", left)
			}
			if !slices.Contains(journaledFor(sid), "skip-launch") {
				t.Fatalf("the retry ended the pause without saying why in the journal: %v", journaledFor(sid))
			}
		})
	}
}

func TestARelaunchThatNeverAnswersGivesUpAfterTheLastAttempt(t *testing.T) {
	sid := "never-answers"
	calls := relaunchSandboxWith(t, fakeClaude("exit 1"))
	parkForRelaunch(t, sid, "batch", "headless")
	updateState(func(state object) {
		getMap(getMap(state, "waits"), sid)["launchAttempts"] = float64(stopFailureMaxAttempts - 1)
	})

	resumeWait(sid, "")

	if got := launchesOf(calls, sid); got != 1 {
		t.Fatalf("the runner launched the session %d time(s), want 1", got)
	}
	if wait := waitOf(sid); wait != nil {
		t.Fatalf("after %d relaunches that did nothing the wait was kept again: %v", stopFailureMaxAttempts, wait)
	}
	if getMap(getMap(readState(), "launchFailures"), sid) == nil || !slices.Contains(journaledFor(sid), "launch-failed") {
		t.Fatalf("giving up was not reported for status, doctor and the journal: %v", journaledFor(sid))
	}
	if loggedTimes(fmt.Sprintf("ended %d times", stopFailureMaxAttempts)) != 1 {
		t.Fatal("giving up was not logged with the number of relaunches")
	}
}

func TestAWindowWhoseLauncherIsSlowToWriteItsPidIsNotLaunchedAgainHeadless(t *testing.T) {
	sid := "slow-pid"
	date, err := exec.LookPath("date")
	if err != nil {
		t.Fatalf("date is not on PATH: %v", err)
	}
	_, bin, calls := terminalSandbox(t)
	if err := os.Symlink(date, filepath.Join(bin, "date")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	t.Setenv("NOCTIS_NO_EARLY_TRIGGER", "")
	t.Setenv(handoffEnv, "")
	// This terminal creates the pid file empty, as the launcher's own redirection does before it
	// writes the pid, and runs the launcher a second later.
	relaunchConfig(object{"mode": "window", "prompt": "carry on", "terminal": `s={script}; exec 3< "$s"; : > "${s%.sh}.pid"; sleep 1; sh <&3`})
	writeStub(t, bin, "claude", fakeClaude(answerLine, "exit 0"))
	parkForRelaunch(t, sid, "batch", "")

	resumeWait(sid, "")

	content, _ := os.ReadFile(calls)
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "-p ") {
			t.Fatalf("the session was launched headless beside its window: %q", line)
		}
	}
	if got := launchesOf(calls, sid); got != 1 {
		t.Fatalf("the runner launched the session %d time(s), want 1", got)
	}
	if wait := waitOf(sid); wait != nil {
		t.Fatalf("the session answered in its window, but its wait was kept for a retry: %v (journal %v)", wait, journaledFor(sid))
	}
}

func TestAWindowThatClosesAtOnceWithoutTouchingTheSessionIsRetried(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		retry bool
	}{
		{"claude exited before it touched the session", []string{"exit 1"}, true},
		// The runner tells a window that answered by the transcript's time, which the system can
		// stamp a clock tick (or on some file systems a second) late; a real claude takes longer
		// than that to answer, and a stand-in that answered at once could be stamped before the
		// launch.
		{"claude wrote to the session before the window closed", []string{"sleep 1", answerLine, "exit 0"}, false},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sid := fmt.Sprintf("window%d", index+1)
			date, err := exec.LookPath("date")
			if err != nil {
				t.Fatalf("date is not on PATH: %v", err)
			}
			_, bin, calls := terminalSandbox(t)
			if err := os.Symlink(date, filepath.Join(bin, "date")); err != nil {
				t.Fatal(err)
			}
			t.Setenv("NOCTIS_NO_TASKS", "1")
			t.Setenv("NOCTIS_NO_SCHEDULE", "1")
			t.Setenv("NOCTIS_NO_EARLY_TRIGGER", "")
			t.Setenv(handoffEnv, "")
			relaunchConfig(object{"mode": "window", "prompt": "carry on", "terminal": "sh {script}"})
			writeStub(t, bin, "claude", fakeClaude(tc.lines...))
			parkForRelaunch(t, sid, "batch", "")

			resumeWait(sid, "")

			if got := launchesOf(calls, sid); got != 1 {
				t.Fatalf("the runner launched the session %d time(s), want 1", got)
			}
			if wait := waitOf(sid); (wait != nil) != tc.retry {
				t.Fatalf("retry=%t, wait after the window closed: %v (journal %v)", tc.retry, wait, journaledFor(sid))
			}
		})
	}
}
