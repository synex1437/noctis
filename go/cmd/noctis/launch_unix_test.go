//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func fakeClaudeRecorder(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	record := filepath.Join(bin, "calls.log")
	t.Setenv("NOCTIS_TEST_CALLS", record)
	script := strings.Join([]string{
		"#!/bin/sh",
		`if [ "$1" = "--help" ]; then exit 0; fi`,
		`printf 'CONFIG_SET=%s CONFIG=%s\n' "${CLAUDE_CONFIG_DIR+yes}" "$CLAUDE_CONFIG_DIR" >> "$NOCTIS_TEST_CALLS"`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return record
}

func headlessRelaunchSees(t *testing.T, launch launchSpec) string {
	t.Helper()
	record := fakeClaudeRecorder(t)
	if !launchClaude(object{"resume": object{"mode": "headless"}}, launch).started {
		t.Fatal("the headless relaunch did not run")
	}
	content, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("the fake claude never ran: %v", err)
	}
	return strings.TrimSpace(string(content))
}

func TestDefaultInstallHeadlessRelaunchRunsWithoutTheConfigDir(t *testing.T) {
	home := relaunchSandbox(t)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "stray"))
	if got := headlessRelaunchSees(t, launchSpec{sid: "s1", cwd: home, mode: "headless"}); got != "CONFIG_SET= CONFIG=" {
		t.Fatalf("a session that never set CLAUDE_CONFIG_DIR must be relaunched without it, even when the runner's own environment has one; claude saw %q", got)
	}
}

func TestExplicitAccountHeadlessRelaunchKeepsTheRecordedConfigDir(t *testing.T) {
	home := relaunchSandbox(t)
	account := filepath.Join(home, "work account") + "/"
	if got := headlessRelaunchSees(t, launchSpec{sid: "s1", cwd: home, mode: "headless", configDir: account}); got != "CONFIG_SET=yes CONFIG="+account {
		t.Fatalf("an explicit account must be relaunched with exactly the value the session had (%q); claude saw %q", account, got)
	}
}

func fakeEnvRecorder(t *testing.T, name string) (record, exe string) {
	t.Helper()
	bin := t.TempDir()
	record = filepath.Join(bin, "env.log")
	t.Setenv("NOCTIS_TEST_ENV_RECORD", record)
	script := strings.Join([]string{
		"#!/bin/sh",
		`if [ "$1" = "--help" ]; then exit 0; fi`,
		`{ printf 'NOCTIS_TEST_ARGS=%s\n' "$*"; env; } > "$NOCTIS_TEST_ENV_RECORD"`,
		"",
	}, "\n")
	exe = filepath.Join(bin, name)
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return record, exe
}

