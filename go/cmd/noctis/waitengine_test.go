package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func windowsTaskSandbox(t *testing.T) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in powershell.exe is a shell script; on Windows the real one would run")
	}
	dir := sandboxFiles(t)
	files.runnerLauncher = filepath.Join(dir, "noctis-runner.cmd")
	previous := isWindows
	t.Cleanup(func() { isWindows = previous })
	isWindows = true
	scheduleBackendOverride = "task"
	t.Cleanup(func() { scheduleBackendOverride = "" })
	t.Setenv("NOCTIS_NO_TASKS", "")
	t.Setenv("NOCTIS_NO_SCHEDULE", "")
	bin := t.TempDir()
	calls := filepath.Join(bin, "powershell.log")
	t.Setenv("NOCTIS_TEST_POWERSHELL", calls)
	script := strings.Join([]string{
		"#!/bin/sh",
		`printf '%s\n' "$*" >> "$NOCTIS_TEST_POWERSHELL"`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(bin, "powershell.exe"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir, calls
}

func taskCalls(t *testing.T, calls string) []string {
	t.Helper()
	content, err := os.ReadFile(calls)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the powershell log cannot be read: %v", err)
	}
	made := []string{}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		verb := "other"
		switch {
		case strings.Contains(line, "Unregister-ScheduledTask "):
			verb = "unregister"
		case strings.Contains(line, "Register-ScheduledTask "):
			verb = "register"
		}
		_, name, _ := strings.Cut(line, "-TaskName '")
		name, _, _ = strings.Cut(name, "'")
		made = append(made, verb+" "+name)
	}
	return made
}

func taskLeftRegistered(made []string, name string) bool {
	registered := false
	for _, call := range made {
		switch call {
		case "register " + name:
			registered = true
		case "unregister " + name:
			registered = false
		}
	}
	return registered
}

func waitEngineConfig() object {
	return object{
		"thresholds": object{"session5h": float64(92), "weeklyAll": float64(89), "weeklyFable": float64(95)},
		"models":     object{"primary": "claude-fable-5", "fallback": "claude-opus-5"},
		"resume":     object{"mode": "window"},
		"alarm":      object{"enabled": false},
		"wait":       object{"workspaceGuard": false},
	}
}

func fableHitDecision() decision {
	return decision{fableHit: true, model: "claude-fable-5", usage: usageView{hasAny: true, fable: &window{used: 96, resetsAt: float64(nowSec() + 3*86400)}}}
}

func TestCancellingUnregistersATaskOnlyForAScheduleThatMayOwnOne(t *testing.T) {
	_, calls := windowsTaskSandbox(t)
	withFakeScheduler(t, nil)
	t.Setenv("HOME", t.TempDir())
	cases := []struct {
		name       string
		scheduled  object
		unregister bool
	}{
		{"no schedule recorded", nil, false},
		{"a task", object{"method": "task", "at": float64(1)}, true},
		{"a schedule recorded without its method", object{"at": float64(1)}, true},
		{"a sleeper", object{"method": "sleeper", "at": float64(1)}, false},
		{"a manual resume", object{"method": "manual", "at": float64(1)}, false},
		{"a systemd timer", object{"method": "systemd", "at": float64(1)}, false},
		{"a launchd job", object{"method": "launchd", "at": float64(1)}, false},
	}
	for index, tc := range cases {
		sid := "cancel-" + strconv.Itoa(index)
		before := len(taskCalls(t, calls))
		cancelScheduled(sid, tc.scheduled)
		made := taskCalls(t, calls)[before:]
		want := []string{}
		if tc.unregister {
			want = append(want, "unregister "+taskName(sid))
		}
		if strings.Join(made, "|") != strings.Join(want, "|") {
			t.Errorf("%s: cancelling ran %q, want %q", tc.name, made, want)
		}
	}
}

func TestStoringASessionsFirstWaitStartsNoPowershell(t *testing.T) {
	dir, calls := windowsTaskSandbox(t)
	now := float64(nowSec())
	record := object{"kind": "batch", "window": "five_hour", "until": now + 3600, "resumeAt": now + 3600, "startedAt": now, "inHook": false, "cwd": dir}
	if !registerWait("first-wait", record, waitEngineConfig()) {
		t.Fatal("the wait was not stored")
	}
	if made := taskCalls(t, calls); len(made) != 0 {
		t.Fatalf("storing the first wait of a session started powershell for a task nothing had registered: %q", made)
	}
}

func TestAFirstPauseOnlyRegistersItsRelaunchTask(t *testing.T) {
	dir, calls := windowsTaskSandbox(t)
	sid := "first-pause"
	now := nowSec()
	plan := &waitPlan{window: "five_hour", label: "5h", used: 93, threshold: 92, until: float64(now + 3*3600), hit: "threshold"}
	outcome := enforceWait("batch", object{"session_id": sid, "cwd": dir}, waitEngineConfig(), decision{wait: plan, model: "claude-opus-5"})
	if outcome.stop == "" {
		t.Fatalf("a three-hour pause was not handed to the runner: %+v", outcome)
	}
	if made, want := taskCalls(t, calls), []string{"register " + taskName(sid)}; strings.Join(made, "|") != strings.Join(want, "|") {
		t.Fatalf("the first pause of a session ran %q, want only %q", made, want)
	}
}

func TestATaskRegisteredBeforeItsWaitIsStoredStaysRegistered(t *testing.T) {
	dir, calls := windowsTaskSandbox(t)
	sid := "scheduled-first"
	name := taskName(sid)
	cfg := waitEngineConfig()
	now := float64(nowSec())
	scheduled := scheduleRunner(cfg, sid, now+600)
	if getString(scheduled, "method") != "task" || getString(scheduled, "taskName") != name {
		t.Fatalf("the runner was not scheduled as a task: %v", scheduled)
	}
	record := object{"kind": "stopfailure", "window": "unknown", "until": now, "resumeAt": now + 600, "startedAt": now, "inHook": false, "cwd": dir, "scheduled": scheduled}
	if !registerWait(sid, record, cfg) {
		t.Fatal("the wait was not stored")
	}
	if made, want := taskCalls(t, calls), []string{"register " + name}; strings.Join(made, "|") != strings.Join(want, "|") {
		t.Fatalf("storing the wait undid the task registered for it: ran %q, want only %q", made, want)
	}
	stored := getMap(getMap(getMap(readState(), "waits"), sid), "scheduled")
	if getString(stored, "method") != "task" || getString(stored, "taskName") != name {
		t.Fatalf("the stored wait does not name its task: %v", stored)
	}
}

func TestAFableSwitchKeepsTheTaskThatRelaunchesOnTheFallbackModel(t *testing.T) {
	dir, calls := windowsTaskSandbox(t)
	sid := "fable-switch"
	name := taskName(sid)
	cfg := waitEngineConfig()
	message := handleFableHit("batch", object{"session_id": sid, "cwd": dir}, cfg, fableHitDecision())
	if want := T("scoped.savedRelaunch", scopedLabel(cfg), formatNumber(96), "claude-opus-5"); message != want {
		t.Fatalf("the Fable switch said %q, want %q", message, want)
	}
	if made, want := taskCalls(t, calls), []string{"register " + name}; strings.Join(made, "|") != strings.Join(want, "|") {
		t.Fatalf("the task promised to relaunch on the fallback model was not kept: ran %q, want only %q", made, want)
	}
	stored := getMap(getMap(getMap(readState(), "waits"), sid), "scheduled")
	if getString(stored, "method") != "task" || getString(stored, "taskName") != name {
		t.Fatalf("the Fable wait does not name its task: %v", stored)
	}
	clearWait(sid, nil)
	if made := taskCalls(t, calls); taskLeftRegistered(made, name) {
		t.Fatalf("clearing the Fable wait left its task registered: ran %q", made)
	}
}

func TestAStopFailureRetryKeepsTheTaskThatRelaunchesTheSession(t *testing.T) {
	dir, calls := windowsTaskSandbox(t)
	sid := "overloaded"
	name := taskName(sid)
	onStopFailure(object{"session_id": sid, "cwd": dir, "error_type": "overloaded"}, waitEngineConfig())
	wait := getMap(getMap(readState(), "waits"), sid)
	if wait == nil || !getBool(wait, "overload", false) {
		t.Fatalf("the overload retry was not parked: %v", wait)
	}
	if made, want := taskCalls(t, calls), []string{"register " + name}; strings.Join(made, "|") != strings.Join(want, "|") {
		t.Fatalf("the task that retries the session was not kept: ran %q, want only %q", made, want)
	}
	if stored := getMap(wait, "scheduled"); getString(stored, "method") != "task" || getString(stored, "taskName") != name {
		t.Fatalf("the retry wait does not name its task: %v", stored)
	}
}

func TestAFableSwitchWhoseWaitCannotBeStoredUnregistersItsTask(t *testing.T) {
	dir, calls := windowsTaskSandbox(t)
	files.stateLock = filepath.Join(dir, "unreachable", "state.lock")
	sid := "fable-unstored"
	name := taskName(sid)
	message := handleFableHit("batch", object{"session_id": sid, "cwd": dir}, waitEngineConfig(), fableHitDecision())
	if want := T("wait.notStored", pluginName); message != want {
		t.Fatalf("a Fable switch whose wait could not be stored said %q, want %q", message, want)
	}
	if made, want := taskCalls(t, calls), []string{"register " + name, "unregister " + name}; strings.Join(made, "|") != strings.Join(want, "|") {
		t.Fatalf("the task registered for a wait that was never stored was not removed once: ran %q, want %q", made, want)
	}
}

func stopFailureMessage(t *testing.T, input object) string {
	t.Helper()
	previous := emitted
	t.Cleanup(func() { emitted = previous })
	printed := capturedStdout(t, func() { onStopFailure(input, waitEngineConfig()) })
	if strings.TrimSpace(printed) == "" {
		return ""
	}
	var output object
	if err := json.Unmarshal([]byte(printed), &output); err != nil {
		t.Fatalf("the StopFailure hook printed something that is not JSON: %q", printed)
	}
	return getString(output, "systemMessage")
}

func TestAStopFailureWhoseWaitCannotBeStoredUnregistersItsTask(t *testing.T) {
	dir, calls := windowsTaskSandbox(t)
	files.stateLock = filepath.Join(dir, "unreachable", "state.lock")
	sid := "stopfailure-unstored"
	name := taskName(sid)
	message := stopFailureMessage(t, object{"session_id": sid, "cwd": dir, "error_type": "rate_limit"})
	if want := T("wait.notStored", pluginName); message != want {
		t.Fatalf("a StopFailure whose wait could not be stored said %q, want %q", message, want)
	}
	if made, want := taskCalls(t, calls), []string{"register " + name, "unregister " + name}; strings.Join(made, "|") != strings.Join(want, "|") {
		t.Fatalf("the retry task registered for a wait that was never stored was not removed once: ran %q, want %q", made, want)
	}
}

