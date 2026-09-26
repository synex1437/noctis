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
	permissionManual   = lazyRegexp(`permission-mode[\s\S]{0,600}?["' ]manual["',)\s]`)
	permissionDontAsk  = lazyRegexp(`permission-mode[\s\S]{0,600}?["' ]dontAsk["',)\s]`)
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
	limit, guarded := thresholdEnabled(cfg, "weeklyAll")
	if !guarded {
		limit = 100
	}
	expected := elapsed * limit
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
	mark := func(win *window, key string) string {
		if threshold, guarded := thresholdEnabled(cfg, key); guarded && win.used >= threshold-warnBand {
			return "⚠"
		}
		return ""
	}
	if usage.fiveHour != nil {
		parts = append(parts, fmt.Sprintf("%s%s %s→%s", mark(usage.fiveHour, "session5h"), T("win.five"), T("badge.percent", int(math.Round(usage.fiveHour.used))), formatTime(usage.fiveHour.resetsAt)))
	}
	if usage.sevenDay != nil {
		parts = append(parts, fmt.Sprintf("%s%s %s%s→%s", mark(usage.sevenDay, "weeklyAll"), T("badge.week"), T("badge.percent", int(math.Round(usage.sevenDay.used))), paceMarker(cfg, usage.sevenDay, now), formatTime(usage.sevenDay.resetsAt)))
	}
	if usage.fable != nil {
		fableMark := ""
		if limit, guarded := scopedThresholdEnabled(cfg); guarded && usage.fable.used >= limit-warnBand {
			fableMark = "⚠"
		}
		parts = append(parts, fmt.Sprintf("%s%s %s", fableMark, scopedLabel(cfg), T("badge.percent", int(math.Round(usage.fable.used)))))
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
			if reported := getString(input, "version"); reported != "" {
				info["version"] = reported
			}
			if context, ok := getNumber(getMap(input, "context_window"), "used_percentage"); ok {
				info["context"] = context
			} else {
				info["context"] = nil
			}
			if tokens, ok := contextTokensOf(getMap(input, "context_window")); ok {
				info["contextTokens"] = tokens
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
		context = leanContextText(cfg, sid, percent)
	}
	marker := "∞"
	if !getBool(statuslineCfg, "emoji", true) {
		marker = "NOCTIS"
	}
	if numberOr(state, "disabledUntil", 0) > float64(now) && ceilingHit(cfg, usage) == nil {
		marker += "⏸"
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

func platformShell(line string) *exec.Cmd {
	if isWindows {
		command := windowsShell(line)
		command.Env = append(os.Environ(), "NoDefaultCurrentDirectoryInExePath=1")
		return command
	}
	return exec.Command("sh", "-c", line)
}

func runChain(chain string, input []byte) string {
	command := platformShell(chain)
	command.Stdin = strings.NewReader(string(input))
	output, err := runTreeWithTimeout(command, 4*time.Second)
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
	if arg != "" && !windowsQuoteNeeded.MatchString(arg) {
		return arg
	}
	escaped := backslashQuote.ReplaceAllString(arg, `$1$1\"`)
	escaped = trailingBackslash.ReplaceAllString(escaped, "$1$1")
	return `"` + escaped + `"`
}

func windowsCommandLine(program string, arguments []string) string {
	parts := []string{windowsQuote(program)}
	for _, arg := range arguments {
		parts = append(parts, windowsQuote(arg))
	}
	return strings.Join(parts, " ")
}

func claudeCommand(claudePath string, claudeArgs []string) *exec.Cmd {
	if isWindows && strings.HasSuffix(strings.ToLower(claudePath), ".cmd") {
		return windowsShell(windowsCommandLine(claudePath, claudeArgs))
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
		requested = orDefault(inherited, "default")
	}
	if refusedPermModes[requested] {
		warn("permission mode %q is not used for unattended relaunches; falling back to acceptEdits", requested)
		return "acceptEdits"
	}
	if !knownPermModes[requested] {
		return "default"
	}
	if requested != "auto" && requested != "manual" && requested != "dontAsk" {
		return requested
	}
	output, _ := runWithTimeout(inGuardDir(claudeCommand(claudePath, []string{"--help"})), 20*time.Second)
	switch {
	case requested == "manual" && permissionManual.Match(output):
		return "manual"
	case requested == "dontAsk" && permissionDontAsk.Match(output):
		return "dontAsk"
	case requested != "auto":
		return "default"
	case permissionAuto.Match(output):
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
	if strings.HasPrefix(value, "-") {
		value = "Continue. " + value
	}
	if runes := []rune(value); len(runes) > 4000 {
		return string(runes[:4000])
	}
	return value
}

func relaunchPrompt(text string) string {
	text = strings.ReplaceAll(text, "\x00", "")
	if isWindows {
		return sanitizePrompt(text)
	}
	value := strings.TrimSpace(text)
	if strings.HasPrefix(value, "-") {
		value = "Continue. " + value
	}
	return value
}

type launchSpec struct {
	sid            string
	fresh          string
	model          string
	effort         string
	prompt         string
	cwd            string
	mode           string
	permissionMode string
	configDir      string
}

func (launch launchSpec) running() string {
	return orDefault(launch.fresh, launch.sid)
}

type launchResult struct {
	started   bool
	window    bool
	exitError bool
}

func launchClaude(cfg object, launch launchSpec) launchResult {
	resume := section(cfg, "resume")
	mode := orDefault(launch.mode, getString(resume, "mode"))
	host := currentHost()
	claudePath := hostExecutable(host.id)
	if claudePath == "" {
		fail("launch aborted: %s executable not found", host.exe)
		return launchResult{}
	}
	if host.id != "claude" {

		return launchHostSession(cfg, host, claudePath, launch)
	}
	if launch.cwd == "" || statSafe(launch.cwd) == nil {
		fail("launch aborted: cwd missing %s", launch.cwd)
		return launchResult{}
	}
	permissionMode := supportedPermissionMode(cfg, claudePath, launch.permissionMode)
	effort := orDefault(launch.effort, getString(section(cfg, "models"), "effort"))
	claudeArgs := hostLaunchArgs("claude", cfg, launch, effort, permissionMode)
	env := relaunchEnv(launch, effort)
	logInfo("launching claude (%s) model=%s mode=%s cwd=%s", mode, launch.model, permissionMode, launch.cwd)
	closePreviousLaunch(cfg, launch.sid, getMap(getMap(readState(), "waits"), launch.sid))
	if mode == "window" {
		switch {
		case isWindows && files.launchScript != "":
			return launchResult{started: launchInWindowsTerminal(cfg, launch, claudePath, claudeArgs, env, effort), window: true}
		case isWindows:
			warn("relaunch of %s runs headless: %s is not the plugin folder of this binary, so its scripts\\launch.ps1 is not run", launch.sid, files.pluginRoot)
		case launchInDesktopTerminal(cfg, launch, claudePath, claudeArgs, effort):
			return launchResult{started: true, window: true}
		}
	}
	if info := statSafe(files.resumeLog); info != nil && info.Size() > logMaxBytes {
		_ = renameAtomic(files.resumeLog, files.resumeLog+".1")
	}
	logFile, err := os.OpenFile(files.resumeLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		fail("resume log unavailable: %v", err)
		return launchResult{}
	}
	defer logFile.Close()
	command := claudeCommand(claudePath, append([]string{"-p"}, claudeArgs...))
	command.Env = env
	command.Dir = launch.cwd
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Run(); err != nil {
		warn("headless session ended with status %v", err)
		_, isExit := err.(*exec.ExitError)
		return launchResult{started: isExit, exitError: isExit}
	}
	logInfo("headless session ended normally")
	return launchResult{started: true}
}

func launchHostSession(cfg object, host hostSpec, exe string, launch launchSpec) launchResult {
	if launch.cwd == "" || statSafe(launch.cwd) == nil {
		fail("launch aborted: cwd missing %s", launch.cwd)
		return launchResult{}
	}
	arguments := hostLaunchArgs(host.id, cfg, launch, "", "")
	if info := statSafe(files.resumeLog); info != nil && info.Size() > logMaxBytes {
		_ = renameAtomic(files.resumeLog, files.resumeLog+".1")
	}
	logFile, err := os.OpenFile(files.resumeLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		fail("resume log unavailable: %v", err)
		return launchResult{}
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
		return launchResult{started: isExit, exitError: isExit}
	}
	logInfo("%s session ended normally", host.exe)
	return launchResult{started: true}
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

func relaunchAnswered(wait object, since float64) (answered, known bool) {
	lines, ok := tailLines(getString(wait, "transcript"), transcriptTailBytes)
	for i := len(lines) - 1; ok && i >= 0; i-- {
		entry, parsed := parseTranscriptLine(lines[i])
		if !parsed {
			continue
		}
		known = true
		at, err := time.Parse(time.RFC3339Nano, entry.timestamp)
		if err == nil && float64(at.UnixMilli())/1000 > since && entry.entryType == "assistant" && !entry.apiError && !entry.sidechain {
			return true, true
		}
	}
	return false, known
}

func relaunchDidNothing(wait object, start time.Time, result launchResult) bool {
	since := float64(start.UnixMilli()) / 1000
	quick := time.Since(start) < launchQuickExit
	if result.window {
		return quick && !sessionActiveAfter(wait, since)
	}
	if !result.exitError {
		return false
	}
	answered, known := relaunchAnswered(wait, since)
	return !answered && (known || quick)
}

func waitContinued(wait object) bool {
	return getString(wait, "kind") != "fable" && sessionContinuedAfter(wait, numberOr(wait, "until", 0))
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
	sid := flagString("sid")
	if sid == "" {
		fmt.Fprintln(os.Stderr, T("resume.usage"))
		os.Exit(2)
	}
	takeProxiesForRunner(sid)
	resumeWait(sid, flagString("release"))
	bootOutFinishedLaunchdJob(sid)
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

func leaveReplacedWait(sid string) {
	journal(sid, "resume", "skip-launch", "the wait was replaced while this runner worked on it", nil)
	logInfo("runner %s: the wait was replaced while this runner worked on it; its own runner resumes it", sid)
}

func resumeWait(sid, release string) {
	if sid == "" {
		fmt.Fprintln(os.Stderr, T("resume.usage"))
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
		if !takeWait(sid, wait, "builtin", true) {
			leaveReplacedWait(sid)
			return
		}
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
			}) || !scheduleOwnRunner(cfg, sid, startedAt, retryAt) {
				leaveReplacedWait(sid)
				return
			}
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
		if !takeWait(sid, wait, "session", true) {
			leaveReplacedWait(sid)
			return
		}
		journal(sid, "resume", "skip-launch", "the session went on in its own window after the reset", nil)
		logInfo("runner %s: the session went on after the reset; not relaunching it", sid)
		return
	}
	if fresh, at := earlierFreshStart(readState(), sid, startedAt); fresh != "" {
		if sessionContinuedAfter(freshProgress(wait, fresh), at) {
			if !takeWait(sid, wait, "session", true) {
				leaveReplacedWait(sid)
				return
			}
			journal(sid, "resume", "skip-launch", fmt.Sprintf("the fresh session %s an earlier runner started went on", fresh), nil)
			logInfo("runner %s: the fresh session %s an earlier runner started went on; not relaunching", sid, fresh)
			return
		}
		undoFreshStart(sid, fresh)
		logInfo("runner %s: the fresh session %s an earlier runner started never answered; the session gets its records back", sid, fresh)
	}
	resume := section(cfg, "resume")
	ready := ""
	capUsage := usageView{}
	if kind != "fable" && currentHost().limits {

		result := decide(cfg, state, object{"session_id": sid, "cwd": getString(wait, "cwd"), "transcript_path": getString(wait, "transcript")}, now, decideOptions{force: true})
		if result.wait != nil && result.wait.until > float64(now+120) {
			resumeAt := result.wait.until + math.Max(0, numberOr(waitCfg, "resetMarginSeconds", 0))
			plan := result.wait
			if !rescheduleOwnWait(sid, startedAt, func(record object) {
				record["until"], record["resumeAt"], record["label"], record["window"] = plan.until, resumeAt, plan.label, plan.window
				record["hit"], record["cause"], record["used"], record["threshold"] = plan.hit, plan.cause, plan.used, plan.threshold
				record["startedAt"] = float64(now)
				delete(record, "earlyTriggeredAt")
			}) || !scheduleOwnRunner(cfg, sid, float64(now), resumeAt) {
				leaveReplacedWait(sid)
				return
			}
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
				notify(cfg, pluginName, T("runner.noData", shortSid(sid), hostResumeCommand(currentHost().id, sid)))
				fail("runner %s: giving up after %d attempts without usage data", sid, attempts)
				return
			}
			retryAt := float64(now) + retryDelaySeconds(cfg, attempts)
			if !rescheduleOwnWait(sid, startedAt, func(record object) {
				record["attempts"], record["resumeAt"] = float64(attempts), retryAt
				record["startedAt"] = float64(now)
				delete(record, "earlyTriggeredAt")
			}) || !scheduleOwnRunner(cfg, sid, float64(now), retryAt) {
				leaveReplacedWait(sid)
				return
			}
			warn("runner %s: no usage data, retry %d at %s", sid, attempts, localISO(retryAt))
			return
		}
		capUsage = result.usage
		if result.fableHit && !observed(sid, "resume", "switch-model", scopedLabel(cfg), usageFacts(result.usage)) {
			persistModelSwitch(cfg, result.usage.fable.resetsAt, now)
			journal(sid, "resume", "switch-model", scopedLabel(cfg)+" "+formatNumber(result.usage.fable.used)+"%", object{"to": getString(section(cfg, "models"), "fallback")})
			logInfo("runner %s: %s is at %s%%, over its pause point; relaunching on the fallback role", sid, scopedLabel(cfg), formatNumber(result.usage.fable.used))
		}
		if getString(resume, "mode") == "none" {
			notify(cfg, pluginName, readyNotice(wait, release, now, T("wait.readyNone")))
		} else {
			ready = readyNotice(wait, release, now, T("wait.readyTail"))
		}
	}
	if getString(resume, "mode") == "none" {
		if !clearWait(sid, state) {
			leaveReplacedWait(sid)
		}
		return
	}
	if wakeAt := numberOr(wait, "wakeAttemptedAt", 0); wakeAt > 0 && sessionActiveAfter(wait, wakeAt) {
		if !takeWait(sid, wait, "wake", true) {
			leaveReplacedWait(sid)
			return
		}
		if ready != "" {
			notify(cfg, pluginName, ready)
		}
		journal(sid, "resume", "skip-launch", "same-session wake succeeded", nil)
		logInfo("runner %s: same-session wake already continued the session", sid)
		return
	} else if wakeAt > 0 && float64(nowSec()) < wakeAt+wakeGraceSeconds(cfg) {
		checkOnWakeAfterItsGrace(cfg, sid, startedAt, wakeAt)
		return
	}
	model := orDefault(getString(wait, "modelOverride"), resolveSessionModel(cfg, readState(), readJSON(files.usage), sid))
	onFallback := getString(wait, "modelOverride") != ""
	if safe := scopedSafeModel(cfg, readState(), capUsage, model, now); safe != model && !observing {
		model, onFallback = safe, true
	}
	effort := ""
	if onFallback {
		effort = appliedEffort("fallback", getMap(section(cfg, "roles"), "fallback"))
	}
	dirs := sessionDirs(getString(wait, "projectDir"), getString(wait, "cwd"))
	plan := freshStartFor(cfg, readState(), wait, sid, nowSec())
	queuePath := sessionQueueFile(cfg, sid, dirs...)
	prompt := orDefault(getString(wait, "queuedPrompt"), getString(resume, "prompt"))
	launchDir := dirs[0]
	view, trusted := queueView{}, false
	if queuePath != "" {
		view, trusted = trustedQueueSnapshot(cfg, queuePath)
	}
	if trusted {
		if len(dirs) > 1 && followedQueueFile(cfg, dirs[1]) == queuePath {
			launchDir = dirs[1]
		}
		if !queueHeld(cfg, readState(), queuePath) {
			listName := filepath.Base(queuePath)
			if isAutoQueue(queuePath) {
				listName = queuePath
			}
			prompt += " Task list: " + listName + " (do not redo items already marked done)"
			items := view.items
			if len(items) > 3 {
				items = items[:3]
			}
			if len(items) > 0 {
				prompt += " Next: " + strings.Join(items, " | ") + "."
			}
			if plan.sid == "" {
				tokens, known := getNumber(wait, "contextTokens")
				prompt += subagentNote(cfg, tokens, known)
			}
		}
		prompt += queueEditRule(cfg, queuePath)
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
	if plan.sid != "" {
		prompt = freshPrompt(sid, plan, getString(wait, "transcript")) + prompt
	}
	prompt = relaunchPrompt(prompt)
	claimed, continued, woken, watcher := false, false, 0.0, 0
	updateState(func(next object) {

		if handoffHeldFor(next, sid, startedAt) > 0 {
			return
		}
		record := ownWait(next, sid, startedAt)
		if record == nil {

			return
		}
		if waitContinued(wait) {
			continued = true
			return
		}
		if wakeAt := numberOr(record, "wakeAttemptedAt", 0); wakeAt > numberOr(wait, "wakeAttemptedAt", 0) {
			woken = wakeAt
			return
		}
		claimed, watcher = true, liveRunner(getMap(getMap(next, "handedOff"), sid))
		handoff, running := object{"at": float64(nowSec()), "model": model, "mode": getString(resume, "mode"), "pid": float64(os.Getpid()), "waitStartedAt": startedAt}, sid
		if plan.sid != "" {
			handoff["fresh"], running = plan.sid, plan.sid
			moveSessionRecords(next, sid, plan.sid)
			stateMap(next, "freshStarts")[plan.sid] = object{"from": sid, "at": float64(nowSec()), "waitStartedAt": startedAt}
		}
		stateMap(next, "handedOff")[sid] = handoff
		markContinued(next, sid, record, "runner")
		stateMap(next, "resumePrompts")[running] = object{"hash": promptDigest(prompt), "at": float64(nowSec())}
		stateMap(next, "modelOverrides")[running] = object{"model": model, "at": float64(nowSec())}
		if checkpoint := getMap(getMap(next, "checkpoints"), sid); checkpoint != nil {
			checkpoint["consumed"] = true
			if plan.sid != "" {
				checkpoint["handedTo"] = plan.sid
			}
		}
		delete(record, "scheduled")
	})
	if continued {
		if !takeWait(sid, wait, "session", true) {
			leaveReplacedWait(sid)
			return
		}
		journal(sid, "resume", "skip-launch", "the session went on in its own window while usage was checked", nil)
		logInfo("runner %s: the session went on after the reset while usage was checked; not relaunching it", sid)
		return
	}
	if woken > 0 {
		checkOnWakeAfterItsGrace(cfg, sid, startedAt, woken)
		return
	}
	if !claimed {
		leaveSessionToItsRunner(sid)
		return
	}
	defer releaseHandoff(sid, startedAt)
	kept := plan.sid == ""
	defer func() {
		if !kept {
			undoFreshStart(sid, plan.sid)
		}
	}()
	if watcher > 0 {
		journal(sid, "resume", "take-over", fmt.Sprintf("runner %d only watches the window it opened before this pause", watcher), nil)
		logInfo("runner %s: runner %d only watches the window it opened before this pause; taking the session over", sid, watcher)
	}
	launchMode := getString(wait, "launchMode")
	if launchMode == "none" {
		launchMode = ""
	}
	launch := launchSpec{sid: sid, fresh: plan.sid, model: model, effort: effort, prompt: prompt, cwd: launchDir, mode: launchMode, permissionMode: getString(wait, "permissionMode"), configDir: relaunchConfigDir(wait)}
	if blocked := launchBlocked(launch); blocked != "" {
		reportLaunchFailure(cfg, sid, model, launch.cwd, blocked)
		return
	}
	if ready != "" {
		notify(cfg, pluginName, ready)
	}
	facts := object{"mode": orDefault(launchMode, getString(resume, "mode"))}
	if plan.sid != "" {
		facts["fresh"], facts["contextTokens"], facts["idleMinutes"] = plan.sid, plan.tokens, math.Round(plan.idle/60)
		logInfo("runner %s: %s since its last turn with about %s tokens of context; relaunching it as the fresh session %s with its handoff note", sid, pauseLength(plan.idle), approxCount(plan.tokens), plan.sid)
	}
	journal(sid, "resume", "launch", model, facts)
	updateState(func(next object) { delete(stateMap(next, "launchFailures"), sid) })
	launchStart := time.Now()
	result := launchClaude(cfg, launch)
	if !result.started {
		reportLaunchFailure(cfg, sid, model, launch.cwd, "")
		return
	}
	progress := wait
	if plan.sid != "" {
		progress = freshProgress(wait, plan.sid)
	}
	if !relaunchDidNothing(progress, launchStart, result) {
		kept = true
		return
	}
	attempts := int(numberOr(wait, "launchAttempts", 0)) + 1
	journal(sid, "resume", "launch-no-progress", model, object{"attempt": float64(attempts)})
	if attempts >= stopFailureMaxAttempts {
		fail("runner %s: the relaunch ended %d times without the session answering", sid, attempts)
		reportLaunchFailure(cfg, sid, model, launch.cwd, "")
		return
	}
	ended := float64(nowSec() + 1)
	retryAt := ended + retryDelaySeconds(cfg, attempts)
	if !rescheduleOwnWait(sid, startedAt, func(record object) {
		record["launchAttempts"], record["hit"], record["inHook"] = float64(attempts), "relaunch", false
		record["startedAt"], record["until"], record["resumeAt"] = ended, ended, retryAt
		if plan.sid != "" {
			record["freshFailed"] = true
		}
		delete(record, "waking")
		delete(record, "wakeAttemptedAt")
		delete(record, "earlyTriggeredAt")
	}) || !scheduleOwnRunner(cfg, sid, ended, retryAt) {
		leaveReplacedWait(sid)
		return
	}
	warn("runner %s: the relaunch ended without the session answering; retry %d at %s", sid, attempts, localISO(retryAt))
}

func checkOnWakeAfterItsGrace(cfg object, sid string, startedAt, wakeAt float64) {
	at := wakeAt + wakeGraceSeconds(cfg)
	if !scheduleOwnRunner(cfg, sid, startedAt, at) {
		leaveReplacedWait(sid)
		return
	}
	journal(sid, "resume", "skip-launch", "the session was woken in place and its wake's grace is not over yet", object{"at": at})
	logInfo("runner %s: the session was woken in place at %s; checking on it at %s before relaunching it", sid, localISO(wakeAt), localISO(at))
}

func launchBlocked(launch launchSpec) string {
	host := currentHost()
	manual := hostResumeCommand(host.id, launch.sid)
	if hostExecutable(host.id) == "" {
		return T("launch.noClaude", shortSid(launch.sid), host.exe, orDefault(launch.cwd, "?"), manual)
	}
	if launch.cwd == "" || statSafe(launch.cwd) == nil {
		return T("launch.noCwd", shortSid(launch.sid), orDefault(launch.cwd, "?"), manual)
	}
	return ""
}

func reportLaunchFailure(cfg object, sid, model, cwd, notice string) {
	journal(sid, "resume", "launch-failed", model, nil)
	manual := hostResumeCommand(currentHost().id, sid)
	notify(cfg, pluginName, orDefault(notice, T("launch.failed", shortSid(sid), orDefault(cwd, "?"), manual)))
	fail("runner %s: automatic relaunch failed; resume manually in %s with %s", sid, orDefault(cwd, "?"), manual)
	updateState(func(next object) {
		stateMap(next, "launchFailures")[sid] = object{"at": float64(nowSec()), "model": model}
	})
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
