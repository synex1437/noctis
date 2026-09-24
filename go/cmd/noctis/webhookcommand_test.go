package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func webhookAccount(t *testing.T, url string) string {
	t.Helper()
	home := t.TempDir()
	cliWrite(t, filepath.Join(home, ".claude", pluginName, "config.json"), []byte(`{"alarm": {"webhook": {"url": "`+url+`", "preset": "generic"}}}`))
	return home
}

func TestNoctisWebhookSaysWhetherTheMessageArrived(t *testing.T) {
	var mu sync.Mutex
	bodies := []string{}
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		code := status
		mu.Unlock()
		w.WriteHeader(code)
	}))
	defer server.Close()

	t.Run("no webhook", func(t *testing.T) {
		run := startNoctisCLIAt(t, webhookAccount(t, ""), "", nil, "webhook")()
		if run.code != 1 || !strings.Contains(run.stderr, "alarm.webhook.url") {
			t.Fatalf("with no webhook set, noctis webhook must say so and exit 1:\n%s", run)
		}
	})
	t.Run("delivered", func(t *testing.T) {
		run := startNoctisCLIAt(t, webhookAccount(t, server.URL+"/hook"), "", nil, "webhook")()
		mu.Lock()
		got := strings.Join(bodies, "\n")
		mu.Unlock()
		if run.code != 0 || !strings.Contains(run.stdout, "delivered") {
			t.Fatalf("a delivered test message is not reported:\n%s", run)
		}
		if !strings.Contains(got, pluginName) || strings.Contains(got, `"body":""`) {
			t.Fatalf("the test message arrived empty: %s", got)
		}
	})
	t.Run("refused", func(t *testing.T) {
		mu.Lock()
		status = http.StatusBadRequest
		mu.Unlock()
		run := startNoctisCLIAt(t, webhookAccount(t, server.URL+"/hook"), "", nil, "webhook", "--title", "T", "--body", "B")()
		if run.code != 1 || !strings.Contains(run.stderr, "http-400") {
			t.Fatalf("a refused delivery must name the reason and exit 1:\n%s", run)
		}
	})
}

func TestAnOpenCircuitHoldsAMessageUntilItClosesButLetsATestThrough(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		mu.Lock()
		requests++
		mu.Unlock()
	}))
	defer server.Close()
	sent := func() int {
		mu.Lock()
		defer mu.Unlock()
		return requests
	}
	home := webhookAccount(t, server.URL+"/hook")
	stateFile := filepath.Join(home, ".claude", pluginName, "state.json")
	openUntil := nowSec() + 900
	cliWrite(t, stateFile, []byte(`{"webhook": {"failures": 3, "openUntil": `+strconv.FormatInt(openUntil, 10)+`}}`))

	run := startNoctisCLIAt(t, home, "", nil, "webhook", "--title", "T", "--body", "B")()
	closes := time.Unix(openUntil, 0).Format("15:04")
	if run.code != 1 || !strings.Contains(run.stderr, "webhooks pause until") || !strings.Contains(run.stderr, closes) {
		t.Fatalf("a message held by an open circuit must say that it waits until %s and exit 1:\n%s", closes, run)
	}
	if n := sent(); n != 0 {
		t.Fatalf("a message went out through an open circuit: %d request(s)", n)
	}

	run = startNoctisCLIAt(t, home, "", nil, "webhook")()
	if run.code != 0 || !strings.Contains(run.stdout, "delivered") {
		t.Fatalf("a test without --title and --body must go out through an open circuit:\n%s", run)
	}
	if n := sent(); n != 1 {
		t.Fatalf("the test through an open circuit made %d request(s), want 1", n)
	}
	var state struct {
		Webhook struct{ Failures, OpenUntil float64 }
	}
	if err := json.Unmarshal(cliRead(t, stateFile), &state); err != nil {
		t.Fatal(err)
	}
	if state.Webhook.OpenUntil != 0 || state.Webhook.Failures != 0 {
		t.Fatalf("a delivered test left the circuit open: %+v", state.Webhook)
	}
}
