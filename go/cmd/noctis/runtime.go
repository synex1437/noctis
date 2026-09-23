package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

var (
	windowsQuoteNeeded = lazyRegexp(`[\s"&|<>^%!()=,;]`)
	trailingBackslash  = lazyRegexp(`(\\+)$`)
	backslashQuote     = lazyRegexp(`(\\*)"`)
	permissionAuto     = lazyRegexp(`permission-mode[\s\S]{0,600}?["' ]auto["',)\s]`)
	promptScrub        = lazyRegexp(`["%^]`)
	whitespaceRun      = lazyRegexp(`\s+`)
	scriptExtension    = lazyRegexp(`(?i)\.(exe|cmd)$`)
)

func weeklyEta(usage usageView, cfg object, now int64) (float64, bool) {
	history := getList(getMap(readJSON(files.usage), "history"), "seven_day")
	win := usage.sevenDay
	if win == nil || history == nil {
		return 0, false
	}
	samples := historySamples(history, win.resetsAt+usage.clockOffset, nil)
	if len(samples) < 2 {
		return 0, false
	}
	first, last := samples[0], samples[len(samples)-1]
	span := numberOr(last, "at", 0) - numberOr(first, "at", 0)
	rise := numberOr(last, "used", 0) - numberOr(first, "used", 0)
	if span < etaMinSpanSeconds || rise <= 0 {
		return 0, false
	}
	remaining := thresholdOf(cfg, "weeklyAll") - win.used
	if remaining <= 0 {
		return 0, false
	}
	eta := remaining / (rise / span)
	if eta < win.resetsAt-float64(now) {
		return eta, true
	}
	return 0, false
}

func hooksLookDead(state, usageFile object, now int64) bool {
	samples := getList(getMap(usageFile, "history"), "five_hour")
	lastHook := numberOr(state, "lastHookAt", 0)
	if lastHook == 0 {

		oldest := float64(0)
		for _, raw := range samples {
			if at := numberOr(toObject(raw), "at", 0); at > 0 && (oldest == 0 || at < oldest) {
				oldest = at
			}
		}
		if oldest == 0 || float64(now)-oldest < hooksDeadAfterSeconds {
			return false
		}
		return statuslineStillReporting(len(samples), usageFile, now)
	}
	if float64(now)-lastHook < hooksDeadAfterSeconds {
		return false
	}
	since := 0
	for _, raw := range samples {
		if numberOr(toObject(raw), "at", 0) > lastHook {
			since++
		}
	}
	return statuslineStillReporting(since, usageFile, now)
}

func statuslineStillReporting(samples int, usageFile object, now int64) bool {
	return samples >= hooksDeadMinStatuslines &&
		float64(now)-numberOr(usageFile, "updatedAt", 0) < statuslineRecentSeconds
}

func paceMarker(cfg object, win *window, now int64) string {
	if win == nil || !getBool(section(cfg, "statusline"), "pace", true) {
		return ""
	}
	remaining := win.resetsAt - float64(now)
	if remaining <= 0 || remaining > 7*86400 {
		return ""
	}
	elapsed := 1 - remaining/(7*86400)
	expected := elapsed * thresholdOf(cfg, "weeklyAll")
	switch {
	case win.used > expected+5:
		return "▼"
	case win.used < expected-5:
		return "▲"
	}
	return "●"
}

func usageBadgeAt(usage usageView, cfg object, now int64) string {
	parts := []string{}
	mark := func(win *window, threshold float64) string {
		if win.used >= threshold-warnBand {
			return "⚠"
		}
		return ""
	}
	if usage.fiveHour != nil {
		parts = append(parts, fmt.Sprintf("%s%s %%%d→%s", mark(usage.fiveHour, thresholdOf(cfg, "session5h")), T("win.five"), int(math.Round(usage.fiveHour.used)), formatTime(usage.fiveHour.resetsAt)))
	}
	if usage.sevenDay != nil {
		parts = append(parts, fmt.Sprintf("%s%s %%%d%s→%s", mark(usage.sevenDay, thresholdOf(cfg, "weeklyAll")), T("badge.week"), int(math.Round(usage.sevenDay.used)), paceMarker(cfg, usage.sevenDay, now), formatTime(usage.sevenDay.resetsAt)))
	}
	if usage.fable != nil {
		parts = append(parts, fmt.Sprintf("%s %%%d", scopedLabel(cfg), int(math.Round(usage.fable.used))))
	}
	return strings.Join(parts, " · ")
}

