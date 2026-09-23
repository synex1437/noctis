package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

var hookSessionMarkers = map[string]string{
	"CLAUDECODE":                   "1",
	"CLAUDE_CODE_SESSION_ID":       "sess-paused",
	"CLAUDE_CODE_CHILD_SESSION":    "1",
	"CLAUDE_CODE_SESSION_ATTENDED": "1",
	"CLAUDE_PID":                   "4242",
	"AI_AGENT":                     "claude-code_2-1-280_agent",
	"CLAUDE_EFFORT":                "high",
	"TRACEPARENT":                  "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	"CLAUDE_CODE_ENTRYPOINT":       "claude-vscode",
}

func inheritHookSessionMarkers(t *testing.T) {
	t.Helper()
	for name, value := range hookSessionMarkers {
		t.Setenv(name, value)
	}
}

func envEntry(env []string, name string) (string, bool) {
	value, found := "", false
	for _, entry := range env {
		key, rest, _ := strings.Cut(entry, "=")
		if key == name || (isWindows && strings.EqualFold(key, name)) {
			value, found = rest, true
		}
	}
	return value, found
}

func leakedMarkers(env []string) []string {
	leaked := []string{}
	for name := range hookSessionMarkers {
		if _, found := envEntry(env, name); found {
			leaked = append(leaked, name)
		}
	}
	sort.Strings(leaked)
	return leaked
}

func missingMarkers(covered map[string]bool) []string {
	missing := []string{}
	for name := range hookSessionMarkers {
		if !covered[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

func requireRunnerEnv(t *testing.T, env []string, names ...string) {
	t.Helper()
	for _, name := range names {
		want := os.Getenv(name)
		if got, found := envEntry(env, name); !found || got != want {
			t.Errorf("the relaunch environment must keep the runner's %s=%q, got %q (present: %v)", name, want, got, found)
		}
	}
}

func requireEnvValue(t *testing.T, env []string, name, want string) {
	t.Helper()
	if got, found := envEntry(env, name); !found || got != want {
		t.Errorf("the relaunch environment must set %s=%q, got %q (present: %v)", name, want, got, found)
	}
}

func TestRelaunchEnvDropsTheMarkersClaudeGaveTheHook(t *testing.T) {
	home := relaunchSandbox(t)
	inheritHookSessionMarkers(t)
	env := relaunchEnv(launchSpec{sid: "s1", cwd: home}, "max")
	if leaked := leakedMarkers(env); len(leaked) > 0 {
		t.Fatalf("the relaunched claude must start as a top-level session, but it inherits %q from the hook that paused it (CLAUDE_CODE_CHILD_SESSION alone turns an interactive session's transcript saving off)", leaked)
	}
	requireRunnerEnv(t, env, "PATH", "HOME")
	requireEnvValue(t, env, "CLAUDE_CODE_EFFORT_LEVEL", "max")
	requireEnvValue(t, env, handoffEnv, "s1")
}

func TestHostRelaunchEnvDropsTheMarkersClaudeGaveTheHook(t *testing.T) {
	home := relaunchSandbox(t)
	inheritHookSessionMarkers(t)
	env := hostRelaunchEnv(hostOf("codex"), launchSpec{sid: "thr_1", cwd: home})
	if leaked := leakedMarkers(env); len(leaked) > 0 {
		t.Fatalf("a relaunched host session must not inherit Claude's session markers, got %q", leaked)
	}
	requireRunnerEnv(t, env, "PATH", "HOME")
	requireEnvValue(t, env, handoffEnv, "thr_1")
	requireEnvValue(t, env, "NOCTIS_HOST", "codex")
}

func TestWindowLauncherUnsetsTheMarkersBeforeItStartsClaude(t *testing.T) {
	home := relaunchSandbox(t)
	inheritHookSessionMarkers(t)
	script, _ := unixLaunchScript(launchSpec{sid: "s1", cwd: home}, "/opt/claude/bin/claude", []string{"--resume", "s1"}, "max")
	if script == "" {
		t.Fatal("the launcher script was not written")
	}
	content, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	unset, started := map[string]bool{}, false
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "exec ") {
			started = true
			break
		}
		if names, found := strings.CutPrefix(line, "unset "); found {
			for _, name := range strings.Fields(names) {
				unset[name] = true
			}
		}
	}
	if !started {
		t.Fatalf("the launcher never starts claude:\n%s", content)
	}
	if missing := missingMarkers(unset); len(missing) > 0 {
		t.Fatalf("terminal openers hand the runner's environment, which it inherited from the hook, to the new window, so the launcher must unset %q before exec:\n%s", missing, content)
	}
}

func TestWindowsLauncherRemovesTheMarkersBeforeItStartsClaude(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "launch.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	removed, started := map[string]bool{}, false
	for _, line := range strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "Start-Process") {
			started = true
			break
		}
		if rest, found := strings.CutPrefix(trimmed, "Remove-Item Env:"); found {
			if name, tail, _ := strings.Cut(rest, " "); tail == "-ErrorAction SilentlyContinue" {
				removed[name] = true
			}
		}
	}
	if !started {
		t.Fatalf("launch.ps1 never starts claude:\n%s", content)
	}
	if missing := missingMarkers(removed); len(missing) > 0 {
		t.Fatalf("a Windows Terminal tab can run with the terminal's own environment, so launch.ps1 must remove %q before Start-Process", missing)
	}
}

