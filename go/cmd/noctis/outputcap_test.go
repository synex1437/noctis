package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const unreachableAPI = "API Error: Can't reach the API server — check your internet or DNS (ENOTFOUND)"

const expiredAWSCredentials = "API Error: Could not load AWS credentials · The security token included in the request is expired. Check or refresh your AWS credentials and try again."

const outputCapped = "API Error: Claude's response exceeded the 32000 output token maximum. To configure this behavior, set the CLAUDE_CODE_MAX_OUTPUT_TOKENS environment variable."

func outputCapStop(sid, cwd string) object {
	return object{"session_id": sid, "cwd": cwd, "error": "max_output_tokens", "last_assistant_message": outputCapped}
}

func noticesLogged() int {
	return loggedTimes("notify: " + pluginName + " — ")
}

func TestAReplyCutAtTheOutputCapIsRetriedOnceWithoutANotice(t *testing.T) {
	cfg, dir := contextFullSandbox(t)
	sid := "output-cap"
	before := float64(nowSec())
	onStopFailure(outputCapStop(sid, dir), cfg)

	wait := pendingWait(sid)
	if wait == nil || !getBool(wait, "outputCap", false) || getBool(wait, "contextFull", false) {
		t.Fatalf("a reply cut at the output cap left no retry: %v (journal %v)", wait, journaledFor(sid))
	}
	if delay := numberOr(wait, "resumeAt", 0) - before; delay < outputCapDelaySeconds || delay > outputCapDelaySeconds+2 {
		t.Fatalf("the retry after the output cap comes %v s later, want %v s", delay, outputCapDelaySeconds)
	}
	if _, _, due := freshStartDue(cfg, wait, sid, nowSec()); due {
		t.Fatal("a session whose reply was cut at the output cap is started afresh, though its context has room")
	}
	if got := noticesLogged(); got != 0 {
		t.Fatalf("the quiet retry after the output cap raised %d notice(s): %q", got, tailFileLines(files.log, 20))
	}
}

func TestAReplyCutAtTheOutputCapAgainBeforeATurnEndsIsGivenUpOn(t *testing.T) {
	cfg, dir := contextFullSandbox(t)
	sid := "output-cap-again"
	onStopFailure(outputCapStop(sid, dir), cfg)
	if pendingWait(sid) == nil {
		t.Fatalf("a reply cut at the output cap left no retry: %v", journaledFor(sid))
	}
	updateState(func(state object) { delete(stateMap(state, "waits"), sid) })
	capturedStdout(t, func() { onPostToolBatch(object{"session_id": sid, "cwd": dir}, cfg) })
	onStopFailure(outputCapStop(sid, dir), cfg)

	if wait := pendingWait(sid); wait != nil {
		t.Fatalf("the reply was cut at the output cap again and the session was retried once more: %v", wait)
	}
	if entry := journaledEntry(sid, "retry-giveup"); entry == nil || !getBool(entry, "outputCap", false) {
		t.Fatalf("giving up was not journaled as a reply cut at the output cap: %v", journaledFor(sid))
	}
	giveup := "notify: " + pluginName + " — " + T("stopfailure.outCapGiveup", shortSid(sid), hostResumeCommand("claude", sid))
	if loggedTimes(giveup) != 1 || noticesLogged() != 1 {
		t.Fatalf("giving up on the output cap did not raise one notice that says how to go on: %q", tailFileLines(files.log, 20))
	}
	if getString(checkpointRecord(sid), "path") == "" {
		t.Fatal("giving up on the output cap left no checkpoint to go on from")
	}

	capturedStdout(t, func() { onStop(object{"session_id": sid, "cwd": dir}, cfg) })
	onStopFailure(outputCapStop(sid, dir), cfg)
	if wait := pendingWait(sid); wait == nil || !getBool(wait, "outputCap", false) {
		t.Fatalf("after a turn that ended normally, a reply cut at the output cap was not retried again: %v (journal %v)", wait, journaledFor(sid))
	}
}

func TestAFreshSessionCutAtTheOutputCapAfterItsRetryIsGivenUpOn(t *testing.T) {
	cfg, dir := contextFullSandbox(t)
	sid, fresh := "output-cap-handed-on", "output-cap-fresh"
	onStopFailure(outputCapStop(sid, dir), cfg)
	if pendingWait(sid) == nil {
		t.Fatalf("a reply cut at the output cap left no retry: %v", journaledFor(sid))
	}
	updateState(func(state object) {
		delete(stateMap(state, "waits"), sid)
		moveSessionRecords(state, sid, fresh)
	})
	onStopFailure(outputCapStop(fresh, dir), cfg)

	if wait := pendingWait(fresh); wait != nil {
		t.Fatalf("the fresh session that took over the retry was cut at the output cap again and retried once more: %v", wait)
	}
	if entry := journaledEntry(fresh, "retry-giveup"); entry == nil || !getBool(entry, "outputCap", false) {
		t.Fatalf("giving up was not journaled as a reply cut at the output cap: %v", journaledFor(fresh))
	}
}

