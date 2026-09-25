package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAWindowThatStoppedUpdatingIsFetchedBeforeItsProjectionPauses(t *testing.T) {
	cases := []struct {
		name    string
		readNow func(now int64, reset, weekReset float64)
	}{
		{"the status line now sends only the weekly window", func(now int64, _, weekReset float64) {
			weeklyOnlyStatusReading("a", now, 21, weekReset)
		}},
		{"another session sent a lower 5-hour reading, so the higher one from 30 minutes ago is kept", func(now int64, reset, weekReset float64) {
			statusReadingFrom("b", now, 70, reset, 21, weekReset)
		}},
		{"the whole usage file is 30 minutes old", func(int64, float64, float64) {}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sandboxFiles(t)
			now := nowSec()
			reset, weekReset := float64(now+3*3600), float64(now+3*86400)
			fetches := &atomic.Int64{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fetches.Add(1)
				_, _ = io.WriteString(w, limitsBody(78, reset, 21, weekReset))
			}))
			t.Cleanup(server.Close)
			t.Setenv("NOCTIS_USAGE_URL", server.URL)
			t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "lab")
			cfg := releaseConfig()
			cfg["fable"] = object{"source": "oauth"}
			mustWriteJSON(files.config, cfg)
			statusReadingFrom("a", now-3000, 60, reset, 20, weekReset)
			statusReadingFrom("a", now-1800, 76, reset, 20, weekReset)
			tc.readNow(now, reset, weekReset)

			result := decide(loadConfig(), readState(), object{"session_id": "a", "cwd": t.TempDir()}, now, decideOptions{})

			if got := fetches.Load(); got != 1 {
				t.Errorf("the 5-hour reading is 30 minutes old, and the usage endpoint was asked %d time(s), want once", got)
			}
			if result.wait != nil {
				t.Errorf("the endpoint shows the 5-hour window at 78 %%, yet the session pauses on the old reading's projection: %s until %s", result.wait.hit, formatTime(result.wait.until))
			}
		})
	}
}

func TestASubagentFetchesAWindowThatStoppedUpdatingBeforeItsProjectionStopsIt(t *testing.T) {
	cases := []struct {
		name    string
		readNow func(now int64, reset, weekReset float64)
	}{
		{"the status line now sends only the weekly window", func(now int64, _, weekReset float64) {
			weeklyOnlyStatusReading("a", now, 21, weekReset)
		}},
		{"another session sent a lower 5-hour reading, so the higher one from 30 minutes ago is kept", func(now int64, reset, weekReset float64) {
			statusReadingFrom("b", now, 70, reset, 21, weekReset)
		}},
		{"the whole usage file is 30 minutes old", func(int64, float64, float64) {}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sandboxFiles(t)
			now := nowSec()
			reset, weekReset := float64(now+3*3600), float64(now+3*86400)
			fetches := &atomic.Int64{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fetches.Add(1)
				_, _ = io.WriteString(w, limitsBody(78, reset, 21, weekReset))
			}))
			t.Cleanup(server.Close)
			t.Setenv("NOCTIS_USAGE_URL", server.URL)
			t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "lab")
			cfg := releaseConfig()
			cfg["fable"] = object{"source": "oauth"}
			mustWriteJSON(files.config, cfg)
			statusReadingFrom("a", now-3000, 60, reset, 20, weekReset)
			statusReadingFrom("a", now-1800, 76, reset, 20, weekReset)
			tc.readNow(now, reset, weekReset)

			result := subagentLimit(loadConfig(), now)

			if got := fetches.Load(); got != 1 {
				t.Errorf("the 5-hour reading is 30 minutes old, and a subagent's check asked the usage endpoint %d time(s), want once", got)
			}
			if result.wait != nil {
				t.Errorf("the endpoint shows the 5-hour window at 78 %%, yet the subagent is stopped on the old reading's projection: %s until %s", result.wait.hit, formatTime(result.wait.until))
			}
		})
	}
}

func TestNoctisCheckCallsAWindowThatStoppedUpdatingStaleData(t *testing.T) {
	cases := []struct {
		name    string
		readNow func(now int64, reset, weekReset float64)
	}{
		{"the status line now sends only the weekly window", func(now int64, _, weekReset float64) {
			weeklyOnlyStatusReading("a", now, 21, weekReset)
		}},
		{"another session sent a lower 5-hour reading, so the higher one from 30 minutes ago is kept", func(now int64, reset, weekReset float64) {
			statusReadingFrom("b", now, 70, reset, 21, weekReset)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := sandboxFiles(t)
			now := nowSec()
			reset, weekReset := float64(now+3*3600), float64(now+3*86400)
			statusReadingFrom("a", now-3000, 60, reset, 20, weekReset)
			statusReadingFrom("a", now-1800, 76, reset, 20, weekReset)
			tc.readNow(now, reset, weekReset)
			usage, err := os.ReadFile(filepath.Join(dir, "usage.json"))
			if err != nil {
				t.Fatal(err)
			}
			account := t.TempDir()
			cliWrite(t, filepath.Join(account, pluginName, "usage.json"), usage)

			run := runNoctisCLI(t, cliAccountEnv(repoRoot(), account), "check", "--json")

			if run.code != 20 {
				t.Errorf("the 5-hour reading is 30 minutes old, and noctis check, which never fetches a new one, answered %d instead of 20 for stale data:\n%s", run.code, run)
			}
		})
	}
}

func TestASessionStartFetchesAWindowThatStoppedUpdatingBeforeItSaysTheLimitIsReached(t *testing.T) {
	cases := []struct {
		name    string
		readNow func(now int64, reset, weekReset float64)
	}{
		{"the status line now sends only the weekly window", func(now int64, _, weekReset float64) {
			weeklyOnlyStatusReading("a", now, 21, weekReset)
		}},
		{"another session sent a lower 5-hour reading, so the higher one from 30 minutes ago is kept", func(now int64, reset, weekReset float64) {
			statusReadingFrom("b", now, 70, reset, 21, weekReset)
		}},
		{"the whole usage file is 30 minutes old", func(int64, float64, float64) {}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, project, _ := limitSandbox(t, nil, 20, 20)
			if err := os.Remove(files.usage); err != nil {
				t.Fatal(err)
			}
			now := nowSec()
			reset, weekReset := float64(now+3*3600), float64(now+3*86400)
			fetches := &atomic.Int64{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fetches.Add(1)
				_, _ = io.WriteString(w, limitsBody(78, reset, 21, weekReset))
			}))
			t.Cleanup(server.Close)
			t.Setenv("NOCTIS_USAGE_URL", server.URL)
			t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "lab")
			statusReadingFrom("a", now-3000, 60, reset, 20, weekReset)
			statusReadingFrom("a", now-1800, 76, reset, 20, weekReset)
			tc.readNow(now, reset, weekReset)

			start := getString(hookOutput(t, onSessionStart, agentHookInput("SessionStart", "a", project, object{"source": "startup"}), cfg), "systemMessage")

			if got := fetches.Load(); got != 1 {
				t.Errorf("the 5-hour reading is 30 minutes old, and the session start asked the usage endpoint %d time(s), want once", got)
			}
			if strings.Contains(start, "⏸") {
				t.Errorf("the endpoint shows the 5-hour window at 78 %%, yet the session starts with a notice from the old reading's projection: %q", start)
			}
		})
	}
}
