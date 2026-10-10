package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func growthSandbox(t *testing.T, limits object) (object, string) {
	t.Helper()
	overrides := object{}
	if limits != nil {
		overrides["subagents"] = limits
	}
	cfg, project, _ := limitSandbox(t, overrides, 10, 30)
	return cfg, project
}

func agentCallLine(id string, input, written, read, output float64, content ...any) string {
	line, _ := json.Marshal(object{"type": "assistant", "timestamp": "2026-10-09T10:00:00Z", "message": object{
		"id": id, "model": "claude-sonnet-4-5", "role": "assistant", "content": content,
		"usage": object{"input_tokens": input, "cache_creation_input_tokens": written, "cache_read_input_tokens": read, "output_tokens": output},
	}})
	return string(line)
}

func toolUse(id, name string) object {
	return object{"type": "tool_use", "id": id, "name": name, "input": object{}}
}

func appendTranscript(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	transcript, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer transcript.Close()
	if _, err := transcript.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		t.Fatal(err)
	}
}

func subagentFixture() []string {
	return []string{
		`{"type":"user","message":{"role":"user","content":"look into the parser"}}`,
		agentCallLine("msg_1", 10, 53000, 0, 500, toolUse("toolu_1", "Grep")),
		agentCallLine("msg_2", 5, 2000, 53000, 300, object{"type": "thinking", "thinking": "the comma case"}),
		agentCallLine("msg_2", 5, 2000, 53000, 300, toolUse("toolu_2", "Read")),
		agentCallLine("msg_3", 5, 10000, 110000, 800, object{"type": "text", "text": "The parser drops trailing commas."}),
	}
}

var readCall = object{"tool_name": "Read", "tool_input": object{}, "tool_use_id": "toolu_main"}

func spendOf(sid string) object {
	return getMap(getMap(getMap(readJSON(files.usage), spendKey), "sessions"), sid)
}

func TestASubagentPastItsToolCallBudgetIsToldToWrapUpThenStopped(t *testing.T) {
	cfg, project := growthSandbox(t, object{"maxToolCalls": float64(5), "maxContextTokens": float64(0), "graceToolCalls": float64(3)})
	sid := "gr-calls"
	batch := func(agent string) object {
		return hookOutput(t, onPostToolBatch, subagentBatch(sid, project, agent, "Read", "Grep"), cfg)
	}

	for i := 1; i <= 2; i++ {
		if output := batch("a1"); output != nil {
			t.Fatalf("batch %d, inside the budget, got %v", i, output)
		}
	}
	warned := batch("a1")
	if stoppedBy(warned) || !strings.Contains(contextOf(warned), "has made 6 tool calls") || !strings.Contains(contextOf(warned), "After 3 more tool calls this agent is stopped") {
		t.Fatalf("6 tool calls past maxToolCalls 5 were not met with the wrap-up note: %v", warned)
	}
	if output := batch("a1"); output != nil {
		t.Fatalf("2 calls after the note, inside graceToolCalls 3, got %v", output)
	}
	stopped := batch("a1")
	if !stoppedBy(stopped) || !strings.Contains(getString(stopped, "stopReason"), "general-purpose a1 after 10 tool calls") {
		t.Fatalf("4 calls after the note were not stopped: %v", stopped)
	}
	if warns, stops := journalWith("warn-subagent-growth"), journalWith("stop-subagent-growth"); len(warns) != 1 || len(stops) != 1 || !strings.Contains(stops[0], "4 after the wrap-up note") {
		t.Fatalf("the journal holds warn %v and stop %v", warns, stops)
	}
	if output := batch("a2"); output != nil {
		t.Fatalf("another agent of the session paid for the first one's growth: %v", output)
	}

	told := contextOf(hookOutput(t, onPostToolBatch, mainBatch(sid, project, readCall), cfg))
	if !strings.Contains(told, "noctis stopped subagents of this session") || !strings.Contains(told, "general-purpose a1 after 10 tool calls") || !strings.Contains(told, "Do not open another subagent for the same task") {
		t.Fatalf("the main thread was not told which subagent was stopped: %q", told)
	}
	if again := contextOf(hookOutput(t, onPostToolBatch, mainBatch(sid, project, readCall), cfg)); strings.Contains(again, "noctis stopped subagents") {
		t.Fatalf("the main thread was told twice: %q", again)
	}
	if !stoppedBy(batch("a1")) {
		t.Fatal("a stopped subagent that was resumed went on past its budget")
	}
	if told := contextOf(hookOutput(t, onPostToolBatch, mainBatch(sid, project, readCall), cfg)); !strings.Contains(told, "general-purpose a1 after 12 tool calls") {
		t.Fatalf("the second stop of the resumed agent was not reported: %q", told)
	}
}