func healHooksCommand() {
	hooks := readJSONStrict(files.hooks)
	groupsByEvent := getMap(hooks.data, "hooks")
	if !hooks.ok || groupsByEvent == nil {
		return
	}
	executable := forwardSlashes(healTargetBinary())
	if executable == "" {
		return
	}
	changed := false
	for _, raw := range groupsByEvent {
		groups, _ := raw.([]any)
		for _, rawGroup := range groups {
			for _, rawHook := range getList(toObject(rawGroup), "hooks") {
				hook := toObject(rawHook)
				if hook == nil || getString(hook, "type") != "command" || getList(hook, "args") == nil {
					continue
				}
				current := getString(hook, "command")
				if strings.Contains(current, "${CLAUDE_PLUGIN_ROOT}") || current == executable || statSafe(current) != nil {
					continue
				}
				hook["command"] = executable
				changed = true
			}
		}
	}
	if !changed {
		return
	}
	mustWriteJSON(files.hooks, hooks.data)
	warn("hooks.json pointed at a missing binary; self-healed to %s (takes effect after /reload-plugins)", executable)
}

func readUsageForUpdate() object {
	primary := readJSONStrict(files.usage)
	if primary.ok && !primary.exists {
		return object{}
	}
	if primary.ok && primary.data != nil {
		return primary.data
	}

	backup := readJSONStrict(files.usageBackup)
	recovered := backup.ok && backup.exists && backup.data != nil
	verdictText := "starting fresh"
	if recovered {
		verdictText = "recovered from backup"
	}
	warn("usage.json corrupt (%s); %s", primary.err, verdictText)
	if recovered {
		return backup.data
	}
	return object{}
}

func recordStatusline(input object, now int64, multiSessionMax bool) (string, bool) {
	sid, heal := "", false
	reporter := ""
	if getString(input, "session_id") != "" {
		reporter = sessionKey(input)
	}
	withFileLock(files.usageLock, func() {
		previous := readUsageForUpdate()
		next := cloneObject(previous)
		version := getString(input, "version")
		if version == "" {
			version = orDefault(getString(previous, "version"), fallbackClaudeVersion)
		}
		next["version"] = version
		next["sessions"] = pruneSessions(getMap(previous, "sessions"), now)
		history := getMap(previous, "history")
		if history == nil {
			history = object{}
		}
		next["history"] = history
		limits := getMap(input, "rate_limits")
		offered, stored := 0, 0
		unusable := []string{}
		for _, key := range []string{"five_hour", "seven_day"} {
			win := getMap(limits, key)
			if win == nil {
				continue
			}
			offered++
			rawUsed, okUsed := windowPercent(win)
			resetsAt, okReset := resetEpoch(win["resets_at"])
			if !okUsed || !okReset {
				continue
			}
			used, sane := sanePercent(rawUsed)
			if !sane || !saneResetTime(resetsAt, now) {
				unusable = append(unusable, fmt.Sprintf("%s used=%v resets_at=%v", key, rawUsed, win["resets_at"]))
				continue
			}
			origin, reportedAt := reporter, float64(now)
			if previousWin := getMap(previous, key); multiSessionMax && previousWin != nil && getString(previousWin, "sid") != "" && getString(previousWin, "sid") != reporter && numberOr(previousWin, "resetsAt", -1) == resetsAt && numberOr(previousWin, "used", 0) > used {
				used = numberOr(previousWin, "used", 0)
				origin = getString(previousWin, "sid")
				reportedAt = numberOr(previousWin, "at", 0)
			}
			next[key] = object{"used": used, "resetsAt": resetsAt, "sid": origin, "at": reportedAt}
			appendHistory(history, key, used, resetsAt, now)
			stored++
		}
		if stored > 0 {
			next["updatedAt"] = float64(now)
		}
		if unread := offered - stored; unread > 0 && float64(now)-numberOr(getMap(readState(), "notified"), "statuslineShape", 0) > 86400 {
			updateState(func(state object) { stateMap(state, "notified")["statuslineShape"] = float64(now) })
			detail := ""
			if len(unusable) > 0 {
				detail = " (" + strings.Join(unusable, "; ") + ")"
			}
			warn("the status line payload has %d usage window(s) this version cannot read%s; those windows come from the API instead — the plugin may need an update", unread, detail)
		}
		if getString(input, "session_id") != "" {
			sid = sessionKey(input)
			sessions := getMap(next, "sessions")
			info := object{"model": getString(getMap(input, "model"), "id"), "cwd": getString(input, "cwd"), "transcript": getString(input, "transcript_path"), "updatedAt": float64(now)}
			if context, ok := getNumber(getMap(input, "context_window"), "used_percentage"); ok {
				info["context"] = context
			} else {
				info["context"] = nil
			}
			sessions[sid] = info
		}
		heal = float64(now)-numberOr(previous, "selfHealAt", 0) > selfHealIntervalSeconds
		if heal {
			next["selfHealAt"] = float64(now)
		}
		if len(previous) > 0 {
			if current := readJSONStrict(files.usage); current.ok && current.exists {
				if err := os.WriteFile(files.usageBackup, current.raw, 0o600); err != nil {
					warn("usage.json backup not written: %v", err)
				}
			}
		}
		mustWriteJSON(files.usage, next)
	})
	return sid, heal
}

