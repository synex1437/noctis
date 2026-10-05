package main

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	autoCompactWindowVar   = "CLAUDE_CODE_AUTO_COMPACT_WINDOW"
	autoCompactPercentVar  = "CLAUDE_AUTOCOMPACT_PCT_OVERRIDE"
	maxOutputTokensVar     = "CLAUDE_CODE_MAX_OUTPUT_TOKENS"
	autoCompactWindowMin   = 100000.0
	autoCompactWindowMax   = 1000000.0
	compactionReplyTokens  = 20000.0
	compactionBufferTokens = 13000.0
	compactionGuardShare   = 0.95
	compactPercentMin      = 10.0
	managedWindowKey       = "managedAutoCompactWindow"
	managedPercentKey      = "managedAutoCompactPercent"
	takenPercentKey        = "takenAutoCompactPercent"
	// percentWindow is the autoCompactWindow setup and ensure put beside a compaction percent: the widest
	// window Claude Code takes, which leaves each model its whole window. Claude Code compacts at a percent
	// only where a window is set for the model, by itself or in the settings: a model it keeps no window of
	// its own for (Haiku 4.5, the other Claude 4 models and older ones, a Bedrock inference profile) it compacts only once the context
	// is full.
	percentWindow = autoCompactWindowMax
	// contextFullDelaySeconds is how soon a session that stopped with its context full starts afresh.
	contextFullDelaySeconds = 30.0
)

var contextFullPattern = lazyRegexp(`(?i)autocompact is thrashing|autocompact_thrashing|prompt is too long|context limit reached|context window limit`)

type contextFill struct {
	percent, tokens, window float64
	known                   bool
	// dir is where the session started, whose project settings Claude Code reads: "" where it is not known.
	dir string
}

// forgetContextFill drops the context fill the status line recorded for sid, once Claude Code has compacted
// that context: until the status line reports the fill after the compaction, the guard, the ▲ notice, a
// hand-off note and a fresh start would weigh the context from before it.
func forgetContextFill(sid string) {
	withFileLock(files.usageLock, func() {
		current := readJSONStrict(files.usage)
		session := getMap(getMap(current.data, "sessions"), sid)
		if !current.ok || session == nil || session["context"] == nil && session["contextTokens"] == nil {
			return
		}
		session["context"] = nil
		delete(session, "contextTokens")
		if err := writeJSONAtomic(files.usage, current.data); err != nil {
			warn("SessionStart after a compaction: usage.json keeps the context fill from before it (%v)", err)
		}
	})
}

func sessionFill(session object) contextFill {
	fill := contextFill{}
	fill.percent, fill.known = getNumber(session, "context")
	fill.tokens, _ = getNumber(session, "contextTokens")
	fill.window, _ = getNumber(session, "contextWindow")
	fill.dir = orDefault(getString(session, "projectDir"), getString(session, "cwd"))
	return fill
}

func compactWindowFor(tokens float64) float64 {
	return tokens + compactionReplyTokens + compactionBufferTokens
}

// compactAim is where compaction.compactAt has Claude Code compact a session: by a number of tokens of
// context, or at a percent of each model's window; at Claude Code's own point when it holds neither.
type compactAim struct {
	tokens, percent float64
}

var compactAtPattern = lazyRegexp(`^(\d+(?:\.\d*)?|\.\d+)\s*([km%]?)$`)

// compactAtOf reads a compaction.compactAt value or a --compact-at flag: a token count from 67000 to 967000
// (280000, "280k", "0.28m", or 100 to 999 for thousands), a percent of each model's window from 10 to 100
// ("60%", or 10 to 99), or Claude Code's own point (0, false, null, "off", "auto").
func compactAtOf(value any) (compactAim, bool) {
	switch typed := value.(type) {
	case nil:
		return compactAim{}, true
	case bool:
		return compactAim{}, !typed
	case float64:
		return compactAtNumber(typed, "")
	case string:
		text := strings.ToLower(strings.TrimSpace(typed))
		if text == "off" || text == "auto" || text == "false" {
			return compactAim{}, true
		}
		match := compactAtPattern.FindStringSubmatch(text)
		if match == nil {
			return compactAim{}, false
		}
		number, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			return compactAim{}, false
		}
		return compactAtNumber(number, match[2])
	}
	return compactAim{}, false
}