func TestASubagentWhoseContextPassesItsBudgetIsToldToWrapUp(t *testing.T) {
	cfg, project := growthSandbox(t, object{"maxToolCalls": float64(0), "maxContextTokens": float64(100000), "graceToolCalls": float64(4)})
	sid := "gr-context"
	transcript := filepath.Join(project, sid, "subagents", "agent-a1.jsonl")
	appendTranscript(t, transcript, subagentFixture()[:4]...)

	if output := hookOutput(t, onPostToolBatch, subagentBatch(sid, project, "a1", "Read"), cfg); output != nil {
		t.Fatalf("a context of 55005 tokens under maxContextTokens 100000 got %v", output)
	}
	result, _ := json.Marshal(object{"type": "user", "message": object{"role": "user", "content": []any{object{"type": "tool_result", "tool_use_id": "toolu_3", "content": strings.Repeat("x", 300*1024)}}}})
	appendTranscript(t, transcript, subagentFixture()[4], string(result))
	warned := hookOutput(t, onPostToolBatch, subagentBatch(sid, project, "a1", "Read"), cfg)
	if stoppedBy(warned) || !strings.Contains(contextOf(warned), "its context is 120005 tokens") {
		t.Fatalf("a context of 120005 tokens behind a 300 KB tool result was not met with the wrap-up note: %v", warned)
	}

	compacted := filepath.Join(project, sid, "subagents", "agent-a2.jsonl")
	appendTranscript(t, compacted, agentCallLine("msg_9", 5, 1000, 149000, 200), `{"type":"system","subtype":"compact_boundary"}`, agentCallLine("msg_10", 5, 30000, 0, 200))
	if output := hookOutput(t, onPostToolBatch, subagentBatch(sid, project, "a2", "Read"), cfg); output != nil {
		t.Fatalf("an agent whose context compacted down to 30005 tokens was judged by its old size: %v", output)
	}
}

func TestTheGrowthLimitOnlyJournalsInObserveMode(t *testing.T) {
	cfg, project := growthSandbox(t, object{"maxToolCalls": float64(2), "maxContextTokens": float64(0), "graceToolCalls": float64(2)})
	defer func(previous bool) { observing = previous }(observing)
	observing = true

	for i := 1; i <= 6; i++ {
		if output := hookOutput(t, onPostToolBatch, subagentBatch("gr-observe", project, "a1", "Read", "Grep"), cfg); output != nil {
			t.Fatalf("observe mode answered batch %d: %v", i, output)
		}
	}
	if warns, stops := journalWith("would-warn-subagent-growth"), journalWith("would-stop-subagent-growth"); len(warns) != 1 || len(stops) != 1 {
		t.Fatalf("observe mode journaled warn %v and stop %v; want one of each", warns, stops)
	}
	if acted := append(journalWith("warn-subagent-growth"), journalWith("stop-subagent-growth")...); len(acted) != 0 {
		t.Fatalf("observe mode acted: %v", acted)
	}
	if stopped := getMap(getMap(readState(), spawnStateKey), "stopped"); len(stopped) != 0 {
		t.Fatalf("observe mode left a stop note for the main thread: %v", stopped)
	}
	if output := hookOutput(t, onSubagentStart, agentHookInput("SubagentStart", "gr-observe", project, object{"agent_id": "a2", "agent_type": "general-purpose"}), cfg); output != nil {
		t.Fatalf("observe mode handed a subagent a budget it does not hold: %v", output)
	}
	if opened := numberOr(spendOf("gr-observe"), "opened", 0); opened != 1 {
		t.Fatalf("observe mode counts %v subagents opened; want 1", opened)
	}
}

func TestTheGrowthLimitCanBeTurnedOff(t *testing.T) {
	for name, settings := range map[string]object{
		"guard off":  {"guard": false, "maxToolCalls": float64(1), "maxContextTokens": float64(1)},
		"no limits":  {"maxToolCalls": float64(0), "maxContextTokens": float64(0)},
		"noctis off": {"maxToolCalls": float64(1), "maxContextTokens": float64(1)},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, project := growthSandbox(t, settings)
			if name == "noctis off" {
				updateState(func(state object) { state["disabledUntil"] = float64(nowSec() + 3600) })
			}
			for i := 1; i <= 4; i++ {
				if output := hookOutput(t, onPostToolBatch, subagentBatch("gr-off", project, "a1", "Read", "Grep"), cfg); output != nil {
					t.Fatalf("batch %d got %v", i, output)
				}
			}
			if output := hookOutput(t, onSubagentStart, agentHookInput("SubagentStart", "gr-off", project, object{"agent_id": "a1", "agent_type": "general-purpose"}), cfg); output != nil {
				t.Fatalf("a subagent was handed a budget that is off: %v", output)
			}
		})
	}
}

