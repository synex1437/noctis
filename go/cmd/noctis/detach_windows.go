//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// The process creation flags hideConsoleWindow reads and sets.
const (
	detachedProcess  = 0x00000008
	createNewConsole = 0x00000010
	createNoWindow   = 0x08000000
)

func configureDetached(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess}
}

func isolateTree(_ *exec.Cmd) {}

func killTree(process *os.Process) {
	_ = terminateProcess(process.Pid)
	_ = process.Kill()
}

// hideConsoleWindow starts a console program in a console that has no window. A runner that Task
// Scheduler or a detached parent started has no visible console a child could share, so each console
// child (PowerShell, git, taskkill, a headless session) would otherwise open a window of its own, and
// closing that window ends the child. A command that asks for a new console or for none keeps what it
// asked for, and the command line windowsShell sets stays.
func hideConsoleWindow(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	if command.SysProcAttr.CreationFlags&(detachedProcess|createNewConsole) != 0 {
		return
	}
	command.SysProcAttr.HideWindow = true
	command.SysProcAttr.CreationFlags |= createNoWindow
}

// headlessConsoleHost returns the console host a scheduled task can start the runner in without a
// window: conhost.exe, whose --headless mode came with the pseudo consoles of Windows 10 1809, so it
// is named only where kernel32 has CreatePseudoConsole. Elsewhere it returns "".
func headlessConsoleHost() string {
	root := os.Getenv("SystemRoot")
	if root == "" || syscall.NewLazyDLL("kernel32.dll").NewProc("CreatePseudoConsole").Find() != nil {
		return ""
	}
	host := filepath.Join(root, "System32", "conhost.exe")
	if info, err := os.Stat(host); err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return host
}
