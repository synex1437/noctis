package main

import (
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var versionPattern = lazyRegexp(`(\d+\.\d+\.\d+)`)

func tailFileLines(file string, count int) []string {
	info := statSafe(file)
	if info == nil {
		return nil
	}
	content, cut, err := readTailBytes(file, info.Size(), tailLineWindowBytes)
	if err != nil {
		return nil
	}
	lines := []string{}
	for _, line := range strings.Split(string(dropPartialFirstLine(content, cut)), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return lines
}

func shortSid(sid string) string {
	if len(sid) > 8 {
		return sid[:8]
	}
	return sid
}

func planWindows(usage usageView, cfg object) string {
	parts := []string{}
	if usage.fiveHour != nil {
		parts = append(parts, "5h")
	}
	if usage.sevenDay != nil {
		parts = append(parts, "7d")
	}
	fable := readJSON(files.fable)
	if buckets := getString(fable, "buckets"); buckets != "" {
		parts = append(parts, buckets)
	} else if usage.fable != nil {
		parts = append(parts, scopedLabel(cfg))
	}
	if len(parts) == 0 {
		return T("status.noData")
	}
	return strings.Join(parts, " + ")
}

func describeState(cfg, state object, usage usageView, now int64) string {
	thresholds := section(cfg, "thresholds")
	models := section(cfg, "models")
	lines := []string{T("status.accountDir", files.configDir)}
	if statSafe(files.config) == nil {
		lines = append(lines, T("status.notSetUp", pluginName))
	}
	usageText := orDefault(usageBadgeAt(usage, cfg, now), T("status.noData"))
	if usage.updatedAt > 0 {
		usageText += T("status.updated", formatTime(usage.updatedAt))
	}
	lines = append(lines, T("status.usage", usageText))
	lines = append(lines, T("status.plan", planWindows(usage, cfg)))
	lines = append(lines, T("status.thresholds", getString(thresholds, "session5h"), getString(thresholds, "weeklyAll"), scopedLabel(cfg), formatNumber(scopedThreshold(cfg))))
	if repaired := repairedThresholds(cfg); len(repaired) > 0 {
		lines = append(lines, T("status.thresholdsFixed", strings.Join(repaired, ", ")))
	}
	if unguarded := unguardedWindows(cfg); len(unguarded) > 0 {
		lines = append(lines, T("status.thresholdsBad", strings.Join(unguarded, ", ")))
	}
	lines = append(lines, T("status.credits", creditsText(cfg)))
	if currentHost().id == "claude" {
		lines = append(lines, leanStatusLines(cfg)...)
	}
	modelLine := T("status.model", orDefault(settingsModel(), T("status.modelNone")), getString(models, "primary"), getString(models, "fallback"), getString(models, "effort"))
	if switched := getMap(state, "modelSwitched"); switched != nil {
		modelLine += T("status.switched", formatTime(numberOr(switched, "at", 0)))
	}
	lines = append(lines, modelLine)
	routerText := T("status.routerOff")
	if getBool(section(cfg, "router"), "enabled", true) {
		routerText = T("status.routerOn", liteAgentType(cfg))
	}
	lines = append(lines, T("status.router", routerText))
	lines = append(lines, T("status.roles", describeRoles(section(cfg, "roles"))))
	if profile := retunedProfile(section(cfg, "roles")); profile != "" {
		lines = append(lines, T("status.roles", retunedNotice(profile)))
	}
	fable := readJSON(files.fable)
	fableText := T("status.notFetched")
	if fetched := numberOr(fable, "fetchedAt", 0); fetched > 0 {
		fableText = T("status.fetchedAt", formatTime(fetched))
	}
	if message := liveRefreshError(fable, nowSec()); message != "" {
		fableText += T("status.error", message)
	}
	if note := getString(fable, "note"); note != "" {
		fableText += " · " + note
	}
	lines = append(lines, T("status.scopedData", scopedLabel(cfg), fableText))
	if usage.clockOffset != 0 {
		direction := T("status.skewAhead")
		if usage.clockOffset > 0 {
			direction = T("status.skewBehind")
		}
		lines = append(lines, T("status.skew", int(absFloat(usage.clockOffset)), direction))
	}
	if configError := getString(cfg, "configError"); configError != "" {
		lines = append(lines, T("status.configBroken", configError))
	}
	if disabled := numberOr(state, "disabledUntil", 0); disabled > float64(now) {
		lines = append(lines, T("status.disabled", formatTime(disabled)))
	}
	if observing {
		lines = append(lines, T("status.observe"))
	}
	waits := getMap(state, "waits")
	waitHeader := T("status.waits")
	if len(waits) == 0 {
		waitHeader += T("status.waitsNone")
	}
	lines = append(lines, waitHeader)
	for _, sid := range sortedKeys(waits) {
		wait := toObject(waits[sid])
		if !waitLive(wait, now) {
			continue
		}
		entry := T("status.waitLine", shortSid(sid), getString(wait, "kind"), getString(wait, "label"), formatTime(numberOr(wait, "resumeAt", 0)))
		if scheduled := getMap(wait, "scheduled"); scheduled != nil {
			entry += " [" + getString(scheduled, "method") + "]"
		}
		if checkpoint := getString(wait, "checkpoint"); checkpoint != "" {
			entry += " cp=" + filepath.Base(checkpoint)
		}
		lines = append(lines, entry)
	}
	if failures := getMap(state, "launchFailures"); len(failures) > 0 {
		for _, sid := range sortedKeys(failures) {
			entry := toObject(failures[sid])
			lines = append(lines, T("status.launchFailed", shortSid(sid), formatTime(numberOr(entry, "at", 0)), hostResumeCommand(currentHost().id, sid)))
		}
	}
	handoffs := getMap(state, "handedOff")
	if len(handoffs) > 0 {
		parts := []string{}
		for _, sid := range sortedKeys(handoffs) {
			parts = append(parts, shortSid(sid)+"→"+getString(toObject(handoffs[sid]), "model"))
		}
		lines = append(lines, T("status.handedOff", strings.Join(parts, ", ")))
	}
	errorLines := tailFileLines(files.errors, 5)
	if len(errorLines) == 0 {
		lines = append(lines, T("status.errorsClean"))
	} else {
		lines = append(lines, T("status.errors", files.errors, len(errorLines)))
		for _, line := range errorLines {
			lines = append(lines, "  "+line)
		}
	}
	return strings.Join(lines, "\n")
}

func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func runStatus() {
	now := nowSec()
	fmt.Println(describeState(loadConfig(), readState(), currentUsage(now), now))
}

const selftestSession = "selftest"

func runCheck() {
	now := nowSec()
	cfg := loadConfig()
	usage := currentUsage(now)
	state := readState()
	model := resolveSessionModel(cfg, state, readJSON(files.usage), flagString("sid"))

	staleSeconds := usageStaleSeconds(cfg)
	code, verdict := 0, "ok"
	result := evaluate(cfg, usage, model, 0, false)
	switch {
	case !usage.hasAny, float64(now)-usage.updatedAt > staleSeconds:
		code, verdict = 20, "no-data"
	case result.wait != nil, result.fableHit:
		code, verdict = 11, "over"
	case result.warnWindow != nil:
		code, verdict = 10, "warn"
	}
	if args.present["json"] {
		facts := usageFacts(usage)
		facts["code"], facts["verdict"], facts["model"] = code, verdict, model
		if usage.hasAny {
			facts["ageSeconds"] = math.Round(float64(now) - usage.updatedAt)
		}
		if result.wait != nil {
			facts["hit"] = hitLabel(result.wait)
			facts["until"] = result.wait.until
		}
		fmt.Println(string(marshalCompact(facts)))
	} else {
		fmt.Println(verdict + " · " + orDefault(usageBadgeAt(usage, cfg, now), T("status.noData")))
	}
	os.Exit(code)
}

const (
	sidPrefixMin    = 4
	maxPauseMinutes = 7 * 24 * 60
)

var (
	pauseNumber = lazyRegexp(`^\d+(?:\.\d+)?$`)
	pauseSpan   = lazyRegexp(`^(?:\d+(?:\.\d+)?[a-z]+)+$`)
	pausePart   = lazyRegexp(`(\d+(?:\.\d+)?)([a-z]+)`)
	pauseUnits  = map[string]float64{
		"m": 1, "min": 1, "mins": 1, "minute": 1, "minutes": 1,
		"h": 60, "hr": 60, "hrs": 60, "hour": 60, "hours": 60,
		"d": 1440, "day": 1440, "days": 1440,
	}
)

func resolveSid(arg string, spaces ...object) (string, bool) {
	for _, candidate := range []string{arg, safeName(arg)} {
		for _, space := range spaces {
			if _, exists := space[candidate]; exists {
				return candidate, true
			}
		}
	}
	prefix := safeName(arg)
	if len(prefix) < sidPrefixMin {
		return arg, true
	}
	matched := map[string]bool{}
	for _, space := range spaces {
		for key := range space {
			if strings.HasPrefix(key, prefix) {
				matched[key] = true
			}
		}
	}
	matches := sortedKeys(matched)
	if len(matches) > 1 {
		fmt.Fprintln(os.Stderr, T("sid.ambiguous", arg, strings.Join(matches, ", ")))
		return arg, false
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return arg, true
}

func runCancel() {
	target := flagString("sid")
	if target == "" {
		target = positional(1)
	}
	if code := cancelPending(target); code != 0 {
		os.Exit(code)
	}
}

func cancelPending(target string) int {
	state := readState()
	pending := []object{getMap(state, "waits"), getMap(state, "handedOff"), getMap(state, "launchFailures")}
	found := map[string]bool{}
	for _, bucket := range pending {
		for sid := range bucket {
			found[sid] = true
		}
	}
	sids := sortedKeys(found)
	if target != "" {
		key, unique := resolveSid(target, pending...)
		if !unique {
			return 2
		}
		if !found[key] {
			cancelRunner(safeName(target), state)
			fmt.Println(T("cancel.noneFor", target))
			return 0
		}
		sids = []string{key}
	}
	if len(sids) == 0 {
		fmt.Println(T("cancel.none"))
		return 0
	}
	applied, failures := false, writeFailures
	updateState(func(next object) {
		applied = true
		for _, sid := range sids {
			delete(stateMap(next, "waits"), sid)
			delete(stateMap(next, "handedOff"), sid)
			delete(stateMap(next, "launchFailures"), sid)
		}
	})
	if !applied || writeFailures != failures {
		fmt.Fprintln(os.Stderr, T("cancel.notSaved", files.errors))
		return 1
	}
	shorts := []string{}
	for _, sid := range sids {
		cancelRunner(sid, state)
		shorts = append(shorts, shortSid(sid))
	}
	fmt.Println(T("cancel.done", strings.Join(shorts, ", ")))
	return 0
}

func runOff() {
	request := ""
	if len(args.positional) > 1 {
		request = strings.Join(args.positional[1:], " ")
	}
	if code := pauseGuard(request); code != 0 {
		os.Exit(code)
	}
}

func pauseGuard(request string) int {
	minutes, ok := pauseMinutes(request)
	if !ok {
		fmt.Fprintln(os.Stderr, T("off.usage"))
		return 2
	}
	until := float64(nowSec()) + minutes*60
	applied, failures := false, writeFailures
	updateState(func(next object) {
		applied = true
		next["disabledUntil"] = until
	})
	if !applied || writeFailures != failures {
		fmt.Fprintln(os.Stderr, T("off.notSaved", pluginName, files.errors))
		return 1
	}
	fmt.Println(T("off.done", pluginName, formatTime(until)))
	return 0
}

func pauseMinutes(request string) (float64, bool) {
	text := strings.ToLower(strings.TrimSpace(request))
	if text == "" {
		return 60, true
	}
	minutes := 0.0
	if pauseNumber.MatchString(text) {
		minutes, _ = strconv.ParseFloat(text, 64)
	} else {
		compact := strings.Join(strings.Fields(text), "")
		if !pauseSpan.MatchString(compact) {
			return 0, false
		}
		for _, part := range pausePart.FindAllStringSubmatch(compact, -1) {
			unit, known := pauseUnits[part[2]]
			if !known {
				return 0, false
			}
			value, _ := strconv.ParseFloat(part[1], 64)
			minutes += value * unit
		}
	}
	if !(minutes > 0 && minutes <= maxPauseMinutes) {
		return 0, false
	}
	return math.Max(minutes, 1), true
}

func runOn() {
	updateState(func(next object) {
		next["disabledUntil"] = float64(0)
		next["hookCapSeconds"] = float64(0)
		next["interruptedWaits"] = []any{}
	})
	fmt.Println(T("on.done", pluginName))
}

func runModel() {
	cfg, state := loadConfig(), readState()
	if sid := flagString("sid"); sid != "" {
		usageFile := readJSON(files.usage)
		key, unique := resolveSid(sid, getMap(state, "modelOverrides"), getMap(usageFile, "sessions"))
		if !unique {
			os.Exit(2)
		}
		fmt.Println(resolveSessionModel(cfg, state, usageFile, key))
		return
	}
	fmt.Println(defaultModel(cfg, state))
}

func runCheckpointCommand() {
	sid := flagString("sid")
	cwd := flagString("cwd")
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	now := nowSec()
	state := readState()
	bestSid, latest := latestCheckpointFor(state, cwd, now)
	if sid != "" {
		key, unique := resolveSid(sid, getMap(state, "checkpoints"))
		if !unique {
			os.Exit(2)
		}
		sid = key
		bestSid, latest = "", nil
		if entry := toObject(getMap(state, "checkpoints")[sid]); checkpointUsable(entry, "", now) {
			bestSid, latest = sid, entry
		}
	}
	if latest == nil {
		fmt.Println("NONE")
		if withheld := countWithheldCheckpoints(state, cwd, sid, now); withheld > 0 {
			fmt.Println(T("checkpoint.withheld", withheld))
		}
		return
	}
	content, err := os.ReadFile(getString(latest, "path"))
	if err != nil {
		fmt.Println("NONE")
		return
	}
	_ = bestSid
	fmt.Println(string(content))
}

func countWithheldCheckpoints(state object, cwd, sid string, now int64) int {
	withheld := 0
	for key, raw := range getMap(state, "checkpoints") {
		if sid != "" && key != sid {
			continue
		}
		scope := cwd
		if sid != "" {
			scope = ""
		}
		entry := toObject(raw)
		if entry == nil || checkpointUsable(entry, scope, now) {
			continue
		}
		if sid == "" && getString(entry, "cwd") != cwd {
			continue
		}
		withheld++
	}
	return withheld
}

var doctorIssues int

func checkLine(ok bool, text string) string {
	if ok {
		return "OK  " + text
	}
	doctorIssues++
	return "!!  " + text
}

func fixLine(ok bool, text, remedyKey string, values ...any) []string {
	line := checkLine(ok, text)
	if ok {
		return []string{line}
	}
	return []string{line, T("doctor.fixHint", T(remedyKey, values...))}
}

func doctorLines(cfg object) []string {
	lines := []string{checkLine(true, T("doctor.engine", platformName()))}

	if disabled := numberOr(readState(), "disabledUntil", 0); disabled > float64(nowSec()) {
		lines = append(lines, fixLine(false, T("doctor.disabled", formatTime(disabled)), "doctor.fixDisabled")...)
	}
	host := currentHost()
	if host.id != "claude" {
		return hostDoctorLines(cfg, host, lines)
	}
	claudePath := claudeLookup()
	lines = append(lines, fixLine(claudePath != "", T("doctor.claude", orDefault(claudePath, T("doctor.notFound"))), "doctor.fixClaude")...)
	settings := readJSONStrict(files.settings)
	settingsText := settings.err
	if settings.ok {
		settingsText = T("doctor.settingsMissing")
		if settings.exists {
			settingsText = T("doctor.settingsReadable")
		}
	}
	settingsRemedy := "doctor.fixSettings"
	if settings.unopened {
		settingsRemedy = "doctor.fixSettingsOpen"
	}
	lines = append(lines, fixLine(settings.ok, T("doctor.settings", settingsText), settingsRemedy, files.settings)...)
	statusLine := getString(getMap(settings.data, "statusLine"), "command")
	lines = append(lines, fixLine(strings.Contains(statusLine, "guard.js") || strings.Contains(statusLine, "noctis"), T("doctor.statusline", orDefault(statusLine, T("doctor.none"))), "doctor.fixStatusline")...)
	effort := getString(getMap(settings.data, "env"), "CLAUDE_CODE_EFFORT_LEVEL")
	wantEffort := getString(section(cfg, "models"), "effort")
	effortText := T("doctor.effort", orDefault(effort, T("doctor.none")))
	if effort != wantEffort {
		effortText = T("doctor.effortMismatch", orDefault(effort, T("doctor.none")), orDefault(wantEffort, T("doctor.none")))
	}
	lines = append(lines, fixLine(effort == wantEffort, effortText, "doctor.fixEffort", orDefault(wantEffort, T("doctor.none")))...)
	lines = append(lines, fixLine(getString(settings.data, "model") != "", T("doctor.model", orDefault(getString(settings.data, "model"), T("doctor.none"))), "doctor.fixSetup")...)
	if profile := retunedProfile(section(cfg, "roles")); profile != "" {
		lines = append(lines, checkLine(false, retunedNotice(profile)))
	}
	tokenText := T("doctor.tokenYes")
	if oauthToken() == "" {
		tokenText = T("doctor.tokenNo", files.credentials)
	}
	lines = append(lines, fixLine(oauthToken() != "", T("doctor.token", scopedLabel(cfg), tokenText), "doctor.fixToken")...)
	repaired := repairedThresholds(cfg)
	unguarded := unguardedWindows(cfg)
	limits := section(cfg, "thresholds")
	thresholdText := T("doctor.thresholdsOk", getString(limits, "session5h"), getString(limits, "weeklyAll"), formatNumber(scopedThreshold(cfg)))
	if len(unguarded) > 0 {
		thresholdText = T("doctor.thresholdsBad", strings.Join(unguarded, ", "))
	}
	if len(repaired) > 0 {
		thresholdText = T("doctor.thresholdsFixed", strings.Join(repaired, ", "))
	}
	lines = append(lines, fixLine(len(repaired)+len(unguarded) == 0, thresholdText, "doctor.fixThresholds")...)
	lines = append(lines, leanDoctorLines(cfg)...)
	lines = append(lines, fixLine(!paidCreditsAllowed(cfg), T("doctor.credits", creditsText(cfg)), "doctor.fixCredits")...)
	lines = append(lines, "    "+T("doctor.creditsNote"))
	if isWindows {
		result := runPowershell("$PSVersionTable.PSVersion.ToString()", 15*time.Second)
		psText := T("doctor.present")
		if !result.ok {
			psText = orDefault(result.err, result.stderr)
		}
		lines = append(lines, checkLine(result.ok, T("doctor.powershell", psText)))
	}
	lines = append(lines, schedulerDoctorLines()...)
	installRoot := files.pluginRoot
	agent := orDefault(getString(section(cfg, "router"), "agent"), "lite")
	lines = append(lines, fixLine(statSafe(filepath.Join(installRoot, "hooks", "hooks.json")) != nil, T("doctor.pluginRoot", installRoot), "doctor.fixPluginRoot")...)
	lines = append(lines, fixLine(statSafe(filepath.Join(installRoot, "agents", agent+".md")) != nil, T("doctor.agent", agent), "doctor.fixAgent")...)
	for _, issue := range unguardedAgentTools(cfg) {
		lines = append(lines, checkLine(false, issue))
	}
	for _, problem := range badRoleValues(section(cfg, "roles")) {
		lines = append(lines, checkLine(false, problem))
	}
	lines = append(lines, doctorConfigLines(cfg)...)
	usage := readJSON(files.usage)
	usageAt := numberOr(usage, "updatedAt", 0)
	usageText := T("doctor.usageNone")
	if usageAt > 0 {
		usageText = T("doctor.usageAt", formatTime(usageAt))
	}
	lines = append(lines, fixLine(usageAt > 0, T("doctor.usage", usageText), "doctor.fixUsage")...)
	lines = append(lines, errorsDoctorLines()...)
	for _, note := range coexistenceNotes(settings.data) {
		lines = append(lines, "ℹ "+note)
	}
	return lines
}

const recentErrorsWindow = 24 * time.Hour

func logEntryTime(entry string) (time.Time, bool) {
	if len(entry) < 24 {
		return time.Time{}, false
	}
	at, err := time.Parse("2006-01-02T15:04:05.000Z", entry[:24])
	return at, err == nil
}

func errorsDoctorLines() []string {
	entries := tailFileLines(files.errors, math.MaxInt32)
	if len(entries) == 0 {
		return []string{checkLine(true, T("doctor.errors", T("doctor.clean")))}
	}
	recent := 0
	for _, entry := range entries {
		if at, dated := logEntryTime(entry); !dated || time.Since(at) < recentErrorsWindow {
			recent++
		}
	}
	latest := entries[len(entries)-1]
	if recent == 0 {
		at, _ := logEntryTime(latest)
		return []string{checkLine(true, T("doctor.errors", T("doctor.errorsOld", formatTime(float64(at.Unix())))))}
	}
	count := itoa(recent)
	if info := statSafe(files.errors); recent == len(entries) && info != nil && info.Size() > tailLineWindowBytes {
		count += "+"
	}
	if runes := []rune(latest); len(runes) > 160 {
		latest = string(runes[:160])
	}
	return fixLine(false, T("doctor.errors", T("doctor.errorsRecent", count, latest)), "doctor.fixErrors", files.errors)
}

func schedulerDoctorLines() []string {
	backend := schedulerBackend()
	lines := []string{checkLine(true, T("doctor.scheduler", backend))}
	if backend == "sleeper" {
		lines = append(lines, "    "+T("doctor.schedulerSleeper"))
	}
	return lines
}

var claudeLookup = claudeExecutable

func doctorConfigLines(cfg object) []string {
	return configDoctorLines(cfg, "doctor.fixSetup")
}

func configDoctorLines(cfg object, missingRemedy string, values ...any) []string {
	configText := files.config
	remedy := missingRemedy
	configError := getString(cfg, "configError")
	if configError != "" {
		configText += " (" + configError + ")"
		remedy, values = "doctor.fixConfig", nil
	}
	return fixLine(statSafe(files.config) != nil && configError == "", T("doctor.config", configText), remedy, values...)
}

func runDoctor() {
	doctorIssues = 0
	fmt.Println(strings.Join(doctorLines(loadConfig()), "\n"))
	if doctorIssues == 0 {
		fmt.Println(T("doctor.allGood"))
		return
	}
	fmt.Println(T("doctor.issuesFound", doctorIssues))

	os.Exit(1)
}

func leadingNumber(part string) int {
	end := 0
	for end < len(part) && part[end] >= '0' && part[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	value, _ := strconv.Atoi(part[:end])
	return value
}

func compareVersions(a, b string) int {
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		valueA, valueB := 0, 0
		if i < len(partsA) {
			valueA = leadingNumber(partsA[i])
		}
		if i < len(partsB) {
			valueB = leadingNumber(partsB[i])
		}
		if valueA != valueB {
			return valueA - valueB
		}
	}
	return 0
}

func claudeVersion(claudePath string) string {
	output, _ := runWithTimeout(inGuardDir(claudeCommand(claudePath, []string{"--version"})), 20*time.Second)
	if match := versionPattern.FindStringSubmatch(string(output)); match != nil {
		return match[1]
	}
	return ""
}

func runSelftestMark() {
	token := flagString("token")
	if token == "" {
		return
	}
	ensureDir(files.launches)
	_ = os.WriteFile(filepath.Join(files.launches, "selftest-"+safeName(token)+".ok"), []byte(time.Now().UTC().Format(time.RFC3339)), 0o600)
}

func stateWriteResult(payload object) object {
	document := getMap(payload, "document")
	if document == nil {
		return object{"ok": false, "reason": "no state document on stdin"}
	}
	expect, guarded := payload["expect"].(string)
	result := object{"ok": false, "reason": "state.lock is held by another process"}
	withFileLock(files.stateLock, func() {
		if stamp := fileStamp(files.state); guarded && expect != stamp {
			result = object{"ok": false, "reason": "conflict", "stamp": stamp}
			return
		}
		if err := writeJSONAtomic(files.state, document); err != nil {
			result = object{"ok": false, "reason": err.Error()}
			return
		}
		result = object{"ok": true, "stamp": fileStamp(files.state)}
	})
	return result
}

func runStateWrite() {
	result := stateWriteResult(readStdinJSON())
	fmt.Println(string(marshalCompact(result)))
	if !getBool(result, "ok", false) {
		os.Exit(1)
	}
}

func yesNo(value bool) string {
	if value {
		return T("yes")
	}
	return T("no")
}

func runSelftest() {
	cfg := loadConfig()
	now := nowSec()
	lines := doctorLines(cfg)
	report := func(ok bool, text string) { lines = append(lines, checkLine(ok, text)) }
	if claudePath := claudeExecutable(); claudePath != "" {
		version := claudeVersion(claudePath)
		switch {
		case version == "":
			report(false, T("selftest.noVersion"))
		case compareVersions(version, testedClaudeMin) < 0:
			report(false, T("selftest.tooOld", version, testedClaudeMin))
		case compareVersions(version, testedClaudeMax) > 0:
			report(true, T("selftest.newer", version, testedClaudeMin, testedClaudeMax))
		default:
			report(true, T("selftest.inRange", version))
		}
	}
	state := readState()
	lastHook := numberOr(state, "lastHookAt", 0)
	if lastHook == 0 {
		report(false, T("selftest.pulseNone"))
	} else {
		age := float64(now) - lastHook
		report(age < 86400, T("selftest.pulse", durationText(age)))
	}
	started := time.Now()
	executable, _ := os.Executable()
	probe := probeHook(executable, []string{"hook", "--account", files.configDir}, string(marshalCompact(object{"hook_event_name": "PostToolBatch", "session_id": selftestSession, "cwd": os.TempDir()})))

	if numberOr(state, "disabledUntil", 0) <= float64(now) {
		report(probe, T("selftest.hookSpeed", time.Since(started).Milliseconds()))
	}

	if getMap(getMap(readState(), "waits"), selftestSession) != nil {
		clearWait(selftestSession, nil)
		consumeCheckpoint(selftestSession)
		logInfo("self-test probe left a wait for %s; cleared", selftestSession)
	}
	notify(cfg, pluginName, T("selftest.notifyBody"))
	report(true, T("selftest.notifySent"))
	token := hashKey(fmt.Sprintf("%s|%d", files.configDir, time.Now().UnixMilli()))
	marker := filepath.Join(files.launches, "selftest-"+token+".ok")
	ensureDir(files.launches)
	if isWindows && !args.present["skip-task"] && os.Getenv("NOCTIS_NO_TASKS") == "" {
		scheduled := scheduleWindowsTask("Noctis-selftest-"+token, float64(now+60), []string{"selftest-mark", "--token", token, "--account", `"` + files.configDir + `"`}, getBool(section(cfg, "alarm"), "wakePc", true))
		if !scheduled.ok {
			report(false, T("selftest.taskFailed", orDefault(scheduled.err, scheduled.stderr)))
		} else {
			fmt.Println(T("selftest.taskWaiting"))
			sleepUntil(float64(nowSec()+60), nil)
			fired := false
			for i := 0; i < 12 && !fired; i++ {
				sleepUntil(float64(nowSec()+5), nil)
				fired = statSafe(marker) != nil
			}
			removeScheduledTask("Noctis-selftest-" + token)
			if fired {
				_ = os.Remove(marker)
				report(true, T("selftest.taskFired"))
			} else {
				report(false, T("selftest.taskMissed"))
			}
		}
		wake := runPowershell("powercfg /waketimers", 15*time.Second)
		wakeText := T("selftest.wakeOk")
		if !wake.ok {
			wakeText = T("selftest.wakeFail")
		}
		report(true, T("selftest.wake", wakeText))
	} else if !isWindows {
		pid := detachedSelf([]string{"selftest-mark", "--token", token, "--account", files.configDir})
		sleepUntil(float64(nowSec()+3), nil)
		fired := statSafe(marker) != nil
		if fired {
			_ = os.Remove(marker)
		}
		report(fired, T("selftest.marker", pid, yesNo(fired)))
	}
	fmt.Println(strings.Join(lines, "\n"))
}

type tokenBucket struct {
	input, output, cacheRead, cacheWrite float64
	calls                                int
}

func (bucket *tokenBucket) add(usage object) {
	bucket.input += numberOr(usage, "input_tokens", 0)
	bucket.output += numberOr(usage, "output_tokens", 0)
	bucket.cacheRead += numberOr(usage, "cache_read_input_tokens", 0)
	bucket.cacheWrite += numberOr(usage, "cache_creation_input_tokens", 0)
	bucket.calls++
}

func (bucket tokenBucket) total() float64 {
	return bucket.input + bucket.output + bucket.cacheRead + bucket.cacheWrite
}

func (bucket tokenBucket) toJSON() object {
	return object{"inputTokens": bucket.input, "outputTokens": bucket.output, "cacheCreationTokens": bucket.cacheWrite, "cacheReadTokens": bucket.cacheRead, "totalTokens": bucket.total(), "calls": bucket.calls}
}

func walkTranscripts(root string, since time.Time) []string {
	found := []string{}
	_ = filepath.WalkDir(root, func(file string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			return nil
		}
		if info, statErr := entry.Info(); statErr == nil && !info.ModTime().Before(since) {
			found = append(found, file)
		}
		return nil
	})
	sort.Strings(found)
	return found
}

func formatTokens(value float64) string {
	switch {
	case value >= 1e6:
		return fmt.Sprintf("%.2fM", value/1e6)
	case value >= 1e3:
		return fmt.Sprintf("%.1fK", value/1e3)
	}
	return formatNumber(value)
}

type reportData struct {
	days           float64
	transcripts    int
	byModel        map[string]*tokenBucket
	byDay          map[string]*tokenBucket
	byDayModel     map[string]map[string]*tokenBucket
	keptOff        tokenBucket
	keptOffByModel map[string]*tokenBucket
	otherSub       tokenBucket
	events         map[string]int
	errors         []string
	prices         map[string]modelPrice
	unpriced       []string
	primaryPrice   modelPrice
	primaryPriced  bool
}

func (data reportData) totalCost() float64 {
	total := 0.0
	for model, bucket := range data.byModel {
		if price, ok := data.prices[model]; ok {
			total += bucket.cost(price)
		}
	}
	return total
}

func (data reportData) dayCost(day string) float64 {
	total := 0.0
	for model, bucket := range data.byDayModel[day] {
		if price, ok := data.prices[model]; ok {
			total += bucket.cost(price)
		}
	}
	return total
}

func (data reportData) keptOffSavings() (actual, onPrimary float64) {
	for model, bucket := range data.keptOffByModel {
		if price, ok := data.prices[model]; ok {
			actual += bucket.cost(price)
		}
		if data.primaryPriced {
			onPrimary += bucket.cost(data.primaryPrice)
		}
	}
	return actual, onPrimary
}

func attachPrices(cfg object, data *reportData) {
	data.prices = map[string]modelPrice{}
	for model := range data.byModel {
		if price, ok := priceFor(cfg, model); ok {
			data.prices[model] = price
		} else {
			data.unpriced = append(data.unpriced, model)
		}
	}
	sort.Strings(data.unpriced)
	primary := regexp.MustCompile("(?i)" + regexp.QuoteMeta(getString(section(cfg, "models"), "primary")))
	for _, model := range sortedModels(data.byModel) {
		if price, ok := data.prices[model]; ok && primary.MatchString(model) {
			data.primaryPrice, data.primaryPriced = price, true
			return
		}
	}
	data.primaryPrice, data.primaryPriced = priceFor(cfg, getString(section(cfg, "models"), "primary"))
}

func collectReport(cfg object, days float64) reportData {
	since := time.Now().Add(-time.Duration(days*24) * time.Hour)
	data := reportData{days: days, byModel: map[string]*tokenBucket{}, byDay: map[string]*tokenBucket{}, byDayModel: map[string]map[string]*tokenBucket{}, keptOffByModel: map[string]*tokenBucket{}, events: map[string]int{}}
	transcripts := walkTranscripts(filepath.Join(files.configDir, "projects"), since)
	data.transcripts = len(transcripts)
	primary := regexp.MustCompile("(?i)" + regexp.QuoteMeta(getString(section(cfg, "models"), "primary")))
	counted := map[string]bool{}
	for _, file := range transcripts {
		info := statSafe(file)
		if info == nil || info.Size() > 64*1024*1024 {
			continue
		}
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(content), "\n") {
			if !strings.Contains(line, `"usage"`) {
				continue
			}
			var raw object
			if err := jsonUnmarshalObject([]byte(line), &raw); err != nil || raw == nil || getString(raw, "type") != "assistant" {
				continue
			}
			message := getMap(raw, "message")
			usage := getMap(message, "usage")
			if usage == nil {
				continue
			}
			if response := orDefault(getString(message, "id"), getString(raw, "requestId")); response != "" {
				if counted[response] {
					continue
				}
				counted[response] = true
			}
			at, parseErr := time.Parse(time.RFC3339Nano, getString(raw, "timestamp"))
			if parseErr == nil && at.Before(since) {
				continue
			}
			model := orDefault(getString(message, "model"), "unknown")
			if data.byModel[model] == nil {
				data.byModel[model] = &tokenBucket{}
			}
			data.byModel[model].add(usage)
			day := "unknown"
			if parseErr == nil {
				day = at.UTC().Format("2006-01-02")
			}
			if data.byDay[day] == nil {
				data.byDay[day] = &tokenBucket{}
				data.byDayModel[day] = map[string]*tokenBucket{}
			}
			data.byDay[day].add(usage)
			if data.byDayModel[day][model] == nil {
				data.byDayModel[day][model] = &tokenBucket{}
			}
			data.byDayModel[day][model].add(usage)
			if getBool(raw, "isSidechain", false) {
				if primary.MatchString(model) {
					data.otherSub.add(usage)
				} else {
					data.keptOff.add(usage)
					if data.keptOffByModel[model] == nil {
						data.keptOffByModel[model] = &tokenBucket{}
					}
					data.keptOffByModel[model].add(usage)
				}
			}
		}
	}
	for _, file := range []string{files.log, files.log + ".1"} {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(content), "\n") {
			if len(line) < 24 {
				continue
			}
			at, err := time.Parse("2006-01-02T15:04:05.000Z", line[:24])
			if err != nil || at.Before(since) {
				continue
			}
			switch {
			case strings.Contains(line, "] route ->"):
				data.events["route"]++
			case strings.Contains(line, "] wait ("):
				data.events["wait"]++
			case strings.Contains(line, "fable threshold hit"):
				data.events["scoped"]++
			case strings.Contains(line, "launching claude"):
				data.events["resume"]++
			case strings.Contains(line, "] queue continue #"):
				data.events["continue"]++
			}
		}
	}
	data.errors = tailFileLines(files.errors, 3)
	attachPrices(cfg, &data)
	return data
}