func TestObserveModeGivesUpOnNoReplyCutAtTheOutputCap(t *testing.T) {
	cfg, dir := contextFullSandbox(t)
	sid := "observed-output-cap"
	previous := observing
	t.Cleanup(func() { observing = previous })
	observing = true
	for step := 1; step <= outputCapRetries+2; step++ {
		onStopFailure(outputCapStop(sid, dir), cfg)
	}
	if journaledEntry(sid, "retry-giveup") != nil || noticesLogged() != 0 || pendingWait(sid) != nil {
		t.Fatalf("observe mode retried nothing, yet acted on the output cap: %v", journaledFor(sid))
	}
	if getMap(getMap(readState(), "outputCaps"), sid) != nil {
		t.Fatalf("observe mode counted retries it never made: %v", getMap(readState(), "outputCaps"))
	}
}

func TestACloudSessionCutAtTheOutputCapAgainIsToldToGoOnWithAPrompt(t *testing.T) {
	dir := t.TempDir()
	cfg, project := cloudSandbox(t, dir, object{"alarm": object{"enabled": true}, "wake": object{"sameSession": false}})
	standInNotifiers(t)
	sid := "cloud-output-cap"
	updateState(func(state object) {
		stateMap(state, "outputCaps")[sid] = object{"count": float64(outputCapRetries), "lastAt": float64(nowSec() - 60)}
	})
	onStopFailure(agentHookInput("StopFailure", sid, project, object{"error": "max_output_tokens", "last_assistant_message": outputCapped}), cfg)
	if wait := pendingWait(sid); wait != nil {
		t.Fatalf("a cloud session cut at the output cap again was parked for one more retry: %v", wait)
	}
	if loggedTimes("notify: "+pluginName+" — "+T("stopfailure.outCapCloud", shortSid(sid))) != 1 {
		t.Fatalf("the cloud session was not told to go on with a prompt: %q", tailFileLines(files.log, 20))
	}
}

func TestTheWakeAfterTheOutputCapAsksForSmallerSteps(t *testing.T) {
	if os.Getenv("NOCTIS_TEST_OUTPUT_CAP_WAKE") != "" {
		cfg, project, _ := limitSandbox(t, nil, 20, 40)
		sid := "output-cap-wake"
		now := float64(nowSec())
		transcript := filepath.Join(project, "transcript.jsonl")
		usage := readJSON(files.usage)
		usage["sessions"] = object{sid: object{"model": "claude-opus-5", "cwd": project, "transcript": transcript, "updatedAt": now, "context": nil}}
		mustWriteJSON(files.usage, usage)
		record := object{"kind": "stopfailure", "window": "unknown", "label": T("stopfailure.outCapLabel"), "used": nil, "outputCap": true, "retry": float64(1), "error": "max_output_tokens",
			"until": now, "resumeAt": now, "startedAt": now, "cwd": project, "transcript": transcript}
		updateState(func(state object) { stateMap(state, "waits")[sid] = cloneObject(record) })
		wakeSameSession(cfg, sid, record, now)
		return
	}
	scratch := t.TempDir()
	child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	child.Env = append(os.Environ(), "NOCTIS_TEST_OUTPUT_CAP_WAKE=1", "TMPDIR="+scratch, "TMP="+scratch, "TEMP="+scratch)
	output, err := child.CombinedOutput()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	if code != 2 || !strings.Contains(string(output), outputCapNote) {
		t.Fatalf("the session woken after the output cap was not asked to go on in smaller steps: exit %d\n%s", code, output)
	}
}

func TestAMaxOutputTokensStopIsReadByWhatItSays(t *testing.T) {
	cases := []struct {
		name                   string
		message                string
		outputCap, contextFull bool
	}{
		{"the context window limit", "API Error: The model has reached its context window limit.", false, true},
		{"the output token maximum", outputCapped, true, false},
	}
	for index, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			cfg, dir := contextFullSandbox(t)
			sid := "max-output-" + formatNumber(float64(index))
			onStopFailure(object{"session_id": sid, "cwd": dir, "error": "max_output_tokens", "last_assistant_message": test.message}, cfg)
			wait := pendingWait(sid)
			if wait == nil {
				t.Fatalf("the stop left no wait: %v", journaledFor(sid))
			}
			if got := getBool(wait, "outputCap", false); got != test.outputCap {
				t.Fatalf("the stop was taken for the output cap: %t, want %t (%v)", got, test.outputCap, wait)
			}
			if got := getBool(wait, "contextFull", false); got != test.contextFull {
				t.Fatalf("the stop was taken for a full context: %t, want %t (%v)", got, test.contextFull, wait)
			}
		})
	}
}