func runStatusline() {
	input := readStdinJSON()
	now := nowSec()
	cfg := loadConfig()
	input = normalizeStatuslineInput(activeHost, input, now)
	sid, heal := recordStatusline(input, now, getBool(section(cfg, "usage"), "multiSessionMax", true))
	if heal {
		healHooksCommand()
	}
	triggerEarlyResumes(cfg)
	repairOrphanWaits()
	sweepStaleLocks()
	usage := currentUsage(now)
	recordBudgetDay(usage, now)
	state := readState()
	applySessionLocale(cfg, state, sid)
	statuslineCfg := section(cfg, "statusline")
	chainOutput := ""
	if chain := getString(statuslineCfg, "chainCommand"); chain != "" {
		chainOutput = runChain(chain, marshalCompact(input))
	}
	if getString(statuslineCfg, "mode") == "silent" {
		if chainOutput != "" {
			fmt.Println(chainOutput)
		}
		return
	}
	modelInfo := getMap(input, "model")
	model := orDefault(getString(modelInfo, "display_name"), getString(modelInfo, "id"))
	effort := ""
	if level := getString(getMap(input, "effort"), "level"); level != "" {
		effort = "/" + level
	}
	context := ""
	if percent, ok := getNumber(getMap(input, "context_window"), "used_percentage"); ok {
		context = T("statusline.ctx", int(math.Round(percent)))
	}
	marker := "∞"
	if !getBool(statuslineCfg, "emoji", true) {
		marker = "NOCTIS"
	}
	waitText := ""
	if sid != "" {
		if handoff := getMap(getMap(state, "handedOff"), sid); handoff != nil && !isHandoffSession(sid) {

			waitText = T("statusline.moved")
		} else if wait := getMap(getMap(state, "waits"), sid); waitLive(wait, now) {
			waitText = T("statusline.paused", formatTime(numberOr(wait, "resumeAt", 0)))
		}
	}
	etaText := ""
	if eta, ok := weeklyEta(usage, cfg, now); ok {
		etaText += T("statusline.weekEta", durationText(eta))
	}
	if usage.fable != nil && scopedModelPattern(cfg).MatchString(model) {
		fableHistory := getList(getMap(readJSON(files.fable), "history"), "fable")
		if eta, ok := etaSeconds(fableHistory, usage.fable, scopedThreshold(cfg), now, usage.clockOffset); ok {
			etaText += T("statusline.scopedEta", scopedLabel(cfg), durationText(eta))
		}
	}
	alert := ""
	if hooksLookDead(state, readJSON(files.usage), now) {
		alert = T("statusline.hooksDead")
		if float64(now)-numberOr(getMap(state, "notified"), "hooksDead", 0) > selfCheckIntervalSec {
			updateState(func(next object) { stateMap(next, "notified")["hooksDead"] = float64(now) })
			fail("statusline keeps updating but no hook has run for 30+ minutes; hooks appear inactive — run /reload-plugins or check hooks.json")
		}
	}
	if observing {
		alert += T("statusline.observe")
	}
	badge := orDefault(usageBadgeAt(usage, cfg, now), T("statusline.waiting"))
	line := fmt.Sprintf("%s %s%s · %s%s%s%s%s", marker, badge, etaText, model, effort, context, waitText, alert)
	if chainOutput != "" {
		fmt.Printf("%s\n%s\n", chainOutput, line)
		return
	}
	fmt.Println(line)
}

func runChain(chain string, input []byte) string {
	var command *exec.Cmd
	if isWindows {
		command = exec.Command("cmd.exe", "/d", "/s", "/c", chain)
		command.Env = append(os.Environ(), "NoDefaultCurrentDirectoryInExePath=1")
	} else {
		command = exec.Command("sh", "-c", chain)
	}
	command.Stdin = strings.NewReader(string(input))
	output, err := runWithTimeout(command, 4*time.Second)
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(output), "\r\n\t ")
}

func locateExecutable(name string) string {
	found, err := exec.LookPath(name)
	if err == nil && filepath.IsAbs(found) {
		return found
	}
	if found != "" {
		return lookPathOutsideWorkingDir(name)
	}
	return probeExecutable(name)
}

func lookPathOutsideWorkingDir(name string) string {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if !filepath.IsAbs(dir) {
			continue
		}
		if found, err := exec.LookPath(filepath.Join(dir, name)); err == nil && outsideWorkingDir(found) {
			return found
		}
	}
	return ""
}

