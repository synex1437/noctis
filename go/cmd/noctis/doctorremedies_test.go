package main

import (
	"os"
	"strings"
	"testing"
)

func doctorRun(t *testing.T, host string, cfg object) []string {
	t.Helper()
	previousHost, previousLocale := activeHost, locale
	t.Cleanup(func() { activeHost, locale = previousHost, previousLocale })
	activeHost, locale = host, "en"
	doctorIssues = 0
	return doctorLines(cfg)
}

func remedyFor(t *testing.T, lines []string, marker string) (string, string) {
	t.Helper()
	for index, line := range lines {
		if strings.HasPrefix(line, "!!") && strings.Contains(line, marker) {
			if index+1 < len(lines) && strings.Contains(lines[index+1], "fix:") {
				return line, lines[index+1]
			}
			t.Errorf("%q says what is wrong but not what to run:\n%s", line, strings.Join(lines, "\n"))
			return line, ""
		}
	}
	t.Errorf("no failure line mentions %q:\n%s", marker, strings.Join(lines, "\n"))
	return "", ""
}

func doctorRemedySandbox(t *testing.T) {
	t.Helper()
	sandboxFiles(t)
	previousRoot := files.pluginRoot
	t.Cleanup(func() { files.pluginRoot = previousRoot })
	files.pluginRoot = t.TempDir()
	t.Setenv("PATH", t.TempDir())
	previousLookup := claudeLookup
	t.Cleanup(func() { claudeLookup = previousLookup })
	claudeLookup = func() string { return "" }
}

func TestEveryDoctorFailureSaysWhatToRun(t *testing.T) {
	t.Run("claude", func(t *testing.T) {
		doctorRemedySandbox(t)
		until := float64(nowSec() + 3600)
		updateState(func(state object) { state["disabledUntil"] = until })
		cliWrite(t, files.settings, []byte(`{"model": "opus",`))

		lines := doctorRun(t, "claude", object{"models": object{"effort": "max"}})

		if _, fix := remedyFor(t, lines, formatTime(until)); fix != "" && !strings.Contains(fix, "noctis on") {
			t.Errorf("the paused guard's remedy does not name noctis on: %q", fix)
		}
		if _, fix := remedyFor(t, lines, "claude:"); fix != "" && !strings.Contains(fix, "PATH") {
			t.Errorf("a missing claude's remedy does not mention PATH: %q", fix)
		}
		if _, fix := remedyFor(t, lines, "settings.json:"); fix != "" && !strings.Contains(fix, files.settings) {
			t.Errorf("a broken settings.json's remedy does not name the file: %q", fix)
		}
		remedyFor(t, lines, "plugin location:")
		remedyFor(t, lines, "agents/lite.md")
	})
	t.Run("settings.json that cannot be opened", func(t *testing.T) {
		doctorRemedySandbox(t)
		if err := os.MkdirAll(files.settings, 0o755); err != nil {
			t.Fatal(err)
		}

		lines := doctorRun(t, "claude", object{})

		if _, fix := remedyFor(t, lines, "settings.json:"); fix != "" && (!strings.Contains(fix, files.settings) || strings.Contains(fix, "JSON")) {
			t.Errorf("the remedy for a settings.json that cannot be opened asks to fix a JSON error, or does not name the file: %q", fix)
		}
	})
	t.Run("effort", func(t *testing.T) {
		doctorRemedySandbox(t)
		cliWrite(t, files.settings, []byte(`{"model": "opus", "env": {"CLAUDE_CODE_EFFORT_LEVEL": "medium"}}`))

		lines := doctorRun(t, "claude", object{"models": object{"effort": "max"}})

		line, fix := remedyFor(t, lines, "CLAUDE_CODE_EFFORT_LEVEL")
		if !strings.Contains(line, "medium") || !strings.Contains(line, "max") {
			t.Errorf("the effort line hides what config.json expects: %q", line)
		}
		if fix != "" && (!strings.Contains(fix, "max") || strings.Contains(fix, "missing")) {
			t.Errorf("the effort remedy calls a mismatch missing settings or leaves out the level: %q", fix)
		}
	})
	t.Run("codex", func(t *testing.T) {
		doctorRemedySandbox(t)

		lines := doctorRun(t, "codex", object{})

		if _, fix := remedyFor(t, lines, "Codex"); fix != "" && !strings.Contains(fix, "PATH") {
			t.Errorf("a missing codex's remedy does not mention PATH: %q", fix)
		}
		for _, marker := range []string{"hooks wired:", "config:"} {
			if _, fix := remedyFor(t, lines, marker); fix != "" && !strings.Contains(fix, "noctis setup --host codex") {
				t.Errorf("%s: the remedy does not name the setup that wires it: %q", marker, fix)
			}
		}
	})
}