func TestRelaunchEnvDropsTheMarkersInAnyCaseOnWindows(t *testing.T) {
	previous := isWindows
	t.Cleanup(func() { isWindows = previous })
	isWindows = true
	got := withoutEnv([]string{"ClaudeCode=1", `PATH=C:\bin`, "claude_code_child_session=1", "Claude_Code_Entrypoint=claude-vscode", "CLAUDE_CODE_CHILD_SESSIONS=keep"}, claudeSessionMarkers...)
	if want := []string{`PATH=C:\bin`, "CLAUDE_CODE_CHILD_SESSIONS=keep"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("Windows environment names ignore case, so every marker must go whatever its spelling: withoutEnv = %q, want %q", got, want)
	}
	isWindows = false
	if got := withoutEnv([]string{"ClaudeCode=kept", "CLAUDECODE=1", "TRACEPARENT=00-1-2-01"}, claudeSessionMarkers...); strings.Join(got, "|") != "ClaudeCode=kept" {
		t.Fatalf("on unix only the exact names are the variables Claude reads, got %q", got)
	}
}

type helperProcess struct {
	pid     int
	command *exec.Cmd
	exited  chan struct{}
}

func startHelper(t *testing.T, name string, arguments ...string) helperProcess {
	t.Helper()
	return startCommand(t, exec.Command(name, arguments...))
}

func startCommand(t *testing.T, command *exec.Cmd) helperProcess {
	t.Helper()
	if err := command.Start(); err != nil {
		t.Fatalf("helper %s: %v", command.Path, err)
	}
	helper := helperProcess{pid: command.Process.Pid, command: command, exited: make(chan struct{})}
	go func() {
		_ = command.Wait()
		close(helper.exited)
	}()
	t.Cleanup(helper.stop)
	return helper
}

func (helper helperProcess) stop() {
	select {
	case <-helper.exited:
		return
	default:
	}
	if isWindows {
		_ = terminateProcess(helper.pid)
	}
	_ = helper.command.Process.Kill()
	<-helper.exited
}

func (helper helperProcess) endsWithin(timeout time.Duration) bool {
	select {
	case <-helper.exited:
		return true
	case <-time.After(timeout):
		return false
	}
}

func idleRunner(t *testing.T) helperProcess {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestThreadedStandIn$")
	command.Env = append(os.Environ(), "NOCTIS_TEST_THREADED_STAND_IN=1")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close() })
	return startCommand(t, command)
}

