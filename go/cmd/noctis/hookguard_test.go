package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func agentSessionSandbox(t *testing.T, weekly float64) (object, string) {
	t.Helper()
	dir := sandboxFiles(t)
	files.pluginRoot = dir
	files.credentials = filepath.Join(dir, ".credentials.json")
	mustWriteJSON(files.credentials, object{})
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	t.Setenv("NOCTIS_NO_QUIET", "")
	mustWriteJSON(filepath.Join(dir, "config.default.json"), shippedDefaults(t))
	now := float64(nowSec())
	mustWriteJSON(files.usage, object{
		"updatedAt": now,
		"five_hour": object{"used": float64(20), "resetsAt": now + 3600},
		"seven_day": object{"used": weekly, "resetsAt": now + 3*86400},
	})
	return loadConfig(), t.TempDir()
}

func agentHookInput(event, sid, cwd string, fields object) object {
	input := object{"hook_event_name": event, "session_id": sid, "cwd": cwd, "transcript_path": filepath.Join(cwd, "transcript.jsonl")}
	for key, value := range fields {
		input[key] = value
	}
	return input
}

func hookOutput(t *testing.T, handler func(object, object), input, cfg object) object {
	t.Helper()
	defer func(previous bool) { emitted = previous }(emitted)
	emitted = false
	printed := strings.TrimSpace(capturedStdout(t, func() { handler(input, cfg) }))
	if printed == "" {
		return nil
	}
	var output object
	if err := json.Unmarshal([]byte(printed), &output); err != nil {
		t.Fatalf("the hook printed something that is not JSON: %q", printed)
	}
	return output
}

func permissionOf(output object) string {
	return getString(getMap(output, "hookSpecificOutput"), "permissionDecision")
}

func pendingWait(sid string) object {
	return getMap(getMap(readState(), "waits"), sid)
}

func TestAnAgentSessionsMainThreadPausesAtTheBatch(t *testing.T) {
	cfg, project := agentSessionSandbox(t, 95)
	subagent := agentHookInput("PostToolBatch", "ag-sub", project, object{"agent_id": "a1", "agent_type": "security-reviewer"})
	if output := hookOutput(t, onPostToolBatch, subagent, cfg); stoppedBy(output) || !strings.Contains(contextOf(output), "start no new work") {
		t.Fatalf("a subagent's first batch past the weekly pause point was not told to wrap up on its own loop: %v", output)
	}
	if wait := pendingWait("ag-sub"); wait != nil {
		t.Fatalf("a subagent's batch registered a wait: %v", wait)
	}
	main := agentHookInput("PostToolBatch", "ag-main", project, object{"agent_type": "security-reviewer"})
	output := hookOutput(t, onPostToolBatch, main, cfg)
	if proceed, ok := output["continue"].(bool); !ok || proceed {
		t.Fatalf("the main thread of a claude --agent session ran past the weekly pause point: PostToolBatch printed %v", output)
	}
	if pendingWait("ag-main") == nil {
		t.Fatal("the main thread of a claude --agent session stopped without a wait to resume it")
	}
}

func TestAnAgentSessionsMainThreadMeetsTheSpawnAndWorkflowGates(t *testing.T) {
	cfg, project := agentSessionSandbox(t, 95)
	workflow := object{"agent_type": "security-reviewer", "tool_name": "Workflow", "tool_input": object{"name": "audit-everything"}}
	if output := hookOutput(t, onPreToolUse, agentHookInput("PreToolUse", "ag-workflow", project, workflow), cfg); permissionOf(output) != "deny" {
		t.Fatalf("the main thread of a claude --agent session launched a workflow at 95%% weekly: %v", output)
	}
	spawn := object{"agent_type": "security-reviewer", "tool_name": "Agent", "tool_input": object{"subagent_type": "Explore", "prompt": "map the parser"}}
	if output := hookOutput(t, onPreToolUse, agentHookInput("PreToolUse", "ag-spawn", project, spawn), cfg); permissionOf(output) != "deny" {
		t.Fatalf("the main thread of a claude --agent session spawned an agent at 95%% weekly: %v", output)
	}
	nested := object{"agent_id": "a1", "agent_type": "security-reviewer", "tool_name": "Workflow", "tool_input": object{"name": "audit-everything"}}
	if output := hookOutput(t, onPreToolUse, agentHookInput("PreToolUse", "ag-nested", project, nested), cfg); permissionOf(output) != "deny" || !strings.Contains(reasonOf(output), "start no new work") {
		t.Fatalf("a subagent launched a workflow past the weekly pause point: %v", output)
	}
	if pendingWait("ag-nested") != nil || len(workflowLaunches(readState(), "ag-nested")) > 0 {
		t.Fatalf("a subagent's tool call went through the main-thread gates: wait=%v workflows=%v", pendingWait("ag-nested"), workflowLaunches(readState(), "ag-nested"))
	}
}

func TestAnAgentSessionsMainThreadFollowsTheResearchRoute(t *testing.T) {
	cfg, project := agentSessionSandbox(t, 10)
	route := func(sid string) {
		updateState(func(state object) {
			stateMap(state, "routes")[sid] = object{"at": float64(nowSec()), "denies": float64(0), "signal": "web-words"}
		})
	}
	search := func(sid string, fields object) object {
		fields["tool_name"], fields["tool_input"] = "WebSearch", object{"query": "best vector database 2026"}
		return hookOutput(t, onPreToolUse, agentHookInput("PreToolUse", sid, project, fields), cfg)
	}
	route("ag-route")
	if output := search("ag-route", object{"agent_type": "security-reviewer"}); permissionOf(output) != "deny" {
		t.Fatalf("the main thread of a claude --agent session ran the research the router sent to %s: %v", liteAgentType(cfg), output)
	}
	if output := search("ag-route", object{"agent_id": "l1", "agent_type": liteAgentType(cfg)}); output != nil {
		t.Fatalf("the lite subagent was stopped from doing the research routed to it: %v", output)
	}
	route("ag-lite")
	if output := search("ag-lite", object{"agent_type": liteAgentType(cfg)}); output != nil {
		t.Fatalf("a session running as %s was told to hand its research to itself: %v", liteAgentType(cfg), output)
	}
}

func TestAnAgentSessionsMainThreadLearnsFromLiteResults(t *testing.T) {
	cfg, project := agentSessionSandbox(t, 10)
	updateState(func(state object) {
		stateMap(state, "routes")["ag-learn"] = object{"at": float64(nowSec()), "denies": float64(0), "signal": "web-words"}
	})
	finish := func(fields object) {
		fields["tool_name"] = "Agent"
		fields["tool_input"] = object{"subagent_type": liteAgentType(cfg), "prompt": "compare vector databases"}
		fields["tool_response"] = "NEEDS_CODE: this needs a change to the parser"
		hookOutput(t, onPostToolUse, agentHookInput("PostToolUse", "ag-learn", project, fields), cfg)
	}
	finish(object{"agent_id": "a1", "agent_type": "security-reviewer"})
	if learned := getMap(readState(), "routerLearned"); len(learned) != 0 {
		t.Fatalf("a subagent's Agent result taught the router: %v", learned)
	}
	finish(object{"agent_type": "security-reviewer"})
	if misroutes := numberOr(getMap(getMap(readState(), "routerLearned"), "web-words"), "misroutes", 0); misroutes != 1 {
		t.Fatalf("the main thread of a claude --agent session got NEEDS_CODE back from %s and the router learned nothing (misroutes=%v)", liteAgentType(cfg), misroutes)
	}
}

func TestTheRouterAgentsWritePolicyHoldsWhenTheyRunTheMainThread(t *testing.T) {
	cfg, project := agentSessionSandbox(t, 10)
	cases := []struct {
		name   string
		fields object
		file   string
		deny   bool
	}{
		{"claude --agent lite writing code", object{"agent_type": liteAgentType(cfg)}, "src/main.go", true},
		{"claude --agent lite writing notes", object{"agent_type": liteAgentType(cfg)}, "notes.md", false},
		{"claude --agent digest writing notes", object{"agent_type": digestAgentType(cfg)}, "notes.md", true},
		{"the lite subagent writing code", object{"agent_id": "l1", "agent_type": liteAgentType(cfg)}, "src/main.go", true},
		{"the digest subagent writing notes", object{"agent_id": "d1", "agent_type": digestAgentType(cfg)}, "notes.md", true},
		{"another agent's main thread writing code", object{"agent_type": "security-reviewer"}, "src/main.go", false},
		{"a plain main thread writing code", object{}, "src/main.go", false},
	}
	for _, c := range cases {
		c.fields["tool_name"], c.fields["tool_input"] = "Write", object{"file_path": filepath.Join(project, c.file), "content": "x"}
		output := hookOutput(t, onPreToolUse, agentHookInput("PreToolUse", "ag-write", project, c.fields), cfg)
		if denied := permissionOf(output) == "deny"; denied != c.deny {
			t.Errorf("%s: denied=%t, want %t (%v)", c.name, denied, c.deny, output)
		}
	}
}

func TestAnAgentSessionsMainThreadTakesTheQuietPath(t *testing.T) {
	cfg, project := agentSessionSandbox(t, 10)
	main := agentHookInput("PostToolBatch", "ag-quiet", project, object{"agent_type": "security-reviewer"})
	if output := hookOutput(t, onPostToolBatch, main, cfg); output != nil {
		t.Fatalf("a batch far from every limit printed %v", output)
	}
	if readJSON(quietMarkerPath("ag-quiet")) == nil {
		t.Fatal("the main thread of a claude --agent session left no quiet marker, so every batch runs the full decision")
	}
	if !quietFastPath(main) {
		t.Fatal("the main thread of a claude --agent session was refused the quiet path its own batch marked safe")
	}
	subagent := agentHookInput("PostToolBatch", "ag-quiet", project, object{"agent_id": "a1", "agent_type": "security-reviewer"})
	if quietFastPath(subagent) {
		t.Fatal("a subagent's batch took the main thread's quiet path")
	}
}

func TestOtherHostsStillTreatAnAgentTypeAsASubagent(t *testing.T) {
	cfg, project := agentSessionSandbox(t, 95)
	previous := activeHost
	t.Cleanup(func() { activeHost = previous })
	for _, host := range []string{"codex", "droid"} {
		activeHost = host
		sid := "ag-" + host
		output := hookOutput(t, onPostToolBatch, agentHookInput("PostToolBatch", sid, project, object{"agent_type": "worker"}), cfg)
		if output != nil || pendingWait(sid) != nil {
			t.Fatalf("%s: a hook carrying agent_type went through the main-thread guard: %v", host, output)
		}
	}
}

