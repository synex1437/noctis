package main

import (
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type recordedCommand struct {
	name string
	args []string
}

func withFakeScheduler(t *testing.T, reply func(cmd *exec.Cmd) ([]byte, error)) *[]recordedCommand {
	t.Helper()
	recorded := &[]recordedCommand{}
	original := runScheduler
	runScheduler = func(command *exec.Cmd, _ time.Duration) ([]byte, error) {
		*recorded = append(*recorded, recordedCommand{name: command.Path, args: append([]string{}, command.Args...)})
		if reply == nil {
			return nil, nil
		}
		return reply(command)
	}
	t.Cleanup(func() { runScheduler = original })
	return recorded
}

func findCommand(recorded []recordedCommand, contains string) (recordedCommand, bool) {
	for _, entry := range recorded {
		if strings.Contains(strings.Join(entry.args, " "), contains) {
			return entry, true
		}
	}
	return recordedCommand{}, false
}

func TestScheduleAtMinuteRoundsUp(t *testing.T) {

	base := time.Date(2026, 3, 14, 14, 32, 0, 0, time.Local)
	cases := []struct {
		name   string
		offset time.Duration
		want   time.Time
	}{
		{"exactly on the minute stays put", 0, base},
		{"one second past rounds up", time.Second, base.Add(time.Minute)},
		{"45 seconds past rounds up", 45 * time.Second, base.Add(time.Minute)},
		{"59 seconds past rounds up", 59 * time.Second, base.Add(time.Minute)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := scheduleAtMinute(float64(base.Add(testCase.offset).Unix()))
			if !got.Equal(testCase.want) {
				t.Fatalf("scheduleAtMinute(%v) = %v, want %v", testCase.offset, got, testCase.want)
			}
			if got.Unix() < base.Add(testCase.offset).Unix() {
				t.Fatalf("rounded to %v, which is BEFORE the deadline %v", got, base.Add(testCase.offset))
			}
		})
	}
}

func TestLaunchdPlistBody(t *testing.T) {
	at := time.Date(2026, 3, 14, 9, 5, 30, 0, time.Local)
	plist := launchdPlistBody("com.synex.noctis.abc", "/opt/no ctis/bin/noctis",
		[]string{"resume", "--sid", "s1", "--account", "/home/a/.claude"}, float64(at.Unix()), "/home/a/.claude/noctis")

	for _, want := range []string{
		`<key>Label</key><string>com.synex.noctis.abc</string>`,
		`<string>/opt/no ctis/bin/noctis</string>`,
		`<string>resume</string><string>--sid</string><string>s1</string>`,
		`<key>RunAtLoad</key><false/>`,
		`<key>WorkingDirectory</key><string>/home/a/.claude/noctis</string>`,
	} {
		if !strings.Contains(plist, want) {
			t.Fatalf("plist is missing %q:\n%s", want, plist)
		}
	}

	if !strings.Contains(plist, `<key>Hour</key><integer>9</integer><key>Minute</key><integer>6</integer>`) {
		t.Fatalf("plist did not round the deadline up to the next minute:\n%s", plist)
	}

	assertWellFormedXML(t, plist)
}

func assertWellFormedXML(t *testing.T, document string) {
	t.Helper()
	decoder := xml.NewDecoder(strings.NewReader(document))
	decoder.Strict = true
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("plist does not parse as XML: %v\n%s", err, document)
		}
	}
}

func TestLaunchdPlistEscapesXML(t *testing.T) {

	plist := launchdPlistBody("label&<", "/opt/a&b/noctis", []string{`--prompt`, `say "hi" & <run>`}, float64(time.Now().Unix()), "/tmp/a<b")
	if strings.Contains(plist, "a&b") || strings.Contains(plist, `"hi"`) || strings.Contains(plist, "<run>") {
		t.Fatalf("unescaped characters survived into the plist:\n%s", plist)
	}
	for _, want := range []string{"a&amp;b", "&quot;hi&quot;", "&lt;run&gt;", "label&amp;&lt;"} {
		if !strings.Contains(plist, want) {
			t.Fatalf("expected %q in the escaped plist:\n%s", want, plist)
		}
	}
	assertWellFormedXML(t, plist)
}

