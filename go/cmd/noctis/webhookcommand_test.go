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

func TestAGenericWebhookNamesTheAccountFolderButNotThePathToIt(t *testing.T) {
	var mu sync.Mutex
	var sent []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		sent = body
		mu.Unlock()
	}))
	defer server.Close()
	home := webhookAccount(t, server.URL+"/hook")
	run := startNoctisCLIAt(t, home, "", nil, "webhook", "--title", "T", "--body", "B")()
	if run.code != 0 {
		t.Fatalf("the message was not delivered:\n%s", run)
	}
	mu.Lock()
	defer mu.Unlock()
	var payload map[string]any
	if err := json.Unmarshal(sent, &payload); err != nil {
		t.Fatalf("the generic payload is not JSON: %s", sent)
	}
	for key, value := range payload {
		if text, _ := value.(string); strings.Contains(text, home) {
			t.Errorf("the generic webhook sent the path of the account folder, and with it the user name, in %q: %s", key, text)
		}
	}
	if payload["account"] != ".claude" {
		t.Errorf("the generic webhook must still name the account by its folder, .claude; it sent %v", payload["account"])
	}
}

func TestEveryWebhookPresetShowsTheHomeFolderAsATilde(t *testing.T) {
	var mu sync.Mutex
	var title string
	var sent []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		title, sent = r.Header.Get("Title"), body
		mu.Unlock()
	}))
	defer server.Close()
	for _, preset := range []string{"generic", "telegram", "discord", "slack", "ntfy"} {
		t.Run(preset, func(t *testing.T) {
			home := t.TempDir()
			cliWrite(t, filepath.Join(home, ".claude", pluginName, "config.json"), []byte(`{"alarm": {"webhook": {"url": "`+server.URL+`/hook", "preset": "`+preset+`", "chatId": "42"}}}`))
			project := filepath.Join(home, "work", "app")
			run := startNoctisCLIAt(t, home, "", nil, "webhook", "--title", "Paused in "+project, "--body", "open a terminal in "+project+" or in "+home)()
			if run.code != 0 {
				t.Fatalf("the message was not delivered:\n%s", run)
			}
			mu.Lock()
			texts := []string{title}
			var payload map[string]any
			if json.Unmarshal(sent, &payload) == nil {
				for _, value := range payload {
					if text, ok := value.(string); ok {
						texts = append(texts, text)
					}
				}
			} else {
				texts = append(texts, string(sent))
			}
			mu.Unlock()
			got := strings.Join(texts, "\n")
			if strings.Contains(got, home) {
				t.Errorf("the %s webhook sent the home folder, and with it the user name: %s", preset, got)
			}
			if strings.Count(got, filepath.Join("~", "work", "app")) != 2 || !strings.Contains(got, "or in ~") {
				t.Errorf("the %s webhook must show the folders under home as ~ in the title and the body: %s", preset, got)
			}
		})
	}
}

func TestTheHomeFolderBecomesATildeOnlyWhereItsNameEnds(t *testing.T) {
	home := filepath.Join(t.TempDir(), "al")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	got := homeAsTilde("in " + filepath.Join(home, "app") + ", " + home + "ice and " + home + ".")
	if want := "in " + filepath.Join("~", "app") + ", " + home + "ice and ~."; got != want {
		t.Errorf("home %s:\n got %s\nwant %s", home, got, want)
	}
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
