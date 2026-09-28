//go:build windows

package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestAConsoleChildStartsWithoutAWindowAndKeepsItsCommandLine(t *testing.T) {
	plain := exec.Command("git", "status", "--short")
	hideConsoleWindow(plain)
	if attr := plain.SysProcAttr; attr == nil || !attr.HideWindow || attr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("git would open a console window from a detached runner: %+v", plain.SysProcAttr)
	}

	shell := windowsShell(`"C:\Program Files\claude\claude.cmd" -p "carry on"`)
	line := shell.SysProcAttr.CmdLine
	hideConsoleWindow(shell)
	if attr := shell.SysProcAttr; attr.CmdLine != line || !attr.HideWindow || attr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("hiding the window of a claude.cmd run lost its command line %q or kept the window: %+v", line, attr)
	}

	detached := exec.Command("noctis.exe", "sleeper")
	configureDetached(detached)
	flags := detached.SysProcAttr.CreationFlags
	hideConsoleWindow(detached)
	if detached.SysProcAttr.CreationFlags != flags {
		t.Fatalf("a detached process got CREATE_NO_WINDOW on top of DETACHED_PROCESS: %#x", detached.SysProcAttr.CreationFlags)
	}
}

func TestTheTaskFindsTheConsoleHostWhereWindowsHasPseudoConsoles(t *testing.T) {
	if syscall.NewLazyDLL("kernel32.dll").NewProc("CreatePseudoConsole").Find() != nil {
		t.Skip("this Windows has no pseudo consoles, so the task keeps its console window")
	}
	if host := headlessConsoleHost(); !strings.EqualFold(filepath.Base(host), "conhost.exe") {
		t.Fatalf("no console host was found to run the task's runner without a window: %q", host)
	}
}