func TestSystemdRunArgsUseACalendarTimer(t *testing.T) {
	at := time.Date(2026, 3, 14, 9, 5, 30, 0, time.Local)
	args := systemdRunArgs("noctis-abc", "/opt/noctis", []string{"resume", "--sid", "s1"}, float64(at.Unix()), false)
	joined := strings.Join(args, " ")

	if strings.Contains(joined, "--on-active") {
		t.Fatalf("a monotonic timer is back; it does not advance over suspend: %s", joined)
	}
	if !strings.Contains(joined, "--on-calendar=2026-03-14 09:05:30") {
		t.Fatalf("expected an absolute calendar deadline, got: %s", joined)
	}
	for _, want := range []string{"--user", "--unit=noctis-abc", "AccuracySec=1s"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in: %s", want, joined)
		}
	}

	executableAt, resumeAt := indexOf(args, "/opt/noctis"), indexOf(args, "resume")
	if executableAt < 0 || resumeAt != executableAt+1 {
		t.Fatalf("executable and its arguments are in the wrong order: %v", args)
	}
}

func TestSystemdWakeFlagIsOptional(t *testing.T) {
	at := float64(time.Now().Add(time.Hour).Unix())
	with := strings.Join(systemdRunArgs("u", "/bin/noctis", nil, at, true), " ")
	without := strings.Join(systemdRunArgs("u", "/bin/noctis", nil, at, false), " ")
	if !strings.Contains(with, "WakeSystem=true") {
		t.Fatalf("wake was asked for but not requested: %s", with)
	}
	if strings.Contains(without, "WakeSystem") {
		t.Fatalf("wake was not asked for but was requested anyway: %s", without)
	}
}

func TestSystemdRetriesWithoutWakeWhenRefused(t *testing.T) {

	attempts := 0
	recorded := withFakeScheduler(t, func(cmd *exec.Cmd) ([]byte, error) {
		if strings.Contains(strings.Join(cmd.Args, " "), "WakeSystem=true") {
			attempts++
			return nil, exec.ErrNotFound
		}
		return nil, nil
	})
	scheduled, ok := scheduleSystemd("s1", float64(time.Now().Add(time.Hour).Unix()), []string{"resume"}, true)
	if !ok {
		t.Fatalf("scheduling gave up instead of retrying without the wake alarm")
	}
	if attempts != 1 {
		t.Fatalf("expected exactly one refused wake attempt, got %d", attempts)
	}
	if getString(scheduled, "method") != "systemd" {
		t.Fatalf("scheduled record is wrong: %v", scheduled)
	}
	if _, found := findCommand(*recorded, "--on-calendar"); !found {
		t.Fatalf("no calendar timer was ever requested: %v", *recorded)
	}
}

func TestSystemdFailureIsReportedNotSwallowed(t *testing.T) {
	withFakeScheduler(t, func(cmd *exec.Cmd) ([]byte, error) {
		if strings.Contains(cmd.Path, "systemd-run") {
			return nil, exec.ErrNotFound
		}
		return nil, nil
	})
	if _, ok := scheduleSystemd("s1", float64(time.Now().Add(time.Hour).Unix()), []string{"resume"}, false); ok {
		t.Fatalf("a failed systemd-run reported success; the caller would never fall back to a sleeper")
	}
}

func TestSystemdStopsTheOldUnitBeforeScheduling(t *testing.T) {

	recorded := withFakeScheduler(t, nil)
	scheduleSystemd("s1", float64(time.Now().Add(time.Hour).Unix()), []string{"resume"}, false)
	if len(*recorded) < 2 {
		t.Fatalf("expected a stop before the schedule, got %v", *recorded)
	}
	if !strings.Contains(strings.Join((*recorded)[0].args, " "), "stop") {
		t.Fatalf("the first command was not a stop: %v", (*recorded)[0].args)
	}
	cancelSystemd("s1")
	if stop, found := findCommand(*recorded, "stop"); !found || !strings.Contains(strings.Join(stop.args, " "), ".timer") {
		t.Fatalf("cancel did not stop the timer unit: %v", *recorded)
	}
}

