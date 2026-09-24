package main

import (
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
)

var needsCodePattern = lazyRegexp(`NEEDS_CODE`)

func agentType(cfg object, key, fallback string) string {
	name := getString(section(cfg, "router"), key)
	if name == "" {
		name = fallback
	}
	return pluginName + ":" + name
}

func liteAgentType(cfg object) string {
	return agentType(cfg, "agent", "lite")
}

func digestAgentType(cfg object) string {
	return agentType(cfg, "digestAgent", "digest")
}

func digestEnabled(cfg object) bool {
	return getBool(section(cfg, "router"), "digest", true)
}

var unhookedFileTools = []string{"Edit", "MultiEdit", "NotebookEdit"}

type agentToolRules struct {
	restricted bool
	tools      map[string]bool
	disallowed map[string]bool
}

func (rules agentToolRules) mayCall(tool string) bool {
	return (!rules.restricted || rules.tools[tool]) && !rules.disallowed[tool]
}

func addAgentTools(into map[string]bool, list string) {
	list, _, _ = strings.Cut(list, " #")
	for _, entry := range strings.Split(strings.Trim(strings.TrimSpace(list), "[]"), ",") {
		name, _, _ := strings.Cut(entry, "(")
		if name = strings.Trim(strings.TrimSpace(name), `"'`); name != "" {
			into[name] = true
		}
	}
}

func readAgentToolRules(file string) (agentToolRules, bool) {
	content, err := os.ReadFile(file)
	if err != nil {
		return agentToolRules{}, false
	}
	text := strings.ReplaceAll(strings.TrimPrefix(string(content), "\uFEFF"), "\r\n", "\n")
	front, _, closed := strings.Cut(strings.TrimPrefix(text, "---\n"), "\n---")
	if !strings.HasPrefix(text, "---\n") || !closed {
		return agentToolRules{}, false
	}
	rules := agentToolRules{tools: map[string]bool{}, disallowed: map[string]bool{}}
	var into map[string]bool
	for _, line := range strings.Split(front, "\n") {
		if item, isItem := strings.CutPrefix(strings.TrimSpace(line), "- "); isItem && into != nil {
			addAgentTools(into, item)
			continue
		}
		key, value, _ := strings.Cut(line, ":")
		switch strings.TrimRight(key, " \t") {
		case "tools":
			into = rules.tools
		case "disallowedTools":
			into = rules.disallowed
		default:
			into = nil
			continue
		}
		addAgentTools(into, value)
	}
	rules.restricted = len(rules.tools) > 0 && !rules.tools["*"]
	return rules, true
}

func unguardedAgentTools(cfg object) []string {
	if files.pluginRoot == "" {
		return nil
	}
	issues := []string{}
	agents := []string{liteAgentType(cfg), digestAgentType(cfg)}
	for i, agent := range agents {
		if i > 0 && agent == agents[0] {
			continue
		}
		file := "agents/" + strings.TrimPrefix(agent, pluginName+":") + ".md"
		rules, ok := readAgentToolRules(filepath.Join(files.pluginRoot, filepath.FromSlash(file)))
		if !ok {
			continue
		}
		open := []string{}
		for _, tool := range unhookedFileTools {
			if rules.mayCall(tool) {
				open = append(open, tool)
			}
		}
		if len(open) > 0 {
			issues = append(issues, T("selfcheck.agentTools", file, strings.Join(open, ", ")))
		}
	}
	return issues
}

func selfCheckIssues(cfg object) []string {
	issues := []string{}
	host := currentHost()
	if host.statusline {
		settings := readJSONStrict(files.settings)
		if !settings.ok {
			issues = append(issues, T("selfcheck.settings"))
		} else {
			statusLine := getString(getMap(settings.data, "statusLine"), "command")
			if !ownStatusLine(statusLine) {
				issues = append(issues, T("selfcheck.statusline"))
			} else if gone := statusLineGone(statusLine); gone != "" {
				issues = append(issues, T("selfcheck.statuslineGone", gone))
			}
		}
	}
	if getString(cfg, "configError") != "" {
		issues = append(issues, T("selfcheck.config"))
	}
	if repaired := repairedThresholds(cfg); host.limits && len(repaired) > 0 {
		issues = append(issues, T("selfcheck.thresholdFixed", strings.Join(repaired, ", ")))
	}
	if unguarded := unguardedWindows(cfg); host.limits && len(unguarded) > 0 {
		issues = append(issues, T("selfcheck.threshold", strings.Join(unguarded, ", ")))
	}
	if host.agents {
		issues = append(issues, unguardedAgentTools(cfg)...)
	}
	if host.limits && paidCreditsAllowed(cfg) {
		issues = append(issues, T("credits.allowed"))
	}
	if profile := retunedProfile(section(cfg, "roles")); profile != "" {
		issues = append(issues, retunedNotice(profile))
	}
	if getString(section(cfg, "fable"), "source") == "oauth" && oauthToken() == "" {
		issues = append(issues, T("selfcheck.token", scopedLabel(cfg)))
	}
	return issues
}

func queueDirective(cfg object, queuePath string, total int) string {
	if !currentHost().agents {
		return fmt.Sprintf(`[noctis] Queue mode (%s: %d open). Work items in order; mark each done in the file before starting the next. Do not stop or ask for confirmation between items; decide yourself.`, filepath.Base(queuePath), total)
	}
	directive := fmt.Sprintf(`[noctis] Queue mode (%s: %d open). Work items in order; mark each done in the file before starting the next. Items needing no code or file edits (research, copy, docs, analysis) go to "%s" in one Agent call each; code stays with you.`, filepath.Base(queuePath), total, liteAgentType(cfg))
	if digestEnabled(cfg) {
		directive += fmt.Sprintf(` Long test runs, big diffs and noisy logs go to "%s" (it runs the command and returns a short digest) so your context stays small.`, digestAgentType(cfg))
	}
	return directive
}

func alreadyOverNotice(wait *waitPlan) string {
	notice := T("session.alreadyOver", wait.label, formatNumber(wait.used), formatTime(wait.until)) + pauseWhy(wait)
	if wait.hit != "ceiling" {
		notice += T("session.pauseHint")
	}
	return notice
}

func onSessionStart(input, cfg object) {
	repairOrphanWaits()
	now := nowSec()
	sid := sessionKey(input)
	state := readState()
	source := getString(input, "source")
	cwd := getString(input, "cwd")
	waits := getMap(state, "waits")
	handedOff := getMap(state, "handedOff")
	if source == "resume" && waits[sid] != nil && handedOff[sid] == nil && !isHandoffSession(sid) {
		clearWaitAndConsume(sid, state)
		logInfo("manual resume of %s: pending wait cleared", sid)
	}
	if source == "clear" {
		releaseClearedSession(sid, cwd, now)
		state = readState()
	}
	output := object{}
	contexts := []string{}
	queueNotice, queueNoticeKey, cutOffSid := "", "", ""
	if source == "startup" || source == "clear" {
		if checkpointSid, checkpoint := checkpointForNewSession(state, cwd, now); checkpoint != nil {
			handOverCheckpoint(checkpointSid, sid)
			contexts = append(contexts, T("session.checkpoint", pluginName, formatTime(numberOr(checkpoint, "at", 0)), getString(checkpoint, "path"), hostResumeCommand(currentHost().id, checkpointSid)))
			if note := cutOffNote(readState(), checkpointSid); note != "" {
				contexts = append(contexts, note)
				cutOffSid = checkpointSid
			}
		}
	}
	if queuePath := sessionQueueFile(cfg, sid, queueDirs(input)...); queuePath != "" {
		snapshot := queueSnapshot(queuePath)
		if !queueTrusted(cfg, queuePath) {

			if snapshot.total > 0 {
				queueNotice = T("queue.trustAsk", filepath.Base(queuePath), snapshot.total, pluginName)
			}
			logInfo("queue file %s found but not trusted (%d open): no directive injected", queuePath, snapshot.total)
		} else {
			rememberOpenIssues(cfg, queuePath)
			if isAutoQueue(queuePath) {
				contexts = append(contexts, autoQueueDirective(queuePath, snapshot.total))
			} else {
				contexts = append(contexts, queueDirective(cfg, queuePath, snapshot.total))
			}
			logInfo("queue mode for %s: %s (%d open)", sid, queuePath, snapshot.total)
		}

		if key := "queue:" + queuePath; queueTrusted(cfg, queuePath) && !isAutoQueue(queuePath) && snapshot.total > 0 && float64(now)-numberOr(getMap(state, "notified"), key, 0) > 7*86400 && (source == "startup" || source == "clear") {
			queueNotice = T("queue.modeNotice", filepath.Base(queuePath), snapshot.total, pluginName)
			queueNoticeKey = key
		}
	}
	if len(contexts) > 0 {
		output["hookSpecificOutput"] = object{"hookEventName": "SessionStart", "additionalContext": strings.Join(contexts, "\n")}
	}
	if source == "startup" && float64(now)-numberOr(getMap(state, "notified"), "selfcheck", 0) > selfCheckIntervalSec {
		if issues := selfCheckIssues(cfg); len(issues) > 0 {
			updateState(func(next object) { stateMap(next, "notified")["selfcheck"] = float64(now) })
			logInfo("self-check: %s", strings.Join(issues, "; "))
			output["systemMessage"] = joinNotices(getString(output, "systemMessage"), T("selfcheck.message", pluginName, strings.Join(issues, "; ")))
		}
	}
	if source == "startup" {
		if notice := restartNotice(cfg, state, now); notice != "" {
			output["systemMessage"] = joinNotices(getString(output, "systemMessage"), notice)
			state = readState()
		} else if notice := checkForUpdate(cfg, state, now); notice != "" {
			output["systemMessage"] = joinNotices(getString(output, "systemMessage"), notice)
			state = readState()
		}
	}
	if source == "startup" || source == "resume" {

		usage := currentUsage(now)
		if reverted := maybeRevertDefaultModel(cfg, state, usage, now); reverted != "" {

			output["systemMessage"] = joinNotices(getString(output, "systemMessage"), reverted)
			state = readState()
		}

		if !usage.hasAny || float64(now)-usage.updatedAt > usageStaleSeconds(cfg) {

			if refreshed := refreshFable(cfg, now, "session-start", -1, false); numberOr(refreshed, "fetchedAt", 0) > 0 {
				usage = currentUsage(now)
			}
		}
		verdict := evaluate(cfg, usage, resolveSessionModel(cfg, state, readJSON(files.usage), sid), 0, false)
		if verdict.wait != nil {
			key := fmt.Sprintf("over:%s:%s", verdict.wait.window, formatNumber(verdict.wait.until))
			if getMap(state, "notified")[key] == nil {
				updateState(func(next object) { stateMap(next, "notified")[key] = float64(now) })
				output["systemMessage"] = joinNotices(getString(output, "systemMessage"), alreadyOverNotice(verdict.wait))
			}
		}
	}
	if queueNotice != "" {

		output["systemMessage"] = joinNotices(getString(output, "systemMessage"), queueNotice)
		if queueNoticeKey != "" {
			updateState(func(next object) { stateMap(next, "notified")[queueNoticeKey] = float64(now) })
		}
	}
	if len(output) == 0 {
		return
	}
	if observed(sid, "SessionStart", "inject-context", source, nil) {
		return
	}
	emit(output)
	if cutOffSid != "" {
		forgetCutOffs(cutOffSid)
	}
}

