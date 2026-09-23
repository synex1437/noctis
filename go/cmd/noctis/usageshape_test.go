package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func usageAnswers(t *testing.T, body string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	t.Setenv("NOCTIS_USAGE_URL", server.URL)
}

func TestAUsageAnswerOfAnUnknownShapeKeepsTheLastReading(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "token")
	cfg := object{"fable": object{"source": "oauth"}, "models": object{"scopedPattern": "fable"}}
	now := nowSec()
	mustWriteJSON(files.fable, object{"fetchedAt": float64(now - 3600),
		"five_hour": object{"used": float64(91), "resetsAt": float64(now + 3600)},
		"seven_day": object{"used": float64(40), "resetsAt": float64(now + 86400)}})
	usageAnswers(t, `{"rate_limit_windows":[{"name":"session","percent":40}]}`)

	for round := int64(0); round < 2; round++ {
		next := refreshFable(cfg, now+round, "probe", 0, true)
		if win := getMap(next, "five_hour"); win == nil || numberOr(win, "used", 0) != 91 {
			t.Fatalf("an answer in a shape noctis does not know wiped the last five-hour reading: %v", next)
		}
		if numberOr(next, "fetchedAt", 0) != float64(now-3600) {
			t.Fatalf("an answer in a shape noctis does not know stamped the old reading as fresh: %v", next)
		}
		if getString(next, "error") != "unexpected-shape" || numberOr(next, "backoffUntil", 0) < float64(now+round+600) {
			t.Fatalf("the unknown shape was not recorded with a 10-minute back-off: %v", next)
		}
	}
	content, _ := os.ReadFile(files.errors)
	if count := strings.Count(string(content), "shape noctis does not know"); count != 1 {
		t.Fatalf("two answers of the same unknown shape were warned about %d times, want once:\n%s", count, content)
	}

	usageAnswers(t, `{"five_hour":null,"seven_day":null,"seven_day_fable":null}`)
	next := refreshFable(cfg, now+2, "probe", 0, true)
	if getMap(next, "five_hour") != nil || numberOr(next, "fetchedAt", 0) != float64(now+2) || getString(next, "error") != "" {
		t.Fatalf("an answer with the known keys and no windows (an idle account) must still replace the reading: %v", next)
	}
}