func sortedModels(byModel map[string]*tokenBucket) []string {
	models := make([]string, 0, len(byModel))
	for model := range byModel {
		models = append(models, model)
	}

	sort.Slice(models, func(a, b int) bool {
		left, right := byModel[models[a]].total(), byModel[models[b]].total()
		if left != right {
			return left > right
		}
		return models[a] < models[b]
	})
	return models
}

func reportJSON(cfg object, data reportData) object {
	daily := []any{}
	totals := tokenBucket{}
	for _, day := range sortedKeys((data.byDay)) {
		bucket := data.byDay[day]
		totals.input += bucket.input
		totals.output += bucket.output
		totals.cacheRead += bucket.cacheRead
		totals.cacheWrite += bucket.cacheWrite
		totals.calls += bucket.calls
		entry := bucket.toJSON()
		entry["date"] = day
		entry["totalCost"] = roundTo(data.dayCost(day), 4)
		breakdown := []any{}
		modelsUsed := []any{}
		for _, model := range sortedModels(data.byDayModel[day]) {
			modelsUsed = append(modelsUsed, model)
			item := data.byDayModel[day][model].toJSON()
			item["modelName"] = model
			if price, ok := data.prices[model]; ok {
				item["cost"] = roundTo(data.byDayModel[day][model].cost(price), 4)
			}
			breakdown = append(breakdown, item)
		}
		entry["modelsUsed"] = modelsUsed
		entry["modelBreakdowns"] = breakdown
		daily = append(daily, entry)
	}
	byModel := []any{}
	for _, model := range sortedModels(data.byModel) {
		item := data.byModel[model].toJSON()
		item["modelName"] = model
		if price, ok := data.prices[model]; ok {
			item["cost"] = roundTo(data.byModel[model].cost(price), 4)
		}
		byModel = append(byModel, item)
	}
	totalsJSON := totals.toJSON()
	totalsJSON["totalCost"] = roundTo(data.totalCost(), 4)
	actual, onPrimary := data.keptOffSavings()
	unpriced := []any{}
	for _, model := range data.unpriced {
		unpriced = append(unpriced, model)
	}
	return object{
		"days":        data.days,
		"transcripts": data.transcripts,
		"account":     files.configDir,
		"daily":       daily,
		"models":      byModel,
		"totals":      totalsJSON,
		"keptOffPrimary": object{
			"primary":        getString(section(cfg, "models"), "primary"),
			"tokens":         data.keptOff.total(),
			"calls":          data.keptOff.calls,
			"cost":           roundTo(actual, 4),
			"costOnPrimary":  roundTo(onPrimary, 4),
			"savedVsPrimary": roundTo(onPrimary-actual, 4),
		},
		"otherSubagentTokens": data.otherSub.total(),
		"events":              data.events,
		"pricing":             object{"source": "builtin+config", "unit": "USD per million tokens, API list prices", "unpricedModels": unpriced},
	}
}

