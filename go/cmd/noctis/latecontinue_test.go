package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTheRunnerDoesNotRelaunchASessionThatContinuedDuringItsUsageCheck(t *testing.T) {
	calls := takeoverSandbox(t)
	previousHost := activeHost
	t.Cleanup(func() { activeHost = previousHost })
	activeHost = "claude"
	sid := "late-builtin"
	now := float64(nowSec())
	reset := now - 280
	cwd := t.TempDir()
	before := []string{userPromptLine(reset-3600, "fix the parser"), apiErrorLine(reset - 3599)}
	transcript := writeTranscriptAt(t, cwd, before, reset-3599)
	fetches := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		continued := strings.Join(append(before, userPromptLine(float64(nowSec()), "Usage limit reset, continuing automatically")), "\n") + "\n"
		if err := os.WriteFile(transcript, []byte(continued), 0o600); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, limitsBody(2, now+5*3600, 20, now+3*86400))
	}))
	t.Cleanup(server.Close)
	t.Setenv("NOCTIS_USAGE_URL", server.URL)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "lab")
	config := releaseConfig()
	config["fable"] = object{"source": "oauth"}
	config["resume"] = object{"mode": "headless", "prompt": "carry on"}
	mustWriteJSON(files.config, config)
	updateState(func(state object) {
		stateMap(state, "waits")[sid] = object{"kind": "stopfailure", "window": "five_hour", "label": "5h", "used": float64(100), "threshold": nil,
			"until": reset, "startedAt": reset - 3599, "resumeAt": reset + 270, "attempts": float64(0), "cwd": cwd, "transcript": transcript, "launchMode": "headless"}
	})

	resumeWait(sid, "")

	if fetched := fetches.Load(); fetched != 1 {
		t.Fatalf("the runner fetched usage %d time(s), want once", fetched)
	}
	if got := launchesOf(calls, sid); got != 0 {
		t.Fatalf("Claude Code's own continue resumed the session while the runner fetched usage, and the runner still relaunched it %d time(s), so two copies of the session run (journal %v)", got, journaledFor(sid))
	}
	if wait := getMap(getMap(readState(), "waits"), sid); wait != nil {
		t.Fatalf("the wait of a session that already continued is still stored: %v", wait)
	}
}