func compactAtNumber(number float64, unit string) (compactAim, bool) {
	switch {
	case math.IsNaN(number) || math.IsInf(number, 0) || number < 0:
		return compactAim{}, false
	case number == 0:
		return compactAim{}, true
	case unit == "%" || unit == "" && number < 100:
		return compactAim{percent: number}, number >= compactPercentMin && number <= 100
	case unit == "m":
		number *= 1e6
	case unit == "k" || number < 1000:
		number *= 1e3
	}
	tokens := math.Round(number)
	window := compactWindowFor(tokens)
	return compactAim{tokens: tokens}, math.Abs(number-tokens) < 1e-6 && window >= autoCompactWindowMin && window <= autoCompactWindowMax
}

// setting is the value setup stores in compaction.compactAt for the aim.
func (aim compactAim) setting() any {
	switch {
	case aim.percent > 0:
		return formatNumber(aim.percent) + "%"
	case aim.tokens > 0:
		return aim.tokens
	}
	return float64(0)
}

var (
	claudeExponent = lazyRegexp(`^[+-]?(\d+(\.\d*)?|\.\d+)[eE][+-]?\d+$`)
	leadingInteger = lazyRegexp(`^[+-]?\d+`)
	leadingDecimal = lazyRegexp(`^[+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?`)
)

const digitGroupMarks = "_,\u00a0\u202f "

// claudeInteger reads a number the way Claude Code reads CLAUDE_CODE_AUTO_COMPACT_WINDOW: an integer in
// exponent form (5e5), digits in groups of three (500,000 or 500_000), else the digits it starts with, as
// JavaScript's parseInt reads them (500k is 500); NaN when it starts with none.
func claudeInteger(value string) float64 {
	text := strings.TrimFunc(value, javaScriptSpace)
	if text == "" {
		return math.NaN()
	}
	if utf8.RuneCountInString(text) <= 32 {
		if claudeExponent.MatchString(text) {
			number, err := strconv.ParseFloat(text, 64)
			if err != nil || number != math.Trunc(number) {
				return math.NaN()
			}
			return number
		}
		if number, grouped := digitGroups(text); grouped {
			return number
		}
	}
	digits := leadingInteger.FindString(text)
	if digits == "" {
		return math.NaN()
	}
	number, _ := strconv.ParseFloat(digits, 64)
	return number
}

func digitGroups(text string) (float64, bool) {
	body := text
	if body != "" && (body[0] == '+' || body[0] == '-') {
		body = body[1:]
	}
	mark, start, groups := rune(0), 0, []string{}
	for index, char := range body {
		if !strings.ContainsRune(digitGroupMarks, char) {
			continue
		}
		if mark != 0 && char != mark {
			return 0, false
		}
		mark = char
		groups = append(groups, body[start:index])
		start = index + utf8.RuneLen(char)
	}
	if mark == 0 {
		return 0, false
	}
	groups = append(groups, body[start:])
	for index, group := range groups {
		size := len(group)
		if strings.Trim(group, "0123456789") != "" || index == 0 && (size < 1 || size > 3) || index > 0 && size != 3 {
			return 0, false
		}
	}
	number, err := strconv.ParseFloat(text[:len(text)-len(body)]+strings.Join(groups, ""), 64)
	return number, err == nil
}

// claudeDecimal reads a number the way Claude Code reads CLAUDE_AUTOCOMPACT_PCT_OVERRIDE, as JavaScript's
// parseFloat does: the number the text starts with (60% is 60); NaN when it starts with none.
func claudeDecimal(value string) float64 {
	text := strings.TrimLeftFunc(value, javaScriptSpace)
	if text == "" {
		return math.NaN()
	}
	number, err := strconv.ParseFloat(leadingDecimal.FindString(text), 64)
	if err != nil {
		return math.NaN()
	}
	return number
}

func sharedSettings() object {
	if settings := readJSONShared(files.settings); settings.ok {
		return settings.data
	}
	return nil
}

func envVariable(env object, name string) string {
	if value := getString(env, name); value != "" {
		return value
	}
	return os.Getenv(name)
}

// windowVariable is the window CLAUDE_CODE_AUTO_COMPACT_WINDOW sets, from settings.json or the process, as
// Claude Code takes it: 100000 to 1000000, a value outside raised or lowered to the nearer end; not set
// when the variable holds no positive number.
func windowVariable(env object) (float64, bool) {
	text := envVariable(env, autoCompactWindowVar)
	if text == "" {
		return 0, false
	}
	value := claudeInteger(text)
	if math.IsNaN(value) || value <= 0 {
		return 0, false
	}
	return math.Max(autoCompactWindowMin, math.Min(value, autoCompactWindowMax)), true
}

func windowVariableText(env object) string {
	value, _ := windowVariable(env)
	text := strings.TrimSpace(envVariable(env, autoCompactWindowVar))
	if text == formatNumber(value) {
		return autoCompactWindowVar + "=" + text
	}
	return autoCompactWindowVar + "=" + text + " (" + formatNumber(value) + ")"
}

