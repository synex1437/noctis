package main

import (
	"path/filepath"
	"strings"
)

// perModelEffortClaudeMin is the first Claude Code that saves an effort level per model, in the
// modelSettings of settings.json.
const perModelEffortClaudeMin = "2.1.251"

// canonicalModels are the models the profiles' aliases stand for; modelSettings is keyed by these ids.
var canonicalModels = map[string]string{"opus": "claude-opus-5-5", "sonnet": "claude-sonnet-5-5", "fable": "claude-fable-5-1", "haiku": "claude-haiku-5-5"}

var (
	modelDateSuffix  = lazyRegexp(`-\d{8}$`)
	canonicalModelID = lazyRegexp(`^claude-[a-z]+(-\d+)+$`)
)

// savedEffortModel is the modelSettings entry that holds the effort level of model, or "" when model
// names no single model (best, default, opusplan, a provider's own id) or one without effort levels.
func savedEffortModel(model string) string {
	name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(model)), "[1m]")
	if id, alias := canonicalModels[name]; alias {
		return id
	}
	name = modelDateSuffix.ReplaceAllString(name, "")
	if !canonicalModelID.MatchString(name) || !modelTakesEffort(name) {
		return ""
	}
	return name
}

// perModelEffortWorks tells whether the Claude Code of the account in configDir saves an effort level per
// model: the version its status line last reported is new enough, or none was reported yet.
func perModelEffortWorks(configDir string) bool {
	version := getString(readJSON(filepath.Join(configDir, pluginName, "usage.json")), "version")
	match := versionPattern.FindStringSubmatch(version)
	return match == nil || compareVersions(match[1], perModelEffortClaudeMin) >= 0
}

// effortModelFor is the modelSettings entry setup saves effort in for sessions on model, or "" when the
// level goes in env.CLAUDE_CODE_EFFORT_LEVEL instead: max, which no setting can save; a model that names
// no single model; a Claude Code older than perModelEffortClaudeMin. The variable overrides every other
// level, the effort of noctis's agents and a typed /effort too, so only max, which needs it, gets it.
func effortModelFor(configDir, model, effort string) string {
	if effort == "" || effort == "max" || !perModelEffortWorks(configDir) {
		return ""
	}
	return savedEffortModel(model)
}

// modelEffortPlaceable tells whether settings can take a level for model: modelSettings, and its entry
// for model, are objects or absent.
func modelEffortPlaceable(settings object, model string) bool {
	holder, present := settings["modelSettings"]
	if !present {
		return true
	}
	entries, isObject := holder.(object)
	if !isObject {
		return false
	}
	entry, present := entries[model]
	_, isObject = entry.(object)
	return !present || isObject
}

// placeModelEffort saves effort as the level of model in the modelSettings of settings and records in
// config what the entry held before the first setup that saved one, for uninstall to put back.
func placeModelEffort(settings, config object, model, effort string) {
	entries, _ := settings["modelSettings"].(object)
	if entries == nil {
		entries = object{}
	}
	entry, _ := entries[model].(object)
	if entry == nil {
		entry = object{}
	}
	record, recorded := getMap(config, "managedModelEffort")[model].(object)
	records := object{model: object{"previous": valueSetupFound(entry, "effortLevel", record["previous"], recorded), "set": effort}}
	entry["effortLevel"] = effort
	entries[model] = entry
	placeSetupObject(settings, config, "modelSettings", entries)
	config["managedModelEffort"] = records
}

// takeBackModelEfforts puts back, in each modelSettings entry but keep's that records name, the level
// found there before the first setup, while the entry still holds the level setup saved; a level
// changed since then is the user's and stays. An entry left with nothing in it goes.
func takeBackModelEfforts(settings, records object, keep string) {
	entries, _ := settings["modelSettings"].(object)
	for model, raw := range records {
		record, _ := raw.(object)
		entry, _ := entries[model].(object)
		if model == keep || entry == nil || getString(entry, "effortLevel") != getString(record, "set") {
			continue
		}
		if previous := record["previous"]; previous != nil {
			entry["effortLevel"] = previous
			continue
		}
		delete(entry, "effortLevel")
		if len(entry) == 0 {
			delete(entries, model)
		}
	}
}

func effortDoctorLines(cfg, settings object) []string {
	none := T("doctor.none")
	env := getString(getMap(settings, "env"), "CLAUDE_CODE_EFFORT_LEVEL")
	want := getString(section(cfg, "models"), "effort")
	model := effortModelFor(files.configDir, getString(settings, "model"), want)
	// Setup saved the level for the model settings.json ran on, which can be the user's own; max and no
	// level at all go back to the variable whatever setup saved before.
	if want != "" && want != "max" {
		for recorded := range getMap(cfg, "managedModelEffort") {
			model = recorded
		}
	}
	if model == "" {
		text, ok := T("doctor.effort", orDefault(env, none)), env == want || want == ""
		if !ok {
			text = T("doctor.effortMismatch", orDefault(env, none), orDefault(want, none))
		}
		return fixLine(ok, text, "doctor.fixEffort", orDefault(want, none))
	}
	saved := getString(getMap(getMap(settings, "modelSettings"), model), "effortLevel")
	text, ok := T("doctor.effortSaved", model, orDefault(saved, none)), saved == want
	if !ok {
		text = T("doctor.effortSavedWant", model, orDefault(saved, none), want)
	}
	lines := fixLine(ok, text, "doctor.fixEffortSaved", want, model)
	if switched := getMap(readState(), "modelSwitched"); env != "" && (switched == nil || getString(switched, "effortSet") != env) {
		lines = append(lines, fixLine(false, T("doctor.effortOverride", env), "doctor.fixEffortOverride")...)
	}
	return lines
}
