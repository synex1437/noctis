package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

var builtinLean = object{"lean": true, "compactAtPercent": float64(70), "keepTurns": float64(6), "maxToolResultChars": float64(2000), "instructions": ""}

var leanKeys = []string{"lean", "compactAtPercent", "keepTurns", "maxToolResultChars", "instructions"}

var leanDecimal = lazyRegexp(`^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$`)

const leanClaudeMin = "2.1.281"

type leanPolicy struct {
	on        bool
	compactAt float64
}

func leanNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, !math.IsInf(typed, 0) && !math.IsNaN(typed)
	case string:
		text := strings.TrimSpace(typed)
		if !leanDecimal.MatchString(text) {
			return 0, false
		}
		number, err := strconv.ParseFloat(text, 64)
		return number, err == nil
	}
	return 0, false
}

func leanSwitchedOff(value any) bool {
	if value == nil {
		return true
	}
	if flag, ok := value.(bool); ok {
		return !flag
	}
	number, ok := leanNumber(value)
	return ok && number == 0
}

func leanValueValid(key string, value any) bool {
	number, isNumber := leanNumber(value)
	switch key {
	case "lean":
		_, ok := value.(bool)
		return ok
	case "compactAtPercent":
		return leanSwitchedOff(value) || (isNumber && number > 0 && number <= 100)
	case "keepTurns":
		return isNumber && number == math.Trunc(number) && number >= 0
	case "maxToolResultChars":
		return isNumber && number == math.Trunc(number) && number >= 100
	case "instructions":
		_, ok := value.(string)
		return ok
	}
	return false
}

func repairCompaction(merged, defaults object) {
	shipped := getMap(defaults, "compaction")
	repaired := []string{}
	if _, present := merged["compaction"]; present && getMap(merged, "compaction") == nil {
		merged["compaction"] = mergeDefaults(shipped, object{})
		repaired = append(repaired, "compaction")
	}
	compaction := getMap(merged, "compaction")
	if compaction == nil {
		return
	}
	for _, key := range leanKeys {
		value, present := compaction[key]
		if !present || leanValueValid(key, value) {
			continue
		}
		if fallback, ok := shipped[key]; ok && leanValueValid(key, fallback) {
			compaction[key] = fallback
		} else {
			compaction[key] = builtinLean[key]
		}
		repaired = append(repaired, "compaction."+key)
	}
	if len(repaired) > 0 {
		merged["compactionRepaired"] = strings.Join(repaired, ", ")
	}
}

func repairedCompaction(cfg object) []string {
	names := getString(cfg, "compactionRepaired")
	if names == "" {
		return nil
	}
	return strings.Split(names, ", ")
}

func leanPolicyOf(cfg object) leanPolicy {
	compaction := section(cfg, "compaction")
	value := func(key string) any {
		if own, present := compaction[key]; present && leanValueValid(key, own) {
			return own
		}
		return builtinLean[key]
	}
	policy := leanPolicy{}
	policy.on, _ = value("lean").(bool)
	if at := value("compactAtPercent"); !leanSwitchedOff(at) {
		policy.compactAt, _ = leanNumber(at)
	}
	return policy
}

func leanDescription(cfg object) string {
	policy := leanPolicyOf(cfg)
	switch {
	case !policy.on:
		return T("lean.off")
	case policy.compactAt == 0:
		return T("lean.noEarly")
	}
	return T("lean.early", T("badge.percent", int(math.Round(policy.compactAt))))
}

func leanDoctorLines(cfg object) []string {
	if repaired := repairedCompaction(cfg); len(repaired) > 0 {
		return fixLine(false, T("doctor.leanFixed", strings.Join(repaired, ", ")), "doctor.fixLean")
	}
	lines := []string{checkLine(true, T("doctor.lean", leanDescription(cfg)))}
	if leanPolicyOf(cfg).on {
		value := leanSwitchValue()
		lines = append(lines, fixLine(switchIsOn(value), T("doctor.leanSwitch", orDefault(value, T("doctor.none"))), "doctor.fixLeanSwitch")...)
		if sid, version := lastClaudeSession(); claudeTooOldForLean(version) && !leanRanIn(sid) {
			lines = append(lines, fixLine(false, T("doctor.leanOld", version, leanClaudeMin), "doctor.fixLeanOld")...)
		}
	}
	return lines
}

func claudeTooOldForLean(version string) bool {
	match := versionPattern.FindStringSubmatch(version)
	return match != nil && compareVersions(match[1], leanClaudeMin) < 0
}

func lastClaudeSession() (string, string) {
	sid, version, latest := "", "", -1.0
	for key, raw := range getMap(readJSON(files.usage), "sessions") {
		info, _ := raw.(object)
		reported, at := getString(info, "version"), numberOr(info, "updatedAt", 0)
		if reported != "" && (at > latest || at == latest && compareVersions(reported, version) > 0) {
			sid, version, latest = key, reported, at
		}
	}
	return sid, version
}

const leanLogLines = 200

func compactLogFile() string {
	return filepath.Join(files.guardDir, "compact.log")
}

func leanCompactions(count int) []object {
	entries := []object{}
	for _, line := range tailFileLines(compactLogFile(), count) {
		var entry object
		if jsonUnmarshalObject([]byte(line), &entry) == nil && entry != nil {
			entries = append(entries, entry)
		}
	}
	return entries
}

