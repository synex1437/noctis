package main

import (
	"strings"
	"testing"
)

func scopedDataLine(t *testing.T, cfg object) string {
	t.Helper()
	for _, line := range strings.Split(describeState(cfg, object{}, usageView{}, nowSec()), "\n") {
		if strings.HasPrefix(line, "Scoped data") {
			return line
		}
	}
	t.Fatal("status has no scoped data line")
	return ""
}

func TestStatusAndDoctorNameARefreshProblemInWords(t *testing.T) {
	sandboxFiles(t)
	previousHost, previousLocale := activeHost, locale
	t.Cleanup(func() { activeHost, locale = previousHost, previousLocale })
	locale = "en"
	cfg := object{"fable": object{"source": "oauth"}}
	cases := []struct{ stored, says, code string }{
		{"no-token", "sign-in", "no-token"},
		{`http-0 Get "https://api.anthropic.com/api/oauth/usage": dial tcp: lookup api.anthropic.com: no such host`, "could not reach the usage endpoint: dial tcp: lookup api.anthropic.com: no such host", "http-0"},
		{"http-0 truncated-response", "the usage endpoint's answer broke off before it was complete", "truncated-response"},
		{"http-0 truncated-response unexpected EOF", "the usage endpoint's answer broke off before it was complete", "http-0"},
		{"http-401", "HTTP 401", "http-401"},
		{"http-429", "rate-limit", "http-429"},
		{"http-500", "HTTP 500", "http-500"},
		{"bad-json invalid character 'x' looking for beginning of value", "not JSON", "bad-json"},
		{"unexpected-shape", "shape", "unexpected-shape"},
	}
	for _, tc := range cases {
		mustWriteJSON(files.fable, object{"error": tc.stored, "backoffUntil": float64(nowSec() + 600)})
		if line := scopedDataLine(t, cfg); !strings.Contains(line, tc.says) || strings.Contains(line, tc.code) {
			t.Errorf("stored %q: status shows %q, want it to say %q and not the code %q", tc.stored, line, tc.says, tc.code)
		}
	}
	mustWriteJSON(files.fable, object{"fetchedAt": float64(nowSec()), "note": "no-scoped-bucket-in-response"})
	if line := scopedDataLine(t, cfg); !strings.Contains(line, "no Fable bucket") || strings.Contains(line, "no-scoped-bucket-in-response") {
		t.Errorf("status shows the internal note: %q", line)
	}

	activeHost = "codex"
	mustWriteJSON(files.fable, object{"error": "codex codex executable not found", "backoffUntil": float64(nowSec() + 600)})
	doctorIssues = 0
	for _, line := range doctorLines(object{}) {
		if strings.Contains(line, "usage.json:") && (strings.Contains(line, "codex codex") || !strings.Contains(line, "Codex")) {
			t.Errorf("the Codex doctor shows the internal refresh code: %q", line)
		}
	}
}

func TestARefreshProblemSaysWhenNoctisTriesAgain(t *testing.T) {
	sandboxFiles(t)
	previousHost, previousLocale := activeHost, locale
	t.Cleanup(func() { activeHost, locale = previousHost, previousLocale })
	locale = "en"
	until := float64(nowSec() + 600)
	next := "next try after " + formatTime(until)

	mustWriteJSON(files.fable, object{"error": "http-429", "backoffUntil": until})
	if line := scopedDataLine(t, object{"fable": object{"source": "oauth"}}); !strings.Contains(line, next) || strings.Contains(line, "later") {
		t.Errorf("status does not say when noctis asks the usage endpoint again: %q, want %q in it", line, next)
	}

	activeHost = "codex"
	mustWriteJSON(files.fable, object{"error": "codex codex executable not found", "backoffUntil": until})
	doctorIssues = 0
	usageLine := ""
	for _, line := range doctorLines(object{}) {
		if strings.Contains(line, "usage.json:") {
			usageLine = line
		}
	}
	if !strings.Contains(usageLine, next) {
		t.Errorf("the Codex doctor does not say when noctis asks Codex again: %q, want %q in it", usageLine, next)
	}
}