func TestAStopFailureWhoseWaitCannotBeStoredStopsItsTimer(t *testing.T) {
	dir := sandboxFiles(t)
	files.stateLock = filepath.Join(dir, "unreachable", "state.lock")
	t.Setenv("NOCTIS_NO_TASKS", "")
	t.Setenv("NOCTIS_NO_SCHEDULE", "")
	recorded := withFakeScheduler(t, nil)
	scheduleBackendOverride = "systemd"
	t.Cleanup(func() { scheduleBackendOverride = "" })
	sid := "stopfailure-timer"
	unit := systemdUnit(sid)
	message := stopFailureMessage(t, object{"session_id": sid, "cwd": dir, "error_type": "rate_limit"})
	if want := T("wait.notStored", pluginName); message != want {
		t.Fatalf("a StopFailure whose wait could not be stored said %q, want %q", message, want)
	}
	commands := []string{}
	for _, entry := range *recorded {
		commands = append(commands, strings.Join(entry.args, " "))
	}
	started := -1
	for index, command := range commands {
		if strings.HasPrefix(command, "systemd-run ") && strings.Contains(command, " --unit="+unit+" ") {
			started = index
		}
	}
	if started < 0 {
		t.Fatalf("the retry was not scheduled as the session's systemd timer: ran %q", commands)
	}
	stop := "systemctl --user stop " + unit + ".timer " + unit + ".service"
	stopped := false
	for _, command := range commands[started+1:] {
		stopped = stopped || command == stop
	}
	if !stopped {
		t.Fatalf("the timer made for a wait that was never stored was left to fire: ran %q", commands)
	}
}

func TestAPauseWhoseWaitCannotBeStoredStartsNoPowershell(t *testing.T) {
	dir, calls := windowsTaskSandbox(t)
	files.stateLock = filepath.Join(dir, "unreachable", "state.lock")
	now := nowSec()
	plan := &waitPlan{window: "five_hour", label: "5h", used: 93, threshold: 92, until: float64(now + 3*3600), hit: "threshold"}
	outcome := enforceWait("batch", object{"session_id": "pause-unstored", "cwd": dir}, waitEngineConfig(), decision{wait: plan, model: "claude-opus-5"})
	if want := T("wait.notStored", pluginName); outcome.notice != want || outcome.stop != "" {
		t.Fatalf("a pause whose wait could not be stored returned %+v, want only the notice %q", outcome, want)
	}
	if made := taskCalls(t, calls); len(made) != 0 {
		t.Fatalf("a pause whose wait could not be stored started powershell: %q", made)
	}
}

func sleepStandIn(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows names a pid through Toolhelp, which needs nothing on PATH")
	}
	path, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("no sleep command to stand in for another process: %v", err)
	}
	return path
}

func standInNamed(t *testing.T, target, name string) string {
	t.Helper()
	link := filepath.Join(t.TempDir(), name)
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	return link
}

func watchStandIn(t *testing.T, child *exec.Cmd) <-chan struct{} {
	t.Helper()
	if err := child.Start(); err != nil {
		t.Fatalf("%s did not start: %v", child.Path, err)
	}
	done := make(chan struct{})
	go func() {
		_ = child.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = child.Process.Kill()
		<-done
	})
	return done
}

func startStandIn(t *testing.T, path string) (*exec.Cmd, <-chan struct{}) {
	t.Helper()
	child := exec.Command(path, "30")
	child.Args[0] = "sleep"
	return child, watchStandIn(t, child)
}

func TestThreadedStandIn(t *testing.T) {
	if os.Getenv("NOCTIS_TEST_THREADED_STAND_IN") == "" {
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func aThreadOf(t *testing.T, pid int) int {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		entries, _ := os.ReadDir(filepath.Join("/proc", strconv.Itoa(pid), "task"))
		for _, entry := range entries {
			if id, err := strconv.Atoi(entry.Name()); err == nil && id != pid {
				return id
			}
		}
	}
	t.Fatalf("process %d runs no thread besides its first", pid)
	return 0
}

func startThreadedStandIn(t *testing.T, name string) (*exec.Cmd, <-chan struct{}, int) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("only on Linux does kill take the id of a thread for its whole process")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(standInNamed(t, executable, name), "-test.run=^TestThreadedStandIn$")
	child.Env = append(os.Environ(), "NOCTIS_TEST_THREADED_STAND_IN=1")
	if _, err := child.StdinPipe(); err != nil {
		t.Fatal(err)
	}
	done := watchStandIn(t, child)
	thread := aThreadOf(t, child.Process.Pid)
	if !processAlive(thread) {
		t.Fatalf("thread %d of %s %d does not pass for a live pid", thread, name, child.Process.Pid)
	}
	return child, done, thread
}

func loggedAbout(what string, pid int) string {
	logged, _ := os.ReadFile(files.log)
	line := ""
	for _, candidate := range strings.Split(string(logged), "\n") {
		if strings.Contains(candidate, fmt.Sprintf(" %s %d ", what, pid)) {
			line = candidate
		}
	}
	return line
}

var detachedHelperKeys = []struct{ key, what string }{{"pid", "sleeper"}, {"watcherPid", "reset watcher"}}

func TestARecycledHelperPidIsLeftAloneWithoutPs(t *testing.T) {
	sleep := sleepStandIn(t)
	sandboxFiles(t)
	t.Setenv("PATH", t.TempDir())
	for _, helper := range detachedHelperKeys {
		stranger, done := startStandIn(t, sleep)
		pid := stranger.Process.Pid
		killDetached(object{"method": "sleeper", helper.key: float64(pid)}, helper.key, helper.what)
		select {
		case <-done:
			t.Fatalf("the recorded %s pid %d now belongs to %s, and with no ps on PATH it was killed: %v", helper.what, pid, sleep, stranger.ProcessState)
		case <-time.After(500 * time.Millisecond):
		}
		if line := loggedAbout(helper.what, pid); !strings.HasSuffix(line, "; left alone") {
			t.Errorf("leaving the %s pid %d alone was not logged: %q", helper.what, pid, line)
		}
	}
}

func TestProcessNamesReadWithoutPsAreWhatPsPrints(t *testing.T) {
	sleep := sleepStandIn(t)
	if runtime.GOOS != "linux" {
		t.Skip("only Linux can name a pid without ps")
	}
	ps, err := exec.LookPath("ps")
	if err != nil {
		t.Skipf("no ps to compare with: %v", err)
	}
	psNames := func(pid int) (string, error) {
		out, err := exec.Command(ps, "-p", strconv.Itoa(pid), "-o", "comm=").Output()
		return strings.TrimSpace(string(out)), err
	}
	if _, err := psNames(os.Getpid()); err != nil {
		t.Skipf("this ps cannot name even the test's own pid (busybox ps has no -p): %v", err)
	}
	long, _ := startStandIn(t, standInNamed(t, sleep, "a-helper-named-past-fifteen-characters"))
	processes := []int{os.Getpid(), long.Process.Pid}
	thread := aThreadOf(t, os.Getpid())
	printed := map[int]string{}
	for _, pid := range processes {
		name, err := psNames(pid)
		if err != nil || name == "" {
			t.Fatalf("ps could not name pid %d: %q, %v", pid, name, err)
		}
		printed[pid] = name
	}
	if name, _ := psNames(thread); name != "" {
		t.Fatalf("ps named thread %d of pid %d %q, as if it were a process of its own", thread, os.Getpid(), name)
	}
	printed[thread] = ""
	t.Setenv("PATH", t.TempDir())
	for _, pid := range append(processes, thread) {
		if got := processName(pid); got != printed[pid] {
			t.Errorf("with no ps on PATH pid %d is named %q, but ps prints %q", pid, got, printed[pid])
		}
	}
}

func TestOurOwnHelperIsStillStoppedWithoutPs(t *testing.T) {
	sleep := sleepStandIn(t)
	if runtime.GOOS != "linux" {
		t.Skip("only Linux can name a pid without ps")
	}
	sandboxFiles(t)
	own := standInNamed(t, sleep, pluginName)
	t.Setenv("PATH", t.TempDir())
	for _, helper := range detachedHelperKeys {
		child, done := startStandIn(t, own)
		pid := child.Process.Pid
		killDetached(object{"method": "sleeper", helper.key: float64(pid)}, helper.key, helper.what)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("our own %s %d was left running with no ps on PATH", helper.what, pid)
		}
		if status, _ := child.ProcessState.Sys().(syscall.WaitStatus); !status.Signaled() || status.Signal() != syscall.SIGKILL {
			t.Fatalf("our own %s %d was not stopped: %v", helper.what, pid, child.ProcessState)
		}
	}
}

func TestALaunchPidThatIsNowAThreadOfNodeIsNotClosed(t *testing.T) {
	node, done, thread := startThreadedStandIn(t, "node")
	sandboxFiles(t)
	t.Setenv("PATH", t.TempDir())
	if !looksLikeSessionProcess(node.Process.Pid) {
		t.Fatalf("the stand-in %d is not taken for a node process, so this proves nothing", node.Process.Pid)
	}
	if looksLikeSessionProcess(thread) {
		t.Errorf("thread %d of node %d is taken for a session window of its own", thread, node.Process.Pid)
	}
	recordLaunch("s1", thread, "terminal")
	closePreviousLaunch(object{}, "s1", nil)
	select {
	case <-done:
		t.Fatalf("the launch record of s1 names %d, now a thread of node %d, and closing the previous window killed that node: %v", thread, node.Process.Pid, node.ProcessState)
	case <-time.After(500 * time.Millisecond):
	}
	recordLaunch("s1", node.Process.Pid, "terminal")
	closePreviousLaunch(object{}, "s1", nil)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("a launch record naming node %d itself did not close it, so leaving its thread alone proves nothing", node.Process.Pid)
	}
}

func TestAHelperPidThatIsNowAThreadIsLeftAlone(t *testing.T) {
	helper, done, thread := startThreadedStandIn(t, pluginName)
	sandboxFiles(t)
	t.Setenv("PATH", t.TempDir())
	for _, recorded := range detachedHelperKeys {
		killDetached(object{"method": "sleeper", recorded.key: float64(thread)}, recorded.key, recorded.what)
		select {
		case <-done:
			t.Fatalf("the recorded %s pid %d is now a thread of %s %d, and that whole process was killed: %v", recorded.what, thread, pluginName, helper.Process.Pid, helper.ProcessState)
		case <-time.After(500 * time.Millisecond):
		}
		if line := loggedAbout(recorded.what, thread); !strings.HasSuffix(line, " cannot be identified; left alone") {
			t.Errorf("leaving the %s pid %d alone was not logged: %q", recorded.what, thread, line)
		}
	}
	own := aThreadOf(t, os.Getpid())
	killDetached(object{"method": "sleeper", "pid": float64(own)}, "pid", "sleeper")
	if line := loggedAbout("sleeper", own); !strings.HasSuffix(line, " cannot be identified; left alone") {
		t.Errorf("the recorded sleeper pid %d is now a thread of this very process, and leaving it alone was not logged: %q", own, line)
	}
	killDetached(object{"method": "sleeper", "pid": float64(helper.Process.Pid)}, "pid", "sleeper")
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("our own sleeper %d was left running, so leaving its thread alone proves nothing", helper.Process.Pid)
	}
}

