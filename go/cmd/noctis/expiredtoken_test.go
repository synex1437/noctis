package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAnExpiredSignInIsAskedAgainSoonAndNamedAsExpired(t *testing.T) {
	dir := sandboxFiles(t)
	previousLocale := locale
	t.Cleanup(func() { locale = previousLocale })
	locale = "en"
	files.credentials = filepath.Join(dir, ".credentials.json")
	mustWriteJSON(files.credentials, object{"claudeAiOauth": object{"accessToken": "old", "expiresAt": float64(nowSec()-60) * 1000}})
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	now := nowSec()
	fiveReset, weekReset := float64(now+3*3600), float64(now+3*86400)
	fetches := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		_, _ = io.WriteString(w, limitsBody(40, fiveReset, 20, weekReset))
	}))
	t.Cleanup(server.Close)
	t.Setenv("NOCTIS_USAGE_URL", server.URL)
	cfg := releaseConfig()
	cfg["fable"] = object{"source": "oauth"}

	refreshFableWaiting(cfg, now, "u5-expired", -1, false, 0)
	statusLine := scopedDataLine(t, cfg)

	mustWriteJSON(files.credentials, object{"claudeAiOauth": object{"accessToken": "renewed", "expiresAt": float64(nowSec()+3600) * 1000}})
	later := now + 5*60
	fresh := refreshFableWaiting(cfg, later, "u5-renewed", -1, false, 0)
	if fetches.Load() != 1 || numberOr(fresh, "fetchedAt", 0) != float64(later) {
		t.Fatalf("Claude Code renewed the expired sign-in, and five minutes later noctis had asked the usage endpoint %d times: %v", fetches.Load(), fresh)
	}
	if !strings.Contains(statusLine, T("refresh.tokenExpired")) || strings.Contains(statusLine, T("refresh.noToken")) {
		t.Fatalf("status names an expired sign-in as a missing one: %q", statusLine)
	}
}