func releaseClearedSession(newSid, cwd string, now int64) {
	usageFile := readJSON(files.usage)
	state := readState()
	sessions := getMap(usageFile, "sessions")
	type candidate struct {
		sid  string
		info object
	}
	candidates := []candidate{}
	for sid, raw := range sessions {
		info := toObject(raw)
		if sid == newSid || info == nil || getString(info, "cwd") != cwd || float64(now)-numberOr(info, "updatedAt", 0) > clearedSessionWindow {
			continue
		}
		candidates = append(candidates, candidate{sid, info})
	}
	if len(candidates) == 0 {
		return
	}
	sort.Slice(candidates, func(a, b int) bool {
		return numberOr(candidates[a].info, "updatedAt", 0) > numberOr(candidates[b].info, "updatedAt", 0)
	})
	waits := getMap(state, "waits")
	chosen := candidates[0]
	for _, entry := range candidates {
		if wait := getMap(waits, entry.sid); wait != nil && hookSleeping(wait) {
			chosen = entry
			break
		}
	}
	if wait := getMap(waits, chosen.sid); wait != nil && hookSleeping(wait) && getMap(getMap(state, "handedOff"), chosen.sid) == nil {
		clearWait(chosen.sid, state)
		logInfo("/clear in %s: interrupted wait of %s released", cwd, chosen.sid)
	}
	override := getMap(getMap(state, "modelOverrides"), chosen.sid)
	model := getString(chosen.info, "model")
	if override != nil && numberOr(override, "at", 0) >= numberOr(chosen.info, "updatedAt", 0) {
		model = getString(override, "model")
	}
	if model != "" {
		updateState(func(next object) {
			stateMap(next, "modelOverrides")[newSid] = object{"model": model, "at": float64(now)}
		})
	}
}

func onSessionEnd(input, _ object) {
	sid := sessionKey(input)
	handingOff := stopFailureHandedOff(sid)
	updateState(func(state object) {
		delete(stateMap(state, "modelOverrides"), sid)
		delete(stateMap(state, "autoResume"), sid)
		delete(stateMap(state, "overload"), sid)
		if getMap(getMap(state, "waits"), sid) == nil && !handingOff {

			delete(stateMap(state, "workflows"), sid)
		}
		delete(stateMap(state, "routes"), sid)
		delete(stateMap(state, "notified"), sid)
		delete(stateMap(state, "notified"), sid+":config")
	})
}

var promptFromPlugin = false

func pluginComposedPrompt(cfg object, sid, prompt string) bool {
	if record := getMap(getMap(readState(), "resumePrompts"), sid); record != nil {
		if getString(record, "hash") == promptDigest(prompt) {
			updateState(func(next object) { delete(stateMap(next, "resumePrompts"), sid) })
			return true
		}
	}
	trimmed := strings.TrimSpace(prompt)
	if base := strings.TrimSpace(getString(section(cfg, "resume"), "prompt")); base != "" && strings.HasPrefix(trimmed, base) {
		return true
	}
	if prefix := strings.SplitN(catalogFor("en")["overload.wakeMessage"], "%s", 2)[0]; prefix != "" && strings.HasPrefix(trimmed, prefix) {
		return true
	}
	return false
}

func joinNotices(parts ...string) string {
	kept := []string{}
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " ")
}

var controlSkills = map[string]bool{"pause": true, "resume": true, "setup": true, "status": true}

func controlCommand(prompt string) string {
	fields := strings.Fields(prompt)
	if len(fields) == 0 {
		return ""
	}
	if name, found := strings.CutPrefix(fields[0], "/"+pluginName+":"); found && controlSkills[name] {
		return fields[0]
	}
	return ""
}

func onControlPrompt(input, cfg object, result decision, command string) {
	sid := sessionKey(input)
	facts := usageFacts(result.usage)
	if ceiling := ceilingHit(cfg, result.usage); ceiling != nil {
		if observed(sid, "UserPromptSubmit", "block-control-prompt", command+": "+hitLabel(ceiling), facts) {
			return
		}
		journal(sid, "UserPromptSubmit", "block-control-prompt", command+": "+hitLabel(ceiling), facts)
		logInfo("%s refused for %s at the credit ceiling: %s %s%%", command, sid, ceiling.window, formatNumber(ceiling.used))
		emit(object{"decision": "block", "reason": "⏸ " + T("wait.reason", ceiling.label, formatNumber(ceiling.used), hitLabel(ceiling), formatTime(ceiling.until))})
		return
	}
	if result.wait != nil {
		journal(sid, "UserPromptSubmit", "control-prompt", command+": "+hitLabel(result.wait), facts)
		logInfo("%s for %s passes the pause point: %s %s%%", command, sid, result.wait.window, formatNumber(result.wait.used))
	}
	if !observing && result.notice != "" {
		emit(object{"systemMessage": result.notice})
	}
}

func onUserPromptSubmit(input, cfg object) {
	now := nowSec()
	sid := sessionKey(input)
	state := readState()
	if guardPaused(cfg, state, now) {
		retireOwnCheckpoint(state, sid)
		return
	}
	clearDeadHandoffs(state)
	if handoff := getMap(getMap(state, "handedOff"), sid); handoff != nil && !isHandoffSession(sid) {
		if observed(sid, "UserPromptSubmit", "block-duplicate-window", getString(handoff, "model"), nil) {
			return
		}
		emit(object{"decision": "block", "reason": T("handoff.blocked", formatTime(numberOr(handoff, "at", 0)), getString(handoff, "model"), sid)})
		return
	}
	releaseInterruptedWait(sid, state)
	clearOverload(state, sid)
	if !queueContinuationPrompt(getString(input, "prompt")) {
		resetIdleGuard(state, sid)
	}
	result := decide(cfg, state, input, now, decideOptions{})
	if result.fableHit {
		if observed(sid, "UserPromptSubmit", "switch-model", scopedLabel(cfg), usageFacts(result.usage)) {
			return
		}
		emit(object{"decision": "block", "reason": handleFableHit("prompt", input, cfg, result)})
		return
	}
	if command := controlCommand(getString(input, "prompt")); command != "" {
		onControlPrompt(input, cfg, result, command)
		return
	}
	output := object{}
	systemMessage, waitContext := "", ""
	if result.wait != nil {
		if observed(sid, "UserPromptSubmit", "pause", hitLabel(result.wait), usageFacts(result.usage)) {
			return
		}
		outcome := enforceWait("prompt", input, cfg, result)
		if outcome.stop != "" {
			emit(object{"decision": "block", "reason": outcome.stop})
			return
		}
		systemMessage = outcome.notice
		waitContext = withCutOffNote(sid, outcome.context)
	} else if promptFromPlugin {
		forgetCutOffs(sid)
	}
	retireOwnCheckpoint(state, sid)
	contexts := []string{}
	if waitContext != "" {
		contexts = append(contexts, waitContext)
	}
	if result.warnWindow != nil {
		warnKey := fmt.Sprintf("%s:warn:%s:%s", sid, result.warnWindow.window, formatNumber(result.warnWindow.resetsAt))
		if getMap(state, "notified")[warnKey] == nil {
			updateState(func(next object) { stateMap(next, "notified")[warnKey] = float64(now) })
			journal(sid, "UserPromptSubmit", "warn", result.warnWindow.label+" "+formatNumber(result.warnWindow.used)+"%", usageFacts(result.usage))
			contexts = append(contexts, fmt.Sprintf("[noctis] %s usage %d%% (auto-pause at %s%%): finish the current step cleanly, avoid big new work.", result.warnWindow.label, int(math.Round(result.warnWindow.used)), formatNumber(result.warnWindow.threshold)))
			logInfo("warn band for %s: %s %%%s", sid, result.warnWindow.window, formatNumber(result.warnWindow.used))
		}
	}
	if getBool(section(cfg, "queue"), "auto", true) && getBool(section(cfg, "queue"), "enabled", true) && !observing && !promptFromPlugin && queueFile(cfg, queueDirs(input)...) == "" {
		if items := autoQueueItems(getString(input, "prompt")); len(items) > 0 {
			if path := startAutoQueue(sid, getString(input, "cwd"), items, now); path != "" {
				rememberOpenIssues(cfg, path)
				contexts = append(contexts, autoQueueDirective(path, len(items)))
				systemMessage = joinNotices(systemMessage, T("queue.autoNotice", len(items), pluginName))
				resetIdleGuard(readState(), sid)
			}
		} else {
			endAutoQueue(sid, true)
		}
	}
	verdict := classifyPrompt(cfg, getMap(state, "routerLearned"), getString(input, "prompt"), getString(input, "transcript_path"), now)
	if !currentHost().agents {

		verdict.route = false
	}
	if verdict.route && observing {
		journal(sid, "UserPromptSubmit", "would-route", verdict.reason, object{"signal": verdict.signal})
		verdict.route = false
	}
	if verdict.route || getMap(state, "routes")[sid] != nil {
		updateState(func(next object) {
			if verdict.route {
				stateMap(next, "routes")[sid] = object{"at": float64(now), "denies": float64(0), "signal": verdict.signal}
			} else {
				delete(stateMap(next, "routes"), sid)
			}
		})
	}
	if verdict.route {
		contexts = append(contexts, routeDirective(cfg))
		systemMessage = joinNotices(systemMessage, T("notice.routed", liteAgentType(cfg), getString(section(cfg, "models"), "primary")))
		journal(sid, "UserPromptSubmit", "route", verdict.reason, object{"signal": verdict.signal})
		logInfo("route -> %s for %s (%s)", getString(section(cfg, "router"), "agent"), sid, verdict.reason)
	} else if advice := suggestWorkflow(cfg, getString(input, "prompt"), result); advice != "" && currentHost().agents {
		contexts = append(contexts, advice)
		journal(sid, "UserPromptSubmit", "suggest-workflow", "fan-out prompt", nil)
		logInfo("workflow suggested for %s (fan-out prompt)", sid)
	}
	if observing {
		return
	}
	if len(contexts) > 0 {
		output["hookSpecificOutput"] = object{"hookEventName": "UserPromptSubmit", "additionalContext": strings.Join(contexts, "\n")}
	}
	systemMessage = joinNotices(systemMessage, result.notice)
	systemMessage = joinNotices(systemMessage, leanNotice(cfg, state, sid))
	if systemMessage != "" {
		output["systemMessage"] = systemMessage
	}
	if len(output) > 0 {
		emit(output)
	}
}

