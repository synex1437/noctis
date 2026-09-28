package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// r4HiddenConsoles records each program the engine asks to start without a console window, as its
// name (lower case, without .exe) followed by its arguments.
func r4HiddenConsoles(t *testing.T) func() []string {
	t.Helper()
	var lock sync.Mutex
	recorded := []string{}
	previous := hideConsole
	hideConsole = func(command *exec.Cmd) {
		name := strings.TrimSuffix(strings.ToLower(filepath.Base(command.Path)), ".exe")
		lock.Lock()
		recorded = append(recorded, strings.Join(append([]string{name}, command.Args[1:]...), " "))
		lock.Unlock()
		previous(command)
	}
	t.Cleanup(func() { hideConsole = previous })
	return func() []string {
		lock.Lock()
		defer lock.Unlock()
		return slices.Clone(recorded)
	}
}

// r4StandIns puts copies of the test binary on PATH under names, each of which exits as soon as it
// starts (the NOCTIS_TEST_POWERSHELL_MARKER stand-in of pluginscripts_test.go).
func r4StandIns(t *testing.T, names ...string) map[string]string {
	t.Helper()
	bin := t.TempDir()
	placed := map[string]string{}
	for _, name := range names {
		file := name
		if isWindows && filepath.Ext(file) == "" {
			file += ".exe"
		}
		placed[name] = copyTestBinary(t, filepath.Join(bin, file))
	}
	t.Setenv("NOCTIS_TEST_POWERSHELL_MARKER", filepath.Join(t.TempDir(), "ran"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return placed
}

func r4Hidden(hidden []string, prefix string) bool {
	return slices.ContainsFunc(hidden, func(entry string) bool { return strings.HasPrefix(entry, prefix) })
}

func TestTheEngineStartsItsHelpersWithoutAConsoleWindow(t *testing.T) {
	sandboxFiles(t)
	standIns := r4StandIns(t, "powershell.exe", "codex", "git")
	hidden := r4HiddenConsoles(t)

	runPowershell("Get-ScheduledTask -TaskName 'Noctis-r4'", 30*time.Second)
	_, _ = fetchCodexRateLimits(standIns["codex"], 5*time.Second)
	_, _ = runWithTimeout(exec.Command(standIns["git"], "status", "--short"), 30*time.Second)

	for _, helper := range []string{"powershell -NoProfile", "codex app-server", "git status"} {
		if !r4Hidden(hidden(), helper) {
			t.Errorf("%s was started with a console window of its own: from a runner that Task Scheduler or a detached parent started, it opens one on the desktop (hidden: %q)", helper, hidden())
		}
	}
}

func TestAHeadlessRelaunchRunsWithoutAConsoleWindow(t *testing.T) {
	home := relaunchSandbox(t)
	standIns := r4StandIns(t, "claude", "codex")
	hidden := r4HiddenConsoles(t)

	launchClaude(object{"resume": object{"mode": "headless"}}, launchSpec{sid: "r4-headless", cwd: home, mode: "headless"})
	launchHostSession(object{}, hostOf("codex"), standIns["codex"], launchSpec{sid: "thr_r4", cwd: home})

	for _, session := range []string{"claude -p", "codex exec resume thr_r4"} {
		if !r4Hidden(hidden(), session) {
			t.Errorf("the headless relaunch %q was started with a console window of its own, a blank window that ends the session when closed (hidden: %q)", session, hidden())
		}
	}
}

func TestTheConsoleLauncherOpensNoWindowOfItsOwnButTheTerminalTabIsShown(t *testing.T) {
	sandboxFiles(t)
	r4StandIns(t, "wt", "powershell.exe")
	files.launchScript = filepath.Join(t.TempDir(), "launch.ps1")
	previousStart, previousPid := launchStartTimeout, launchPidTimeout
	launchStartTimeout, launchPidTimeout = 300*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { launchStartTimeout, launchPidTimeout = previousStart, previousPid })
	hidden := r4HiddenConsoles(t)

	launchInWindowsTerminal(object{}, launchSpec{sid: "r4-window", cwd: t.TempDir()}, "claude", []string{"--resume", "r4-window"}, os.Environ(), "max")

	if !r4Hidden(hidden(), "powershell -NoProfile") {
		t.Errorf("the console launcher opened a blank PowerShell window next to the claude window it starts (hidden: %q)", hidden())
	}
	if r4Hidden(hidden(), "wt ") {
		t.Errorf("the Windows Terminal tab is the window the user is meant to see, yet it was started hidden (hidden: %q)", hidden())
	}
}

func TestTheScheduledTaskStartsTheRunnerInAConsoleWithoutAWindow(t *testing.T) {
	sandboxFiles(t)
	conhost := `C:\Windows\System32\conhost.exe`
	previous := taskConsoleHost
	t.Cleanup(func() { taskConsoleHost = previous })
	sid := "0f4a6c1e-5d3b-4a8e-9c2f-7b1d3e5a9c20"
	cases := []struct {
		name, consoleHost, account string
		extra                      []string
		hidden                     bool
	}{
		{"a profile path without a space", conhost, `C:\Users\dev\.claude`, nil, true},
		{"another tool's account", conhost, `C:\Users\dev\.codex`, []string{"--host", "codex"}, true},
		{"a profile path with a space", conhost, `C:\Users\dev team\.claude`, nil, false},
		{"a profile path with a character cmd.exe reads as syntax", conhost, `C:\Users\R&D\.claude`, nil, false},
		{"a profile path with a typographic apostrophe", conhost, `C:\Users\dev’s\.claude`, nil, false},
		{"a Windows without pseudo consoles", "", `C:\Users\dev\.claude`, nil, false},
	}
	for _, tc := range cases {
		taskConsoleHost = func() string { return tc.consoleHost }
		files.guardDir = tc.account + `\noctis`
		launcher := files.guardDir + `\runner.cmd`
		arguments := append([]string{"resume", "--sid", sid, "--account", `"` + tc.account + `"`}, tc.extra...)

		script := windowsTaskScript(launcher, "Noctis-r4", float64(nowSec()+3600), arguments, true)

		execute, argument := "cmd.exe", windowsTaskArgument(launcher, arguments)
		if tc.hidden {
			execute = conhost
			argument = strings.Join(append([]string{"--headless cmd.exe /d /c", launcher, "resume --sid", sid, "--account", tc.account}, tc.extra...), " ")
		}
		want := "New-ScheduledTaskAction -Execute " + psQuote(execute) + " -Argument " + psQuote(argument) + " -WorkingDirectory " + psQuote(files.guardDir)
		if !strings.Contains(script, want) {
			t.Errorf("%s: the task action should read\n  %s\nbut the script is\n  %s", tc.name, want, script)
		}
	}
}
