package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var fanOutPatterns = []*lazyRe{
	lazyRegexp(`(?i)\b(migrate|convert|port|upgrade|refactor|rewrite|update|audit|review|document|translate|rename)\b.*\b(every|each)\b(?:\s+\S+){0,3}\s+(files?|endpoints?|components?|modules?|tests?|routes?|handlers?|packages?|services?|pages?|screens?|functions?|classes?|tables?|models?)\b`),
	lazyRegexp(`(?i)\b(migrate|convert|port|upgrade|refactor|rewrite|update|audit|review|document|translate|rename)\b.*\b(across|throughout)\s+the\s+(whole\s+|entire\s+)?(repo|repository|codebase|project|monorepo)\b`),
	lazyRegexp(`(?i)\b(migrate|convert|port|upgrade|refactor|rewrite|update|audit|sweep|review|scan|document|translate|rename)\b(?:\s+\S+){0,4}\s+(\d{2,}|dozens|hundreds)(?:\s+\S+){0,2}\s+(files?|endpoints?|components?|modules?|tests?|routes?|handlers?|packages?|services?|pages?|screens?|functions?|classes?|tables?|models?|scripts?|prs?|pull\s+requests?|microservices?|jobs?|repos?|repositories)\b`),
	lazyRegexp(`(?i)\b(migrate|convert|port|upgrade|refactor|rewrite|update|audit|review|document|translate|rename)\b.*\b(codebase|repo)[- ]wide\b`),
	lazyRegexp(`(?i)\b(migrate|convert|port|rewrite|refactor|audit|review|translate)\s+(?:\S+\s+)?all\s+(?:\S+\s+){0,3}(files|endpoints|components|modules|tests|routes|handlers|packages|services|pages|screens|functions|classes|tables|models)\b`),
	lazyRegexp(`(?i)(?:^|\s)(tüm|bütün)\s+(?:\S+\s+){0,3}(dosya\S*|bileşen\S*|modül\S*|test\S*|endpoint\S*|servis\S*|sayfa\S*|fonksiyon\S*|sınıf\S*|tablo\S*).*(?:^|\s)(taşı|dönüştür|çevir|yeniden yaz|incele|denetle)`),
	lazyRegexp(`(?i)\bher\s+(?:\S+\s+){0,3}(dosya\S*|bileşen\S*|modül\S*|test\S*|endpoint\S*|servis\S*|sayfa\S*|fonksiyon\S*|sınıf\S*|tablo\S*).*(?:^|\s)(taşı|dönüştür|çevir|güncelle|yeniden yaz|yeniden adlandır|incele|denetle|belgele|yükselt|refactor)`),
	lazyRegexp(`(?i)\b(repo|kod taban|proje)\S*\s+(genelinde|tamamında|baştan sona).*(?:^|\s)(taşı|dönüştür|çevir|güncelle|yeniden yaz|yeniden adlandır|incele|denetle|belgele|yükselt|refactor)`),
	lazyRegexp(`(?i)\b(\d{2,})\s+(dosya|bileşen|modül|test|endpoint|servis).*(?:^|\s)(taşı|dönüştür|çevir|güncelle|yeniden yaz|yeniden adlandır|incele|denetle|belgele|yükselt|refactor)`),
}

const sourceFileName = `[\w-]+\.(?:go|js|mjs|ts|tsx|jsx|py|rb|rs|java|kt|cs|php|swift|c|cc|cpp|h|vue|svelte)`

var singleFileScope = lazyRegexp(`(?i)\b(?:in|within|inside)\s+(?:this|that|the|one|a|a single)\s+file\b|\b(?:in|within|inside|of)\s+(?:the\s+)?(?:\S*/)?` + sourceFileName + `\b|\b` + sourceFileName + `(?:'[dt][ae]ki|\s+içindeki)|(?:^|\s)(?:bu|şu|o) dosya(?:da|daki|nın|yı|nın içindeki)?(?:\s|$)|\S+ dosyasındaki`)

var herTimeWord = lazyRegexp(`(?i)(?:^|\s)her\s+(?:zaman|gün|sefer|seferinde|hafta|ay)(?:\s|$)`)

var fanOutClauseBreak = lazyRegexp(`(?i)[,;.!?\n]+|\s(?:and|then|ve|sonra|ardından)\s`)

var workflowKeywords = lazyRegexp(`(?i)\b(ultracode|workflow|iş akışı|/deep-research)\b`)

var nestedWorkflowCall = lazyRegexp(`\bworkflow\s*\(`)

var agentTypeOption = lazyRegexp(`\bagentType\b`)

var agentTypeLiteral = lazyRegexp(`\bagentType\b["']?\s*:\s*(?:'([^'\\\r\n]*)'|"([^"\\\r\n]*)")\s*(?:[,})\r\n]|$)`)

func looksLikeFanOut(text string) bool {
	if text == "" || workflowKeywords.MatchString(text) || singleFileScope.MatchString(text) {
		return false
	}
	for _, clause := range fanOutClauseBreak.Split(herTimeWord.ReplaceAllString(text, " "), -1) {
		for _, pattern := range fanOutPatterns {
			if pattern.MatchString(clause) {
				return true
			}
		}
	}
	return false
}