func contextOf(output object) string {
	return getString(getMap(output, "hookSpecificOutput"), "additionalContext")
}

func reasonOf(output object) string {
	return getString(getMap(output, "hookSpecificOutput"), "permissionDecisionReason")
}

func stoppedBy(output object) bool {
	proceed, ok := output["continue"].(bool)
	return ok && !proceed
}

func writeUsage(five, weekly, fiveReset float64) float64 {
	now := float64(nowSec())
	if fiveReset == 0 {
		fiveReset = now + 3600
	}
	mustWriteJSON(files.usage, object{
		"updatedAt": now,
		"five_hour": object{"used": five, "resetsAt": fiveReset},
		"seven_day": object{"used": weekly, "resetsAt": now + 3*86400},
	})
	return fiveReset
}

func limitSandbox(t *testing.T, overrides object, five, weekly float64) (object, string, float64) {
	t.Helper()
	dir := sandboxFiles(t)
	files.pluginRoot = dir
	files.credentials = filepath.Join(dir, ".credentials.json")
	mustWriteJSON(files.credentials, object{})
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	t.Setenv("NOCTIS_NO_QUIET", "")
	mustWriteJSON(filepath.Join(dir, "config.default.json"), shippedDefaults(t))
	user := object{"alarm": object{"enabled": false}, "update": object{"check": false}}
	for key, value := range overrides {
		user[key] = value
	}
	mustWriteJSON(files.config, user)
	fiveReset := writeUsage(five, weekly, 0)
	return loadConfig(), t.TempDir(), fiveReset
}

func subagentTool(sid, cwd, tool string, toolInput object) object {
	return agentHookInput("PreToolUse", sid, cwd, object{"agent_id": "a1", "agent_type": "general-purpose", "tool_name": tool, "tool_input": toolInput})
}

func subagentBatch(sid, cwd, agent string, tools ...string) object {
	calls := []any{}
	for i, tool := range tools {
		calls = append(calls, object{"tool_name": tool, "tool_input": object{}, "tool_use_id": "toolu_" + strconv.Itoa(i)})
	}
	return agentHookInput("PostToolBatch", sid, cwd, object{"agent_id": agent, "agent_type": "general-purpose", "tool_calls": calls})
}

func plantQuietMarker(t *testing.T, sid string) []byte {
	t.Helper()
	mustWriteJSON(quietMarkerPath(sid), object{"at": float64(nowSec()), "state": "planted"})
	content, err := os.ReadFile(quietMarkerPath(sid))
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func quietMarkerUntouched(t *testing.T, sid string, planted []byte) {
	t.Helper()
	if content, err := os.ReadFile(quietMarkerPath(sid)); err != nil || !bytes.Equal(content, planted) {
		t.Fatalf("a subagent hook rewrote or removed the main thread's quiet marker (%v)", err)
	}
}

func TestASubagentPastThePausePointIsDeniedNewWork(t *testing.T) {
	cfg, project, fiveReset := limitSandbox(t, nil, 93, 40)
	sid := "sg-deny"
	planted := plantQuietMarker(t, sid)
	write := subagentTool(sid, project, "Write", object{"file_path": filepath.Join(project, "src", "x.go"), "content": "x"})
	output := hookOutput(t, onPreToolUse, write, cfg)
	if permissionOf(output) != "deny" {
		t.Fatalf("a subagent wrote a file with the 5h window at 93%% (pause point 92%%): %v", output)
	}
	for _, want := range []string{windowLabel("five_hour") + " usage is 93%", "pause point 92%", "start no new work", "the way you normally deliver your report", "name what is unfinished", "after the reset at " + formatTime(fiveReset)} {
		if !strings.Contains(reasonOf(output), want) {
			t.Errorf("the deny reason lacks %q: %s", want, reasonOf(output))
		}
	}
	for _, tool := range []string{"WebFetch", "WebSearch", "Agent", "Task", "Workflow"} {
		if output := hookOutput(t, onPreToolUse, subagentTool(sid, project, tool, object{"prompt": "more work"}), cfg); permissionOf(output) != "deny" {
			t.Errorf("a subagent's %s went through at 93%%: %v", tool, output)
		}
	}
	lite := agentHookInput("PreToolUse", sid, project, object{"agent_id": "l1", "agent_type": liteAgentType(cfg), "tool_name": "Write", "tool_input": object{"file_path": filepath.Join(project, "notes.md")}})
	if output := hookOutput(t, onPreToolUse, lite, cfg); permissionOf(output) != "deny" || !strings.Contains(reasonOf(output), "usage is 93%") {
		t.Errorf("the lite agent wrote its notes at 93%%: %v", output)
	}
	if wait := pendingWait(sid); wait != nil {
		t.Fatalf("a subagent's tool call registered a wait for the whole session: %v", wait)
	}
	if records := agentCutOffs(readState(), sid); len(records) != 0 {
		t.Fatalf("a denied tool call already counted as the agent's warning, so its next batch would stop it before it can report: %v", records)
	}
	quietMarkerUntouched(t, sid, planted)
	writeUsage(50, 40, 0)
	if output := hookOutput(t, onPreToolUse, write, cfg); output != nil {
		t.Fatalf("a subagent was denied a write at 50%%: %v", output)
	}
	if output := hookOutput(t, onPreToolUse, lite, cfg); output != nil {
		t.Fatalf("the lite agent was denied its notes at 50%%: %v", output)
	}
	liteCode := agentHookInput("PreToolUse", sid, project, object{"agent_id": "l1", "agent_type": liteAgentType(cfg), "tool_name": "Write", "tool_input": object{"file_path": filepath.Join(project, "src", "x.go")}})
	if output := hookOutput(t, onPreToolUse, liteCode, cfg); permissionOf(output) != "deny" || strings.Contains(reasonOf(output), "usage is") {
		t.Fatalf("below the limits the lite write policy no longer ran: %v", output)
	}
}

func TestAtTheCreditCeilingASubagentStopsAtOnce(t *testing.T) {
	cases := []struct {
		name      string
		overrides object
	}{
		{"with the thresholds switched off", object{"thresholds": object{"session5h": nil, "weeklyAll": nil, "weeklyFable": nil}}},
		{"with the shipped thresholds, whose pause shares the ceiling's reset", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, project, _ := limitSandbox(t, c.overrides, 100, 40)
			sid := "sg-ceiling"
			output := hookOutput(t, onPreToolUse, subagentTool(sid, project, "Write", object{"file_path": filepath.Join(project, "x.go")}), cfg)
			if permissionOf(output) != "deny" {
				t.Fatalf("a subagent wrote at 100%% with paid credits refused: %v", output)
			}
			reason := reasonOf(output)
			if !strings.Contains(reason, "paid-credit ceiling") || !strings.Contains(reason, "stops now") {
				t.Fatalf("the ceiling deny does not say the agent stops: %s", reason)
			}
			for _, reply := range []string{"report", "Return what you have", "unfinished"} {
				if strings.Contains(reason, reply) {
					t.Fatalf("the ceiling deny asks for a reply (%q) that the immediate stop never lets through: %s", reply, reason)
				}
			}
			batch := hookOutput(t, onPostToolBatch, subagentBatch(sid, project, "a1", "Bash"), cfg)
			if !stoppedBy(batch) {
				t.Fatalf("a subagent's first batch at the ceiling went on: %v", batch)
			}
			if stopReason := getString(batch, "stopReason"); !strings.HasPrefix(stopReason, "⏸ ") || !strings.Contains(stopReason, T("hit.ceiling")) {
				t.Fatalf("the ceiling stop does not say why: %q", stopReason)
			}
			if wait := pendingWait(sid); wait != nil {
				t.Fatalf("a subagent's stop registered a wait for the whole session: %v", wait)
			}
			if records := agentCutOffs(readState(), sid); len(records) != 1 || !getBool(records[0], "stopped", false) {
				t.Fatalf("the stopped agent was not recorded as stopped: %v", records)
			}
		})
	}
}

func TestASubagentIsWarnedOnceThenStoppedWhateverTheWindowSays(t *testing.T) {
	cases := []struct {
		name    string
		between func(fiveReset float64)
	}{
		{"the OAuth copy reports the reset a second later than the status line", func(fiveReset float64) {
			now := float64(nowSec())
			mustWriteJSON(files.usage, object{"updatedAt": now - 30, "five_hour": object{"used": float64(93), "resetsAt": fiveReset}, "seven_day": object{"used": float64(40), "resetsAt": now + 3*86400}})
			mustWriteJSON(files.fable, object{"fetchedAt": now, "five_hour": object{"used": float64(93), "resetsAt": fiveReset + 1}, "seven_day": object{"used": float64(40), "resetsAt": now + 3*86400}})
		}},
		{"the weekly window crosses its own pause point after the warning", func(fiveReset float64) {
			writeUsage(93, 90, fiveReset)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, project, fiveReset := limitSandbox(t, nil, 93, 40)
			sid := "sg-warn"
			planted := plantQuietMarker(t, sid)
			first := hookOutput(t, onPostToolBatch, subagentBatch(sid, project, "a1", "Bash"), cfg)
			if stoppedBy(first) {
				t.Fatalf("the first batch past the pause point stopped the agent before it could report: %v", first)
			}
			for _, want := range []string{"usage is 93%", "start no new work", "the way you normally deliver your report", "Any tool call other than delivering that report ends this agent"} {
				if !strings.Contains(contextOf(first), want) {
					t.Fatalf("the warning lacks %q: %v", want, first)
				}
			}
			c.between(fiveReset)
			second := hookOutput(t, onPostToolBatch, subagentBatch(sid, project, "a1", "Edit"), cfg)
			if !stoppedBy(second) {
				t.Fatalf("a warned agent kept working past the pause point: %v", second)
			}
			if !strings.HasPrefix(getString(second, "stopReason"), "⏸ ") {
				t.Fatalf("the stop reason is not the pause reason: %v", second)
			}
			other := hookOutput(t, onPostToolBatch, subagentBatch(sid, project, "a2", "Bash"), cfg)
			if stoppedBy(other) || contextOf(other) == "" {
				t.Fatalf("another agent was stopped on its first batch by a1's warning: %v", other)
			}
			if wait := pendingWait(sid); wait != nil {
				t.Fatalf("a subagent registered a wait for the whole session: %v", wait)
			}
			quietMarkerUntouched(t, sid, planted)
		})
	}
}