func pinnedSubagentModel(cfg, input object) string {
	toolInput := getMap(input, "tool_input")
	if getString(toolInput, "model") != "" {
		return ""
	}
	requested := firstString(toolInput, "subagent_type", "agent")
	if requested == "" || strings.HasPrefix(requested, pluginName+":") {
		return ""
	}
	return getString(getMap(section(cfg, "router"), "subagentModels"), requested)
}

func scopedFallbackFor(cfg object, model string) string {
	host := currentHost()
	scoped := scopedModelPattern(cfg)
	fallback := getString(section(cfg, "models"), "fallback")
	if !host.agents || !host.modelSwitch || model == "" || !scoped.MatchString(model) || fallback == "" || scoped.MatchString(fallback) || !validThreshold(scopedThresholdValue(cfg)) {
		return ""
	}
	return fallback
}

func scopedQuotaOut(cfg, state object, usage usageView, now int64) bool {
	threshold, guarded := scopedThresholdEnabled(cfg)
	if !guarded {
		return false
	}
	if usage.fable != nil && usage.fable.used >= threshold {
		return true
	}
	switched := getMap(state, "modelSwitched")
	if switched == nil || float64(now) >= numberOr(switched, "fableResetsAt", 0) {
		return false
	}
	return usage.fable == nil || usage.fable.used >= threshold/2
}

func scopedSafeModel(cfg, state object, usage usageView, model string, now int64) string {
	if fallback := scopedFallbackFor(cfg, model); fallback != "" && scopedQuotaOut(cfg, state, usage, now) {
		return fallback
	}
	return model
}

func subagentFallback(cfg object, model string) (string, usageView) {
	if scopedFallbackFor(cfg, model) == "" {
		return model, usageView{}
	}
	now := nowSec()
	usage := currentUsage(now)
	maxAge := -1.0
	if limit, guarded := scopedThresholdEnabled(cfg); guarded && usage.fable != nil && usage.fable.used >= limit-nearEdgeBand {
		maxAge = nearEdgePollNormal
	}
	before := numberOr(readJSON(files.fable), "fetchedAt", 0)
	if after := refreshFable(cfg, now, "fable-subagent", maxAge, false); numberOr(after, "fetchedAt", 0) != before {
		usage = currentUsage(now)
	}
	return scopedSafeModel(cfg, readState(), usage, model, now), usage
}

