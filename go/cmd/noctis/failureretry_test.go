package main

import (
	"slices"
	"testing"
)

func TestRepeatedUnexplainedFailuresClimbTheRetryLadderAndThenStop(t *testing.T) {
	dir := sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	cfg := waitEngineConfig()
	cfg["fable"] = object{"source": "off"}
	sid := "keeps-failing"
	input := object{"session_id": sid, "cwd": dir, "error_type": "invalid_request", "error_message": "prompt is too long"}
	relaunched := func() {
		updateState(func(state object) { delete(stateMap(state, "waits"), sid) })
	}
	failAgain := func(step int) {
		t.Helper()
		relaunched()
		before := float64(nowSec())
		onStopFailure(input, cfg)
		wait := getMap(getMap(readState(), "waits"), sid)
		if wait == nil {
			t.Fatalf("failure %d in a row was not retried", step)
		}
		want := retryDelaySeconds(cfg, step)
		if delay := numberOr(wait, "resumeAt", 0) - before; delay < want || delay > want+2 {
			t.Fatalf("failure %d in a row is retried after %v s, want step %d of the ladder (%v s)", step, delay, step, want)
		}
	}

	for step := 1; step <= stopFailureMaxAttempts; step++ {
		failAgain(step)
	}
	relaunched()
	onStopFailure(input, cfg)
	if wait := getMap(getMap(readState(), "waits"), sid); wait != nil {
		t.Fatalf("the session failed %d times in a row and was parked for yet another retry: %v", stopFailureMaxAttempts+1, wait)
	}
	if !slices.Contains(journaledFor(sid), "retry-giveup") {
		t.Fatalf("giving up was not journaled: %v", journaledFor(sid))
	}
	if getMap(getMap(readState(), "launchFailures"), sid) == nil {
		t.Fatal("giving up is not shown by noctis status")
	}

	capturedStdout(t, func() { onPostToolBatch(object{"session_id": sid, "cwd": dir}, cfg) })
	failAgain(1)
	failAgain(2)
	relaunched()
	capturedStdout(t, func() { onStop(object{"session_id": sid, "cwd": dir}, cfg) })
	failAgain(1)
	updateState(func(state object) {
		getMap(getMap(state, "failureRetries"), sid)["lastAt"] = float64(nowSec()) - retryDelaySeconds(cfg, 1) - failureEpisodeSlack - 60
	})
	failAgain(1)
}