func outsideWorkingDir(file string) bool {
	if !filepath.IsAbs(file) {
		return false
	}
	dir := filepath.Dir(file)
	cwd, cwdErr := os.Getwd()
	here, hereErr := os.Stat(".")
	there, thereErr := os.Stat(dir)
	if cwdErr != nil || hereErr != nil || thereErr != nil {
		return false
	}
	if relative, err := filepath.Rel(cwd, dir); err == nil && relative == "." {
		return false
	}
	return !os.SameFile(here, there)
}

func probeExecutable(name string) string {
	finder := "which"
	if isWindows {
		finder = "where.exe"
	}
	output, _ := runWithTimeout(exec.Command(finder, name), 5*time.Second)
	lines := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && outsideWorkingDir(trimmed) {
			lines = append(lines, trimmed)
		}
	}
	for _, line := range lines {
		if scriptExtension.MatchString(line) {
			return line
		}
	}
	if len(lines) > 0 {
		return lines[0]
	}
	return ""
}

func claudeExecutable() string {
	if found := locateExecutable("claude"); found != "" {
		return found
	}
	var candidates []string
	if isWindows {
		candidates = []string{filepath.Join(os.Getenv("APPDATA"), "npm", "claude.cmd"), filepath.Join(homeDir(), ".local", "bin", "claude.exe"), filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "claude", "claude.exe")}
	} else {
		candidates = []string{filepath.Join(homeDir(), ".local", "bin", "claude"), "/usr/local/bin/claude", "/opt/homebrew/bin/claude"}
	}
	for _, candidate := range candidates {
		if filepath.IsAbs(candidate) && statSafe(candidate) != nil {
			return candidate
		}
	}
	return ""
}

func windowsQuote(arg string) string {
	if !windowsQuoteNeeded.MatchString(arg) {
		return arg
	}
	escaped := backslashQuote.ReplaceAllString(arg, `$1$1\"`)
	escaped = trailingBackslash.ReplaceAllString(escaped, "$1$1")
	return `"` + escaped + `"`
}

func claudeCommand(claudePath string, claudeArgs []string) *exec.Cmd {
	if isWindows && strings.HasSuffix(strings.ToLower(claudePath), ".cmd") {
		parts := []string{windowsQuote(claudePath)}
		for _, arg := range claudeArgs {
			parts = append(parts, windowsQuote(arg))
		}
		return exec.Command("cmd.exe", "/d", "/s", "/c", strings.Join(parts, " "))
	}
	return exec.Command(claudePath, claudeArgs...)
}

func inGuardDir(command *exec.Cmd) *exec.Cmd {
	ensureDir(files.guardDir)
	command.Dir = files.guardDir
	return command
}

func supportedPermissionMode(cfg object, claudePath, inherited string) string {
	requested := orDefault(getString(section(cfg, "resume"), "permissionMode"), "acceptEdits")
	if requested == "inherit" {
		requested = orDefault(inherited, "acceptEdits")
	}
	if refusedPermModes[requested] {
		warn("permission mode %q is not used for unattended relaunches; falling back to acceptEdits", requested)
		return "acceptEdits"
	}
	if !knownPermModes[requested] {
		return "acceptEdits"
	}
	if requested != "auto" {
		return requested
	}
	output, _ := runWithTimeout(inGuardDir(claudeCommand(claudePath, []string{"--help"})), 20*time.Second)
	if permissionAuto.Match(output) {
		return "auto"
	}
	return "acceptEdits"
}

func promptDigest(text string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(whitespaceRun.ReplaceAllString(text, " "))))
	return hex.EncodeToString(sum[:8])
}

func sanitizePrompt(text string) string {
	value := whitespaceRun.ReplaceAllString(promptScrub.ReplaceAllString(text, "'"), " ")
	value = strings.TrimSpace(value)
	if runes := []rune(value); len(runes) > 4000 {
		return string(runes[:4000])
	}
	return value
}

type launchSpec struct {
	sid            string
	model          string
	prompt         string
	cwd            string
	mode           string
	permissionMode string
	configDir      string
}