func workflowCfg(cfg object) object {
	return section(cfg, "workflow")
}

type roleModel struct {
	model  string
	effort string
}

func workflowRoleModels(cfg object, usage usageView) []roleModel {
	roles := section(cfg, "roles")
	state, now := readState(), nowSec()
	pick := func(role, fallback string) roleModel {
		entry := getMap(roles, role)
		model := orDefault(getString(entry, "model"), fallback)
		effort := appliedEffort(role, object{"model": model, "effort": getString(entry, "effort")})
		if safe := scopedSafeModel(cfg, state, usage, model, now); safe != model {
			model, effort = safe, appliedEffort("fallback", object{"model": safe, "effort": getString(getMap(roles, "fallback"), "effort")})
		}
		return roleModel{model: model, effort: effort}
	}
	return []roleModel{pick("code", getString(section(cfg, "models"), "primary")), pick("research", "opus"), pick("explore", "haiku"), pick("digest", "haiku")}
}

func roleModelText(role roleModel) string {
	if role.effort != "" {
		return role.model + " (effort " + role.effort + ")"
	}
	return role.model
}

func workflowNotice(cfg object, key string, usage usageView) string {
	models := workflowRoleModels(cfg, usage)
	size := orDefault(getString(workflowCfg(cfg), "size"), "medium")
	return T(key, size, roleModelText(models[0]), roleModelText(models[1]), roleModelText(models[2]), roleModelText(models[3]))
}

func launchedScript(launch object) (string, bool) {
	toolInput := getMap(launch, "tool_input")
	script := ""
	if path := firstString(toolInput, "scriptPath", "script_path"); path != "" {
		if !filepath.IsAbs(path) {
			path = filepath.Join(getString(launch, "cwd"), path)
		}
		info := statSafe(path)
		if info == nil || !info.Mode().IsRegular() || info.Size() > workflowScriptMaxBytes {
			return "", false
		}
		content, err := readFileShared(path)
		if err != nil {
			return "", false
		}
		script = string(content)
	} else if script = getString(toolInput, "script"); script == "" {
		return "", false
	}
	if nestedWorkflowCall.MatchString(script) {
		return "", false
	}
	if args, present := toolInput["args"]; present {
		script += "\n" + string(marshalCompact(args))
	}
	return script, true
}

func fanOutMayUseScoped(cfg object, result decision, launch object) bool {
	scoped := scopedModelPattern(cfg)
	if result.model == "" || scoped.MatchString(result.model) {
		return true
	}
	if launch == nil {
		for _, role := range workflowRoleModels(cfg, result.usage) {
			if scoped.MatchString(role.model) {
				return true
			}
		}
		return false
	}
	script, readable := launchedScript(launch)
	return !readable || scoped.MatchString(script) || agentTypesMayUseScoped(script, scoped)
}

func agentTypesMayUseScoped(script string, scoped *regexp.Regexp) bool {
	literals := agentTypeLiteral.FindAllStringSubmatch(script, -1)
	if len(literals) < len(agentTypeOption.FindAllStringSubmatch(script, -1)) {
		return true
	}
	for _, literal := range literals {
		name, own := strings.CutPrefix(literal[1]+literal[2], pluginName+":")
		if !own || name == "" || strings.ContainsAny(name, `/\:.`) || files.pluginRoot == "" {
			return true
		}
		model, readable := agentFileModel(filepath.Join(files.pluginRoot, "agents", name+".md"))
		if !readable || (model != "" && model != "inherit" && scoped.MatchString(model)) {
			return true
		}
	}
	return false
}

func agentFileModel(file string) (string, bool) {
	content, err := readFileShared(file)
	if err != nil {
		return "", false
	}
	text := strings.ReplaceAll(strings.TrimPrefix(string(content), "\uFEFF"), "\r\n", "\n")
	front, _, closed := strings.Cut(strings.TrimPrefix(text, "---\n"), "\n---")
	if !strings.HasPrefix(text, "---\n") || !closed {
		return "", false
	}
	for _, line := range strings.Split(front, "\n") {
		if key, value, found := strings.Cut(line, ":"); found && strings.TrimRight(key, " \t") == "model" {
			value, _, _ = strings.Cut(value, " #")
			return strings.Trim(strings.TrimSpace(value), `"'`), true
		}
	}
	return "", true
}

func workflowAdvisable(cfg object, result decision) bool {
	if !getBool(workflowCfg(cfg), "suggest", false) || result.wait != nil || result.warnWindow != nil || result.fableHit {
		return false
	}
	return gateWorkflowLaunch(cfg, result, nil) == ""
}

func suggestWorkflow(cfg object, prompt string, result decision) string {
	if !looksLikeFanOut(prompt) || !workflowAdvisable(cfg, result) {
		return ""
	}
	return workflowNotice(cfg, "notice.workflowPrompt", result.usage)
}