func approxCount(value float64) string {
	switch {
	case value >= 1e6:
		return strconv.FormatFloat(value/1e6, 'f', 1, 64) + "M"
	case value >= 1e3:
		return formatNumber(math.Round(value/1e3)) + "k"
	}
	return formatNumber(math.Round(value))
}

func leanTally() string {
	entries := leanCompactions(leanLogLines)
	if len(entries) == 0 {
		return T("lean.trimmedNone")
	}
	total := 0.0
	for _, entry := range entries {
		total += numberOr(entry, "trimmed", 0)
	}
	return T("lean.trimmed", len(entries), approxCount(total))
}

func compactionReason(entry object) string {
	reason := fmt.Sprintf("%s: %s rows, %s changed, %s characters trimmed", orDefault(getString(entry, "trigger"), "?"), formatNumber(numberOr(entry, "rows", 0)), formatNumber(numberOr(entry, "changed", 0)), formatNumber(numberOr(entry, "trimmed", 0)))
	before, hasBefore := getNumber(entry, "tokensBefore")
	after, hasAfter := getNumber(entry, "tokensAfter")
	if hasBefore && hasAfter {
		reason += fmt.Sprintf(", %s → %s tokens", formatNumber(before), formatNumber(after))
	}
	if agent := getString(entry, "agent"); agent != "" {
		reason += ", agent " + agent
	}
	return reason
}

func compactionJournalEntry(entry object) object {
	shaped := cloneObject(entry)
	shaped["event"] = "session.compact"
	shaped["action"] = "compact"
	shaped["reason"] = compactionReason(entry)
	return shaped
}

func leanStatusLines(cfg object) []string {
	lines := []string{T("status.lean", leanDescription(cfg)+leanTally())}
	if repaired := repairedCompaction(cfg); len(repaired) > 0 {
		lines = append(lines, T("status.leanFixed", strings.Join(repaired, ", ")))
	}
	return lines
}

const functionHooksVar = "CLAUDE_CODE_ENABLE_FUNCTION_HOOKS"

func switchIsOn(value any) bool {
	switch typed := value.(type) {
	case string:
		return slices.Contains([]string{"1", "true", "yes", "on"}, strings.ToLower(strings.TrimSpace(typed)))
	case bool:
		return typed
	case float64:
		return typed == 1
	}
	return false
}

func ownLeanSwitch(config, env object) bool {
	managed := getMap(config, "managedFunctionHooks")
	value, present := env[functionHooksVar]
	return managed != nil && present && value == managed["set"]
}

func takeBackLeanSwitch(config, env object) bool {
	if !ownLeanSwitch(config, env) {
		return false
	}
	if previous := getMap(config, "managedFunctionHooks")["previous"]; previous != nil {
		env[functionHooksVar] = previous
	} else {
		delete(env, functionHooksVar)
	}
	return true
}

func wireLeanSwitch(config, env object) string {
	if args.present["no-lean"] {
		compaction := section(config, "compaction")
		compaction["lean"] = false
		config["compaction"] = compaction
	}
	if !leanPolicyOf(config).on {
		removed := takeBackLeanSwitch(config, env)
		delete(config, "managedFunctionHooks")
		if removed {
			return T("install.leanOffRemoved")
		}
		return T("install.leanOff")
	}
	current, present := env[functionHooksVar]
	switch {
	case !present || current == "":
		config["managedFunctionHooks"] = object{"previous": current, "set": "1"}
		env[functionHooksVar] = "1"
	case !switchIsOn(current):
		return T("install.leanKept", fmt.Sprint(current))
	}
	return T("install.lean")
}

func leanSwitchValue() string {
	settings := readJSONStrict(files.settings)
	if value, present := getMap(settings.data, "env")[functionHooksVar]; present {
		return fmt.Sprint(value)
	}
	return os.Getenv(functionHooksVar)
}

func leanRanIn(sid string) bool {
	for key := range getMap(readJSON(filepath.Join(files.guardDir, "lean.json")), "sessions") {
		if safeName(key) == sid {
			return true
		}
	}
	return false
}

func leanHintDue(cfg object, sid string, percent float64) bool {
	if sid == "" || sid == "unknown" || currentHost().id != "claude" {
		return false
	}
	policy := leanPolicyOf(cfg)
	return policy.on && policy.compactAt > 0 && percent >= policy.compactAt && !leanRanIn(sid)
}

func leanContextText(cfg object, sid string, percent float64) string {
	if leanHintDue(cfg, sid, percent) {
		return T("statusline.ctxCompact", int(math.Round(percent)))
	}
	return T("statusline.ctx", int(math.Round(percent)))
}

func leanNotice(cfg, state object, sid string) string {
	session := getMap(getMap(readJSON(files.usage), "sessions"), sid)
	percent, known := getNumber(session, "context")
	key := "lean:" + sid
	if !known || !leanHintDue(cfg, sid, percent) || getMap(state, "notified")[key] != nil {
		return ""
	}
	updateState(func(next object) { stateMap(next, "notified")[key] = float64(nowSec()) })
	journal(sid, "UserPromptSubmit", "lean-hint", "lean module not running", object{"ctx": percent})
	badge := T("badge.percent", int(math.Round(percent)))
	if version := getString(session, "version"); claudeTooOldForLean(version) {
		return T("lean.noticeOld", badge, version, leanClaudeMin)
	}
	return T("lean.notice", badge)
}
