package main

import (
	"fmt"
	"strings"
)

var fanOutPatterns = []*lazyRe{
	lazyRegexp(`(?i)\b(every|all|each)\b(?:\s+\S+){0,3}\s+(files?|endpoints?|components?|modules?|tests?|routes?|handlers?|packages?|services?|pages?|screens?|functions?|classes?|tables?|models?)\b`),
	lazyRegexp(`(?i)\b(across|throughout)\s+the\s+(whole\s+|entire\s+)?(repo|repository|codebase|project|monorepo)\b`),
	lazyRegexp(`(?i)\b(migrate|convert|port|upgrade|refactor|audit|sweep|review|scan)\b(?:\s+\S+){0,4}\s+(\d{2,}|dozens|hundreds)\b`),
	lazyRegexp(`(?i)\b(codebase|repo)[- ]wide\b`),
	lazyRegexp(`(?i)\b(tüm|bütün|her)\s+(?:\S+\s+){0,3}(dosya\S*|bileşen\S*|modül\S*|test\S*|endpoint\S*|servis\S*|sayfa\S*|fonksiyon\S*|sınıf\S*|tablo\S*)`),
	lazyRegexp(`(?i)\b(repo|kod taban|proje)\S*\s+(genelinde|tamamında|baştan sona)`),
	lazyRegexp(`(?i)\b(\d{2,})\s+(dosya|bileşen|modül|test|endpoint|servis)`),
}

var workflowKeywords = lazyRegexp(`(?i)\b(ultracode|workflow|iş akışı|/deep-research)\b`)

func looksLikeFanOut(text string) bool {
	if text == "" || workflowKeywords.MatchString(text) {
		return false
	}
	for _, pattern := range fanOutPatterns {
		if pattern.MatchString(text) {
			return true
		}
	}
	return false
}

func workflowCfg(cfg object) object {
	return section(cfg, "workflow")
}

func workflowAdvice(cfg object, subject string, usage usageView) string {
	roles := section(cfg, "roles")
	state, now := readState(), nowSec()
	spec := func(role, fallback string) string {
		entry := getMap(roles, role)
		model := orDefault(getString(entry, "model"), fallback)
		effort := appliedEffort(role, object{"model": model, "effort": getString(entry, "effort")})
		if safe := scopedSafeModel(cfg, state, usage, model, now); safe != model {
			model, effort = safe, appliedEffort("fallback", object{"model": safe, "effort": getString(getMap(roles, "fallback"), "effort")})
		}
		if effort != "" {
			return model + " (effort " + effort + ")"
		}
		return model
	}
	size := orDefault(getString(workflowCfg(cfg), "size"), "medium")
	return fmt.Sprintf(`[noctis] %s looks like a fan-out task: run it as a dynamic workflow (ultracode) instead of working item by item — one agent per unit, results verified before they are reported, size guideline %s. Agent models: code-writing agents → %s; read-only analysis and review agents → %s; discovery/search agents → %s; test runs and other noisy verification → %s. Give parallel editors isolated copies (worktrees) so their edits never collide, and keep the run's script path: if the run is interrupted, relaunch that same script (completed agents return saved results) rather than starting a new run.`, subject, size, spec("code", getString(section(cfg, "models"), "primary")), spec("research", "opus"), spec("explore", "haiku"), spec("digest", "haiku"))
}

func workflowAdvisable(cfg object, result decision) bool {
	if !getBool(workflowCfg(cfg), "suggest", true) || result.wait != nil || result.warnWindow != nil || result.fableHit {
		return false
	}
	return gateWorkflowLaunch(cfg, result) == ""
}

func suggestWorkflow(cfg object, prompt string, result decision) string {
	if !looksLikeFanOut(prompt) || !workflowAdvisable(cfg, result) {
		return ""
	}
	return workflowAdvice(cfg, "This request", result.usage)
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

func gateWorkflowLaunch(cfg object, result decision) string {
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
		if room, label := headroomLeft(cfg, result.usage); room < needed {
			return fmt.Sprintf("[noctis] only %s points of the %s window are left before the pause point, and a workflow fans out many agents at once: between two checks they can burn through the rest, and past the subscription limit the account pays for the overflow in usage credits. A workflow needs %s points of room. Do this as a single-agent job now, or launch the workflow after the reset.", formatNumber(roundTo(room, 1)), label, formatNumber(needed))
		}
	}
	return ""
}
