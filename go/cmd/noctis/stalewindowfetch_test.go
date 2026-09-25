package main

import (
	"io"
	"net/http"
	"net/http/httptest"
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