func idleWindow(t *testing.T) helperProcess {
	t.Helper()
	if isWindows {
		return startHelper(t, "cmd.exe", "/d", "/c", "ping", "-n", "300", "127.0.0.1")
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	return startHelper(t, standInNamed(t, sleep, "claude"), "300")
}

func goneRunner(t *testing.T) int {
	t.Helper()
	command := exec.Command("sh", "-c", "exit 0")
	if isWindows {
		command = exec.Command("cmd.exe", "/d", "/c", "exit 0")
	}
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
	return command.Process.Pid
}

func takeoverSandbox(t *testing.T) string {
	t.Helper()
	relaunchSandbox(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	t.Setenv("NOCTIS_NO_TERMINAL", "1")
	t.Setenv("NOCTIS_NO_EARLY_TRIGGER", "")
	t.Setenv(handoffEnv, "")
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls.log")
	t.Setenv("NOCTIS_TEST_CALLS", calls)
	if isWindows {
		writeScript(t, filepath.Join(bin, "claude.cmd"), "@(echo %1 %2 %3)>>\"%NOCTIS_TEST_CALLS%\"\r\n@exit /b 0\r\n")
	} else {
		writeScript(t, filepath.Join(bin, "claude"), "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$NOCTIS_TEST_CALLS\"\n")
		for _, notifier := range []string{"notify-send", "osascript"} {
			writeScript(t, filepath.Join(bin, notifier), "#!/bin/sh\nexit 0\n")
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	config := releaseConfig()
	config["resume"] = object{"mode": "window", "prompt": "carry on"}
	mustWriteJSON(files.config, config)
	return calls
}

func launchesOf(calls, sid string) int {
	content, _ := os.ReadFile(calls)
	count := 0
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		for index := 0; index+1 < len(fields); index++ {
			if fields[index] == "--resume" && fields[index+1] == sid {
				count++
				break
			}
		}
	}
	return count
}

func journaledFor(sid string) []string {
	actions := []string{}
	for _, line := range tailFileLines(files.decisions, 1000) {
		var entry object
		if jsonUnmarshalObject([]byte(line), &entry) == nil && getString(entry, "sid") == sid {
			actions = append(actions, getString(entry, "action"))
		}
	}
	return actions
}

func loggedTimes(text string) int {
	count := 0
	for _, line := range tailFileLines(files.log, 1000) {
		if strings.Contains(line, text) {
			count++
		}
	}
	return count
}

func quietTranscript(t *testing.T, dir string, since float64) string {
	t.Helper()
	transcript := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	moment := time.Unix(int64(since), 0)
	if err := os.Chtimes(transcript, moment, moment); err != nil {
		t.Fatal(err)
	}
	return transcript
}

func TestResumeTakesOverFromARunnerWatchingAnOlderWindow(t *testing.T) {
	calls := takeoverSandbox(t)
	now := float64(nowSec())
	t0 := now - 600
	resuming := "notify: " + pluginName + " — " + T("wait.ready", "5h", T("wait.readyTail"))
	cases := []struct {
		name       string
		handoffAt  float64
		launchedAt float64
		window     bool
		takesOver  bool
	}{
		{"the runner only watches the window it opened before the session paused again", 0, 0, true, true},
		{"the runner opened its window a little after it took the session", 0, 5, true, true},
		{"the hand-off was taken for this very pause", 60, 60, true, false},
		{"the hand-off is newer than the pause", 90, 95, true, false},
		{"the runner holding the session has not opened its window yet", 30, 0, true, false},
		{"the window was opened after the pause began", 0, 120, true, false},
		{"a headless relaunch holds the session", 0, 0, false, false},
	}
	for index, tc := range cases {
		sid := fmt.Sprintf("tk%d", index+1)
		runner, window := idleRunner(t), idleWindow(t)
		cwd := t.TempDir()
		transcript := quietTranscript(t, cwd, t0)
		handoff := object{"at": t0 + tc.handoffAt, "model": "claude-opus-5", "mode": "window", "pid": float64(runner.pid)}
		launched := object{"pid": float64(window.pid), "at": t0 + tc.launchedAt, "how": "terminal", "runner": float64(runner.pid)}
		updateState(func(state object) {
			stateMap(state, "handedOff")[sid] = cloneObject(handoff)
			if tc.window {
				stateMap(state, "launched")[sid] = cloneObject(launched)
			}
			stateMap(state, "waits")[sid] = object{"kind": "batch", "window": "five_hour", "label": "5h", "used": float64(95), "threshold": float64(92), "hit": "threshold",
				"startedAt": t0 + 60, "until": now - 10, "resumeAt": now - 5, "cwd": cwd, "transcript": transcript, "launchMode": "headless"}
		})
		statusReadingFrom(sid, nowSec(), 3, now+18000, 20, now+3*86400)
		noticesBefore := loggedTimes(resuming)

		resumeWait(sid, "")

		notices := loggedTimes(resuming) - noticesBefore
		state := readState()
		if tc.takesOver {
			if got := launchesOf(calls, sid); got != 1 {
				t.Fatalf("%s: the session paused again in the window runner %d opened; the runner for the new pause launched it %d time(s), so the pause is stranded behind a hand-off that belongs to the pause before it (journal %v)", tc.name, runner.pid, got, journaledFor(sid))
			}
			if !window.endsWithin(10 * time.Second) {
				t.Fatalf("%s: the idle window (pid %d) was left open next to the relaunched session", tc.name, window.pid)
			}
			if journal := journaledFor(sid); !slices.Contains(journal, "take-over") || !slices.Contains(journal, "close-previous") {
				t.Fatalf("%s: taking the session over and closing the previous window must both be journaled: %v", tc.name, journal)
			}
			if notices != 1 {
				t.Fatalf("%s: %d 'resuming' notices for one relaunch", tc.name, notices)
			}
			for _, bucket := range []string{"waits", "handedOff", "launched"} {
				if record := getMap(getMap(state, bucket), sid); record != nil {
					t.Fatalf("%s: %s still holds the session after its relaunch ended: %v", tc.name, bucket, record)
				}
			}
			if runner.endsWithin(500 * time.Millisecond) {
				t.Fatalf("%s: taking the session over must leave the runner that watched the old window alone", tc.name)
			}
			continue
		}
		if got := launchesOf(calls, sid); got != 0 {
			t.Fatalf("%s: runner %d is still resuming this session, yet it was launched again %d time(s)", tc.name, runner.pid, got)
		}
		if window.endsWithin(500 * time.Millisecond) {
			t.Fatalf("%s: the window of the runner that holds the session was closed", tc.name)
		}
		if notices != 0 {
			t.Fatalf("%s: a runner that left the session to runner %d still announced %d time(s) that work resumes", tc.name, runner.pid, notices)
		}
		if holder := getMap(getMap(state, "handedOff"), sid); numberOr(holder, "pid", 0) != float64(runner.pid) || numberOr(holder, "at", 0) != numberOr(handoff, "at", -1) {
			t.Fatalf("%s: the hand-off of the runner that holds the session was changed: %v", tc.name, holder)
		}
		if wait := getMap(getMap(state, "waits"), sid); numberOr(wait, "startedAt", 0) != t0+60 {
			t.Fatalf("%s: the pause runner %d is resuming was touched: %v", tc.name, runner.pid, wait)
		}
		if record := getMap(getMap(state, "launched"), sid); tc.window && numberOr(record, "pid", 0) != float64(window.pid) {
			t.Fatalf("%s: the launch record of the open window was dropped: %v", tc.name, record)
		}
		if !slices.Contains(journaledFor(sid), "skip-launch") {
			t.Fatalf("%s: leaving the session to the runner that holds it was not journaled: %v", tc.name, journaledFor(sid))
		}
	}
}

func TestTheStatusLineResumesAPauseWhoseHandOffOnlyWatchesAnEarlierWindow(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("NOCTIS_NO_EARLY_TRIGGER", "")
	cfg := releaseConfig()
	now := float64(nowSec())
	t0 := now - 600
	cases := []struct {
		name      string
		handoffAt float64
		window    bool
		triggered bool
	}{
		{"the hand-off only covers the window opened before this pause", 0, true, true},
		{"the hand-off was taken for this pause", 60, true, false},
		{"a headless relaunch holds the session", 0, false, false},
	}
	for index, tc := range cases {
		sid := fmt.Sprintf("st%d", index+1)
		runner, window := idleRunner(t), idleWindow(t)
		cwd := t.TempDir()
		transcript := quietTranscript(t, cwd, t0)
		updateState(func(state object) {
			stateMap(state, "handedOff")[sid] = object{"at": t0 + tc.handoffAt, "model": "claude-opus-5", "mode": "window", "pid": float64(runner.pid)}
			if tc.window {
				stateMap(state, "launched")[sid] = object{"pid": float64(window.pid), "at": t0, "how": "terminal", "runner": float64(runner.pid)}
			}
			stateMap(state, "waits")[sid] = object{"kind": "batch", "window": "five_hour", "label": "5h", "used": float64(95), "threshold": float64(92), "hit": "threshold",
				"startedAt": t0 + 60, "until": now + 3600, "resumeAt": now + 3600, "cwd": cwd, "transcript": transcript, "scheduled": object{"method": "manual", "at": now + 3600}}
		})
		statusReadingFrom("another-window", nowSec(), 3, now+18000, 20, now+3*86400)

		triggerEarlyResumes(cfg)

		triggered := numberOr(getMap(getMap(readState(), "waits"), sid), "earlyTriggeredAt", 0) > 0
		if triggered != tc.triggered {
			t.Fatalf("%s: the early reset seen in another window triggered the runner: %v, want %v (journal %v)", tc.name, triggered, tc.triggered, journaledFor(sid))
		}
	}
}

func TestAFableRelaunchClosesTheWindowItsOwnPauseLeftIdle(t *testing.T) {
	relaunchSandbox(t)
	now := float64(nowSec())
	cases := []struct {
		name      string
		startedAt float64
		lastWrite float64
		closed    bool
	}{
		{"the pause settled seconds ago", now - 20, now - 15, true},
		{"the session was used after its pause, minutes ago", now - 3600, now - 1800, true},
		{"the session was used after its pause, just now", now - 600, now - 30, false},
	}
	for index, tc := range cases {
		sid := fmt.Sprintf("fb%d", index+1)
		runner, window := idleRunner(t), idleWindow(t)
		transcript := quietTranscript(t, t.TempDir(), tc.lastWrite)
		updateState(func(state object) {
			stateMap(state, "launched")[sid] = object{"pid": float64(window.pid), "at": now - 7200, "how": "terminal", "runner": float64(runner.pid)}
		})

		closePreviousLaunch(object{}, sid, object{"kind": "fable", "startedAt": tc.startedAt, "transcript": transcript})

		wait := 500 * time.Millisecond
		if tc.closed {
			wait = 5 * time.Second
		}
		if closed := window.endsWithin(wait); closed != tc.closed {
			t.Fatalf("%s: the previous window closed: %v, want %v", tc.name, closed, tc.closed)
		}
		if tc.closed && !slices.Contains(journaledFor(sid), "close-previous") {
			t.Fatalf("%s: closing the previous window was not journaled: %v", tc.name, journaledFor(sid))
		}
	}
}

func TestAWindowRunnerLeavesTheLaunchRecordOfTheWindowThatReplacedItsOwn(t *testing.T) {
	sandboxFiles(t)
	sid := "replaced-window"
	window, successor := idleWindow(t), idleWindow(t)
	ensureDir(files.launches)
	_, _, pidFile := launchFiles(sid)
	if err := os.WriteFile(pidFile, []byte(fmt.Sprint(window.pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	watched := make(chan bool, 1)
	go func() { watched <- waitForLaunchedSession(sid, pidFile, "terminal") }()
	recorded := false
	for deadline := time.Now().Add(20 * time.Second); !recorded && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		recorded = numberOr(getMap(getMap(readState(), "launched"), sid), "pid", 0) == float64(window.pid)
	}
	if !recorded {
		window.stop()
		<-watched
		t.Fatal("the launched window was never recorded")
	}
	next := object{"pid": float64(successor.pid), "at": float64(nowSec()), "how": "terminal"}
	updateState(func(state object) { stateMap(state, "launched")[sid] = cloneObject(next) })
	window.stop()
	select {
	case <-watched:
	case <-time.After(20 * time.Second):
		t.Fatal("the runner never noticed its window closed")
	}
	if record := getMap(getMap(readState(), "launched"), sid); numberOr(record, "pid", 0) != float64(successor.pid) {
		t.Fatalf("the next runner closed this window and recorded its own (pid %d); the runner that watched the closed one erased that record on its way out, so the next pause cannot close the new window: %v", successor.pid, record)
	}
}

func TestARunnerLeavesAPauseAnotherRunnerIsResumingAsItIs(t *testing.T) {
	calls := takeoverSandbox(t)
	sid := "claimed"
	now := float64(nowSec())
	runner, window := idleRunner(t), idleWindow(t)
	cwd := t.TempDir()
	startedAt := now - 600
	wait := object{"kind": "batch", "window": "five_hour", "label": "5h", "used": float64(95), "threshold": float64(92), "hit": "threshold",
		"startedAt": startedAt, "until": now - 10, "resumeAt": now - 5, "cwd": cwd, "transcript": quietTranscript(t, cwd, startedAt), "launchMode": "headless"}
	updateState(func(state object) {
		stateMap(state, "handedOff")[sid] = object{"at": startedAt + 590, "model": "claude-opus-5", "mode": "window", "pid": float64(runner.pid), "waitStartedAt": startedAt}
		stateMap(state, "launched")[sid] = object{"pid": float64(window.pid), "at": startedAt + 595, "how": "terminal"}
		stateMap(state, "waits")[sid] = cloneObject(wait)
	})
	statusReadingFrom(sid, nowSec(), 96, now+3600, 20, now+3*86400)

	resumeWait(sid, "")

	if got := launchesOf(calls, sid); got != 0 {
		t.Fatalf("runner %d is resuming this pause, yet a second runner launched the session %d time(s)", runner.pid, got)
	}
	stored := getMap(getMap(readState(), "waits"), sid)
	for _, field := range []string{"startedAt", "until", "resumeAt"} {
		if numberOr(stored, field, -1) != numberOr(wait, field, -2) {
			t.Fatalf("runner %d is resuming this pause; a second runner that read the limit as still active rewrote its %s (%v), so the pause would later pass for a newer one and a third runner would take the session from the window runner %d opened for it: %v", runner.pid, field, stored[field], runner.pid, stored)
		}
	}
	if getMap(stored, "scheduled") != nil {
		t.Fatalf("a second runner rescheduled the pause runner %d is resuming: %v", runner.pid, getMap(stored, "scheduled"))
	}
	if !slices.Contains(journaledFor(sid), "skip-launch") {
		t.Fatalf("leaving the session to the runner that holds it was not journaled: %v", journaledFor(sid))
	}
}

func TestARunnerReleasesOnlyItsOwnHandOffAndPause(t *testing.T) {
	sandboxFiles(t)
	sid := "release"
	now := float64(nowSec())
	own, replacement := now-600, now-60
	successor := object{"at": now, "model": "claude-opus-5", "mode": "window", "pid": float64(os.Getpid() + 1), "waitStartedAt": replacement}
	updateState(func(state object) {
		stateMap(state, "handedOff")[sid] = cloneObject(successor)
		stateMap(state, "waits")[sid] = object{"kind": "batch", "startedAt": replacement, "resumeAt": now + 3600}
	})

	releaseHandoff(sid, own)

	state := readState()
	if holder := getMap(getMap(state, "handedOff"), sid); numberOr(holder, "pid", 0) != numberOr(successor, "pid", -1) {
		t.Fatalf("a runner on its way out removed the hand-off of the runner that took the session over: %v", holder)
	}
	if wait := getMap(getMap(state, "waits"), sid); numberOr(wait, "startedAt", 0) != replacement {
		t.Fatalf("a runner on its way out removed the pause that replaced its own: %v", wait)
	}
	updateState(func(state object) {
		stateMap(state, "handedOff")[sid] = object{"at": now, "pid": float64(os.Getpid()), "waitStartedAt": replacement}
	})

	releaseHandoff(sid, replacement)

	state = readState()
	if getMap(getMap(state, "handedOff"), sid) != nil || getMap(getMap(state, "waits"), sid) != nil {
		t.Fatalf("the runner left its own hand-off or pause behind: %v / %v", getMap(getMap(state, "handedOff"), sid), getMap(getMap(state, "waits"), sid))
	}
}

func TestAPreviousWindowIsClosedOnlyWhileTheRunnerThatOpenedItStillWatchesIt(t *testing.T) {
	relaunchSandbox(t)
	now := float64(nowSec())
	cases := []struct {
		name   string
		runner func() float64
		closed bool
	}{
		{"its runner is alive and ours", func() float64 { return float64(idleRunner(t).pid) }, true},
		{"its runner is gone, so the pid may belong to something else now", func() float64 { return float64(goneRunner(t)) }, false},
		{"the record names no runner", func() float64 { return 0 }, false},
		{"its runner pid now runs another program", func() float64 { return float64(idleWindow(t).pid) }, false},
	}
	for index, tc := range cases {
		sid := fmt.Sprintf("pw%d", index+1)
		window := idleWindow(t)
		record := object{"pid": float64(window.pid), "at": now - 3*86400, "how": "terminal"}
		if runner := tc.runner(); runner > 0 {
			record["runner"] = runner
		}
		updateState(func(state object) { stateMap(state, "launched")[sid] = record })

		closePreviousLaunch(object{}, sid, nil)

		wait := 500 * time.Millisecond
		if tc.closed {
			wait = 5 * time.Second
		}
		if closed := window.endsWithin(wait); closed != tc.closed {
			t.Fatalf("%s: the recorded window was closed: %v, want %v", tc.name, closed, tc.closed)
		}
		if record := getMap(getMap(readState(), "launched"), sid); record != nil {
			t.Fatalf("%s: the launch record stayed behind: %v", tc.name, record)
		}
	}
}