func recordedEnv(t *testing.T, record string) []string {
	t.Helper()
	content, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("the relaunched program never ran: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(content)), "\n")
}

func TestHeadlessRelaunchStartsClaudeWithoutTheHookSessionMarkers(t *testing.T) {
	home := relaunchSandbox(t)
	record, _ := fakeEnvRecorder(t, "claude")
	inheritHookSessionMarkers(t)
	if !launchClaude(object{"resume": object{"mode": "headless"}}, launchSpec{sid: "s1", cwd: home, mode: "headless"}).started {
		t.Fatal("the headless relaunch did not run")
	}
	env := recordedEnv(t, record)
	if args, _ := envEntry(env, "NOCTIS_TEST_ARGS"); !strings.HasPrefix(args, "-p ") {
		t.Fatalf("expected the headless path (claude -p), claude got %q", args)
	}
	if leaked := leakedMarkers(env); len(leaked) > 0 {
		t.Fatalf("the headless claude was started with %q from the hook's environment", leaked)
	}
	requireEnvValue(t, env, handoffEnv, "s1")
}

func TestTerminalRelaunchStartsClaudeWithoutTheHookSessionMarkers(t *testing.T) {
	home := relaunchSandbox(t)
	record, _ := fakeEnvRecorder(t, "claude")
	inheritHookSessionMarkers(t)
	t.Setenv("NOCTIS_NO_TERMINAL", "")
	if !launchClaude(object{"resume": object{"mode": "window", "terminal": "sh {script}"}}, launchSpec{sid: "s1", cwd: home}).started {
		t.Fatal("the terminal relaunch did not run")
	}
	env := recordedEnv(t, record)
	if args, _ := envEntry(env, "NOCTIS_TEST_ARGS"); !strings.HasPrefix(args, "--resume s1 ") {
		t.Fatalf("expected the interactive terminal path (claude --resume), claude got %q", args)
	}
	if leaked := leakedMarkers(env); len(leaked) > 0 {
		t.Fatalf("a terminal that passes its caller's environment on started the interactive claude with %q; with CLAUDE_CODE_CHILD_SESSION set it saves no transcript", leaked)
	}
	requireEnvValue(t, env, handoffEnv, "s1")
}

func TestHostRelaunchStartsTheAgentWithoutTheHookSessionMarkers(t *testing.T) {
	home := relaunchSandbox(t)
	record, codex := fakeEnvRecorder(t, "codex")
	inheritHookSessionMarkers(t)
	if !launchHostSession(object{}, hostOf("codex"), codex, launchSpec{sid: "thr_1", cwd: home}).started {
		t.Fatal("the host relaunch did not run")
	}
	env := recordedEnv(t, record)
	if leaked := leakedMarkers(env); len(leaked) > 0 {
		t.Fatalf("the relaunched codex session was started with Claude's session markers %q", leaked)
	}
	requireEnvValue(t, env, handoffEnv, "thr_1")
	requireEnvValue(t, env, "NOCTIS_HOST", "codex")
}

func TestAWindowThatPausesAgainIsRelaunchedWhileItsFirstRunnerStillWatchesIt(t *testing.T) {
	sid := "repause"
	t.Setenv("HOME", t.TempDir())
	t.Setenv(claudeConfigEnv, "")
	if err := os.Unsetenv(claudeConfigEnv); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"NOCTIS_PLUGIN_ROOT", "CLAUDE_PLUGIN_ROOT", "NOCTIS_NO_TERMINAL", "NOCTIS_TIME_OFFSET", handoffEnv} {
		t.Setenv(name, "")
	}
	for _, name := range []string{"NOCTIS_NO_TASKS", "NOCTIS_NO_SCHEDULE", "NOCTIS_NO_WATCHER"} {
		t.Setenv(name, "1")
	}
	previousFiles, previousArgs := files, args
	t.Cleanup(func() { files, args = previousFiles, previousArgs })
	account := filepath.Join(t.TempDir(), "account")
	args = parseArgs(runnerArgs("resume", sid, account))
	initPaths()

	bin := t.TempDir()
	calls, gate, open := filepath.Join(bin, "calls.log"), filepath.Join(bin, "gate"), filepath.Join(bin, "open")
	t.Setenv("NOCTIS_TEST_CALLS", calls)
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	writeScript(t, filepath.Join(bin, "claude"), "#!/bin/sh\nprintf '%s %s\\n' \"$$\" \"$*\" >> \"$NOCTIS_TEST_CALLS\"\n"+promptLine+"\nexec "+shellQuote(standInNamed(t, sleep, "claude"))+" 3600\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	terminal := "(while [ -f " + shellQuote(gate) + " ] && [ ! -f " + shellQuote(open) + " ]; do sleep 0.1; done; sh {script}) >/dev/null 2>&1 &"
	mustWriteJSON(files.config, object{"resume": object{"mode": "window", "terminal": terminal, "prompt": "carry on"}, "alarm": object{"enabled": false}})
	windows := []int{}
	t.Cleanup(func() {
		for _, pid := range windows {
			if processAlive(pid) && looksLikeSessionProcess(pid) {
				_ = terminateProcess(pid)
			}
		}
	})

	cwd := t.TempDir()
	transcript := quietTranscript(t, cwd, float64(nowSec()-3600))
	t.Setenv("NOCTIS_TEST_TRANSCRIPT", transcript)
	pause := func(startedAt float64) object {
		return object{"kind": "fable", "window": "fable", "label": "Fable", "until": startedAt, "resumeAt": startedAt + 20, "startedAt": startedAt,
			"cwd": cwd, "transcript": transcript, "launchMode": "window", "modelOverride": "claude-sonnet-5", "queuedPrompt": ""}
	}
	updateState(func(state object) { stateMap(state, "waits")[sid] = pause(float64(nowSec() - 100)) })
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	first := exec.Command(executable)
	first.Env = append(os.Environ(), "NOCTIS_TEST_RUNNER_SID="+sid, "NOCTIS_TEST_RUNNER_ACCOUNT="+account)
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	firstGone := make(chan struct{})
	go func() {
		_ = first.Wait()
		close(firstGone)
	}()
	t.Cleanup(func() {
		_ = first.Process.Kill()
		<-firstGone
	})
	logTail := func() string { return strings.Join(tailFileLines(files.log, 15), "\n") }

	firstWindow, launchedAt := 0, 0.0
	for deadline := time.Now().Add(30 * time.Second); firstWindow == 0 && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		state := readState()
		record := getMap(getMap(state, "launched"), sid)
		if pid := int(numberOr(record, "pid", 0)); pid > 0 && int(numberOr(getMap(getMap(state, "handedOff"), sid), "pid", 0)) == first.Process.Pid {
			firstWindow, launchedAt = pid, numberOr(record, "at", 0)
			windows = append(windows, pid)
		}
	}
	if firstWindow == 0 {
		t.Fatalf("the first runner never opened its window:\n%s", logTail())
	}

	writeScript(t, gate, "")
	for float64(nowSec()) <= launchedAt {
		time.Sleep(50 * time.Millisecond)
	}
	second := pause(float64(nowSec()))
	updateState(func(state object) { stateMap(state, "waits")[sid] = second })
	resumed := make(chan struct{})
	go func() {
		defer close(resumed)
		resumeWait(sid, "")
	}()
	t.Cleanup(func() {
		_ = os.WriteFile(open, nil, 0o600)
		for deadline := time.Now().Add(90 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			select {
			case <-resumed:
				return
			default:
			}
			if pid := int(numberOr(getMap(getMap(readState(), "launched"), sid), "pid", 0)); pid > 0 && processAlive(pid) && looksLikeSessionProcess(pid) {
				_ = terminateProcess(pid)
			}
		}
	})

	select {
	case <-firstGone:
	case <-resumed:
		t.Fatalf("the session paused again in the window runner %d opened; the runner for the new pause gave up while that runner still watched its window (journal %v):\n%s", first.Process.Pid, journaledFor(sid), logTail())
	case <-time.After(30 * time.Second):
		t.Fatalf("runner %d still watches the window that paused again:\n%s", first.Process.Pid, logTail())
	}
	if processAlive(firstWindow) || !slices.Contains(journaledFor(sid), "close-previous") {
		t.Fatalf("the idle window %d was not closed before the relaunch (journal %v)", firstWindow, journaledFor(sid))
	}
	state := readState()
	if holder := getMap(getMap(state, "handedOff"), sid); int(numberOr(holder, "pid", 0)) != os.Getpid() {
		t.Fatalf("runner %d went away and took the hand-off of the runner that relaunches the session with it: %v", first.Process.Pid, holder)
	}
	if wait := getMap(getMap(state, "waits"), sid); numberOr(wait, "startedAt", -1) != numberOr(second, "startedAt", -2) {
		t.Fatalf("runner %d went away and took the pause being resumed with it: %v", first.Process.Pid, wait)
	}
	spec, _, _ := launchFiles(sid)
	if script := strings.TrimSuffix(spec, ".json") + ".sh"; statSafe(script) == nil {
		t.Fatalf("runner %d went away and deleted %s, the launcher the new window has yet to run; the window never opens and the runner falls back to a headless run", first.Process.Pid, script)
	}

	writeScript(t, open, "")
	secondWindow := 0
	for deadline := time.Now().Add(30 * time.Second); secondWindow == 0 && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if pid := int(numberOr(getMap(getMap(readState(), "launched"), sid), "pid", 0)); pid > 0 && pid != firstWindow {
			secondWindow = pid
			windows = append(windows, pid)
		}
	}
	if secondWindow == 0 {
		t.Fatalf("the new window was never recorded:\n%s", logTail())
	}
	content, _ := os.ReadFile(calls)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 2 || launchesOf(calls, sid) != 2 || strings.Contains(string(content), " -p ") {
		t.Fatalf("expected the first window and one relaunch in a window, claude ran %d time(s): %q", len(lines), lines)
	}
	if err := terminateProcess(secondWindow); err != nil {
		t.Fatal(err)
	}
	select {
	case <-resumed:
	case <-time.After(30 * time.Second):
		t.Fatalf("the new runner never noticed its window closed:\n%s", logTail())
	}
	state = readState()
	for _, bucket := range []string{"waits", "handedOff", "launched"} {
		if record := getMap(getMap(state, bucket), sid); record != nil {
			t.Fatalf("%s still holds the session after both windows closed: %v", bucket, record)
		}
	}
	entries, _ := os.ReadDir(files.launches)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), safeName(sid)+".") {
			t.Fatalf("launcher file %s was left behind", entry.Name())
		}
	}
}

