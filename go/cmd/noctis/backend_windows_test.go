//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTheWindowsLauncherHandsTheRelaunchItsBackend(t *testing.T) {
	script, err := filepath.Abs(filepath.Join("..", "..", "..", "scripts", "launch.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	claude := copyTestBinary(t, filepath.Join(t.TempDir(), "claude.exe"))
	marker := filepath.Join(t.TempDir(), "claude-ran")
	t.Setenv("NOCTIS_TEST_POWERSHELL_MARKER", marker)
	t.Setenv("NOCTIS_TEST_MARKER_ENV", "ANTHROPIC_BASE_URL")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	spec := filepath.Join(t.TempDir(), "lb-ps.1.json")
	mustWriteJSON(spec, object{"claude": claude, "cwd": t.TempDir(), "configDir": "", "sessionId": "lb-ps", "effort": "", "arguments": "--resume lb-ps", "title": "noctis lb",
		"env": object{"ANTHROPIC_BASE_URL": "http://localhost:11434"}})
	command := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script, "-Spec", spec, "-Attached")
	if _, err := runWithTimeout(command, 2*time.Minute); err != nil {
		t.Fatalf("the launcher failed: %v", err)
	}
	content, err := os.ReadFile(marker)
	if err != nil || !strings.Contains(string(content), "ANTHROPIC_BASE_URL=http://localhost:11434") {
		t.Fatalf("the relaunched claude did not get the backend its spec names (%v):\n%s", err, content)
	}
}