func launchClaude(cfg object, launch launchSpec) bool {
	resume := section(cfg, "resume")
	mode := orDefault(launch.mode, getString(resume, "mode"))
	host := currentHost()
	claudePath := hostExecutable(host.id)
	if claudePath == "" {
		notify(cfg, pluginName, T("launch.noClaude"))
		fail("launch aborted: %s executable not found", host.exe)
		return false
	}
	if host.id != "claude" {

		return launchHostSession(cfg, host, claudePath, launch)
	}
	if launch.cwd == "" || statSafe(launch.cwd) == nil {
		notify(cfg, pluginName, T("launch.noCwd", launch.cwd))
		fail("launch aborted: cwd missing %s", launch.cwd)
		return false
	}
	permissionMode := supportedPermissionMode(cfg, claudePath, launch.permissionMode)
	effort := getString(section(cfg, "models"), "effort")
	claudeArgs := hostLaunchArgs("claude", cfg, launch, effort, permissionMode)
	env := relaunchEnv(launch, effort)
	logInfo("launching claude (%s) model=%s mode=%s cwd=%s", mode, launch.model, permissionMode, launch.cwd)
	closePreviousLaunch(cfg, launch.sid, getMap(getMap(readState(), "waits"), launch.sid))
	if mode == "window" {
		if isWindows {
			return launchInWindowsTerminal(cfg, launch, claudePath, claudeArgs, env, effort)
		}
		if launchInDesktopTerminal(cfg, launch, claudePath, claudeArgs, effort) {
			return true
		}
	}
	if info := statSafe(files.resumeLog); info != nil && info.Size() > logMaxBytes {
		_ = renameAtomic(files.resumeLog, files.resumeLog+".1")
	}
	logFile, err := os.OpenFile(files.resumeLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		fail("resume log unavailable: %v", err)
		return false
	}
	defer logFile.Close()
	command := claudeCommand(claudePath, append([]string{"-p"}, claudeArgs...))
	command.Env = env
	command.Dir = launch.cwd
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Run(); err != nil {
		warn("headless session ended with status %v", err)
		_, isExit := err.(*exec.ExitError)
		return isExit
	}
	logInfo("headless session ended normally")
	return true
}

func launchHostSession(cfg object, host hostSpec, exe string, launch launchSpec) bool {
	if launch.cwd == "" || statSafe(launch.cwd) == nil {
		notify(cfg, pluginName, T("launch.noCwd", launch.cwd))
		fail("launch aborted: cwd missing %s", launch.cwd)
		return false
	}
	arguments := hostLaunchArgs(host.id, cfg, launch, "", "")
	if info := statSafe(files.resumeLog); info != nil && info.Size() > logMaxBytes {
		_ = renameAtomic(files.resumeLog, files.resumeLog+".1")
	}
	logFile, err := os.OpenFile(files.resumeLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		fail("resume log unavailable: %v", err)
		return false
	}
	defer logFile.Close()
	logInfo("launching %s (headless) sid=%s cwd=%s", host.exe, launch.sid, launch.cwd)
	command := exec.Command(exe, arguments...)
	command.Env = hostRelaunchEnv(host, launch)
	command.Dir = launch.cwd
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Run(); err != nil {
		warn("%s session ended with status %v", host.exe, err)
		_, isExit := err.(*exec.ExitError)
		return isExit
	}
	logInfo("%s session ended normally", host.exe)
	return true
}

func sessionActiveAfter(wait object, epoch float64) bool {
	info := statSafe(getString(wait, "transcript"))
	return info != nil && float64(info.ModTime().UnixMilli())/1000 > epoch
}

func sessionContinuedAfter(wait object, epoch float64) bool {
	lines, ok := tailLines(getString(wait, "transcript"), transcriptTailBytes)
	parsedAny := false
	for i := len(lines) - 1; ok && i >= 0; i-- {
		entry, parsed := parseTranscriptLine(lines[i])
		if !parsed {
			continue
		}
		parsedAny = true
		at, err := time.Parse(time.RFC3339Nano, entry.timestamp)
		if err != nil || float64(at.UnixMilli())/1000 <= epoch || entry.sidechain || entry.isMeta || entry.isCompact {
			continue
		}
		switch entry.entryType {
		case "assistant":
			if !entry.apiError {
				return true
			}
		case "user":
			if !carriesToolResult(entry.content) && strings.TrimSpace(joinText(entry.content)) != "" {
				return true
			}
		}
	}
	if !parsedAny {
		return sessionActiveAfter(wait, epoch+pauseSettleSeconds)
	}
	return false
}

func carriesToolResult(content []any) bool {
	for _, raw := range content {
		if getString(toObject(raw), "type") == "tool_result" {
			return true
		}
	}
	return false
}

func waitContinued(wait object) bool {
	until := numberOr(wait, "until", 0)
	switch kind := getString(wait, "kind"); {
	case kind == "fable":
		return false
	case kind == "stopfailure" && until <= numberOr(wait, "startedAt", 0)+1:
		return sessionContinuedAfter(wait, until)
	}
	return sessionActiveAfter(wait, until)
}

func mergeInto(target, source object) {
	for key, value := range source {
		target[key] = value
	}
}

func readyNotice(wait object, release string, now int64, tail string) string {
	if release != "reset" && dataPause(wait) && float64(now) < numberOr(wait, "until", 0) {
		return T("wait.dataReady", getString(wait, "label"), tail)
	}
	return T("wait.ready", getString(wait, "label"), tail)
}