func indexOf(list []string, value string) int {
	for index, item := range list {
		if item == value {
			return index
		}
	}
	return -1
}

func sandboxFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	previous := files
	t.Cleanup(func() { files = previous })
	t.Setenv("NOCTIS_NO_WATCHER", "1")
	files.guardDir = dir
	files.configDir = dir
	files.state = filepath.Join(dir, "state.json")
	files.stateBackup = filepath.Join(dir, "state.json.bak")
	files.stateLock = filepath.Join(dir, "state.lock")
	files.log, files.errors = filepath.Join(dir, "guard.log"), filepath.Join(dir, "errors.log")
	files.usage = filepath.Join(dir, "usage.json")
	files.usageBackup = filepath.Join(dir, "usage.json.bak")
	files.usageLock = filepath.Join(dir, "usage.lock")
	files.fable = filepath.Join(dir, "fable.json")
	files.fableLock = filepath.Join(dir, "fable.lock")
	files.release = filepath.Join(dir, "release.json")
	files.decisions = filepath.Join(dir, "decisions.jsonl")
	files.checkpoints = filepath.Join(dir, "checkpoints")
	files.launches = filepath.Join(dir, "launches")
	files.settings = filepath.Join(dir, "settings.json")
	files.settingsLock = filepath.Join(dir, "settings.lock")
	files.config = filepath.Join(dir, "config.json")
	return dir
}

func TestScheduleRunnerRecordsTheMethodOnTheWait(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "")
	withFakeScheduler(t, nil)
	scheduleBackendOverride = "systemd"
	t.Cleanup(func() { scheduleBackendOverride = "" })

	at := float64(nowSec() + 3600)
	updateState(func(state object) {
		stateMap(state, "waits")["s1"] = object{"resumeAt": at, "kind": "batch"}
	})
	scheduled := scheduleRunner(object{}, "s1", at)

	if getString(scheduled, "method") != "systemd" {
		t.Fatalf("expected a systemd schedule, got %v", scheduled)
	}
	stored := getMap(getMap(getMap(readState(), "waits"), "s1"), "scheduled")
	if getString(stored, "method") != "systemd" {
		t.Fatalf("the schedule was not written onto the wait: %v", stored)
	}
	if numberOr(stored, "at", 0) < float64(nowSec()) {
		t.Fatalf("the recorded deadline is in the past: %v", stored)
	}
}

func TestScheduleRunnerFallsBackWhenTheBackendRefuses(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	withFakeScheduler(t, func(cmd *exec.Cmd) ([]byte, error) {
		if strings.Contains(cmd.Path, "systemd-run") {
			return nil, exec.ErrNotFound
		}
		return nil, nil
	})
	scheduleBackendOverride = "systemd"
	t.Cleanup(func() { scheduleBackendOverride = "" })

	at := float64(nowSec() + 3600)
	updateState(func(state object) { stateMap(state, "waits")["s1"] = object{"resumeAt": at} })
	scheduled := scheduleRunner(object{}, "s1", at)

	if getString(scheduled, "method") == "systemd" {
		t.Fatalf("a refused backend was recorded as if it had worked: %v", scheduled)
	}
	if getString(scheduled, "method") == "" {
		t.Fatalf("nothing was scheduled and nothing was recorded: %v", scheduled)
	}
}

func TestScheduleRunnerCancelsThePreviousTimer(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "")
	recorded := withFakeScheduler(t, nil)
	scheduleBackendOverride = "systemd"
	t.Cleanup(func() { scheduleBackendOverride = "" })

	at := float64(nowSec() + 3600)
	updateState(func(state object) { stateMap(state, "waits")["s1"] = object{"resumeAt": at} })
	scheduleRunner(object{}, "s1", at)
	before := len(*recorded)
	scheduleRunner(object{}, "s1", at+600)

	stops := 0
	for _, entry := range (*recorded)[before:] {
		if strings.Contains(strings.Join(entry.args, " "), "stop") {
			stops++
		}
	}
	if stops == 0 {
		t.Fatalf("the second schedule did not stop the first: %v", (*recorded)[before:])
	}
}

