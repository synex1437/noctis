package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func errorsLogEntry(at time.Time, message string) string {
	return at.UTC().Format("2006-01-02T15:04:05.000Z") + " [WARN 4242 hook] " + message + "\n"
}

func doctorErrorsLine(t *testing.T, host string) (string, []string) {
	t.Helper()
	previousHost, previousLocale := activeHost, locale
	t.Cleanup(func() { activeHost, locale = previousHost, previousLocale })
	activeHost, locale = host, "en"
	doctorIssues = 0
	lines := doctorLines(object{})
	for index, line := range lines {
		if strings.Contains(line, "errors.log:") {
			return line, lines[index+1:]
		}
	}
	t.Fatalf("the %s doctor has no errors.log line:\n%s", host, strings.Join(lines, "\n"))
	return "", nil
}

func TestTheDoctorCountsOnlyTheLastDaysErrors(t *testing.T) {
	for _, host := range []string{"claude", "codex"} {
		t.Run(host, func(t *testing.T) {
			sandboxFiles(t)
			old := errorsLogEntry(time.Now().Add(-49*time.Hour), `fable refresh failed (session-start): http-0 Get "https://api.anthropic.com/api/oauth/usage": dial tcp: lookup api.anthropic.com: no such host`)
			cliWrite(t, files.errors, []byte(old))

			line, _ := doctorErrorsLine(t, host)
			if !strings.HasPrefix(line, "OK") {
				t.Fatalf("an entry from two days ago still fails the doctor: %q", line)
			}

			handle, err := os.OpenFile(files.errors, os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			_, err = handle.WriteString(errorsLogEntry(time.Now().Add(-time.Minute), "webhook failed (ntfy): http-500"))
			handle.Close()
			if err != nil {
				t.Fatal(err)
			}

			line, rest := doctorErrorsLine(t, host)
			if !strings.HasPrefix(line, "!!") || !strings.Contains(line, "webhook failed") || !strings.Contains(line, ": 1 ") {
				t.Fatalf("an entry from a minute ago is not reported as the one recent problem: %q", line)
			}
			if len(rest) == 0 || !strings.Contains(rest[0], "fix:") || !strings.Contains(rest[0], files.errors) {
				t.Fatalf("the recent entry has no remedy naming the file: %q", rest)
			}
		})
	}
}

func TestAPlanWithoutTheScopedBucketIsNotLoggedAsAnError(t *testing.T) {
	sandboxFiles(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"five_hour": {"utilization": 10, "resets_at": null}, "seven_day": {"utilization": 20, "resets_at": null}}`))
	}))
	defer server.Close()
	t.Setenv("NOCTIS_USAGE_URL", server.URL)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "token")

	next := refreshFable(object{"fable": object{"source": "oauth"}}, nowSec(), "test", 0, true)

	if getString(next, "note") != "no-scoped-bucket-in-response" {
		t.Fatalf("the answer did not take the no-bucket path: %v", next)
	}
	if content, _ := os.ReadFile(files.errors); len(content) > 0 {
		t.Fatalf("a plan without the scoped bucket is logged as an error, so the doctor fails on it:\n%s", content)
	}
	if content, _ := os.ReadFile(files.log); !strings.Contains(string(content), "no Fable bucket") {
		t.Fatalf("the note is not in guard.log either:\n%s", content)
	}
}

func TestNotesNoctisLeavesForItselfDoNotFailTheDoctor(t *testing.T) {
	cfg, project := agentSessionSandbox(t, 20)
	t.Setenv("NOCTIS_UPDATE_URL", "off")
	previousHost := activeHost
	t.Cleanup(func() { activeHost = previousHost })
	activeHost = "claude"
	mustWriteJSON(files.settings, object{"statusLine": object{"type": "command", "command": "ccstatusline"}})
	if err := os.Remove(files.usage); err != nil {
		t.Fatal(err)
	}

	start := hookOutput(t, onSessionStart, agentHookInput("SessionStart", "notes-1", project, object{"source": "startup"}), cfg)
	if !strings.Contains(getString(start, "systemMessage"), T("selfcheck.statusline")) || !strings.Contains(getString(start, "systemMessage"), T("selfcheck.token", scopedLabel(cfg))) {
		t.Fatalf("the startup self-check did not tell the user about the foreign status line and the missing sign-in: %v", start)
	}
	if getString(readJSON(files.fable), "error") != "no-token" {
		t.Fatalf("the session start did not try the usage refresh without a sign-in: %v", readJSON(files.fable))
	}

	path := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())
	files.notifyScript = filepath.Join(files.pluginRoot, "scripts", "notify.ps1")
	previousWindows := isWindows
	t.Cleanup(func() { isWindows = previousWindows })
	for _, windows := range []bool{previousWindows, true} {
		isWindows = windows
		if reason := notify(object{}, pluginName, "probe"); reason == "" {
			t.Fatalf("a machine without a desktop notifier reported the notification as shown (windows %v)", windows)
		}
	}
	isWindows = previousWindows
	t.Setenv("PATH", path)

	if content, _ := os.ReadFile(files.errors); len(content) > 0 {
		t.Fatalf("notes noctis already showed or that describe the machine are logged as errors, so the doctor fails on them for a day:\n%s", content)
	}
	logged, _ := os.ReadFile(files.log)
	for _, note := range []string{"self-check:", "no usable OAuth token", "no desktop notifier here"} {
		if !strings.Contains(string(logged), note) {
			t.Fatalf("guard.log lost the note %q:\n%s", note, logged)
		}
	}
	if line, _ := doctorErrorsLine(t, "claude"); !strings.HasPrefix(line, "OK") {
		t.Fatalf("the doctor fails on notes noctis wrote itself: %q", line)
	}
}
