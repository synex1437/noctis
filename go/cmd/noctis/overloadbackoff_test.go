package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// h1OverloadSandbox is limitSandbox with the alarm on and stand-ins for the desktop notifiers, so
// every notification reaches guard.log and nothing shows up on the machine running the tests.
func h1OverloadSandbox(t *testing.T) (object, string) {
	t.Helper()
	cfg, project, _ := limitSandbox(t, object{"alarm": object{"enabled": true}}, 20, 40)
	if !isWindows {
		bin := t.TempDir()
		for _, notifier := range []string{"notify-send", "osascript"} {
			writeScript(t, filepath.Join(bin, notifier), "#!/bin/sh\nexit 0\n")
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	return cfg, project
}

func TestARetryPromptOrTheEndOfARelaunchDoesNotRestartTheOverloadBackoff(t *testing.T) {
	cfg, project := h1OverloadSandbox(t)
	sid := "h1-overload"
	failure := agentHookInput("StopFailure", sid, project, claudeFailure("overloaded", "", "API Error: Repeated 529 Overloaded errors"))
	delays := []float64{}
	overloaded := func() {
		t.Helper()
		hookOutput(t, onStopFailure, failure, cfg)
		wait := pendingWait(sid)
		if !getBool(wait, "overload", false) {
			t.Fatalf("the overload was not filed as a backoff: %v", wait)
		}
		delays = append(delays, numberOr(wait, "resumeAt", 0)-numberOr(wait, "startedAt", 0))
	}
	overloaded()
	// A claude -p relaunch ends with SessionEnd after every retry.
	hookOutput(t, onSessionEnd, agentHookInput("SessionEnd", sid, project, object{"reason": "other"}), cfg)
	overloaded()
	// The retry itself arrives as a prompt: the wake's or the relaunch's text.
	hookOutput(t, onUserPromptSubmit, promptInput(sid, project, T("overload.wakeMessage", "overloaded", "2", durationText(delays[1]))), cfg)
	overloaded()
	if attempts := numberOr(getMap(getMap(readState(), "overload"), sid), "attempts", 0); attempts != 3 {
		t.Fatalf("the third overload in a row counts as attempt %v, want 3: the episode was started over, so the backoff never grows and maxTotalMinutes is never spent", attempts)
	}
	for i := 1; i < len(delays); i++ {
		if delays[i] <= delays[i-1] {
			t.Fatalf("the overload backoff did not grow from one attempt to the next: %v s", delays)
		}
	}
	notice, _, _ := strings.Cut(T("overload.notify", "overloaded", "\x00"), "\x00")
	if told := loggedTimes("notify: " + pluginName + " — " + notice); told != 1 {
		t.Fatalf("one overload episode notified %d times, want once", told)
	}
	hookOutput(t, onPostToolBatch, mainBatch(sid, project, shellCall("Bash", "go test ./...")), cfg)
	if episode := getMap(getMap(readState(), "overload"), sid); episode != nil {
		t.Fatalf("a batch of tools after the retry did not end the overload episode: %v", episode)
	}
}

func TestObserveModeOnlyJournalsTheGiveUpsOfFailuresItNeverRetried(t *testing.T) {
	cfg, project := h1OverloadSandbox(t)
	previous := observing
	t.Cleanup(func() { observing = previous })
	observing = true
	now := float64(nowSec())
	// The person sent each turn again by hand: an overload that has lasted past maxTotalMinutes, and a
	// failure noctis cannot place that came back as often as noctis retries one.
	updateState(func(state object) {
		stateMap(state, "overload")["observed-overload"] = object{"firstAt": now - 3*3600, "lastAt": now - 60, "attempts": float64(7)}
		stateMap(state, "failureRetries")["observed-failure"] = object{"retries": float64(stopFailureMaxAttempts), "firstAt": now - 600, "lastAt": now - 10, "error": "unknown"}
	})
	hookOutput(t, onStopFailure, agentHookInput("StopFailure", "observed-overload", project, claudeFailure("overloaded", "", "API Error: Repeated 529 Overloaded errors")), cfg)
	hookOutput(t, onStopFailure, agentHookInput("StopFailure", "observed-failure", project, object{"error_type": "unknown", "error_message": "API Error: Request timed out"}), cfg)

	for sid, action := range map[string]string{"observed-overload": "overload-giveup", "observed-failure": "retry-giveup"} {
		if journaledEntry(sid, action) != nil || journaledEntry(sid, "would-"+action) == nil {
			t.Errorf("observe mode retried nothing for %s, yet did not just journal that it would give up: %v", sid, journaledFor(sid))
		}
	}
	if told := loggedTimes("notify: " + pluginName); told != 0 {
		t.Errorf("observe mode notified %d times that it gave up on retries it never made: %q", told, tailFileLines(files.log, 20))
	}
	if failure := getMap(getMap(readState(), "launchFailures"), "observed-failure"); failure != nil {
		t.Errorf("observe mode left noctis status a give-up on retries it never made: %v", failure)
	}
}

func TestAGiveUpTakesBackTheRetryARunnerPutOffToTheReset(t *testing.T) {
	for _, give := range []struct {
		name, action string
		failure      func(sid, project string) object
		spent        func(state object, sid string, now float64)
	}{
		{"overload", "overload-giveup", func(sid, project string) object {
			return agentHookInput("StopFailure", sid, project, claudeFailure("overloaded", "", "API Error: Repeated 529 Overloaded errors"))
		}, func(state object, sid string, now float64) {
			stateMap(state, "overload")[sid] = object{"firstAt": now - 3*3600, "lastAt": now - 60, "attempts": float64(28)}
		}},
		{"failure", "retry-giveup", func(sid, project string) object {
			return agentHookInput("StopFailure", sid, project, object{"error_type": "unknown", "error_message": "API Error: Request timed out"})
		}, func(state object, sid string, now float64) {
			stateMap(state, "failureRetries")[sid] = object{"retries": float64(stopFailureMaxAttempts), "firstAt": now - 600, "lastAt": now - 10, "error": "unknown"}
		}},
		{"full context", "retry-giveup", contextFullStop, func(state object, sid string, now float64) {
			stateMap(state, "contextFulls")[sid] = object{"count": float64(stopFailureMaxAttempts), "lastAt": now - 600}
		}},
	} {
		t.Run(give.name, func(t *testing.T) {
			cfg, project := h1OverloadSandbox(t)
			sid := "put-off-" + strings.ReplaceAll(give.name, " ", "-")
			hookOutput(t, onStopFailure, give.failure(sid, project), cfg)
			wait := pendingWait(sid)
			if wait == nil {
				t.Fatalf("the failure left no retry: %v", journaledFor(sid))
			}
			// When the retry came, the runner found a limit still in force and put it off to the reset.
			now := float64(nowSec())
			if !rescheduleOwnWait(sid, numberOr(wait, "startedAt", 0), func(record object) {
				record["window"], record["label"], record["until"], record["resumeAt"] = "five_hour", windowLabel("five_hour"), now+3*3600, now+3*3600
				record["startedAt"] = now
			}) {
				t.Fatal("the retry could not be put off to the reset")
			}
			// The session failed again before the reset, with the retries spent.
			updateState(func(state object) { give.spent(state, sid, now) })
			hookOutput(t, onStopFailure, give.failure(sid, project), cfg)
			if journaledEntry(sid, give.action) == nil {
				t.Fatalf("noctis did not give up on the failure: %v", journaledFor(sid))
			}
			if wait := pendingWait(sid); wait != nil {
				t.Fatalf("noctis gave up and said it leaves the session stopped, yet the retry put off to the reset still starts it then: %v", wait)
			}
		})
	}
}
