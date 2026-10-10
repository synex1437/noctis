package main

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func spawnAgent(t *testing.T, cfg object, sid, project, kind string) object {
	t.Helper()
	return hookOutput(t, onPreToolUse, agentHookInput("PreToolUse", sid, project, object{"tool_name": "Agent", "tool_input": object{"subagent_type": kind, "description": "parser", "prompt": "look into the parser"}}), cfg)
}

func journalWith(action string) []string {
	rows := []string{}
	for _, line := range tailFileLines(files.decisions, 200) {
		if strings.Contains(line, `"`+action+`"`) {
			rows = append(rows, line)
		}
	}
	return rows
}

func TestTheSpawnGateWarnsThenRefusesPastTheSessionLimit(t *testing.T) {
	cfg, project, _ := limitSandbox(t, object{"subagents": object{"perSessionWarn": float64(2), "perSessionDeny": float64(3)}}, 10, 30)

	if output := spawnAgent(t, cfg, "sg-s", project, "general-purpose"); output != nil {
		t.Fatalf("the session's first subagent got %v", output)
	}
	warned := spawnAgent(t, cfg, "sg-s", project, "general-purpose")
	if permissionOf(warned) == "deny" || !strings.Contains(contextOf(warned), "opened 2 subagents") || !strings.Contains(getString(warned, "systemMessage"), "2 subagents opened") {
		t.Fatalf("the second subagent at perSessionWarn 2 got %v", warned)
	}
	again := spawnAgent(t, cfg, "sg-s", project, "general-purpose")
	if permissionOf(again) == "deny" || !strings.Contains(contextOf(again), "opened 3 subagents") || getString(again, "systemMessage") != "" {
		t.Fatalf("the third subagent: Claude is told again, the user only once; got %v", again)
	}
	refused := spawnAgent(t, cfg, "sg-s", project, "general-purpose")
	if permissionOf(refused) != "deny" || !strings.Contains(reasonOf(refused), "as many subagents as it may (3") || !strings.Contains(getString(refused, "systemMessage"), "subagents.perSessionDeny") {
		t.Fatalf("the fourth subagent past perSessionDeny 3 got %v", refused)
	}
	if output := spawnAgent(t, cfg, "sg-s", project, "general-purpose"); permissionOf(output) != "deny" || getString(output, "systemMessage") != "" {
		t.Fatalf("the fifth subagent: want a refusal without a second notice, got %v", output)
	}
	if output := spawnAgent(t, cfg, "sg-other", project, "general-purpose"); permissionOf(output) == "deny" {
		t.Fatalf("another session was refused for this session's count: %v", output)
	}

	if rows := journalWith("warn-subagent-spawn"); len(rows) != 2 || !strings.Contains(rows[0], "general-purpose: 2 in this session, 2 in the last hour") {
		t.Fatalf("the warnings were journaled as %v", rows)
	}
	if rows := journalWith("deny-subagent-spawn"); len(rows) != 2 || !strings.Contains(rows[0], "3 already opened in this session, limit subagents.perSessionDeny 3") {
		t.Fatalf("the refusals were journaled as %v", rows)
	}
	if rows := journalWith("spawn-subagent"); len(rows) != 2 {
		t.Fatalf("the plain spawns were journaled as %v", rows)
	}
}

func TestTheSpawnGateCountsTheLastHourAcrossSessions(t *testing.T) {
	cfg, project, _ := limitSandbox(t, object{"subagents": object{"perHourWarn": float64(0), "perHourDeny": float64(2)}}, 10, 30)
	defer func(previous int64) { timeOffset = previous }(timeOffset)

	for _, sid := range []string{"sg-h1", "sg-h2"} {
		if output := spawnAgent(t, cfg, sid, project, "general-purpose"); permissionOf(output) == "deny" {
			t.Fatalf("subagent of %s refused under the hourly limit: %v", sid, output)
		}
	}
	refused := spawnAgent(t, cfg, "sg-h3", project, "general-purpose")
	if permissionOf(refused) != "deny" || !strings.Contains(reasonOf(refused), "as many subagents as an hour allows") || !strings.Contains(getString(refused, "systemMessage"), "subagents.perHourDeny") {
		t.Fatalf("a third subagent in the hour past perHourDeny 2 got %v", refused)
	}

	timeOffset += spawnHourSeconds + 1
	writeUsage(10, 30, 0)
	if output := spawnAgent(t, cfg, "sg-h3", project, "general-purpose"); permissionOf(output) == "deny" {
		t.Fatalf("an hour later the hourly count still refused a subagent: %v", output)
	}
	spawns := getMap(readState(), spawnStateKey)
	if recent := getList(spawns, "recent"); len(recent) != 1 {
		t.Fatalf("the hourly record kept %v; want only the spawn of this hour", recent)
	}
	if count := numberOr(getMap(getMap(spawns, "sessions"), "sg-h3"), "count", 0); count != 1 {
		t.Fatalf("the refused spawn was counted for its session: count %v", count)
	}
}

