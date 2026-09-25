package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type usageRequests struct {
	mu   sync.Mutex
	sent []int64
}

func (r *usageRequests) times() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int64(nil), r.sent...)
}

func recordingUsageEndpoint(t *testing.T, retryAfter, body string) *usageRequests {
	t.Helper()
	requests := &usageRequests{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.mu.Lock()
		requests.sent = append(requests.sent, nowSec())
		requests.mu.Unlock()
		if body != "" {
			_, _ = io.WriteString(w, body)
			return
		}
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)
	t.Setenv("NOCTIS_USAGE_URL", server.URL)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "lab")
	return requests
}

func blindAtTheCeiling(t *testing.T, now, backoffUntil int64, rounds float64) (object, object, float64) {
	t.Helper()
	dir := sandboxFiles(t)
	reset, weekReset := float64(now+2*3600), float64(now+3*86400)
	mustWriteJSON(files.usage, object{"updatedAt": float64(now - 120), "five_hour": object{"used": float64(97), "resetsAt": reset}, "seven_day": object{"used": float64(20), "resetsAt": weekReset}})
	mustWriteJSON(files.fable, object{"fetchedAt": float64(now - 3600), "error": "http-429", "backoffUntil": float64(backoffUntil)})
	cfg := unguardedConfig()
	cfg["usage"] = object{"blindProbeSeconds": float64(1), "blindProbeRounds": rounds}
	return cfg, object{"session_id": "rate-limited", "cwd": dir}, reset
}

func TestTheBlindProbeSendsNothingWhileTheUsageEndpointRateLimitsNoctis(t *testing.T) {
	requests := recordingUsageEndpoint(t, "", "")
	now := nowSec()
	cfg, input, reset := blindAtTheCeiling(t, now, now+600, 3)
	started := time.Now()
	result := decide(cfg, readState(), input, now, decideOptions{})
	if sent := requests.times(); len(sent) != 0 {
		t.Fatalf("3 points from the paid-credit ceiling the blind probe sent %d request(s) to a usage endpoint that answered 429, inside its 10-minute backoff", len(sent))
	}
	if plan := result.wait; plan == nil || plan.hit != "blind" || plan.window != "five_hour" || plan.until != reset {
		t.Fatalf("without data and with the endpoint rate-limiting noctis, the session near the ceiling was not paused blind: %+v", plan)
	}
	if elapsed := time.Since(started); elapsed >= 3*time.Second {
		t.Fatalf("the hook sat through %s of probe rounds that the backoff kept from sending anything", elapsed.Round(time.Second))
	}
}

func TestTheBlindProbeTriesAgainOnceARateLimitBackoffEnds(t *testing.T) {
	now := nowSec()
	backoffUntil := now + 2
	requests := recordingUsageEndpoint(t, "", limitsBody(50, float64(now+5*3600), 20, float64(now+3*86400)))
	cfg, input, _ := blindAtTheCeiling(t, now, backoffUntil, 4)
	result := decide(cfg, readState(), input, now, decideOptions{})
	sent := requests.times()
	if len(sent) == 0 {
		t.Fatal("the blind probe never asked the usage endpoint again after its rate-limit backoff ended")
	}
	if sent[0] < backoffUntil {
		t.Fatalf("the blind probe asked the usage endpoint %d s before the end of its rate-limit backoff", backoffUntil-sent[0])
	}
	if result.wait != nil {
		t.Fatalf("fresh data with room after the backoff still paused the session: %+v", result.wait)
	}
}

func TestARateLimitBacksOffAsLongAsTheRetryAfterHeaderAsks(t *testing.T) {
	sandboxFiles(t)
	cfg := object{"fable": object{"source": "oauth"}}
	for _, tc := range []struct {
		name   string
		header func(now int64) string
		want   int64
	}{
		{"no Retry-After", func(int64) string { return "" }, 600},
		{"a Retry-After shorter than ten minutes", func(int64) string { return "30" }, 600},
		{"a Retry-After in seconds", func(int64) string { return "1500" }, 1500},
		{"a Retry-After as a date", func(now int64) string { return time.Unix(now+2400, 0).UTC().Format(http.TimeFormat) }, 2400},
		{"a Retry-After beyond an hour", func(int64) string { return "86400" }, 3600},
		{"a Retry-After noctis cannot read", func(int64) string { return "soon" }, 600},
	} {
		now := nowSec()
		recordingUsageEndpoint(t, tc.header(now), "")
		mustWriteJSON(files.fable, object{"fetchedAt": float64(now - 3600)})
		next := refreshFable(cfg, now, "probe", 0, false)
		got := int64(numberOr(next, "backoffUntil", 0)) - now
		if !strings.HasPrefix(getString(next, "error"), "http-429") || got < tc.want-1 || got > tc.want+1 {
			t.Fatalf("%s: a 429 backed off %d s (error %q), want %d s", tc.name, got, getString(next, "error"), tc.want)
		}
	}
}