func TestASubagentStartsWithItsBudgetAndIsCounted(t *testing.T) {
	cfg, project := growthSandbox(t, nil)
	start := func(kind, agent string) object {
		return hookOutput(t, onSubagentStart, agentHookInput("SubagentStart", "gr-start", project, object{"agent_id": agent, "agent_type": kind}), cfg)
	}

	briefed := start("general-purpose", "a1")
	if event := getString(getMap(briefed, "hookSpecificOutput"), "hookEventName"); event != "SubagentStart" || !strings.Contains(contextOf(briefed), "about 60 tool calls and 120000 tokens of context") || !strings.Contains(contextOf(briefed), "work from the text of your brief instead of exploring") || strings.Contains(contextOf(briefed), "final report under") {
		t.Fatalf("the subagent was not handed its budget: %v", briefed)
	}
	if output := start("noctis:digest", "a2"); output != nil {
		t.Fatalf("the exempt digest agent was handed a budget: %v", output)
	}
	if opened := numberOr(spendOf("gr-start"), "opened", 0); opened != 2 {
		t.Fatalf("usage.json counts %v subagents opened; want 2", opened)
	}
}

func TestNoctisAgentsStartWithTheirOwnBudgetAndReportSize(t *testing.T) {
	cfg, project := growthSandbox(t, nil)
	for _, tc := range []struct{ kind, budget, report string }{
		{"noctis:lite", "about 20 tool calls and 120000 tokens of context.", " Keep your final report under 450 words."},
		{"lite", "about 20 tool calls and 120000 tokens of context.", " Keep your final report under 450 words."},
		{"noctis:worker", "about 60 tool calls and 120000 tokens of context.", " Keep your final report under 300 words."},
	} {
		note := contextOf(hookOutput(t, onSubagentStart, agentHookInput("SubagentStart", "gr-budgets", project, object{"agent_id": "a-" + tc.kind, "agent_type": tc.kind}), cfg))
		if !strings.Contains(note, "Budget for this subagent: "+tc.budget+tc.report) {
			t.Errorf("%s was not handed its own budget and report size: %q", tc.kind, note)
		}
	}
}

func liteBatch(sid, project, agent string) object {
	input := subagentBatch(sid, project, agent, "WebSearch", "WebFetch")
	input["agent_type"] = "noctis:lite"
	return input
}

func TestTheLiteAgentIsToldToWrapUpAtItsSmallerBudget(t *testing.T) {
	cfg, project := growthSandbox(t, nil)
	sid := "gr-lite"
	for i := 1; i < 10; i++ {
		if output := hookOutput(t, onPostToolBatch, liteBatch(sid, project, "l1"), cfg); output != nil {
			t.Fatalf("batch %d, %d tool calls inside lite's budget of 20, got %v", i, 2*i, output)
		}
	}
	warned := hookOutput(t, onPostToolBatch, liteBatch(sid, project, "l1"), cfg)
	if note := contextOf(warned); !strings.Contains(note, "has made 20 tool calls") || !strings.Contains(note, "past its budget of 20 tool calls") || !strings.Contains(note, "Keep your final report under 450 words.") {
		t.Fatalf("lite at 20 tool calls was not told to wrap up within its report size: %v", warned)
	}
	if warns := journalWith("warn-subagent-growth"); len(warns) != 1 || !strings.Contains(warns[0], "noctis:lite l1: 20 tool calls, context 0 tokens, budget 20 tool calls and 120000 tokens of context") {
		t.Fatalf("the wrap-up note left no journal row with lite's budget: %v", warns)
	}
}

