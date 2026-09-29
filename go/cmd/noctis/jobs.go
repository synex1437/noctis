package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// A job is a long command Claude starts with noctis job run instead of waiting on it in a tool call:
// a build, a full test suite, a migration, a download. It runs apart from the session, so neither
// the tool's time limit nor the end of the session cuts it off, and its output goes to a log. When
// Claude ends its turn while a job it started runs and a trusted queue drives the session, the Stop
// hook waits for the job as long as an in-hook wait may, then tells Claude how it ended with the end
// of its log, so no turn goes to polling it. A job that ends while nothing waits for it is reported
// at the session's next stop.

const (
	jobTailLines = 20
	jobTailBytes = 4096
	jobKeepDays  = 3
	jobStopGrace = 5 * time.Second
)

// jobPoll is how often a Stop hook that waits for a job looks at it again.
var jobPoll = 10 * time.Second

// jobWaitSeconds is how long a Stop hook may wait for a job.
var jobWaitSeconds = jobWaitLimit

var ansiEscapes = lazyRegexp(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// jobView is a job as noctis reports it.
type jobView struct {
	id, label, log string
	at, ended      float64
	exit           float64
	exited         bool // the job's wrapper recorded its exit status
	gone           bool // its process is gone without a record of how it ended
	stopped        bool // noctis job stop ended it
	running        bool
}

func jobsDir() string {
	return filepath.Join(files.guardDir, "jobs")
}

func runJob() {
	switch positional(1) {
	case "run":
		startJob()
	case "exec":
		execJob()
	case "", "list":
		printJobs()
	case "stop":
		stopJob()
	case "forget":
		forgetJobs()
	default:
		fmt.Fprintln(os.Stderr, T("job.usage"))
		os.Exit(2)
	}
}

// jobSession names the session whose tool runs noctis job run: Claude Code gives the commands it
// runs its session id in CLAUDE_CODE_SESSION_ID. A job started anywhere else belongs to no session
// and is only listed.
func jobSession() string {
	if sid := strings.TrimSpace(os.Getenv("CLAUDE_CODE_SESSION_ID")); sid != "" {
		return sessionKey(object{"session_id": sid})
	}
	return ""
}

// jobCommand is what a job runs: one word after -- is a command line for the shell (sh, or cmd.exe
// on Windows), several are a program and its arguments, run without a shell.
func jobCommand(words []string) *exec.Cmd {
	if len(words) == 1 {
		return platformShell(words[0])
	}
	return exec.Command(words[0], words[1:]...)
}

// startJob is noctis job run: it records the job, then starts noctis job exec detached, with the
// log as its output, to run the command and record how it ended.
func startJob() {
	line := strings.TrimSpace(strings.Join(args.rest, " "))
	if line == "" {
		fmt.Fprintln(os.Stderr, T("job.usage"))
		os.Exit(2)
	}
	folder, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, T("job.startFailed", err))
		os.Exit(1)
	}
	label := strings.TrimSpace(flagString("label"))
	if label == "" {
		label = line
	}
	label = truncateText(printableItem(label), 80)
	logPath := strings.TrimSpace(flagString("log"))
	if logPath != "" && !filepath.IsAbs(logPath) {
		logPath = filepath.Join(folder, logPath)
	}
	// A log named among noctis's own files, or reaching one of them through a link, would overwrite
	// it: the PreToolUse hook keeps Claude's writes off those files, and a job's log is one.
	if logPath != "" && ((isInside(filepath.Clean(logPath), files.guardDir) && !isInside(filepath.Clean(logPath), jobsDir())) || noctisOwnFile(logPath, folder) != "") {
		fmt.Fprintln(os.Stderr, T("job.logFailed", logPath, errors.New("noctis keeps its own files in that folder")))
		os.Exit(2)
	}
	sid := jobSession()
	now := float64(nowSec())
	id := ""
	updateState(func(next object) {
		number := numberOr(next, "jobSeq", 0) + 1
		next["jobSeq"] = number
		id = strconv.Itoa(int(number))
		if logPath == "" {
			logPath = filepath.Join(jobsDir(), id+".log")
		}
		stateMap(next, "jobs")[id] = object{"label": label, "command": truncateText(line, 500), "folder": folder, "log": logPath, "sid": sid, "at": now}
	})
	if id == "" {
		fmt.Fprintln(os.Stderr, T("job.startFailed", errors.New("state.json could not be written")))
		os.Exit(1)
	}
	forget := func() { updateState(func(next object) { delete(stateMap(next, "jobs"), id) }) }
	ensureDir(filepath.Dir(logPath))
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		forget()
		fmt.Fprintln(os.Stderr, T("job.logFailed", logPath, err))
		os.Exit(1)
	}
	defer logFile.Close()
	executable, err := os.Executable()
	if err != nil {
		forget()
		fmt.Fprintln(os.Stderr, T("job.startFailed", err))
		os.Exit(1)
	}
	wrapper := exec.Command(executable, append([]string{"job", "exec", "--id", id, "--"}, args.rest...)...)
	wrapper.Dir = folder
	wrapper.Env = withoutEnv(os.Environ(), statuslineChainEnv)
	wrapper.Stdin, wrapper.Stdout, wrapper.Stderr = nil, logFile, logFile
	configureDetached(wrapper)
	if err := wrapper.Start(); err != nil {
		forget()
		fmt.Fprintln(os.Stderr, T("job.startFailed", err))
		os.Exit(1)
	}
	pid := wrapper.Process.Pid
	started := processStarted(pid)
	_ = wrapper.Process.Release()
	updateState(func(next object) {
		if record := getMap(getMap(next, "jobs"), id); record != nil {
			record["pid"] = float64(pid)
			record["started"] = started
		}
	})
	dropStaleJobLogs()
	journal(sid, "cli", "job-started", label, object{"id": id, "pid": pid, "folder": folder})
	logInfo("job %s started for %s in %s (pid %d): %s", id, sid, folder, pid, truncateText(line, 200))
	fmt.Println(T("job.started", id, label, pid, logPath, pluginName))
}