func sharedWaitConfig() object {
	cfg := waitEngineConfig()
	cfg["wait"] = object{"earlyResetPollMinutes": 0.05, "resetMarginSeconds": float64(0), "builtinGraceSeconds": float64(0), "maxInHookMinutes": float64(330), "workspaceGuard": false}
	return cfg
}

func fiveHourPlan(until float64) *waitPlan {
	return &waitPlan{window: "five_hour", label: "5h", used: 95, threshold: 92, until: until, hit: "threshold"}
}

type pauseResult struct {
	outcome waitOutcome
	at      int64
}

func pauseAside(kind, sid, cwd string, cfg object, plan *waitPlan) <-chan pauseResult {
	done := make(chan pauseResult, 1)
	go func() {
		outcome := enforceWait(kind, object{"session_id": sid, "cwd": cwd, "prompt": "keep going"}, cfg, decision{wait: plan, model: "claude-opus-5"})
		done <- pauseResult{outcome: outcome, at: nowSec()}
	}()
	return done
}

func storedWaitOf(t *testing.T, sid string) object {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if wait := getMap(getMap(readState(), "waits"), sid); getMap(wait, "scheduled") != nil {
			return wait
		}
	}
	return nil
}

func actionCount(actions []string, wanted string) int {
	count := 0
	for _, action := range actions {
		if action == wanted {
			count++
		}
	}
	return count
}

func heldUntilTheReset(t *testing.T, results []pauseResult, until float64) {
	t.Helper()
	want := T("wait.resumed", "5h", formatNumber(95), durationText(0))
	for index, result := range results {
		if result.outcome.stop != "" || result.outcome.notice != want {
			t.Errorf("hook %d returned %+v, want only the notice %q", index+1, result.outcome, want)
		}
		if float64(result.at) < until {
			t.Errorf("hook %d let its tool run at %d, %ds before the 5h reset", index+1, result.at, int64(until)-result.at)
		}
	}
	if waits := getMap(readState(), "waits"); len(waits) != 0 {
		t.Errorf("the shared wait outlived the hooks that held it: %v", waits)
	}
}

func TestAnInHookWaitLeavesTheWaitThatReplacedItAndItsRunnerAlone(t *testing.T) {
	dir := sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "")
	t.Setenv("NOCTIS_NO_SCHEDULE", "")
	recorded := withFakeScheduler(t, nil)
	scheduleBackendOverride = "systemd"
	t.Cleanup(func() { scheduleBackendOverride = "" })
	cfg := sharedWaitConfig()
	sid := "replaced"
	now := nowSec()
	sleeper := pauseAside("batch", sid, dir, cfg, fiveHourPlan(float64(now+8)))
	registered := storedWaitOf(t, sid)
	weekly := &waitPlan{window: "seven_day", label: "Weekly", used: 90, threshold: 89, until: float64(now + 3*86400), hit: "threshold"}
	var prompt waitOutcome
	if registered != nil {
		time.Sleep(1100 * time.Millisecond)
		prompt = enforceWait("prompt", object{"session_id": sid, "cwd": dir, "prompt": "next step"}, cfg, decision{wait: weekly, model: "claude-opus-5"})
	}
	armed := len(*recorded)
	held := <-sleeper
	if registered == nil {
		t.Fatal("the in-hook wait was never stored with its runner")
	}
	if prompt.stop == "" {
		t.Fatalf("the weekly pause did not stop the prompt: %+v", prompt)
	}
	stored := getMap(getMap(readState(), "waits"), sid)
	if getString(stored, "kind") != "prompt" || getString(stored, "window") != "seven_day" || getString(getMap(stored, "scheduled"), "method") != "systemd" {
		t.Fatalf("the weekly wait that replaced the in-hook one did not survive it: %v", stored)
	}
	for _, entry := range (*recorded)[armed:] {
		if command := strings.Join(entry.args, " "); strings.HasPrefix(command, "systemctl --user stop ") {
			t.Fatalf("the sleeper stopped the runner of the weekly wait that replaced it: %q", command)
		}
	}
	if getBool(getMap(getMap(readState(), "checkpoints"), sid), "consumed", false) {
		t.Fatal("the sleeper consumed the checkpoint of the weekly wait that replaced it")
	}
	if want := T("wait.saved", "Weekly", formatNumber(90), formatTime(weekly.until), ""); held.outcome.stop != want || held.outcome.notice != "" {
		t.Fatalf("the replaced sleeper returned %+v at %d, before the 5h reset, want only the stop %q", held.outcome, held.at, want)
	}
	if actions := journalActions(); hasAction(actions, "wait-cancelled") {
		t.Fatalf("the replaced wait was journaled as cancelled: %v", actions)
	}
}

func TestTwoPausesOfOneSessionMomentsApartShareOneWait(t *testing.T) {
	dir := sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	cfg := sharedWaitConfig()
	sid := "fan-out"
	until := float64(nowSec() + 6)
	first := pauseAside("batch", sid, dir, cfg, fiveHourPlan(until))
	time.Sleep(1100 * time.Millisecond)
	second := pauseAside("batch", sid, dir, cfg, fiveHourPlan(until+1))
	heldUntilTheReset(t, []pauseResult{<-first, <-second}, until)
	actions := journalActions()
	if actionCount(actions, "pause") != 1 || actionCount(actions, "join-wait") != 1 || hasAction(actions, "wait-cancelled") {
		t.Fatalf("two pauses of one session for the same reset, a second apart, did not share one wait: %v", actions)
	}
	if !getBool(getMap(getMap(readState(), "checkpoints"), sid), "consumed", false) {
		t.Fatal("the checkpoint of the shared wait was not consumed when it ended")
	}
}

func TestAPauseThatMissedASlowSiblingsWaitStillJoinsIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the slow stand-in git is a shell script")
	}
	dir := sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nsleep 1.5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg := sharedWaitConfig()
	sid := "slow-checkpoint"
	until := float64(nowSec() + 7)
	first := pauseAside("batch", sid, dir, cfg, fiveHourPlan(until))
	time.Sleep(1100 * time.Millisecond)
	second := pauseAside("batch", sid, dir, cfg, fiveHourPlan(until))
	heldUntilTheReset(t, []pauseResult{<-first, <-second}, until)
	actions := journalActions()
	if actionCount(actions, "pause") != 1 || actionCount(actions, "join-wait") != 1 || hasAction(actions, "wait-cancelled") {
		t.Fatalf("a pause that looked before its sibling stored the wait did not join it once its checkpoint was written: %v", actions)
	}
}

func TestAnInHookWaitReplacedByAPauseForTheSameResetHoldsUntilTheReset(t *testing.T) {
	dir := sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	cfg := sharedWaitConfig()
	sid := "interrupted"
	until := float64(nowSec() + 6)
	batch := pauseAside("batch", sid, dir, cfg, fiveHourPlan(until))
	registered := storedWaitOf(t, sid)
	prompt := make(<-chan pauseResult)
	if registered != nil {
		time.Sleep(1100 * time.Millisecond)
		prompt = pauseAside("prompt", sid, dir, cfg, fiveHourPlan(until))
	}
	first := <-batch
	if registered == nil {
		t.Fatal("the in-hook wait was never stored")
	}
	heldUntilTheReset(t, []pauseResult{first, <-prompt}, until)
	actions := journalActions()
	if actionCount(actions, "pause") != 2 || actionCount(actions, "join-wait") != 1 || hasAction(actions, "wait-cancelled") {
		t.Fatalf("the replaced sleeper did not go on holding on the wait that replaced it: %v", actions)
	}
	if !getBool(getMap(getMap(readState(), "checkpoints"), sid), "consumed", false) {
		t.Fatal("the checkpoint of the wait that replaced the first one was not consumed when it ended")
	}
}

func TestAnInHookWaitLeavesAWaitStoredInTheSameSecondAlone(t *testing.T) {
	dir := sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "")
	t.Setenv("NOCTIS_NO_SCHEDULE", "")
	recorded := withFakeScheduler(t, nil)
	scheduleBackendOverride = "systemd"
	t.Cleanup(func() { scheduleBackendOverride = "" })
	cfg := sharedWaitConfig()
	sid := "same-second"
	now := nowSec()
	sleeper := pauseAside("batch", sid, dir, cfg, fiveHourPlan(float64(now+4)))
	registered := storedWaitOf(t, sid)
	weeklyUntil := float64(now + 3*86400)
	if registered != nil {
		updateState(func(state object) {
			stateMap(state, "waits")[sid] = object{
				"kind": "prompt", "window": "seven_day", "label": "Weekly", "used": float64(90), "until": weeklyUntil, "resumeAt": weeklyUntil,
				"inHook": false, "startedAt": numberOr(registered, "startedAt", 0), "scheduled": object{"method": "systemd", "unit": systemdUnit(sid), "at": weeklyUntil},
			}
		})
	}
	armed := len(*recorded)
	held := <-sleeper
	if registered == nil {
		t.Fatal("the in-hook wait was never stored with its runner")
	}
	stored := getMap(getMap(readState(), "waits"), sid)
	if getString(stored, "window") != "seven_day" || getString(getMap(stored, "scheduled"), "method") != "systemd" {
		t.Fatalf("a weekly wait stored in the same second as the in-hook one was deleted when the sleeper woke: %v", stored)
	}
	for _, entry := range (*recorded)[armed:] {
		if command := strings.Join(entry.args, " "); strings.HasPrefix(command, "systemctl --user stop ") {
			t.Fatalf("the sleeper stopped the runner of a weekly wait it does not own: %q", command)
		}
	}
	if want := T("wait.saved", "Weekly", formatNumber(90), formatTime(weeklyUntil), ""); held.outcome.stop != want || held.outcome.notice != "" {
		t.Fatalf("the sleeper went on over the weekly pause stored in its place: %+v, want only the stop %q", held.outcome, want)
	}
}

func TestClaimingAWaitJoinsOnlyAFreshWaitForTheSameReset(t *testing.T) {
	sandboxFiles(t)
	cfg := sharedWaitConfig()
	now := float64(nowSec())
	fiveHour := func(until, startedAt float64) object {
		return object{"kind": "batch", "window": "five_hour", "until": until, "resumeAt": until, "startedAt": startedAt, "inHook": true}
	}
	weekly := object{"kind": "batch", "window": "seven_day", "until": now + 600, "resumeAt": now + 600, "startedAt": now + 1, "inHook": true}
	cases := []struct {
		name  string
		kind  string
		held  object
		claim object
		join  bool
	}{
		{"a reset one second later", "batch", fiveHour(now+600, now), fiveHour(now+601, now+1), true},
		{"a reset two seconds earlier", "stop", fiveHour(now+600, now), fiveHour(now+598, now+1), true},
		{"a reset three seconds later", "batch", fiveHour(now+600, now), fiveHour(now+603, now+1), false},
		{"another window", "batch", fiveHour(now+600, now), weekly, false},
		{"a wait stored over two minutes ago", "batch", fiveHour(now+600, now-121), fiveHour(now+600, now+1), false},
		{"a prompt", "prompt", fiveHour(now+600, now), fiveHour(now+600, now+1), false},
	}
	for index, tc := range cases {
		sid := "claim-" + strconv.Itoa(index)
		updateState(func(state object) { stateMap(state, "waits")[sid] = cloneObject(tc.held) })
		current, joined := claimWait(tc.kind, sid, cloneObject(tc.claim), cfg)
		kept := numberOr(getMap(getMap(readState(), "waits"), sid), "startedAt", -1)
		want := tc.claim
		if tc.join {
			want = tc.held
		}
		if joined != tc.join || numberOr(current, "startedAt", -1) != numberOr(want, "startedAt", 0) || kept != numberOr(want, "startedAt", 0) {
			t.Errorf("%s: claiming returned the wait started at %v (joined %t) and left the one started at %v, want %v (joined %t)", tc.name, current["startedAt"], joined, kept, want["startedAt"], tc.join)
		}
	}
}

