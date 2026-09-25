package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func bundleEntries(t *testing.T, file string) map[string]string {
	t.Helper()
	archive, err := zip.OpenReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	entries := map[string]string{}
	for _, entry := range archive.File {
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[entry.Name] = string(content)
	}
	return entries
}

func TestABundleLeavesOutParkedPromptsTaskSubjectsAndPermissionRules(t *testing.T) {
	home := t.TempDir()
	account := filepath.Join(home, ".claude")
	now := float64(nowSec())
	prompt := "Rotate the Stripe key in deploy/prod.env for ACME Corp"
	subject := "Email the ACME contract to legal@acme.example"
	cliWrite(t, filepath.Join(account, pluginName, "state.json"), marshalPretty(object{
		"waits": object{"s1": object{
			"kind": "prompt", "window": "five_hour", "used": float64(93), "until": now + 7200, "resumeAt": now + 7290,
			"startedAt": now, "heartbeat": now, "queuedPrompt": prompt,
		}},
		"tasks": object{"s1": object{"at": now, "items": object{"t1": object{"subject": subject, "status": "open", "at": now}}}},
	}))
	rules := []string{"Bash(./deploy-acme.sh:*)", "Read(~/clients/acme/**)", "Read(~/.ssh/**)", "/srv/acme-billing"}
	cliWrite(t, filepath.Join(account, "settings.json"), marshalPretty(object{"model": "opus", "permissions": object{
		"defaultMode": "auto", "allow": []any{rules[0], rules[1]}, "deny": []any{rules[2]}, "additionalDirectories": []any{rules[3]},
	}}))
	target := filepath.Join(home, "bundle.zip")
	run := startNoctisCLIAt(t, home, "", nil, "report", "--bundle", target)()
	if run.code != 0 {
		t.Fatalf("noctis report --bundle failed:\n%s", run)
	}
	entries := bundleEntries(t, target)
	for name, content := range entries {
		for _, private := range append([]string{prompt, subject}, rules...) {
			if strings.Contains(content, private) {
				t.Errorf("%s in the bundle carries %q", name, private)
			}
		}
	}
	for _, want := range []string{
		fmt.Sprintf(`"queuedPrompt": "<redacted prompt, length %d>"`, utf8.RuneCountInString(prompt)),
		fmt.Sprintf(`"subject": "<redacted task, length %d>"`, utf8.RuneCountInString(subject)),
	} {
		if !strings.Contains(entries["state.json"], want) {
			t.Errorf("state.json in the bundle must keep the length of what it leaves out (%s):\n%s", want, entries["state.json"])
		}
	}
	settings := entries["settings-subset.json"]
	for _, want := range []string{`"defaultMode": "auto"`, `"allow": "<redacted list, length 2>"`, `"deny": "<redacted list, length 1>"`, `"additionalDirectories": "<redacted list, length 1>"`} {
		if !strings.Contains(settings, want) {
			t.Errorf("settings-subset.json in the bundle must keep %s:\n%s", want, settings)
		}
	}
}

func TestABundleLeavesOutWhatAWorkflowWasLaunchedWith(t *testing.T) {
	home := t.TempDir()
	account := filepath.Join(home, ".claude")
	now := float64(nowSec())
	launch := object{
		"at": now, "name": "acme-billing-migration", "script_path": "/srv/acme/migrate.js", "description": "Move ACME billing to the new cluster",
		"script": "agent('Rotate the Stripe key in deploy/prod.env for ACME Corp')", "args": "--client acme --region eu-west",
	}
	cutOff := object{"at": now, "agent": "a1b2c3", "agentType": "general-purpose", "until": now + 3600}
	cliWrite(t, filepath.Join(account, pluginName, "state.json"), marshalPretty(object{"workflows": object{"s1": []any{launch, cutOff}}}))
	target := filepath.Join(home, "bundle.zip")
	run := startNoctisCLIAt(t, home, "", nil, "report", "--bundle", target)()
	if run.code != 0 {
		t.Fatalf("noctis report --bundle failed:\n%s", run)
	}
	entries := bundleEntries(t, target)
	for _, key := range []string{"name", "script_path", "description", "script", "args"} {
		text := getString(launch, key)
		for name, content := range entries {
			if strings.Contains(content, text) {
				t.Errorf("%s in the bundle carries the workflow's %s %q", name, key, text)
			}
		}
		if want := fmt.Sprintf(`"%s": "<redacted workflow %s, length %d>"`, key, key, utf8.RuneCountInString(text)); !strings.Contains(entries["state.json"], want) {
			t.Errorf("state.json in the bundle must keep the length of the workflow's %s (%s):\n%s", key, want, entries["state.json"])
		}
	}
	for _, want := range []string{`"agent": "a1b2c3"`, `"agentType": "general-purpose"`} {
		if !strings.Contains(entries["state.json"], want) {
			t.Errorf("state.json in the bundle must keep the record of a cut-off agent (%s):\n%s", want, entries["state.json"])
		}
	}
}