func terminalSandbox(t *testing.T) (home, bin, calls string) {
	t.Helper()
	home = relaunchSandbox(t)
	bin = t.TempDir()
	for _, tool := range []string{"sh", "rm", "sleep", "grep"} {
		found, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("%s is not on PATH: %v", tool, err)
		}
		if err := os.Symlink(found, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	calls = filepath.Join(bin, "calls.log")
	t.Setenv("NOCTIS_TEST_CALLS", calls)
	t.Setenv("NOCTIS_TEST_STATE", files.state)
	t.Setenv("PATH", bin)
	t.Setenv("DISPLAY", ":9")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("NOCTIS_NO_TERMINAL", "")
	previousPid, previousPoll := launchPidTimeout, launchPollInterval
	launchPidTimeout, launchPollInterval = 4*time.Second, 50*time.Millisecond
	t.Cleanup(func() { launchPidTimeout, launchPollInterval = previousPid, previousPoll })
	writeStub(t, bin, "claude", strings.Join([]string{
		"#!/bin/sh",
		`printf 'run %s\n' "$*" >> "$NOCTIS_TEST_CALLS"`,
		"sleep 2",
		`if grep -Eq '"how": *"(terminal|window)"' "$NOCTIS_TEST_STATE"; then echo recorded >> "$NOCTIS_TEST_CALLS"; else echo unrecorded >> "$NOCTIS_TEST_CALLS"; fi`,
		"",
	}, "\n"))
	return home, bin, calls
}

func writeStub(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func terminalRuns(t *testing.T, calls string) (runs []string, recorded bool) {
	t.Helper()
	content, err := os.ReadFile(calls)
	if err != nil {
		t.Fatalf("the fake claude never ran: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		switch {
		case strings.HasPrefix(line, "run "):
			runs = append(runs, strings.TrimPrefix(line, "run "))
		case line == "recorded":
			recorded = true
		}
	}
	return runs, recorded
}

func TestATerminalThatStaysInTheForegroundKeepsTheSessionAndRecordsItWhileItRuns(t *testing.T) {
	home, _, calls := terminalSandbox(t)
	if !launchClaude(object{"resume": object{"mode": "window", "terminal": "sh {script}"}}, launchSpec{sid: "fg1", cwd: home, prompt: "carry on"}).started {
		t.Fatal("the window relaunch reported failure")
	}
	runs, recorded := terminalRuns(t, calls)
	if len(runs) != 1 || strings.HasPrefix(runs[0], "-p ") {
		t.Fatalf("expected exactly one interactive run in the window, claude ran %q", runs)
	}
	if !recorded {
		t.Fatal("the window's claude ran without a launch record: the runner was still blocked on the terminal opener, so a later relaunch could not close this window")
	}
}

var terminalOpeners = []struct {
	name, opener string
	darwin       bool
}{
	{"an X terminal", "x-terminal-emulator", false},
	{"Terminal.app", "osascript", true},
}

func openerOnPlatform(t *testing.T, darwin bool) {
	t.Helper()
	previous := isDarwin
	t.Cleanup(func() { isDarwin = previous })
	isDarwin = darwin
}

func forkingOpener(darwin bool) string {
	if !darwin {
		return "#!/bin/sh\nshift\n\"$@\" </dev/null >/dev/null 2>&1 &\nexit 0\n"
	}
	return strings.Join([]string{
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
		`sh -c "$command" </dev/null >/dev/null 2>&1 &`,
		`exit 0`,
		"",
	}, "\n")
}

func TestATerminalThatFailsAtOnceFallsBackToHeadlessWithoutWaiting(t *testing.T) {
	for index, terminal := range terminalOpeners {
		t.Run(terminal.name, func(t *testing.T) {
			home, bin, calls := terminalSandbox(t)
			openerOnPlatform(t, terminal.darwin)
			writeStub(t, bin, terminal.opener, "#!/bin/sh\nexit 1\n")
			started := time.Now()
			if !launchClaude(object{"resume": object{"mode": "window"}}, launchSpec{sid: fmt.Sprintf("fail%d", index+1), cwd: home, prompt: "carry on"}).started {
				t.Fatal("the headless fallback did not run")
			}
			if elapsed := time.Since(started); elapsed > launchPidTimeout {
				t.Fatalf("a terminal that exited 1 at once kept the runner waiting %s", elapsed)
			}
			if runs, _ := terminalRuns(t, calls); len(runs) != 1 || !strings.HasPrefix(runs[0], "-p ") {
				t.Fatalf("expected one headless run after %s failed, claude ran %q", terminal.opener, runs)
			}
		})
	}
}

func TestATerminalThatForksAndReturnsIsRecorded(t *testing.T) {
	for index, terminal := range terminalOpeners {
		t.Run(terminal.name, func(t *testing.T) {
			home, bin, calls := terminalSandbox(t)
			openerOnPlatform(t, terminal.darwin)
			writeStub(t, bin, terminal.opener, forkingOpener(terminal.darwin))
			if !launchClaude(object{"resume": object{"mode": "window"}}, launchSpec{sid: fmt.Sprintf("fork%d", index+1), cwd: home, prompt: "carry on"}).started {
				t.Fatal("the window relaunch reported failure")
			}
			runs, recorded := terminalRuns(t, calls)
			if len(runs) != 1 || strings.HasPrefix(runs[0], "-p ") || !recorded {
				t.Fatalf("expected one recorded interactive run through %s, claude ran %q (recorded=%t)", terminal.opener, runs, recorded)
			}
		})
	}
}

func TestXfceTerminalIsHandedTheLauncherAsACommandLineItAccepts(t *testing.T) {
	home, bin, calls := terminalSandbox(t)
	openerOnPlatform(t, false)
	writeStub(t, bin, "xfce4-terminal", strings.Join([]string{
		"#!/bin/sh",
		`case "$1" in`,
		`-x) shift; exec "$@" ;;`,
		`-e) command="$2"; shift 2; if [ "$#" -gt 0 ]; then echo "xfce4-terminal: unknown option $1" >&2; exit 1; fi; exec sh -c "$command" ;;`,
		"esac",
		"exit 1",
		"",
	}, "\n"))
	if !launchClaude(object{"resume": object{"mode": "window"}}, launchSpec{sid: "xfce1", cwd: home, prompt: "carry on"}).started {
		t.Fatal("the window relaunch reported failure")
	}
	if runs, recorded := terminalRuns(t, calls); len(runs) != 1 || strings.HasPrefix(runs[0], "-p ") || !recorded {
		t.Fatalf("xfce4-terminal did not run the launcher: claude ran %q (recorded=%t)", runs, recorded)
	}
}

func TestAPreviousWindowPidThatIsNowAShellIsNotClosed(t *testing.T) {
	relaunchSandbox(t)
	runner, shell := idleRunner(t), startHelper(t, "sh", "-c", "while :; do sleep 1; done")
	updateState(func(state object) {
		stateMap(state, "launched")["sh1"] = object{"pid": float64(shell.pid), "at": float64(nowSec() - 3600), "how": "terminal", "runner": float64(runner.pid)}
	})
	closePreviousLaunch(object{}, "sh1", nil)
	if shell.endsWithin(500 * time.Millisecond) {
		t.Fatal("the recorded window pid is now a shell, which a relaunch window never is (the launcher execs claude), and it was killed")
	}
}

func TestALauncherInAFolderWithASpaceOrAQuoteOpensItsWindow(t *testing.T) {
	for index, terminal := range terminalOpeners {
		t.Run(terminal.name, func(t *testing.T) {
			home, bin, calls := terminalSandbox(t)
			openerOnPlatform(t, terminal.darwin)
			files.launches = filepath.Join(t.TempDir(), "Application Support", "owner's launches")
			writeStub(t, bin, terminal.opener, forkingOpener(terminal.darwin))
			if !launchClaude(object{"resume": object{"mode": "window"}}, launchSpec{sid: fmt.Sprintf("space%d", index+1), cwd: home, prompt: "carry on"}).started {
				t.Fatal("the window relaunch reported failure")
			}
			if runs, recorded := terminalRuns(t, calls); len(runs) != 1 || strings.HasPrefix(runs[0], "-p ") || !recorded {
				t.Fatalf("the launcher in %q never ran in the window %s opened: claude ran %q (recorded=%t)", files.launches, terminal.opener, runs, recorded)
			}
		})
	}
}