func reportText(cfg object, data reportData) string {
	lines := []string{T("report.header", formatNumber(data.days), data.transcripts, files.configDir), "", T("report.byModel")}
	for _, model := range sortedModels(data.byModel) {
		bucket := data.byModel[model]
		costText := "-"
		if price, ok := data.prices[model]; ok {
			costText = formatUSD(bucket.cost(price))
		}
		lines = append(lines, fmt.Sprintf("  %-24s %8s %8s %9s %9s · %d · %s", model, formatTokens(bucket.input), formatTokens(bucket.output), formatTokens(bucket.cacheRead), formatTokens(bucket.cacheWrite), bucket.calls, costText))
	}
	lines = append(lines, "  "+T("report.cost", formatUSD(data.totalCost())))
	if len(data.unpriced) > 0 {
		lines = append(lines, "  "+T("report.unpriced", strings.Join(data.unpriced, ", ")))
	}
	lines = append(lines, "", T("report.byDay"))
	for _, day := range sortedKeys((data.byDay)) {
		lines = append(lines, fmt.Sprintf("  %s  %8s · %d · %s", day, formatTokens(data.byDay[day].total()), data.byDay[day].calls, formatUSD(data.dayCost(day))))
	}
	actual, onPrimary := data.keptOffSavings()
	lines = append(lines, "", T("report.keptOff", getString(section(cfg, "models"), "primary"), formatTokens(data.keptOff.total()), data.keptOff.calls, formatTokens(data.otherSub.total())))
	if data.primaryPriced && data.keptOff.calls > 0 {
		lines = append(lines, T("report.saved", formatUSD(onPrimary-actual), formatUSD(actual), formatUSD(onPrimary)))
	}
	lines = append(lines, "", T("report.events", data.events["route"], data.events["wait"], data.events["scoped"], data.events["continue"], data.events["resume"]))
	if len(data.errors) > 0 {
		lines = append(lines, "", T("report.errors", files.errors))
		for _, line := range data.errors {
			if runes := []rune(line); len(runes) > 160 {
				line = string(runes[:160])
			}
			lines = append(lines, "  "+line)
		}
	}
	return strings.Join(lines, "\n")
}

