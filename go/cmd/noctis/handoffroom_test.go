package main

import (
	"fmt"
	"strings"
	"testing"
)

const noSubagentWords = "No subagent can be opened now (10 points left before the weekly pause point, under subagents.weeklyRoom 15): do the next item in this session yourself"

func leaveLittleWeeklyRoom(t *testing.T) {
	t.Helper()
	usage := readJSON(files.usage)
	week := getMap(usage, "seven_day")
	if week == nil {
		t.Fatal("the usage file has no weekly window")
	}
	week["used"] = float64(85)
	mustWriteJSON(files.usage, usage)
}

func TestAnItemForAnotherModelStaysInTheSessionWhenNoSubagentCanBeOpened(t *testing.T) {
	cfg, _, frontend := modelQueueSandbox(t, "sonnet")
	runsOn("hr1", "claude-opus-5-5", 20000)
	leaveLittleWeeklyRoom(t)
	reason := getString(stopHookOutput(t, stopInput("hr1", frontend), cfg), "reason")
	if !strings.Contains(reason, noSubagentWords) || strings.Contains(reason, "hand it to") {
		t.Fatalf("with 10 points of weekly room the (sonnet) item is still handed to a subagent:\n%s", reason)
	}
	if entry := journaledEntry("hr1", "continue-queue"); !strings.Contains(getString(entry, "noSubagent"), "subagents.weeklyRoom 15") || getString(entry, "itemModel") != "sonnet" {
		t.Fatalf("the continuation is journaled as %v", entry)
	}
}

func TestTheLargeContextHandOffStaysInTheSessionWhenNoSubagentCanBeOpened(t *testing.T) {
	cfg, _, frontend, _ := deferSandbox(t)
	section(cfg, "queue")["subagentAboveTokens"] = float64(100000)
	runsOn("hr2", "claude-opus-5-5", 150000)
	if reason := getString(stopHookOutput(t, stopInput("hr2", frontend), cfg), "reason"); !strings.Contains(reason, "context already holds") {
		t.Fatalf("with room, a session that opted in is not told to hand the next item to a fresh context:\n%s", reason)
	}
	runsOn("hr2b", "claude-opus-5-5", 150000)
	leaveLittleWeeklyRoom(t)
	reason := getString(stopHookOutput(t, stopInput("hr2b", frontend), cfg), "reason")
	if strings.Contains(reason, "context already holds") || !strings.Contains(reason, noSubagentWords) {
		t.Fatalf("with 10 points of weekly room the next item is still handed to a fresh context:\n%s", reason)
	}
}

func TestAStuckItemIsSetAsideInsteadOfGoingUpWhenNoSubagentCanBeOpened(t *testing.T) {
	cfg, _, frontend, path := escalationSandbox(t, "hr3", "claude-sonnet-5-5", "high")
	leaveLittleWeeklyRoom(t)
	last := stopsWithoutProgress(t, "hr3", frontend, cfg)
	if reason := getString(last, "reason"); !strings.Contains(reason, "queue defer <a unique part of its text>") || strings.Contains(reason, "Hand it once") || strings.Contains(reason, "stronger model") {
		t.Fatalf("with 10 points of weekly room the stuck item is not set aside:\n%s", reason)
	}
	if want := T("queue.setAsideMessage", 2, "migrate the users table"); !strings.Contains(getString(last, "systemMessage"), want) {
		t.Fatalf("the user was told %q, want %q", getString(last, "systemMessage"), want)
	}
	entry := journaledEntry("hr3", "continue-queue")
	if !strings.Contains(getString(entry, "escalateNoRoom"), "subagents.weeklyRoom 15") || !getBool(entry, "setAside", false) || getBool(entry, "escalated", false) {
		t.Fatalf("the set-aside is journaled as %v", entry)
	}
	state := readState()
	if escalationsToday(state, nowSec()) != 0 || escalationRecord(state, path, "migrate the users table") != nil {
		t.Fatalf("an item that stayed in the session is counted as one that went up: %v", getMap(state, "escalateDay"))
	}
}

func TestAnItemThatWentUpStaysInTheSessionWhenNoSubagentCanBeOpened(t *testing.T) {
	cfg, _, frontend, _ := escalationSandbox(t, "hr4", "claude-sonnet-5-5", "high")
	if reason := getString(stopsWithoutProgress(t, "hr4", frontend, cfg), "reason"); !strings.Contains(reason, "Hand it once") {
		t.Fatalf("with room the stuck item did not go up:\n%s", reason)
	}
	runsOn("hr4b", "claude-sonnet-5-5", 20000)
	leaveLittleWeeklyRoom(t)
	reason := getString(stopHookOutput(t, stopInput("hr4b", frontend), cfg), "reason")
	if !strings.Contains(reason, noSubagentWords) || strings.Contains(reason, "keep it with") {
		t.Fatalf("with 10 points of weekly room a later session keeps the item with its subagent:\n%s", reason)
	}
}

func TestAFailingCheckIsHeldWithoutAStrongerModelWhenNoSubagentCanBeOpened(t *testing.T) {
	cfg, project, frontend, command := failingCheckSandbox(t, "hr5", "claude-sonnet-5-5")
	leaveLittleWeeklyRoom(t)
	if first := stopHookOutput(t, stopInput("hr5", frontend), cfg); !strings.Contains(getString(first, "reason"), checkLastRuleWords) {
		t.Fatalf("the send-back before the hold does not say that the next failure holds the queue: %v", first)
	}
	second := stopHookOutput(t, stopAgain("hr5", frontend), cfg)
	if getString(second, "decision") == "block" || getString(second, "systemMessage") != T("queue.heldMessage", command, 2, "TASKS.md") {
		t.Fatalf("with 10 points of weekly room the second failure did not hold the queue: %v", second)
	}
	if entry := journaledEntry("hr5", "hold-queue"); !strings.Contains(getString(entry, "escalateNoRoom"), "subagents.weeklyRoom 15") {
		t.Fatalf("the hold is journaled as %v", entry)
	}
	if escalationsToday(readState(), nowSec()) != 0 || queueCheckRuns(project) != 2 {
		t.Fatalf("the hold counted %v items that went up and ran the check %d times", getMap(readState(), "escalateDay"), queueCheckRuns(project))
	}
}

