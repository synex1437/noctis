//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func configureDetached(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008}
}

func isolateTree(_ *exec.Cmd) {}

func killTree(process *os.Process) {
	_ = terminateProcess(process.Pid)
	_ = process.Kill()
}
