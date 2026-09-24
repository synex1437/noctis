//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakePowershell(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	copyTestBinary(t, filepath.Join(bin, "powershell.exe"))
	marker := filepath.Join(t.TempDir(), "ran")
	t.Setenv("NOCTIS_TEST_POWERSHELL_MARKER", marker)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return marker
}

func TestNoToastRunsWithoutATrustedNotifyScript(t *testing.T) {
	sandboxFiles(t)
	marker := fakePowershell(t)
	files.notifyScript = ""

	notify(object{}, "title", "body")
	time.Sleep(2 * time.Second)
	if ran, err := os.ReadFile(marker); err == nil {
		t.Fatalf("PowerShell was started for a toast without a trusted notify.ps1: %s", ran)
	}

	files.notifyScript = filepath.Join(t.TempDir(), "notify.ps1")
	notify(object{}, "title", "body")
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the stand-in PowerShell never ran for a trusted notify.ps1, so the check above proves nothing")
		}
	}
}

func TestAWindowRelaunchWithoutATrustedLauncherRunsHeadless(t *testing.T) {
	home := relaunchSandbox(t)
	marker := fakePowershell(t)
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls.log")
	writeScript(t, filepath.Join(bin, "claude.cmd"), "@echo off\r\nif \"%1\"==\"--help\" exit /b 0\r\necho %*>>\""+calls+"\"\r\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	files.launchScript = ""

	result := launchClaude(object{"resume": object{"mode": "window"}}, launchSpec{sid: "w1", cwd: home, prompt: "carry on"})

	if !result.started || result.window {
		t.Fatalf("a window relaunch without a trusted launch.ps1 should run headless, got %+v", result)
	}
	if recorded, err := os.ReadFile(calls); err != nil || !strings.HasPrefix(strings.TrimSpace(string(recorded)), "-p ") {
		t.Fatalf("claude was not run headless: %q %v", recorded, err)
	}
	if ran, err := os.ReadFile(marker); err == nil {
		t.Fatalf("PowerShell was started without a trusted launch.ps1: %s", ran)
	}
}
