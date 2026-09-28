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
