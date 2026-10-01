package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const payItem = `"connect the payment provider #pay"`

// queueWakeSandbox is deferSandbox with no scheduler, so a queue wake is recorded as "manual", and a
// Stop hook that waits for a deferral looks at the queue every few milliseconds.
func queueWakeSandbox(t *testing.T) (object, string, string, string) {
	t.Helper()
	cfg, project, frontend, path := deferSandbox(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	previous := deferralPoll
	t.Cleanup(func() { deferralPoll = previous })
	deferralPoll = 20 * time.Millisecond
	return cfg, project, frontend, path
}

// deferPayUntil defers the payment item until the --until given and ticks every item not held by
// it, so only that deferral stands between the queue and its next item.
func deferPayUntil(t *testing.T, cfg object, project, until string) {
	t.Helper()
	queueCommand(t, cfg, project, "defer", "payment", "--reason", deferReason, "--until", until)
	writeQueueFile(t, project, deferTicked("migrate the users table", "write the release notes"))
}

// queueWakeRelaunchSandbox is queueWakeSandbox with takeoverSandbox's stand-in claude, which writes
// every launch to the file it returns.
func queueWakeRelaunchSandbox(t *testing.T) (object, string, string, string) {
	t.Helper()
	cfg, project, frontend, _ := queueWakeSandbox(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(claudeConfigEnv, "")
	if err := os.Unsetenv(claudeConfigEnv); err != nil {
		t.Fatal(err)
	}
	files.configDir = filepath.Join(home, ".claude")
	files.resumeLog = filepath.Join(files.guardDir, "resume-output.log")
	t.Setenv("NOCTIS_NO_TERMINAL", "1")
	t.Setenv("NOCTIS_NO_EARLY_TRIGGER", "")
	t.Setenv(handoffEnv, "")
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls.log")
	t.Setenv("NOCTIS_TEST_CALLS", calls)
	if isWindows {
		writeScript(t, filepath.Join(bin, "claude.cmd"), "@(echo %1 %2 %3)>>\"%NOCTIS_TEST_CALLS%\"\r\n@exit /b 0\r\n")
	} else {
		writeScript(t, filepath.Join(bin, "claude"), "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$NOCTIS_TEST_CALLS\"\n")
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	mustWriteJSON(files.config, object{"resume": object{"mode": "window", "prompt": "carry on"}})
	return cfg, project, frontend, calls
}

// armedQueueWake stops sid on a deferral too far off for the hook to wait, and returns the wake that
// sets and what the stop said.
func armedQueueWake(t *testing.T, cfg object, project, frontend, sid string) (object, object) {
	t.Helper()
	section(cfg, "wait")["maxInHookMinutes"] = float64(1)
	deferPayUntil(t, cfg, project, "2h")
	output := stopHookOutput(t, stopInput(sid, frontend), cfg)
	wake := getMap(getMap(readState(), "queueWakes"), sid)
	if wake == nil {
		t.Fatalf("a stop on a deferral that ends in two hours set no wake (journal %v, output %v)", journaledFor(sid), output)
	}
	return wake, output
}

func TestTheStopHookWaitsForADeferralThatEndsSoonAndGoesOnWithItsItem(t *testing.T) {
	cfg, project, frontend, path := queueWakeSandbox(t)
	deferPayUntil(t, cfg, project, "2s")
	until := numberOr(queueWakeRecordOf(t, path), "until", 0)
	if view := queueSnapshot(path); len(view.items) != 0 || view.freeAt <= 0 || len(view.freed) != 1 {
		t.Fatalf("with only the deferred item and what waits for it left, the view has %d eligible, frees %d item(s) at %v", len(view.items), len(view.freed), view.freeAt)
	}
	started := time.Now()
	output := stopHookOutput(t, stopInput("qw1", frontend), cfg)
	reason := getString(output, "reason")
	if getString(output, "decision") != "block" || !strings.Contains(reason, `("connect the payment provider #pay")`) {
		t.Fatalf("once the deferral ended, the queue did not go on with its item in the same session: %v", output)
	}
	if waited := time.Since(started); waited < 500*time.Millisecond {
		t.Fatalf("the Stop hook answered after %v, before the deferral ended", waited)
	}
	if !strings.Contains(reason, "The deferral of these items has ended") || !strings.Contains(reason, deferReason) {
		t.Fatalf("the continuation did not tell Claude which deferral ended and what it waited on: %q", reason)
	}
	if message, want := getString(output, "systemMessage"), T("queue.deferralEnded", payItem); !strings.Contains(message, want) {
		t.Fatalf("the user was not told the deferral ended: %q, want it to hold %q", message, want)
	}
	if told := loggedTimes(T("queue.deferralWaitNotify", "TASKS.md", formatTime(until), payItem)); told != 1 {
		t.Fatalf("the user was told %d times that the queue waits for the deferral, want once", told)
	}
	if wakes := getMap(readState(), "queueWakes"); len(wakes) != 0 {
		t.Fatalf("the wake set in case the hook was cut off outlived the wait: %v", wakes)
	}
	actions := journaledFor("qw1")
	for _, want := range []string{"wait-deferral", "deferral-ended", "continue-queue"} {
		if !slices.Contains(actions, want) {
			t.Fatalf("the journal has no %q entry: %v", want, actions)
		}
	}
}

// queueWakeRecordOf is the deferral record of the payment item in the queue at path.
func queueWakeRecordOf(t *testing.T, path string) object {
	t.Helper()
	return getMap(getMap(getMap(readState(), "queueDefer"), queueTrustKey(path)), queueItemDigest("connect the payment provider #pay"))
}

func TestAPauseWhileTheStopHookWaitsForADeferralLetsTheSessionStop(t *testing.T) {
	cfg, project, frontend, _ := queueWakeSandbox(t)
	// The deferral lasts a minute so the pause lands while the hook still waits for it. One of 2s
	// ends one to two seconds on, counted from the whole second, and on a busy Windows runner the
	// pause landed after that.
	deferPayUntil(t, cfg, project, "1m")
	answered, paused := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(paused)
		for !slices.Contains(journaledFor("qw2"), "wait-deferral") {
			select {
			case <-answered:
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
		updateState(func(next object) { next["disabledUntil"] = float64(nowSec() + 3600) })
	}()
	output := stopHookOutput(t, stopInput("qw2", frontend), cfg)
	close(answered)
	<-paused
	if getString(output, "decision") == "block" {
		t.Fatalf("the queue went on although noctis was paused while it waited: %v", output)
	}
	if actions := journaledFor("qw2"); !slices.Contains(actions, "wait-deferral") || !slices.Contains(actions, "allow-stop") {
		t.Fatalf("the journal does not show the wait and the stop the pause let go: %v", actions)
	}
	if wake := getMap(getMap(readState(), "queueWakes"), "qw2"); wake == nil {
		t.Fatal("the wake that takes the queue up after the pause was dropped with the stop")
	}
}

func TestADeferralTooFarOffForTheHookSetsAWakeAndSaysWhen(t *testing.T) {
	cfg, project, frontend, path := queueWakeSandbox(t)
	wake, output := armedQueueWake(t, cfg, project, frontend, "qw3")
	if getString(output, "decision") == "block" {
		t.Fatalf("the session was kept going with only a deferred item left: %v", output)
	}
	until := numberOr(queueWakeRecordOf(t, path), "until", 0)
	if at := numberOr(wake, "at", 0); at != until {
		t.Fatalf("the wake is set for %v, want the end of the deferral, %v", at, until)
	}
	if method := getString(getMap(wake, "scheduled"), "method"); method != "manual" {
		t.Fatalf("with no scheduler the wake's runner is %q, want manual", method)
	}
	if getString(wake, "queue") != path || getString(wake, "holder") != "" {
		t.Fatalf("the wake does not name the queue, or names a hook that waits for it: %v", wake)
	}
	if message, want := getString(output, "systemMessage"), T("queue.deferredWake", formatTime(until)); !strings.Contains(message, want) {
		t.Fatalf("the stop said %q, want it to say when the session goes on: %q", message, want)
	}
	if actions := journaledFor("qw3"); slices.Contains(actions, "wait-deferral") || !slices.Contains(actions, "arm-queue-wake") {
		t.Fatalf("a deferral past wait.maxInHookMinutes was waited for in the hook, or set no wake: %v", actions)
	}
	status := capturedStdout(t, runStatus)
	if want := T("status.waitLine", shortSid("qw3"), "queue", "TASKS.md", formatTime(until)) + " [manual]"; !strings.Contains(status, want) {
		t.Fatalf("noctis status does not show the queue wake as %q:\n%s", want, status)
	}
}

func TestAQueueWakeResumesTheStoppedSessionOnceItsDeferralEnds(t *testing.T) {
	cfg, project, frontend, calls := queueWakeRelaunchSandbox(t)
	defer func(previous int64) { timeOffset = previous }(timeOffset)
	wake, _ := armedQueueWake(t, cfg, project, frontend, "qw4")
	timeOffset += int64(numberOr(wake, "at", 0)) - nowSec() + 30
	now := float64(nowSec())
	statusReadingFrom("qw4", nowSec(), 3, now+18000, 20, now+3*86400)

	fireQueueWake("qw4")

	if got := launchesOf(calls, "qw4"); got != 1 {
		t.Fatalf("the wake relaunched the session %d time(s), want once (journal %v)", got, journaledFor("qw4"))
	}
	if actions := journaledFor("qw4"); !slices.Contains(actions, "resume") {
		t.Fatalf("the journal does not show the wake resuming the session: %v", actions)
	}
	if wake := getMap(getMap(readState(), "queueWakes"), "qw4"); wake != nil {
		t.Fatalf("the wake is still set after it resumed the session: %v", wake)
	}
}

func TestAQueueWakeWaitsForADeferralThatWasMoved(t *testing.T) {
	cfg, project, frontend, path := queueWakeSandbox(t)
	defer func(previous int64) { timeOffset = previous }(timeOffset)
	wake, _ := armedQueueWake(t, cfg, project, frontend, "qw5")
	queueCommand(t, cfg, project, "defer", "payment", "--reason", deferReason, "--until", "5h")
	later := numberOr(queueWakeRecordOf(t, path), "until", 0)
	timeOffset += int64(numberOr(wake, "at", 0)) - nowSec() + 30

	fireQueueWake("qw5")

	again := getMap(getMap(readState(), "queueWakes"), "qw5")
	if at := numberOr(again, "at", 0); at != later {
		t.Fatalf("the wake of a deferral moved to %v is set for %v", later, at)
	}
	if actions := journaledFor("qw5"); slices.Contains(actions, "resume") {
		t.Fatalf("the wake resumed the session while its item was still deferred: %v", actions)
	}
}

func TestAQueueWakeWithNoItemToTakeLeavesTheSessionStopped(t *testing.T) {
	cfg, project, frontend, _ := queueWakeSandbox(t)
	defer func(previous int64) { timeOffset = previous }(timeOffset)
	wake, _ := armedQueueWake(t, cfg, project, frontend, "qw6")
	queueCommand(t, cfg, project, "defer", "payment", "--reason", deferReason)
	timeOffset += int64(numberOr(wake, "at", 0)) - nowSec() + 30

	fireQueueWake("qw6")

	if wake := getMap(getMap(readState(), "queueWakes"), "qw6"); wake != nil {
		t.Fatalf("a wake whose item stays deferred with no end is still set: %v", wake)
	}
	if actions := journaledFor("qw6"); !slices.Contains(actions, "skip") || slices.Contains(actions, "resume") {
		t.Fatalf("the wake did not leave the session stopped with a journal entry saying why: %v", actions)
	}
}

func TestATypedPromptDropsTheQueueWake(t *testing.T) {
	cfg, project, frontend, _ := queueWakeSandbox(t)
	armedQueueWake(t, cfg, project, frontend, "qw7")
	prompt := object{"hook_event_name": "UserPromptSubmit", "session_id": "qw7", "cwd": frontend, "prompt": "the key is in the vault now, go on"}
	hookOutput(t, onUserPromptSubmit, prompt, cfg)
	if wake := getMap(getMap(readState(), "queueWakes"), "qw7"); wake != nil {
		t.Fatalf("a typed prompt took the queue up, and its wake is still set: %v", wake)
	}
	if actions := journaledFor("qw7"); !slices.Contains(actions, "drop-queue-wake") {
		t.Fatalf("the journal does not show the prompt dropping the wake: %v", actions)
	}
}

func TestCancelDropsAQueueWake(t *testing.T) {
	cfg, project, frontend, _ := queueWakeSandbox(t)
	armedQueueWake(t, cfg, project, frontend, "qw8")
	code := 0
	printed := capturedStdout(t, func() { code = cancelPending("qw8") })
	if code != 0 || !strings.Contains(printed, T("cancel.done", shortSid("qw8"))) {
		t.Fatalf("noctis cancel of a session with a queue wake exited %d and printed %q", code, printed)
	}
	if wake := getMap(getMap(readState(), "queueWakes"), "qw8"); wake != nil {
		t.Fatalf("noctis cancel left the queue wake set: %v", wake)
	}
}

func TestAQueueWakeLongPastIsPruned(t *testing.T) {
	cfg, project, frontend, _ := queueWakeSandbox(t)
	wake, _ := armedQueueWake(t, cfg, project, frontend, "qw9")
	state := readState()
	pruneState(state, int64(numberOr(wake, "at", 0))+waitStaleSeconds+60)
	if wakes := getMap(state, "queueWakes"); len(wakes) != 0 {
		t.Fatalf("a queue wake two days past its time is still kept: %v", wakes)
	}
	state = readState()
	pruneState(state, nowSec())
	if wakes := getMap(state, "queueWakes"); len(wakes) != 1 {
		t.Fatalf("a queue wake still to come was pruned: %v", wakes)
	}
}
