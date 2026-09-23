//go:build windows

package main

import (
	"os"
	"testing"
)

func TestSomeoneAtAWindowsConsoleIsAskedWhichToolAndRoles(t *testing.T) {
	console, err := os.Open("CONIN$")
	if err != nil {
		t.Skipf("this process has no console to read from: %v", err)
	}
	defer console.Close()
	saved := os.Stdin
	os.Stdin = console
	t.Cleanup(func() { os.Stdin = saved })
	if !stdinIsTerminal() {
		t.Fatal("stdin is the console, so someone can answer, but it was not seen as a terminal: install.ps1 and setup would skip the tool and roles questions and quietly install for Claude Code")
	}
}