func runResume() {
	resumeWait(flagString("sid"), flagString("release"))
}

func liveRunner(handoff object) int {
	pid := int(numberOr(handoff, "pid", 0))
	if pid == os.Getpid() || !processAlive(pid) {
		return 0
	}
	return pid
}

func handoffHeldFor(state object, sid string, startedAt float64) int {
	if runner := liveRunner(getMap(getMap(state, "handedOff"), sid)); runner > 0 && !handoffWatchesEarlierWindow(state, sid, startedAt) {
		return runner
	}
	return 0
}

func leaveSessionToItsRunner(sid string) {
	journal(sid, "resume", "skip-launch", "another runner is already resuming this session", nil)
	logInfo("runner %s: another runner already holds the session; not launching twice", sid)
}

func rescheduleOwnWait(sid string, startedAt float64, change func(record object)) bool {
	stored := false
	updateState(func(next object) {
		if record := getMap(getMap(next, "waits"), sid); record != nil && numberOr(record, "startedAt", -1) == startedAt {
			change(record)
			stored = true
		}
	})
	return stored
}

func clearOwnWait(sid string, startedAt float64) bool {
	state := readState()
	if numberOr(getMap(getMap(state, "waits"), sid), "startedAt", -1) != startedAt {
		return false
	}
	cancelRunner(sid, state)
	removed := false
	updateState(func(next object) {
		if numberOr(getMap(getMap(next, "waits"), sid), "startedAt", -1) == startedAt {
			delete(stateMap(next, "waits"), sid)
			removed = true
		}
	})
	return removed
}

func leaveReplacedWait(sid string) {
	journal(sid, "resume", "skip-launch", "the wait was replaced while this runner worked on it", nil)
	logInfo("runner %s: the wait was replaced while this runner worked on it; its own runner resumes it", sid)
}

func releaseHandoff(sid string, startedAt float64) {
	updateState(func(next object) {
		if int(numberOr(getMap(getMap(next, "handedOff"), sid), "pid", 0)) == os.Getpid() {
			delete(stateMap(next, "handedOff"), sid)
		}
		if record := getMap(getMap(next, "waits"), sid); record != nil && numberOr(record, "startedAt", -1) == startedAt {
			delete(stateMap(next, "waits"), sid)
		}
	})
}