func TestAUserCanLoosenAnAgentsBudget(t *testing.T) {
	cfg, project := growthSandbox(t, object{"budgets": object{"noctis:lite": object{"maxToolCalls": float64(40)}}})
	sid := "gr-loose"
	note := contextOf(hookOutput(t, onSubagentStart, agentHookInput("SubagentStart", sid, project, object{"agent_id": "l1", "agent_type": "noctis:lite"}), cfg))
	if !strings.Contains(note, "about 40 tool calls and 120000 tokens of context. Keep your final report under 450 words.") {
		t.Fatalf("lite's loosened budget, with the shipped report size kept, was not handed over: %q", note)
	}
	for i := 1; i < 20; i++ {
		if output := hookOutput(t, onPostToolBatch, liteBatch(sid, project, "l1"), cfg); output != nil {
			t.Fatalf("batch %d, %d tool calls inside the loosened budget of 40, got %v", i, 2*i, output)
		}
	}
	if warned := hookOutput(t, onPostToolBatch, liteBatch(sid, project, "l1"), cfg); !strings.Contains(contextOf(warned), "has made 40 tool calls") {
		t.Fatalf("lite at its loosened budget of 40 was not told to wrap up: %v", warned)
	}
}

func TestABudgetNamedWithoutThePluginPrefixWinsEveryTime(t *testing.T) {
	cfg := mergeDefaults(shippedDefaults(t), object{"subagents": object{"budgets": object{"lite": object{"maxToolCalls": float64(40)}}}})
	for round := 0; round < 200; round++ {
		for _, kind := range []string{"noctis:lite", "lite"} {
			if limits := subagentGrowthLimits(cfg, kind); limits.calls != 40 || limits.words != 450 {
				t.Fatalf("round %d, %s: a budget named \"lite\" gave %v tool calls and %v report words, want 40 and 450", round, kind, limits.calls, limits.words)
			}
		}
	}
}