// execJob is noctis job exec, the detached wrapper noctis job run starts: it runs the command with
// the log as its output and records how it ended.
func execJob() {
	id := flagString("id")
	if id == "" || len(args.rest) == 0 {
		os.Exit(2)
	}
	child := jobCommand(args.rest)
	child.Stdin, child.Stdout, child.Stderr = nil, os.Stdout, os.Stderr
	// On Unix the command gets a process group of its own, which noctis job stop ends as a whole.
	isolateTree(child)
	hideConsoleWindow(child)
	code := 127
	if err := child.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: the job could not start: %v\n", pluginName, err)
	} else {
		pid := child.Process.Pid
		updateState(func(next object) {
			if record := getMap(getMap(next, "jobs"), id); record != nil {
				record["child"] = float64(pid)
			}
		})
		code = exitStatus(child.Wait())
	}
	sid, label := "", ""
	updateState(func(next object) {
		if record := getMap(getMap(next, "jobs"), id); record != nil {
			record["exit"] = float64(code)
			record["ended"] = float64(nowSec())
			sid, label = getString(record, "sid"), getString(record, "label")
		}
	})
	journal(sid, "job", "job-ended", label, object{"id": id, "exit": code})
	logInfo("job %s ended with status %d", id, code)
}

// exitStatus is a finished command's exit status: -1 when a signal ended it.
func exitStatus(err error) int {
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.ExitCode()
	}
	return -1
}

// jobGone tells a job whose end noctis has no record of whether its wrapper is gone: killed, or the
// machine restarted. A pid that now names another process counts as gone.
func jobGone(record object, now int64) bool {
	pid := int(numberOr(record, "pid", 0))
	if pid == 0 {
		// Recorded by noctis job run but not started yet; a start that never came is gone after a
		// minute.
		return float64(now)-numberOr(record, "at", 0) > 60
	}
	if !processAlive(pid) {
		return true
	}
	started := getString(record, "started")
	return started != "" && processStarted(pid) != started
}

func jobOf(id string, record object, now int64) jobView {
	job := jobView{id: id, label: getString(record, "label"), log: getString(record, "log"), at: numberOr(record, "at", 0), ended: numberOr(record, "ended", 0), stopped: numberOr(record, "stopped", 0) > 0}
	job.exit, job.exited = getNumber(record, "exit")
	if job.ended == 0 {
		job.gone = jobGone(record, now)
		job.running = !job.gone
	}
	return job
}

func sortJobs(jobs []jobView) {
	sort.Slice(jobs, func(a, b int) bool {
		left, _ := strconv.Atoi(jobs[a].id)
		right, _ := strconv.Atoi(jobs[b].id)
		if left != right {
			return left < right
		}
		return jobs[a].id < jobs[b].id
	})
}

// jobSeconds is how long a job ran, or has run so far.
func jobSeconds(job jobView, now int64) float64 {
	end := job.ended
	if end == 0 {
		end = float64(now)
	}
	return math.Max(0, end-job.at)
}

