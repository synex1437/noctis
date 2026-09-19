package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
