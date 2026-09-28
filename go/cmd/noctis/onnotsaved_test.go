package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnSaysWhenTurningBackOnWasNotSaved(t *testing.T) {
	t.Run("state.json cannot be read", func(t *testing.T) {
		account := t.TempDir()
		if err := os.MkdirAll(filepath.Join(account, pluginName, "state.json"), 0o755); err != nil {
			t.Fatal(err)
		}

		run := runNoctisCLI(t, map[string]string{"CLAUDE_CONFIG_DIR": account}, "on")

		if run.code != 1 || strings.Contains(run.stdout, "active.") || !strings.Contains(run.stderr, "could not be saved") {
			t.Fatalf("on reported noctis active although it could not save that:\n%s", run)
		}
	})
	t.Run("the write of state.json fails", func(t *testing.T) {
		sandboxFiles(t)
		previousLocale, previousFailures := locale, writeFailures
		t.Cleanup(func() { locale, writeFailures = previousLocale, previousFailures })
		locale = "en"
		updateState(func(state object) { state["disabledUntil"] = float64(nowSec() + 3600) })
		before := cliRead(t, files.state)
		if err := os.MkdirAll(fmt.Sprintf("%s.%d.tmp", files.state, os.Getpid()), 0o755); err != nil {
			t.Fatal(err)
		}

		code := 0
		var stderr string
		stdout := capturedStdout(t, func() { stderr = capturedStderr(t, func() { code = resumeGuard() }) })

		if code != 1 || strings.Contains(stdout, "active.") || !strings.Contains(stderr, "could not be saved") {
			t.Fatalf("on reported noctis active while the pause state.json holds is still in force: exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
		}
		cliUnchanged(t, files.state, before)
	})
}