func TestASubagentDeliveringItsReportIsLeftAlone(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 93, 40)
	sid := "sg-report"
	for _, tools := range [][]string{{"SubagentHandback"}, {"StructuredOutput"}, {"SubagentHandback", "StructuredOutput"}} {
		if output := hookOutput(t, onPostToolBatch, subagentBatch(sid, project, "a1", tools...), cfg); output != nil {
			t.Fatalf("a batch of only %v was treated as new work: %v", tools, output)
		}
		if records := agentCutOffs(readState(), sid); len(records) != 0 {
			t.Fatalf("a batch of only %v was recorded as a warning: %v", tools, records)
		}
	}
	if output := hookOutput(t, onPostToolBatch, subagentBatch(sid, project, "a1", "SubagentHandback", "Bash"), cfg); contextOf(output) == "" {
		t.Fatalf("a report sent alongside more work escaped the warning: %v", output)
	}
	if output := hookOutput(t, onPostToolBatch, subagentBatch(sid, project, "a1", "SubagentHandback"), cfg); output != nil {
		t.Fatalf("a warned agent was stopped while it handed its report back: %v", output)
	}
	if output := hookOutput(t, onPostToolBatch, subagentBatch(sid, project, "a2"), cfg); contextOf(output) == "" {
		t.Fatalf("a batch that lists no tool calls counted as a report: %v", output)
	}
}

func TestOtherHostsLeaveASubagentsBatchAlone(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 93, 40)
	previous := activeHost
	t.Cleanup(func() { activeHost = previous })
	for _, host := range []string{"codex", "droid"} {
		activeHost = host
		sid := "sg-" + host
		for _, fields := range []object{{"agent_id": "a1"}, {"agent_id": "a1", "agent_type": "worker"}} {
			if output := hookOutput(t, onPostToolBatch, agentHookInput("PostToolBatch", sid, project, fields), cfg); output != nil {
				t.Fatalf("%s: a subagent batch at 93%% printed %v", host, output)
			}
		}
		if output := hookOutput(t, onPreToolUse, subagentTool(sid, project, "Write", object{"file_path": filepath.Join(project, "x.go")}), cfg); output != nil {
			t.Fatalf("%s: a subagent tool call at 93%% printed %v", host, output)
		}
		if records := agentCutOffs(readState(), sid); len(records) != 0 || pendingWait(sid) != nil {
			t.Fatalf("%s: a subagent hook left state behind: records=%v wait=%v", host, records, pendingWait(sid))
		}
	}
}

func TestASubagentsWorkflowMeetsTheFanOutGate(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 88, 40)
	sid := "sg-fanout"
	workflow := subagentTool(sid, project, "Workflow", object{"name": "audit-everything"})
	if output := hookOutput(t, onPreToolUse, workflow, cfg); permissionOf(output) != "deny" || !strings.Contains(reasonOf(output), "too close to the limit") {
		t.Fatalf("a subagent fanned out a workflow inside the warn band: %v", output)
	}
	if output := hookOutput(t, onPreToolUse, subagentTool(sid, project, "Write", object{"file_path": filepath.Join(project, "x.go")}), cfg); output != nil {
		t.Fatalf("inside the warn band a subagent's single write was denied: %v", output)
	}
	writeUsage(70, 40, 0)
	if output := hookOutput(t, onPreToolUse, workflow, cfg); permissionOf(output) != "deny" || !strings.Contains(reasonOf(output), "only 22 points") {
		t.Fatalf("a subagent fanned out a workflow with 22 points of room: %v", output)
	}
	writeUsage(50, 40, 0)
	if output := hookOutput(t, onPreToolUse, workflow, cfg); output != nil {
		t.Fatalf("a subagent was denied a workflow at 50%%: %v", output)
	}
	if runs := workflowLaunches(readState(), sid); len(runs) != 0 {
		t.Fatalf("a subagent's workflow was recorded as the session's own run: %v", runs)
	}
}

func TestTheSubagentGuardFollowsObserveModeAndThePause(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 93, 40)
	defer func(previous bool) { observing = previous }(observing)
	observing = true
	if output := hookOutput(t, onPostToolBatch, subagentBatch("sg-observe", project, "a1", "Bash"), cfg); output != nil {
		t.Fatalf("observe mode warned a subagent: %v", output)
	}
	if output := hookOutput(t, onPreToolUse, subagentTool("sg-observe", project, "Write", object{"file_path": filepath.Join(project, "x.go")}), cfg); output != nil {
		t.Fatalf("observe mode denied a subagent's write: %v", output)
	}
	if records := agentCutOffs(readState(), "sg-observe"); len(records) != 0 {
		t.Fatalf("observe mode recorded a warning: %v", records)
	}
	if journal, _ := os.ReadFile(files.decisions); !strings.Contains(string(journal), "would-warn-subagent") || !strings.Contains(string(journal), "would-deny-subagent-tool") {
		t.Fatalf("observe mode journaled nothing for the subagent: %s", journal)
	}
	observing = false
	updateState(func(state object) { state["disabledUntil"] = float64(nowSec() + 3600) })
	if output := hookOutput(t, onPostToolBatch, subagentBatch("sg-off", project, "a1", "Bash"), cfg); output != nil {
		t.Fatalf("a paused guard warned a subagent: %v", output)
	}
	writeUsage(100, 40, 0)
	if output := hookOutput(t, onPostToolBatch, subagentBatch("sg-off", project, "a1", "Bash"), cfg); !stoppedBy(output) {
		t.Fatalf("a paused guard let a subagent spend paid credits past the ceiling: %v", output)
	}
}

func warnAndStop(t *testing.T, cfg object, sid, cwd, agent string) {
	t.Helper()
	hookOutput(t, onPostToolBatch, subagentBatch(sid, cwd, agent, "Bash"), cfg)
	if output := hookOutput(t, onPostToolBatch, subagentBatch(sid, cwd, agent, "Edit"), cfg); !stoppedBy(output) {
		t.Fatalf("agent %s was not stopped on its second batch: %v", agent, output)
	}
}

func TestACutOffAgentIsNamedWhereTheSessionResumes(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 93, 40)
	sid := "sg-named"
	warnAndStop(t, cfg, sid, project, "a1")
	explore := agentHookInput("PostToolBatch", sid, project, object{"agent_id": "a2", "agent_type": "Explore", "tool_calls": []any{object{"tool_name": "Grep"}}})
	if output := hookOutput(t, onPostToolBatch, explore, cfg); contextOf(output) == "" {
		t.Fatalf("the Explore agent was not warned: %v", output)
	}
	runs := strings.Join(workflowRuns(readState(), sid), "\n")
	for _, want := range []string{"general-purpose agent a1 — stopped by the usage limit at ", "; its result is partial", "Explore agent a2 — told at "} {
		if !strings.Contains(runs, want) {
			t.Fatalf("workflowRuns lacks %q:\n%s", want, runs)
		}
	}
	note := workflowResumeNote(readState(), sid)
	for _, want := range []string{"redo their unfinished part instead of trusting a saved result", "general-purpose agent a1", "Explore agent a2"} {
		if !strings.Contains(note, want) {
			t.Fatalf("the resume note lacks %q with no workflow launched: %q", want, note)
		}
	}
	if strings.Contains(note, "A dynamic workflow was running") {
		t.Fatalf("the resume note invents a workflow: %q", note)
	}
	checkpoint := buildCheckpoint(agentHookInput("PostToolBatch", sid, project, nil), "paused", "opus", cfg)
	if content, err := os.ReadFile(checkpoint); err != nil || !strings.Contains(string(content), "general-purpose agent a1 — stopped by the usage limit") {
		t.Fatalf("the checkpoint does not name the cut-off agent (%v):\n%s", err, content)
	}
	recordWorkflowLaunch(sid, object{"tool_input": object{"name": "audit-routes"}}, nowSec())
	if note := workflowResumeNote(readState(), sid); !strings.Contains(note, "audit-routes") || !strings.Contains(note, "general-purpose agent a1") {
		t.Fatalf("a workflow launch pushed the cut-off agents out of the resume note: %q", note)
	}
	writeUsage(20, 40, 0)
	prompt := getString(section(cfg, "resume"), "prompt") + " " + workflowResumeNote(readState(), sid)
	defer func(previous bool) { promptFromPlugin = previous }(promptFromPlugin)
	if promptFromPlugin = pluginComposedPrompt(cfg, sid, prompt); !promptFromPlugin {
		t.Fatal("the relaunch prompt was not recognised as the plugin's own")
	}
	hookOutput(t, onUserPromptSubmit, agentHookInput("UserPromptSubmit", sid, project, object{"prompt": prompt}), cfg)
	if records := agentCutOffs(readState(), sid); len(records) != 0 {
		t.Fatalf("the relaunched session was told about the cut-off agents, yet they stay listed for the next pause: %v", records)
	}
	if runs := workflowLaunches(readState(), sid); len(runs) != 1 {
		t.Fatalf("forgetting the cut-off agents dropped the workflow launch: %v", runs)
	}
}

func TestANewSessionTakingOverTheCheckpointHearsOfTheCutOffAgents(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 93, 40)
	warnAndStop(t, cfg, "sg-old", project, "a1")
	buildCheckpoint(agentHookInput("PostToolBatch", "sg-old", project, nil), "paused", "opus", cfg)
	start := hookOutput(t, onSessionStart, agentHookInput("SessionStart", "sg-new", project, object{"source": "clear"}), cfg)
	if context := contextOf(start); !strings.Contains(context, "sg-old") || !strings.Contains(context, "general-purpose agent a1 — stopped by the usage limit") {
		t.Fatalf("the session taking over the checkpoint was not told which agent was cut off: %v", start)
	}
	if records := agentCutOffs(readState(), "sg-old"); len(records) != 0 {
		t.Fatalf("the cut-off agents stay listed after the checkpoint was handed over: %v", records)
	}
}

func TestAnInHookWaitEndsByNamingTheCutOffAgents(t *testing.T) {
	cfg, project, _ := limitSandbox(t, object{"wait": object{"resetMarginSeconds": 0, "earlyResetPollMinutes": 0}}, 93, 40)
	sid := "sg-inhook"
	writeUsage(93, 40, float64(nowSec()+3))
	warnAndStop(t, cfg, sid, project, "a1")
	output := hookOutput(t, onPostToolBatch, agentHookInput("PostToolBatch", sid, project, nil), cfg)
	if stoppedBy(output) {
		t.Fatalf("a three-second wait was not held in the hook: %v", output)
	}
	if context := contextOf(output); !strings.Contains(context, "general-purpose agent a1 — stopped by the usage limit") || !strings.Contains(context, "redo their unfinished part") {
		t.Fatalf("the main thread came back from the wait without hearing which agent was cut off: %v", output)
	}
	if records := agentCutOffs(readState(), sid); len(records) != 0 {
		t.Fatalf("the cut-off agents stay listed after the main thread was told: %v", records)
	}
}