func TestTheSpawnGateKeepsTheLastWeeklyPointsForTheSession(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 10, 85)

	refused := spawnAgent(t, cfg, "sg-room", project, "general-purpose")
	if permissionOf(refused) != "deny" || !strings.Contains(reasonOf(refused), "only 10 points of the weekly window are left") || !strings.Contains(getString(refused, "systemMessage"), "subagents.weeklyRoom") {
		t.Fatalf("a subagent with 10 points of weekly room left got %v", refused)
	}
	if output := spawnAgent(t, cfg, "sg-room", project, digestAgentType(cfg)); permissionOf(output) == "deny" {
		t.Fatalf("the exempt digest agent was refused: %v", output)
	}
	if output := spawnAgent(t, cfg, "sg-room", project, "noctis:worker"); permissionOf(output) != "deny" || getString(output, "systemMessage") != "" {
		t.Fatalf("a second refusal for the same reason: want a deny without a second notice, got %v", output)
	}

	if rows := journalWith("deny-subagent-spawn"); len(rows) != 2 || !strings.Contains(rows[0], "general-purpose: 10 points left before the weekly pause point, under subagents.weeklyRoom 15") {
		t.Fatalf("the refusals were journaled as %v", rows)
	}
	if rows := journalWith("spawn-subagent"); len(rows) != 1 || !strings.Contains(rows[0], "noctis:digest: exempt (subagents.exempt)") {
		t.Fatalf("the exempt spawn was journaled as %v", rows)
	}
	lines := whyLines(20)
	shown := ""
	for _, line := range lines {
		shown += line.raw + "\n"
	}
	if !strings.Contains(shown, "deny-subagent-spawn") || !strings.Contains(shown, "subagents.weeklyRoom") {
		t.Fatalf("noctis why does not show the refusal and its reason:\n%s", shown)
	}
}

func TestAWeekBurningTooFastRefusesSubagentsAndWorkflows(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 10, 30)
	if err := os.Remove(files.usage); err != nil {
		t.Fatal(err)
	}
	now := nowSec()
	weekReadings(t, now, float64(now+5*86400), 3, 30, 2, 33, 1, 36, 0, 39)

	refused := spawnAgent(t, cfg, "sg-burn", project, "general-purpose")
	if permissionOf(refused) != "deny" || !strings.Contains(reasonOf(refused), "New subagents are refused until the pace drops") || !strings.Contains(getString(refused, "systemMessage"), "refuses new subagents") {
		t.Fatalf("a subagent in a week burning 3 points an hour with 5 days to go got %v", refused)
	}
	workflow := hookOutput(t, onPreToolUse, agentHookInput("PreToolUse", "sg-burn", project, object{"tool_name": "Workflow", "tool_input": object{"script": "export const meta = {name: 'x'}"}}), cfg)
	if permissionOf(workflow) != "deny" || !strings.Contains(reasonOf(workflow), "Workflows and new subagents are refused") {
		t.Fatalf("a workflow in a week burning too fast got %v", workflow)
	}
	if rows := journalWith("deny-workflow"); len(rows) != 1 || !strings.Contains(rows[0], "weekly burn 3 points/h") {
		t.Fatalf("the workflow refusal was journaled as %v", rows)
	}
	nested := hookOutput(t, onPreToolUse, subagentTool("sg-burn", project, "Agent", object{"subagent_type": "general-purpose", "prompt": "dig deeper"}), cfg)
	if permissionOf(nested) != "deny" || !strings.Contains(reasonOf(nested), "New subagents are refused") {
		t.Fatalf("a subagent's own subagent in a week burning too fast got %v", nested)
	}
}