// jobStatusText says how a job ended, or that it runs, for the user.
func jobStatusText(job jobView) string {
	switch {
	case job.running:
		return T("job.stateRunning")
	case job.stopped:
		return T("job.stateStopped")
	case job.gone || !job.exited:
		return T("job.stateGone")
	case job.exit < 0:
		return T("job.stateSignal")
	}
	return T("job.stateExit", int(job.exit))
}

// jobOutcome says how a job ended, for Claude.
func jobOutcome(job jobView) string {
	switch {
	case job.stopped:
		return fmt.Sprintf("was stopped with %s job stop", pluginName)
	case job.gone || !job.exited:
		return "is gone without a record of how it ended (it was killed, or the machine restarted)"
	case job.exit < 0:
		return "was ended by a signal"
	}
	return fmt.Sprintf("exited with status %d", int(job.exit))
}

func jobName(job jobView) string {
	return fmt.Sprintf(`%s job %s ("%s")`, pluginName, job.id, job.label)
}

func jobNames(jobs []jobView) string {
	names := []string{}
	for _, job := range jobs {
		names = append(names, jobName(job))
	}
	return strings.Join(names, ", ")
}

// jobSpan is a job's run time for Claude.
func jobSpan(seconds float64) string {
	return (time.Duration(math.Round(seconds)) * time.Second).String()
}

// jobLogTail is the end of a job's log as Claude reads it: its last lines, without terminal colors,
// and with a progress line that redraws itself in its last state.
func jobLogTail(path string) string {
	content := tailOfFile(path, jobTailBytes)
	if len(content) == 0 {
		return ""
	}
	text := ansiEscapes.ReplaceAllString(strings.ToValidUTF8(string(content), ""), "")
	lines := []string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r\t ")
		if cut := strings.LastIndexByte(line, '\r'); cut >= 0 {
			line = line[cut+1:]
		}
		lines = append(lines, strings.Map(func(r rune) rune {
			if r != '\t' && (unicode.IsControl(r) || unicode.Is(unicode.Cf, r)) {
				return -1
			}
			return r
		}, line))
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > jobTailLines {
		lines = lines[len(lines)-jobTailLines:]
	}
	return strings.Join(lines, "\n")
}

// jobLogNote names a job's log for Claude, with its last lines.
func jobLogNote(job jobView) string {
	if tail := jobLogTail(job.log); tail != "" {
		return fmt.Sprintf("The end of its log (%s):\n%s", job.log, tail)
	}
	return fmt.Sprintf("Its log (%s) is empty.", job.log)
}

// sessionJobs are the unreported jobs sid started, or the session it took over from started, split
// into the ones that ended and the ones that still run.
func sessionJobs(state object, sid string, now int64) (ended, running []jobView) {
	from := getString(getMap(getMap(state, "freshStarts"), sid), "from")
	for id, raw := range getMap(state, "jobs") {
		record, _ := raw.(object)
		owner := getString(record, "sid")
		if record == nil || owner == "" || (owner != sid && owner != from) || numberOr(record, "reported", 0) > 0 {
			continue
		}
		if job := jobOf(id, record, now); job.running {
			running = append(running, job)
		} else {
			ended = append(ended, job)
		}
	}
	sortJobs(ended)
	sortJobs(running)
	return ended, running
}

// jobWaitLimit is how long a Stop hook may wait for a job, in seconds: as long as an in-hook wait
// may (waitsInHook), and 0 in a hook the host gives too little time to wait in.
func jobWaitLimit(cfg, state object) float64 {
	limit := math.Max(1, numberOr(section(cfg, "wait"), "maxInHookMinutes", 0)) * 60
	if learned := numberOr(state, "hookCapSeconds", 0); learned > 0 {
		limit = math.Min(limit, math.Max(60, learned-60))
	}
	if budget, known := hookBudget(activeHost, activeEvent); known {
		if budget < minInHookBudgetSeconds {
			return 0
		}
		limit = math.Min(limit, budget-hookBudgetSlackSeconds)
	}
	return limit
}

