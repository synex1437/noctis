package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTheUsageRequestNamesNoctisAsItsUserAgent(t *testing.T) {
	sandboxFiles(t)
	agents := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		select {
		case agents <- request.Header.Get("User-Agent"):
		default:
		}
	}))
	defer server.Close()
	t.Setenv("NOCTIS_USAGE_URL", server.URL+"/api/oauth/usage")

	fetchOauthUsage("test-token")
	select {
	case got := <-agents:
		if want := pluginName + "/" + pluginVersion; got != want {
			t.Errorf("the usage request says it is %q; it is noctis asking, so it should say %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the usage request never reached the local server")
	}
}