func TestClaimingAWaitStopsOnlyTheRunnerOfTheWaitItReplaces(t *testing.T) {
	sandboxFiles(t)
	recorded := withFakeScheduler(t, nil)
	cfg := sharedWaitConfig()
	sid := "claim-runner"
	unit := systemdUnit(sid)
	now := float64(nowSec())
	updateState(func(state object) {
		stateMap(state, "waits")[sid] = object{"kind": "batch", "window": "five_hour", "until": now + 600, "resumeAt": now + 600, "startedAt": now, "inHook": true, "scheduled": object{"method": "systemd", "unit": unit, "at": now + 600}}
	})
	stops := func() int {
		count := 0
		for _, entry := range *recorded {
			if strings.Join(entry.args, " ") == "systemctl --user stop "+unit+".timer "+unit+".service" {
				count++
			}
		}
		return count
	}
	if _, joined := claimWait("batch", sid, object{"kind": "batch", "window": "five_hour", "until": now + 601, "resumeAt": now + 601, "startedAt": now + 1, "inHook": true}, cfg); !joined || stops() != 0 {
		t.Fatalf("joining the wait of the same reset stopped its runner (joined %t, %d stop(s))", joined, stops())
	}
	if _, joined := claimWait("batch", sid, object{"kind": "batch", "window": "seven_day", "until": now + 900, "resumeAt": now + 900, "startedAt": now + 1, "inHook": true}, cfg); joined || stops() != 1 {
		t.Fatalf("replacing the wait of another window did not stop its runner exactly once (joined %t, %d stop(s))", joined, stops())
	}
	if stored := getMap(getMap(readState(), "waits"), sid); getString(stored, "window") != "seven_day" || getMap(stored, "scheduled") != nil {
		t.Fatalf("the replacing wait was not stored as it was claimed: %v", stored)
	}
}

func TestAPromptPauseNeverJoinsTheWaitOfTheSameReset(t *testing.T) {
	dir := sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	cfg := waitEngineConfig()
	sid := "prompt-join"
	now := float64(nowSec())
	until := now + 3*3600
	updateState(func(state object) {
		stateMap(state, "waits")[sid] = object{"kind": "batch", "window": "five_hour", "label": "5h", "used": float64(95), "until": until, "resumeAt": until, "startedAt": now, "inHook": false, "queuedPrompt": ""}
	})
	prompt := enforceWait("prompt", object{"session_id": sid, "cwd": dir, "prompt": "finish the migration"}, cfg, decision{wait: fiveHourPlan(until + 1), model: "claude-opus-5"})
	stored := getMap(getMap(readState(), "waits"), sid)
	if getString(stored, "kind") != "prompt" || getString(stored, "queuedPrompt") != "finish the migration" {
		t.Fatalf("a prompt pause joined the wait of the same reset instead of queueing its prompt: %v", stored)
	}
	if want := T("wait.saved", "5h", formatNumber(95), formatTime(until+1), "") + " " + T("wait.savedHint"); prompt.stop != want {
		t.Fatalf("the prompt pause said %q, want %q", prompt.stop, want)
	}
	batch := enforceWait("batch", object{"session_id": sid, "cwd": dir}, cfg, decision{wait: fiveHourPlan(until), model: "claude-opus-5"})
	if want := T("wait.saved", "5h", formatNumber(95), formatTime(until+1), ""); batch.stop != want || getString(getMap(getMap(readState(), "waits"), sid), "queuedPrompt") != "finish the migration" {
		t.Fatalf("a batch pause for the same reset did not join the prompt's wait: %+v", batch)
	}
	if actions := journalActions(); actionCount(actions, "pause") != 1 || actionCount(actions, "join-wait") != 1 {
		t.Fatalf("want one pause for the prompt and one join for the batch, journaled %v", actions)
	}
}

func toolHookAside(sid, cwd string, cfg object, plan *waitPlan, deciding time.Duration) <-chan pauseResult {
	done := make(chan pauseResult, 1)
	go func() {
		releaseInterruptedWait(sid, readState())
		time.Sleep(deciding)
		outcome := enforceWait("batch", object{"session_id": sid, "cwd": cwd}, cfg, decision{wait: plan, model: "claude-opus-5"})
		done <- pauseResult{outcome: outcome, at: nowSec()}
	}()
	return done
}

func unitStops(recorded []recordedCommand, unit string) int {
	count := 0
	for _, entry := range recorded {
		if strings.Join(entry.args, " ") == "systemctl --user stop "+unit+".timer "+unit+".service" {
			count++
		}
	}
	return count
}

func TestAToolHookBesideASiblingSleepingOnTheSameResetJoinsItsWait(t *testing.T) {
	dir := sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "")
	t.Setenv("NOCTIS_NO_SCHEDULE", "")
	recorded := withFakeScheduler(t, nil)
	scheduleBackendOverride = "systemd"
	t.Cleanup(func() { scheduleBackendOverride = "" })
	cfg := sharedWaitConfig()
	sid := "per-call"
	until := float64(nowSec() + 6)
	first := toolHookAside(sid, dir, cfg, fiveHourPlan(until), 0)
	registered := storedWaitOf(t, sid)
	second := make(<-chan pauseResult)
	if registered != nil {
		second = toolHookAside(sid, dir, cfg, fiveHourPlan(until+1), 0)
	}
	held := <-first
	if registered == nil {
		t.Fatal("the first hook's in-hook wait was never stored with its runner")
	}
	heldUntilTheReset(t, []pauseResult{held, <-second}, until)
	actions := journalActions()
	if actionCount(actions, "pause") != 1 || actionCount(actions, "join-wait") != 1 || hasAction(actions, "wait-cancelled") || hasAction(actions, "wait-replaced") {
		t.Fatalf("a tool hook that ran beside a sibling sleeping on the same reset did not join its wait: %v", actions)
	}
	armed, stopped := 0, 0
	for _, entry := range *recorded {
		switch command := strings.Join(entry.args, " "); {
		case strings.HasPrefix(command, "systemd-run "):
			armed++
		case armed > 0 && command == "systemctl --user stop "+systemdUnit(sid)+".timer "+systemdUnit(sid)+".service":
			stopped++
		}
	}
	if armed != 1 || stopped != 1 {
		t.Fatalf("the hooks armed %d runner(s) and stopped them %d time(s), want one runner for the shared wait, stopped once when it ended", armed, stopped)
	}
	if samples := getList(readState(), "interruptedWaits"); len(samples) != 0 {
		t.Fatalf("a sibling that was still sleeping was counted as an interrupted wait: %v", samples)
	}
}

func TestAToolHookThatDecidesSlowlyBesideASleepingSiblingDoesNotCancelIt(t *testing.T) {
	dir := sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	cfg := sharedWaitConfig()
	sid := "slow-sibling"
	until := float64(nowSec() + 12)
	first := toolHookAside(sid, dir, cfg, fiveHourPlan(until), 0)
	registered := storedWaitOf(t, sid)
	second := make(<-chan pauseResult)
	if registered != nil {
		second = toolHookAside(sid, dir, cfg, fiveHourPlan(until), 3500*time.Millisecond)
	}
	held := <-first
	if registered == nil {
		t.Fatal("the first hook's in-hook wait was never stored")
	}
	heldUntilTheReset(t, []pauseResult{held, <-second}, until)
	actions := journalActions()
	if actionCount(actions, "pause") != 1 || actionCount(actions, "join-wait") != 1 || hasAction(actions, "wait-cancelled") {
		t.Fatalf("a sibling that took longer than a tick to decide cancelled the wait it should have joined: %v", actions)
	}
}

type heldResult struct {
	hold waitHold
	at   int64
}

func holdAside(sid string, cfg object, plan *waitPlan, held object) <-chan heldResult {
	done := make(chan heldResult, 1)
	go func() {
		hold := holdWait("batch", sid, cfg, plan, plan.until, held, true)
		done <- heldResult{hold: hold, at: nowSec()}
	}()
	return done
}

func TestAnInHookWaitHoldsOnAWaitForTheSameResetThatItFindsInItsPlaceLongAfterBothBegan(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	cfg := sharedWaitConfig()
	sid := "found-late"
	now := nowSec()
	until, began := float64(now+5), float64(now-200)
	wait := func(holder string, reset float64) object {
		return object{"kind": "batch", "window": "five_hour", "label": "5h", "used": float64(95), "until": reset, "resumeAt": reset, "inHook": true, "startedAt": began, "heartbeat": float64(now), "holder": holder}
	}
	held := wait("first-hook", until)
	updateState(func(state object) { stateMap(state, "waits")[sid] = cloneObject(held) })
	sleeping := holdAside(sid, cfg, fiveHourPlan(until), held)
	sibling := wait("second-hook", until+1)
	sibling["scheduled"] = object{"method": "manual", "at": until + 1}
	updateState(func(state object) { stateMap(state, "waits")[sid] = sibling })
	result := <-sleeping
	if result.hold.stop != "" || result.hold.cancelled || result.hold.owned {
		t.Fatalf("a sleeper that found a wait for its own reset stored in its place in the same second, %ds after both began, did not hold on it: %+v", now-int64(began), result.hold)
	}
	if float64(result.at) < until {
		t.Fatalf("the sleeper let its tool run at %d, %ds before the 5h reset", result.at, int64(until)-result.at)
	}
	if stored := getMap(getMap(readState(), "waits"), sid); getString(stored, "holder") != "second-hook" || getMap(stored, "scheduled") == nil {
		t.Fatalf("the sleeper that held on its sibling's wait deleted it: %v", stored)
	}
	if actions := journalActions(); !hasAction(actions, "join-wait") || hasAction(actions, "wait-replaced") {
		t.Fatalf("want the sleeper to join the wait found in its place, journaled %v", actions)
	}
}

