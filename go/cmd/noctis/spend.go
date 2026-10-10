package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	spendKey           = "spend"
	spendWeekSeconds   = 7 * 86400
	spendTTLSeconds    = 8 * 86400
	growthRecordMaxAge = 24 * time.Hour
)

func spendLedger(doc object, now int64) object {
	spend := stateMap(doc, spendKey)
	reset := numberOr(getMap(doc, "seven_day"), "resetsAt", 0)
	if reset <= float64(now) {
		reset = 0
	}
	week := numberOr(spend, "week", 0)
	switch {
	case week <= float64(now) || reset > week+sameWindowSeconds:
		spend["total"] = 0.0
		for _, raw := range getMap(spend, "sessions") {
			delete(toObject(raw), "sub")
		}
		spend["week"] = float64(now + spendWeekSeconds)
		if reset > 0 {
			spend["week"] = reset
		}
	case reset > 0 && reset < week-sameWindowSeconds:
		spend["week"] = reset
	}
	sessions := getMap(spend, "sessions")
	for sid, raw := range sessions {
		if float64(now)-numberOr(toObject(raw), "at", 0) > spendTTLSeconds {
			delete(sessions, sid)
		}
	}
	return spend
}

func spendSession(spend object, sid string, now int64) object {
	session := stateMap(stateMap(spend, "sessions"), sid)
	session["at"] = float64(now)
	return session
}

func addSpend(record object, key string, amount float64) {
	record[key] = roundTo(numberOr(record, key, 0)+amount, 6)
}

func noteSessionCost(doc object, sid string, cost float64, now int64) {
	spend := spendLedger(doc, now)
	session := spendSession(spend, sid, now)
	if last, seen := getNumber(session, "seen"); seen {
		delta := cost - last
		if delta < 0 {
			delta = cost
		}
		addSpend(spend, "total", delta)
	}
	session["seen"] = cost
}

func updateSpend(change func(spend object)) {
	withFileLock(files.usageLock, func() {
		current := readJSONStrict(files.usage)
		if !current.ok || !current.exists || current.data == nil {
			return
		}
		change(spendLedger(current.data, nowSec()))
		if err := writeJSONAtomic(files.usage, current.data); err != nil {
			warn("usage.json: subagent spend not recorded (%v)", err)
		}
	})
}

func forgetSpend(sid string) {
	withFileLock(files.usageLock, func() {
		current := readJSONStrict(files.usage)
		sessions := getMap(getMap(current.data, spendKey), "sessions")
		if !current.ok || sessions[sid] == nil {
			return
		}
		delete(sessions, sid)
		if err := writeJSONAtomic(files.usage, current.data); err != nil {
			warn("usage.json: %s left in the spend ledger (%v)", sid, err)
		}
	})
}

func subagentStatusText(doc object, sid string) string {
	spend := getMap(doc, spendKey)
	session := getMap(getMap(spend, "sessions"), sid)
	opened := numberOr(session, "opened", 0)
	if sid == "" || opened < 1 {
		return ""
	}
	total, sub := numberOr(spend, "total", 0), numberOr(session, "sub", 0)
	if total <= 0 || sub <= 0 {
		return T("statusline.subagents", formatNumber(opened))
	}
	return T("statusline.subagentsShare", formatNumber(opened), T("badge.percent", int(math.Round(math.Min(sub/total, 1)*100))))
}

func subagentBudgetNote(limits growthLimits) string {
	return "[noctis] Budget for this subagent: about " + limits.text() + "." + limits.reportText() + " Every call re-reads the whole context, so work from the text of your brief instead of exploring: read only the files and pages the task names or plainly needs, and return as soon as you have the answer. Past the budget you are told to wrap up, and stopped a few calls later."
}

func onSubagentStart(input, cfg object) {
	now := nowSec()
	sid := sessionKey(input)
	updateSpend(func(spend object) { addSpend(spendSession(spend, sid, now), "opened", 1) })
	if observing || guardPaused(cfg, readState(), now) {
		return
	}
	if limits, active := agentGrowthLimits(input, cfg); active {
		emit(object{"hookSpecificOutput": object{"hookEventName": "SubagentStart", "additionalContext": subagentBudgetNote(limits)}})
	}
}

type agentSpend struct {
	calls, tools, compactions int
	start, peak, cost         float64
	parts                     costParts
}

func measureAgent(cfg object, path string) agentSpend {
	spend := agentSpend{}
	if path == "" {
		return spend
	}
	scan := newTranscriptScan(time.Time{})
	scan.read(filepath.Dir(path), path)
	ledger := costLedger{}
	for _, call := range scan.calls {
		if call.context()+call.output == 0 {
			continue
		}
		if spend.calls == 0 {
			spend.start = call.context()
		}
		spend.calls++
		ledger.add(call.model, call.costUsage)
		spend.peak = math.Max(spend.peak, call.context())
	}
	for _, count := range scan.tools {
		spend.tools += count
	}
	for _, count := range scan.compactions {
		spend.compactions += count
	}
	prices := map[string]reportPrice{}
	for model := range ledger {
		if price, ok := priceFor(cfg, model); ok {
			prices[model] = price
		}
	}
	spend.parts = ledger.parts(prices)
	spend.cost = spend.parts.total()
	return spend
}

func pruneGrowthRecords(now time.Time) {
	dir := filepath.Join(files.guardDir, "subagents")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() && now.Sub(info.ModTime()) > growthRecordMaxAge {
			os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

func onSubagentStop(input, cfg object) {
	now := nowSec()
	pruneGrowthRecords(time.Unix(now, 0))
	measured := measureAgent(cfg, agentTranscript(input))
	if measured.calls == 0 {
		return
	}
	sid := sessionKey(input)
	added := measured.cost
	if path := growthRecordPath(input); path != "" {
		record := readJSON(path)
		if record == nil {
			record = object{"at": float64(now), "kind": getString(input, "agent_type")}
		}
		added = math.Max(measured.cost-numberOr(record, "reported", 0), 0)
		record["reported"] = measured.cost
		if err := writeEncodedAtomic(path, marshalCompact(record)); err != nil {
			warn("subagent record %s: %v", filepath.Base(path), err)
		}
	}
	if added > 0 {
		updateSpend(func(spend object) { addSpend(spendSession(spend, sid, now), "sub", added) })
	}
	who := strings.TrimSpace(getString(input, "agent_type") + " " + getString(input, "agent_id"))
	share := func(part float64) string {
		if measured.cost <= 0 {
			return "0%"
		}
		return percentShare(part / measured.cost)
	}
	reason := fmt.Sprintf("%s: %d tool calls, %d API calls, context %s → %s tokens, %d compactions, ~%s at API prices (cache read %s, output %s)",
		who, measured.tools, measured.calls, formatTokens(measured.start), formatTokens(measured.peak), measured.compactions, formatUSD(measured.cost), share(measured.parts.cacheRead), share(measured.parts.output))
	journal(sid, "SubagentStop", "subagent-report", reason, object{"tools": float64(measured.tools), "calls": float64(measured.calls), "peak": measured.peak, "cost": roundTo(measured.cost, 4)})
}