// jobsAtStop answers a stop while jobs this session started are not yet reported: it tells Claude
// how the ones that ended did, and when a trusted queue drives the session and the others still
// run, it first waits for one of them to end. An interactive session is never held: its jobs are
// reported at the first stop after they end. It returns true when it answered the stop.
func jobsAtStop(cfg, state object, sid, queuePath string) bool {
	if len(getMap(state, "jobs")) == 0 {
		return false
	}
	ended, running := sessionJobs(state, sid, nowSec())
	if len(ended) == 0 && len(running) == 0 {
		return false
	}
	if len(ended) == 0 {
		limit := jobWaitSeconds(cfg, state)
		if limit <= 0 || queuePath == "" || !queueTrusted(cfg, queuePath) {
			return false
		}
		names := jobNames(running)
		if observed(sid, "Stop", "wait-job", names, nil) {
			return false
		}
		journal(sid, "Stop", "wait-job", truncateText(names, 200), object{"limit": limit})
		logInfo("stop of %s waits up to %s for %s", sid, jobSpan(limit), names)
		restoreCollector()
		deadline := time.Now().Add(time.Duration(limit * float64(time.Second)))
		for len(ended) == 0 && time.Now().Before(deadline) {
			time.Sleep(min(jobPoll, time.Until(deadline)))
			current := peekState()
			if numberOr(current, "disabledUntil", 0) > float64(nowSec()) {
				logInfo("noctis was paused while the stop of %s waited for its jobs; stop allowed", sid)
				return true
			}
			ended, running = sessionJobs(current, sid, nowSec())
			if len(ended) == 0 && len(running) == 0 {
				logInfo("the jobs the stop of %s waited for were forgotten; stop allowed", sid)
				return true
			}
		}
	}
	for _, job := range ended {
		if job.gone {
			// A wrapper records the end just before it exits: look again, so a job that ended
			// between the two looks is not taken for one that died.
			ended, running = sessionJobs(peekState(), sid, nowSec())
			break
		}
	}
	now := nowSec()
	if len(ended) == 0 {
		return stillRunningJobs(sid, running, now)
	}
	if observed(sid, "Stop", "job-report", jobNames(ended), nil) {
		return false
	}
	parts, notices := []string{}, []string{}
	for _, job := range ended {
		parts = append(parts, fmt.Sprintf("Your background job %s %s after %s. %s", jobName(job), jobOutcome(job), jobSpan(jobSeconds(job, now)), jobLogNote(job)))
		notices = append(notices, T("job.ended", pluginName, job.id, job.label, durationText(jobSeconds(job, now)), jobStatusText(job)))
	}
	reason := "[noctis] " + strings.Join(parts, "\n\n")
	if len(running) > 0 {
		reason += "\n\nStill running: " + jobNames(running) + "."
	}
	reason += "\n\nCheck the result, then go on with the work that waited for it; start a job again only to fix what made it fail."
	updateState(func(next object) {
		jobs := getMap(next, "jobs")
		for _, job := range ended {
			if record := getMap(jobs, job.id); record != nil {
				record["reported"] = float64(now)
			}
		}
		// Claude goes on on noctis's word, not the user's: see noteUserTurn.
		delete(stateMap(next, "userTurns"), sid)
	})
	journal(sid, "Stop", "job-report", truncateText(jobNames(ended), 200), nil)
	logInfo("reported %d ended job(s) to %s", len(ended), sid)
	emit(object{"decision": "block", "reason": reason, "systemMessage": joinNotices(notices...)})
	return true
}

// stillRunningJobs answers a stop whose wait for its jobs ran out: Claude hears what their logs
// show and decides whether to wait on.
func stillRunningJobs(sid string, running []jobView, now int64) bool {
	if observed(sid, "Stop", "job-running", jobNames(running), nil) {
		return false
	}
	parts, notices := []string{}, []string{}
	for _, job := range running {
		parts = append(parts, fmt.Sprintf("Your background job %s is still running after %s. %s", jobName(job), jobSpan(jobSeconds(job, now)), jobLogNote(job)))
		notices = append(notices, T("job.stillRunning", pluginName, job.id, job.label, durationText(jobSeconds(job, now))))
	}
	reason := "[noctis] " + strings.Join(parts, "\n\n") + fmt.Sprintf("\n\nIf it still makes progress, end your turn again and noctis waits on; if it hangs, stop it with %s job stop <id>, fix the cause and start it again.", pluginName)
	updateState(func(next object) { delete(stateMap(next, "userTurns"), sid) })
	journal(sid, "Stop", "job-running", truncateText(jobNames(running), 200), nil)
	logInfo("the wait of %s for %d job(s) ran out; Claude decides", sid, len(running))
	emit(object{"decision": "block", "reason": reason, "systemMessage": joinNotices(notices...)})
	return true
}

