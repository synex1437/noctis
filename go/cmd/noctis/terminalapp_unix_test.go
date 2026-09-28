//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// r9Tools links the named programs, as the test's own PATH finds them, into a new folder and returns
// it, so a PATH can be narrowed to them.
func r9Tools(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		found, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("%s is not on PATH: %v", name, err)
		}
		if err := os.Symlink(found, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// r9TerminalApp stands in for osascript and plays Terminal.app's part in a relaunch: `do script`
// types its command into the login shell of a new window. That shell has the user's login
// environment, with loginPath as its PATH and none of the runner's variables, and once the command
// has returned it waits at its prompt, which keeps the window open ("window-shell-open"). When the
// window's last process has ended it notes "window-closed".
func r9TerminalApp(t *testing.T, bin, loginPath string) {
	t.Helper()
	writeStub(t, bin, "osascript", strings.Join([]string{
		"#!/bin/sh",
		`prefix='tell application "Terminal" to do script "'`,
		`if [ "$#" -ne 4 ] || [ "$1" != -e ] || [ "$3" != -e ] || [ "$4" != 'tell application "Terminal" to activate' ]; then echo "osascript: unexpected arguments: $*" >&2; exit 1; fi`,
		`case "$2" in "$prefix"*'"') ;; *) echo "osascript: not a do script command: $2" >&2; exit 1 ;; esac`,
		`literal=${2#"$prefix"}`,
		`literal=${literal%'"'}`,
		`command=`,
		`while [ -n "$literal" ]; do`,
		`  rest=${literal#?}; char=${literal%"$rest"}`,
		`  if [ "$char" = '\' ]; then literal=$rest; rest=${literal#?}; char=${literal%"$rest"}; fi`,
		`  command=$command$char; literal=$rest`,
		`done`,
		`calls=$NOCTIS_TEST_CALLS`,
		`(`,
		`  env -i HOME="$HOME" PATH=` + shellQuote(loginPath) + ` NOCTIS_TEST_CALLS="$calls" sh -c "$command`,
		`echo window-shell-open >> \"\$NOCTIS_TEST_CALLS\""`,
		`  echo window-closed >> "$calls"`,
		`) </dev/null >/dev/null 2>&1 &`,
		`exit 0`,
		"",
	}, "\n"))
}

func r9WindowNotes(t *testing.T, calls string) []string {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		content, _ := os.ReadFile(calls)
		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		for _, line := range lines {
			if line == "window-closed" {
				return lines
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the relaunch window never ended: %q", lines)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestATerminalAppWindowRunsTheSessionInPlaceOfItsShellWithTheSessionsVariables(t *testing.T) {
	loginPath := r9Tools(t, "sh", "rm", "env")
	envTool, err := exec.LookPath("env")
	if err != nil {
		t.Fatal(err)
	}
	home, bin, calls := terminalSandbox(t)
	openerOnPlatform(t, true)
	if err := os.Symlink(envTool, filepath.Join(bin, "env")); err != nil {
		t.Fatal(err)
	}
	r9TerminalApp(t, bin, loginPath)
	writeStub(t, bin, "claude", strings.Join([]string{
		"#!/bin/sh",
		`case "$1" in --help|--version) exit 0 ;; esac`,
		`printf 'run %s\n' "$*" >> "$NOCTIS_TEST_CALLS"`,
		`env > "$NOCTIS_TEST_CALLS.env"`,
		"",
	}, "\n"))
	// What the runner restored from proxies/<session>.json: the session's PATH (bin, set by the
	// sandbox), display, certificate and proxy variables.
	carried := map[string]string{
		"HTTPS_PROXY":         "http://proxy.invalid:3128",
		"no_proxy":            "localhost,.internal",
		"SSL_CERT_FILE":       filepath.Join(home, "certs", "ca.pem"),
		"NODE_EXTRA_CA_CERTS": filepath.Join(home, "team certs", "it's ours.pem"),
	}
	for _, name := range append(append([]string{}, proxyEnvNames...), "SSL_CERT_DIR", "SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS") {
		t.Setenv(name, carried[name])
	}

	if !launchClaude(object{"resume": object{"mode": "window"}}, launchSpec{sid: "r9-app", cwd: home, prompt: "carry on", effort: "max"}).started {
		t.Fatal("the window relaunch reported failure")
	}

	notes := r9WindowNotes(t, calls)
	runs := 0
	for _, line := range notes {
		if strings.HasPrefix(line, "run ") {
			runs++
			if strings.HasPrefix(line, "run -p ") {
				t.Fatalf("the relaunch ran headless instead of in the window: %q", notes)
			}
		}
		if line == "window-shell-open" {
			t.Errorf("the window's login shell was still at its prompt after claude ended, so the window never closes and piles up with every relaunch: %q", notes)
		}
	}
	if runs != 1 {
		t.Fatalf("expected one interactive run in the window, got %q", notes)
	}
	env := recordedEnv(t, calls+".env")
	requireEnvValue(t, env, "PATH", bin)
	requireEnvValue(t, env, "DISPLAY", ":9")
	for name, value := range carried {
		requireEnvValue(t, env, name, value)
	}
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "NO_PROXY", "https_proxy", "SSL_CERT_DIR"} {
		if value, found := envEntry(env, name); found {
			t.Errorf("the window's claude got %s=%q, which the paused session did not have", name, value)
		}
	}
	requireEnvValue(t, env, handoffEnv, "r9-app")
	requireEnvValue(t, env, "CLAUDE_CODE_EFFORT_LEVEL", "max")
}

func TestTheRelaunchScriptThatCarriesTheProxyVariablesIsReadableOnlyByItsOwner(t *testing.T) {
	home := relaunchSandbox(t)
	t.Setenv("HTTPS_PROXY", "http://proxy.invalid:3128")
	script, _ := unixLaunchScript(launchSpec{sid: "r9-mode", cwd: home}, "/opt/claude/bin/claude", []string{"--resume", "r9-mode"}, "max")
	if script == "" {
		t.Fatal("the launcher script was not written")
	}
	info, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("the launcher script is mode %v; a proxy URL in it can hold a password, which proxies/ keeps readable only by its owner", info.Mode().Perm())
	}
	content, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "export HTTPS_PROXY='http://proxy.invalid:3128'") {
		t.Fatalf("the launcher script does not carry the session's proxy:\n%s", content)
	}
}