func TestAConnectionErrorIsRetriedQuietlyUntilNoctisGivesUp(t *testing.T) {
	for _, failure := range []struct{ errorType, message string }{
		{"unknown", unreachableAPI},
		{"cloud_credential_error", expiredAWSCredentials},
	} {
		errorType := failure.errorType
		t.Run(errorType, func(t *testing.T) {
			cfg, dir := contextFullSandbox(t)
			writeUsage(95, 20, 0)
			sid := "quiet-" + errorType
			stop := object{"session_id": sid, "cwd": dir, "error": errorType, "last_assistant_message": failure.message}
			for step := 1; step <= stopFailureMaxAttempts; step++ {
				updateState(func(state object) { delete(stateMap(state, "waits"), sid) })
				before := float64(nowSec())
				onStopFailure(stop, cfg)
				wait := pendingWait(sid)
				if wait == nil || getString(wait, "window") != "unknown" {
					t.Fatalf("failure %d was not retried as one no limit explains, with the 5-hour window at 95%%: %v (journal %v)", step, wait, journaledFor(sid))
				}
				want := retryDelaySeconds(cfg, step)
				if delay := numberOr(wait, "resumeAt", 0) - before; delay < want || delay > want+2 {
					t.Fatalf("retry %d comes %v s later, want %v s", step, delay, want)
				}
			}
			if got := noticesLogged(); got != 0 {
				t.Fatalf("the quiet retries raised %d notice(s): %q", got, tailFileLines(files.log, 20))
			}

			updateState(func(state object) { delete(stateMap(state, "waits"), sid) })
			onStopFailure(stop, cfg)
			if wait := pendingWait(sid); wait != nil {
				t.Fatalf("the failure came back after %d retries and was retried once more: %v", stopFailureMaxAttempts, wait)
			}
			giveup := "notify: " + pluginName + " — " + T("stopfailure.giveup", shortSid(sid), errorType, formatNumber(stopFailureMaxAttempts), hostResumeCommand("claude", sid))
			if loggedTimes(giveup) != 1 || noticesLogged() != 1 {
				t.Fatalf("giving up did not raise one notice that says how to go on: %q", tailFileLines(files.log, 20))
			}
		})
	}
	t.Run("another host", func(t *testing.T) {
		cfg, dir := contextFullSandbox(t)
		previous := activeHost
		t.Cleanup(func() { activeHost = previous })
		activeHost = "copilot"
		sid := "other-host-unknown"
		onStopFailure(object{"session_id": sid, "cwd": dir, "error": "unknown", "last_assistant_message": unreachableAPI}, cfg)
		wait := pendingWait(sid)
		if wait == nil || loggedTimes("notify: "+pluginName+" — "+T("stopfailure.transient", formatTime(numberOr(wait, "resumeAt", 0)))) != 1 {
			t.Fatalf("another host's failure lost its retry notice: %v %q", wait, tailFileLines(files.log, 20))
		}
	})
}

func TestAQuietRetryIsRelaunchedWithoutANotice(t *testing.T) {
	cases := []struct {
		name    string
		failure object
		usage   bool
		notices int
	}{
		{"the output token maximum", claudeFailure("max_output_tokens", "", outputCapped), true, 0},
		{"a connection error", claudeFailure("unknown", "", unreachableAPI), true, 0},
		{"a connection error on an account with no usage data", claudeFailure("unknown", "", unreachableAPI), false, 0},
		{"expired AWS credentials, on Bedrock, which has no usage data", claudeFailure("cloud_credential_error", "", expiredAWSCredentials), false, 0},
		{"a failure the limits do not explain, told of at the pause and at the relaunch", claudeFailure("rate_limit", "", "API Error: Request rejected (429)"), true, 2},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := takeoverSandbox(t)
			previousHost, previousOffset := activeHost, timeOffset
			t.Cleanup(func() { activeHost, timeOffset = previousHost, previousOffset })
			activeHost = "claude"
			t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
			t.Setenv("CLAUDE_CODE_REMOTE", "")
			sid := fmt.Sprintf("quiet%d", index+1)
			cwd := t.TempDir()
			failedAt := float64(nowSec())
			failure := agentHookInput("StopFailure", sid, cwd, tc.failure)
			failure["transcript_path"] = writeTranscriptAt(t, cwd, []string{userPromptLine(failedAt-60, "write the report"), apiErrorLine(failedAt)}, failedAt)

			hookOutput(t, onStopFailure, failure, loadConfig())
			wait := pendingWait(sid)
			if wait == nil {
				t.Fatalf("the failure was not set to retry (journal %v)", journaledFor(sid))
			}
			timeOffset += int64(numberOr(wait, "resumeAt", 0)) - nowSec() + 5
			if tc.usage {
				now := float64(nowSec())
				statusReadingFrom(sid, nowSec(), 3, now+18000, 20, now+3*86400)
			}

			resumeWait(sid, "")

			if got := launchesOf(calls, sid); got != 1 {
				t.Fatalf("the runner relaunched the session %d time(s), want 1 (journal %v)", got, journaledFor(sid))
			}
			if got := noticesLogged(); got != tc.notices {
				t.Fatalf("%d notification(s) were sent, want %d: %q", got, tc.notices, tailFileLines(files.log, 20))
			}
		})
	}
}
