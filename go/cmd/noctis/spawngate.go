package main

import (
	"fmt"
	"math"
)

const (
	spawnSessionWarnDefault = 10.0
	spawnSessionDenyDefault = 20.0
	spawnHourWarnDefault    = 6.0
	spawnHourDenyDefault    = 12.0
	spawnWeeklyRoomDefault  = 15.0
	spawnHourSeconds        = 3600
	spawnStateKey           = "spawns"
)

type spawnLimits struct {
	guard                                                    bool
	sessionWarn, sessionDeny, hourWarn, hourDeny, weeklyRoom float64
	exempt                                                   []string
}

func subagentLimits(cfg object) spawnLimits {
	settings := section(cfg, "subagents")
	limits := spawnLimits{
		guard:       getBool(settings, "guard", true),
		sessionWarn: numberOr(settings, "perSessionWarn", spawnSessionWarnDefault),
		sessionDeny: numberOr(settings, "perSessionDeny", spawnSessionDenyDefault),
		hourWarn:    numberOr(settings, "perHourWarn", spawnHourWarnDefault),
		hourDeny:    numberOr(settings, "perHourDeny", spawnHourDenyDefault),
		weeklyRoom:  numberOr(settings, "weeklyRoom", spawnWeeklyRoomDefault),
		exempt:      []string{digestAgentType(cfg)},
	}
	if listed, ok := settings["exempt"].([]any); ok {
		limits.exempt = nil
		for _, raw := range listed {
			if name, ok := raw.(string); ok && name != "" {
				limits.exempt = append(limits.exempt, name)
			}
		}
	}
	return limits
}

func (limits spawnLimits) exempts(kind string) bool {
	for _, name := range limits.exempt {
		if sameAgentType(name, kind) {
			return true
		}
	}
	return false
}

func reached(count, limit float64) bool {
	return limit > 0 && count >= limit
}

func spawnedType(input object) string {
	return orDefault(firstString(getMap(input, "tool_input"), "subagent_type", "agent"), "general-purpose")
}

func weeklyRoomLeft(cfg object, usage usageView) float64 {
	target, guarded := stopPoint(cfg, "weeklyAll")
	if !guarded || usage.sevenDay == nil {
		return math.Inf(1)
	}
	return target - usage.sevenDay.used
}

type spawnVerdict struct {
	deny, warn    bool
	kind, level   string
	why           string
	claude, user  string
	session, hour float64
}

func claimSpawn(limits spawnLimits, sid string, now int64) (session, hour float64, over string) {
	updateState(func(state object) {
		spawns := stateMap(state, spawnStateKey)
		pruneSpawns(spawns, now)
		recent := getList(spawns, "recent")
		sessions := stateMap(spawns, "sessions")
		session, hour = numberOr(toObject(sessions[sid]), "count", 0), float64(len(recent))
		switch {
		case reached(session, limits.sessionDeny):
			over = "session"
		case reached(hour, limits.hourDeny):
			over = "hour"
		default:
			session++
			hour++
			if sid != selftestSession {
				sessions[sid] = object{"count": session, "at": float64(now)}
				recent = append(recent, float64(now))
			}
		}
		spawns["recent"] = recent
	})
	return session, hour, over
}

func pruneSpawns(spawns object, now int64) {
	if spawns == nil {
		return
	}
	sessions := getMap(spawns, "sessions")
	for sid, raw := range sessions {
		if float64(now)-numberOr(toObject(raw), "at", 0) > stateEntryTTLSeconds {
			delete(sessions, sid)
		}
	}
	recent := []any{}
	for _, raw := range getList(spawns, "recent") {
		if at, ok := toNumber(raw); ok && at > float64(now-spawnHourSeconds) {
			recent = append(recent, at)
		}
	}
	spawns["recent"] = recent
	stopped := getMap(spawns, "stopped")
	for sid := range stopped {
		kept := []any{}
		for _, entry := range getList(stopped, sid) {
			if float64(now)-numberOr(toObject(entry), "at", 0) <= stateEntryTTLSeconds {
				kept = append(kept, entry)
			}
		}
		if len(kept) == 0 {
			delete(stopped, sid)
		} else {
			stopped[sid] = kept
		}
	}
}

func gateSpawn(input, cfg object, usage usageView, burn burnView, now int64) spawnVerdict {
	limits := subagentLimits(cfg)
	verdict := spawnVerdict{kind: spawnedType(input)}
	if !limits.guard {
		return verdict
	}
	if limits.exempts(verdict.kind) {
		verdict.why = verdict.kind + ": exempt (subagents.exempt)"
		return verdict
	}
	room := weeklyRoomLeft(cfg, usage)
	switch {
	case limits.weeklyRoom > 0 && room < limits.weeklyRoom:
		verdict.deny, verdict.level = true, "room"
		verdict.why = verdict.kind + ": " + roomWhy(room, limits)
		verdict.claude = spawnRefusal(roomLeftText(room, englishWindowLabel("seven_day")) + ", too little for a subagent")
		verdict.user = T("notice.spawnRoom", formatNumber(roundTo(room, 1)), formatNumber(limits.weeklyRoom))
		return verdict
	case burn.stop:
		_, lookback, _ := burnSettings(cfg)
		verdict.deny, verdict.level = true, "burn"
		verdict.why = verdict.kind + ": " + burnWhy(burn)
		verdict.claude = burn.burnReason(lookback, "New subagents")
		return verdict
	}
	session, hour, over := claimSpawn(limits, sessionKey(input), now)
	verdict.session, verdict.hour = session, hour
	switch over {
	case "session":
		verdict.deny, verdict.level = true, "session"
		verdict.why = verdict.kind + ": " + sessionWhy(session, limits)
		verdict.claude = spawnRefusal(fmt.Sprintf("this session has opened as many subagents as it may (%s, subagents.perSessionDeny)", formatNumber(session)))
		verdict.user = T("notice.spawnSession", formatNumber(session))
	case "hour":
		verdict.deny, verdict.level = true, "hour"
		verdict.why = verdict.kind + ": " + hourWhy(hour, limits)
		verdict.claude = spawnRefusal(fmt.Sprintf("as many subagents as an hour allows were opened in the last hour (%s, subagents.perHourDeny)", formatNumber(hour)))
		verdict.user = T("notice.spawnHour", formatNumber(hour))
	default:
		verdict.why = fmt.Sprintf("%s: %s in this session, %s in the last hour", verdict.kind, formatNumber(session), formatNumber(hour))
		if reached(session, limits.sessionWarn) || reached(hour, limits.hourWarn) {
			verdict.warn, verdict.level = true, "warn"
			verdict.claude = fmt.Sprintf("[noctis] This session has opened %s subagents (%s in the last hour). Each starts with a context of its own and re-reads it on every call. Do work that needs few reads yourself, and give each subagent a brief that carries what it needs, so it does not explore.", formatNumber(session), formatNumber(hour))
			verdict.user = T("notice.spawnWarn", formatNumber(session), formatNumber(hour))
		}
	}
	return verdict
}