func TestABundleShowsAHomeWithABackslashAsATilde(t *testing.T) {
	home := filepath.Join(t.TempDir(), `back\slash`)
	account := filepath.Join(home, ".claude")
	now := float64(nowSec())
	project := filepath.Join(home, "work", "app")
	cliWrite(t, filepath.Join(account, pluginName, "state.json"), marshalPretty(object{"waits": object{"s1": object{
		"kind": "batch", "window": "five_hour", "used": float64(93), "until": now + 7200, "resumeAt": now + 7290,
		"startedAt": now, "heartbeat": now, "cwd": project,
	}}}))
	target := filepath.Join(t.TempDir(), "bundle.zip")
	run := startNoctisCLIAt(t, home, "", nil, "report", "--bundle", target)()
	if run.code != 0 {
		t.Fatalf("noctis report --bundle failed:\n%s", run)
	}
	escaped := string(marshalCompact(home))
	entries := bundleEntries(t, target)
	for name, content := range entries {
		for _, form := range []string{home, forwardSlashes(home), escaped[1 : len(escaped)-1]} {
			if strings.Contains(content, form) {
				t.Errorf("%s in the bundle carries the home folder as %s", name, form)
			}
		}
	}
	if want := `"cwd": ` + string(marshalCompact(filepath.Join("~", "work", "app"))); !strings.Contains(entries["state.json"], want) {
		t.Errorf("state.json in the bundle must show the project folder under ~ (%s):\n%s", want, entries["state.json"])
	}
}

func TestTheBundleStateEntryRedactsOrStandsIn(t *testing.T) {
	prompt := "Rotate the Stripe key"
	parked := marshalPretty(object{"waits": object{"s1": object{"queuedPrompt": prompt}}})
	for name, content := range map[string][]byte{
		"a state.json that does not parse":   []byte(`{"waits": {"s1": {"queuedPrompt": "Rotate the Stripe key"`),
		"a state.json that is not an object": []byte(`["Rotate the Stripe key"]`),
	} {
		want := fmt.Sprintf("state.json is not a JSON object (%d bytes); left out\n", len(content))
		if got := string(bundleState(content)); got != want {
			t.Errorf("%s must stand in as %q, got %q", name, want, got)
		}
	}
	got := string(bundleState(append([]byte("\xef\xbb\xbf"), parked...)))
	if strings.Contains(got, prompt) || !strings.Contains(got, fmt.Sprintf("<redacted prompt, length %d>", utf8.RuneCountInString(prompt))) {
		t.Errorf("a state.json with a byte-order mark must be read and redacted like any other:\n%s", got)
	}
}

func TestRedactBundleText(t *testing.T) {
	text := "Bearer abc.def-ghi and sk-ant-api03-XYZ plus https://hooks.slack.com/services/T/B/x and https://discord.com/api/webhooks/1/abc"
	got := redactBundleText(text)
	for _, secret := range []string{"abc.def-ghi", "sk-ant-api03-XYZ", "T/B/x", "webhooks/1/abc"} {
		if strings.Contains(got, secret) {
			t.Errorf("secret %q survived redaction: %s", secret, got)
		}
	}
	if !strings.Contains(got, "<redacted-token>") || !strings.Contains(got, "<redacted-webhook>") {
		t.Errorf("placeholders missing: %s", got)
	}
}

func TestShippedChecksum(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "SHA256SUMS"), []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  linux-amd64/noctis\nbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb *windows-amd64/noctis.exe\nshort  darwin-amd64/noctis\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := shippedChecksum(dir, "linux-amd64/noctis"); got != strings.Repeat("a", 64) {
		t.Errorf("want the 64-hex sum, got %q", got)
	}
	if got := shippedChecksum(dir, "windows-amd64/noctis.exe"); got != strings.Repeat("b", 64) {
		t.Errorf("want the 64-hex sum with the binary-mode star stripped, got %q", got)
	}
	if got := shippedChecksum(dir, "darwin-arm64/noctis"); got != "" {
		t.Errorf("want empty for an unlisted file, got %q", got)
	}
	if got := shippedChecksum(dir, "darwin-amd64/noctis"); got != "" {
		t.Errorf("a malformed checksum must be ignored, got %q", got)
	}
}