func claudeFailure(errorName, details, message string) object {
	fields := object{"error": errorName}
	if details != "" {
		fields["error_details"] = details
	}
	if message != "" {
		fields["last_assistant_message"] = message
	}
	return fields
}

func adapterFailure(errorType, message string) object {
	return object{"error_type": errorType, "error_message": message}
}

func TestClaudeCodesModelNotFoundHealsTheModel(t *testing.T) {
	cases := []struct {
		name   string
		fields object
	}{
		{"the payload Claude Code sends", claudeFailure("model_not_found", "model: claude-gone-9", "There's an issue with the selected model (claude-gone-9). It may not exist or you may not have access to it.")},
		{"the payload a host adapter builds", adapterFailure("model_not_found", "model: claude-gone-9")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, project, _ := limitSandbox(t, nil, 20, 40)
			mustWriteJSON(files.settings, object{"model": "claude-gone-9"})
			sid := "sf-model"
			hookOutput(t, onStopFailure, agentHookInput("StopFailure", sid, project, c.fields), cfg)
			if got, want := settingsModel(), getString(section(cfg, "models"), "fallback"); want == "" || got != want {
				t.Fatalf("model_not_found left settings.model at %q instead of the fallback %q", got, want)
			}
			if wait := pendingWait(sid); wait != nil {
				t.Fatalf("model_not_found scheduled a retry on the model the plan lacks: %v", wait)
			}
		})
	}
}

func TestClaudeCodesOverloadIsBackedOffNotFiledAsAWall(t *testing.T) {
	cases := []struct {
		name   string
		five   float64
		fields object
	}{
		{"overloaded far from the limits", 20, claudeFailure("overloaded", "", "API Error: Repeated 529 Overloaded errors")},
		{"server_error with the 5-hour window at 95%", 95, claudeFailure("server_error", "500 Internal Server Error", "API Error: 500 Internal server error")},
		{"a host adapter's overloaded with the 5-hour window at 95%", 95, adapterFailure("overloaded", "overloaded_error 529")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, project, _ := limitSandbox(t, nil, c.five, 40)
			sid := "sf-overload"
			hookOutput(t, onStopFailure, agentHookInput("StopFailure", sid, project, c.fields), cfg)
			wait := pendingWait(sid)
			if wait == nil || !getBool(wait, "overload", false) || getString(wait, "window") != "unknown" {
				t.Fatalf("the overload was not filed as a backoff: %v", wait)
			}
			if delay := numberOr(wait, "resumeAt", 0) - float64(nowSec()); delay < 1 || delay > 60 {
				t.Fatalf("the first overload retry is %vs away, want the ~30s backoff", delay)
			}
			if attempts := numberOr(getMap(getMap(readState(), "overload"), sid), "attempts", 0); attempts != 1 {
				t.Fatalf("the overload episode counts %v attempts, want 1", attempts)
			}
		})
	}
}

func TestClaudeCodesAccountErrorsAreNotRetried(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 20, 40)
	cases := []struct {
		name   string
		fields object
	}{
		{"authentication_failed", claudeFailure("authentication_failed", "401 Unauthorized", "Invalid API key · Please run /login")},
		{"billing_error", claudeFailure("billing_error", "", "Credit balance is too low")},
		{"account_on_hold", claudeFailure("account_on_hold", "", "Your account is on hold")},
	}
	for _, c := range cases {
		sid := "sf-" + c.name
		hookOutput(t, onStopFailure, agentHookInput("StopFailure", sid, project, c.fields), cfg)
		if wait := pendingWait(sid); wait != nil {
			t.Fatalf("%s was scheduled for a retry that cannot succeed, so the session is woken only to fail again: %v", c.name, wait)
		}
		if numberOr(getMap(readState(), "notified"), "account:"+c.name, 0) == 0 {
			t.Fatalf("%s raised no account notice", c.name)
		}
	}
}

func TestClaudeCodesRateLimitMessageNamesTheCulprit(t *testing.T) {
	cases := []struct {
		name         string
		five, weekly float64
		fields       object
		want         string
		journaled    string
	}{
		{"the weekly limit, named where Claude Code puts it", 50, 70, claudeFailure("rate_limit", "", "You've hit your weekly limit · resets Mon 9am"), "seven_day", "weekly limit"},
		{"the weekly limit, named in error_details", 50, 70, claudeFailure("rate_limit", "weekly limit reached", ""), "seven_day", "weekly limit reached"},
		{"the session limit", 30, 40, claudeFailure("rate_limit", "", "You've hit your session limit · resets 3pm"), "five_hour", "session limit"},
		{"a plain 429 that names no limit", 30, 40, claudeFailure("rate_limit", "429 Too Many Requests", "API Error: Rate limit reached"), "unknown", "Rate limit reached"},
		{"a host adapter's error_message naming the weekly limit", 50, 70, adapterFailure("rate_limit", "You've hit your weekly limit · resets Monday"), "seven_day", "weekly limit"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, project, _ := limitSandbox(t, nil, c.five, c.weekly)
			sid := "sf-limit"
			hookOutput(t, onStopFailure, agentHookInput("StopFailure", sid, project, c.fields), cfg)
			if window := getString(pendingWait(sid), "window"); window != c.want {
				t.Fatalf("the rate limit was filed under %q, want %q: %v", window, c.want, pendingWait(sid))
			}
			if journal, _ := os.ReadFile(files.decisions); !strings.Contains(string(journal), c.journaled) {
				t.Fatalf("the journal does not keep the error text %q:\n%s", c.journaled, journal)
			}
		})
	}
}

func TestAnOverloadRetryDoesNotWakeASessionPastThePausePoint(t *testing.T) {
	if five := os.Getenv("NOCTIS_TEST_OVERLOAD_WAKE"); five != "" {
		used, err := strconv.ParseFloat(five, 64)
		if err != nil {
			t.Fatal(err)
		}
		cfg, project, _ := limitSandbox(t, nil, used, 40)
		sid := "sf-wake"
		now := float64(nowSec())
		usage := readJSON(files.usage)
		usage["sessions"] = object{sid: object{"model": "claude-opus-5", "cwd": project, "transcript": filepath.Join(project, "transcript.jsonl"), "updatedAt": now, "context": nil}}
		mustWriteJSON(files.usage, usage)
		record := object{"kind": "stopfailure", "window": "unknown", "label": "overloaded", "used": nil, "overload": true, "attempt": float64(1), "until": now, "resumeAt": now, "startedAt": now, "cwd": project, "transcript": filepath.Join(project, "transcript.jsonl")}
		updateState(func(state object) { stateMap(state, "waits")[sid] = cloneObject(record) })
		wakeSameSession(cfg, sid, record, now)
		if wait := pendingWait(sid); wait == nil || wait["wakeAttemptedAt"] != nil {
			t.Fatalf("the overload wait was not left to the runner: %v", wait)
		}
		return
	}
	wake := func(five string) (int, string) {
		scratch := t.TempDir()
		child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
		child.Env = append(os.Environ(), "NOCTIS_TEST_OVERLOAD_WAKE="+five, "TMPDIR="+scratch, "TMP="+scratch, "TEMP="+scratch)
		output, err := child.CombinedOutput()
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode(), string(output)
		} else if err != nil {
			t.Fatal(err)
		}
		return 0, string(output)
	}
	if code, output := wake("95"); code != 0 || strings.Contains(output, "[noctis] The API was") {
		t.Fatalf("an overload retry woke a session whose 5-hour window is at 95%% (pause point 92%%) without re-checking the limit: exit %d\n%s", code, output)
	}
	if code, output := wake("50"); code != 2 || !strings.Contains(output, "[noctis] The API was overloaded") {
		t.Fatalf("an overload retry did not wake a session at 50%%: exit %d\n%s", code, output)
	}
}

func controlSandbox(t *testing.T, five, weekly float64) (object, string) {
	t.Helper()
	cfg, project, _ := limitSandbox(t, object{"wait": object{"maxInHookMinutes": 1}}, five, weekly)
	writeUsage(five, weekly, float64(nowSec()+7200))
	return cfg, project
}

func promptInput(sid, cwd, prompt string) object {
	return agentHookInput("UserPromptSubmit", sid, cwd, object{"prompt": prompt})
}

func shellCall(tool, command string) object {
	return object{"tool_name": tool, "tool_input": object{"command": command}, "tool_use_id": "toolu_shell", "tool_response": object{"stdout": "ok"}}
}

func mainBatch(sid, cwd string, calls ...object) object {
	list := []any{}
	for _, call := range calls {
		list = append(list, call)
	}
	return agentHookInput("PostToolBatch", sid, cwd, object{"tool_calls": list})
}

func TestTheControlCommandsPassThePausePoint(t *testing.T) {
	cfg, project := controlSandbox(t, 95, 40)
	for i, prompt := range []string{"/noctis:status", "/noctis:pause 120", "  /noctis:setup --profile economy", "/noctis:resume", "/noctis:pause"} {
		sid := "cp-" + strconv.Itoa(i)
		if output := hookOutput(t, onUserPromptSubmit, promptInput(sid, project, prompt), cfg); output != nil {
			t.Fatalf("%q with the 5h window at 95%% (pause point 92%%) was not let through: %v", prompt, output)
		}
		if wait := pendingWait(sid); wait != nil {
			t.Fatalf("%q registered a wait, so the relaunch after the reset would replay it: %v", prompt, wait)
		}
	}
	if journal, _ := os.ReadFile(files.decisions); !strings.Contains(string(journal), `"action":"control-prompt"`) || !strings.Contains(string(journal), "/noctis:pause") {
		t.Fatalf("the journal does not say which control prompt passed the pause point:\n%s", journal)
	}
	for i, prompt := range []string{"fix the parser", "/noctis:statusbar", "/noctis:unknown 120", "/other:pause 120", "please run /noctis:pause 120"} {
		sid := "cw-" + strconv.Itoa(i)
		output := hookOutput(t, onUserPromptSubmit, promptInput(sid, project, prompt), cfg)
		if getString(output, "decision") != "block" {
			t.Fatalf("%q went past the 5h pause point: %v", prompt, output)
		}
		if queued := getString(pendingWait(sid), "queuedPrompt"); queued != prompt {
			t.Fatalf("%q was not parked for the relaunch (queuedPrompt %q)", prompt, queued)
		}
	}
	now := float64(nowSec())
	updateState(func(state object) {
		stateMap(state, "waits")["cp-esc"] = object{"kind": "prompt", "window": "five_hour", "until": now + 7200, "resumeAt": now + 7290, "inHook": true, "startedAt": now - 600, "heartbeat": now - 5, "queuedPrompt": "fix the parser"}
	})
	hookOutput(t, onUserPromptSubmit, promptInput("cp-esc", project, "/noctis:pause 120"), cfg)
	if wait := pendingWait("cp-esc"); wait != nil {
		t.Fatalf("an in-hook wait the user broke off survived the control prompt that followed, so its runner can still relaunch the session: %v", wait)
	}
}