func resumeWait(sid, release string) {
	if sid == "" {
		fmt.Fprintln(os.Stderr, "usage: noctis resume --sid <session-id> [--account <config-dir>]")
		return
	}
	cfg := loadConfig()
	now := nowSec()
	state := readState()
	wait := getMap(getMap(state, "waits"), sid)
	if isWindows {
		removeScheduledTask(taskName(sid))
	}
	if wait == nil {

		cancelLaunchdJobs(sid)
		logInfo("runner %s: no wait record (already completed or cancelled)", sid)
		return
	}
	if getString(getMap(wait, "scheduled"), "method") == "launchd" {
		cancelLaunchdJobs(sid)
	}
	auto := getMap(getMap(state, "autoResume"), sid)
	if getString(auto, "type") == "quota_auto_resume_fired" && numberOr(auto, "at", 0) >= numberOr(wait, "startedAt", 0) {
		clearWaitAndConsume(sid, state)
		logInfo("runner %s: builtin auto-continue already resumed the session", sid)
		return
	}
	waitCfg := section(cfg, "wait")
	if hookSleeping(wait) {
		sleepUntil(float64(nowSec())+math.Max(1, numberOr(waitCfg, "heartbeatGraceSeconds", 45)), nil)
		refreshed := getMap(getMap(readState(), "waits"), sid)
		if refreshed == nil {
			logInfo("runner %s: waiting hook finished during grace period", sid)
			return
		}
		if numberOr(refreshed, "startedAt", -1) != numberOr(wait, "startedAt", -2) {
			logInfo("runner %s: the wait was replaced during the grace period; its own runner resumes it", sid)
			return
		}
		mergeInto(wait, refreshed)
	}
	startedAt := numberOr(wait, "startedAt", 0)
	if hookSleeping(wait) && float64(nowSec())-numberOr(wait, "heartbeat", 0) < heartbeatFreshSeconds {
		checks := int(numberOr(wait, "aliveChecks", 0)) + 1
		if checks <= aliveHookMaxChecks {
			retryAt := float64(now + 300)
			if !rescheduleOwnWait(sid, startedAt, func(record object) {
				record["aliveChecks"] = float64(checks)
				record["resumeAt"] = retryAt
			}) {
				leaveReplacedWait(sid)
				return
			}
			scheduleRunner(cfg, sid, retryAt)
			logInfo("runner %s: waiting hook still alive (heartbeat), re-check %d at %s", sid, checks, localISO(retryAt))
			return
		}
		warn("runner %s: hook heartbeat still fresh after %d checks, treating as stuck", sid, checks-1)
	}
	if handoffHeldFor(readState(), sid, startedAt) > 0 {
		leaveSessionToItsRunner(sid)
		return
	}
	kind := getString(wait, "kind")
	if waitContinued(wait) {
		clearWaitAndConsume(sid, state)
		logInfo("runner %s: transcript changed after reset, session already continued", sid)
		return
	}
	resume := section(cfg, "resume")
	ready := ""
	if kind != "fable" && currentHost().limits {

		result := decide(cfg, state, object{"session_id": sid, "cwd": getString(wait, "cwd"), "transcript_path": getString(wait, "transcript")}, now, decideOptions{force: true})
		if result.wait != nil && result.wait.until > float64(now+120) {
			resumeAt := result.wait.until + math.Max(0, numberOr(waitCfg, "resetMarginSeconds", 0))
			plan := result.wait
			if !rescheduleOwnWait(sid, startedAt, func(record object) {
				record["until"], record["resumeAt"], record["label"], record["window"] = plan.until, resumeAt, plan.label, plan.window
				record["hit"], record["used"], record["threshold"] = plan.hit, plan.used, plan.threshold
				record["startedAt"] = float64(now)
				delete(record, "earlyTriggeredAt")
			}) {
				leaveReplacedWait(sid)
				return
			}
			scheduleRunner(cfg, sid, resumeAt)
			logInfo("runner %s: limit still active (%s %%%s), rescheduled to %s", sid, plan.label, formatNumber(plan.used), localISO(resumeAt))
			return
		}
		if kind == "stopfailure" && !result.usage.hasAny {
			attempts := int(numberOr(wait, "attempts", 0)) + 1
			if attempts >= stopFailureMaxAttempts {
				if !clearOwnWait(sid, startedAt) {
					leaveReplacedWait(sid)
					return
				}
				notify(cfg, pluginName, T("runner.noData", shortSid(sid), sid))
				fail("runner %s: giving up after %d attempts without usage data", sid, attempts)
				return
			}
			retryAt := float64(now) + retryDelaySeconds(cfg, attempts)
			if !rescheduleOwnWait(sid, startedAt, func(record object) {
				record["attempts"], record["resumeAt"] = float64(attempts), retryAt
				record["startedAt"] = float64(now)
				delete(record, "earlyTriggeredAt")
			}) {
				leaveReplacedWait(sid)
				return
			}
			scheduleRunner(cfg, sid, retryAt)
			warn("runner %s: no usage data, retry %d at %s", sid, attempts, localISO(retryAt))
			return
		}
		if getString(resume, "mode") == "none" {
			notify(cfg, pluginName, readyNotice(wait, release, now, T("wait.readyNone")))
		} else {
			ready = readyNotice(wait, release, now, T("wait.readyTail"))
		}
	}
	if getString(resume, "mode") == "none" {
		clearWait(sid, state)
		return
	}
	if wakeAt := numberOr(wait, "wakeAttemptedAt", 0); wakeAt > 0 && sessionActiveAfter(wait, wakeAt) {
		clearWaitAndConsume(sid, state)
		if ready != "" {
			notify(cfg, pluginName, ready)
		}
		journal(sid, "resume", "skip-launch", "same-session wake succeeded", nil)
		logInfo("runner %s: same-session wake already continued the session", sid)
		return
	}
	model := orDefault(getString(wait, "modelOverride"), resolveSessionModel(cfg, readState(), readJSON(files.usage), sid))
	queuePath := queueFileFor(cfg, getString(wait, "cwd"), sid)
	prompt := orDefault(getString(wait, "queuedPrompt"), getString(resume, "prompt"))
	if queuePath != "" && queueTrusted(cfg, queuePath) {
		listName := filepath.Base(queuePath)
		if isAutoQueue(queuePath) {
			listName = queuePath
		}
		prompt += " Task list: " + listName + " (do not redo items already marked done)"
		items := queueSnapshot(queuePath).items
		if len(items) > 3 {
			items = items[:3]
		}
		if len(items) > 0 {
			prompt += " Next: " + strings.Join(items, " | ") + "."
		}
	}
	if workspaceChanged(wait) {
		journal(sid, "resume", "workspace-changed", "tree differs from the checkpoint", nil)
		prompt += " " + T("workspace.context")
	}
	if note := workflowResumeNote(readState(), sid); note != "" {
		prompt += " " + note
	}
	if getBool(wait, "overload", false) {
		prompt = T("overload.wakeMessage", getString(wait, "label"), formatNumber(numberOr(wait, "attempt", 1)), durationText(numberOr(wait, "resumeAt", 0)-numberOr(wait, "startedAt", numberOr(wait, "resumeAt", 0)))) + " " + prompt
	}
	prompt = sanitizePrompt(prompt)
	claimed, watcher := false, 0
	updateState(func(next object) {

		if handoffHeldFor(next, sid, startedAt) > 0 {
			return
		}
		if record := getMap(getMap(next, "waits"), sid); record == nil || numberOr(record, "startedAt", -1) != startedAt {

			return
		}
		claimed, watcher = true, liveRunner(getMap(getMap(next, "handedOff"), sid))
		stateMap(next, "handedOff")[sid] = object{"at": float64(nowSec()), "model": model, "mode": getString(resume, "mode"), "pid": float64(os.Getpid()), "waitStartedAt": startedAt}
		stateMap(next, "resumePrompts")[sid] = object{"hash": promptDigest(prompt), "at": float64(nowSec())}
		stateMap(next, "modelOverrides")[sid] = object{"model": model, "at": float64(nowSec())}
		if checkpoint := getMap(getMap(next, "checkpoints"), sid); checkpoint != nil {
			checkpoint["consumed"] = true
		}
		if record := getMap(getMap(next, "waits"), sid); record != nil {
			delete(record, "scheduled")
		}
	})
	if !claimed {
		leaveSessionToItsRunner(sid)
		return
	}
	defer releaseHandoff(sid, startedAt)
	if watcher > 0 {
		journal(sid, "resume", "take-over", fmt.Sprintf("runner %d only watches the window it opened before this pause", watcher), nil)
		logInfo("runner %s: runner %d only watches the window it opened before this pause; taking the session over", sid, watcher)
	}
	if ready != "" {
		notify(cfg, pluginName, ready)
	}
	launchMode := getString(wait, "launchMode")
	if launchMode == "none" {
		launchMode = ""
	}
	journal(sid, "resume", "launch", model, object{"mode": orDefault(launchMode, getString(resume, "mode"))})
	updateState(func(next object) { delete(stateMap(next, "launchFailures"), sid) })
	if !launchClaude(cfg, launchSpec{sid: sid, model: model, prompt: prompt, cwd: getString(wait, "cwd"), mode: launchMode, permissionMode: getString(wait, "permissionMode"), configDir: relaunchConfigDir(wait)}) {
		journal(sid, "resume", "launch-failed", model, nil)
		notify(cfg, pluginName, T("launch.failed", shortSid(sid), sid))
		fail("runner %s: automatic relaunch failed; resume manually with claude --resume %s", sid, sid)

		updateState(func(next object) {
			stateMap(next, "launchFailures")[sid] = object{"at": float64(nowSec()), "model": model}
		})
	}
}