// percentVariable is the percent of the window CLAUDE_AUTOCOMPACT_PCT_OVERRIDE has Claude Code compact at,
// from settings.json or the process: more than 0 and at most 100, or not set.
func percentVariable(env object) (float64, bool) {
	percent := claudeDecimal(envVariable(env, autoCompactPercentVar))
	return percent, percent > 0 && percent <= 100
}

func percentVariableText(env object) string {
	return autoCompactPercentVar + "=" + strings.TrimSpace(envVariable(env, autoCompactPercentVar))
}

// leftoverPercent tells whether the CLAUDE_AUTOCOMPACT_PCT_OVERRIDE of this process is the one setup or
// ensure took back from settings.json. Claude Code puts the env of settings.json into its environment as it
// starts and keeps a value taken away later until it exits, and the hooks it starts inherit it: that value
// is noctis's own, not one the user set.
func leftoverPercent(config, env object) bool {
	process := strings.TrimSpace(os.Getenv(autoCompactPercentVar))
	return getString(env, autoCompactPercentVar) == "" && process != "" && process == getString(config, takenPercentKey)
}

// personsPercent tells whether CLAUDE_AUTOCOMPACT_PCT_OVERRIDE holds a percent noctis did not write, in
// settings.json or in the process. That percent then decides where Claude Code compacts: setup and ensure put
// the percentWindow beside it in place of the window for a token count, so that it holds for every model.
func personsPercent(config, env object) bool {
	_, set := percentVariable(env)
	return set && strings.TrimSpace(envVariable(env, autoCompactPercentVar)) != getString(config, managedPercentKey) && !leftoverPercent(config, env)
}

// autoCompactOff tells whether Claude Code compacts no session by itself, and what says so:
// DISABLE_AUTO_COMPACT or DISABLE_COMPACT, or autoCompactEnabled false in the settings, or in Claude Code's
// global config where the settings do not set it.
func autoCompactOff(settings object) (string, bool) {
	env := getMap(settings, "env")
	for _, name := range []string{"DISABLE_AUTO_COMPACT", "DISABLE_COMPACT"} {
		if switchIsOn(envVariable(env, name)) {
			return variableFrom(settings, name, name), true
		}
	}
	enabled, set := settings["autoCompactEnabled"].(bool)
	if set && !enabled {
		return settingFrom(settings, "autoCompactEnabled", "autoCompactEnabled false"), true
	}
	if !set && globalCompactionOff() {
		return forwardSlashes(globalConfigFile()) + ": autoCompactEnabled false", true
	}
	return "", false
}

// compactSwitchOff finds autoCompactEnabled false in a file before a parse tells whether it is the file's own.
var compactSwitchOff = lazyRegexp(`"autoCompactEnabled"\s*:\s*false`)

// globalCompactionOff tells whether Claude Code's global config turns its compaction off, as it still does
// for a person who turned it off before Claude Code kept the switch in settings.json. That config is the
// legacy .config.json in the account's directory where there is one, else .claude.json in the home
// directory or in the one CLAUDE_CONFIG_DIR names (.claude-custom-oauth.json under
// CLAUDE_CODE_CUSTOM_OAUTH_URL). It can run to megabytes, and the status line and each prompt ask, so the
// answer is kept with the file's size and modification time and the file is read again only once they
// change; only a file that holds the switch turned off is parsed.
func globalCompactionOff() bool {
	file := globalConfigFile()
	if file == "" {
		return false
	}
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if time.Since(info.ModTime()) < compactionSwitchSettle {
		return compactionSwitchedOff(file)
	}
	seen := fmt.Sprintf("%s|%d|%d", file, info.Size(), info.ModTime().UnixNano())
	compactionSwitch.Lock()
	defer compactionSwitch.Unlock()
	if compactionSwitch.seen == seen {
		return compactionSwitch.off
	}
	record := filepath.Join(files.guardDir, compactionSwitchFile)
	kept := readJSONShared(record)
	off, known := kept.data["off"].(bool)
	if !kept.ok || !known || getString(kept.data, "seen") != seen {
		off = compactionSwitchedOff(file)
		if dir, err := os.Stat(files.guardDir); files.guardDir != "" && err == nil && dir.IsDir() {
			_ = writeEncodedAtomic(record, marshalPretty(object{"seen": seen, "off": off}))
		}
	}
	compactionSwitch.seen, compactionSwitch.off = seen, off
	return off
}

// compactionSwitchFile keeps globalCompactionOff's last answer for the next noctis process, under the global
// config's path, size and modification time it was read at, and compactionSwitch keeps it in this one.
const compactionSwitchFile = "compaction-switch.json"