func onAgentSpawn(input, cfg, state object, now int64) {
	sid := sessionKey(input)
	if guardPaused(cfg, state, now) {
		return
	}
	result := decide(cfg, state, input, now, decideOptions{})
	if result.fableHit {
		if observed(sid, "PreToolUse", "switch-model", scopedLabel(cfg), usageFacts(result.usage)) {
			return
		}

		emit(object{"hookSpecificOutput": object{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": handleFableHit("prompt", input, cfg, result)}})
		return
	}
	systemMessage := ""
	specific := object{}
	if result.wait != nil {
		if observed(sid, "PreToolUse", "pause", hitLabel(result.wait), usageFacts(result.usage)) {
			return
		}
		outcome := enforceWait("batch", input, cfg, result)
		if outcome.stop != "" {
			emit(object{"hookSpecificOutput": object{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": outcome.stop}})
			return
		}
		systemMessage = outcome.notice
		if context := withCutOffNote(sid, outcome.context); context != "" {
			specific["additionalContext"] = context
		}
	}
	toolInput := getMap(input, "tool_input")
	requested := orDefault(firstString(toolInput, "subagent_type", "agent"), getString(input, "tool_name"))
	model := pinnedSubagentModel(cfg, input)
	action, reason, facts := "pin-subagent-model", requested+" → "+model, object(nil)
	target := orDefault(getString(toolInput, "model"), model)
	if safe, usage := subagentFallback(cfg, target); safe != target {
		model, action, facts = safe, "subagent-fallback", usageFacts(usage)
		reason = fmt.Sprintf("%s: %s → %s, the %s quota is out", requested, target, safe, scopedLabel(cfg))
	}
	if model != "" {
		if observed(sid, "PreToolUse", action, reason, facts) {
			return
		}
		updated := cloneObject(toolInput)
		updated["model"] = model
		specific["permissionDecision"], specific["updatedInput"] = "allow", updated
		journal(sid, "PreToolUse", action, reason, facts)
		logInfo("subagent model for %s: %s (%s)", sid, reason, action)
	}
	if observing {
		return
	}
	output := object{}
	if len(specific) > 0 {
		specific["hookEventName"] = "PreToolUse"
		output["hookSpecificOutput"] = specific
	}
	if systemMessage = joinNotices(systemMessage, result.notice); systemMessage != "" {
		output["systemMessage"] = systemMessage
	}
	if len(output) > 0 {
		emit(output)
	}
}

func textExtensionList() string {
	return strings.Join(sortedKeys(textExtensions), " ")
}

func insideSubagent(input object) bool {
	return getString(input, "agent_id") != "" || (activeHost != "claude" && getString(input, "agent_type") != "")
}

func runsAsAgent(input object, full string) bool {
	name := getString(input, "agent_type")
	return name == full || name == strings.TrimPrefix(full, pluginName+":")
}

func agentWritePolicy(input, cfg object) {
	toolName := getString(input, "tool_name")
	if !fileTools[toolName] {
		return
	}
	toolInput := getMap(input, "tool_input")
	filePath := getString(toolInput, "file_path")
	if filePath == "" {
		filePath = getString(toolInput, "notebook_path")
	}

	switch {
	case runsAsAgent(input, liteAgentType(cfg)):
		if textExtensions[strings.ToLower(filepath.Ext(filePath))] {
			return
		}
		logInfo("lite agent write denied: %s", orDefault(filePath, "(no path)"))
		emit(object{"hookSpecificOutput": object{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": fmt.Sprintf("The lite agent may only write text documents (%s); code and config files belong to the main model. Return the content in your answer instead.", textExtensionList())}})
	case runsAsAgent(input, digestAgentType(cfg)):
		logInfo("digest agent write denied: %s", orDefault(filePath, "(no path)"))
		emit(object{"hookSpecificOutput": object{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": "The digest agent only runs commands and summarizes output; it never writes files. Return the digest in your answer."}})
	}
}

var reportTools = map[string]bool{"SubagentHandback": true, "StructuredOutput": true}

const cutOffAgentsKept = 32

func deliveringReport(input object) bool {
	calls := getList(input, "tool_calls")
	for _, raw := range calls {
		if !reportTools[getString(toObject(raw), "tool_name")] {
			return false
		}
	}
	return len(calls) > 0
}

func subagentLimit(cfg object, now int64) decision {
	usage := currentUsage(now)
	edge := nearEdge(cfg, usage)
	if edge || float64(now)-usage.updatedAt > usageStaleSeconds(cfg) {
		maxAge := -1.0
		if edge {
			maxAge = edgePollSeconds(cfg, usage)
		}
		refreshFable(cfg, now, "subagent", maxAge, false)
		usage = currentUsage(now)
	}
	return evaluate(cfg, usage, "", 0, false)
}

func subagentLimitReason(wait, ceiling *waitPlan) string {
	resume := "The session continues after the reset at " + formatTime(wait.until) + "."
	if ceiling != nil {
		where := "at the paid-credit ceiling"
		if ceiling.used < ceiling.threshold {
			where = "close enough to the paid-credit ceiling to cross it at the current burn rate"
		}
		return fmt.Sprintf("[noctis] %s usage is %s%%, %s (%s%%): past it the account pays for the overflow in usage credits, so this agent stops now. %s", ceiling.label, formatNumber(ceiling.used), where, formatNumber(ceiling.threshold), resume)
	}
	point := formatNumber(wait.threshold) + "%"
	if wait.used < wait.threshold {
		point += ", reached early at the current burn rate"
	}
	return fmt.Sprintf("[noctis] %s usage is %s%% (pause point %s): start no new work. Return what you have the way you normally deliver your report, and name what is unfinished. %s", wait.label, formatNumber(wait.used), point, resume)
}

func shownLimit(wait, ceiling *waitPlan) *waitPlan {
	if ceiling != nil {
		return ceiling
	}
	return wait
}

func denySubagentTool(input, cfg object) bool {
	if !currentHost().agents {
		return false
	}
	now := nowSec()
	if guardPaused(cfg, readState(), now) {
		return false
	}
	result := subagentLimit(cfg, now)
	toolName := getString(input, "tool_name")
	reason, what := "", ""
	switch ceiling := ceilingHit(cfg, result.usage); {
	case result.wait != nil:
		reason, what = subagentLimitReason(result.wait, ceiling), hitLabel(shownLimit(result.wait, ceiling))
	case toolName == "Workflow":
		reason, what = gateWorkflowLaunch(cfg, result, input), orDefault(hitLabelOrWarn(result), "fan-out headroom")
	}
	if reason == "" {
		return false
	}
	sid := sessionKey(input)
	facts := usageFacts(result.usage)
	if observed(sid, "PreToolUse", "deny-subagent-tool", toolName+": "+what, facts) {
		return false
	}
	journal(sid, "PreToolUse", "deny-subagent-tool", toolName+": "+what, facts)
	logInfo("subagent %s of %s: %s denied (%s)", getString(input, "agent_id"), sid, toolName, what)
	emit(object{"hookSpecificOutput": object{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason}})
	return true
}

func agentWarned(state object, sid, agent string, now int64) bool {
	for _, record := range agentCutOffs(state, sid) {
		if getString(record, "agent") == agent && numberOr(record, "until", 0) > float64(now) {
			return true
		}
	}
	return false
}

func recordCutOff(sid, agent, agentType string, until float64, stopped bool, now int64) {
	updateState(func(state object) {
		record := object{"agent": agent, "agentType": agentType}
		kept := []any{}
		for _, raw := range getList(stateMap(state, "workflows"), sid) {
			entry := toObject(raw)
			name := getString(entry, "agent")
			switch {
			case entry == nil:
			case name == agent:
				if numberOr(entry, "until", 0) > float64(now) {
					record = entry
				}
			case name != "" && numberOr(entry, "until", 0)+stateEntryTTLSeconds < float64(now):
			default:
				kept = append(kept, entry)
			}
		}
		if agentType != "" {
			record["agentType"] = agentType
		}
		record["at"], record["stopped"] = float64(now), stopped || getBool(record, "stopped", false)
		record["until"] = math.Max(numberOr(record, "until", 0), until)
		kept = append(kept, record)
		excess := -cutOffAgentsKept
		for _, raw := range kept {
			if getString(toObject(raw), "agent") != "" {
				excess++
			}
		}
		trimmed := []any{}
		for _, raw := range kept {
			if excess > 0 && getString(toObject(raw), "agent") != "" {
				excess--
				continue
			}
			trimmed = append(trimmed, raw)
		}
		stateMap(state, "workflows")[sid] = trimmed
	})
}

func forgetCutOffs(sid string) {
	if len(agentCutOffs(readState(), sid)) == 0 {
		return
	}
	updateState(func(state object) {
		kept := []any{}
		for _, raw := range getList(stateMap(state, "workflows"), sid) {
			if getString(toObject(raw), "agent") == "" {
				kept = append(kept, raw)
			}
		}
		if len(kept) == 0 {
			delete(stateMap(state, "workflows"), sid)
			return
		}
		stateMap(state, "workflows")[sid] = kept
	})
}

func withCutOffNote(sid, context string) string {
	note := cutOffNote(readState(), sid)
	if note == "" {
		return context
	}
	forgetCutOffs(sid)
	if context == "" {
		return note
	}
	return context + "\n" + note
}

func onSubagentBatch(input, cfg object) {
	if !currentHost().agents || deliveringReport(input) {
		return
	}
	now := nowSec()
	state := readState()
	if guardPaused(cfg, state, now) {
		return
	}
	result := subagentLimit(cfg, now)
	wait := result.wait
	if wait == nil {
		return
	}
	sid, agent, agentType := sessionKey(input), getString(input, "agent_id"), getString(input, "agent_type")
	who := strings.TrimSpace(agentType + " " + agent)
	facts := usageFacts(result.usage)
	if ceiling := ceilingHit(cfg, result.usage); ceiling != nil || agentWarned(state, sid, agent, now) {
		shown := shownLimit(wait, ceiling)
		if observed(sid, "PostToolBatch", "stop-subagent", who+": "+hitLabel(shown), facts) {
			return
		}
		recordCutOff(sid, agent, agentType, wait.until, true, now)
		journal(sid, "PostToolBatch", "stop-subagent", who+": "+hitLabel(shown), facts)
		warn("subagent %s of %s stopped: %s at %s%%", who, sid, shown.window, formatNumber(shown.used))
		emit(object{"continue": false, "stopReason": "⏸ " + T("wait.reason", shown.label, formatNumber(shown.used), hitLabel(shown), formatTime(shown.until))})
		return
	}
	if observed(sid, "PostToolBatch", "warn-subagent", who+": "+hitLabel(wait), facts) {
		return
	}
	recordCutOff(sid, agent, agentType, wait.until, false, now)
	journal(sid, "PostToolBatch", "warn-subagent", who+": "+hitLabel(wait), facts)
	logInfo("subagent %s of %s told to wrap up: %s at %s%%", who, sid, wait.window, formatNumber(wait.used))
	emit(object{"hookSpecificOutput": object{"hookEventName": "PostToolBatch", "additionalContext": subagentLimitReason(wait, nil) + " Any tool call other than delivering that report ends this agent."}})
}

func onPreToolUse(input, cfg object) {
	toolName := getString(input, "tool_name")
	if insideSubagent(input) {
		if !denySubagentTool(input, cfg) {
			agentWritePolicy(input, cfg)
		}
		return
	}
	if fileTools[toolName] {
		agentWritePolicy(input, cfg)
		return
	}
	now := nowSec()
	sid := sessionKey(input)
	state := readState()
	if agentTools[toolName] {
		onAgentSpawn(input, cfg, state, now)
		return
	}
	if toolName == "Workflow" {
		onWorkflowLaunch(input, cfg, state, now, sid)
		return
	}
	if runsAsAgent(input, liteAgentType(cfg)) {
		return
	}
	route := getMap(getMap(state, "routes"), sid)
	if route == nil || float64(now)-numberOr(route, "at", 0) > routeTTLSeconds {
		return
	}
	maxDenies := math.Max(1, numberOr(section(cfg, "router"), "maxDeniesPerPrompt", 3))
	if numberOr(route, "denies", 0) >= maxDenies {
		warn("route enforcement gave up for %s after %s denies (%s)", sid, formatNumber(numberOr(route, "denies", 0)), toolName)
		updateState(func(next object) { delete(stateMap(next, "routes"), sid) })
		return
	}
	if observed(sid, "PreToolUse", "deny-main-thread-research", toolName, nil) {
		return
	}
	updateState(func(next object) {
		if record := getMap(getMap(next, "routes"), sid); record != nil {
			record["denies"] = numberOr(record, "denies", 0) + 1
		}
	})
	journal(sid, "PreToolUse", "deny", toolName+" on routed prompt", nil)
	logInfo("denied %s on main thread for routed prompt %s", toolName, sid)
	emit(object{"hookSpecificOutput": object{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": fmt.Sprintf(`Research for this prompt must run in the "%s" subagent (Agent tool); delegate it instead of calling %s here.`, liteAgentType(cfg), toolName)}})
}

func onWorkflowLaunch(input, cfg object, state object, now int64, sid string) {
	result := decide(cfg, state, input, now, decideOptions{force: true})
	if reason := gateWorkflowLaunch(cfg, result, input); reason != "" {
		if observed(sid, "PreToolUse", "deny-workflow", reason, usageFacts(result.usage)) {
			recordWorkflowLaunch(sid, input, now)
			return
		}
		journal(sid, "PreToolUse", "deny-workflow", hitLabelOrWarn(result), usageFacts(result.usage))
		warn("workflow launch denied for %s: %s", sid, reason)
		emit(object{"hookSpecificOutput": object{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason}})
		return
	}
	recordWorkflowLaunch(sid, input, now)
	journal(sid, "PreToolUse", "workflow-launch", "recorded for checkpoints", nil)
	logInfo("workflow launch recorded for %s", sid)
}

func hitLabelOrWarn(result decision) string {
	switch {
	case result.wait != nil:
		return hitLabel(result.wait)
	case result.warnWindow != nil:
		return "warn band " + result.warnWindow.label
	case result.fableHit:
		return "scoped quota"
	}
	return ""
}

const claudeStopBlockCap = 8

func stopBlockCap() float64 {
	if activeHost != "claude" {
		return 0
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv("CLAUDE_CODE_STOP_HOOK_BLOCK_CAP")), 64)
	if err != nil || math.IsNaN(value) {
		return claudeStopBlockCap
	}
	if value <= 0 || math.IsInf(value, 1) {
		return 0
	}
	return value
}

func queueContinuationPrompt(prompt string) bool {
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), queueContinuesPrefix) {
			return true
		}
	}
	return false
}

func resetIdleGuard(state object, sid string) {
	guard := getMap(getMap(state, "stopGuard"), sid)
	if guard == nil || numberOr(guard, "idle", 0) == 0 {
		return
	}
	updateState(func(next object) {
		if record := getMap(getMap(next, "stopGuard"), sid); record != nil {
			record["idle"] = float64(0)
		}
	})
}

func completionPromised(cfg object, transcriptPath string) bool {
	promise := strings.TrimSpace(getString(section(cfg, "queue"), "completionPromise"))
	if promise == "" || transcriptPath == "" {
		return false
	}
	summary := summarizeTranscript(transcriptPath)
	return strings.Contains(summary.lastAssistantRaw, "<promise>"+promise+"</promise>")
}

func onPermissionRequest(input, cfg object) {
	tool := getString(input, "tool_name")
	switch tool {
	case "Read", "Edit", "MultiEdit", "Write":
	default:
		return
	}
	if getString(input, "permission_mode") == "plan" {
		return
	}
	requested := filepath.FromSlash(getString(getMap(input, "tool_input"), "file_path"))
	if !filepath.IsAbs(requested) {
		return
	}
	sid := sessionKey(input)
	if tool == "Read" && checkpointHandedTo(readState(), sid, requested) {
		allowFileRequest(sid, tool, requested, "allow-checkpoint-note", "the resume note handed to")
		return
	}
	queuePath := sessionQueueFile(cfg, sid, queueDirs(input)...)
	if queuePath == "" || requested != queuePath || !queueTrusted(cfg, queuePath) {
		return
	}
	allowFileRequest(sid, tool, queuePath, "allow-queue-file", "the queue file of")
}

func allowFileRequest(sid, tool, path, action, role string) {
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		return
	}
	reason := tool + " " + filepath.Base(path)
	if observed(sid, "PermissionRequest", action, reason, nil) {
		return
	}
	journal(sid, "PermissionRequest", action, reason, nil)
	logInfo("permission request for %s answered: %s is %s %s", tool, path, role, sid)
	emit(object{"hookSpecificOutput": object{"hookEventName": "PermissionRequest", "decision": object{"behavior": "allow"}}})
}

func onStop(input, cfg object) {
	now := nowSec()
	sid := sessionKey(input)
	state := readState()
	if guardPaused(cfg, state, now) {
		return
	}
	if getMap(getMap(state, "handedOff"), sid) != nil && !isHandoffSession(sid) {
		return
	}
	if wait := getMap(getMap(state, "waits"), sid); wait != nil && !getBool(wait, "inHook", false) && !resumedByThisSession(state, sid, wait) {
		return
	}
	clearOverload(state, sid)
	clearFailureRetries(state, sid)
	queuePath := sessionQueueFile(cfg, sid, queueDirs(input)...)
	if queuePath == "" || !queueTrusted(cfg, queuePath) {
		return
	}
	queueLabel := filepath.Base(queuePath)
	if isAutoQueue(queuePath) {
		queueLabel = T("queue.autoLabel")
	}
	snapshot := queueSnapshot(queuePath)
	syncDoneIssues(cfg, queuePath, getString(input, "cwd"))
	if snapshot.total == 0 {
		driven := getMap(getMap(state, "stopGuard"), sid) != nil || isAutoQueue(queuePath)
		updateState(func(next object) { delete(stateMap(next, "stopGuard"), sid) })
		if isAutoQueue(queuePath) {
			endAutoQueue(sid, true)
		}
		logInfo("queue empty for %s; stop allowed", sid)
		if driven {

			journal(sid, "Stop", "allow-stop", "queue finished", nil)
			notify(cfg, pluginName, T("queue.doneNotify", queueLabel))
			emit(object{"systemMessage": T("queue.doneMessage", queueLabel)})
		}
		return
	}
	if len(snapshot.items) == 0 && snapshot.blocked > 0 {

		updateState(func(next object) { delete(stateMap(next, "stopGuard"), sid) })
		journal(sid, "Stop", "allow-stop", "queue blocked", object{"open": snapshot.total, "blocked": snapshot.blocked})
		fail("queue blocked for %s: %d open items all wait on unfinished dependencies; stop allowed", sid, snapshot.blocked)
		if observing {
			return
		}
		notify(cfg, pluginName, T("queue.blockedNotify", snapshot.blocked, queueLabel))
		emit(object{"systemMessage": T("queue.blockedMessage", snapshot.blocked)})
		return
	}
	if completionPromised(cfg, getString(input, "transcript_path")) {
		updateState(func(next object) { delete(stateMap(next, "stopGuard"), sid) })
		journal(sid, "Stop", "allow-stop", "completion promise", object{"open": snapshot.total})
		logInfo("completion promise seen for %s; stop allowed with %d open", sid, snapshot.total)
		return
	}
	guard := getMap(getMap(state, "stopGuard"), sid)
	if guard == nil {
		guard = object{"forced": float64(0), "idle": float64(0), "lastOpen": nil, "at": float64(now)}
	}
	lastOpen, hasLastOpen := getNumber(guard, "lastOpen")
	if hasLastOpen && float64(snapshot.total) != lastOpen {
		guard["idle"] = float64(0)
	} else if getBool(input, "stop_hook_active", false) {
		guard["idle"] = numberOr(guard, "idle", 0) + 1
	}
	guard["lastOpen"] = float64(snapshot.total)
	guard["at"] = float64(now)
	queue := section(cfg, "queue")
	maxIdle := math.Max(1, numberOr(queue, "maxIdleContinues", 4))
	if limit := stopBlockCap(); limit > 0 {
		maxIdle = math.Max(1, math.Min(maxIdle, math.Floor(limit)))
	}
	maxForced := math.Max(1, numberOr(queue, "maxContinuesPerSession", 200))
	if numberOr(guard, "idle", 0) >= maxIdle || numberOr(guard, "forced", 0) >= maxForced {

		stuckKey := fmt.Sprintf("stuck:%s:%s", sid, queuePath)
		alreadyTold := getMap(state, "notified")[stuckKey] != nil
		updateState(func(next object) {
			stateMap(next, "stopGuard")[sid] = object{"forced": float64(0), "idle": float64(0), "lastOpen": nil, "at": float64(now), "gaveUp": float64(now), "cycles": numberOr(guard, "cycles", 0) + 1}
			stateMap(next, "notified")[stuckKey] = float64(now)
		})
		fail("queue not progressing for %s: %d open after %s idle continues (total %s); stop allowed", sid, snapshot.total, formatNumber(numberOr(guard, "idle", 0)), formatNumber(numberOr(guard, "forced", 0)))
		journal(sid, "Stop", "allow-stop", "queue not progressing", object{"open": snapshot.total})
		if observing || alreadyTold {
			return
		}
		notify(cfg, pluginName, T("queue.stuckNotify", snapshot.total, queueLabel))
		emit(object{"systemMessage": T("queue.stuckMessage", snapshot.total)})
		return
	}
	result := decide(cfg, state, input, now, decideOptions{})
	if result.fableHit {
		if observed(sid, "Stop", "switch-model", scopedLabel(cfg), usageFacts(result.usage)) {
			return
		}
		emit(object{"systemMessage": handleFableHit("batch", input, cfg, result)})
		return
	}
	systemMessage, waitContext := "", ""
	if result.wait != nil {
		if observed(sid, "Stop", "pause", hitLabel(result.wait), usageFacts(result.usage)) {
			return
		}
		outcome := enforceWait("stop", input, cfg, result)
		if outcome.stop != "" {
			emit(object{"systemMessage": outcome.stop})
			return
		}
		systemMessage, waitContext = outcome.notice, withCutOffNote(sid, outcome.context)
	}
	if observed(sid, "Stop", "continue-queue", fmt.Sprintf("%d open", snapshot.total), nil) {
		return
	}
	guard["forced"] = numberOr(guard, "forced", 0) + 1
	updateState(func(next object) { stateMap(next, "stopGuard")[sid] = guard })
	journal(sid, "Stop", "continue-queue", fmt.Sprintf("%d open", snapshot.total), object{"forced": numberOr(guard, "forced", 0)})
	logInfo("queue continue #%s for %s: %d open", formatNumber(numberOr(guard, "forced", 0)), sid, snapshot.total)
	nextItem := ""
	if len(snapshot.items) > 0 {
		nextItem = fmt.Sprintf(` ("%s")`, snapshot.items[0])
	}
	blockedNote := ""
	if snapshot.blocked > 0 {
		blockedNote = fmt.Sprintf(" %d item(s) wait on unfinished dependencies and are not eligible yet.", snapshot.blocked)
	}
	if len(snapshot.items) > 0 && currentHost().agents && looksLikeFanOut(snapshot.items[0]) && workflowAdvisable(cfg, result) {
		blockedNote += " " + workflowAdvice(cfg, "The next item", result.usage)
	}
	if snapshot.plain {
		blockedNote += " This list has no checkboxes: first rewrite every open item as \"- [ ] …\" (finished ones as \"- [x] …\") so progress can be tracked, then continue."
	}
	where := filepath.Base(queuePath)
	if isAutoQueue(queuePath) {
		where = queuePath
	}
	reason := fmt.Sprintf(queueContinuesPrefix+": %d open in %s. Take the next eligible item%s (priority and (after …) dependencies already applied), finish it completely, mark it done in the file, then move to the following one.%s Do not stop or ask for confirmation; decide yourself.", snapshot.total, where, nextItem, blockedNote)
	if waitContext != "" {
		reason = waitContext + "\n" + reason
	}
	output := object{"decision": "block", "reason": reason}
	if systemMessage = joinNotices(systemMessage, result.notice); systemMessage != "" {
		output["systemMessage"] = systemMessage
	}
	emit(output)
}

var shellTools = map[string]bool{"Bash": true, "PowerShell": true}

var platformBinDir = lazyRegexp(`^[a-z0-9]+-[a-z0-9]+/$`)

type shellWord struct {
	text   string
	quoted bool
}

func ownCommandsOnly(input object) bool {
	calls := getList(input, "tool_calls")
	for _, raw := range calls {
		call := toObject(raw)
		if !shellTools[getString(call, "tool_name")] || !runsOwnBinaryOnly(getString(getMap(call, "tool_input"), "command")) {
			return false
		}
	}
	return len(calls) > 0
}

func runsOwnBinaryOnly(command string) bool {
	segments, ok := shellSegments(command)
	if !ok || len(segments) == 0 {
		return false
	}
	for _, words := range segments {
		if !words[0].quoted && words[0].text == "&" {
			words = words[1:]
		}
		if len(words) == 0 || !ownBinary(words[0]) {
			return false
		}
		for _, word := range words[1:] {
			if !plainArgument(word) {
				return false
			}
		}
	}
	return true
}

func shellSegments(command string) ([][]shellWord, bool) {
	if strings.IndexFunc(command, func(r rune) bool {
		return (r < ' ' && r != '\t' && r != '\n' && r != '\r') || r == 0x7f || r == '`' || (r >= 0x2018 && r <= 0x201f)
	}) >= 0 {
		return nil, false
	}
	segments, words := [][]shellWord{}, []shellWord{}
	flush := func() {
		if len(words) > 0 {
			segments, words = append(segments, words), []shellWord{}
		}
	}
	for i := 0; i < len(command); {
		switch c := command[i]; {
		case c == ' ' || c == '\t':
			i++
		case c == '\n' || c == '\r' || c == ';':
			flush()
			i++
		case strings.HasPrefix(command[i:], "&&") || strings.HasPrefix(command[i:], "||"):
			flush()
			i += 2
		case c == '"' || c == '\'':
			end := strings.IndexByte(command[i+1:], c)
			if end < 0 {
				return nil, false
			}
			words = append(words, shellWord{text: command[i+1 : i+1+end], quoted: true})
			i += end + 2
			if i < len(command) && !wordEnds(command[i:]) {
				return nil, false
			}
		default:
			start := i
			for i < len(command) && !wordEnds(command[i:]) {
				if command[i] == '"' || command[i] == '\'' {
					return nil, false
				}
				i++
			}
			words = append(words, shellWord{text: command[start:i]})
		}
	}
	flush()
	return segments, true
}

func wordEnds(rest string) bool {
	return strings.IndexByte(" \t\n\r;", rest[0]) >= 0 || strings.HasPrefix(rest, "&&") || strings.HasPrefix(rest, "||")
}

func plainArgument(word shellWord) bool {
	if word.quoted {
		return !strings.HasSuffix(word.text, `\`) && strings.IndexFunc(word.text, func(r rune) bool { return r < ' ' || r == '$' }) < 0
	}
	if word.text == "2>&1" {
		return true
	}
	return strings.IndexFunc(word.text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(`-_.,:=/\~`, r)
	}) < 0
}

func ownBinary(word shellWord) bool {
	root := strings.TrimRight(strings.ReplaceAll(files.pluginRoot, `\`, "/"), "/")
	tail, rooted := word.text, false
	for _, placeholder := range []string{"${CLAUDE_PLUGIN_ROOT}", "$CLAUDE_PLUGIN_ROOT", "$env:CLAUDE_PLUGIN_ROOT"} {
		if rest, found := strings.CutPrefix(tail, placeholder); found {
			tail, rooted = rest, true
			break
		}
	}
	if root == "" || !plainArgument(shellWord{text: tail, quoted: word.quoted}) {
		return false
	}
	path := strings.ReplaceAll(tail, `\`, "/")
	if rooted {
		path = root + path
	}
	prefix := root + "/bin/"
	if len(path) <= len(prefix) || !strings.EqualFold(path[:len(prefix)], prefix) {
		return false
	}
	dir, name := "", strings.ToLower(path[len(prefix):])
	if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
		dir, name = name[:slash+1], name[slash+1:]
	}
	return (name == pluginName || name == pluginName+".exe") && (dir == "" || platformBinDir.MatchString(dir))
}

func onPostToolBatch(input, cfg object) {
	if insideSubagent(input) {
		onSubagentBatch(input, cfg)
		return
	}
	now := nowSec()
	sid := sessionKey(input)
	state := readState()
	clearOverload(state, sid)
	clearFailureRetries(state, sid)
	if guardPaused(cfg, state, now) {
		return
	}
	releaseInterruptedWait(sid, state)
	result := decide(cfg, state, input, now, decideOptions{})
	if result.fableHit {
		if observed(sid, "PostToolBatch", "switch-model", scopedLabel(cfg), usageFacts(result.usage)) {
			return
		}
		emit(object{"continue": false, "stopReason": handleFableHit("batch", input, cfg, result)})
		return
	}
	systemMessage, waitContext := "", ""
	if result.wait != nil && ownCommandsOnly(input) && ceilingHit(cfg, result.usage) == nil {
		journal(sid, "PostToolBatch", "control-batch", hitLabel(result.wait), usageFacts(result.usage))
		logInfo("batch of %s commands only for %s passes the pause point: %s %s%%", pluginName, sid, result.wait.window, formatNumber(result.wait.used))
	} else if result.wait != nil {
		if observed(sid, "PostToolBatch", "pause", hitLabel(result.wait), usageFacts(result.usage)) {
			return
		}
		outcome := enforceWait("batch", input, cfg, result)
		if outcome.stop != "" {
			emit(object{"continue": false, "stopReason": outcome.stop})
			return
		}
		systemMessage, waitContext = outcome.notice, withCutOffNote(sid, outcome.context)
	}
	if observing {
		return
	}
	systemMessage = joinNotices(systemMessage, result.notice)
	output := object{}
	if waitContext != "" {
		output["hookSpecificOutput"] = object{"hookEventName": "PostToolBatch", "additionalContext": waitContext}
	}
	if systemMessage != "" {
		output["systemMessage"] = systemMessage
	}
	if len(output) > 0 {
		emit(output)
	}

	stateStamp := fileStamp(files.state)
	if quietEligible(cfg, readState(), sid, result, result.usageStale) {
		writeQuietMarker(sid, stateStamp, result.contextPercent, result.hasContext, result.model)
	} else {
		clearQuietMarker(sid)
	}
}

func overloadDelay(cfg object, attempt float64) float64 {
	overload := section(cfg, "overload")
	base := math.Max(5, numberOr(overload, "baseSeconds", 30))
	ceiling := math.Max(base, numberOr(overload, "maxSeconds", 300))
	delay := math.Min(ceiling, base*math.Pow(2, math.Max(0, attempt-1)))
	jitter := (float64(nowSec()%1000)/1000 - 0.5) * 0.5 * delay
	return math.Min(ceiling, math.Max(base, delay+jitter))
}

func overloadEpisode(cfg object, sid string, now int64) (attempt float64, ok bool) {
	budget := math.Max(1, numberOr(section(cfg, "overload"), "maxTotalMinutes", 120)) * 60
	gap := 2 * math.Max(30, numberOr(section(cfg, "overload"), "maxSeconds", 300))
	updateState(func(state object) {
		episodes := stateMap(state, "overload")
		episode := getMap(episodes, sid)

		if episode == nil || float64(now)-numberOr(episode, "lastAt", 0) > gap+60 {
			episode = object{"firstAt": float64(now), "attempts": float64(0)}
		}
		episode["attempts"] = numberOr(episode, "attempts", 0) + 1
		episode["lastAt"] = float64(now)
		episodes[sid] = episode
		attempt = numberOr(episode, "attempts", 1)
		ok = float64(now)-numberOr(episode, "firstAt", 0) <= budget
	})
	return attempt, ok
}

func failureRetry(cfg object, sid string, now int64) (retry int) {
	updateState(func(state object) {
		episodes := stateMap(state, "failureRetries")
		episode := getMap(episodes, sid)
		if episode == nil || float64(now)-numberOr(episode, "lastAt", 0) > retryDelaySeconds(cfg, int(numberOr(episode, "retries", 1)))+failureEpisodeSlack {
			episode = object{"retries": float64(0)}
		}
		episode["retries"] = numberOr(episode, "retries", 0) + 1
		episode["lastAt"] = float64(now)
		episodes[sid] = episode
		retry = int(numberOr(episode, "retries", 1))
	})
	return retry
}

func clearFailureRetries(state object, sid string) {
	if getMap(getMap(state, "failureRetries"), sid) == nil {
		return
	}
	updateState(func(next object) { delete(stateMap(next, "failureRetries"), sid) })
}

func clearOverload(state object, sid string) {
	if getMap(getMap(state, "overload"), sid) == nil {
		return
	}
	updateState(func(next object) { delete(stateMap(next, "overload"), sid) })
}

var nonInteractiveEntrypoints = map[string]bool{"sdk-cli": true, "sdk-ts": true, "sdk-py": true, "claude-code-github-action": true}

const handOffLifetime = time.Hour

func handOffDir() string {
	return filepath.Join(files.guardDir, "pending")
}

func handsStopFailuresOff() bool {
	return currentHost().id == "claude" && nonInteractiveEntrypoints[os.Getenv("CLAUDE_CODE_ENTRYPOINT")]
}

func stopFailureHandedOff(sid string) bool {
	entries, _ := os.ReadDir(handOffDir())
	prefix := hashKey(sid) + "-"
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) < handOffLifetime {
			return true
		}
	}
	return false
}

func handOffStopFailure(raw object) bool {
	sid := sessionKey(raw)
	file := filepath.Join(handOffDir(), hashKey(sid)+"-"+strconv.Itoa(os.Getpid())+".json")
	if err := writeJSONAtomic(file, raw); err != nil {
		warn("StopFailure for %s handled in the hook: hand-off not written (%v)", sid, err)
		return false
	}
	if detachedSelf([]string{"hook", "--input", file, "--account", files.configDir}) > 0 {
		return true
	}
	_ = os.Remove(file)
	warn("StopFailure for %s handled in the hook: the detached copy did not start", sid)
	return false
}

func handedOffStopFailure(file string) (object, string) {
	resolved, err := filepath.Abs(file)
	if err != nil || filepath.Dir(resolved) != handOffDir() {
		warn("hook --input %s ignored: not a StopFailure hand-off", file)
		return nil, ""
	}
	payload := readJSON(resolved)
	if getString(payload, "hook_event_name") != "StopFailure" {
		_ = os.Remove(resolved)
		return nil, ""
	}
	logInfo("StopFailure for %s taken over from the claude -p hook", sessionKey(payload))
	return payload, resolved
}

func onStopFailure(input, cfg object) {

	if !currentHost().stopFailure {
		return
	}
	now := nowSec()
	sid := sessionKey(input)
	state := readState()
	errorType := firstString(input, "error_type", "error")
	errorText := joinNotices(getString(input, "error_message"), getString(input, "last_assistant_message"), getString(input, "error_details"))
	if errorType == "model_not_found" {
		healUnavailableModel(cfg, sid)
		return
	}
	if errorType == "billing_error" || errorType == "account_on_hold" || errorType == "authentication_failed" {
		if float64(now)-numberOr(getMap(state, "notified"), "account:"+errorType, 0) > 6*3600 {
			updateState(func(next object) { stateMap(next, "notified")["account:"+errorType] = float64(now) })
			journal(sid, "StopFailure", "account-error", errorType, nil)
			fail("%s for %s: nothing to retry; the account needs attention", errorType, sid)
			notify(cfg, pluginName, T("stopfailure.account", errorType))
		}
		return
	}
	overloaded := errorType == "overloaded" || errorType == "server_error"
	attempt, retryable := 0.0, true
	if overloaded {
		attempt, retryable = overloadEpisode(cfg, sid, now)
		if !retryable {
			journal(sid, "StopFailure", "overload-giveup", errorType, object{"attempts": attempt})
			fail("%s for %s: retry budget spent after %s attempts; leaving the session stopped", errorType, sid, formatNumber(attempt))
			notify(cfg, pluginName, T("overload.giveup", errorType, formatNumber(attempt)))
			return
		}
	}
	result := decide(cfg, state, input, now, decideOptions{force: true, noProbe: true})
	usage := result.usage

	hint := limitHint(errorText, scopedLabel(cfg), usage)
	nearCap := func(win *window) bool { return win != nil && win.used >= culpritFloor }
	weeklyCulprit := hint == "seven_day" || (hint == "" && nearCap(usage.sevenDay) && (!nearCap(usage.fiveHour) || usage.sevenDay.resetsAt > usage.fiveHour.resetsAt))
	fiveCulprit := hint == "five_hour" || (hint == "" && nearCap(usage.fiveHour))
	fableCulprit := scopedModelPattern(cfg).MatchString(result.model) && usage.fable != nil && (hint == "fable" || (hint == "" && usage.fable.used >= culpritFloor))
	waitCfg := section(cfg, "wait")
	margin := math.Max(0, numberOr(waitCfg, "resetMarginSeconds", 0)) + math.Max(0, numberOr(waitCfg, "builtinGraceSeconds", 0))
	record := object{"kind": "stopfailure", "threshold": nil}
	switch {
	case overloaded:
		delay := overloadDelay(cfg, attempt)
		record["window"], record["label"], record["used"] = "unknown", errorType, nil
		record["until"], record["resumeAt"] = float64(now), float64(now)+delay
		record["overload"], record["attempt"] = true, attempt
	case weeklyCulprit:
		record["window"], record["label"], record["used"] = "seven_day", windowLabel("seven_day"), usage.sevenDay.used
		record["until"], record["resumeAt"] = usage.sevenDay.resetsAt, usage.sevenDay.resetsAt+margin
	case fiveCulprit:
		record["window"], record["label"], record["used"] = "five_hour", windowLabel("five_hour"), usage.fiveHour.used
		record["until"], record["resumeAt"] = usage.fiveHour.resetsAt, usage.fiveHour.resetsAt+margin
	case fableCulprit:
		if observed(sid, "StopFailure", "switch-model", scopedLabel(cfg), usageFacts(usage)) {
			return
		}
		persistModelSwitch(cfg, usage.fable.resetsAt, now)
		record["window"], record["label"], record["used"] = "fable", scopedLabel(cfg), usage.fable.used
		record["until"], record["resumeAt"] = float64(now), float64(now+120)
		record["modelOverride"] = getString(section(cfg, "models"), "fallback")
	default:
		retry := failureRetry(cfg, sid, now)
		if retry > stopFailureMaxAttempts {
			manual := hostResumeCommand(currentHost().id, sid)
			journal(sid, "StopFailure", "retry-giveup", errorType, object{"retries": float64(retry - 1)})
			fail("%s StopFailure for %s: failed again after %d retries; leaving the session stopped", errorType, sid, retry-1)
			notify(cfg, pluginName, T("stopfailure.giveup", shortSid(sid), errorType, formatNumber(float64(retry-1)), manual))
			updateState(func(next object) {
				stateMap(next, "launchFailures")[sid] = object{"at": float64(now), "model": result.model}
			})
			return
		}
		record["window"], record["label"], record["used"] = "unknown", windowLabel("unknown"), nil
		record["until"], record["resumeAt"] = float64(now), float64(now)+retryDelaySeconds(cfg, retry)
	}
	resumeAt := numberOr(record, "resumeAt", 0)
	label := getString(record, "label")
	if observed(sid, "StopFailure", "schedule-resume", label, object{"resumeAt": resumeAt}) {
		return
	}
	checkpoint := buildCheckpoint(input, T("stopfailure.reason", label, formatTime(resumeAt)), result.model, cfg)
	record["inHook"], record["cwd"], record["transcript"] = false, getString(input, "cwd"), getString(input, "transcript_path")
	record["checkpoint"], record["queuedPrompt"], record["startedAt"], record["attempts"] = checkpoint, "", float64(now), float64(0)
	record["permissionMode"] = permissionModeOf(input)
	recordTree(cfg, record, getString(input, "cwd"))
	wakeCfg := section(cfg, "wake")
	interactive := flagString("input") == "" && getMap(getMap(readJSON(files.usage), "sessions"), sid) != nil
	wakeable := currentHost().wake && getBool(wakeCfg, "sameSession", true) && interactive && getString(record, "window") != "fable" && resumeAt-float64(now) <= math.Max(1, numberOr(wakeCfg, "maxMinutes", 330))*60
	runnerAt := resumeAt
	if wakeable {
		runnerAt += math.Max(60, numberOr(wakeCfg, "graceSeconds", 300))
	}

	if !registerWait(sid, record, cfg) {
		emit(object{"systemMessage": T("wait.notStored", pluginName)})
		return
	}
	scheduleRunner(cfg, sid, runnerAt)
	if overloaded {
		journal(sid, "StopFailure", "overload-backoff", errorType, object{"attempt": attempt, "delaySeconds": math.Round(resumeAt - float64(now)), "wake": wakeable})
		warn("%s StopFailure for %s: attempt %s, retry in %ss wake=%t", errorType, sid, formatNumber(attempt), formatNumber(math.Round(resumeAt-float64(now))), wakeable)
		if attempt == 1 {
			notify(cfg, pluginName, T("overload.notify", errorType, formatTime(resumeAt)))
		}
	} else {
		journal(sid, "StopFailure", "schedule-resume", label, object{"resumeAt": resumeAt, "wake": wakeable, "window": getString(record, "window"), "hint": hint, "message": truncateText(errorText, 160)})
		warn("rate_limit StopFailure for %s: culprit=%s resumeAt=%s wake=%t", sid, getString(record, "window"), localISO(resumeAt), wakeable)
		if getString(record, "window") == "unknown" {
			notify(cfg, pluginName, T("stopfailure.transient", formatTime(resumeAt)))
		} else {
			notify(cfg, pluginName, T("stopfailure.wall", label, formatTime(resumeAt)))
		}
	}
	if wakeable {
		wakeSameSession(cfg, sid, record, resumeAt)
	}
}

func healUnavailableModel(cfg object, sid string) {
	models := section(cfg, "models")
	current := settingsModel()
	fallback := getString(models, "fallback")
	next := fallback
	if current == "" || current == fallback || fallback == "" {
		next = ""
	}
	if next == "" {
		withSettings(func(data object) bool {
			if data["model"] == nil {
				return false
			}
			delete(data, "model")
			return true
		})
	} else {
		setSettingsModel(next)
	}
	journal(sid, "StopFailure", "model-unavailable", orDefault(current, "(default)"), object{"next": orDefault(next, "(claude default)")})
	fail("model_not_found for %s: settings.model %q is not available on this plan; now %q", sid, current, orDefault(next, "(claude default)"))
	notify(cfg, pluginName, T("stopfailure.modelMissing", orDefault(current, "?"), orDefault(next, "Claude Code default")))
}

func wakeSameSession(cfg object, sid string, record object, resumeAt float64) {
	startedAt := numberOr(record, "startedAt", 0)
	watch := newWaitWatch(cfg, sid, true)
	watch.startedAt = startedAt

	updateState(func(next object) {
		if wait := getMap(getMap(next, "waits"), sid); wait != nil && numberOr(wait, "startedAt", -1) == startedAt {
			wait["waking"] = float64(nowSec())
		}
	})
	defer updateState(func(next object) {
		if wait := getMap(getMap(next, "waits"), sid); wait != nil {
			delete(wait, "waking")
		}
	})
	sleepUntilEvery(resumeAt, watch.tickSeconds(), watch.tick)
	state := readState()
	wait := getMap(getMap(state, "waits"), sid)
	if watch.cancelled || wait == nil || numberOr(wait, "startedAt", -1) != startedAt {
		logInfo("wake %s: wait cleared meanwhile, nothing to do", sid)
		return
	}
	if watch.early {
		journal(sid, "StopFailure", "early-reset", getString(record, "label"), nil)
		logInfo("wake %s: window cleared ahead of schedule; waking now", sid)
	}
	auto := getMap(getMap(state, "autoResume"), sid)
	if getString(auto, "type") == "quota_auto_resume_fired" && numberOr(auto, "at", 0) >= startedAt {
		logInfo("wake %s: builtin auto-continue already fired", sid)
		return
	}
	if getString(record, "window") != "unknown" || getBool(record, "overload", false) {
		check := decide(cfg, state, object{"session_id": sid, "cwd": getString(record, "cwd"), "transcript_path": getString(record, "transcript")}, nowSec(), decideOptions{force: true, noProbe: true})
		if check.wait != nil && check.wait.until > float64(nowSec()+120) {
			logInfo("wake %s: limit still active (%s %s%%), leaving it to the runner", sid, check.wait.label, formatNumber(check.wait.used))
			return
		}
		if check.fableHit {
			logInfo("wake %s: %s is over its pause point, leaving it to the runner, which relaunches on the fallback role", sid, scopedLabel(cfg))
			return
		}
	}
	updateState(func(next object) {
		if current := getMap(getMap(next, "waits"), sid); current != nil {
			current["wakeAttemptedAt"] = float64(nowSec())

			delete(current, "waking")
		}
	})
	journal(sid, "StopFailure", "wake-same-session", getString(record, "label"), object{"resumeAt": resumeAt})
	logInfo("wake %s: waking the session in place (exit 2)", sid)
	message := T("wait.wakeMessage", getString(record, "label"), formatTime(resumeAt), formatNumber(numberOr(record, "used", 0)))
	if getBool(record, "overload", false) {
		message = T("overload.wakeMessage", getString(record, "label"), formatNumber(numberOr(record, "attempt", 1)), durationText(resumeAt-numberOr(record, "startedAt", resumeAt)))
	}
	if workspaceChanged(wait) {
		journal(sid, "StopFailure", "workspace-changed", "tree differs from the checkpoint", nil)
		message += " " + T("workspace.context")
	}
	if note := workflowResumeNote(state, sid); note != "" {
		message += " " + note
	}
	fmt.Fprint(os.Stdout, message)
	os.Exit(2)
}

func onNotification(input, cfg object) {
	now := nowSec()
	sid := sessionKey(input)
	kind := getString(input, "notification_type")
	if kind == "" {
		kind = getString(input, "type")
	}
	if kind == "" {
		kind = getString(input, "matcher")
	}
	state := updateState(func(next object) {
		stateMap(next, "autoResume")[sid] = object{"type": kind, "at": float64(now)}
	})
	switch kind {
	case "quota_auto_resume_fired":
		if getMap(getMap(state, "waits"), sid) != nil {
			clearWait(sid, state)
		}
		logInfo("builtin auto-continue fired for %s; runner cancelled", sid)
	case "quota_auto_resume_stale":
		notify(cfg, pluginName, T("notification.stale"))
	case "quota_auto_resume_disabled":
		notify(cfg, pluginName, T("notification.disabled"))
	}
}

func firstString(source object, keys ...string) string {
	for _, key := range keys {
		if value := getString(source, key); value != "" {
			return value
		}
	}
	return ""
}

func onTaskEvent(input, _ object) {
	sid := sessionKey(input)
	id := firstString(input, "task_id", "id")
	if id == "" {
		return
	}
	subject := truncateText(firstString(input, "task_subject", "subject", "task_description", "description"), 160)
	completed := getString(input, "hook_event_name") == "TaskCompleted"
	now := float64(nowSec())
	updateState(func(state object) {
		entry := getMap(stateMap(state, "tasks"), sid)
		if entry == nil || getMap(entry, "items") == nil {
			entry = object{"items": object{}}
		}
		items := getMap(entry, "items")
		dropFinishedTasks(items)
		if completed {
			delete(items, id)
		} else {
			if subject == "" {
				subject = orDefault(getString(getMap(items, id), "subject"), id)
			}
			items[id] = object{"subject": subject, "status": "open", "at": now}
			dropOldestTasks(items)
		}
		if len(items) == 0 {
			delete(stateMap(state, "tasks"), sid)
			return
		}
		entry["at"] = now
		stateMap(state, "tasks")[sid] = entry
	})
}

func onPostToolUse(input, cfg object) {
	if !agentTools[getString(input, "tool_name")] || insideSubagent(input) {
		return
	}
	toolInput := getMap(input, "tool_input")
	requested := firstString(toolInput, "subagent_type", "agent")
	if !strings.Contains(requested, liteAgentType(cfg)) {
		return
	}
	sid := sessionKey(input)
	state := readState()
	signal := getString(getMap(getMap(state, "routes"), sid), "signal")
	if signal == "" {
		return
	}
	var response string
	if text, ok := input["tool_response"].(string); ok {
		response = text
	} else if input["tool_response"] != nil {
		response = string(marshalCompact(input["tool_response"]))
	}
	misrouted := needsCodePattern.MatchString(response)
	updateState(func(next object) {
		learned := stateMap(next, "routerLearned")
		record := getMap(learned, signal)
		if record == nil {
			record = object{"misroutes": float64(0), "ok": float64(0)}
		}
		if misrouted {
			record["misroutes"] = numberOr(record, "misroutes", 0) + 1
		} else {
			record["ok"] = numberOr(record, "ok", 0) + 1
		}
		record["at"] = float64(nowSec())
		learned[signal] = record
	})
	if misrouted {
		verdictText := "watching that signal"
		if numberOr(getMap(getMap(state, "routerLearned"), signal), "misroutes", 0)+1 >= learnedBlockMisroutes {
			verdictText = "that signal no longer routes"
		}
		journal(sid, "PostToolUse", "learn-misroute", signal, nil)
		warn(`lite agent returned NEEDS_CODE for a prompt routed on "%s"; %s`, signal, verdictText)
	}
}

func onPostModelSwitch(input, _ object) {
	sid := sessionKey(input)
	model := getString(input, "to_model")
	if model == "" {
		return
	}
	updateState(func(state object) {
		stateMap(state, "modelOverrides")[sid] = object{"model": model, "at": float64(nowSec())}
	})
}

func emitFallback() {
	fallback := hostDefaultOutput(activeHost, activeEvent)
	if notice := miswiredNotice(); notice != "" {
		if fallback == nil {
			fallback = object{}
		}
		fallback["systemMessage"] = joinNotices(getString(fallback, "systemMessage"), notice)
	}
	if fallback != nil {
		os.Stdout.Write(marshalCompact(fallback))
	}
}

func copilotEventName(raw object) string {
	has := func(key string) bool {
		_, ok := raw[key]
		return ok
	}
	switch {
	case has("stopReason"):
		if has("agentId") || has("agentName") {
			return "subagentStop"
		}
		return "agentStop"
	case has("errorContext"):
		if getBool(raw, "recoverable", true) {
			return ""
		}
		return "errorOccurred"
	case has("toolName"):
		switch {
		case has("toolResult"):
			return "postToolUse"
		case has("error"):
			return "postToolUseFailure"
		}
		return "preToolUse"
	case has("prompt"):
		if has("transformedPrompt") {
			return "userPromptTransformed"
		}
		return "userPromptSubmitted"
	case has("reason"):
		return "sessionEnd"
	case has("source") || has("initialPrompt"):
		return "sessionStart"
	}
	return ""
}

func runHook() {
	var raw object
	if handedOff := flagString("input"); handedOff != "" {
		signal.Ignore(syscall.SIGTERM)
		payload, file := handedOffStopFailure(handedOff)
		if payload == nil {
			return
		}
		defer os.Remove(file)
		raw = payload
	} else {
		raw = readStdinJSON()
		if getString(raw, "hook_event_name") == "StopFailure" && handsStopFailuresOff() {
			signal.Ignore(syscall.SIGTERM)
			if handOffStopFailure(raw) {
				return
			}
		}
	}
	if os.Getenv("NOCTIS_DEBUG_HOOKS") != "" {

		_ = appendRotating(filepath.Join(files.guardDir, "hooks-debug.log"), localISO(float64(nowSec()))+" "+activeHost+" "+string(marshalCompact(raw)))
	}
	if activeHost == "copilot" && getString(raw, "hook_event_name") == "" {
		if event := copilotEventName(raw); event != "" {
			raw["hook_event_name"] = event
		}
	}
	activeEvent = getString(raw, "hook_event_name")
	event, input := normalizeHookInput(activeHost, raw)
	if input == nil {
		input = object{}
	}
	if event == "" {
		emitFallback()
		return
	}
	input["hook_event_name"] = event
	handlers := map[string]func(object, object){
		"SessionStart":      onSessionStart,
		"SessionEnd":        onSessionEnd,
		"UserPromptSubmit":  onUserPromptSubmit,
		"PreToolUse":        onPreToolUse,
		"PostToolUse":       onPostToolUse,
		"PostToolBatch":     onPostToolBatch,
		"Stop":              onStop,
		"StopFailure":       onStopFailure,
		"Notification":      onNotification,
		"PostModelSwitch":   onPostModelSwitch,
		"TaskCreated":       onTaskEvent,
		"TaskCompleted":     onTaskEvent,
		"PermissionRequest": onPermissionRequest,
	}
	handler, ok := handlers[event]
	if !ok {
		emitFallback()
		return
	}
	if event == "PostToolBatch" && quietFastPath(input) {
		emitFallback()
		return
	}
	cfg := loadConfig()
	sid := sessionKey(input)
	promptFromPlugin = false
	if prompt := getString(input, "prompt"); prompt != "" && event == "UserPromptSubmit" {
		promptFromPlugin = pluginComposedPrompt(cfg, sid, prompt)
		if !promptFromPlugin {
			rememberSessionLanguage(sid, prompt)
		}
	}
	applySessionLocale(cfg, readState(), sid)
	handler(input, cfg)
	if !emitted {
		emitFallback()
	}
}