func TestAControlCommandLeavesAParkedPromptAlone(t *testing.T) {
	cfg, project := controlSandbox(t, 40, 95)
	sid := "cp-parked"
	work := "fix the parser so that nested brackets are handled"
	if output := hookOutput(t, onUserPromptSubmit, promptInput(sid, project, work), cfg); getString(output, "decision") != "block" {
		t.Fatalf("a work prompt at 95%% weekly was not parked: %v", output)
	}
	parked := pendingWait(sid)
	if getString(parked, "queuedPrompt") != work {
		t.Fatalf("the work prompt was not queued for the relaunch: %v", parked)
	}
	for _, prompt := range []string{"/noctis:pause 120", "/noctis:status"} {
		if output := hookOutput(t, onUserPromptSubmit, promptInput(sid, project, prompt), cfg); output != nil {
			t.Fatalf("%q at 95%% weekly was not let through: %v", prompt, output)
		}
		wait := pendingWait(sid)
		if getString(wait, "queuedPrompt") != work || numberOr(wait, "startedAt", -1) != numberOr(parked, "startedAt", 0) {
			t.Fatalf("%q replaced the parked wait, so the relaunch after the reset replays it instead of the work: %v", prompt, wait)
		}
	}
}

func TestAtTheCreditCeilingAControlCommandIsStillRefused(t *testing.T) {
	cfg, project := controlSandbox(t, 40, 95)
	sid := "cc-parked"
	work := "fix the parser so that nested brackets are handled"
	hookOutput(t, onUserPromptSubmit, promptInput(sid, project, work), cfg)
	parked := pendingWait(sid)
	if getString(parked, "queuedPrompt") != work {
		t.Fatalf("the work prompt was not parked: %v", parked)
	}
	writeUsage(100, 95, float64(nowSec()+7200))
	for _, prompt := range []string{"/noctis:status", "/noctis:pause 120", "/noctis:resume"} {
		output := hookOutput(t, onUserPromptSubmit, promptInput(sid, project, prompt), cfg)
		reason := getString(output, "reason")
		if getString(output, "decision") != "block" || !strings.Contains(reason, T("hit.ceiling")) {
			t.Fatalf("%q at 100%% with paid credits refused was not refused for the ceiling: %v", prompt, output)
		}
		if strings.Contains(reason, "/noctis:pause") || strings.Contains(reason, "noctis off") {
			t.Fatalf("the ceiling refusal offers a pause that cannot lift it: %s", reason)
		}
		wait := pendingWait(sid)
		if getString(wait, "queuedPrompt") != work || numberOr(wait, "startedAt", -1) != numberOr(parked, "startedAt", 0) {
			t.Fatalf("%q at the ceiling replaced the parked wait: %v", prompt, wait)
		}
	}
	if output := hookOutput(t, onUserPromptSubmit, promptInput("cc-fresh", project, "/noctis:status"), cfg); getString(output, "decision") != "block" {
		t.Fatalf("a control prompt at the ceiling was let through: %v", output)
	}
	if wait := pendingWait("cc-fresh"); wait != nil {
		t.Fatalf("a control prompt refused at the ceiling was parked for a relaunch: %v", wait)
	}
	updateState(func(state object) { state["disabledUntil"] = float64(nowSec() + 3600) })
	if output := hookOutput(t, onUserPromptSubmit, promptInput("cc-paused", project, "/noctis:resume"), cfg); getString(output, "decision") != "block" {
		t.Fatalf("a paused guard let a control prompt spend paid credits past the ceiling: %v", output)
	}
	defer func(previous bool) { observing = previous }(observing)
	observing = true
	if output := hookOutput(t, onUserPromptSubmit, promptInput("cc-observe", project, "/noctis:status"), cfg); output != nil {
		t.Fatalf("observe mode refused a control prompt: %v", output)
	}
	if journal, _ := os.ReadFile(files.decisions); !strings.Contains(string(journal), "would-block-control-prompt") {
		t.Fatalf("observe mode journaled nothing for the control prompt at the ceiling:\n%s", journal)
	}
}

func TestABatchOfTheOwnCommandsIsNotHeldPastThePausePoint(t *testing.T) {
	cfg, project := controlSandbox(t, 95, 40)
	binary := `"` + filepath.Join(files.pluginRoot, "bin", "noctis") + `"`
	status := shellCall("Bash", binary+" status")
	why := shellCall("Bash", binary+" why --last 10")
	if output := hookOutput(t, onPostToolBatch, mainBatch("cb-status", project, status, why), cfg); output != nil {
		t.Fatalf("the batch of /noctis:status was held at 95%%: %v", output)
	}
	if wait := pendingWait("cb-status"); wait != nil {
		t.Fatalf("the batch of /noctis:status registered a wait: %v", wait)
	}
	both := shellCall("Bash", binary+" status\n"+binary+" why --last 10")
	if output := hookOutput(t, onPostToolBatch, mainBatch("cb-lines", project, both), cfg); output != nil || pendingWait("cb-lines") != nil {
		t.Fatalf("the two lines of /noctis:status in one Bash call were held at 95%%: %v", output)
	}
	windows := shellCall("PowerShell", `& `+binary+` status`)
	if output := hookOutput(t, onPostToolBatch, mainBatch("cb-pwsh", project, windows), cfg); output != nil || pendingWait("cb-pwsh") != nil {
		t.Fatalf("the PowerShell form of /noctis:status was held at 95%%: %v", output)
	}
	edit := object{"tool_name": "Edit", "tool_input": object{"file_path": filepath.Join(project, "src", "parser.go")}, "tool_use_id": "toolu_edit"}
	held := map[string]object{
		"cb-edit":    mainBatch("cb-edit", project, status, edit),
		"cb-chained": mainBatch("cb-chained", project, shellCall("Bash", binary+" status && go test ./...")),
		"cb-other":   mainBatch("cb-other", project, shellCall("Bash", "go test ./...")),
		"cb-empty":   mainBatch("cb-empty", project),
		"cb-none":    agentHookInput("PostToolBatch", "cb-none", project, nil),
	}
	for sid, input := range held {
		if output := hookOutput(t, onPostToolBatch, input, cfg); !stoppedBy(output) {
			t.Fatalf("%s: a batch that is not only the plugin's own commands went on past the pause point: %v", sid, output)
		}
		if pendingWait(sid) == nil {
			t.Fatalf("%s: the held batch has no wait to resume it", sid)
		}
	}
	writeUsage(100, 40, float64(nowSec()+7200))
	if output := hookOutput(t, onPostToolBatch, mainBatch("cb-ceiling", project, status), cfg); !stoppedBy(output) {
		t.Fatalf("the batch of /noctis:status went on at the paid-credit ceiling: %v", output)
	}
}

func TestOnlyPlainCallsOfThePluginBinaryCountAsItsOwnCommands(t *testing.T) {
	defer func(previous string) { files.pluginRoot = previous }(files.pluginRoot)
	cases := []struct {
		root     string
		accepted []string
		rejected []string
	}{
		{
			root: "/home/dev/.claude/plugins/cache/noctis/noctis/7.0.0",
			accepted: []string{
				`"{root}/bin/noctis" status`,
				`"{root}/bin/noctis" why --last 10`,
				`'{root}/bin/noctis' off 120`,
				`{root}/bin/noctis on`,
				`"${CLAUDE_PLUGIN_ROOT}/bin/noctis" setup --profile economy --code opus:high --research sonnet:high`,
				`$CLAUDE_PLUGIN_ROOT/bin/noctis status`,
				`"{root}/bin/linux-arm64/noctis" doctor`,
				"\"{root}/bin/noctis\" status\n\"{root}/bin/noctis\" why --last 10\n",
				`"{root}/bin/noctis" status && "{root}/bin/noctis" why --last 10`,
				`"{root}/bin/noctis" status 2>&1`,
				`"{root}/bin/noctis" setup --config-dir "/home/dev/claude work"`,
				`  "{root}/bin/noctis" off  `,
			},
			rejected: []string{
				`"{root}/bin/noctis" status && npm test`,
				`"{root}/bin/noctis" status; rm -rf build`,
				`"{root}/bin/noctis" status || make`,
				`"{root}/bin/noctis" status | tee status.txt`,
				`"{root}/bin/noctis" status > status.txt`,
				`"{root}/bin/noctis" status & npm test`,
				"\"{root}/bin/noctis\" status\nnpm test",
				`"{root}/bin/noctis" status $(npm test)`,
				`"{root}/bin/noctis" status "$(npm test)"`,
				"\"{root}/bin/noctis\" status `npm test`",
				`"{root}/bin/noctis" status $HOME`,
				`"{root}/bin/noctis" status # done`,
				`"{root}/bin/noctis" status * `,
				`"{root}/bin/noctis" status \"; npm test; echo \"`,
				`"{root}/bin/noctis" status <<<x`,
				`( "{root}/bin/noctis" status )`,
				`cd /tmp && "{root}/bin/noctis" status`,
				`NOCTIS_LANG=en "{root}/bin/noctis" status`,
				`noctis status`,
				`"/usr/local/bin/noctis" status`,
				`"{root}/bin/noctis-dev" status`,
				`"{root}/bin/tools/extra/noctis" status`,
				`"{root}/scripts/install.sh"`,
				`"{root}/bin/noctis"status`,
				`"{root}/bin/noctis status`,
				`"${CLAUDE_PLUGIN_ROOT}/bin/noctis$(npm test)" status`,
				``,
				` ; `,
			},
		},
		{
			root: `C:\Users\Dev User\.claude\plugins\cache\noctis\noctis\7.0.0`,
			accepted: []string{
				`& "{root}\bin\noctis.exe" status`,
				`& "{root}/bin/noctis" why --last 10`,
				`& '{root}\bin\windows-arm64\noctis.exe' off 120`,
				`& "$env:CLAUDE_PLUGIN_ROOT\bin\noctis.exe" status`,
				`& "{root}\bin\noctis.exe" status; & "{root}\bin\noctis.exe" why --last 10`,
				"& \"{root}\\bin\\noctis.exe\" status\r\n& \"{root}\\bin\\noctis.exe\" why --last 10",
				`"{root}/bin/noctis" status`,
				`& "c:\users\dev user\.claude\plugins\cache\noctis\noctis\7.0.0\BIN\NOCTIS.EXE" status`,
			},
			rejected: []string{
				`& "{root}\bin\noctis.exe" status; Remove-Item -Recurse build`,
				`& "{root}\bin\noctis.exe" status | Out-File status.txt`,
				`& "{root}\bin\noctis.exe" (Remove-Item build)`,
				`& "{root}\bin\noctis.exe" "$(Remove-Item build)"`,
				"& \"{root}\\bin\\noctis.exe\" \"status`\"; Remove-Item build\"",
				"& \"{root}\\bin\\noctis.exe\" \"status\u201d; Remove-Item build; \u201c\"",
				`& "{root}\bin\noctis.exe" @rest`,
				`& "{root}\bin\noctis.exe" --% status`,
				`& "{root}\bin\noctis.exe" status 2>&1 > out.txt`,
				`& & "{root}\bin\noctis.exe" status`,
				`&"{root}\bin\noctis.exe" status`,
				`& "{root}\bin\noctis.exe\" status`,
				`{root}\bin\noctis.exe status`,
			},
		},
	}
	for _, c := range cases {
		files.pluginRoot = c.root
		for _, command := range c.accepted {
			if command = strings.ReplaceAll(command, "{root}", c.root); !runsOwnBinaryOnly(command) {
				t.Errorf("a plain call of the plugin's binary was not recognised: %q", command)
			}
		}
		for _, command := range c.rejected {
			if command = strings.ReplaceAll(command, "{root}", c.root); runsOwnBinaryOnly(command) {
				t.Errorf("a command that is not only the plugin's binary passed as its own: %q", command)
			}
		}
	}
	files.pluginRoot = ""
	if runsOwnBinaryOnly(`"/bin/noctis" status`) {
		t.Fatal("with no known plugin root a bare /bin/noctis passed as the plugin's own binary")
	}
}