func recordWorkflowLaunch(sid string, input object, now int64) {
	toolInput := getMap(input, "tool_input")
	summary := object{"at": float64(now)}
	for _, key := range []string{"name", "script_path", "scriptPath", "path", "script", "description", "workflow", "args"} {
		if value := getString(toolInput, key); value != "" {
			summary[key] = truncateText(value, 600)
		}
	}
	updateState(func(state object) {
		launches, agents := []any{}, []any{}
		for _, raw := range getList(stateMap(state, "workflows"), sid) {
			if getString(toObject(raw), "agent") != "" {
				agents = append(agents, raw)
			} else {
				launches = append(launches, raw)
			}
		}
		launches = append(launches, summary)
		if len(launches) > 3 {
			launches = launches[len(launches)-3:]
		}
		stateMap(state, "workflows")[sid] = append(launches, agents...)
	})
}

func agentCutOffs(state object, sid string) []object {
	records := []object{}
	for _, raw := range getList(getMap(state, "workflows"), sid) {
		if record := toObject(raw); getString(record, "agent") != "" {
			records = append(records, record)
		}
	}
	return records
}

func cutOffLines(state object, sid string) []string {
	lines := []string{}
	for _, record := range agentCutOffs(state, sid) {
		name := strings.TrimSpace(getString(record, "agentType") + " agent " + getString(record, "agent"))
		at := formatTime(numberOr(record, "at", 0))
		if getBool(record, "stopped", false) {
			lines = append(lines, fmt.Sprintf("%s — stopped by the usage limit at %s; its result is partial", name, at))
		} else {
			lines = append(lines, fmt.Sprintf("%s — told at %s to wrap up before the usage limit; its result may be partial", name, at))
		}
	}
	return lines
}

func cutOffNote(state object, sid string) string {
	lines := cutOffLines(state, sid)
	if len(lines) == 0 {
		return ""
	}
	return "[noctis] The usage limit cut agents of this session short, so redo their unfinished part instead of trusting a saved result: " + strings.Join(lines, "; ") + "."
}

func workflowLaunches(state object, sid string) []string {
	lines := []string{}
	for _, raw := range getList(getMap(state, "workflows"), sid) {
		run := toObject(raw)
		if run == nil || getString(run, "agent") != "" {
			continue
		}
		label := orDefault(getString(run, "name"), orDefault(getString(run, "script_path"), orDefault(getString(run, "scriptPath"), orDefault(getString(run, "path"), orDefault(getString(run, "workflow"), "")))))
		if label == "" {
			if script := getString(run, "script"); script != "" {
				label = "script " + truncateText(strings.ReplaceAll(script, "\n", " "), 120)
			} else {
				label = orDefault(getString(run, "description"), "(unnamed)")
			}
		}
		lines = append(lines, fmt.Sprintf("%s — launched %s", label, formatTime(numberOr(run, "at", 0))))
	}
	return lines
}

func workflowRuns(state object, sid string) []string {
	return append(workflowLaunches(state, sid), cutOffLines(state, sid)...)
}

func workflowResumeNote(state object, sid string) string {
	notes := []string{}
	if runs := workflowLaunches(state, sid); len(runs) > 0 {
		notes = append(notes, "[noctis] A dynamic workflow was running in this session ("+strings.Join(runs, "; ")+"). If it did not finish, relaunch it with the same script so completed agents return their saved results; never start it over as a new run, and do not redo work its agents already completed.")
	}
	if note := cutOffNote(state, sid); note != "" {
		notes = append(notes, note)
	}
	return strings.Join(notes, " ")
}

func gateWorkflowLaunch(cfg object, result decision, launch object) string {
	if !getBool(workflowCfg(cfg), "gate", true) {
		return ""
	}
	switch {
	case result.wait != nil:
		return fmt.Sprintf("[noctis] usage is at the pause threshold (%s %s%%); a workflow would fan out many agents into the wall. Wait for the reset at %s — the plugin continues the session on its own — then launch the same script.", result.wait.label, formatNumber(result.wait.used), formatTime(result.wait.until))
	case result.warnWindow != nil:
		return fmt.Sprintf("[noctis] %s usage is %s%% (auto-pause at %s%%): too close to the limit to fan out a workflow. Do the task as a single-agent job now, or wait for the reset at %s and launch the workflow then.", result.warnWindow.label, formatNumber(result.warnWindow.used), formatNumber(result.warnWindow.threshold), formatTime(result.warnWindow.resetsAt))
	case result.fableHit:
		return "[noctis] the scoped model quota is out; the plugin is switching the default model. Launch the workflow after the switch, on the fallback model."
	}
	if needed := fanOutHeadroom(cfg); needed > 0 {
		usage := result.usage
		if !fanOutMayUseScoped(cfg, result, launch) {
			usage.fable = nil
		}
		if room, label := headroomLeft(cfg, usage); room < needed {
			return fmt.Sprintf("[noctis] only %s points of the %s window are left before the pause point, and a workflow fans out many agents at once: between two checks they can burn through the rest, and past the subscription limit the account pays for the overflow in usage credits. A workflow needs %s points of room. Do this as a single-agent job now, or launch the workflow after the reset.", formatNumber(roundTo(room, 1)), label, formatNumber(needed))
		}
	}
	return ""
}
