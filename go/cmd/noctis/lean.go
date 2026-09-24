package main

import (
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
)

var builtinLean = object{"lean": true, "compactAtPercent": float64(70), "keepTurns": float64(6), "maxToolResultChars": float64(2000), "instructions": ""}

var leanKeys = []string{"lean", "compactAtPercent", "keepTurns", "maxToolResultChars", "instructions"}

var leanDecimal = lazyRegexp(`^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$`)

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
	}
	return lines
}

func leanStatusLines(cfg object) []string {
	lines := []string{T("status.lean", leanDescription(cfg))}
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