// compactionSwitchSettle is how long the global config must have gone unmodified for its answer to be kept.
// A file written again within its timestamp's granularity (two seconds on some file systems) at the same
// size keeps the size and modification time the answer was kept under, so a newer one is read each time.
const compactionSwitchSettle = 2 * time.Second

var compactionSwitch struct {
	sync.Mutex
	seen string
	off  bool
}

// globalConfigFile is the file Claude Code reads its global config from for the account, or "".
func globalConfigFile() string {
	if files.configDir == "" {
		return ""
	}
	if legacy := filepath.Join(files.configDir, ".config.json"); statSafe(legacy) != nil {
		return legacy
	}
	base := files.configDir
	if home := homeDir(); home != "" && os.Getenv(claudeConfigEnv) == "" && base == filepath.Join(home, ".claude") {
		base = home
	}
	suffix := ""
	if strings.TrimFunc(os.Getenv("CLAUDE_CODE_CUSTOM_OAUTH_URL"), javaScriptSpace) != "" {
		suffix = "-custom-oauth"
	}
	return filepath.Join(base, ".claude"+suffix+".json")
}

// compactionSwitchedOff reads whether the global config in file turns Claude Code's compaction off.
func compactionSwitchedOff(file string) bool {
	content, err := os.ReadFile(file)
	var config object
	if err != nil || !compactSwitchOff.Match(content) || jsonUnmarshalObject(bytes.TrimPrefix(content, utf8BOM), &config) != nil {
		return false
	}
	enabled, set := config["autoCompactEnabled"].(bool)
	return set && !enabled
}

// windowSetting is the window an autoCompactWindow value sets, where Claude Code takes the value: a whole
// number of tokens from 100000 to 1000000. Claude Code sets any other value aside as if it were not there.
func windowSetting(value any) (float64, bool) {
	number, ok := value.(float64)
	return number, ok && number == math.Trunc(number) && number >= autoCompactWindowMin && number <= autoCompactWindowMax
}

// ownWindowSetting tells whether a model's own autoCompactWindow is one Claude Code takes: "auto" or a window.
func ownWindowSetting(value any) bool {
	_, window := windowSetting(value)
	return window || value == "auto"
}

// settingsCompactWindow is the window autoCompactWindow in settings sets for sessions on model, read as
// Claude Code reads it: the model's own entry in modelSettings before the top-level value. "auto" in the
// entry that applies leaves the window to Claude Code; an entry Claude Code does not take leaves the
// top-level value to apply.
func settingsCompactWindow(settings object, model string) (float64, bool) {
	setting := settings["autoCompactWindow"]
	if models := getMap(settings, "modelSettings"); ownWindows(models) && model != "" {
		naming := modelNamingOf(settings)
		if _, own, found := modelCompactWindowIn(models, naming, naming.key(model), nil); found {
			setting = own
		}
	}
	return windowSetting(setting)
}