func TestAControlCommandFromAFableSessionStillMeetsTheModelSwitch(t *testing.T) {
	cfg, project := controlSandbox(t, 40, 40)
	now := float64(nowSec())
	mustWriteJSON(files.fable, object{"fetchedAt": now, "fable": object{"used": float64(96), "resetsAt": now + 86400}})
	sid := "cp-fable"
	updateState(func(state object) {
		stateMap(state, "modelOverrides")[sid] = object{"model": "claude-fable-5-1", "at": now}
	})
	output := hookOutput(t, onUserPromptSubmit, promptInput(sid, project, "/noctis:pause 120"), cfg)
	fallback := getString(section(cfg, "models"), "fallback")
	if getString(output, "decision") != "block" || !strings.Contains(getString(output, "reason"), fallback) {
		t.Fatalf("a control prompt from a Fable session at 96%% of the Fable bucket skipped the model switch: %v", output)
	}
	if model := settingsModel(); model != fallback {
		t.Fatalf("the default model was not switched to %q: %q", fallback, model)
	}
}

func TestEveryControlCommandIsAShippedSkill(t *testing.T) {
	for name := range controlSkills {
		if _, err := os.Stat(filepath.Join("..", "..", "..", "skills", name, "SKILL.md")); err != nil {
			t.Errorf("/%s:%s passes the pause point, but the plugin ships no such skill: %v", pluginName, name, err)
		}
	}
}

func retiredProfileSandbox(t *testing.T, overrides object, fable float64) (object, string) {
	t.Helper()
	_, project, _ := limitSandbox(t, overrides, 20, 10)
	roles := cloneObject(retiredProfiles["noctis"])
	roles["profile"] = "noctis"
	applyRoles(files.config, readJSON(files.config), roles)
	now := float64(nowSec())
	mustWriteJSON(files.usage, object{
		"updatedAt": now,
		"five_hour": object{"used": float64(20), "resetsAt": now + 3600},
		"seven_day": object{"used": float64(10), "resetsAt": now + 3*86400},
		"sessions":  object{"s1": object{"model": "claude-opus-5-5", "updatedAt": now}},
	})
	mustWriteJSON(files.settings, object{"model": "opus"})
	if fable >= 0 {
		writeFableBucket(fable, now, now+2*86400)
	}
	return loadConfig(), project
}

func writeFableBucket(used, fetchedAt, resetsAt float64) {
	mustWriteJSON(files.fable, object{"fetchedAt": fetchedAt, "fable": object{"used": used, "resetsAt": resetsAt}})
}

func agentSpawn(sid, cwd string, toolInput object) object {
	return agentHookInput("PreToolUse", sid, cwd, object{"tool_name": "Agent", "tool_input": toolInput})
}

func spawnedModel(output object) string {
	return getString(getMap(getMap(output, "hookSpecificOutput"), "updatedInput"), "model")
}

func planSpawn(t *testing.T, cfg object, cwd string) object {
	t.Helper()
	return hookOutput(t, onPreToolUse, agentSpawn("s1", cwd, object{"subagent_type": "Plan", "prompt": "plan the parser rewrite"}), cfg)
}

func fanOutPrompt(sid, cwd string) object {
	return promptInput(sid, cwd, "Audit every route handler under src/routes for missing auth checks and fix what you find")
}

func TestFableSubagentsRunOnTheFallbackWhileTheFableQuotaIsOut(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, nil, 97)
	if pinned := getString(getMap(section(cfg, "router"), "subagentModels"), "Plan"); pinned != "fable" {
		t.Fatalf("the earlier noctis profile does not pin Plan to fable in this sandbox: %q", pinned)
	}
	plan := planSpawn(t, cfg, project)
	if permissionOf(plan) != "allow" || spawnedModel(plan) != "opus" {
		t.Fatalf("with the Fable bucket at 97%% (switch point 95%%) and the session on opus, the Plan subagent was still pinned to Fable: %v", plan)
	}
	explicit := hookOutput(t, onPreToolUse, agentSpawn("s1", project, object{"subagent_type": "general-purpose", "model": "fable", "prompt": "rewrite the parser"}), cfg)
	if permissionOf(explicit) != "allow" || spawnedModel(explicit) != "opus" {
		t.Fatalf("with the Fable bucket at 97%% an explicit Fable subagent went through on Fable: %v", explicit)
	}
	if updated := getMap(getMap(explicit, "hookSpecificOutput"), "updatedInput"); getString(updated, "prompt") != "rewrite the parser" || getString(updated, "subagent_type") != "general-purpose" {
		t.Fatalf("moving the subagent to the fallback dropped the rest of its input: %v", updated)
	}
	if explore := hookOutput(t, onPreToolUse, agentSpawn("s1", project, object{"subagent_type": "Explore", "prompt": "map the parser"}), cfg); spawnedModel(explore) != "haiku" {
		t.Fatalf("the Explore pin, outside the Fable quota, changed: %v", explore)
	}
	if output := hookOutput(t, onPreToolUse, agentSpawn("s1", project, object{"subagent_type": "general-purpose", "model": "sonnet", "prompt": "x"}), cfg); output != nil {
		t.Fatalf("an explicit model outside the Fable quota was touched: %v", output)
	}
	if journal, _ := os.ReadFile(files.decisions); !strings.Contains(string(journal), `"action":"subagent-fallback"`) || !strings.Contains(string(journal), "Plan: fable → opus") {
		t.Fatalf("the move to the fallback was not journaled: %s", journal)
	}
	writeFableBucket(50, float64(nowSec()), float64(nowSec()+2*86400))
	if plan := planSpawn(t, cfg, project); spawnedModel(plan) != "fable" {
		t.Fatalf("with the Fable bucket at 50%% the Plan pin no longer ran on Fable: %v", plan)
	}
	if output := hookOutput(t, onPreToolUse, agentSpawn("s1", project, object{"subagent_type": "general-purpose", "model": "fable", "prompt": "x"}), cfg); output != nil {
		t.Fatalf("with the Fable bucket at 50%% an explicit Fable subagent was changed: %v", output)
	}
	writeFableBucket(97, float64(nowSec()), float64(nowSec()+2*86400))
	section(cfg, "thresholds")["weeklyFable"] = nil
	if plan := planSpawn(t, cfg, project); spawnedModel(plan) != "fable" {
		t.Fatalf("with the Fable threshold switched off the Plan pin was moved anyway: %v", plan)
	}
}

func TestAFableSubagentFollowsTheModelSwitchOnlyWhileItStands(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, object{"fable": object{"revertOnReset": false}}, -1)
	now := float64(nowSec())
	switched := func(fableResetsAt float64) {
		updateState(func(state object) {
			state["modelSwitched"] = object{"at": now - 600, "from": "fable", "to": "opus", "fableResetsAt": fableResetsAt}
		})
	}
	switched(now + 2*86400)
	if model := spawnedModel(planSpawn(t, cfg, project)); model != "opus" {
		t.Fatalf("with no live Fable bucket and the model switch standing until the Fable reset, the Plan subagent ran on %q", model)
	}
	writeFableBucket(60, now, now+2*86400)
	if model := spawnedModel(planSpawn(t, cfg, project)); model != "opus" {
		t.Fatalf("after a switch at 60%% (a rate limit that named the Fable bucket) the Plan subagent ran on %q in the same window", model)
	}
	writeFableBucket(5, now, now+7*86400)
	if model := spawnedModel(planSpawn(t, cfg, project)); model != "fable" {
		t.Fatalf("after the Fable window reset early the Plan subagent was still kept off Fable (%q): revertOnReset=false never clears the switch", model)
	}
	switched(now - 60)
	writeFableBucket(40, now, now+7*86400)
	if model := spawnedModel(planSpawn(t, cfg, project)); model != "fable" {
		t.Fatalf("a model switch whose Fable window has reset kept the Plan subagent off Fable (%q)", model)
	}
	if err := os.Remove(files.fable); err != nil {
		t.Fatal(err)
	}
	if model := spawnedModel(planSpawn(t, cfg, project)); model != "fable" {
		t.Fatalf("with no live Fable bucket a model switch whose Fable window has reset kept the Plan subagent off Fable (%q)", model)
	}
}