func roomWhy(room float64, limits spawnLimits) string {
	return fmt.Sprintf("%s points left before the weekly pause point, under subagents.weeklyRoom %s", formatNumber(roundTo(room, 1)), formatNumber(limits.weeklyRoom))
}

func burnWhy(burn burnView) string {
	return fmt.Sprintf("weekly burn %s points/h, pause point in %s, %s before the reset", burn.paceText(), englishDurationText(burn.runOut), englishDurationText(burn.resetIn-burn.runOut))
}

func sessionWhy(session float64, limits spawnLimits) string {
	return fmt.Sprintf("%s already opened in this session, limit subagents.perSessionDeny %s", formatNumber(session), formatNumber(limits.sessionDeny))
}

func hourWhy(hour float64, limits spawnLimits) string {
	return fmt.Sprintf("%s opened in the last hour, limit subagents.perHourDeny %s", formatNumber(hour), formatNumber(limits.hourDeny))
}

func spawnHeadroom(cfg object, usage usageView, burn burnView, sid string, now int64) string {
	limits := subagentLimits(cfg)
	if !limits.guard {
		return ""
	}
	if room := weeklyRoomLeft(cfg, usage); limits.weeklyRoom > 0 && room < limits.weeklyRoom {
		return roomWhy(room, limits)
	}
	if burn.stop {
		return burnWhy(burn)
	}
	spawns := getMap(peekState(), spawnStateKey)
	if record := getMap(getMap(spawns, "sessions"), sid); float64(now)-numberOr(record, "at", 0) <= stateEntryTTLSeconds {
		if session := numberOr(record, "count", 0); reached(session, limits.sessionDeny) {
			return sessionWhy(session, limits)
		}
	}
	hour := 0.0
	for _, raw := range getList(spawns, "recent") {
		if at, ok := toNumber(raw); ok && at > float64(now-spawnHourSeconds) {
			hour++
		}
	}
	if reached(hour, limits.hourDeny) {
		return hourWhy(hour, limits)
	}
	return ""
}

func spawnHeadroomNow(cfg object, sid string, now int64) string {
	usage := currentUsage(now)
	return spawnHeadroom(cfg, usage, weeklyBurn(cfg, usage, now), sid, now)
}

func noSubagentNote(why string) string {
	return " No subagent can be opened now (" + why + "): do the next item in this session yourself, reading only what it needs."
}

func spawnRefusal(cause string) string {
	return "[noctis] New subagent refused: " + cause + ". Each subagent starts with a context of its own and re-reads it on every call, which is where most of the weekly limit goes. Do this work yourself in this session, reading only what it needs; do not call the Agent tool for it again."
}

func applySpawnGate(input, cfg object, usage usageView, burn burnView, now int64) spawnVerdict {
	verdict := gateSpawn(input, cfg, usage, burn, now)
	if verdict.why == "" {
		return spawnVerdict{}
	}
	sid := sessionKey(input)
	action := "spawn-subagent"
	switch {
	case verdict.deny:
		action = "deny-subagent-spawn"
	case verdict.warn:
		action = "warn-subagent-spawn"
	}
	facts := usageFacts(usage)
	if verdict.level != "" && observed(sid, "PreToolUse", action, verdict.why, facts) {
		return spawnVerdict{}
	}
	journal(sid, "PreToolUse", action, verdict.why, facts)
	if verdict.user != "" && sid != selftestSession {
		mark := "spawn:" + sid + ":" + verdict.level
		if getMap(peekState(), "notified")[mark] != nil {
			verdict.user = ""
		} else {
			updateState(func(state object) { stateMap(state, "notified")[mark] = float64(now) })
		}
	}
	if verdict.deny {
		logInfo("subagent %s refused for %s: %s", verdict.kind, sid, verdict.why)
		if getMap(getMap(peekState(), "routes"), sid) != nil {
			updateState(func(state object) { delete(stateMap(state, "routes"), sid) })
		}
	}
	return verdict
}

func spawnDenial(reason, message string) object {
	output := object{"hookSpecificOutput": object{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason}}
	if message != "" {
		output["systemMessage"] = message
	}
	return output
}

func withContext(specific object, context string) {
	if context == "" {
		return
	}
	if earlier := getString(specific, "additionalContext"); earlier != "" {
		context = earlier + "\n" + context
	}
	specific["additionalContext"] = context
}