func TestABurnRefusalGivesClaudeAndTheJournalItsTimesInEnglish(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 10, 30)
	if err := os.Remove(files.usage); err != nil {
		t.Fatal(err)
	}
	now := nowSec()
	weekReadings(t, now, float64(now+5*86400), 3, 30, 2, 33, 1, 36, 0, 39)
	speakTurkish(t)

	refused := spawnAgent(t, cfg, "sg-burn-tr", project, "general-purpose")

	if reason := reasonOf(refused); !regexp.MustCompile(`in about \d+h \d+m, \d+d \d+h \d+m before its reset`).MatchString(reason) {
		t.Fatalf("Claude was told a Turkish user's burn refusal without English durations: %q", reason)
	}
	if rows := journalWith("deny-subagent-spawn"); len(rows) != 1 || !regexp.MustCompile(`pause point in \d+h \d+m, \d+d \d+h \d+m before the reset`).MatchString(rows[0]) {
		t.Fatalf("the refusal was journaled without English durations: %v", rows)
	}
	if notice := getString(refused, "systemMessage"); !regexp.MustCompile(`\d+sa \d+dk`).MatchString(notice) {
		t.Fatalf("the user's own notice lost its Turkish durations: %q", notice)
	}
}

func TestASubagentsOwnSubagentsCountForItsSession(t *testing.T) {
	cfg, project, _ := limitSandbox(t, object{"subagents": object{"perSessionDeny": float64(1)}}, 10, 30)

	if output := spawnAgent(t, cfg, "sg-nest", project, "general-purpose"); permissionOf(output) == "deny" {
		t.Fatalf("the session's first subagent was refused: %v", output)
	}
	nested := hookOutput(t, onPreToolUse, subagentTool("sg-nest", project, "Agent", object{"subagent_type": "general-purpose", "prompt": "dig deeper"}), cfg)
	if permissionOf(nested) != "deny" || !strings.Contains(reasonOf(nested), "as many subagents as it may (1") || !strings.Contains(getString(nested, "systemMessage"), "subagents.perSessionDeny") {
		t.Fatalf("a subagent's own subagent past the session's limit got %v", nested)
	}
}

func TestTheSpawnGateOnlyJournalsInObserveMode(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 10, 85)
	defer func(previous bool) { observing = previous }(observing)
	observing = true

	if output := spawnAgent(t, cfg, "sg-observe", project, "general-purpose"); permissionOf(output) == "deny" || getString(output, "systemMessage") != "" {
		t.Fatalf("observe mode refused a subagent: %v", output)
	}
	if rows := journalWith("would-deny-subagent-spawn"); len(rows) != 1 {
		t.Fatalf("observe mode journaled %v", rows)
	}
	if rows := journalWith("deny-subagent-spawn"); len(rows) != 0 {
		t.Fatalf("observe mode journaled a refusal: %v", rows)
	}
}

func TestARefusedSubagentLiftsTheResearchRoute(t *testing.T) {
	cfg, project, _ := limitSandbox(t, object{"router": object{"enabled": true}}, 10, 85)
	updateState(func(state object) {
		stateMap(state, "routes")["sg-route"] = object{"at": float64(nowSec()), "denies": float64(0), "signal": "web-words"}
	})

	if output := spawnAgent(t, cfg, "sg-route", project, liteAgentType(cfg)); permissionOf(output) != "deny" {
		t.Fatalf("setup: the research agent was not refused with 10 points of weekly room: %v", output)
	}
	if route := getMap(getMap(readState(), "routes"), "sg-route"); route != nil {
		t.Fatalf("the route to the refused research agent is still set: %v", route)
	}
	search := hookOutput(t, onPreToolUse, agentHookInput("PreToolUse", "sg-route", project, object{"tool_name": "WebSearch", "tool_input": object{"query": "vector database"}}), cfg)
	if permissionOf(search) == "deny" {
		t.Fatalf("after the refusal the main thread was still denied the research it must now do: %v", search)
	}
}