func TestAFableSubagentSpawnRefreshesAStaleFableBucket(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, nil, -1)
	now := float64(nowSec())
	var hits atomic.Int32
	body, err := json.Marshal(object{"limits": []any{
		object{"kind": "session", "utilization": 20, "resets_at": now + 3600},
		object{"kind": "weekly_all", "utilization": 10, "resets_at": now + 3*86400},
		object{"kind": "weekly_scoped", "utilization": 97, "resets_at": now + 2*86400, "scope": object{"model": object{"display_name": "Fable"}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	t.Setenv("NOCTIS_USAGE_URL", server.URL)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "token")
	writeFableBucket(50, now-120, now+2*86400)
	if model := spawnedModel(planSpawn(t, cfg, project)); model != "fable" || hits.Load() != 0 {
		t.Fatalf("a two-minute-old Fable bucket far from the switch point was fetched again (%d fetches) or the pin changed (%q)", hits.Load(), model)
	}
	writeFableBucket(50, now-20*60, now+2*86400)
	if model := spawnedModel(hookOutput(t, onPreToolUse, agentSpawn("s1", project, object{"subagent_type": "Explore", "prompt": "x"}), cfg)); model != "haiku" || hits.Load() != 0 {
		t.Fatalf("a spawn outside the Fable quota fetched the Fable bucket (%d fetches, model %q)", hits.Load(), model)
	}
	if model := spawnedModel(planSpawn(t, cfg, project)); model != "opus" || hits.Load() != 1 {
		t.Fatalf("a Plan subagent pinned to Fable went by a 20-minute-old bucket at 50%% while the live one reads 97%%: model %q after %d fetches", model, hits.Load())
	}
	writeFableBucket(90, now-120, now+2*86400)
	if model := spawnedModel(planSpawn(t, cfg, project)); model != "opus" || hits.Load() != 2 {
		t.Fatalf("within 8 points of the Fable switch point a two-minute-old bucket was not fetched again: model %q after %d fetches", model, hits.Load())
	}
}

func TestTheFableSubagentFallbackFollowsObserveModeAndTheHost(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, nil, 97)
	defer func(previous bool) { observing = previous }(observing)
	observing = true
	if output := planSpawn(t, cfg, project); output != nil {
		t.Fatalf("observe mode changed the Plan subagent's model: %v", output)
	}
	if journal, _ := os.ReadFile(files.decisions); !strings.Contains(string(journal), "would-subagent-fallback") {
		t.Fatalf("observe mode journaled nothing for the Fable subagent: %s", journal)
	}
	observing = false
	now := nowSec()
	usage := currentUsage(now)
	if model := scopedSafeModel(cfg, readState(), usage, "fable", now); model != "opus" {
		t.Fatalf("on Claude Code a Fable subagent at 97%% stays on %q", model)
	}
	previous := activeHost
	t.Cleanup(func() { activeHost = previous })
	for _, host := range []string{"codex", "antigravity", "droid", "copilot"} {
		activeHost = host
		if model := scopedSafeModel(cfg, readState(), usage, "fable", now); model != "fable" {
			t.Errorf("%s, which has no subagents to pin and no model switch, moved a Fable model to %q", host, model)
		}
	}
}

func TestTheFanOutAdviceSendsNoAgentToFableWhileTheFableQuotaIsOut(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, object{"workflow": object{"gate": false}}, 97)
	advice := contextOf(hookOutput(t, onUserPromptSubmit, fanOutPrompt("s1", project), cfg))
	if !strings.Contains(advice, "fan-out task") || !strings.Contains(advice, "code-writing agents → opus (effort max)") || strings.Contains(strings.ToLower(advice), "fable") {
		t.Fatalf("with the Fable bucket at 97%% the fan-out advice still sends agents to Fable: %q", advice)
	}
	trustQueueFile(writeQueueFile(t, project, "# q\n- [ ] migrate every component under src/components to TypeScript\n- [ ] fix typo\n"), true)
	reason := getString(hookOutput(t, onStop, stopInput("s1", project), cfg), "reason")
	if !strings.Contains(reason, "The next item looks like a fan-out task") || !strings.Contains(reason, "code-writing agents → opus (effort max)") || strings.Contains(strings.ToLower(reason), "fable") {
		t.Fatalf("with the Fable bucket at 97%% the queue's fan-out advice still sends agents to Fable: %q", reason)
	}
	writeFableBucket(50, float64(nowSec()), float64(nowSec()+2*86400))
	if advice := contextOf(hookOutput(t, onUserPromptSubmit, fanOutPrompt("s1", project), cfg)); !strings.Contains(advice, "code-writing agents → fable (effort max)") {
		t.Fatalf("with the Fable bucket at 50%% the fan-out advice no longer names the code role's model: %q", advice)
	}
}

func TestNoWorkflowIsAdvisedThatTheLaunchGateWouldRefuse(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, nil, 97)
	if advice := contextOf(hookOutput(t, onUserPromptSubmit, fanOutPrompt("s1", project), cfg)); strings.Contains(advice, "fan-out task") {
		t.Fatalf("with -2 points of Fable room a workflow was advised that the launch gate refuses: %q", advice)
	}
	launch := agentHookInput("PreToolUse", "s1", project, object{"tool_name": "Workflow", "tool_input": object{"name": "audit-routes"}})
	if output := hookOutput(t, onPreToolUse, launch, cfg); permissionOf(output) != "deny" {
		t.Fatalf("the launch gate let a workflow fan out with -2 points of Fable room: %v", output)
	}
	trustQueueFile(writeQueueFile(t, project, "# q\n- [ ] migrate every component under src/components to TypeScript\n- [ ] fix typo\n"), true)
	if reason := getString(hookOutput(t, onStop, stopInput("s1", project), cfg), "reason"); !strings.Contains(reason, "Queue continues") || strings.Contains(reason, "fan-out task") {
		t.Fatalf("with -2 points of Fable room the queue advised a workflow that the launch gate refuses: %q", reason)
	}
	writeFableBucket(50, float64(nowSec()), float64(nowSec()+2*86400))
	if advice := contextOf(hookOutput(t, onUserPromptSubmit, fanOutPrompt("s1", project), cfg)); !strings.Contains(advice, "fan-out task") {
		t.Fatalf("with 45 points of Fable room the fan-out prompt got no workflow advice: %q", advice)
	}
}

func copilotSandbox(t *testing.T) (object, string) {
	t.Helper()
	dir := sandboxFiles(t)
	files.pluginRoot = dir
	files.credentials = filepath.Join(dir, ".credentials.json")
	mustWriteJSON(files.credentials, object{})
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	mustWriteJSON(filepath.Join(dir, "config.default.json"), shippedDefaults(t))
	mustWriteJSON(files.config, object{"host": "copilot", "fable": object{"source": "off"}, "alarm": object{"enabled": false}, "update": object{"check": false}})
	project := t.TempDir()
	trustQueueFile(writeQueueFile(t, project, "# q\n- [ ] write the parser tests\n- [ ] document the flags\n"), true)
	return loadConfig(), project
}

func copilotPayload(sid, cwd string, fields object) object {
	payload := object{"sessionId": sid, "timestamp": float64(nowSec() * 1000), "cwd": cwd}
	for key, value := range fields {
		payload[key] = value
	}
	return payload
}

func hostHook(t *testing.T, host string, payload object) object {
	t.Helper()
	defer func(previousHost, previousEvent, previousCommand, previousLocale string, loaded, fromPlugin, printed bool, cached object) {
		activeHost, activeEvent, command, locale = previousHost, previousEvent, previousCommand, previousLocale
		stdinLoaded, promptFromPlugin, emitted, stdinCache = loaded, fromPlugin, printed, cached
	}(activeHost, activeEvent, command, locale, stdinLoaded, promptFromPlugin, emitted, stdinCache)
	activeHost, command, emitted = host, "hook", false
	stdinLoaded, stdinCache = true, payload
	printed := strings.TrimSpace(capturedStdout(t, runHook))
	if printed == "" {
		return nil
	}
	var output object
	if err := json.Unmarshal([]byte(printed), &output); err != nil {
		t.Fatalf("the %s hook printed something that is not JSON: %q", host, printed)
	}
	return output
}

func copilotStop(sid, cwd string) object {
	return copilotPayload(sid, cwd, object{"transcriptPath": filepath.Join(cwd, "events.jsonl"), "stopReason": "end_turn", "stop_hook_active": false})
}

func TestCopilotsCamelCasePayloadsAreNamedByTheirShape(t *testing.T) {
	transcript := filepath.Join(t.TempDir(), "events.jsonl")
	failure := object{"message": "Rate limit exceeded, retry later", "name": "RateLimitError", "stack": "RateLimitError: Rate limit exceeded, retry later"}
	wired := []struct {
		fields object
		event  string
	}{
		{object{"source": "new", "initialPrompt": "write the parser tests"}, "SessionStart"},
		{object{"source": "resume"}, "SessionStart"},
		{object{"reason": "complete"}, "SessionEnd"},
		{object{"reason": "error"}, "SessionEnd"},
		{object{"prompt": "write the parser tests"}, "UserPromptSubmit"},
		{object{"toolName": "bash", "toolArgs": object{"command": "go test ./...", "description": "run the tests"}}, "PreToolUse"},
		{object{"toolName": "task", "toolArgs": `{"prompt":"map the parser"}`}, "PreToolUse"},
		{object{"transcriptPath": transcript, "stopReason": "end_turn", "stop_hook_active": false}, "Stop"},
		{object{"errorContext": "model_call", "recoverable": false, "error": failure}, "StopFailure"},
		{object{"errorContext": "system", "recoverable": false, "error": object{"message": "the session could not be saved", "name": "Error"}}, "StopFailure"},
		{object{"reason": "error", "error": failure}, "SessionEnd"},
	}
	for _, tc := range wired {
		raw := copilotPayload("cp-shape", "/p", tc.fields)
		name := copilotEventName(raw)
		raw["hook_event_name"] = name
		if event, input := normalizeHookInput("copilot", raw); event != tc.event || getString(input, "session_id") != "cp-shape" {
			t.Errorf("the Copilot payload %v was named %q and normalised to %q for session %q, want %s", tc.fields, name, event, getString(input, "session_id"), tc.event)
		}
	}
	unwired := []object{
		{"toolName": "bash", "toolArgs": object{}, "toolResult": object{"resultType": "success", "textResultForLlm": "ok"}},
		{"toolName": "bash", "toolArgs": object{}, "error": "exit status 1"},
		{"transcriptPath": transcript, "agentId": "a1", "agentType": "explore", "agentName": "explore", "response": "done", "stopReason": "end_turn"},
		{"transcriptPath": transcript, "agentName": "explore", "agentDescription": "maps the code"},
		{"prompt": "fix it", "transformedPrompt": "fix it now"},
		{"transcriptPath": transcript, "trigger": "auto", "customInstructions": ""},
		{"error": failure},
		{},
	}
	for _, fields := range unwired {
		raw := copilotPayload("cp-shape", "/p", fields)
		name := copilotEventName(raw)
		raw["hook_event_name"] = name
		if event, _ := normalizeHookInput("copilot", raw); event != "" {
			t.Errorf("the Copilot payload %v, of an event noctis does not wire, was named %q and taken for %s", fields, name, event)
		}
	}
	recovered := []object{
		{"errorContext": "model_call", "recoverable": true, "error": failure},
		{"errorContext": "tool_execution", "recoverable": true, "error": object{"message": "ENOENT: no such file or directory, open 'notes.md'", "name": "Error"}},
		{"errorContext": "model_call", "error": failure},
	}
	for _, fields := range recovered {
		raw := copilotPayload("cp-shape", "/p", fields)
		name := copilotEventName(raw)
		raw["hook_event_name"] = name
		if event, _ := normalizeHookInput("copilot", raw); event != "" {
			t.Errorf("the Copilot error %v, which Copilot does not mark unrecoverable, was named %q and taken for %s", fields, name, event)
		}
	}
}