func TestResearchIsNotRoutedToTheLiteAgentWhenNoSubagentCanBeOpened(t *testing.T) {
	cfg, project := retiredProfileSandbox(t, object{"router": object{"enabled": true}, "subagents": object{"guard": true}}, 20)
	research := "research the best mechanical keyboards of 2026 on https://example.com/reviews"
	hookOutput(t, onUserPromptSubmit, promptInput("hr6", project, research), cfg)
	if getMap(getMap(readState(), "routes"), "hr6") == nil {
		t.Fatalf("with room the research prompt %q was not routed", research)
	}
	leaveLittleWeeklyRoom(t)
	output := hookOutput(t, onUserPromptSubmit, promptInput("hr6b", project, research), cfg)
	if getMap(getMap(readState(), "routes"), "hr6b") != nil || journaledAction("hr6b", "route") || strings.Contains(fmt.Sprint(output), "Non-code research") {
		t.Fatalf("with 10 points of weekly room the research prompt was routed: %v", output)
	}
	if entry := journaledEntry("hr6b", "skip-route"); !strings.Contains(getString(entry, "reason"), "subagents.weeklyRoom 15") {
		t.Fatalf("the skipped route is journaled as %v", entry)
	}
}

func TestSpawnHeadroomNamesWhatLeavesNoRoomForASubagent(t *testing.T) {
	cfg, _, _ := limitSandbox(t, nil, 10, 20)
	now := nowSec()
	usage := currentUsage(now)
	if why := spawnHeadroom(cfg, usage, burnView{}, "hr8", now); why != "" {
		t.Fatalf("with room and no subagent opened, spawnHeadroom = %q", why)
	}
	updateState(func(state object) {
		stateMap(stateMap(state, spawnStateKey), "sessions")["hr8"] = object{"count": float64(20), "at": float64(now)}
		stateMap(stateMap(state, spawnStateKey), "sessions")["hr8-old"] = object{"count": float64(20), "at": float64(now - stateEntryTTLSeconds - 1)}
	})
	if why := spawnHeadroom(cfg, usage, burnView{}, "hr8", now); why != "20 already opened in this session, limit subagents.perSessionDeny 20" {
		t.Fatalf("a session at subagents.perSessionDeny: spawnHeadroom = %q", why)
	}
	for _, sid := range []string{"hr8-other", "hr8-old"} {
		if why := spawnHeadroom(cfg, usage, burnView{}, sid, now); why != "" {
			t.Fatalf("session %s, which opened none lately, is refused: %q", sid, why)
		}
	}
	recent := []any{float64(now - spawnHourSeconds)}
	for index := range 12 {
		recent = append(recent, float64(now-int64(index)))
	}
	updateState(func(state object) { stateMap(state, spawnStateKey)["recent"] = recent })
	if why := spawnHeadroom(cfg, usage, burnView{}, "hr8-other", now); why != "12 opened in the last hour, limit subagents.perHourDeny 12" {
		t.Fatalf("12 subagents in the last hour: spawnHeadroom = %q", why)
	}
	burn := burnView{known: true, stop: true, perHour: 4, runOut: 7200, resetIn: 86400}
	if why := spawnHeadroom(cfg, usage, burn, "hr8-other", now); !strings.HasPrefix(why, "weekly burn 4 points/h") {
		t.Fatalf("a burn past subagents' share: spawnHeadroom = %q", why)
	}
	updateState(func(state object) { delete(state, spawnStateKey) })
	section(cfg, "subagents")["weeklyRoom"] = float64(80)
	if why := spawnHeadroom(cfg, usage, burnView{}, "hr8", now); why != "75 points left before the weekly pause point, under subagents.weeklyRoom 80" {
		t.Fatalf("75 points of weekly room under subagents.weeklyRoom 80: spawnHeadroom = %q", why)
	}
	section(cfg, "subagents")["guard"] = false
	if why := spawnHeadroom(cfg, usage, burn, "hr8", now); why != "" {
		t.Fatalf("with subagents.guard off, spawnHeadroom = %q", why)
	}
}

func TestTheQueueDirectiveSendsOnlyResearchThatNeedsSeveralReadsToTheLiteAgent(t *testing.T) {
	cfg, project, _, _ := deferSandbox(t)
	start := func(sid string) string {
		input := object{"hook_event_name": "SessionStart", "source": "startup", "session_id": sid, "cwd": project}
		return getString(getMap(hookOutput(t, onSessionStart, input, cfg), "hookSpecificOutput"), "additionalContext")
	}
	context := start("hr9")
	if !strings.Contains(context, `Items needing no code or file edits that take several searches or reads (research, copy, docs, analysis) go to "noctis:lite"`) || !strings.Contains(context, "Look up a short fact yourself with one search; code stays with you.") {
		t.Fatalf("the queue directive does not keep short facts out of the lite agent: %q", context)
	}
	leaveLittleWeeklyRoom(t)
	context = start("hr9b")
	if !strings.Contains(context, "No subagent can be opened now (10 points left before the weekly pause point, under subagents.weeklyRoom 15): do every item in this session yourself") || strings.Contains(context, `go to "noctis:lite"`) {
		t.Fatalf("with 10 points of weekly room the queue directive still sends items to the lite agent: %q", context)
	}
}