func TestASubagentsSpendIsMeasuredWhenItStops(t *testing.T) {
	cfg, project := growthSandbox(t, nil)
	sid := "gr-stop"
	transcript := filepath.Join(project, sid, "subagents", "agent-a1.jsonl")
	appendTranscript(t, transcript, subagentFixture()...)
	stale := filepath.Join(files.guardDir, "subagents", "old-a9.json")
	mustWriteJSON(stale, object{"calls": float64(3)})
	old := time.Now().Add(-growthRecordMaxAge - time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	stop := func() {
		hookOutput(t, onSubagentStop, agentHookInput("SubagentStop", sid, project, object{"agent_id": "a1", "agent_type": "general-purpose", "agent_transcript_path": transcript, "stop_hook_active": false}), cfg)
	}

	stop()
	reports := journalWith("subagent-report")
	if len(reports) != 1 || !strings.Contains(reports[0], "general-purpose a1: 2 tool calls, 3 API calls, context 53.0K → 120.0K tokens, 0 compactions, ~$0.317 at API prices (cache read 15%, output 8%)") {
		t.Fatalf("the subagent's report reads %v", reports)
	}
	if sub := numberOr(spendOf(sid), "sub", 0); sub != 0.31671 {
		t.Fatalf("the session's subagent spend is %v; want 0.31671", sub)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("a growth record older than %v was kept: %v", growthRecordMaxAge, err)
	}

	appendTranscript(t, transcript, agentCallLine("msg_4", 5, 1000, 120000, 200))
	stop()
	if sub := numberOr(spendOf(sid), "sub", 0); sub != 0.359475 {
		t.Fatalf("a resumed agent's second stop left the spend at %v; want 0.359475 (only the new call added)", sub)
	}
	stop()
	if sub := numberOr(spendOf(sid), "sub", 0); sub != 0.359475 {
		t.Fatalf("a stop with nothing new changed the spend to %v", sub)
	}
}

func TestTheSpendLedgerFollowsTheWeeklyWindow(t *testing.T) {
	now := int64(1_800_000_000)
	reset := float64(now + 3*86400)
	doc := object{"seven_day": object{"used": 30.0, "resetsAt": reset}}
	total := func() float64 { return numberOr(getMap(doc, spendKey), "total", 0) }

	noteSessionCost(doc, "s1", 1.5, now)
	if total() != 0 {
		t.Fatalf("a session's first reading counted %v; it is only the baseline", total())
	}
	noteSessionCost(doc, "s1", 2.0, now+60)
	noteSessionCost(doc, "s2", 4.0, now+60)
	noteSessionCost(doc, "s2", 4.25, now+120)
	noteSessionCost(doc, "s1", 0.5, now+180)
	if total() != 1.25 {
		t.Fatalf("the week's spend is %v; want 0.5 + 0.25 + 0.5 after s1 restarted", total())
	}
	spend := getMap(doc, spendKey)
	if week := numberOr(spend, "week", 0); week != reset {
		t.Fatalf("the ledger's week ends at %v; want the window's reset %v", week, reset)
	}

	spendSession(spend, "s1", now+180)["sub"] = 0.8
	doc["seven_day"] = object{"used": 2.0, "resetsAt": reset + spendWeekSeconds}
	spend = spendLedger(doc, int64(reset)+60)
	if numberOr(spend, "total", -1) != 0 || getMap(getMap(spend, "sessions"), "s1")["sub"] != nil || numberOr(spend, "week", 0) != reset+spendWeekSeconds {
		t.Fatalf("the ledger did not start over at the reset: %v", spend)
	}

	fresh := object{}
	noteSessionCost(fresh, "s3", 1, now)
	noteSessionCost(fresh, "s3", 3, now+60)
	if week := numberOr(getMap(fresh, spendKey), "week", 0); week != float64(now+spendWeekSeconds) {
		t.Fatalf("without a weekly reading the ledger's week ends at %v", week)
	}
	fresh["seven_day"] = object{"used": 40.0, "resetsAt": reset}
	if spend := spendLedger(fresh, now+120); numberOr(spend, "total", 0) != 2 || numberOr(spend, "week", 0) != reset {
		t.Fatalf("an earlier reset learned later did not keep the week's spend: %v", spend)
	}
	if sessions := getMap(spendLedger(fresh, now+spendTTLSeconds+61), "sessions"); sessions["s3"] != nil {
		t.Fatalf("a session silent for over %d s was kept: %v", spendTTLSeconds, sessions)
	}
}

func TestTheSubagentStatusTextShowsCountAndShare(t *testing.T) {
	doc := object{spendKey: object{"total": 2.0, "sessions": object{
		"counted": object{"opened": 2.0},
		"shared":  object{"opened": 3.0, "sub": 0.5},
		"over":    object{"opened": 1.0, "sub": 5.0},
	}}}
	for sid, want := range map[string]string{
		"":        "",
		"none":    "",
		"counted": " · 🤖 2",
		"shared":  " · 🤖 3, 25% of the week's spend",
		"over":    " · 🤖 1, 100% of the week's spend",
	} {
		if got := subagentStatusText(doc, sid); got != want {
			t.Fatalf("session %q shows %q; want %q", sid, got, want)
		}
	}
}

func TestTheStatusLineShowsTheSessionsSubagentsAndTheirShareOfTheWeek(t *testing.T) {
	box := newLeanBox(t)
	sid := "sub-share"
	now := nowSec()
	statusline := func(cost float64) string {
		t.Helper()
		payload, _ := json.Marshal(object{"session_id": sid, "model": object{"id": "claude-sonnet-4-5", "display_name": "Sonnet 4.5"}, "cwd": forwardSlashes(box.home),
			"context_window": object{"used_percentage": 20.0}, "cost": object{"total_cost_usd": cost},
			"rate_limits": object{"five_hour": object{"used_percentage": 12.0, "resets_at": float64(now + 7200)}, "seven_day": object{"used_percentage": 30.0, "resets_at": float64(now + 3*86400)}}})
		run := box.run(t, string(payload), "statusline")
		if run.code != 0 {
			t.Fatalf("the status line failed:\n%s", run)
		}
		return run.stdout
	}
	hook := func(event string, fields object) {
		t.Helper()
		payload, _ := json.Marshal(agentHookInput(event, sid, box.home, fields))
		if run := box.run(t, string(payload), "hook"); run.code != 0 {
			t.Fatalf("the %s hook failed:\n%s", event, run)
		}
	}

	if line := statusline(1.0); strings.Contains(line, "🤖") {
		t.Fatalf("a session with no subagent shows them: %q", line)
	}
	transcript := filepath.Join(box.home, sid, "subagents", "agent-a1.jsonl")
	appendTranscript(t, transcript, subagentFixture()...)
	hook("SubagentStart", object{"agent_id": "a1", "agent_type": "general-purpose"})
	hook("SubagentStart", object{"agent_id": "a2", "agent_type": "Explore"})
	if line := statusline(1.2); !strings.Contains(line, " · 🤖 2") || strings.Contains(line, "of the week's spend") {
		t.Fatalf("two subagents opened, none measured yet: %q", line)
	}
	hook("SubagentStop", object{"agent_id": "a1", "agent_type": "general-purpose", "agent_transcript_path": transcript, "stop_hook_active": false})
	if line := statusline(1.63342); !strings.Contains(line, " · 🤖 2, 50% of the week's spend") {
		t.Fatalf("a subagent that cost $0.31671 of the $0.63342 spent this week shows %q", line)
	}
}
