//go:build !windows

package main

import (
	"strings"
	"testing"
)

func parkOutputCap(t *testing.T, sid string, idleMinutes, tokens float64) {
	t.Helper()
	parkBigPause(t, sid, "headless", idleMinutes, tokens)
	updateState(func(state object) {
		wait := getMap(getMap(state, "waits"), sid)
		wait["kind"], wait["window"], wait["label"], wait["used"], wait["threshold"] = "stopfailure", "unknown", T("stopfailure.outCapLabel"), nil, nil
		wait["outputCap"], wait["quietRetry"], wait["retry"], wait["error"] = true, true, float64(1), "max_output_tokens"
		delete(wait, "hit")
	})
}

func TestARelaunchAfterTheOutputCapAsksForSmallerSteps(t *testing.T) {
	calls := relaunchSandboxWith(t, fakeClaude("exit 0"))
	sid := "outputcap1"
	parkOutputCap(t, sid, 1, 0)

	resumeWait(sid, "")

	if got := launchesOf(calls, sid); got != 1 {
		t.Fatalf("the session was resumed %d time(s), want 1: %q", got, launchLines(calls))
	}
	if prompt := launchPromptOf(calls); !strings.Contains(prompt, outputCapNote) {
		t.Fatalf("the resumed session is not asked to go on in smaller steps: %q", prompt)
	}
}

func TestAFreshStartAfterTheOutputCapAsksForSmallerStepsToo(t *testing.T) {
	calls := relaunchSandboxWith(t, fakeClaude("sleep 1", answerWhereItRuns, "exit 1"))
	sid := "outputcap2"
	parkOutputCap(t, sid, 150, 160000)

	resumeWait(sid, "")

	fresh, prompt := freshLaunchOf(calls)
	if fresh == "" {
		t.Fatalf("no fresh session was started: %q", launchLines(calls))
	}
	if !strings.Contains(prompt, outputCapNote) || !strings.Contains(prompt, "fresh session taking over from session "+sid) {
		t.Fatalf("the fresh session is not asked to go on in smaller steps: %q", prompt)
	}
}