func reportHTML(cfg object, data reportData) string {
	maxDay := 0.0
	for _, bucket := range data.byDay {
		if bucket.total() > maxDay {
			maxDay = bucket.total()
		}
	}
	maxModel := 0.0
	for _, bucket := range data.byModel {
		if bucket.total() > maxModel {
			maxModel = bucket.total()
		}
	}
	var body strings.Builder
	write := func(format string, values ...any) { body.WriteString(fmt.Sprintf(format, values...)) }
	bar := func(value, max float64, label string) string {
		width := 0.0
		if max > 0 {
			width = value / max * 100
		}
		return fmt.Sprintf(`<div class="row"><span class="label">%s</span><span class="track"><span class="bar" style="width:%.1f%%"></span></span><span class="value">%s</span></div>`, html.EscapeString(label), width, html.EscapeString(formatTokens(value)))
	}
	write(`<section><h2>%s</h2>`, html.EscapeString(T("report.byDay")))
	for _, day := range sortedKeys((data.byDay)) {
		body.WriteString(bar(data.byDay[day].total(), maxDay, day))
	}
	write(`</section><section><h2>%s</h2>`, html.EscapeString(T("report.byModel")))
	for _, model := range sortedModels(data.byModel) {
		body.WriteString(bar(data.byModel[model].total(), maxModel, model))
	}
	write(`</section><section><table><thead><tr><th>model</th><th>in</th><th>out</th><th>cache read</th><th>cache write</th><th>calls</th><th>cost</th></tr></thead><tbody>`)
	for _, model := range sortedModels(data.byModel) {
		bucket := data.byModel[model]
		costText := "-"
		if price, ok := data.prices[model]; ok {
			costText = formatUSD(bucket.cost(price))
		}
		write(`<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%d</td><td>%s</td></tr>`, html.EscapeString(model), formatTokens(bucket.input), formatTokens(bucket.output), formatTokens(bucket.cacheRead), formatTokens(bucket.cacheWrite), bucket.calls, html.EscapeString(costText))
	}
	actual, onPrimary := data.keptOffSavings()
	saved := ""
	if data.primaryPriced && data.keptOff.calls > 0 {
		saved = `<p>` + html.EscapeString(T("report.saved", formatUSD(onPrimary-actual), formatUSD(actual), formatUSD(onPrimary))) + `</p>`
	}
	write(`</tbody></table><p>%s</p></section><section><p>%s</p>%s<p>%s</p></section>`, html.EscapeString(T("report.cost", formatUSD(data.totalCost()))), html.EscapeString(T("report.keptOff", getString(section(cfg, "models"), "primary"), formatTokens(data.keptOff.total()), data.keptOff.calls, formatTokens(data.otherSub.total()))), saved, html.EscapeString(T("report.events", data.events["route"], data.events["wait"], data.events["scoped"], data.events["continue"], data.events["resume"])))
	return fmt.Sprintf(`<!doctype html><html lang="%s"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>noctis report</title><style>
:root{color-scheme:light dark;--surface:#fcfcfb;--text:#0b0b0b;--muted:#52514e;--track:#e6e5e1;--bar:#2a78d6}
@media (prefers-color-scheme: dark){:root:not([data-theme="light"]){--surface:#1a1a19;--text:#ffffff;--muted:#c3c2b7;--track:#2c2c2a;--bar:#3987e5}}
:root[data-theme="dark"]{--surface:#1a1a19;--text:#ffffff;--muted:#c3c2b7;--track:#2c2c2a;--bar:#3987e5}
body{margin:0;padding:24px 16px;background:var(--surface);color:var(--text);font:14px/1.5 system-ui,-apple-system,Segoe UI,Roboto,sans-serif;max-width:860px;margin-inline:auto}
h1{font-size:20px;margin:0 0 4px}h2{font-size:15px;margin:24px 0 8px;color:var(--muted);font-weight:600}
.sub{color:var(--muted);margin:0 0 8px}
.row{display:grid;grid-template-columns:minmax(90px,180px) 1fr 64px;gap:8px;align-items:center;padding:3px 0}
.label{color:var(--text);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.track{display:block;height:10px;background:var(--track);border-radius:0 4px 4px 0;overflow:hidden}
.bar{display:block;height:100%%;background:var(--bar);border-radius:0 4px 4px 0}
.value{text-align:right;color:var(--muted);font-variant-numeric:tabular-nums}
table{border-collapse:collapse;width:100%%;font-variant-numeric:tabular-nums}th,td{text-align:right;padding:6px 8px;border-bottom:1px solid var(--track)}th:first-child,td:first-child{text-align:left}th{color:var(--muted);font-weight:600}
</style></head><body><h1>noctis</h1><p class="sub">%s</p>%s</body></html>`, locale, html.EscapeString(T("report.header", formatNumber(data.days), data.transcripts, files.configDir)), body.String())
}

