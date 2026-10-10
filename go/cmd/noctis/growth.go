package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	growthCallsDefault           = 60.0
	growthContextDefault         = 120000.0
	growthGraceDefault           = 10.0
	growthTailBytes              = 256 * 1024
	growthTailMaxBytes           = 4 * 1024 * 1024
	subagentCompactWindowDefault = 200000.0
	agentCompactWindowMin        = 100000.0
	agentCompactWindowMax        = 1000000.0
)

var assistantMarker = []byte(`"assistant"`)

var builtinSubagentBudgets = object{
	pluginName + ":lite":   object{"maxToolCalls": 20.0, "reportWords": 450.0},
	pluginName + ":worker": object{"reportWords": 300.0},
}

type growthLimits struct {
	guard                        bool
	calls, context, grace, words float64
}

func sameAgentType(name, kind string) bool {
	return name == kind || strings.TrimPrefix(name, pluginName+":") == strings.TrimPrefix(kind, pluginName+":")
}

func subagentBudget(settings object, kind string) object {
	budgets, listed := settings["budgets"].(object)
	if !listed {
		budgets = builtinSubagentBudgets
	}
	name := strings.TrimPrefix(kind, pluginName+":")
	budget := object{}
	for _, key := range []string{pluginName + ":" + name, name} {
		mergeInto(budget, toObject(budgets[key]))
	}
	return budget
}

func subagentGrowthLimits(cfg object, kind string) growthLimits {
	settings := section(cfg, "subagents")
	budget := subagentBudget(settings, kind)
	return growthLimits{
		guard:   getBool(settings, "guard", true),
		calls:   numberOr(budget, "maxToolCalls", numberOr(settings, "maxToolCalls", growthCallsDefault)),
		context: numberOr(budget, "maxContextTokens", numberOr(settings, "maxContextTokens", growthContextDefault)),
		grace:   numberOr(budget, "graceToolCalls", numberOr(settings, "graceToolCalls", growthGraceDefault)),
		words:   numberOr(budget, "reportWords", 0),
	}
}

func agentGrowthLimits(input, cfg object) (growthLimits, bool) {
	kind := getString(input, "agent_type")
	limits := subagentGrowthLimits(cfg, kind)
	return limits, limits.active() && !subagentLimits(cfg).exempts(kind)
}

func (limits growthLimits) active() bool {
	return limits.guard && (limits.calls > 0 || limits.context > 0)
}

func (limits growthLimits) text() string {
	parts := []string{}
	if limits.calls > 0 {
		parts = append(parts, formatNumber(limits.calls)+" tool calls")
	}
	if limits.context > 0 {
		parts = append(parts, formatNumber(limits.context)+" tokens of context")
	}
	return strings.Join(parts, " and ")
}

func (limits growthLimits) reportText() string {
	if limits.words <= 0 {
		return ""
	}
	return " Keep your final report under " + formatNumber(limits.words) + " words."
}

func plainID(id string) bool {
	return id != "" && safeName(id) == id && strings.Trim(id, ".") != ""
}

func agentTranscript(input object) string {
	if path := getString(input, "agent_transcript_path"); path != "" {
		return path
	}
	main, sid, agent := getString(input, "transcript_path"), getString(input, "session_id"), getString(input, "agent_id")
	if main == "" || !plainID(sid) || !plainID(agent) {
		return ""
	}
	return filepath.Join(filepath.Dir(main), sid, "subagents", "agent-"+agent+".jsonl")
}

func growthRecordPath(input object) string {
	agent := getString(input, "agent_id")
	if !plainID(agent) {
		return ""
	}
	return filepath.Join(files.guardDir, "subagents", sessionKey(input)+"-"+agent+".json")
}

func lastContext(path string) float64 {
	if path == "" {
		return 0
	}
	transcript, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer transcript.Close()
	info, err := transcript.Stat()
	if err != nil {
		return 0
	}
	for span := int64(growthTailBytes); ; span *= 4 {
		start := max(info.Size()-span, 0)
		tail := make([]byte, info.Size()-start)
		if read, _ := transcript.ReadAt(tail, start); read < len(tail) {
			tail = tail[:read]
		}
		if tokens, found := contextInTail(tail, start > 0); found {
			return tokens
		}
		if start == 0 || span >= growthTailMaxBytes {
			return 0
		}
	}
}

func contextInTail(tail []byte, cutFirst bool) (float64, bool) {
	for end := len(tail); end > 0; {
		start := bytes.LastIndexByte(tail[:end], '\n') + 1
		line := tail[start:end]
		if (start > 0 || !cutFirst) && bytes.Contains(line, usageMarker) && bytes.Contains(line, assistantMarker) {
			var raw object
			if jsonUnmarshalObject(line, &raw) == nil && getString(raw, "type") == "assistant" {
				if usage := getMap(getMap(raw, "message"), "usage"); usage != nil {
					return costUsageOf(usage).context(), true
				}
			}
		}
		end = start - 1
	}
	return 0, false
}