func TestAnInHookWaitSeesAWaitStoredInItsPlaceInTheSameSecondAtItsNextTick(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	cfg := sharedWaitConfig()
	sid := "seen-soon"
	now := nowSec()
	until, weeklyUntil := float64(now+40), float64(now+3*86400)
	held := object{"kind": "batch", "window": "five_hour", "label": "5h", "used": float64(95), "until": until, "resumeAt": until, "inHook": true, "startedAt": float64(now), "heartbeat": float64(now), "holder": "first-hook"}
	updateState(func(state object) { stateMap(state, "waits")[sid] = cloneObject(held) })
	sleeping := holdAside(sid, cfg, fiveHourPlan(until), held)
	updateState(func(state object) {
		stateMap(state, "waits")[sid] = object{"kind": "prompt", "window": "seven_day", "label": "Weekly", "used": float64(90), "until": weeklyUntil, "resumeAt": weeklyUntil, "inHook": false, "startedAt": float64(now), "holder": "prompt-hook"}
	})
	replaced := time.Now()
	result := <-sleeping
	if want := T("wait.saved", "Weekly", formatNumber(90), formatTime(weeklyUntil), ""); result.hold.stop != want {
		t.Fatalf("the sleeper whose wait was replaced by a weekly pause returned %+v, want the stop %q", result.hold, want)
	}
	if took := time.Since(replaced); took > 10*time.Second {
		t.Fatalf("a wait stored in the sleeper's place in the same second was seen only after %s, at the reset, not at the next tick", took.Round(time.Second))
	}
}

func deadPid(t *testing.T) int {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	gone := exec.Command(executable, "-test.run=^$")
	if err := gone.Run(); err != nil {
		t.Fatalf("the short-lived stand-in did not run: %v", err)
	}
	return gone.Process.Pid
}

func sleepingWait(sid, holder string, heartbeat float64) object {
	now := float64(nowSec())
	return object{"kind": "batch", "window": "five_hour", "until": now + 600, "resumeAt": now + 600, "inHook": true, "startedAt": now - 300, "heartbeat": heartbeat, "holder": holder, "scheduled": object{"method": "systemd", "unit": systemdUnit(sid), "at": now + 600}}
}

func TestReleasingAnInterruptedWaitLeavesOnlyTheWaitOfALiveSleeperAlone(t *testing.T) {
	sandboxFiles(t)
	recorded := withFakeScheduler(t, nil)
	live, killed := strconv.Itoa(os.Getpid()), strconv.Itoa(deadPid(t))
	now := float64(nowSec())
	cases := []struct {
		name      string
		holder    string
		heartbeat float64
		waking    bool
		kept      bool
	}{
		{"a hook still sleeping on it", live + "-a", now, false, true},
		{"a hook that was killed", killed + "-b", now, false, false},
		{"a hook from before sleepers named themselves", "", now, false, false},
		{"a hook whose heartbeat stopped long ago", live + "-c", now - 400, false, false},
		{"a same-session wake the person took over from", "", now, true, false},
	}
	for index, tc := range cases {
		sid := "sleeper-" + strconv.Itoa(index)
		record := sleepingWait(sid, tc.holder, tc.heartbeat)
		if tc.waking {
			record["inHook"], record["waking"] = false, now
		}
		updateState(func(state object) { stateMap(state, "waits")[sid] = record })
		releaseInterruptedWait(sid, readState())
		kept := getMap(getMap(readState(), "waits"), sid) != nil
		stops := unitStops(*recorded, systemdUnit(sid))
		if kept != tc.kept || (stops == 0) != tc.kept {
			t.Errorf("%s: the in-hook wait was kept %t with its runner stopped %d times, want kept %t", tc.name, kept, stops, tc.kept)
		}
	}
}

func TestReleasingAnInterruptedWaitLeavesASleeperThatMissedItsHeartbeatAlone(t *testing.T) {
	sleep := sleepStandIn(t)
	if runtime.GOOS != "linux" {
		t.Skip("the stand-in is named through /proc only on Linux")
	}
	sandboxFiles(t)
	recorded := withFakeScheduler(t, nil)
	suspended, _ := startStandIn(t, standInNamed(t, sleep, pluginName))
	stranger, _ := startStandIn(t, sleep)
	stale := float64(nowSec() - 400)
	cases := []struct {
		name string
		pid  int
		kept bool
	}{
		{"our hook, back from a suspend before its next heartbeat", suspended.Process.Pid, true},
		{"a pid that now runs another program", stranger.Process.Pid, false},
	}
	for index, tc := range cases {
		sid := "stale-" + strconv.Itoa(index)
		updateState(func(state object) {
			stateMap(state, "waits")[sid] = sleepingWait(sid, strconv.Itoa(tc.pid)+"-x", stale)
		})
		releaseInterruptedWait(sid, readState())
		kept := getMap(getMap(readState(), "waits"), sid) != nil
		if stops := unitStops(*recorded, systemdUnit(sid)); kept != tc.kept || (stops == 0) != tc.kept {
			t.Errorf("%s: the in-hook wait was kept %t with its runner stopped %d times, want kept %t", tc.name, kept, stops, tc.kept)
		}
	}
}

func TestAClaimWhoseWaitIsNotKeptStopsTheRunnerOfTheWaitItDisplaced(t *testing.T) {
	sandboxFiles(t)
	recorded := withFakeScheduler(t, nil)
	cfg := sharedWaitConfig()
	sid := "claim-lost"
	now := float64(nowSec())
	updateState(func(state object) {
		stateMap(state, "waits")[sid] = object{"kind": "batch", "window": "five_hour", "until": now + 600, "resumeAt": now + 600, "startedAt": now, "inHook": true, "scheduled": object{"method": "systemd", "unit": systemdUnit(sid), "at": now + 600}}
	})
	lapsed := now - 3*86400
	stored, joined := claimWait("batch", sid, object{"kind": "batch", "window": "seven_day", "until": lapsed, "resumeAt": lapsed, "startedAt": now + 1, "inHook": false}, cfg)
	if stored != nil || joined {
		t.Fatalf("a claim whose wait was dropped as soon as it was stored reported %v (joined %t)", stored, joined)
	}
	if stops := unitStops(*recorded, systemdUnit(sid)); stops != 1 {
		t.Fatalf("the runner of the wait the claim displaced was stopped %d times, want once", stops)
	}
}

func TestAnInHookWaitJoinsAWaitForTheSameResetWithNoStartTimeOnce(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	cfg := sharedWaitConfig()
	sid := "no-start"
	now := nowSec()
	until := float64(now + 4)
	held := object{"kind": "batch", "window": "five_hour", "label": "5h", "used": float64(95), "until": until, "resumeAt": until, "inHook": true, "startedAt": float64(now), "heartbeat": float64(now), "holder": "first-hook"}
	updateState(func(state object) { stateMap(state, "waits")[sid] = cloneObject(held) })
	sleeping := holdAside(sid, cfg, fiveHourPlan(until), held)
	updateState(func(state object) {
		stateMap(state, "waits")[sid] = object{"kind": "batch", "window": "five_hour", "label": "5h", "used": float64(95), "until": until, "resumeAt": until, "inHook": true, "holder": "older-hook"}
	})
	select {
	case result := <-sleeping:
		if result.hold.stop != "" || result.hold.owned || float64(result.at) < until {
			t.Fatalf("a sleeper that found a wait for its reset with no start time in its place returned %+v at %d, want it held on until %d", result.hold, result.at, int64(until))
		}
	case <-time.After(20 * time.Second):
		t.Fatalf("the sleeper never let go of the wait with no start time: %v", journalActions())
	}
	if joins := actionCount(journalActions(), "join-wait"); joins != 1 {
		t.Fatalf("the sleeper joined the wait found in its place %d times, want once", joins)
	}
}

type hostHookRun struct {
	answer   object
	took     time.Duration
	answered bool
	held     object
}

