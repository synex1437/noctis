package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A copy of the test binary run as wt or powershell.exe while NOCTIS_TEST_R5_REPORT is set notes each
// start there as "<name> <spec>" and exits. As wt it starts no tab, or, with NOCTIS_TEST_R5_WT=takes,
// takes the spec the way launch.ps1 does and writes its pid but no .started file: a tab that started
// just as the runner stopped waiting for it. As powershell.exe it also notes whether the tab's spec
// (NOCTIS_TEST_R5_TAB_SPEC) could still be read when the console launcher started, and writes its pid.
func init() {
	report := os.Getenv("NOCTIS_TEST_R5_REPORT")
	if report == "" {
		return
	}
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(os.Args[0])), ".exe")
	spec := ""
	for index, arg := range os.Args[:len(os.Args)-1] {
		if arg == "-Spec" {
			spec = os.Args[index+1]
		}
	}
	base := strings.TrimSuffix(spec, ".json")
	line := name + " " + spec
	switch {
	case name == "powershell":
		_, err := os.Stat(os.Getenv("NOCTIS_TEST_R5_TAB_SPEC"))
		line += " tab-spec-readable=" + strconv.FormatBool(err == nil)
		_ = os.WriteFile(base+".pid", []byte(strconv.Itoa(os.Getpid())), 0o600)
	case name == "wt" && os.Getenv("NOCTIS_TEST_R5_WT") == "takes":
		if os.Rename(spec, base+".taken.json") == nil {
			_ = os.WriteFile(base+".pid", []byte(strconv.Itoa(os.Getpid())), 0o600)
		}
	}
	if file, err := os.OpenFile(report, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		_, _ = file.WriteString(line + "\n")
		_ = file.Close()
	}
	os.Exit(0)
}

// r5WindowLaunch runs a Windows window relaunch of sid with wt and powershell.exe replaced by the
// stand-ins above (wt behaving as tab says) and returns whether it reported a started session, the
// tab's spec and what the stand-ins noted.
func r5WindowLaunch(t *testing.T, sid, tab string) (started bool, tabSpec string, noted []string) {
	t.Helper()
	sandboxFiles(t)
	bin := t.TempDir()
	for _, name := range []string{"wt", "powershell.exe"} {
		if isWindows && filepath.Ext(name) == "" {
			name += ".exe"
		}
		copyTestBinary(t, filepath.Join(bin, name))
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	report := filepath.Join(t.TempDir(), "noted.log")
	tabSpec, _, _ = launchFiles(sid)
	t.Setenv("NOCTIS_TEST_R5_REPORT", report)
	t.Setenv("NOCTIS_TEST_R5_TAB_SPEC", tabSpec)
	t.Setenv("NOCTIS_TEST_R5_WT", tab)
	files.launchScript = filepath.Join(t.TempDir(), "launch.ps1")
	previousStart, previousPid := launchStartTimeout, launchPidTimeout
	launchStartTimeout, launchPidTimeout = 300*time.Millisecond, 10*time.Second
	t.Cleanup(func() { launchStartTimeout, launchPidTimeout = previousStart, previousPid })

	started = launchInWindowsTerminal(object{}, launchSpec{sid: sid, cwd: t.TempDir()}, "claude", []string{"--resume", sid}, os.Environ(), "max")

	content, _ := os.ReadFile(report)
	return started, tabSpec, strings.Split(strings.TrimSpace(string(content)), "\n")
}

func TestATabThatStartsAfterTheConsoleFallbackFindsNothingToLaunch(t *testing.T) {
	started, tabSpec, noted := r5WindowLaunch(t, "r5-late", "late")

	console := ""
	for _, line := range noted {
		if rest, found := strings.CutPrefix(line, "powershell "); found {
			console = rest
		}
	}
	if console == "" {
		t.Fatalf("no console window was opened after the tab did not start: %q", noted)
	}
	spec, readable, _ := strings.Cut(console, " ")
	if spec == tabSpec {
		t.Errorf("the console launcher was given the tab's spec %s, so a tab that starts late launches the session a second time and both write the same pid file", spec)
	}
	if readable != "tab-spec-readable=false" {
		t.Errorf("the tab's spec could still be read when the console window opened (%s), so a tab that starts late launches claude --resume a second time", readable)
	}
	if !started {
		t.Errorf("the console launch was not followed through its own pid file: %q", noted)
	}
}

func TestATabThatTookTheSpecAsTheWaitRanOutIsNotDoubledByAConsole(t *testing.T) {
	started, _, noted := r5WindowLaunch(t, "r5-taken", "takes")

	for _, line := range noted {
		if strings.HasPrefix(line, "powershell ") {
			t.Fatalf("the tab had already taken the spec, yet a console window was opened as well, which starts the session a second time: %q", noted)
		}
	}
	if !started {
		t.Fatalf("the tab that took the spec was not followed through its pid file: %q", noted)
	}
}
