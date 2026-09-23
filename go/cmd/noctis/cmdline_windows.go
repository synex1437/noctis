//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

func windowsShell(line string) *exec.Cmd {
	command := exec.Command("cmd.exe")
	command.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd.exe " + windowsShellArgument(line)}
	return command
}
