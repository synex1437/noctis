package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func doctorLineWith(t *testing.T, host, marker string) (string, []string) {
	t.Helper()
	previousHost, previousLocale := activeHost, locale
	t.Cleanup(func() { activeHost, locale = previousHost, previousLocale })
	activeHost, locale = host, "en"
	doctorIssues = 0
	lines := doctorLines(object{})
	for index, line := range lines {
		if strings.Contains(line, marker) {
			return line, lines[index+1:]
		}
	}
	t.Fatalf("the %s doctor has no line with %q:\n%s", host, marker, strings.Join(lines, "\n"))
	return "", nil
}

func TestTheDoctorFlagsAStatusLineWhoseBinaryIsGone(t *testing.T) {
	dir := sandboxFiles(t)
	gone := filepath.ToSlash(filepath.Join(dir, "plugins", "cache", "noctis", "noctis", "5.1.0", "bin", "noctis"))
	cliWrite(t, files.settings, []byte(`{"statusLine": {"type": "command", "command": "\"`+gone+`\" statusline"}}`))

	line, rest := doctorLineWith(t, "claude", "statusLine:")
	if !strings.HasPrefix(line, "!!") || !strings.Contains(line, gone) {
		t.Fatalf("a status line that runs a deleted binary is reported as %q", line)
	}
	if len(rest) == 0 || !strings.Contains(rest[0], "fix:") {
		t.Fatalf("the missing binary has no remedy: %q", rest)
	}
	if issues := selfCheckIssues(object{}); !strings.Contains(strings.Join(issues, "\n"), gone) {
		t.Fatalf("the session-start self-check does not name the missing binary: %q", issues)
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cliWrite(t, files.settings, []byte(`{"statusLine": {"type": "command", "command": "\"`+filepath.ToSlash(executable)+`\" statusline"}}`))
	if line, _ := doctorLineWith(t, "claude", "statusLine:"); !strings.HasPrefix(line, "OK") {
		t.Fatalf("a status line that runs a binary that exists is reported as %q", line)
	}
}

func TestTheHostDoctorFlagsHooksWhoseBinaryIsGone(t *testing.T) {
	dir := sandboxFiles(t)
	gone := filepath.ToSlash(filepath.Join(dir, "gone", "noctis", "plugin", "bin", "noctis"))
	cliWrite(t, filepath.Join(files.configDir, "hooks.json"), []byte(`{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "\"`+gone+`\" hook --host codex", "statusMessage": "noctis"}]}]}}`))

	line, _ := doctorLineWith(t, "codex", "hooks wired:")

	if !strings.HasPrefix(line, "!!") || !strings.Contains(line, gone) {
		t.Fatalf("Codex hooks that run a deleted binary are reported as %q", line)
	}
}

func TestTheCopilotDoctorFlagsHooksWhoseBinaryIsGoneInAFolderWithASpace(t *testing.T) {
	dir := sandboxFiles(t)
	gone := filepath.Join(dir, "John Smith", ".copilot", "noctis", "plugin", "bin", "noctis")
	if _, err := wireHostHooks("copilot", gone, files.configDir); err != nil {
		t.Fatal(err)
	}

	line, _ := doctorLineWith(t, "copilot", "hooks wired:")

	if !strings.HasPrefix(line, "!!") || !strings.Contains(line, gone) {
		t.Fatalf("Copilot hooks that run a deleted binary in a folder with a space are reported as %q", line)
	}

	present := filepath.Join(dir, "Jane Doe", ".copilot", "noctis", "plugin", "bin", "noctis")
	cliWrite(t, present, []byte("binary"))
	if _, err := wireHostHooks("copilot", present, files.configDir); err != nil {
		t.Fatal(err)
	}
	if line, _ := doctorLineWith(t, "copilot", "hooks wired:"); !strings.HasPrefix(line, "OK") {
		t.Fatalf("Copilot hooks that run a binary that exists in a folder with a space are reported as %q", line)
	}
}