func TestCopilotHooksThatNameNoEventStillReachTheirHandlers(t *testing.T) {
	_, project := copilotSandbox(t)
	stop := hostHook(t, "copilot", copilotStop("cp-1", project))
	if getString(stop, "decision") != "block" || !strings.Contains(getString(stop, "reason"), "Queue continues: 2 open in TASKS.md") {
		t.Fatalf("Copilot's agentStop names no event and the trusted queue did not continue: %v", stop)
	}
	updateState(func(state object) {
		stateMap(state, "overload")["cp-2"] = object{"firstAt": float64(nowSec() - 60), "attempts": float64(1), "lastAt": float64(nowSec() - 60)}
	})
	hostHook(t, "copilot", copilotPayload("cp-2", project, object{"prompt": "parser testlerini yaz ve bayrakları belgele"}))
	if lang, episode := sessionLanguage(readState(), "cp-2"), getMap(getMap(readState(), "overload"), "cp-2"); lang != "tr" || episode != nil {
		t.Fatalf("Copilot's userPromptSubmitted names no event and the prompt handler never saw it: session language %q, overload episode %v", lang, episode)
	}
	updateState(func(state object) {
		stateMap(state, "autoResume")["cp-3"] = object{"type": "quota_auto_resume_fired", "at": float64(nowSec())}
	})
	hostHook(t, "copilot", copilotPayload("cp-3", project, object{"reason": "complete"}))
	if auto := getMap(getMap(readState(), "autoResume"), "cp-3"); auto != nil {
		t.Fatalf("Copilot's sessionEnd names no event and the ended session's state was kept: %v", auto)
	}
	failure := copilotPayload("cp-1", project, object{"errorContext": "model_call", "recoverable": false, "error": object{"message": "Rate limit exceeded, retry later", "name": "RateLimitError", "stack": "RateLimitError: Rate limit exceeded, retry later"}})
	if output := hostHook(t, "copilot", failure); output != nil {
		t.Fatalf("Copilot ignores what errorOccurred prints, yet the hook printed %v", output)
	}
	if wait := pendingWait("cp-1"); getString(wait, "kind") != "stopfailure" || getString(wait, "window") != "unknown" {
		t.Fatalf("Copilot's errorOccurred for an error it cannot recover from names no event and no retry was scheduled: %v", wait)
	}
}

func TestOnlyAnErrorCopilotCannotRecoverFromIsRetried(t *testing.T) {
	_, project := copilotSandbox(t)
	recovered := []object{
		{"errorContext": "tool_execution", "recoverable": true, "error": object{"message": "ENOENT: no such file or directory, open 'notes.md'", "name": "Error"}},
		{"errorContext": "model_call", "recoverable": true, "error": object{"message": "503 Service Unavailable", "name": "APIError"}},
		{"errorContext": "model_call", "recoverable": true, "error": object{"message": "Rate limit exceeded, retry later", "name": "RateLimitError"}},
		{"errorContext": "model_call", "error": object{"message": "Request timed out", "name": "TimeoutError"}},
	}
	for _, fields := range recovered {
		if output := hostHook(t, "copilot", copilotPayload("cp-live", project, fields)); output != nil {
			t.Fatalf("Copilot ignores what errorOccurred prints, yet the hook printed %v", output)
		}
		state := readState()
		if wait, episode, checkpoint := getMap(getMap(state, "waits"), "cp-live"), getMap(getMap(state, "overload"), "cp-live"), getMap(getMap(state, "checkpoints"), "cp-live"); wait != nil || episode != nil || checkpoint != nil {
			t.Fatalf("an error Copilot goes on from (%v) set up a relaunch of the live session: wait %v, overload episode %v, checkpoint %v", fields, wait, episode, checkpoint)
		}
	}
	if journal, _ := os.ReadFile(files.decisions); strings.Contains(string(journal), `"event":"StopFailure"`) {
		t.Fatalf("an error Copilot goes on from was handled as a failed turn: %s", journal)
	}
	stop := hostHook(t, "copilot", copilotStop("cp-live", project))
	if getString(stop, "decision") != "block" || !strings.Contains(getString(stop, "reason"), "Queue continues: 2 open in TASKS.md") {
		t.Fatalf("the agentStop that ends the turn after a recoverable error did not continue the trusted queue: %v", stop)
	}
	fatal := object{"message": "Rate limit exceeded, retry later", "name": "RateLimitError"}
	hostHook(t, "copilot", copilotPayload("cp-dead", project, object{"errorContext": "model_call", "recoverable": false, "error": fatal}))
	if wait := pendingWait("cp-dead"); getString(wait, "kind") != "stopfailure" || getString(wait, "window") != "unknown" || getString(wait, "checkpoint") == "" {
		t.Fatalf("an error Copilot cannot recover from got no checkpoint and retry: %v", wait)
	}
	updateState(func(state object) {
		stateMap(state, "autoResume")["cp-dead"] = object{"type": "quota_auto_resume_fired", "at": float64(nowSec())}
	})
	hostHook(t, "copilot", copilotPayload("cp-dead", project, object{"reason": "error", "error": fatal}))
	if auto, wait := getMap(getMap(readState(), "autoResume"), "cp-dead"), pendingWait("cp-dead"); auto != nil || wait == nil {
		t.Fatalf("the sessionEnd that follows an unrecoverable error was not run as the end of the session, or it dropped the retry: autoResume %v, wait %v", auto, wait)
	}
	if journal, _ := os.ReadFile(files.decisions); strings.Count(string(journal), `"event":"StopFailure"`) != 1 {
		t.Fatalf("the sessionEnd that carries the error was taken for a second failed turn: %s", journal)
	}
	hostHook(t, "copilot", copilotPayload("cp-busy", project, object{"errorContext": "model_call", "recoverable": false, "error": object{"message": "503 Service Unavailable", "name": "APIError"}}))
	if busy := pendingWait("cp-busy"); getString(busy, "kind") != "stopfailure" || !getBool(busy, "overload", false) {
		t.Fatalf("an overload Copilot cannot recover from got no backoff retry: %v", busy)
	}
}

func TestACopilotSessionStartHandsOverTheCheckpointOnce(t *testing.T) {
	cfg, project := copilotSandbox(t)
	checkpoint := buildCheckpoint(object{"session_id": "cp-old", "cwd": project}, "paused", "", cfg)
	if checkpoint == "" {
		t.Fatal("no checkpoint was written")
	}
	start := hostHook(t, "copilot", copilotPayload("cp-new", project, object{"source": "new", "initialPrompt": "write the parser tests"}))
	if context := getString(start, "additionalContext"); !strings.Contains(context, checkpoint) || !strings.Contains(context, "Queue mode (TASKS.md: 2 open)") {
		t.Fatalf("Copilot's sessionStart names no event and the new session was not handed the checkpoint and the queue: %v", start)
	}
	if !getBool(getMap(getMap(readState(), "checkpoints"), "cp-old"), "consumed", false) {
		t.Fatal("the checkpoint Copilot's sessionStart delivered is still offered to the next session")
	}
	later := hostHook(t, "copilot", copilotPayload("cp-later", project, object{"source": "new"}))
	if context := getString(later, "additionalContext"); strings.Contains(context, checkpoint) || !strings.Contains(context, "Queue mode (TASKS.md: 2 open)") {
		t.Fatalf("a later Copilot session was handed the spent checkpoint again, or lost the queue directive: %v", later)
	}
}

func TestAnEventNameTheHostSentIsKeptAsItIs(t *testing.T) {
	_, project := copilotSandbox(t)
	labeled := copilotStop("cp-labeled", project)
	labeled["hook_event_name"] = "agentStop"
	if output := hostHook(t, "copilot", labeled); getString(output, "decision") != "block" || getString(labeled, "hook_event_name") != "agentStop" {
		t.Fatalf("a Copilot agentStop that names its event no longer continues the queue: %v (event %q)", output, getString(labeled, "hook_event_name"))
	}
	contrary := copilotStop("cp-contrary", project)
	contrary["hook_event_name"], contrary["reason"] = "sessionEnd", "complete"
	if output := hostHook(t, "copilot", contrary); output != nil || getMap(getMap(readState(), "stopGuard"), "cp-contrary") != nil || getString(contrary, "hook_event_name") != "sessionEnd" {
		t.Fatalf("a payload named sessionEnd was run as the agentStop its fields resemble: %v", output)
	}
	for _, host := range []string{"claude", "codex", "droid"} {
		sid := "cp-" + host
		if output := hostHook(t, host, copilotStop(sid, project)); output != nil || getMap(getMap(readState(), "stopGuard"), sid) != nil {
			t.Fatalf("%s: a payload without hook_event_name was given an event: %v", host, output)
		}
	}
}
