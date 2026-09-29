package main

import (
	"bytes"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// jobLab runs this test binary as noctis for noctis job, so the wrapper a job run starts is a real
// detached process; the state the processes record is read here through files.
type jobLab struct {
	t       *testing.T
	env     []string
	project string
}

func newJobLab(t *testing.T, sid string) *jobLab {
	t.Helper()
	pluginRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil || !looksLikePluginRoot(pluginRoot) {
		t.Fatalf("the plugin root is not three levels above the package (%s): %v", pluginRoot, err)
	}
	root := t.TempDir()
	account := filepath.Join(root, "claude home")
	project := filepath.Join(root, "project")
	ensureDir(project)
	previous, previousArgs := files, args
	t.Cleanup(func() { files, args = previous, previousArgs })
	args = parseArgs(nil)
	files = pathsFor(account, pluginRoot)
	mustWriteJSON(files.config, object{"alarm": object{"enabled": false}, "update": object{"check": false}})
	lab := &jobLab{t: t, project: project}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "NOCTIS_") || strings.HasPrefix(key, "CLAUDE") || key == "HOME" || key == "USERPROFILE" {
			continue
		}
		lab.env = append(lab.env, entry)
	}
	lab.env = append(lab.env, "HOME="+root, "USERPROFILE="+root, "CLAUDE_CONFIG_DIR="+account, "NOCTIS_PLUGIN_ROOT="+pluginRoot,
		"NOCTIS_NO_TASKS=1", "NOCTIS_NO_SCHEDULE=1", "NOCTIS_NO_WATCHER=1", "NOCTIS_UPDATE_URL=off", "NOCTIS_LANG=en", testAsNoctis+"=1")
	if sid != "" {
		lab.env = append(lab.env, "CLAUDE_CODE_SESSION_ID="+sid)
	}
	// A job a failing test leaves running ends with the test, before its folder is removed.
	t.Cleanup(func() {
		for _, raw := range getMap(peekState(), "jobs") {
			for _, field := range []string{"child", "pid"} {
				if pid := int(numberOr(toObject(raw), field, 0)); processAlive(pid) {
					if process, err := os.FindProcess(pid); err == nil {
						killTree(process)
					}
				}
			}
		}
	})
	return lab
}

func (lab *jobLab) noctis(argv ...string) cliRun {
	lab.t.Helper()
	executable, err := os.Executable()
	if err != nil {
		lab.t.Fatal(err)
	}
	command := exec.Command(executable, argv...)
	command.Env, command.Dir = lab.env, lab.project
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	code := 0
	var exit *exec.ExitError
	if err := command.Run(); errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		lab.t.Fatalf("noctis %s did not run: %v", strings.Join(argv, " "), err)
	}
	return cliRun{code, stdout.String(), stderr.String()}
}

