package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// thrashing is the message Claude Code ends a turn with when its compactions leave the context full again.
const thrashing = "Autocompact is thrashing: the context refilled to the limit within 3 turns of the previous compact, 3 times in a row. A file being read or a tool output is likely too large for the context window. Try reading in smaller chunks, or use /clear to start fresh."

// contextFullSandbox has the alarm on and stand-ins for the desktop notifiers, so every notification
// reaches guard.log and nothing shows up on the machine running the tests.
func contextFullSandbox(t *testing.T) (object, string) {
	t.Helper()
	dir := sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	t.Setenv("CLAUDE_CODE_REMOTE", "")
	if !isWindows {
		bin := t.TempDir()
		for _, notifier := range []string{"notify-send", "osascript"} {
			writeScript(t, filepath.Join(bin, notifier), "#!/bin/sh\nexit 0\n")
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	cfg := waitEngineConfig()
	cfg["fable"] = object{"source": "off"}
	cfg["alarm"] = object{"enabled": true}
	return cfg, dir
}

func contextFullStop(sid, cwd string) object {
	return object{"session_id": sid, "cwd": cwd, "error": "invalid_request", "last_assistant_message": thrashing}
}

func TestAStopWithTheContextFullStartsAfreshInsteadOfWakingInPlace(t *testing.T) {
	cfg, dir := contextFullSandbox(t)
	sid := "full-context"
	now := float64(nowSec())
	// The status line reports the session, so a wait for a transient failure would wake it in place.
	statusReadingFrom(sid, nowSec(), 3, now+18000, 20, now+3*86400)

	before := float64(nowSec())
	onStopFailure(contextFullStop(sid, dir), cfg)

	wait := pendingWait(sid)
	if wait == nil || !getBool(wait, "contextFull", false) {
		t.Fatalf("a turn that ended with the context full left no wait to start the session afresh: %v (journal %v)", wait, journaledFor(sid))
	}
	if delay := numberOr(wait, "resumeAt", 0) - before; delay < contextFullDelaySeconds || delay > contextFullDelaySeconds+2 {
		t.Fatalf("the session is started afresh after %v s, want %v s", delay, contextFullDelaySeconds)
	}
	if entry := journaledEntry(sid, "schedule-resume"); entry == nil || getBool(entry, "wake", true) {
		t.Fatalf("the session would be woken in place, where it would send the same full context again: %v", entry)
	}
	if _, _, due := freshStartDue(cfg, wait, sid, nowSec()); !due {
		t.Fatal("the session that stopped with its context full is not started afresh from its handoff note")
	}
	if loggedTimes("notify: "+pluginName+" — "+T("stopfailure.ctxFull", shortSid(sid), formatTime(numberOr(wait, "resumeAt", 0)))) != 1 {
		t.Fatalf("no notice says the session stopped with its context full: %q", tailFileLines(files.log, 20))
	}
}

func TestAContextThatFillsUpInEveryFreshStartIsGivenUpOn(t *testing.T) {
	cfg, dir := contextFullSandbox(t)
	sid := "fills-again"
	fillsAgain := func(sid string, step int) {
		t.Helper()
		updateState(func(state object) { delete(stateMap(state, "waits"), sid) })
		before := float64(nowSec())
		onStopFailure(contextFullStop(sid, dir), cfg)
		wait := pendingWait(sid)
		if wait == nil {
			t.Fatalf("context full %d time(s) in a row was not started afresh", step)
		}
		want := contextFullDelaySeconds
		if step > 1 {
			want = retryDelaySeconds(cfg, step-1)
		}
		if delay := numberOr(wait, "resumeAt", 0) - before; delay < want || delay > want+2 {
			t.Fatalf("context full %d time(s) in a row is started afresh after %v s, want %v s", step, delay, want)
		}
	}

	fillsAgain(sid, 1)
	// Work goes on between the fresh starts without ending a turn: that does not start the count again.
	capturedStdout(t, func() { onPostToolBatch(object{"session_id": sid, "cwd": dir}, cfg) })
	fillsAgain(sid, 2)
	// The fresh session that takes over carries the count along.
	fresh := "fills-again-fresh"
	updateState(func(state object) { moveSessionRecords(state, sid, fresh) })
	for step := 3; step <= stopFailureMaxAttempts; step++ {
		fillsAgain(fresh, step)
	}
	updateState(func(state object) { delete(stateMap(state, "waits"), fresh) })
	onStopFailure(contextFullStop(fresh, dir), cfg)
	if wait := pendingWait(fresh); wait != nil {
		t.Fatalf("the context was full %d times in a row and the session was started afresh once more: %v", stopFailureMaxAttempts+1, wait)
	}
	if entry := journaledEntry(fresh, "retry-giveup"); entry == nil || !getBool(entry, "contextFull", false) {
		t.Fatalf("giving up was not journaled as a context that fills up again: %v", journaledFor(fresh))
	}
	if loggedTimes("notify: "+pluginName+" — "+T("stopfailure.ctxFullGiveup", shortSid(fresh), formatNumber(stopFailureMaxAttempts))) != 1 {
		t.Fatalf("giving up does not say how the work can go on: %q", tailFileLines(files.log, 20))
	}

	// A turn that ends starts the count again.
	capturedStdout(t, func() { onStop(object{"session_id": fresh, "cwd": dir}, cfg) })
	fillsAgain(fresh, 1)
}

func TestObserveModeSpendsNoFreshStartsOfAContextThatKeepsFillingUp(t *testing.T) {
	cfg, dir := contextFullSandbox(t)
	sid := "observed-full"
	previous := observing
	t.Cleanup(func() { observing = previous })
	observing = true
	// The person sends the turn again each time it stops: observe mode starts nothing afresh.
	for step := 1; step <= stopFailureMaxAttempts+1; step++ {
		onStopFailure(contextFullStop(sid, dir), cfg)
	}
	giveup := "notify: " + pluginName + " — " + T("stopfailure.ctxFullGiveup", shortSid(sid), formatNumber(stopFailureMaxAttempts))
	if journaledEntry(sid, "retry-giveup") != nil || loggedTimes(giveup) != 0 {
		t.Fatalf("observe mode started no fresh session, yet said it gave up after %d fresh starts: %v", stopFailureMaxAttempts, journaledFor(sid))
	}

	observing = false
	before := float64(nowSec())
	onStopFailure(contextFullStop(sid, dir), cfg)
	wait := pendingWait(sid)
	if wait == nil || numberOr(wait, "resumeAt", 0)-before > contextFullDelaySeconds+2 {
		t.Fatalf("once noctis acts again, the session that stopped with its context full is not started afresh as the first time: %v (journal %v)", wait, journaledFor(sid))
	}
}

func TestATurnThatEndsWhileNoctisIsOffStartsTheFullContextCountAgain(t *testing.T) {
	cfg, dir := contextFullSandbox(t)
	sid := "full-while-off"
	onStopFailure(contextFullStop(sid, dir), cfg)
	if pendingWait(sid) == nil {
		t.Fatalf("a turn that ended with the context full was not started afresh: %v", journaledFor(sid))
	}
	// noctis off pauses the guard for an hour, and the session goes on and ends a turn normally.
	updateState(func(state object) {
		delete(stateMap(state, "waits"), sid)
		state["disabledUntil"] = float64(nowSec() + 3600)
	})
	capturedStdout(t, func() { onStop(object{"session_id": sid, "cwd": dir}, cfg) })

	before := float64(nowSec())
	onStopFailure(contextFullStop(sid, dir), cfg)
	wait := pendingWait(sid)
	if wait == nil {
		t.Fatalf("the context filled up again and the session was not started afresh: %v", journaledFor(sid))
	}
	if delay := numberOr(wait, "resumeAt", 0) - before; delay > contextFullDelaySeconds+2 {
		t.Fatalf("a turn ended normally while noctis was off, yet the next full context counts as one more in a row: started afresh after %v s, want %v s (%v)", delay, contextFullDelaySeconds, getMap(getMap(readState(), "contextFulls"), sid))
	}
}

func TestAContextGivenUpOnLeavesTheNextSessionItsResumeNote(t *testing.T) {
	cfg, dir := contextFullSandbox(t)
	sid := "full-for-good"
	// Five fresh starts in a row filled up again, and so does the session the last one started.
	updateState(func(state object) {
		stateMap(state, "contextFulls")[sid] = object{"count": float64(stopFailureMaxAttempts), "lastAt": float64(nowSec() - 600)}
	})
	onStopFailure(contextFullStop(sid, dir), cfg)
	if entry := journaledEntry(sid, "retry-giveup"); entry == nil || pendingWait(sid) != nil {
		t.Fatalf("noctis did not give up on the context that kept filling up: %v", journaledFor(sid))
	}

	// The notice says to go on from the resume note, in a session of the person's.
	note := getString(checkpointRecord(sid), "path")
	context := contextOf(hookOutput(t, onSessionStart, object{"session_id": "after-giveup", "cwd": dir, "source": "startup"}, cfg))
	if note == "" || !strings.Contains(context, note) {
		t.Fatalf("noctis gave up on the session and said to go on from its resume note, but the next session was handed none: note %q, context %q", note, context)
	}
}

func TestACloudSessionWithItsContextFullIsLeftToThePerson(t *testing.T) {
	dir := t.TempDir()
	cfg, project := cloudSandbox(t, dir, nil)
	sid := "cloud-full"
	onStopFailure(agentHookInput("StopFailure", sid, project, object{"error": "invalid_request", "last_assistant_message": thrashing}), cfg)
	if wait := pendingWait(sid); wait != nil {
		t.Fatalf("a cloud session with its context full was parked, to be woken with the same full context: %v", wait)
	}
	if entry := journaledEntry(sid, "context-full"); entry == nil || !getBool(entry, "cloud", false) {
		t.Fatalf("the full context of a cloud session was not journaled: %v", journaledFor(sid))
	}
}

func TestTheSessionAClearStartsAfterACloudSessionFilledUpIsHandedItsResumeNote(t *testing.T) {
	dir := t.TempDir()
	cfg, project := cloudSandbox(t, dir, nil)
	sid := "cloud-full-cleared"
	onStopFailure(agentHookInput("StopFailure", sid, project, object{"error": "invalid_request", "last_assistant_message": thrashing}), cfg)

	// The notice says to run /clear to go on.
	note := getString(checkpointRecord(sid), "path")
	context := contextOf(hookOutput(t, onSessionStart, agentHookInput("SessionStart", "cloud-cleared", project, object{"source": "clear"}), cfg))
	if note == "" || !strings.Contains(context, note) {
		t.Fatalf("the session /clear started after the context filled up was handed no resume note of the work: note %q, context %q", note, context)
	}
}

func TestOnlyAnInvalidRequestThatNamesAFullContextIsTakenForOne(t *testing.T) {
	cases := []struct {
		name    string
		input   object
		wait    bool
		context bool
	}{
		{"the prompt is too long", object{"error": "invalid_request", "error_details": "prompt is too long: 1012345 tokens > 1000000 maximum"}, true, true},
		{"the context limit is reached", object{"error": "invalid_request", "last_assistant_message": "Context limit reached · /compact or /clear to continue"}, true, true},
		{"another invalid request", object{"error": "invalid_request", "error_details": "messages: text content blocks must be non-empty"}, false, false},
		{"a rate limit that quotes the text", object{"error": "rate_limit", "last_assistant_message": "API Error: Rate limit reached. Prompt is too long to retry now."}, true, false},
	}
	for index, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			cfg, dir := contextFullSandbox(t)
			sid := "classified-" + formatNumber(float64(index))
			input := object{"session_id": sid, "cwd": dir}
			for key, value := range test.input {
				input[key] = value
			}
			onStopFailure(input, cfg)
			wait := pendingWait(sid)
			if !test.wait {
				// A request Claude Code turns down for another reason would be turned down again on a retry.
				if wait != nil || len(journaledFor(sid)) > 0 {
					t.Fatalf("a request turned down for another reason than a full context was retried: %v (journal %v)", wait, journaledFor(sid))
				}
				return
			}
			if wait == nil {
				t.Fatalf("the failure left no wait: %v", journaledFor(sid))
			}
			if full := getBool(wait, "contextFull", false); full != test.context {
				t.Fatalf("the failure was taken for a full context: %t, want %t (%v)", full, test.context, wait)
			}
		})
	}
	t.Run("another host", func(t *testing.T) {
		cfg, dir := contextFullSandbox(t)
		previous := activeHost
		t.Cleanup(func() { activeHost = previous })
		activeHost = "copilot"
		sid := "other-host"
		onStopFailure(contextFullStop(sid, dir), cfg)
		if wait := pendingWait(sid); wait == nil || getBool(wait, "contextFull", false) || slices.Contains(journaledFor(sid), "context-full") {
			t.Fatalf("a host whose compaction noctis does not know was taken for Claude Code: %v (journal %v)", wait, journaledFor(sid))
		}
	})
}