func TestSchedulingASessionDoesNotWaitForAnotherSessionsLock(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "")
	withFakeScheduler(t, nil)
	scheduleBackendOverride = "systemd"
	t.Cleanup(func() { scheduleBackendOverride = "" })

	busy := scheduleLockFile("a")
	if busy == scheduleLockFile("b") {
		t.Fatalf("two sessions share one schedule lock: %s", busy)
	}
	ensureDir(files.guardDir)
	if err := os.WriteFile(busy, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatalf("hold a's lock: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(busy) })

	at := float64(nowSec() + 3600)
	updateState(func(state object) { stateMap(state, "waits")["b"] = liveWait(3600) })
	started := time.Now()
	scheduled := scheduleRunner(object{}, "b", at)

	if waited := time.Since(started); waited > 2*time.Second {
		t.Fatalf("scheduling b waited %v while a's lock was held", waited)
	}
	if getString(scheduled, "method") != "systemd" {
		t.Fatalf("b was not scheduled while a's lock was held: %v", scheduled)
	}
	if statSafe(busy) == nil {
		t.Fatalf("a's lock was taken away from it")
	}
}

func TestAnAbandonedScheduleLockIsSweptAway(t *testing.T) {
	sandboxFiles(t)
	ensureDir(files.guardDir)
	abandoned := scheduleLockFile("gone")
	if err := os.WriteFile(abandoned, []byte("2147483646"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(abandoned, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	retired := filepath.Join(files.guardDir, "schedule.lock")
	if err := os.WriteFile(retired, []byte("2147483646"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chtimes(retired, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	mine := scheduleLockFile("live")
	if err := os.WriteFile(mine, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(mine) })

	sweepStaleLocks()

	if statSafe(abandoned) != nil {
		t.Fatalf("a schedule lock whose owner is gone was left behind")
	}
	if statSafe(retired) != nil {
		t.Fatalf("the shared schedule.lock this version stopped using was left behind on upgrade")
	}
	if statSafe(mine) == nil {
		t.Fatalf("a schedule lock a live process is holding was swept away")
	}
}

func sandboxLaunchd(t *testing.T, config object) (string, *[]recordedCommand) {
	t.Helper()
	files.pluginRoot = sandboxFiles(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("NOCTIS_NO_TASKS", "")
	t.Setenv("XPC_SERVICE_NAME", "")
	recorded := withFakeScheduler(t, nil)
	scheduleBackendOverride = "launchd"
	t.Cleanup(func() { scheduleBackendOverride = "" })
	previous := args
	t.Cleanup(func() { args = previous })
	mustWriteJSON(files.config, config)
	return filepath.Join(home, "Library", "LaunchAgents"), recorded
}

func firedByLaunchd(t *testing.T, sid, label string) {
	t.Helper()
	t.Setenv("XPC_SERVICE_NAME", label)
	args = parseArgs(runnerArgs("resume", sid, files.configDir))
}

func bootedOut(recorded []recordedCommand, agents, label string) bool {
	for _, entry := range recorded {
		joined := strings.Join(entry.args, " ")
		if strings.Contains(joined, " bootout ") && strings.HasSuffix(joined, "/"+label) {
			return true
		}
		if strings.Contains(joined, " unload ") && strings.HasSuffix(joined, filepath.Join(agents, label+".plist")) {
			return true
		}
	}
	return false
}

func TestLaunchdRunnerDoesNotBootOutItsOwnJob(t *testing.T) {
	agents, recorded := sandboxLaunchd(t, object{"resume": object{"mode": "none"}, "alarm": object{"enabled": false}})
	sid := "mac-fable"
	now := float64(nowSec())
	updateState(func(state object) {
		stateMap(state, "waits")[sid] = object{"kind": "fable", "window": "fable", "until": now, "resumeAt": now + 20, "startedAt": now}
	})
	label := getString(scheduleRunner(loadConfig(), sid, now+20), "label")
	plist := filepath.Join(agents, label+".plist")
	if label == "" || statSafe(plist) == nil {
		t.Fatalf("no launchd job was registered to fire: label %q", label)
	}

	firedByLaunchd(t, sid, label)
	before := len(*recorded)
	runResume()

	if bootedOut((*recorded)[before:], agents, label) {
		t.Fatalf("the runner launchd started booted its own job %s out; launchd answers that with SIGTERM before anything is resumed: %v", label, (*recorded)[before:])
	}
	if statSafe(plist) != nil {
		t.Fatalf("the fired job's plist is still in LaunchAgents; the next login would load it again")
	}
	if getMap(getMap(readState(), "waits"), sid) != nil {
		t.Fatalf("the runner never got as far as closing the wait")
	}
}

func TestLaunchdRunnerThatReschedulesRegistersANewJob(t *testing.T) {
	config := testConfig()
	config["resume"] = object{"mode": "none"}
	config["alarm"] = object{"enabled": false}
	agents, recorded := sandboxLaunchd(t, config)
	sid := "mac-five-hour"
	now := float64(nowSec())
	mustWriteJSON(files.usage, object{"updatedAt": now,
		"five_hour": object{"used": float64(96), "resetsAt": now + 3600},
		"seven_day": object{"used": float64(10), "resetsAt": now + 3*86400}})
	updateState(func(state object) {
		stateMap(state, "waits")[sid] = object{"kind": "batch", "window": "five_hour", "until": now, "resumeAt": now + 60, "startedAt": now}
	})
	fired := getString(scheduleRunner(loadConfig(), sid, now+60), "label")

	firedByLaunchd(t, sid, fired)
	before := len(*recorded)
	runResume()

	if bootedOut((*recorded)[before:], agents, fired) {
		t.Fatalf("the runner booted its own job %s out while rescheduling: %v", fired, (*recorded)[before:])
	}
	scheduled := getMap(getMap(getMap(readState(), "waits"), sid), "scheduled")
	next := getString(scheduled, "label")
	if getString(scheduled, "method") != "launchd" || next == "" || next == fired {
		t.Fatalf("the runner did not register a job of its own for the new deadline: %v (the fired job is %s)", scheduled, fired)
	}
	bootstrapped, found := findCommand((*recorded)[before:], "bootstrap")
	if !found || !strings.HasSuffix(strings.Join(bootstrapped.args, " "), filepath.Join(agents, next+".plist")) {
		t.Fatalf("the new job %s was not bootstrapped: %v", next, (*recorded)[before:])
	}
	if statSafe(filepath.Join(agents, fired+".plist")) != nil {
		t.Fatalf("the fired job's plist was left in LaunchAgents")
	}
	if statSafe(filepath.Join(agents, next+".plist")) == nil {
		t.Fatalf("the new job has no plist in LaunchAgents")
	}
}

func TestLaunchdRunnerWithoutAWaitDoesNotBootOutItsOwnJob(t *testing.T) {
	agents, recorded := sandboxLaunchd(t, object{})
	sid := "mac-done"
	now := float64(nowSec())
	updateState(func(state object) {
		stateMap(state, "waits")[sid] = object{"kind": "fable", "resumeAt": now + 20, "startedAt": now}
	})
	label := getString(scheduleRunner(loadConfig(), sid, now+20), "label")
	updateState(func(state object) { delete(stateMap(state, "waits"), sid) })

	firedByLaunchd(t, sid, label)
	before := len(*recorded)
	runResume()

	if bootedOut((*recorded)[before:], agents, label) {
		t.Fatalf("a runner that found no wait booted its own job %s out: %v", label, (*recorded)[before:])
	}
	if statSafe(filepath.Join(agents, label+".plist")) != nil {
		t.Fatalf("the fired job's plist is still in LaunchAgents")
	}
}

func TestCancellingALaunchdWaitBootsOutItsJob(t *testing.T) {
	agents, recorded := sandboxLaunchd(t, object{})
	sid := "mac-cancel"
	updateState(func(state object) { stateMap(state, "waits")[sid] = liveWait(3600) })
	label := getString(scheduleRunner(loadConfig(), sid, float64(nowSec()+3600)), "label")

	clearWait(sid, nil)

	if !bootedOut(*recorded, agents, label) {
		t.Fatalf("cancelling the wait left launchd job %s loaded: %v", label, *recorded)
	}
	if statSafe(filepath.Join(agents, label+".plist")) != nil {
		t.Fatalf("cancelling the wait left the plist behind")
	}
}

func TestLaunchdSchedulingRetiresEveryEarlierJobOfTheSession(t *testing.T) {
	agents, recorded := sandboxLaunchd(t, object{})
	sid := "mac-stale"
	ensureDir(agents)
	legacy, stale := launchdLabel(sid), launchdLabel(sid)+".1700000000"
	other := launchdLabel("another-session") + ".1700000000"
	for _, label := range []string{legacy, stale, other} {
		if err := os.WriteFile(filepath.Join(agents, label+".plist"), []byte("<plist/>"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	updateState(func(state object) { stateMap(state, "waits")[sid] = liveWait(3600) })

	fresh := getString(scheduleRunner(loadConfig(), sid, float64(nowSec()+3600)), "label")

	for _, label := range []string{legacy, stale} {
		if !bootedOut(*recorded, agents, label) || statSafe(filepath.Join(agents, label+".plist")) != nil {
			t.Fatalf("an earlier job of this session, %s, is still loaded next to the new one: %v", label, *recorded)
		}
	}
	if bootedOut(*recorded, agents, other) || statSafe(filepath.Join(agents, other+".plist")) == nil {
		t.Fatalf("another session's job was booted out")
	}
	if fresh == "" || statSafe(filepath.Join(agents, fresh+".plist")) == nil {
		t.Fatalf("the new job has no plist in LaunchAgents: %q", fresh)
	}
}

func TestCancellingNeverTouchesAJobThatIsNotThisSessions(t *testing.T) {
	agents, recorded := sandboxLaunchd(t, object{})
	home := filepath.Dir(filepath.Dir(agents))
	ensureDir(agents)
	foreign := filepath.Join(agents, "com.apple.Finder.plist")
	victim := filepath.Join(home, "victim.plist")
	for _, file := range []string{foreign, victim} {
		if err := os.WriteFile(file, []byte("<plist/>"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	for sid, label := range map[string]string{
		"mac-foreign":   "com.apple.Finder",
		"mac-traversal": launchdLabel("mac-traversal") + "./../../../victim",
	} {
		updateState(func(state object) {
			wait := liveWait(3600)
			wait["scheduled"] = object{"method": "launchd", "label": label}
			stateMap(state, "waits")[sid] = wait
		})
		clearWait(sid, nil)
	}

	if bootedOut(*recorded, agents, "com.apple.Finder") || statSafe(foreign) == nil {
		t.Fatalf("a label from state.json that is not this session's booted out another agent: %v", *recorded)
	}
	if statSafe(victim) == nil {
		t.Fatalf("a label from state.json reached a file outside LaunchAgents")
	}
	for _, entry := range *recorded {
		if strings.Contains(strings.Join(entry.args, " "), "..") {
			t.Fatalf("a label from state.json reached launchctl unchecked: %v", entry.args)
		}
	}
}

func TestLaunchdJobLabelNeverReusesTheRunningJobsLabel(t *testing.T) {
	sandboxFiles(t)
	at := float64(nowSec() + 600)
	t.Setenv("XPC_SERVICE_NAME", "")
	plain := launchdJobLabel("s1", at)
	if !launchdJobOf("s1", plain) || launchdJobOf("s2", plain) {
		t.Fatalf("label %q is not recognised as s1's job alone", plain)
	}

	t.Setenv("XPC_SERVICE_NAME", plain)
	if again := launchdJobLabel("s1", at); again == plain || !launchdJobOf("s1", again) {
		t.Fatalf("a job scheduled from inside job %s reused its label (%s); launchd refuses to bootstrap a label that is still loaded", plain, again)
	}
}
