//go:build !windows

package main

import "os/exec"

func windowsShell(line string) *exec.Cmd {
	return exec.Command("cmd.exe", "/d", "/s", "/c", line)
}