// modelCompactWindowIn is the entry of models, a modelSettings, whose autoCompactWindow applies to the model
// Claude Code files under key, and its value, as Claude Code picks it: the entry under key itself, else the
// first other name of the model in order, the order of the names in the file models comes from (nil for
// settings.json, which is read for it). It passes over the entries Claude Code does not take.
func modelCompactWindowIn(models object, naming modelNaming, key string, order []string) (string, any, bool) {
	if key == "" {
		return "", nil, false
	}
	names := []string{}
	for _, name := range sortedKeys(models) {
		own, present := getMap(models, name)["autoCompactWindow"]
		if !present || !ownWindowSetting(own) || naming.key(name) != key {
			continue
		}
		if name == key {
			return name, own, true
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return "", nil, false
	}
	chosen := names[0]
	if order == nil {
		chosen = firstInFile("modelSettings", models, names)
	} else if index := slices.IndexFunc(order, func(name string) bool { return slices.Contains(names, name) }); index >= 0 {
		chosen = order[index]
	}
	return chosen, getMap(models, chosen)["autoCompactWindow"], true
}

// ownWindows tells whether an entry of models, a modelSettings, has an autoCompactWindow Claude Code takes.
// Most have none (noctis keeps each model's effort there), and the name Claude Code files a model under,
// which only such an entry needs, costs the status line and each prompt more than all else they reckon.
func ownWindows(models object) bool {
	for name := range models {
		if own, present := getMap(models, name)["autoCompactWindow"]; present && ownWindowSetting(own) {
			return true
		}
	}
	return false
}

// usablePercentPoint is how many tokens of context Claude Code compacts at, at percent of a window that
// leaves usable tokens beside the summary's share.
func usablePercentPoint(usable, percent float64) float64 {
	return math.Min(math.Floor(usable*(percent/100)), usable-compactionBufferTokens)
}

// replyTokens is how much of the window Claude Code keeps for the summary: 20000 tokens, or the fewer that
// CLAUDE_CODE_MAX_OUTPUT_TOKENS, from settings.json or the process, lets a reply have.
func replyTokens(env object) float64 {
	limit := claudeInteger(envVariable(env, maxOutputTokensVar))
	if math.IsNaN(limit) || limit <= 0 {
		return compactionReplyTokens
	}
	return math.Min(limit, compactionReplyTokens)
}

// compactionPoint is how many tokens of context a session on model, in a window of window tokens, holds
// when Claude Code compacts it, reckoned as Claude Code reckons it: the window CLAUDE_CODE_AUTO_COMPACT_WINDOW,
// or else autoCompactWindow, sets where it is the smaller, less 20000 tokens kept for the summary (fewer
// where CLAUDE_CODE_MAX_OUTPUT_TOKENS is lower) and 13000 more; CLAUDE_AUTOCOMPACT_PCT_OVERRIDE's percent of
// the window less the summary's share where that comes sooner. on is false while Claude Code compacts
// nothing by itself.
func compactionPoint(settings object, model string, window float64) (point float64, on bool) {
	if _, off := autoCompactOff(settings); off {
		return 0, false
	}
	return pointIn(settings, model, window), true
}

// pointIn is compactionPoint for settings that leave Claude Code compacting by itself.
func pointIn(settings object, model string, window float64) float64 {
	env := getMap(settings, "env")
	if configured, set := windowVariable(env); set {
		window = math.Min(window, configured)
	} else if configured, set := settingsCompactWindow(settings, model); set {
		window = math.Min(window, configured)
	}
	usable := window - replyTokens(env)
	if percent, set := percentVariable(env); set {
		return usablePercentPoint(usable, percent)
	}
	return usable - compactionBufferTokens
}

// pointsAfter are where Claude Code compacts in a 1M and in a 200k window once it starts with data as its
// settings.json and env as the env there, for the note setup or ensure gives as it writes them.
func pointsAfter(data, env object) (string, string) {
	settings := object{"autoCompactWindow": data["autoCompactWindow"], "env": env}
	return approxCount(pointIn(settings, "", 1e6)), approxCount(pointIn(settings, "", 2e5))
}

// pointShare is how full a session's context is, in percent of where Claude Code compacts it: of the
// compaction point where the session reported its token count and window and Claude Code compacts by
// itself, else of the whole window, as the status line reported it.
func pointShare(settings object, model string, fill contextFill) (float64, bool) {
	if fill.tokens > 0 && fill.window > 0 {
		if point, on := compactionPoint(settings, model, fill.window); on && point > 0 {
			return 100 * fill.tokens / point, true
		}
	}
	return fill.percent, fill.known
}

func ownGuardPercent(cfg object) bool {
	configured, ok := getNumber(section(cfg, "compaction"), "contextPercent")
	return ok && configured > 0 && configured <= 100 && configured != compactionContextPct
}

func compactionNear(cfg object, model string, fill contextFill) bool {
	settings := compactionSettings(fill.dir)
	if _, off := autoCompactOff(settings); off {
		return false
	}
	if fill.tokens > 0 && fill.window > 0 && !ownGuardPercent(cfg) {
		if point, on := compactionPoint(settings, model, fill.window); on && point > 0 {
			return fill.tokens >= compactionGuardShare*point
		}
	}
	return fill.known && fill.percent >= compactionGuardPercent(cfg, settings)
}

func ownCompactWindow(config, data object) bool {
	set, recorded := getNumber(config, managedWindowKey)
	current, present := data["autoCompactWindow"].(float64)
	return recorded && present && current == set
}

func ownCompactPercent(config, env object) bool {
	set := getString(config, managedPercentKey)
	return set != "" && getString(env, autoCompactPercentVar) == set
}

func takeBackCompactWindow(config, data object) bool {
	if !ownCompactWindow(config, data) {
		return false
	}
	delete(data, "autoCompactWindow")
	return true
}

// takeBackCompactPercent removes the CLAUDE_AUTOCOMPACT_PCT_OVERRIDE setup or ensure wrote, while it still
// holds that value, and keeps the value in config: a Claude Code started before goes on with it.
func takeBackCompactPercent(config, env object) bool {
	if !ownCompactPercent(config, env) {
		return false
	}
	config[takenPercentKey] = env[autoCompactPercentVar]
	delete(env, autoCompactPercentVar)
	return true
}

func forgetRecord(config object, key string) bool {
	_, recorded := config[key]
	delete(config, key)
	return recorded
}

// takeBackOtherCompaction takes back what setup or ensure wrote for a compaction point other than aim's:
// autoCompactWindow once aim is off, CLAUDE_AUTOCOMPACT_PCT_OVERRIDE when it is no percent, each while it
// still holds the value written. It tells whether it took a value or a record away.
func takeBackOtherCompaction(config, data, env object, aim compactAim) (taken, changed bool) {
	if aim.tokens == 0 && aim.percent == 0 {
		taken = takeBackCompactWindow(config, data)
		changed = forgetRecord(config, managedWindowKey) || taken
	}
	if aim.percent == 0 {
		percent := takeBackCompactPercent(config, env)
		changed = forgetRecord(config, managedPercentKey) || percent || changed
		taken = taken || percent
	}
	return taken, changed
}

// wireCompaction has setup put Claude Code's compaction point where compaction.compactAt, or --compact-at,
// asks: autoCompactWindow for a token count; CLAUDE_AUTOCOMPACT_PCT_OVERRIDE for a percent of each model's
// window, with the percentWindow beside it, as beside a percent of the user's own; neither for off, taking
// back what it wrote for another choice. A value the user set stays.
func wireCompaction(config, data, env object) string {
	if values := args.values["compact-at"]; len(values) > 0 {
		aim, _ := compactAtOf(values[len(values)-1])
		compaction := section(config, "compaction")
		compaction["compactAt"] = aim.setting()
		config["compaction"] = compaction
	}
	aim := leanPolicyOf(config).compactAt
	yours := personsPercent(config, env)
	taken, _ := takeBackOtherCompaction(config, data, env, aim)
	switch {
	case aim.tokens > 0 && yours:
		if note := placeCompactWindow(config, data, env, percentWindow); note != "" {
			return note
		}
		return T("install.compactAtPctYours", formatNumber(percentWindow), percentVariableText(env))
	case aim.tokens > 0:
		window := compactWindowFor(aim.tokens)
		if note := placeCompactWindow(config, data, env, window); note != "" {
			return note
		}
		return T("install.compactAt", formatNumber(window), approxCount(aim.tokens))
	case aim.percent > 0:
		whole := placeCompactWindow(config, data, env, percentWindow)
		if whole == "" {
			whole = T("install.compactAtWhole", formatNumber(percentWindow))
		}
		return wireCompactPercent(config, data, env, aim.percent) + "\n" + whole
	case taken:
		return T("install.compactAtRemoved")
	}
	return T("install.compactAtOff")
}

// placeCompactWindow has setup write window in autoCompactWindow, and record it, unless
// CLAUDE_CODE_AUTO_COMPACT_WINDOW or a window of the user's own decides: "" once it wrote, else what decides.
func placeCompactWindow(config, data, env object, window float64) string {
	if _, set := windowVariable(env); set {
		return T("install.compactAtEnv", windowVariableText(env))
	}
	if current, present := data["autoCompactWindow"]; present && !ownCompactWindow(config, data) {
		return T("install.compactAtKept", settingText(current))
	}
	data["autoCompactWindow"] = window
	config[managedWindowKey] = window
	return ""
}

func wireCompactPercent(config, data, env object, percent float64) string {
	if current := envVariable(env, autoCompactPercentVar); current != "" && current != getString(config, managedPercentKey) && !leftoverPercent(config, env) {
		return T("install.compactAtPctKept", current)
	}
	value := formatNumber(percent)
	env[autoCompactPercentVar] = value
	config[managedPercentKey] = value
	delete(config, takenPercentKey)
	wide, narrow := pointsAfter(data, env)
	return T("install.compactAtPct", value, wide, narrow)
}

// settleCompaction moves the settings in data where aim asks, for ensure, and records in config the value
// it writes. Unlike setup it leaves alone a value the user set, a value the user removed after it was
// written (the record holds it still), and a window CLAUDE_CODE_AUTO_COMPACT_WINDOW sets; beside a percent
// of the user's own it puts the percentWindow in place of the window it wrote. It tells what it wrote or
// took back, and whether it changed config or data.
func settleCompaction(config, data object, aim compactAim) (string, bool) {
	env, _ := data["env"].(object)
	yours := personsPercent(config, env)
	_, changed := takeBackOtherCompaction(config, data, env, aim)
	switch {
	case aim.tokens > 0 && yours:
		if !settleCompactWindow(config, data, env, percentWindow) {
			return "", changed
		}
		return T("ensure.compactAtPctYours", percentVariableText(env), formatNumber(percentWindow)), true
	case aim.tokens > 0:
		window := compactWindowFor(aim.tokens)
		if !settleCompactWindow(config, data, env, window) {
			return "", changed
		}
		return T("ensure.compactAt", approxCount(aim.tokens), formatNumber(window)), true
	case aim.percent > 0:
		if _, present := data["env"]; present && env == nil {
			// An env that is not an object is the user's to mend: ensure writes neither the percent nor the
			// window for it.
			return "", changed
		}
		whole := settleCompactWindow(config, data, env, percentWindow)
		written := settleCompactPercent(config, data, env, aim.percent)
		switch {
		case written != nil && whole:
			wide, narrow := pointsAfter(data, written)
			return T("ensure.compactAtPctWhole", formatNumber(aim.percent), formatNumber(percentWindow), wide, narrow), true
		case written != nil:
			wide, narrow := pointsAfter(data, written)
			return T("ensure.compactAtPct", formatNumber(aim.percent), wide, narrow), true
		case whole:
			return T("ensure.compactAtWhole", formatNumber(percentWindow)), true
		}
	}
	return "", changed
}

// settleCompactWindow has ensure write window in autoCompactWindow, and record it, unless
// CLAUDE_CODE_AUTO_COMPACT_WINDOW decides, autoCompactWindow holds window or a value of the user's own, or the
// user removed the window ensure wrote and the record still holds it. It tells whether it wrote.
func settleCompactWindow(config, data, env object, window float64) bool {
	current, present := data["autoCompactWindow"]
	recorded, hasRecord := getNumber(config, managedWindowKey)
	_, variable := windowVariable(env)
	if variable || present && (current == window || !ownCompactWindow(config, data)) || !present && hasRecord && recorded == window {
		return false
	}
	data["autoCompactWindow"] = window
	config[managedWindowKey] = window
	return true
}

// settleCompactPercent has ensure write percent in CLAUDE_AUTOCOMPACT_PCT_OVERRIDE, and record it, unless the
// variable holds it or a percent of the user's own, or the user removed the percent ensure wrote and the
// record still holds it: the env it wrote the percent in, or nil.
func settleCompactPercent(config, data, env object, percent float64) object {
	value := formatNumber(percent)
	current, recorded := envVariable(env, autoCompactPercentVar), getString(config, managedPercentKey)
	if leftoverPercent(config, env) {
		current = ""
	}
	if current != "" && current != recorded || current == value || current == "" && recorded == value {
		return nil
	}
	if env == nil {
		env = object{}
	}
	env[autoCompactPercentVar] = value
	placeSetupObject(data, config, "env", env)
	config[managedPercentKey] = value
	delete(config, takenPercentKey)
	return env
}

// syncCompaction keeps Claude Code's compaction point where compaction.compactAt puts it, each time a
// session starts: on a first install, in an account set up before compactAt, after compactAt changed, and
// once compactAt is off. It reads settings.json first and locks it only when there is something to change.
func syncCompaction(cfg object) string {
	if currentHost().id != "claude" {
		return ""
	}
	aim := leanPolicyOf(cfg).compactAt
	records := object{}
	for _, key := range []string{managedWindowKey, managedPercentKey, takenPercentKey} {
		if value, present := cfg[key]; present {
			records[key] = value
		}
	}
	preview := copyObject(sharedSettings())
	if preview == nil {
		preview = object{}
	}
	if _, changed := settleCompaction(records, preview, aim); !changed {
		return ""
	}
	note, recorded := "", strictRead{}
	if withSettings(func(data object) bool {
		stored := readConfigStrict(files.config)
		if !stored.ok || stored.data == nil {
			return false
		}
		text, changed := settleCompaction(stored.data, data, aim)
		if !changed {
			return false
		}
		if err := writeJSONAtomic(files.config, stored.data); err != nil {
			warn("ensure: the compaction point in settings.json was left as it was: config.json could not record it (%v)", err)
			return false
		}
		note, recorded = text, stored
		return true
	}) {
		return note
	}
	// settings.json was not written: the records go back to what it holds, or the next start would take the value
	// they name for one the user removed, or a value of noctis's they forgot for the user's. A change that never
	// reached config.json leaves recorded empty, which putBack leaves alone.
	putBack(files.config, recorded)
	return ""
}

// compactionSource names the settings that move Claude Code's compaction point for every model: the
// window variable or autoCompactWindow, and the percent override, each after the file it comes from where
// that is not the person's settings.json; "" when none does.
func compactionSource(settings object) string {
	env := getMap(settings, "env")
	parts := []string{}
	if _, set := windowVariable(env); set {
		parts = append(parts, variableFrom(settings, autoCompactWindowVar, windowVariableText(env)))
	} else if value, set := settingsCompactWindow(settings, ""); set {
		parts = append(parts, settingFrom(settings, "autoCompactWindow", "autoCompactWindow "+formatNumber(value)))
	}
	if percent, set := percentVariable(env); set {
		parts = append(parts, variableFrom(settings, autoCompactPercentVar, autoCompactPercentVar+"="+formatNumber(percent)))
	}
	return strings.Join(parts, ", ")
}

// modelCompactions are the modelSettings entries that give a model a window of its own, as /autocompact
// saves them, each with the text that names it: for each model, the one entry Claude Code takes.
func modelCompactions(settings object) [][2]string {
	models := getMap(settings, "modelSettings")
	entries := [][2]string{}
	if len(models) == 0 {
		return entries
	}
	naming, keys := modelNamingOf(settings), map[string]bool{}
	for _, name := range sortedKeys(models) {
		key := naming.key(name)
		if key == "" || keys[key] {
			continue
		}
		keys[key] = true
		if chosen, value, found := modelCompactWindowIn(models, naming, key, nil); found {
			entries = append(entries, [2]string{chosen, settingFrom(settings, "modelSettings."+chosen, "modelSettings."+chosen+".autoCompactWindow "+settingText(value))})
		}
	}
	slices.SortFunc(entries, func(a, b [2]string) int { return strings.Compare(a[0], b[0]) })
	return entries
}

// settingText is a value of settings.json as a message names it: a number in plain digits (1000000, not 1e+06).
func settingText(value any) string {
	if number, isNumber := value.(float64); isNumber {
		return formatNumber(number)
	}
	return fmt.Sprint(value)
}

// compactionPoints are where Claude Code compacts a session on model in a 1M and in a 200k window.
func compactionPoints(settings object, model string) (string, string) {
	wide, _ := compactionPoint(settings, model, 1e6)
	narrow, _ := compactionPoint(settings, model, 2e5)
	return approxCount(wide), approxCount(narrow)
}

// compactWindowStatusLines say where Claude Code compacts a session started in the working directory.
func compactWindowStatusLines() []string {
	settings := compactionSettings(workingDir())
	if why, off := autoCompactOff(settings); off {
		return []string{T("status.compactAtDisabled", why)}
	}
	lines := []string{}
	if source := compactionSource(settings); source != "" {
		wide, narrow := compactionPoints(settings, "")
		lines = append(lines, T("status.compactAt", wide, narrow, source))
	}
	for _, entry := range modelCompactions(settings) {
		wide, narrow := compactionPoints(settings, entry[0])
		lines = append(lines, T("status.compactAt", wide, narrow, entry[1]))
	}
	return lines
}

// compactionMissing names the setting compaction.compactAt asks for that Claude Code does not have, or "":
// a token count asks for a window, and a percent for the percent and a window beside it.
func compactionMissing(settings object, aim compactAim) string {
	env := getMap(settings, "env")
	if _, set := percentVariable(env); aim.percent > 0 && !set {
		return autoCompactPercentVar
	}
	_, variable := windowVariable(env)
	if _, window := settingsCompactWindow(settings, ""); (aim.tokens > 0 || aim.percent > 0) && !variable && !window {
		return "autoCompactWindow"
	}
	return ""
}

// compactWindowDoctorLines say where Claude Code compacts a session started in the working directory, and
// what of compaction.compactAt the person's settings.json, which setup writes it to, lacks.
func compactWindowDoctorLines(cfg object) []string {
	settings := compactionSettings(workingDir())
	if why, off := autoCompactOff(settings); off {
		return []string{checkLine(true, T("doctor.compactAtDisabled", why))}
	}
	aim := leanPolicyOf(cfg).compactAt
	lines := []string{}
	if source := compactionSource(settings); source != "" {
		wide, narrow := compactionPoints(settings, "")
		lines = append(lines, checkLine(true, T("doctor.compactAt", wide, narrow, source)))
	} else if aim.tokens == 0 && aim.percent == 0 {
		lines = append(lines, checkLine(true, T("doctor.compactAtOff")))
	}
	for _, entry := range modelCompactions(settings) {
		wide, narrow := compactionPoints(settings, entry[0])
		lines = append(lines, checkLine(true, T("doctor.compactAt", wide, narrow, entry[1])))
	}
	if missing := compactionMissing(sharedSettings(), aim); missing != "" {
		lines = append(lines, fixLine(false, T("doctor.compactAtMissing", fmt.Sprint(aim.setting()), missing), "doctor.fixCompactAt")...)
	}
	return lines
}