func TestTheSpawnGateCanBeTurnedOffOrLoosened(t *testing.T) {
	cfg, project, _ := limitSandbox(t, object{"subagents": object{"guard": false}}, 10, 85)
	for i := 0; i < 25; i++ {
		if output := spawnAgent(t, cfg, "sg-off", project, "general-purpose"); permissionOf(output) == "deny" || output != nil {
			t.Fatalf("subagents.guard false still gated spawn %d: %v", i+1, output)
		}
	}
	if rows := journalWith("spawn-subagent"); len(rows) != 0 {
		t.Fatalf("an unguarded spawn was journaled: %v", rows)
	}

	loose, project, _ := limitSandbox(t, object{"subagents": object{"weeklyRoom": float64(0), "perSessionDeny": float64(0), "perHourDeny": float64(0), "perSessionWarn": float64(0), "perHourWarn": float64(0)}}, 10, 94)
	for i := 0; i < 25; i++ {
		if output := spawnAgent(t, loose, "sg-loose", project, "general-purpose"); permissionOf(output) == "deny" {
			t.Fatalf("limits set to 0 still refused spawn %d: %v", i+1, output)
		}
	}

	strict, project, _ := limitSandbox(t, object{"subagents": object{"exempt": []any{}}}, 10, 85)
	if output := spawnAgent(t, strict, "sg-strict", project, digestAgentType(strict)); permissionOf(output) != "deny" {
		t.Fatalf("an empty exempt list still let the digest agent through: %v", output)
	}
}

func TestTheSpawnAndBurnDefaultsMatchTheShippedConfig(t *testing.T) {
	shipped := shippedDefaults(t)
	if code, file := subagentLimits(object{}), subagentLimits(shipped); !reflect.DeepEqual(code, file) {
		t.Fatalf("the code's subagent defaults %+v differ from config.default.json %+v", code, file)
	}
	codeAlarm, codeLookback, codeShare := burnSettings(object{})
	fileAlarm, fileLookback, fileShare := burnSettings(shipped)
	if codeAlarm != fileAlarm || codeLookback != fileLookback || codeShare != fileShare {
		t.Fatalf("the code's burn defaults (%v %v %v) differ from config.default.json (%v %v %v)", codeAlarm, codeLookback, codeShare, fileAlarm, fileLookback, fileShare)
	}
	if limits := subagentLimits(shipped); !limits.guard || limits.weeklyRoom <= 0 || limits.sessionDeny <= 0 || limits.hourDeny <= 0 {
		t.Fatalf("the shipped subagent guard is not on: %+v", limits)
	}
	for _, kind := range []string{"general-purpose", "noctis:lite", "noctis:worker", "noctis:deep"} {
		if code, file := subagentGrowthLimits(object{}, kind), subagentGrowthLimits(shipped, kind); code != file || !file.active() || file.grace <= 0 {
			t.Fatalf("%s: the code's growth limits %+v differ from config.default.json %+v, or the shipped ones are off", kind, code, file)
		}
	}
	if lite, worker := subagentGrowthLimits(shipped, "noctis:lite"), subagentGrowthLimits(shipped, "noctis:worker"); lite.calls >= worker.calls || lite.words <= 0 || worker.words <= 0 {
		t.Fatalf("the shipped budgets give lite %+v and worker %+v; want lite tighter, and a report size for both", lite, worker)
	}
}

func TestSpawnRecordsAreKeptOnlyAsLongAsTheyCount(t *testing.T) {
	now := nowSec()
	state := object{spawnStateKey: object{
		"sessions": object{
			"old": object{"count": float64(3), "at": float64(now) - stateEntryTTLSeconds - 1},
			"new": object{"count": float64(1), "at": float64(now - 60)},
		},
		"recent": []any{float64(now - spawnHourSeconds - 1), float64(now - 60)},
	}}
	pruneState(state, now)

	spawns := getMap(state, spawnStateKey)
	if sessions := getMap(spawns, "sessions"); sessions["old"] != nil || sessions["new"] == nil {
		t.Fatalf("pruning kept the sessions %v; want only the recent one", sessions)
	}
	if recent := getList(spawns, "recent"); len(recent) != 1 {
		t.Fatalf("pruning kept %v of the hourly record; want the spawn of the last hour", recent)
	}
}