func runHostHook(t *testing.T, host string, payload object, sid string, within time.Duration) hostHookRun {
	t.Helper()
	stdin := filepath.Join(t.TempDir(), "hook-input.json")
	if err := os.WriteFile(stdin, marshalCompact(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(stdin)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	previousIn, previousOut := os.Stdin, os.Stdout
	previousHost, previousEvent, previousCommand, previousEmitted := activeHost, activeEvent, command, emitted
	previousLoaded, previousCache := stdinLoaded, stdinCache
	defer func() {
		os.Stdin, os.Stdout = previousIn, previousOut
		activeHost, activeEvent, command, emitted = previousHost, previousEvent, previousCommand, previousEmitted
		stdinLoaded, stdinCache = previousLoaded, previousCache
	}()
	os.Stdin, os.Stdout = input, writer
	activeHost, command, emitted = host, "hook", false
	stdinLoaded, stdinCache = false, nil
	started := time.Now()
	finished := make(chan time.Duration, 1)
	go func() {
		runHook()
		finished <- time.Since(started)
	}()
	run := hostHookRun{answered: true}
	select {
	case run.took = <-finished:
	case <-time.After(within):
		run.answered = false
		run.held = getMap(getMap(readState(), "waits"), sid)
		clearWait(sid, nil)
		select {
		case run.took = <-finished:
		case <-time.After(time.Minute):
			t.Fatalf("the %s hook went on sleeping a minute after its wait was cleared", host)
		}
	}
	writer.Close()
	printed, _ := io.ReadAll(reader)
	if text := strings.TrimSpace(string(printed)); text != "" {
		if err := json.Unmarshal([]byte(text), &run.answer); err != nil {
			t.Fatalf("the %s hook printed something that is not JSON: %q", host, text)
		}
	}
	return run
}

func shortHookSandbox(t *testing.T, host string) (string, *[]recordedCommand) {
	t.Helper()
	dir := sandboxFiles(t)
	files.pluginRoot = repoRoot()
	t.Setenv("NOCTIS_NO_TASKS", "")
	t.Setenv("NOCTIS_NO_SCHEDULE", "")
	recorded := withFakeScheduler(t, nil)
	scheduleBackendOverride = "systemd"
	t.Cleanup(func() { scheduleBackendOverride = "" })
	source := "off"
	if host == "codex" {
		source = "codex"
	}
	if err := writeJSONAtomic(files.config, object{"host": host, "fable": object{"source": source}, "alarm": object{"enabled": false}}); err != nil {
		t.Fatal(err)
	}
	shipped := section(loadConfig(), "wait")
	if minutes, margin := numberOr(shipped, "maxInHookMinutes", 0), numberOr(shipped, "resetMarginSeconds", 0); minutes*60 < 7200+margin {
		t.Fatalf("the shipped config waits in the hook for up to %s min, too short for a pause two hours before the reset to reach the hook at all", formatNumber(minutes))
	}
	queue := filepath.Join(dir, "TASKS.md")
	if err := os.WriteFile(queue, []byte("# q\n- [ ] first item\n- [ ] second item\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	trustQueueFile(queue, true)
	now := float64(nowSec())
	if err := writeJSONAtomic(files.fable, object{"fetchedAt": now, "five_hour": object{"used": float64(95), "resetsAt": now + 7200}, "seven_day": object{"used": float64(40), "resetsAt": now + 5*86400}}); err != nil {
		t.Fatal(err)
	}
	return dir, recorded
}

func TestAPauseAtTheLimitAnswersLongBeforeAShortHookTimesOut(t *testing.T) {
	cases := []struct {
		name, host, event, kind string
		payload                 func(sid, dir string) object
		answer                  func(answer object, saved string) bool
	}{
		{
			"a Codex agent spawn", "codex", "PreToolUse", "batch",
			func(sid, dir string) object {
				return object{"hook_event_name": "PreToolUse", "session_id": sid, "cwd": dir, "model": "gpt-5.6", "permission_mode": "default", "tool_name": "spawn_agent", "tool_input": object{"message": "review the diff"}}
			},
			func(answer object, saved string) bool {
				specific := getMap(answer, "hookSpecificOutput")
				return getString(specific, "permissionDecision") == "deny" && getString(specific, "permissionDecisionReason") == saved
			},
		},
		{
			"a Codex stop with the queue open", "codex", "Stop", "stop",
			func(sid, dir string) object {
				return object{"hook_event_name": "Stop", "session_id": sid, "cwd": dir, "model": "gpt-5.6", "permission_mode": "default", "stop_hook_active": false, "last_assistant_message": "done with the first item"}
			},
			func(answer object, saved string) bool {
				return getString(answer, "systemMessage") == saved && answer["decision"] == nil
			},
		},
		{
			"an Antigravity subagent call", "antigravity", "PreToolUse", "batch",
			func(sid, dir string) object {
				return object{"hook_event_name": "PreToolUse", "conversationId": sid, "workspacePaths": []any{dir}, "modelName": "gemini-3.5-flash", "toolCall": object{"name": "invoke_subagent", "args": object{"task": "review the diff"}}, "stepIdx": float64(3)}
			},
			func(answer object, saved string) bool {
				return getString(answer, "decision") == "deny" && getString(answer, "reason") == saved
			},
		},
		{
			"an Antigravity stop with the queue open", "antigravity", "Stop", "stop",
			func(sid, dir string) object {
				return object{"hook_event_name": "Stop", "conversationId": sid, "workspacePaths": []any{dir}, "modelName": "gemini-3.5-flash", "executionNum": float64(1), "terminationReason": "model_stop", "error": "", "fullyIdle": true}
			},
			func(answer object, saved string) bool {
				return getString(answer, "decision") == "stop"
			},
		},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, recorded := shortHookSandbox(t, tc.host)
			sid := "short-hook-" + strconv.Itoa(index)
			run := runHostHook(t, tc.host, tc.payload(sid, dir), sid, 5*time.Second)
			if !run.answered {
				t.Fatalf("%s at 95 %% with the reset two hours away was still sleeping in its %s hook after 5s, and %s kills that hook at %ss with no answer; it had stored %v", tc.name, tc.event, tc.host, formatNumber(hookTimeoutsWired(t, tc.host)[tc.event]), run.held)
			}
			wait := getMap(getMap(readState(), "waits"), sid)
			saved := T("wait.saved", getString(wait, "label"), formatNumber(95), formatTime(numberOr(wait, "resumeAt", 0)), "")
			if !tc.answer(run.answer, saved) {
				t.Fatalf("%s at 95 %% answered %v in %s, want the pause told at once (%q)", tc.name, run.answer, run.took.Round(time.Millisecond), saved)
			}
			scheduled := getMap(wait, "scheduled")
			if getString(wait, "kind") != tc.kind || getBool(wait, "inHook", true) {
				t.Fatalf("%s left no %s wait for the runner outside the hook: %v", tc.name, tc.kind, wait)
			}
			if getString(scheduled, "method") != "systemd" || numberOr(scheduled, "at", 0) != numberOr(wait, "resumeAt", -1) {
				t.Fatalf("%s did not hand its wait to a runner at the resume time %v: %v", tc.name, wait["resumeAt"], scheduled)
			}
			armed := 0
			for _, entry := range *recorded {
				if strings.HasPrefix(strings.Join(entry.args, " "), "systemd-run ") && strings.Contains(strings.Join(entry.args, " "), " --unit="+systemdUnit(sid)+" ") {
					armed++
				}
			}
			if armed != 1 {
				t.Fatalf("%s armed the session's runner %d time(s), want once", tc.name, armed)
			}
		})
	}
}

func timeoutsWithin(value any) []float64 {
	found := []float64{}
	switch typed := value.(type) {
	case object:
		for key, inner := range typed {
			if number, ok := toNumber(inner); ok && (key == "timeout" || key == "timeoutSec") {
				found = append(found, number)
				continue
			}
			found = append(found, timeoutsWithin(inner)...)
		}
	case []any:
		for _, inner := range typed {
			found = append(found, timeoutsWithin(inner)...)
		}
	}
	return found
}

func hookTimeoutsWired(t *testing.T, host string) map[string]float64 {
	t.Helper()
	wired := map[string]float64{}
	if host == "claude" {
		for event, raw := range getMap(readJSON(filepath.Join(repoRoot(), "hooks", "hooks.json")), "hooks") {
			groups, _ := raw.([]any)
			for _, group := range groups {
				for _, entry := range getList(toObject(group), "hooks") {
					handler := toObject(entry)
					if arguments := getList(handler, "args"); len(arguments) == 0 || arguments[0] != "hook" {
						continue
					}
					if earlier, seen := wired[event]; seen && earlier != numberOr(handler, "timeout", -1) {
						t.Fatalf("hooks/hooks.json runs the hook for %s with two timeouts, %s and %s", event, formatNumber(earlier), formatNumber(numberOr(handler, "timeout", -1)))
					}
					wired[event] = numberOr(handler, "timeout", -1)
				}
			}
		}
		return wired
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("NOCTIS_ANTIGRAVITY_HOOKS", filepath.Join(dir, "antigravity-hooks.json"))
	written, err := wireHostHooks(host, filepath.Join(dir, "bin", pluginName), dir)
	if err != nil || len(written) == 0 {
		t.Fatalf("%s: noctis could not wire its hooks: %v", host, err)
	}
	root := readJSON(written[0].path)
	table := getMap(root, "hooks")
	switch host {
	case "droid":
		table = root
	case "antigravity":
		table = getMap(root, hookMarker)
	}
	for event, entries := range table {
		timeouts := timeoutsWithin(entries)
		if len(timeouts) != 1 {
			t.Fatalf("%s: the %s hook noctis wires carries %d timeouts (%v), want one", host, event, len(timeouts), timeouts)
		}
		wired[event] = timeouts[0]
	}
	if len(wired) == 0 {
		t.Fatalf("%s: no hook timeouts found in %s", host, written[0].path)
	}
	return wired
}

func TestAWaitSleepsInTheHookOnlyWhenTheHookItRunsInOutlastsIt(t *testing.T) {
	previousHost, previousEvent := activeHost, activeEvent
	t.Cleanup(func() { activeHost, activeEvent = previousHost, previousEvent })
	shipped := object{"maxInHookMinutes": float64(330)}
	longer := object{"maxInHookMinutes": float64(400)}
	cases := []struct {
		host, event string
		waitCfg     object
		learnedCap  float64
		remaining   float64
		inHook      bool
	}{
		{"claude", "PreToolUse", shipped, 0, 7200, true},
		{"codex", "PreToolUse", shipped, 0, 7200, false},
		{"codex", "PreToolUse", shipped, 0, 5, false},
		{"codex", "Stop", shipped, 0, 7200, false},
		{"codex", "Stop", shipped, 0, 20, false},
		{"codex", "PostToolUse", shipped, 0, 7200, true},
		{"codex", "UserPromptSubmit", shipped, 0, 7200, true},
		{"droid", "PreToolUse", shipped, 0, 7200, false},
		{"droid", "Stop", shipped, 0, 7200, false},
		{"droid", "PostToolUse", shipped, 0, 7200, true},
		{"antigravity", "PreToolUse", shipped, 0, 7200, false},
		{"antigravity", "Stop", shipped, 0, 7200, false},
		{"antigravity", "PreInvocation", shipped, 0, 7200, true},
		{"antigravity", "PostInvocation", shipped, 0, 7200, true},
		{"copilot", "preToolUse", shipped, 0, 7200, false},
		{"copilot", "PreToolUse", shipped, 0, 7200, false},
		{"copilot", "agentstop", shipped, 0, 7200, false},
		{"copilot", "errorOccurred", shipped, 0, 7200, false},
		{"copilot", "userPromptSubmitted", shipped, 0, 7200, true},
		{"claude", "Stop", shipped, 0, 19800, true},
		{"claude", "Stop", shipped, 0, 19801, false},
		{"claude", "Stop", longer, 0, 21570, true},
		{"claude", "Stop", longer, 0, 21590, false},
		{"claude", "Stop", shipped, 600, 540, true},
		{"claude", "Stop", shipped, 600, 7200, false},
		{"codex", "PostToolUse", shipped, 600, 7200, false},
		{"claude", "", longer, 0, 21590, true},
		{"codex", "", shipped, 0, 7200, true},
		{"codex", "Notification", shipped, 0, 7200, true},
	}
	for _, tc := range cases {
		activeHost, activeEvent = tc.host, tc.event
		if got := waitsInHook(tc.waitCfg, tc.learnedCap, tc.remaining); got != tc.inHook {
			budget, known := hookBudget(tc.host, tc.event)
			t.Errorf("%s %q (hook budget %ss, known %t, maxInHookMinutes %s, learned cap %s): a wait %ss away sleeps in the hook %t, want %t", tc.host, tc.event, formatNumber(budget), known, formatNumber(numberOr(tc.waitCfg, "maxInHookMinutes", 0)), formatNumber(tc.learnedCap), formatNumber(tc.remaining), got, tc.inHook)
		}
	}
}

func TestTheHookBudgetsAreTheTimeoutsNoctisWires(t *testing.T) {
	for _, host := range hostOrder {
		wired := hookTimeoutsWired(t, host)
		for event, timeout := range wired {
			if budget, known := hookBudget(host, event); !known || budget != timeout {
				t.Errorf("%s: noctis wires the %s hook with a %ss timeout, but hookBudgets holds %ss for it (known %t)", host, event, formatNumber(timeout), formatNumber(budget), known)
			}
		}
		for event := range hookBudgets[host] {
			if _, found := wired[event]; !found {
				t.Errorf("%s: hookBudgets holds a budget for %s, a hook noctis does not wire", host, event)
			}
		}
	}
	for host := range hookBudgets {
		if _, known := hostSpecs[host]; !known {
			t.Errorf("hookBudgets holds budgets for %q, a host noctis does not know", host)
		}
	}
}

func strandedSandbox(t *testing.T) (string, *[]recordedCommand) {
	t.Helper()
	dir := sandboxFiles(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	recorded := withFakeScheduler(t, nil)
	if err := writeJSONAtomic(files.config, object{"wait": object{"builtinGraceSeconds": float64(120), "workspaceGuard": false}, "wake": object{"graceSeconds": float64(240)}, "alarm": object{"enabled": false}}); err != nil {
		t.Fatal(err)
	}
	previous := activeEvent
	t.Cleanup(func() { activeEvent = previous })
	activeEvent = "UserPromptSubmit"
	return dir, recorded
}

func parkedWait(cwd string, resumeAt float64, scheduled object) object {
	wait := object{
		"kind": "batch", "window": "five_hour", "label": "5h", "used": float64(95), "threshold": float64(92), "hit": "threshold",
		"until": resumeAt, "resumeAt": resumeAt, "inHook": false, "startedAt": float64(nowSec() - 7200), "cwd": cwd, "queuedPrompt": "",
	}
	if scheduled != nil {
		wait["scheduled"] = scheduled
	}
	return wait
}

func storeWaits(waits map[string]object) {
	updateState(func(state object) {
		for sid, wait := range waits {
			stateMap(state, "waits")[sid] = wait
		}
	})
}

func storedWaitsJSON() map[string]string {
	stored := map[string]string{}
	for sid, wait := range getMap(readState(), "waits") {
		stored[sid] = string(marshalCompact(wait))
	}
	return stored
}

func journaledCount(sid, action string) int {
	count := 0
	for _, line := range tailFileLines(files.decisions, 1000) {
		var entry object
		if err := jsonUnmarshalObject([]byte(line), &entry); err == nil && getString(entry, "sid") == sid && getString(entry, "action") == action {
			count++
		}
	}
	return count
}

func writeLaunchdPlist(t *testing.T, label string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(launchdPlist(label)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launchdPlist(label), []byte("<plist/>\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTheRepairPassRearmsEveryWaitWhoseRunnerIsGone(t *testing.T) {
	dir, _ := strandedSandbox(t)
	now := float64(nowSec())
	gone := float64(deadPid(t))
	day := strconv.FormatInt(int64(now-86400), 10)
	missedLabel, startedLabel := launchdLabel("launchd-missed")+"."+day, launchdLabel("launchd-started")+"."+day
	writeLaunchdPlist(t, missedLabel)

	budget := parkedWait(dir, now+3600, object{"method": "sleeper", "pid": gone, "at": now + 3600})
	budget["window"], budget["label"], budget["hit"] = "seven_day", "Weekly", "budget"
	unknown := parkedWait(dir, now-1200, object{"method": "systemd", "unit": systemdUnit("unknown-retry"), "at": now - 1200})
	unknown["kind"], unknown["window"], unknown["label"] = "stopfailure", "unknown", "unknown"
	hookDied := parkedWait(dir, now+1200, nil)
	hookDied["inHook"], hookDied["startedAt"], hookDied["heartbeat"], hookDied["holder"] = true, now-700, now-600, strconv.Itoa(int(gone))+"-x"
	wakeDied := parkedWait(dir, now+900, nil)
	wakeDied["kind"], wakeDied["startedAt"], wakeDied["waking"], wakeDied["heartbeat"] = "stopfailure", now-700, now-690, now-600
	stranded := map[string]struct {
		wait object
		at   float64
	}{
		"no-runner":      {parkedWait(dir, now+1800, nil), now + 1800},
		"dead-sleeper":   {parkedWait(dir, now+3600, object{"method": "sleeper", "pid": gone, "at": now + 3600}), now + 3600},
		"budget-sleeper": {budget, now + 3600},
		"systemd-missed": {parkedWait(dir, now-3600, object{"method": "systemd", "unit": systemdUnit("systemd-missed"), "at": now - 3600}), 0},
		"launchd-missed": {parkedWait(dir, now-86400, object{"method": "launchd", "label": missedLabel, "at": now - 86400}), 0},
		"unknown-retry":  {unknown, 0},
		"hook-died":      {hookDied, now + 1200 + 120},
		"wake-died":      {wakeDied, now + 900 + 240},
	}

	sleeping := parkedWait(dir, now+600, object{"method": "sleeper", "pid": gone, "at": now + 600})
	sleeping["inHook"], sleeping["heartbeat"], sleeping["holder"] = true, now, strconv.Itoa(os.Getpid())+"-x"
	waking := parkedWait(dir, now+600, nil)
	waking["kind"], waking["startedAt"], waking["waking"] = "stopfailure", now-40, now-30
	justPaused := parkedWait(dir, now+600, nil)
	justPaused["startedAt"] = now
	youngSleeper := parkedWait(dir, now+600, object{"method": "sleeper", "pid": gone, "at": now + 600})
	youngSleeper["inHook"], youngSleeper["startedAt"], youngSleeper["heartbeat"], youngSleeper["holder"] = true, now-5, now-400, strconv.Itoa(int(gone))+"-y"
	kept := map[string]object{
		"young-sleeper":   youngSleeper,
		"systemd-ahead":   parkedWait(dir, now+3600, object{"method": "systemd", "unit": systemdUnit("systemd-ahead"), "at": now + 3600}),
		"handed-off":      parkedWait(dir, now-3600, object{"method": "systemd", "unit": systemdUnit("handed-off"), "at": now - 3600}),
		"hook-sleeping":   sleeping,
		"waking":          waking,
		"just-paused":     justPaused,
		"manual":          parkedWait(dir, now-3600, object{"method": "manual", "at": now - 3600}),
		"sleeper-running": parkedWait(dir, now-3600, object{"method": "sleeper", "pid": float64(os.Getpid()), "at": now - 3600}),
		"launchd-started": parkedWait(dir, now-86400, object{"method": "launchd", "label": startedLabel, "at": now - 86400}),
		"no-time":         parkedWait(dir, now-3600, object{"method": "systemd", "unit": systemdUnit("no-time")}),
	}
	all := map[string]object{}
	for sid, tc := range stranded {
		all[sid] = tc.wait
	}
	for sid, wait := range kept {
		all[sid] = wait
	}
	storeWaits(all)
	updateState(func(state object) {
		stateMap(state, "handedOff")["handed-off"] = object{"at": now, "model": "claude-opus-5", "mode": "window", "pid": float64(os.Getpid())}
	})
	before := storedWaitsJSON()

	repairOrphanWaits()

	waits := getMap(readState(), "waits")
	for sid, tc := range stranded {
		scheduled := getMap(getMap(waits, sid), "scheduled")
		at := numberOr(scheduled, "at", 0)
		if getString(scheduled, "method") != "manual" || (tc.at > 0 && at != tc.at) || (tc.at == 0 && at < now+15) {
			t.Errorf("%s: the wait whose runner is gone was left with %v, want a runner re-armed at %s", sid, scheduled, formatNumber(math.Max(tc.at, now+15)))
		}
		if entries := journaledCount(sid, "reschedule"); entries != 1 {
			t.Errorf("%s: the repair was journaled %d time(s) for noctis why, want once", sid, entries)
		}
	}
	after := storedWaitsJSON()
	for sid := range kept {
		if after[sid] != before[sid] {
			t.Errorf("%s: a wait whose runner is alive, due, handed off or held by its hook was changed from %s to %s", sid, before[sid], after[sid])
		}
		if entries := journaledCount(sid, "reschedule"); entries != 0 {
			t.Errorf("%s: a wait that still has its runner was journaled as rescheduled %d time(s)", sid, entries)
		}
	}

	repairOrphanWaits()
	for sid := range stranded {
		if entries := journaledCount(sid, "reschedule"); entries != 1 {
			t.Errorf("%s: the next pass re-armed the runner it had just armed (%d reschedules)", sid, entries)
		}
	}
}

func TestAnOverdueRunnerIsRearmedOnlyAFewTimesInARow(t *testing.T) {
	dir, recorded := strandedSandbox(t)
	t.Setenv("NOCTIS_NO_TASKS", "")
	t.Setenv("NOCTIS_NO_SCHEDULE", "")
	scheduleBackendOverride = "systemd"
	t.Cleanup(func() { scheduleBackendOverride = "" })
	sid := "late-timer"
	unit := systemdUnit(sid)
	now := float64(nowSec())
	storeWaits(map[string]object{sid: parkedWait(dir, now-3600, object{"method": "systemd", "unit": unit, "at": now - 3600})})
	armed := func() int {
		count := 0
		for _, entry := range *recorded {
			if command := strings.Join(entry.args, " "); strings.HasPrefix(command, "systemd-run ") && strings.Contains(command, " --unit="+unit+" ") {
				count++
			}
		}
		return count
	}
	for pass := 1; pass <= strandedRearmLimit+2; pass++ {
		repairOrphanWaits()
		want := pass
		if want > strandedRearmLimit {
			want = strandedRearmLimit
		}
		if armed() != want || journaledCount(sid, "reschedule") != want {
			t.Fatalf("pass %d over a timer that never fires armed the runner %d time(s) and journaled %d reschedule(s), want %d", pass, armed(), journaledCount(sid, "reschedule"), want)
		}
		scheduled := getMap(getMap(getMap(readState(), "waits"), sid), "scheduled")
		if pass <= strandedRearmLimit && (getString(scheduled, "method") != "systemd" || numberOr(scheduled, "at", 0) < now+15) {
			t.Fatalf("pass %d did not re-arm the session's timer ahead of now: %v", pass, scheduled)
		}
		updateState(func(state object) {
			getMap(getMap(getMap(state, "waits"), sid), "scheduled")["at"] = float64(nowSec() - 700)
		})
	}
}

func TestTheRepairPassActsOnlyOnWhatIsStillStored(t *testing.T) {
	dir, recorded := strandedSandbox(t)
	now := float64(nowSec())
	gone := float64(deadPid(t))
	wait := func(startedAt, resumeAt float64, scheduled object) object {
		record := parkedWait(dir, resumeAt, scheduled)
		record["startedAt"] = startedAt
		return record
	}
	seen := object{
		"waits": object{
			"cleared":  wait(now-7200, now-3600, object{"method": "systemd", "unit": systemdUnit("cleared"), "at": now - 3600}),
			"rearmed":  wait(now-7200, now+3600, object{"method": "sleeper", "pid": gone, "at": now + 3600}),
			"replaced": wait(now-7200, now+3600, object{"method": "sleeper", "pid": gone, "at": now + 3600}),
			"handed":   wait(now-7200, now-3600, object{"method": "systemd", "unit": systemdUnit("handed"), "at": now - 3600}),
		},
		"handedOff": object{},
	}
	storeWaits(map[string]object{
		"rearmed":  wait(now-7200, now+3600, object{"method": "sleeper", "pid": float64(os.Getpid()), "at": now + 3600}),
		"replaced": wait(now-60, now+3600, object{"method": "manual", "at": now + 3600}),
		"handed":   wait(now-7200, now-3600, object{"method": "systemd", "unit": systemdUnit("handed"), "at": now - 3600}),
	})
	updateState(func(state object) {
		stateMap(state, "handedOff")["handed"] = object{"at": now, "model": "claude-opus-5", "mode": "window", "pid": float64(os.Getpid())}
	})
	before := storedWaitsJSON()
	rescheduleStrandedWaits(seen)
	after := storedWaitsJSON()
	if len(after) != len(before) {
		t.Fatalf("the repair pass brought back a wait cleared since it looked: stored %v, was %v", after, before)
	}
	for sid, was := range before {
		if after[sid] != was {
			t.Errorf("%s: the repair pass acted on what it saw, not on what is stored: %s became %s", sid, was, after[sid])
		}
	}
	for _, sid := range []string{"cleared", "rearmed", "replaced", "handed"} {
		if entries := journaledCount(sid, "reschedule"); entries != 0 {
			t.Errorf("%s: journaled %d reschedule(s) for a wait that no longer needed one", sid, entries)
		}
	}
	if len(*recorded) != 0 {
		t.Errorf("the repair pass touched the scheduler for waits that no longer needed it: %v", *recorded)
	}
}

func TestOnlyASessionStartRearmsAWaitWhoseSleeperPidNowRunsAnotherProgram(t *testing.T) {
	sleep := sleepStandIn(t)
	if runtime.GOOS != "linux" {
		t.Skip("the stand-ins are named through /proc only on Linux")
	}
	dir, _ := strandedSandbox(t)
	helper, _ := startStandIn(t, standInNamed(t, sleep, pluginName))
	now := float64(nowSec())
	sleeperWait := func(pid int) object {
		return parkedWait(dir, now+3600, object{"method": "sleeper", "pid": float64(pid), "at": now + 3600})
	}
	stranger, strangerDone := startStandIn(t, sleep)
	storeWaits(map[string]object{
		"helper":   sleeperWait(helper.Process.Pid),
		"thread":   sleeperWait(aThreadOf(t, os.Getpid())),
		"stranger": sleeperWait(stranger.Process.Pid),
	})
	before := storedWaitsJSON()
	for _, event := range []string{"UserPromptSubmit", "PostToolBatch", "Stop", ""} {
		activeEvent = event
		repairOrphanWaits()
	}
	if after := storedWaitsJSON(); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("a hook or status line tick ran the process-name check it leaves to SessionStart: %v became %v", before, after)
	}
	for _, event := range []string{"SessionStart", "sessionStart"} {
		sid, done := "stranger", strangerDone
		if event != "SessionStart" {
			another, anotherDone := startStandIn(t, sleep)
			sid, done = "stranger-"+event, anotherDone
			storeWaits(map[string]object{sid: sleeperWait(another.Process.Pid)})
		}
		activeEvent = event
		repairOrphanWaits()
		scheduled := getMap(getMap(getMap(readState(), "waits"), sid), "scheduled")
		if getString(scheduled, "method") != "manual" || numberOr(scheduled, "at", 0) != now+3600 || journaledCount(sid, "reschedule") != 1 {
			t.Errorf("a %s hook left a wait whose sleeper pid now runs sleep with %v", event, scheduled)
		}
		select {
		case <-done:
			t.Errorf("re-arming the wait at %s killed the unrelated process that now holds its old sleeper pid", event)
		case <-time.After(300 * time.Millisecond):
		}
	}
	after := storedWaitsJSON()
	for _, sid := range []string{"helper", "thread"} {
		if after[sid] != before[sid] || journaledCount(sid, "reschedule") != 0 {
			t.Errorf("%s: a SessionStart re-armed a wait whose sleeper pid is ours or cannot be named: %s became %s", sid, before[sid], after[sid])
		}
	}
}

func TestAStaleWaitOutlivesItsResumeTimeOnlyWhileItsSleeperMayStillBeOurs(t *testing.T) {
	sleep := sleepStandIn(t)
	if runtime.GOOS != "linux" {
		t.Skip("the stand-ins are named through /proc only on Linux")
	}
	sandboxFiles(t)
	helper, _ := startStandIn(t, standInNamed(t, sleep, pluginName))
	stranger, strangerDone := startStandIn(t, sleep)
	now := nowSec()
	long := float64(now) - 5*86400
	cases := []struct {
		name string
		pid  int
		live bool
	}{
		{"our own sleeper, still running", helper.Process.Pid, true},
		{"a pid that cannot be named", aThreadOf(t, os.Getpid()), true},
		{"a pid that now runs another program", stranger.Process.Pid, false},
		{"a pid that is gone", deadPid(t), false},
	}
	state := object{"waits": object{}}
	for index, tc := range cases {
		wait := object{"resumeAt": long, "scheduled": object{"method": "sleeper", "pid": float64(tc.pid), "at": long}}
		if got := waitLive(wait, now); got != tc.live {
			t.Errorf("%s: a wait whose resume time passed five days ago is live %t, want %t", tc.name, got, tc.live)
		}
		getMap(state, "waits")[strconv.Itoa(index)] = wait
	}
	pruneState(state, now)
	drainPrunedRunners()
	for index, tc := range cases {
		if kept := getMap(getMap(state, "waits"), strconv.Itoa(index)) != nil; kept != tc.live {
			t.Errorf("%s: pruning kept the wait %t, want %t", tc.name, kept, tc.live)
		}
	}
	select {
	case <-strangerDone:
		t.Fatal("pruning the stale wait killed the unrelated process that now holds its old sleeper pid")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestAWaitThatCannotBeStoredLeavesTheRunnerOfTheWaitItWouldReplace(t *testing.T) {
	sleep := sleepStandIn(t)
	if runtime.GOOS != "linux" {
		t.Skip("the stand-in is named through /proc only on Linux")
	}
	dir := sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	helper, done := startStandIn(t, standInNamed(t, sleep, pluginName))
	sid := "kept-runner"
	now := float64(nowSec())
	storeWaits(map[string]object{sid: parkedWait(dir, now+3600, object{"method": "sleeper", "pid": float64(helper.Process.Pid), "at": now + 3600})})
	relaunch := func() object {
		return object{"kind": "fable", "window": "fable", "label": "Fable", "until": now, "resumeAt": now + 20, "inHook": false, "startedAt": now, "cwd": dir, "queuedPrompt": "", "scheduled": object{"method": "manual", "at": now + 20}}
	}
	lock := files.stateLock
	files.stateLock = filepath.Join(dir, "unreachable", "state.lock")
	if registerWait(sid, relaunch(), waitEngineConfig()) {
		t.Fatal("a wait that could not be stored was reported as stored")
	}
	select {
	case <-done:
		t.Fatal("the runner of the wait still stored was killed for a wait that never replaced it")
	case <-time.After(500 * time.Millisecond):
	}
	if stored := getMap(getMap(readState(), "waits"), sid); numberOr(getMap(stored, "scheduled"), "pid", 0) != float64(helper.Process.Pid) {
		t.Fatalf("the wait that stayed stored lost its runner: %v", stored)
	}
	files.stateLock = lock
	if !registerWait(sid, relaunch(), waitEngineConfig()) {
		t.Fatal("the relaunch wait was not stored")
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the runner of the wait the relaunch replaced was left running")
	}
	if stored := getMap(getMap(readState(), "waits"), sid); getString(stored, "kind") != "fable" {
		t.Fatalf("the relaunch wait is not the one stored: %v", stored)
	}
}

func TestReleasingASessionsInterruptedWaitDoesNotRearmItFirst(t *testing.T) {
	dir, _ := strandedSandbox(t)
	now := float64(nowSec())
	sid := "interrupted-own"
	wait := parkedWait(dir, now+1200, nil)
	wait["inHook"], wait["startedAt"], wait["heartbeat"], wait["holder"] = true, now-1000, now-400, strconv.Itoa(deadPid(t))+"-x"
	storeWaits(map[string]object{sid: wait})
	releaseInterruptedWait(sid, readState())
	if stored := getMap(getMap(readState(), "waits"), sid); stored != nil {
		t.Fatalf("the session's own interrupted in-hook wait was not released: %v", stored)
	}
	if entries := journaledCount(sid, "reschedule"); entries != 0 {
		t.Fatalf("the session's own interrupted wait was re-armed %d time(s) before it was released", entries)
	}
}

func TestAnOverdueRelaunchTaskIsRegisteredAgainOnlyAtASessionStart(t *testing.T) {
	dir, calls := windowsTaskSandbox(t)
	withFakeScheduler(t, nil)
	previous := activeEvent
	t.Cleanup(func() { activeEvent = previous })
	sid := "task-missed"
	name := taskName(sid)
	now := float64(nowSec())
	storeWaits(map[string]object{sid: parkedWait(dir, now-3600, object{"method": "task", "taskName": name, "at": now - 3600})})
	for _, event := range []string{"UserPromptSubmit", "PostToolBatch", ""} {
		activeEvent = event
		repairOrphanWaits()
	}
	if made := taskCalls(t, calls); len(made) != 0 {
		t.Fatalf("a hook or status line tick started powershell to re-register a task Windows starts when it can: %q", made)
	}
	activeEvent = "SessionStart"
	repairOrphanWaits()
	if made, want := taskCalls(t, calls), []string{"register " + name}; strings.Join(made, "|") != strings.Join(want, "|") {
		t.Fatalf("a session start over a task that never ran ran %q, want %q", made, want)
	}
	if scheduled := getMap(getMap(getMap(readState(), "waits"), sid), "scheduled"); getString(scheduled, "method") != "task" || numberOr(scheduled, "at", 0) < now+15 {
		t.Fatalf("the re-registered task is not recorded ahead of now: %v", scheduled)
	}
}

func TestTheRunnerLauncherSwitchesCmdToUTF8BeforeItNamesTheExecutable(t *testing.T) {
	sandboxFiles(t)
	files.runnerLauncher = filepath.Join(files.guardDir, "runner.cmd")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if launcher := ensureRunnerLauncher(); launcher != files.runnerLauncher {
		t.Fatalf("the task would start %q, not the launcher at %q", launcher, files.runnerLauncher)
	}
	written, err := os.ReadFile(files.runnerLauncher)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"@echo off",
		`@"%SystemRoot%\System32\chcp.com" 65001>nul`,
		`"` + executable + `" %*`,
		"exit /b %errorlevel%",
		"",
	}
	if got := string(written); got != strings.Join(want, "\r\n") {
		t.Fatalf("runner.cmd reads %q, want %q: cmd.exe decodes each line of a batch file in the console code page "+
			"current when it reads that line, a task's console starts in the OEM one (437, 850, 857, 866, 932 ...), "+
			"so every line before the executable's must be ASCII and the one right before it must switch to UTF-8, "+
			"or a profile such as C:\\Users\\Çağrı reads as C:\\Users\\├ça─ƒr─▒ and the resume never starts",
			strings.Split(got, "\r\n"), want)
	}
}

func TestAnUpdatedRunnerLauncherIsSwappedInNotRewrittenUnderATaskReadingIt(t *testing.T) {
	sandboxFiles(t)
	files.runnerLauncher = filepath.Join(files.guardDir, "runner.cmd")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	stale := "@echo off\r\n\"C:\\noctis\\old\\noctis.exe\" %*\r\nexit /b %errorlevel%\r\n"
	if err := os.WriteFile(files.runnerLauncher, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	reading, err := openShared(files.runnerLauncher)
	if err != nil {
		t.Fatal(err)
	}
	defer reading.Close()

	ensureRunnerLauncher()

	held, err := io.ReadAll(reading)
	if err != nil {
		t.Fatalf("a task reading runner.cmd could not read on once the launcher was updated: %v", err)
	}
	if string(held) != stale {
		t.Fatalf("a task reading runner.cmd got %q instead of the %q it opened: the update rewrote the file in place, "+
			"so a task that starts while another session schedules can read half a launcher", held, stale)
	}
	fresh, err := readFileShared(files.runnerLauncher)
	if err != nil || !strings.Contains(string(fresh), "\r\n\""+executable+"\" %*\r\n") {
		t.Fatalf("the updated launcher reads %q (%v), want one that starts %s", fresh, err, executable)
	}
	entries, err := os.ReadDir(files.guardDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("the update left %s behind", entry.Name())
		}
	}
}