func runSleeper() {
	at, ok := toNumber(flagString("at"))
	sid := flagString("sid")
	if sid == "" || !ok {
		return
	}
	watching := args.present["watch"]
	cfg := loadConfig()
	watch := newWaitWatch(cfg, sid, false)
	watch.pollEvery = earlyResetPollSeconds(cfg)
	watch.relaunch = true
	if !currentHost().limits {
		watch.pollEvery = 0
	}
	sleepUntilPaced(at, watch.tickPace, func() bool {
		stop := watch.tick()
		if !stop {
			debug.FreeOSMemory()
		}
		return stop
	})
	if watch.cancelled {
		logInfo("sleeper %s: wait cancelled; nothing to resume", sid)
		return
	}
	if watch.early {
		journal(sid, "sleeper", "early-reset", "window cleared ahead of schedule", nil)
		logInfo("sleeper %s: window cleared ahead of schedule; resuming now", sid)
		resumeWait(sid, "reset")
		return
	}
	if watch.dataBack {
		journal(sid, "sleeper", "data-back", "fresh usage data shows room", nil)
		logInfo("sleeper %s: fresh usage data shows room; resuming now", sid)
		resumeWait(sid, "data")
		return
	}
	if watching {
		return
	}
	resumeWait(sid, "")
}

func retryDelaySeconds(cfg object, attempt int) float64 {
	ladder := []float64{10, 20, 30, 45, 60}
	if configured := getList(section(cfg, "wait"), "retryMinutes"); len(configured) > 0 {
		ladder = ladder[:0]
		for _, raw := range configured {
			if minutes, ok := toNumber(raw); ok && minutes > 0 {
				ladder = append(ladder, minutes)
			}
		}
	}
	if len(ladder) == 0 {
		ladder = []float64{30}
	}
	if attempt < 1 {
		attempt = 1
	}
	if attempt > len(ladder) {
		attempt = len(ladder)
	}
	return ladder[attempt-1] * 60
}

func platformName() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

func execCommand(binary string, arguments ...string) *exec.Cmd {
	return exec.Command(binary, arguments...)
}
