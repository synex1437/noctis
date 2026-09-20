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
		next["updatedAt"] = float64(now)
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
		offered, parsed := 0, 0
		for _, key := range []string{"five_hour", "seven_day"} {
			win := getMap(limits, key)
			if win != nil {
				offered++
			}
			rawUsed, okUsed := getNumber(win, "used_percentage")
			resetsAt, okReset := getNumber(win, "resets_at")
			if win == nil || !okUsed || !okReset {
				continue
			}
			parsed++

			used, sane := sanePercent(rawUsed)
			if !sane || !saneResetTime(resetsAt, now) {
				warn("status line reported an unusable %s window (used=%v resets_at=%v); ignored", key, rawUsed, resetsAt)
				continue
			}
			origin := reporter
			if previousWin := getMap(previous, key); multiSessionMax && previousWin != nil && getString(previousWin, "sid") != "" && getString(previousWin, "sid") != reporter && numberOr(previousWin, "resetsAt", -1) == resetsAt && numberOr(previousWin, "used", 0) > used {
				used = numberOr(previousWin, "used", 0)
				origin = getString(previousWin, "sid")
			}
			next[key] = object{"used": used, "resetsAt": resetsAt, "sid": origin}
			appendHistory(history, key, used, resetsAt, now)
		}

		if offered > 0 && parsed == 0 {
			next["updatedAt"] = numberOr(previous, "updatedAt", 0)
			if float64(now)-numberOr(getMap(readState(), "notified"), "statuslineShape", 0) > 86400 {
				updateState(func(state object) { stateMap(state, "notified")["statuslineShape"] = float64(now) })
				warn("the status line payload has %d usage window(s) this version cannot read; usage is coming from the API instead — the plugin may need an update", offered)
			}
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
				if content, err := os.ReadFile(files.usage); err == nil {
					_ = os.WriteFile(files.usageBackup, content, 0o600)
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
	finder := "which"
	if isWindows {
		finder = "where.exe"
	}
	output, _ := runWithTimeout(exec.Command(finder, name), 5*time.Second)
	lines := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
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
		if statSafe(candidate) != nil {
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
	output, _ := runWithTimeout(claudeCommand(claudePath, []string{"--help"}), 20*time.Second)
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
	env := append(os.Environ(), "CLAUDE_CONFIG_DIR="+files.configDir, "CLAUDE_CODE_EFFORT_LEVEL="+effort, handoffEnv+"="+launch.sid)
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
		_ = os.Rename(files.resumeLog, files.resumeLog+".1")
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
		_ = os.Rename(files.resumeLog, files.resumeLog+".1")
	}
	logFile, err := os.OpenFile(files.resumeLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		fail("resume log unavailable: %v", err)
		return false
	}
	defer logFile.Close()
	logInfo("launching %s (headless) sid=%s cwd=%s", host.exe, launch.sid, launch.cwd)
	command := exec.Command(exe, arguments...)
	command.Env = append(os.Environ(), handoffEnv+"="+launch.sid, "NOCTIS_HOST="+host.id)
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

func mergeInto(target, source object) {
	for key, value := range source {
		target[key] = value
	}
}

func runResume() {
	sid := flagString("sid")
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

		cancelLaunchd(sid)
		cancelSystemd(sid)
		logInfo("runner %s: no wait record (already completed or cancelled)", sid)
		return
	}
	if getString(getMap(wait, "scheduled"), "method") == "launchd" {
		cancelLaunchd(sid)
	}
	auto := getMap(getMap(state, "autoResume"), sid)
	if getString(auto, "type") == "quota_auto_resume_fired" && numberOr(auto, "at", 0) >= numberOr(wait, "startedAt", 0) {
		clearWait(sid, state)
		consumeCheckpoint(sid)
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
		mergeInto(wait, refreshed)
	}
	if hookSleeping(wait) && float64(nowSec())-numberOr(wait, "heartbeat", 0) < heartbeatFreshSeconds {
		checks := int(numberOr(wait, "aliveChecks", 0)) + 1
		if checks <= aliveHookMaxChecks {
			retryAt := float64(now + 300)
			updateState(func(next object) {
				if record := getMap(getMap(next, "waits"), sid); record != nil {
					record["aliveChecks"] = float64(checks)
					record["resumeAt"] = retryAt
				}
			})
			scheduleRunner(cfg, sid, retryAt)
			logInfo("runner %s: waiting hook still alive (heartbeat), re-check %d at %s", sid, checks, localISO(retryAt))
			return
		}
		warn("runner %s: hook heartbeat still fresh after %d checks, treating as stuck", sid, checks-1)
	}
	kind := getString(wait, "kind")
	if kind != "fable" && sessionActiveAfter(wait, numberOr(wait, "until", 0)) {
		clearWait(sid, state)
		consumeCheckpoint(sid)
		logInfo("runner %s: transcript changed after reset, session already continued", sid)
		return
	}
	resume := section(cfg, "resume")
	if kind != "fable" && currentHost().limits {

		result := decide(cfg, state, object{"session_id": sid, "cwd": getString(wait, "cwd"), "transcript_path": getString(wait, "transcript")}, now, decideOptions{force: true})
		if result.wait != nil && result.wait.until > float64(now+120) {
			resumeAt := result.wait.until + math.Max(0, numberOr(waitCfg, "resetMarginSeconds", 0))
			plan := result.wait
			updateState(func(next object) {
				if record := getMap(getMap(next, "waits"), sid); record != nil {
					record["until"], record["resumeAt"], record["label"], record["window"] = plan.until, resumeAt, plan.label, plan.window

					record["startedAt"] = float64(now)
					delete(record, "earlyTriggeredAt")
				}
			})
			scheduleRunner(cfg, sid, resumeAt)
			logInfo("runner %s: limit still active (%s %%%s), rescheduled to %s", sid, plan.label, formatNumber(plan.used), localISO(resumeAt))
			return
		}
		if kind == "stopfailure" && !result.usage.hasAny {
			attempts := int(numberOr(wait, "attempts", 0)) + 1
			if attempts >= stopFailureMaxAttempts {
				clearWait(sid, state)
				notify(cfg, pluginName, T("runner.noData", shortSid(sid), sid))
				fail("runner %s: giving up after %d attempts without usage data", sid, attempts)
				return
			}
			retryAt := float64(now) + retryDelaySeconds(cfg, attempts)
			updateState(func(next object) {
				if record := getMap(getMap(next, "waits"), sid); record != nil {
					record["attempts"], record["resumeAt"] = float64(attempts), retryAt
					record["startedAt"] = float64(now)
					delete(record, "earlyTriggeredAt")
				}
			})
			scheduleRunner(cfg, sid, retryAt)
			warn("runner %s: no usage data, retry %d at %s", sid, attempts, localISO(retryAt))
			return
		}
		tail := T("wait.readyTail")
		if getString(resume, "mode") == "none" {
			tail = T("wait.readyNone")
		}
		notify(cfg, pluginName, T("wait.ready", getString(wait, "label"), tail))
	}
	if getString(resume, "mode") == "none" {
		clearWait(sid, state)
		return
	}
	if wakeAt := numberOr(wait, "wakeAttemptedAt", 0); wakeAt > 0 && sessionActiveAfter(wait, wakeAt) {
		clearWait(sid, state)
		consumeCheckpoint(sid)
		journal(sid, "resume", "skip-launch", "same-session wake succeeded", nil)
		logInfo("runner %s: same-session wake already continued the session", sid)
		return
	}
	model := orDefault(getString(wait, "modelOverride"), resolveSessionModel(cfg, readState(), readJSON(files.usage), sid))
	queuePath := queueFileFor(cfg, getString(wait, "cwd"), sid)
	prompt := orDefault(getString(wait, "queuedPrompt"), getString(resume, "prompt"))
	if queuePath != "" {
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
	startedAt := numberOr(wait, "startedAt", 0)
	claimed := false
	updateState(func(next object) {

		if other := getMap(stateMap(next, "handedOff"), sid); other != nil && processAlive(int(numberOr(other, "pid", 0))) && int(numberOr(other, "pid", 0)) != os.Getpid() {
			return
		}
		if record := getMap(getMap(next, "waits"), sid); record == nil || numberOr(record, "startedAt", -1) != startedAt {

			return
		}
		claimed = true
		stateMap(next, "handedOff")[sid] = object{"at": float64(nowSec()), "model": model, "mode": getString(resume, "mode"), "pid": float64(os.Getpid())}
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
		journal(sid, "resume", "skip-launch", "another runner is already resuming this session", nil)
		logInfo("runner %s: another runner already holds the session; not launching twice", sid)
		return
	}
	defer updateState(func(next object) {
		delete(stateMap(next, "handedOff"), sid)
		if record := getMap(getMap(next, "waits"), sid); record != nil && numberOr(record, "startedAt", -1) == startedAt {
			delete(stateMap(next, "waits"), sid)
		}
	})
	launchMode := getString(wait, "launchMode")
	if launchMode == "none" {
		launchMode = ""
	}
	journal(sid, "resume", "launch", model, object{"mode": orDefault(launchMode, getString(resume, "mode"))})
	updateState(func(next object) { delete(stateMap(next, "launchFailures"), sid) })
	if !launchClaude(cfg, launchSpec{sid: sid, model: model, prompt: prompt, cwd: getString(wait, "cwd"), mode: launchMode, permissionMode: getString(wait, "permissionMode")}) {
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
	warn("sleeper-diag: argv=%q sid=%q at=%v ok=%t flags=%v positional=%v", os.Args, sid, at, ok, args.flags, args.positional)
	if sid == "" || !ok {
		warn("sleeper-diag: giving up before any work: sid=%q ok=%t", sid, ok)
		return
	}
	watching := args.present["watch"]
	cfg := loadConfig()
	watch := newWaitWatch(cfg, sid, false)
	watch.pollEvery = earlyResetPollSeconds(cfg)
	if !currentHost().limits {
		watch.pollEvery = 0
	}
	sleepUntilEvery(at, watch.tickSeconds(), watch.tick)
	if watch.cancelled {
		logInfo("sleeper %s: wait cancelled; nothing to resume", sid)
		return
	}
	if watch.early {
		journal(sid, "sleeper", "early-reset", "window cleared ahead of schedule", nil)
		logInfo("sleeper %s: window cleared ahead of schedule; resuming now", sid)
		runResume()
		return
	}
	if watching {
		warn("sleeper-diag: --watch set and window did not clear early; not resuming sid=%q", sid)
		return
	}
	warn("sleeper-diag: reached the scheduled time, resuming sid=%q", sid)
	runResume()
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