func runReport() {
	cfg := loadConfig()
	days := 7.0
	if value, ok := toNumber(flagString("days")); ok && value >= 1 {
		days = value
	}
	if args.present["bundle"] {
		target, err := writeBundle(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "!! %s\n", err)
			os.Exit(1)
		}
		fmt.Println(T("report.bundle", forwardSlashes(target)))
		return
	}
	data := collectReport(cfg, days)
	switch {
	case args.present["json"]:
		fmt.Println(string(marshalPretty(reportJSON(cfg, data))))
	case args.present["html"]:
		page := reportHTML(cfg, data)
		if target := flagString("html"); target != "" {
			if err := os.WriteFile(target, []byte(page), 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", err)
				os.Exit(1)
			}
			fmt.Println(target)
			return
		}
		fmt.Println(page)
	default:
		fmt.Println(reportText(cfg, data))
	}
}

func probeHook(binary string, arguments []string, stdin string) bool {
	command := execCommand(binary, arguments...)
	command.Stdin = strings.NewReader(stdin)
	command.Env = os.Environ()
	_, err := runWithTimeout(command, 30*time.Second)
	return err == nil
}

func hostDoctorLines(cfg object, host hostSpec, lines []string) []string {
	exe := hostExecutable(host.id)
	lines = append(lines, fixLine(exe != "", T("doctor.host", host.display, orDefault(exe, T("doctor.notFound"))), "doctor.fixHost", host.display, host.exe)...)
	wired, where := hostHooksWired(host.id, files.configDir)
	lines = append(lines, fixLine(wired, T("doctor.hostHooks", where), "doctor.fixHostSetup", host.id)...)
	if host.id == "antigravity" {
		statusLine := getString(getMap(readJSON(files.settings), "statusLine"), "command")
		lines = append(lines, fixLine(strings.Contains(statusLine, "noctis"), T("doctor.statusline", orDefault(statusLine, T("doctor.none"))), "doctor.fixHostSetup", host.id)...)
	}
	lines = append(lines, configDoctorLines(cfg, "doctor.fixHostSetup", host.id)...)
	switch host.id {
	case "codex":
		fable := readJSON(files.fable)
		text := T("doctor.usageNone")
		if at := numberOr(fable, "fetchedAt", 0); at > 0 {
			text = T("doctor.usageAt", formatTime(at))
		} else if message := liveRefreshError(fable, nowSec()); message != "" {
			text = message
		}
		lines = append(lines, checkLine(numberOr(fable, "fetchedAt", 0) > 0, T("doctor.usage", text)))
	case "antigravity":
		usage := readJSON(files.usage)
		usageAt := numberOr(usage, "updatedAt", 0)
		text := T("doctor.usageNone")
		if usageAt > 0 {
			text = T("doctor.usageAt", formatTime(usageAt))
		}
		lines = append(lines, checkLine(usageAt > 0, T("doctor.usage", text)))
	default:
		lines = append(lines, checkLine(true, T("doctor.usageOff", host.display)))
	}
	lines = append(lines, schedulerDoctorLines()...)
	lines = append(lines, errorsDoctorLines()...)
	return lines
}
