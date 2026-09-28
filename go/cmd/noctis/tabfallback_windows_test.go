//go:build windows

package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTheWindowsLauncherStartsNothingOnceTheRunnerTookItsSpecBack(t *testing.T) {
	script, err := filepath.Abs(filepath.Join("..", "..", "..", "scripts", "launch.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	claude := copyTestBinary(t, filepath.Join(t.TempDir(), "claude.exe"))
	marker := filepath.Join(t.TempDir(), "claude-ran")
	t.Setenv("NOCTIS_TEST_POWERSHELL_MARKER", marker)
	spec := filepath.Join(t.TempDir(), "r5-ps.1.json")
	base := strings.TrimSuffix(spec, ".json")
	launchWindow := func() error {
		command := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script, "-Spec", spec, "-Attached")
		_, err := runWithTimeout(command, 2*time.Minute)
		return err
	}

	if err := launchWindow(); err != nil {
		t.Fatalf("a window that starts after the runner took its spec back should end quietly: %v", err)
	}
	if statSafe(base+".started") != nil || statSafe(marker) != nil {
		t.Fatal("a window that started after the runner took its spec back and opened a console launched claude as well")
	}

	mustWriteJSON(spec, object{"claude": claude, "cwd": t.TempDir(), "configDir": "", "sessionId": "r5-ps", "effort": "max", "arguments": "--resume r5-ps", "title": "noctis r5"})
	if err := launchWindow(); err != nil {
		t.Fatalf("the launcher failed with its spec in place: %v", err)
	}
	if statSafe(spec) != nil || statSafe(base+".taken.json") == nil {
		t.Fatal("the launcher did not take its spec before it started claude, so the runner cannot tell a window that starts late from one that never will")
	}
	for _, file := range []string{base + ".started", base + ".pid", marker} {
		if statSafe(file) == nil {
			t.Fatalf("with its spec in place the launcher did not start claude (%s missing)", file)
		}
	}
}
