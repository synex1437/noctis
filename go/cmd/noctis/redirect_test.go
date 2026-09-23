package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestTheUsageFetchDoesNotFollowARedirectWithTheToken(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "secret-token")
	var reached atomic.Int32
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":5,"resets_at":"2030-01-01T00:00:00Z"}}`))
	}))
	t.Cleanup(elsewhere.Close)
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/usage", http.StatusFound)
	}))
	t.Cleanup(redirecting.Close)
	t.Setenv("NOCTIS_USAGE_URL", redirecting.URL)

	next := refreshFable(object{"fable": object{"source": "oauth"}}, nowSec(), "probe", 0, true)

	if reached.Load() != 0 {
		t.Fatal("the usage fetch followed a redirect and sent the bearer token to the server it pointed at")
	}
	if getString(next, "error") != "http-302" {
		t.Fatalf("a redirect from the usage endpoint was not recorded as a failed fetch: %v", next)
	}
}