// jobHint tells Claude, where a stop can wait for a job, to run long commands as jobs.
func jobHint() string {
	if budget, known := hookBudget(currentHost().id, "Stop"); !known || budget < minInHookBudgetSeconds {
		return ""
	}
	return fmt.Sprintf(" Start a command that may outlast a tool call's time limit (a build, a full test suite, a migration, a download) with %[1]s job run --label <name> -- <command> instead of waiting on it in a tool call or polling it; go on with work that does not need its result, and when nothing else is left end your turn: %[1]s waits for the job and tells you how it ended. %[1]s job stop <id> stops one that hangs. Never start a server or a watcher this way.", pluginName)
}

func printJobs() {
	jobs := getMap(peekState(), "jobs")
	if len(jobs) == 0 {
		fmt.Println(T("job.none"))
		return
	}
	now := nowSec()
	views := []jobView{}
	for id, raw := range jobs {
		if record, _ := raw.(object); record != nil {
			views = append(views, jobOf(id, record, now))
		}
	}
	sortJobs(views)
	for _, job := range views {
		fmt.Println(T("job.line", job.id, jobStatusText(job), durationText(jobSeconds(job, now)), job.label, job.log))
	}
}

func jobReference() string {
	return strings.TrimPrefix(strings.TrimSpace(positional(2)), "#")
}

// stopJob is noctis job stop: it ends a running job's command with everything it started.
func stopJob() {
	id := jobReference()
	record := getMap(getMap(peekState(), "jobs"), id)
	if record == nil {
		fmt.Fprintln(os.Stderr, T("job.unknown", id, pluginName))
		os.Exit(1)
	}
	now := nowSec()
	job := jobOf(id, record, now)
	target := int(numberOr(record, "child", 0))
	if !processAlive(target) {
		target = int(numberOr(record, "pid", 0))
	}
	if !job.running || !processAlive(target) {
		fmt.Fprintln(os.Stderr, T("job.notRunning", id, job.label, jobStatusText(job)))
		os.Exit(1)
	}
	updateState(func(next object) {
		if current := getMap(getMap(next, "jobs"), id); current != nil {
			current["stopped"] = float64(now)
		}
	})
	if process, err := os.FindProcess(target); err == nil {
		killTree(process)
	}
	// The wrapper records the end as the command exits: wait a moment, so the answer is final.
	for deadline := time.Now().Add(jobStopGrace); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if numberOr(getMap(getMap(peekState(), "jobs"), id), "ended", 0) > 0 {
			break
		}
	}
	journal(getString(record, "sid"), "cli", "job-stopped", job.label, object{"id": id})
	logInfo("job %s stopped (pid %d)", id, target)
	fmt.Println(T("job.stopped", id, job.label))
}

// forgetJobs is noctis job forget: it drops a job's record, or with --all those of every job that
// ended. A job that runs goes on untracked; its log stays.
func forgetJobs() {
	id, all := jobReference(), args.present["all"]
	if id == "" && !all {
		fmt.Fprintln(os.Stderr, T("job.usage"))
		os.Exit(2)
	}
	now := nowSec()
	forgotten := 0
	updateState(func(next object) {
		jobs := stateMap(next, "jobs")
		for key, raw := range jobs {
			record, _ := raw.(object)
			if key == id || (all && id == "" && !jobOf(key, record, now).running) {
				delete(jobs, key)
				forgotten++
			}
		}
	})
	if id != "" && forgotten == 0 {
		fmt.Fprintln(os.Stderr, T("job.unknown", id, pluginName))
		os.Exit(1)
	}
	fmt.Println(T("job.forgotten", forgotten))
}

// pruneJobs drops the jobs reported or ended over three days ago, and any started over thirty days
// ago.
func pruneJobs(state object, now int64) {
	jobs := getMap(state, "jobs")
	for id, raw := range jobs {
		record, _ := raw.(object)
		last := math.Max(numberOr(record, "ended", 0), numberOr(record, "reported", 0))
		if record == nil || (last > 0 && float64(now)-last > jobKeepDays*86400) || float64(now)-numberOr(record, "at", 0) > 30*86400 {
			delete(jobs, id)
		}
	}
}

// dropStaleJobLogs removes the logs in noctis's jobs folder that no job names any more once they
// are three days old.
func dropStaleJobLogs() {
	entries, err := os.ReadDir(jobsDir())
	if err != nil {
		return
	}
	named := map[string]bool{}
	for _, raw := range getMap(peekState(), "jobs") {
		named[filepath.Clean(getString(toObject(raw), "log"))] = true
	}
	for _, entry := range entries {
		path := filepath.Join(jobsDir(), entry.Name())
		info, err := entry.Info()
		if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") || named[path] || time.Since(info.ModTime()) < jobKeepDays*24*time.Hour {
			continue
		}
		_ = os.Remove(path)
	}
}