type growthVerdict struct {
	warn, stop, again bool
	calls, context    float64
	who, why          string
}

func growthReason(verdict growthVerdict, limits growthLimits) string {
	return fmt.Sprintf("[noctis] This subagent has made %s tool calls and its context is %s tokens, past its budget of %s; every further call re-reads all of it. Stop exploring now and return your result the way you normally deliver your report: what you found or changed, and what is left.%s After %s more tool calls this agent is stopped.",
		formatNumber(verdict.calls), formatNumber(verdict.context), limits.text(), limits.reportText(), formatNumber(limits.grace))
}

func checkGrowth(input object, limits growthLimits, now int64) growthVerdict {
	path := growthRecordPath(input)
	if path == "" {
		return growthVerdict{}
	}
	record := readJSON(path)
	if record == nil {
		record = object{"at": float64(now), "kind": getString(input, "agent_type")}
	}
	verdict := growthVerdict{who: strings.TrimSpace(getString(input, "agent_type") + " " + getString(input, "agent_id"))}
	verdict.calls = numberOr(record, "calls", 0) + float64(len(getList(input, "tool_calls")))
	if limits.context > 0 {
		verdict.context = lastContext(agentTranscript(input))
	}
	record["calls"], record["peak"] = verdict.calls, max(numberOr(record, "peak", 0), verdict.context)
	warned, stopped := numberOr(record, "warned", 0), numberOr(record, "stopped", 0)
	switch {
	case warned > 0 && verdict.calls-warned >= limits.grace:
		verdict.stop, verdict.again = true, stopped > 0
		record["stopped"] = verdict.calls
		verdict.why = fmt.Sprintf("%s: %s tool calls, %s after the wrap-up note (grace %s)", verdict.who, formatNumber(verdict.calls), formatNumber(verdict.calls-warned), formatNumber(limits.grace))
	case warned == 0 && (reached(verdict.calls, limits.calls) || reached(verdict.context, limits.context)):
		verdict.warn = true
		record["warned"] = verdict.calls
		verdict.why = fmt.Sprintf("%s: %s tool calls, context %s tokens, budget %s", verdict.who, formatNumber(verdict.calls), formatNumber(verdict.context), limits.text())
	}
	if err := writeEncodedAtomic(path, marshalCompact(record)); err != nil {
		warn("subagent record %s: %v", filepath.Base(path), err)
	}
	return verdict
}

func limitGrowth(input, cfg object, now int64) bool {
	limits, active := agentGrowthLimits(input, cfg)
	if !active {
		return false
	}
	verdict := checkGrowth(input, limits, now)
	sid := sessionKey(input)
	switch {
	case verdict.stop:
		if observing && verdict.again || observed(sid, "PostToolBatch", "stop-subagent-growth", verdict.why, nil) {
			return false
		}
		journal(sid, "PostToolBatch", "stop-subagent-growth", verdict.why, nil)
		warn("subagent %s of %s stopped: %s", verdict.who, sid, verdict.why)
		noteGrowthStop(sid, verdict.who, verdict.calls, now)
		emit(object{"continue": false, "stopReason": T("notice.subagentGrowthStop", verdict.who, formatNumber(verdict.calls))})
		return true
	case verdict.warn:
		if observed(sid, "PostToolBatch", "warn-subagent-growth", verdict.why, nil) {
			return false
		}
		journal(sid, "PostToolBatch", "warn-subagent-growth", verdict.why, nil)
		logInfo("subagent %s of %s told to wrap up: %s", verdict.who, sid, verdict.why)
		emit(object{"hookSpecificOutput": object{"hookEventName": "PostToolBatch", "additionalContext": growthReason(verdict, limits)}})
		return true
	}
	return false
}

func noteGrowthStop(sid, who string, calls float64, now int64) {
	updateState(func(state object) {
		stopped := stateMap(stateMap(state, spawnStateKey), "stopped")
		stopped[sid] = append(getList(stopped, sid), object{"who": who, "calls": calls, "at": float64(now)})
	})
}

func takeGrowthStops(sid string) string {
	if len(getList(getMap(getMap(peekState(), spawnStateKey), "stopped"), sid)) == 0 {
		return ""
	}
	var taken []any
	updateState(func(state object) {
		stopped := getMap(getMap(state, spawnStateKey), "stopped")
		taken = getList(stopped, sid)
		delete(stopped, sid)
	})
	lines := []string{}
	for _, raw := range taken {
		entry := toObject(raw)
		lines = append(lines, fmt.Sprintf("%s after %s tool calls", getString(entry, "who"), formatNumber(numberOr(entry, "calls", 0))))
	}
	if len(lines) == 0 {
		return ""
	}
	return "[noctis] noctis stopped subagents of this session that went past their budget and did not wrap up: " + strings.Join(lines, "; ") + ". What they returned may be incomplete. Do not open another subagent for the same task; finish it yourself from what they returned, reading only what is still missing."
}