// waitFor waits until the job's record has field set, and returns the record.
func (lab *jobLab) waitFor(id, field string, within time.Duration) object {
	lab.t.Helper()
	deadline := time.Now().Add(within)
	for {
		record := getMap(getMap(peekState(), "jobs"), id)
		if numberOr(record, field, 0) != 0 {
			return record
		}
		if time.Now().After(deadline) {
			lab.t.Fatalf("job %s has no %s after %s: %v", id, field, within, record)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAJobRunsApartAndRecordsHowItEnded(t *testing.T) {
	lab := newJobLab(t, "jb-e2e")
	run := lab.noctis("job", "run", "--label", "greet", "--", "echo hello from the job && exit 3")
	if run.code != 0 || !strings.Contains(run.stdout, "job 1 started: greet (pid ") {
		t.Fatalf("noctis job run did not start the job:\n%s", run)
	}
	job := lab.waitFor("1", "ended", 30*time.Second)
	if numberOr(job, "exit", -99) != 3 || getString(job, "sid") != "jb-e2e" || !sameDir(getString(job, "folder"), lab.project) {
		t.Fatalf("the job's record is %v; want exit 3, session jb-e2e and folder %s", job, lab.project)
	}
	if !strings.HasPrefix(getString(job, "log"), filepath.Join(files.guardDir, "jobs")) {
		t.Fatalf("the job's log is %s, not in noctis's jobs folder", getString(job, "log"))
	}
	if log := string(cliRead(t, getString(job, "log"))); !strings.Contains(log, "hello from the job") {
		t.Fatalf("the job's log holds %q, not what the command printed", log)
	}
	if list := lab.noctis("job", "list"); !strings.Contains(list.stdout, "exit status 3") || !strings.Contains(list.stdout, "greet") {
		t.Fatalf("noctis job list does not show how the job ended:\n%s", list)
	}
	if forget := lab.noctis("job", "forget", "--all"); !strings.Contains(forget.stdout, "1 job(s) forgotten") {
		t.Fatalf("noctis job forget --all did not drop the ended job:\n%s", forget)
	}
	if list := lab.noctis("job", "list"); strings.TrimSpace(list.stdout) != "no jobs" {
		t.Fatalf("a forgotten job is still listed:\n%s", list)
	}
}

func TestAJobKeepsTheFlagsAfterTheDoubleDash(t *testing.T) {
	lab := newJobLab(t, "")
	argv := []string{"job", "run", "--", "sh", "-c", `printf '%s|' "$@"`, "job", "-h", "--help", "--version"}
	want := "-h|--help|--version|"
	if isWindows {
		argv, want = []string{"job", "run", "--", "cmd", "/c", "echo", "-h", "--help", "--version"}, "-h --help --version"
	}
	run := lab.noctis(argv...)
	if run.code != 0 || !strings.Contains(run.stdout, "job 1 started") {
		t.Fatalf("noctis read the job's own flags as its own:\n%s", run)
	}
	job := lab.waitFor("1", "ended", 30*time.Second)
	if getString(job, "sid") != "" {
		t.Fatalf("a job started outside a Claude Code session names session %q", getString(job, "sid"))
	}
	if log := string(cliRead(t, getString(job, "log"))); !strings.Contains(log, want) {
		t.Fatalf("the job's command did not get its flags: its log holds %q, want %q", log, want)
	}
}

func TestAJobStopEndsTheCommandAndWhatItStarted(t *testing.T) {
	lab := newJobLab(t, "js-e2e")
	run := lab.noctis("job", "run", "--label", "sleeper", "--", shellFor("sleep 60", "ping -n 60 127.0.0.1 > nul"))
	if run.code != 0 {
		t.Fatalf("noctis job run failed:\n%s", run)
	}
	child := int(numberOr(lab.waitFor("1", "child", 30*time.Second), "child", 0))
	if stop := lab.noctis("job", "stop", "1"); stop.code != 0 || !strings.Contains(stop.stdout, "job 1 (sleeper) stopped") {
		t.Fatalf("noctis job stop did not stop the job:\n%s", stop)
	}
	job := lab.waitFor("1", "ended", 30*time.Second)
	if numberOr(job, "stopped", 0) == 0 {
		t.Fatalf("the stopped job's record does not say it was stopped: %v", job)
	}
	if processAlive(child) {
		t.Fatalf("the job's command (pid %d) still runs after noctis job stop", child)
	}
	if list := lab.noctis("job", "list"); !strings.Contains(list.stdout, "stopped") {
		t.Fatalf("noctis job list does not show the job as stopped:\n%s", list)
	}
	if again := lab.noctis("job", "stop", "1"); again.code != 1 || !strings.Contains(again.stderr, "job 1 (sleeper) is not running: stopped") {
		t.Fatalf("stopping an ended job again did not say it is not running:\n%s", again)
	}
	if unknown := lab.noctis("job", "stop", "9"); unknown.code != 1 || !strings.Contains(unknown.stderr, "no job 9") {
		t.Fatalf("stopping a job that does not exist did not say so:\n%s", unknown)
	}
	if usage := lab.noctis("job", "run"); usage.code != 2 || !strings.Contains(usage.stderr, "noctis job run [--label <name>]") {
		t.Fatalf("noctis job run without a command did not print the usage:\n%s", usage)
	}
	before := string(cliRead(t, files.state))
	if clobber := lab.noctis("job", "run", "--log", files.state, "--", "echo overwritten"); clobber.code != 2 || !strings.Contains(clobber.stderr, "noctis keeps its own files") {
		t.Fatalf("a job's log was let overwrite noctis's state:\n%s", clobber)
	}
	if after := string(cliRead(t, files.state)); after != before {
		t.Fatal("a refused job still changed noctis's state")
	}
}

func TestADoubleDashEndsWhatNoctisReadsAsItsOwn(t *testing.T) {
	previous := args
	t.Cleanup(func() { args = previous })
	args = parseArgs([]string{"job", "run", "--label", "build", "--", "make", "-j4", "--help", "-h", "--", "x"})
	if strings.Join(args.positional, " ") != "job run" || flagString("label") != "build" || strings.Join(args.rest, " ") != "make -j4 --help -h -- x" {
		t.Fatalf("parsed %q, label %q, rest %q", args.positional, flagString("label"), args.rest)
	}
	if helpAsked() || args.present["help"] {
		t.Fatal("a --help or -h after -- was read as a request for noctis's help")
	}
	if rest := parseArgs([]string{"job", "run", "--"}).rest; len(rest) != 0 {
		t.Fatalf("a -- with nothing after it gave %q", rest)
	}
	if shell := jobCommand([]string{"echo one && echo two"}); !strings.Contains(strings.Join(shell.Args, " "), "echo one && echo two") || len(shell.Args) < 2 {
		t.Fatalf("a single word was not run as a command line: %q", shell.Args)
	}
	if direct := jobCommand([]string{"prog", "a b", "--flag"}); strings.Join(direct.Args, "|") != "prog|a b|--flag" {
		t.Fatalf("several words were not run as a program and its arguments: %q", direct.Args)
	}
}

func TestTheTrustGuardLooksIntoAJobsCommandLine(t *testing.T) {
	for _, command := range []string{
		`noctis job run -- "noctis queue trust TASKS.md"`,
		`noctis job run --label x -- noctis queue trust TASKS.md`,
		`noctis job run -- "sh -c 'noctis state-write'"`,
		`noctis job run -- "noctis job run -- 'noctis queue trust'"`,
		`noctis job run -- sh -c "noctis queue trust TASKS.md"`,
		`noctis job run -- bash -c 'noctis state-write'`,
		`noctis job run -- env FOO=1 noctis queue trust TASKS.md`,
		`noctis job run -- "cd sub && noctis queue trust TASKS.md"`,
	} {
		if deniedNoctisCommand(command, false) == nil {
			t.Errorf("%s is not refused, though its job trusts a queue or writes the state", command)
		}
	}
	for _, command := range []string{
		`noctis job run -- npm test`,
		`noctis job run --label build -- "npm run build && npm test"`,
		`noctis job list`,
	} {
		if target := deniedNoctisCommand(command, false); target != nil {
			t.Errorf("%s is refused as %s", command, target.logName)
		}
	}
}

// jobSandbox is a session a trusted TASKS.md drives, with jobs written into the state as the
// wrapper writes them. A stop waits for a job at most 20 seconds here, so a test whose job never
// ends fails instead of waiting the whole limit.
func jobSandbox(t *testing.T) (object, string) {
	t.Helper()
	cfg, project, _ := queueCheckSandbox(t, "")
	previousPoll, previousWait := jobPoll, jobWaitSeconds
	jobPoll = 20 * time.Millisecond
	jobWaitSeconds = func(cfg, state object) float64 { return math.Min(jobWaitLimit(cfg, state), 20) }
	t.Cleanup(func() { jobPoll, jobWaitSeconds = previousPoll, previousWait })
	return cfg, project
}

func addJob(t *testing.T, id string, record object) {
	t.Helper()
	updateState(func(next object) { stateMap(next, "jobs")[id] = record })
}

func jobLog(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "job.log")
	cliWrite(t, path, []byte(content))
	return path
}

func runningJob(sid, log string) object {
	return object{"label": "suite", "log": log, "sid": sid, "at": float64(nowSec()), "pid": float64(os.Getpid()), "started": processStarted(os.Getpid())}
}

func TestAJobThatEndedIsReportedAtTheNextStop(t *testing.T) {
	cfg, project := jobSandbox(t)
	log := jobLog(t, "\x1b[32mcompiling\x1b[0m\n10%\r50%\r100%\nBUILD OK\n\n")
	now := float64(nowSec())
	addJob(t, "1", object{"label": "build", "log": log, "sid": "jr1", "at": now - 125, "ended": now - 5, "exit": float64(0), "pid": float64(1)})
	addJob(t, "2", object{"label": "their build", "log": log, "sid": "someone-else", "at": now, "ended": now, "exit": float64(1)})
	output := stopHookOutput(t, stopInput("jr1", project), cfg)
	reason := getString(output, "reason")
	if getString(output, "decision") != "block" || !strings.Contains(reason, `noctis job 1 ("build") exited with status 0 after 2m0s`) {
		t.Fatalf("the ended job was not reported: %v", output)
	}
	if !strings.Contains(reason, "compiling\n100%\nBUILD OK") || strings.Contains(reason, "\x1b") || strings.Contains(reason, "50%") {
		t.Fatalf("the report does not show the log's last lines as they read: %q", reason)
	}
	if strings.Contains(reason, "their build") {
		t.Fatalf("another session's job was reported to this one: %q", reason)
	}
	if want := T("job.ended", pluginName, "1", "build", durationText(120), T("job.stateExit", 0)); !strings.Contains(getString(output, "systemMessage"), want) {
		t.Fatalf("the user did not hear %q: %v", want, output)
	}
	jobs := getMap(readState(), "jobs")
	if numberOr(getMap(jobs, "1"), "reported", 0) == 0 || numberOr(getMap(jobs, "2"), "reported", 0) != 0 {
		t.Fatalf("the reported marks are wrong: %v", jobs)
	}
	next := stopHookOutput(t, stopInput("jr1", project), cfg)
	if strings.Contains(getString(next, "reason"), "background job") || !strings.Contains(getString(next, "reason"), "migrate the users table") {
		t.Fatalf("the next stop did not go on with the queue: %v", next)
	}
}

func TestAStopWaitsForARunningJobWhenATrustedQueueDrivesTheSession(t *testing.T) {
	cfg, project := jobSandbox(t)
	addJob(t, "1", runningJob("jw1", jobLog(t, "ran 312 tests\n")))
	marked := make(chan struct{})
	go func() {
		defer close(marked)
		time.Sleep(300 * time.Millisecond)
		updateState(func(next object) {
			record := getMap(getMap(next, "jobs"), "1")
			record["exit"], record["ended"] = float64(3), float64(nowSec())
		})
	}()
	started := time.Now()
	output := stopHookOutput(t, stopInput("jw1", project), cfg)
	<-marked
	if waited := time.Since(started); waited < 250*time.Millisecond {
		t.Fatalf("the stop answered after %s, before the job ended", waited)
	}
	if reason := getString(output, "reason"); !strings.Contains(reason, `noctis job 1 ("suite") exited with status 3`) || !strings.Contains(reason, "ran 312 tests") {
		t.Fatalf("the stop did not report how the job it waited for ended: %v", output)
	}
	if !strings.Contains(journaledReason("jw1", "wait-job"), "noctis job 1") {
		t.Fatal("the wait for the job was not journaled")
	}
}

func TestAnInteractiveSessionIsNotHeldForItsJob(t *testing.T) {
	cfg, project := jobSandbox(t)
	trustQueueFile(filepath.Join(project, "TASKS.md"), false)
	addJob(t, "1", runningJob("ji1", jobLog(t, "")))
	started := time.Now()
	output := stopHookOutput(t, stopInput("ji1", project), cfg)
	if waited := time.Since(started); waited > 5*time.Second || strings.Contains(getString(output, "reason"), "background job") {
		t.Fatalf("a session no trusted queue drives was held for its job (%s): %v", waited, output)
	}
	updateState(func(next object) {
		record := getMap(getMap(next, "jobs"), "1")
		record["exit"], record["ended"] = float64(0), float64(nowSec())
	})
	output = stopHookOutput(t, stopInput("ji1", project), cfg)
	if reason := getString(output, "reason"); getString(output, "decision") != "block" || !strings.Contains(reason, "exited with status 0") || !strings.Contains(reason, "Its log (") {
		t.Fatalf("the job that ended was not reported at the next stop: %v", output)
	}
}

func TestAStopWhoseWaitRunsOutLetsClaudeDecide(t *testing.T) {
	cfg, project := jobSandbox(t)
	jobWaitSeconds = func(object, object) float64 { return 0.3 }
	addJob(t, "1", runningJob("jt1", jobLog(t, "step 41 of 90\n")))
	output := stopHookOutput(t, stopInput("jt1", project), cfg)
	reason := getString(output, "reason")
	if getString(output, "decision") != "block" || !strings.Contains(reason, `noctis job 1 ("suite") is still running after`) || !strings.Contains(reason, "step 41 of 90") || !strings.Contains(reason, "noctis job stop <id>") {
		t.Fatalf("the wait ran out without telling Claude what the job shows: %v", output)
	}
	if !strings.Contains(getString(output, "systemMessage"), "still runs after") {
		t.Fatalf("the user did not hear that the job still runs: %v", output)
	}
	if record := getMap(getMap(readState(), "jobs"), "1"); numberOr(record, "reported", 0) != 0 || numberOr(record, "ended", 0) != 0 {
		t.Fatalf("a job that still runs was marked: %v", record)
	}
}

func TestAJobWhoseProcessIsGoneIsReportedAsSuch(t *testing.T) {
	cfg, project := jobSandbox(t)
	finished := exec.Command(os.Args[0], "-test.run=^$")
	if err := finished.Run(); err != nil {
		t.Fatal(err)
	}
	record := runningJob("jg1", jobLog(t, "half way\n"))
	record["pid"] = float64(finished.ProcessState.Pid())
	addJob(t, "1", record)
	output := stopHookOutput(t, stopInput("jg1", project), cfg)
	if reason := getString(output, "reason"); !strings.Contains(reason, "is gone without a record of how it ended") || !strings.Contains(reason, "half way") {
		t.Fatalf("a job whose process died was not reported as gone: %v", output)
	}
	if !strings.Contains(getString(output, "systemMessage"), T("job.stateGone")) {
		t.Fatalf("the user did not hear the job is gone: %v", output)
	}
}

func TestAFreshSessionHearsOfTheJobsOfTheSessionItTookOverFrom(t *testing.T) {
	cfg, project := jobSandbox(t)
	now := float64(nowSec())
	updateState(func(next object) { stateMap(next, "freshStarts")["jf2"] = object{"from": "jf1", "at": now} })
	addJob(t, "1", object{"label": "migration", "log": jobLog(t, "done\n"), "sid": "jf1", "at": now - 60, "ended": now, "exit": float64(0)})
	output := stopHookOutput(t, stopInput("jf2", project), cfg)
	if !strings.Contains(getString(output, "reason"), `noctis job 1 ("migration") exited with status 0`) {
		t.Fatalf("the fresh session did not hear of the job the session it took over from started: %v", output)
	}
}

func TestPausingNoctisEndsAStopsWaitForAJob(t *testing.T) {
	cfg, project := jobSandbox(t)
	addJob(t, "1", runningJob("jp1", jobLog(t, "")))
	go func() {
		time.Sleep(200 * time.Millisecond)
		updateState(func(next object) { next["disabledUntil"] = float64(nowSec() + 3600) })
	}()
	started := time.Now()
	output := stopHookOutput(t, stopInput("jp1", project), cfg)
	if waited := time.Since(started); output != nil || waited > 10*time.Second {
		t.Fatalf("the wait for the job went on after noctis was paused (%s): %v", waited, output)
	}
}

func TestTheQueueDirectiveTellsClaudeToRunLongCommandsAsJobs(t *testing.T) {
	cfg, project := jobSandbox(t)
	start := object{"hook_event_name": "SessionStart", "source": "startup", "session_id": "jd1", "cwd": project}
	context := getString(getMap(hookOutput(t, onSessionStart, start, cfg), "hookSpecificOutput"), "additionalContext")
	if !strings.Contains(context, "noctis job run --label <name> -- <command>") || !strings.Contains(context, "noctis job stop <id>") {
		t.Fatalf("the queue directive does not tell Claude about noctis job: %q", context)
	}
	activeHost = "codex"
	if hint := jobHint(); hint != "" {
		t.Fatalf("a host whose stop cannot wait is told to run jobs: %q", hint)
	}
}

func TestOldJobsAndTheirLogsArePruned(t *testing.T) {
	_, _ = jobSandbox(t)
	now := nowSec()
	day := float64(86400)
	addJob(t, "1", object{"label": "old", "at": float64(now) - 5*day, "ended": float64(now) - 4*day, "exit": float64(0)})
	addJob(t, "2", object{"label": "recent", "at": float64(now) - 2*day, "ended": float64(now) - day, "exit": float64(0)})
	addJob(t, "3", object{"label": "ancient", "at": float64(now) - 31*day})
	jobs := getMap(readState(), "jobs")
	if jobs["1"] != nil || jobs["3"] != nil || jobs["2"] == nil {
		t.Fatalf("pruning kept or dropped the wrong jobs: %v", jobs)
	}
	stale, kept := filepath.Join(jobsDir(), "7.log"), filepath.Join(jobsDir(), "8.log")
	cliWrite(t, stale, []byte("old output\n"))
	cliWrite(t, kept, []byte("new output\n"))
	old := time.Now().Add(-4 * 24 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	dropStaleJobLogs()
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("a log no job names was kept after three days: %v", err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("a fresh log was removed: %v", err)
	}
}
