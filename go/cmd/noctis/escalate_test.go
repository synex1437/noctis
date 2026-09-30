package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// escalationSandbox is a trusted deferQueue driven by the session sid on model at effort, with
// queue.maxIdleContinues 3: the third continuation without progress is the last one before the queue
// gives up.
func escalationSandbox(t *testing.T, sid, model, effort string) (object, string, string, string) {
	t.Helper()
	cfg, project, frontend, path := deferSandbox(t)
	t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", effort)
	section(cfg, "queue")["maxIdleContinues"] = float64(3)
	runsOn(sid, model, 20000)
	return cfg, project, frontend, path
}

// stopsWithoutProgress runs the stop and the continuations that make no progress up to the last one
// before the queue gives up, and returns that one.
func stopsWithoutProgress(t *testing.T, sid, frontend string, cfg object) object {
	t.Helper()
	stopHookOutput(t, stopInput(sid, frontend), cfg)
	stopHookOutput(t, stopAgain(sid, frontend), cfg)
	return stopHookOutput(t, stopAgain(sid, frontend), cfg)
}

func TestAStuckItemGoesOnceToAStrongerModelBeforeItIsSetAside(t *testing.T) {
	cfg, _, frontend, path := escalationSandbox(t, "es1", "claude-sonnet-5-5", "high")
	up := stopsWithoutProgress(t, "es1", frontend, cfg)
	reason := getString(up, "reason")
	if getString(up, "decision") != "block" || !strings.Contains(reason, `"migrate the users table" is still the next item. Hand it once to a general-purpose subagent with model "opus", in the foreground`) || strings.Contains(reason, "Take the next eligible item") {
		t.Fatalf("the last continuation before the queue gives up did not hand the stuck item to Opus: %v", up)
	}
	if !strings.Contains(reason, "what you tried, where and why it failed (the errors verbatim)") || !strings.Contains(reason, "the next stop asks you to set it aside") {
		t.Fatalf("the hand-over does not ask for a brief that stands on its own:\n%s", reason)
	}
	// An item that waits on the user needs no stronger model: the continuation still says how to set it aside.
	if strings.Count(reason, "queue defer") != 1 || !strings.Contains(reason, "waits on something outside this session") {
		t.Fatalf("the hand-over does not keep the word on an item that waits on the user:\n%s", reason)
	}
	if want := T("queue.escalateMessage", 2, "migrate the users table", "Opus"); !strings.Contains(getString(up, "systemMessage"), want) {
		t.Fatalf("the user was told %q, want %q", getString(up, "systemMessage"), want)
	}
	if entry := journaledEntry("es1", "continue-queue"); getString(entry, "escalateTo") != "opus" || !getBool(entry, "escalated", false) || getBool(entry, "setAside", false) {
		t.Fatalf("the hand-over is journaled as %v", entry)
	}
	state := readState()
	record := escalationRecord(state, path, "migrate the users table")
	if getString(record, "model") != "opus" || getString(record, "agent") != "general-purpose" || getString(record, "setup") != "sonnet/high" || getString(record, "outcome") != "" {
		t.Fatalf("the item that went up is recorded as %v", record)
	}
	if escalationsToday(state, nowSec()) != 1 {
		t.Fatalf("the day's count of items that went up is %v, want 1", getMap(state, "escalateDay"))
	}
	if stats := setupStatsOf(state, path); len(stats) != 1 || stats[0].setup != "sonnet/high" || stats[0].escalated != 1 {
		t.Fatalf("the setup that got stuck is recorded as %+v", stats)
	}

	stuck := stopHookOutput(t, stopAgain("es1", frontend), cfg)
	reason = getString(stuck, "reason")
	if getString(stuck, "decision") != "block" || !strings.Contains(reason, `"migrate the users table" is still the next item. Find the root cause`) || !strings.Contains(reason, "It already went to a stronger model (Opus), which did not finish it either") {
		t.Fatalf("the stop after the stronger model did not finish the item did not ask to set it aside: %v", stuck)
	}
	if strings.Count(reason, "queue defer") != 1 || strings.Contains(reason, "Hand it once") || strings.Contains(reason, "keep it with") {
		t.Fatalf("the continuation after the hand-over hands the item over again:\n%s", reason)
	}
	if want := T("queue.escalatedStuck", "migrate the users table", "Opus"); !strings.Contains(getString(stuck, "systemMessage"), want) {
		t.Fatalf("the user was told %q, want %q", getString(stuck, "systemMessage"), want)
	}
	if record := escalationRecord(readState(), path, "migrate the users table"); getString(record, "outcome") != "stuck" {
		t.Fatalf("the item the stronger model did not finish is recorded as %v", record)
	}
	if escalationsToday(readState(), nowSec()) != 1 {
		t.Fatal("setting the item aside counted as another item that went up")
	}
	if output := stopHookOutput(t, stopAgain("es1", frontend), cfg); getString(output, "decision") == "block" {
		t.Fatalf("the queue did not give up at the stop after the one that asked to set the item aside: %v", output)
	}

	// Another session keeps the item with its stronger model.
	runsOn("es1b", "claude-sonnet-5-5", 20000)
	reason = getString(stopHookOutput(t, stopInput("es1b", frontend), cfg), "reason")
	if !strings.Contains(reason, "Take the next eligible item") || !strings.Contains(reason, `keep it with a general-purpose subagent with model "opus" in the foreground`) {
		t.Fatalf("a later session does not keep the item with its stronger model:\n%s", reason)
	}
	// There it has the continuations of any item before it is set aside.
	reason = getString(stopHookOutput(t, stopAgain("es1b", frontend), cfg), "reason")
	if !strings.Contains(reason, `keep it with a general-purpose subagent with model "opus" in the foreground`) || strings.Contains(reason, "Find the root cause") {
		t.Fatalf("a later session set the item aside at its first stop without progress:\n%s", reason)
	}
	if reason = getString(stopHookOutput(t, stopAgain("es1b", frontend), cfg), "reason"); !strings.Contains(reason, "It already went to a stronger model (Opus)") {
		t.Fatalf("a later session did not set the item aside at its last continuation:\n%s", reason)
	}
}

func TestAQueueFileKeepsTheLastItemsThatWentUp(t *testing.T) {
	items := object{}
	for index := 0; index < escalationsKept+2; index++ {
		items[escalationKey(formatNumber(float64(index)))] = object{"at": float64(1000 + index)}
	}
	pruneEscalations(items)
	if len(items) != escalationsKept || items[escalationKey("0")] != nil || items[escalationKey("1")] != nil || items[escalationKey("2")] == nil {
		t.Fatalf("pruning kept %d items, want the %d that went up last", len(items), escalationsKept)
	}
}

func TestAStuckItemOnOpusBelowMaxGoesToTheDeepAgent(t *testing.T) {
	cfg, _, frontend, path := escalationSandbox(t, "es2", "claude-opus-5-5", "xhigh")
	up := stopsWithoutProgress(t, "es2", frontend, cfg)
	if reason := getString(up, "reason"); !strings.Contains(reason, "Hand it once to the "+pluginName+":deep subagent (Opus at max effort)") {
		t.Fatalf("an Opus session at xhigh did not hand the stuck item to the deep agent:\n%s", reason)
	}
	if want := T("queue.escalateMessage", 2, "migrate the users table", "Opus · max"); !strings.Contains(getString(up, "systemMessage"), want) {
		t.Fatalf("the user was told %q, want %q", getString(up, "systemMessage"), want)
	}
	if record := escalationRecord(readState(), path, "migrate the users table"); getString(record, "agent") != pluginName+":deep" || getString(record, "setup") != "opus/xhigh" {
		t.Fatalf("the item that went to the deep agent is recorded as %v", record)
	}
}

func TestAStuckItemOnTheStrongestSetupIsSetAsideAsBefore(t *testing.T) {
	cfg, _, frontend, path := escalationSandbox(t, "es3", "claude-opus-5-5", "max")
	last := stopsWithoutProgress(t, "es3", frontend, cfg)
	if reason := getString(last, "reason"); !strings.Contains(reason, "queue defer <a unique part of its text>") || strings.Contains(reason, "Hand it once") || strings.Contains(reason, "stronger model") {
		t.Fatalf("an Opus session at max effort did not set the stuck item aside:\n%s", reason)
	}
	if want := T("queue.setAsideMessage", 2, "migrate the users table"); !strings.Contains(getString(last, "systemMessage"), want) {
		t.Fatalf("the user was told %q, want %q", getString(last, "systemMessage"), want)
	}
	if escalationRecord(readState(), path, "migrate the users table") != nil {
		t.Fatal("an item set aside is recorded as one that went up")
	}
	if stats := setupStatsOf(readState(), path); len(stats) != 1 || stats[0].setup != "opus/max" || stats[0].stuck != 1 || stats[0].escalated != 0 {
		t.Fatalf("the stuck item is recorded as %+v, want one stuck item on opus/max", stats)
	}
}

func TestAStuckItemIsSetAsideWhenEscalationIsOffOrTheDayHadEnough(t *testing.T) {
	cfg, _, frontend, _ := escalationSandbox(t, "es4", "claude-sonnet-5-5", "high")
	section(cfg, "queue")["escalate"] = "off"
	if reason := getString(stopsWithoutProgress(t, "es4", frontend, cfg), "reason"); strings.Contains(reason, "Hand it once") || !strings.Contains(reason, "queue defer <a unique part of its text>") {
		t.Fatalf("with queue.escalate off the stuck item still went up:\n%s", reason)
	}

	cfg, _, frontend, _ = escalationSandbox(t, "es5", "claude-sonnet-5-5", "high")
	now := nowSec()
	updateState(func(next object) {
		next["escalateDay"] = object{"day": localDay(now), "count": float64(5)}
	})
	last := stopsWithoutProgress(t, "es5", frontend, cfg)
	if reason := getString(last, "reason"); strings.Contains(reason, "Hand it once") || !strings.Contains(reason, "queue defer <a unique part of its text>") {
		t.Fatalf("with queue.maxEscalationsPerDay reached the stuck item still went up:\n%s", reason)
	}
	if entry := journaledEntry("es5", "continue-queue"); !getBool(entry, "escalateCapped", false) || !getBool(entry, "setAside", false) {
		t.Fatalf("the continuation held back by the daily cap is journaled as %v", entry)
	}
}

func TestAnItemTheStrongerModelFinishesIsCreditedToTheSetup(t *testing.T) {
	cfg, project, frontend, path := escalationSandbox(t, "es6", "claude-sonnet-5-5", "high")
	stopsWithoutProgress(t, "es6", frontend, cfg)
	writeQueueFile(t, project, deferTicked("migrate the users table"))
	now := float64(nowSec())
	usage := readJSON(files.usage)
	getMap(usage, "seven_day")["used"] = float64(23)
	usage["updatedAt"] = now
	mustWriteJSON(files.usage, usage)
	next := stopHookOutput(t, stopAgain("es6", frontend), cfg)
	if reason := getString(next, "reason"); !strings.Contains(reason, `("connect the payment provider #pay")`) || strings.Contains(reason, "stronger model") {
		t.Fatalf("once the item was ticked the queue did not go on with the next one:\n%s", reason)
	}
	state := readState()
	if record := escalationRecord(state, path, "migrate the users table"); getString(record, "outcome") != "done" {
		t.Fatalf("the ticked item that went up is recorded as %v", record)
	}
	stats := setupStatsOf(state, path)
	if len(stats) != 1 || stats[0].setup != "sonnet/high" || stats[0].items != 1 || stats[0].escalated != 1 || stats[0].escalatedDone != 1 || stats[0].weekly != 3 || stats[0].escalatedWeekly != 3 {
		t.Fatalf("the setup is credited with %+v, want the one item, done after it went up, at 3 %% of the weekly limit", stats)
	}
	lines := setupLines(state, path)
	if want := T("queue.setupItems", "Sonnet · high", 1) + T("queue.setupEscalated", 1, 1); len(lines) != 1 || lines[0] != want {
		t.Fatalf("the setup lines read %q, want %q", lines, want)
	}
}

func TestEscalationTargetIsOneStepUp(t *testing.T) {
	cfg, _, _, _ := deferSandbox(t)
	cases := []struct {
		name, setting, session, effort, item string
		switched                             bool
		model, agent                         string
	}{
		{"sonnet session", "auto", "claude-sonnet-5-5", "high", "migrate the users table", false, "opus", "general-purpose"},
		{"haiku session", "auto", "claude-haiku-4-5-20251001", "", "migrate the users table", false, "opus", "general-purpose"},
		{"opus below max", "auto", "claude-opus-5-5", "high", "migrate the users table", false, "opus", pluginName + ":deep"},
		{"opus at max", "auto", "claude-opus-5-5", "max", "migrate the users table", false, "", ""},
		{"fable session", "auto", "claude-fable-5-1", "max", "migrate the users table", false, "", ""},
		{"an item tagged for sonnet", "auto", "claude-opus-5-5", "max", "migrate the users table (sonnet)", false, "opus", "general-purpose"},
		{"an item tagged for opus on sonnet", "auto", "claude-sonnet-5-5", "high", "migrate the users table (opus)", false, "opus", pluginName + ":deep"},
		{"off", "off", "claude-sonnet-5-5", "high", "migrate the users table", false, "", ""},
		{"a named model", "fable", "claude-opus-5-5", "max", "migrate the users table", false, "fable", "general-purpose"},
		{"fable out of quota", "fable", "claude-sonnet-5-5", "high", "migrate the users table", true, "opus", "general-purpose"},
		{"an unknown setting", "gpt", "claude-sonnet-5-5", "high", "migrate the users table", false, "", ""},
	}
	for _, c := range cases {
		section(cfg, "queue")["escalate"] = c.setting
		t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", c.effort)
		runsOn("et", c.session, 20000)
		state := object{}
		if c.switched {
			state["modelSwitched"] = object{"at": float64(nowSec())}
		}
		if model, agent := escalationTarget(cfg, state, "et", c.item); model != c.model || agent != c.agent {
			t.Errorf("%s: the item goes to %q through %q, want %q through %q", c.name, model, agent, c.model, c.agent)
		}
	}
	previous := activeHost
	t.Cleanup(func() { activeHost = previous })
	activeHost = "codex"
	section(cfg, "queue")["escalate"] = "auto"
	runsOn("et", "claude-sonnet-5-5", 20000)
	if model, _ := escalationTarget(cfg, object{}, "et", "migrate the users table"); model != "" {
		t.Errorf("a host without subagents hands a stuck item to %q", model)
	}
}

func TestAStuckItemDoesNotGoUpWhenAPauseIsDue(t *testing.T) {
	cfg, _, _, path := escalationSandbox(t, "es7", "claude-sonnet-5-5", "high")
	items := []string{"migrate the users table"}
	if step := stuckItemStep(cfg, readState(), "es7", path, 2, 3, items, true, nowSec()); step.escalate || !step.setAside {
		t.Fatalf("with a pause due the stuck item went up: %+v", step)
	}
	if step := stuckItemStep(cfg, readState(), "es7", path, 1, 3, items, false, nowSec()); step.escalate || step.setAside {
		t.Fatalf("a continuation before the last one did something about the item: %+v", step)
	}
	if step := stuckItemStep(cfg, readState(), "es7", path, 2, 3, items, false, nowSec()); !step.escalate || step.setAside || step.setup != "sonnet/high" {
		t.Fatalf("the last continuation did not hand the item up: %+v", step)
	}
}

func TestTheSessionEffortComesFromTheSessionThenTheSettingsThenTheConfig(t *testing.T) {
	cfg, _, _, _ := deferSandbox(t)
	section(cfg, "models")["effort"] = "high"
	t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "")
	if effort := sessionEffort(cfg); effort != "high" {
		t.Fatalf("with no effort set anywhere else the session runs at %q, want the config's high", effort)
	}
	mustWriteJSON(files.settings, object{"env": object{"CLAUDE_CODE_EFFORT_LEVEL": "xhigh"}})
	if effort := sessionEffort(cfg); effort != "xhigh" {
		t.Fatalf("the settings' effort is read as %q, want xhigh", effort)
	}
	t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "medium")
	if effort := sessionEffort(cfg); effort != "medium" {
		t.Fatalf("the session's own effort is read as %q, want medium", effort)
	}
	t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "loud")
	if effort := sessionEffort(cfg); effort != "xhigh" {
		t.Fatalf("an effort that is no level is read as %q, want the settings' xhigh", effort)
	}
	t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "")
	section(cfg, "models")["primary"] = "opus"
	mustWriteJSON(files.settings, object{"modelSettings": object{"claude-opus-5-5": object{"effortLevel": "low"}, "claude-sonnet-5-5": object{"effortLevel": "max"}}})
	if effort := sessionEffort(cfg); effort != "low" {
		t.Fatalf("the level saved for the code model is read as %q, want low", effort)
	}
	t.Setenv("CLAUDE_EFFORT", "medium")
	if effort := sessionEffort(cfg); effort != "medium" {
		t.Fatalf("the level Claude Code hands its hooks is read as %q, want medium", effort)
	}
	previous := activeEffort
	t.Cleanup(func() { activeEffort = previous })
	activeEffort = "max"
	if effort := sessionEffort(cfg); effort != "max" {
		t.Fatalf("the level the hook input reports is read as %q, want max", effort)
	}
}

func TestSetupTitlesNameTheProfile(t *testing.T) {
	for setup, want := range map[string]string{
		"sonnet/high": "Sonnet · high",
		"opus/max":    "Opus · max",
		"haiku":       "Haiku",
		"":            "",
	} {
		if title := setupTitle(setup); title != want {
			t.Errorf("setupTitle(%q) is %q, want %q", setup, title, want)
		}
	}
	for _, profile := range []string{"balanced", "code", "synex"} {
		setup := profileCodeSetup(profile)
		if title := setupProfileTitle(setup); title != profileTitles[profile]+" ("+setupTitle(setup)+")" {
			t.Errorf("the setup %q of the %s profile is titled %q", setup, profile, title)
		}
	}
	if title := setupProfileTitle("haiku"); title != "Haiku" {
		t.Errorf("a setup no profile has is titled %q", title)
	}
}

func TestTheSetupHintNamesTheSetupThatFinishesTheQueueForLess(t *testing.T) {
	balanced, code := profileCodeSetup("balanced"), profileCodeSetup("code")
	// On Sonnet a weekly limit covers 50 items, escalations included; on Opus 40.
	cheaper := []setupStat{
		{setup: balanced, items: 10, weekly: 20, weeklyItems: 10, escalated: 2, escalatedDone: 2},
		{setup: code, items: 8, weekly: 20, weeklyItems: 8},
	}
	if hint, want := setupHint(cheaper), T("queue.hintCheaper", 50, setupTitle(balanced), 40, setupTitle(code), setupProfileTitle(balanced)); hint != want {
		t.Fatalf("the hint reads %q, want %q", hint, want)
	}
	close := []setupStat{
		{setup: balanced, items: 10, weekly: 20, weeklyItems: 10},
		{setup: code, items: 8, weekly: 17, weeklyItems: 8},
	}
	if hint := setupHint(close); hint != "" {
		t.Fatalf("with 50 items against 47 a weekly limit the hint still names one: %q", hint)
	}
	few := []setupStat{
		{setup: balanced, items: 5, weekly: 10, weeklyItems: 5},
		{setup: code, items: 5, weekly: 20, weeklyItems: 5},
	}
	if hint := setupHint(few); hint != "" {
		t.Fatalf("with five items on each setup the hint already names one: %q", hint)
	}
	up := []setupStat{{setup: "sonnet/high", items: 8, escalated: 3}}
	if hint, want := setupHint(up), T("queue.hintUp", 3, 8, "Sonnet · high", setupProfileTitle(code)); hint != want {
		t.Fatalf("the hint reads %q, want %q", hint, want)
	}
	down := []setupStat{{setup: "opus/max", items: 9}}
	if hint, want := setupHint(down), T("queue.hintDown", 9, "Opus · max", setupProfileTitle(code)); hint != want {
		t.Fatalf("the hint reads %q, want %q", hint, want)
	}
	if hint, want := setupHint([]setupStat{{setup: "opus/xhigh", items: 9}}), T("queue.hintDown", 9, "Opus · xhigh", setupProfileTitle(balanced)); hint != want {
		t.Fatalf("the hint reads %q, want %q", hint, want)
	}
	for _, stats := range [][]setupStat{
		{{setup: "opus/max", items: 7}},
		{{setup: "opus/max", items: 9, stuck: 1}},
		{{setup: "sonnet/high", items: 12, escalated: 2}},
		nil,
	} {
		if hint := setupHint(stats); hint != "" {
			t.Fatalf("%+v gives the hint %q", stats, hint)
		}
	}
}

func TestTheDigestAndQueueStatusSayWhatWentToAStrongerModel(t *testing.T) {
	cfg, project, _, path, _ := digestSandbox(t, "21:00")
	now := nowSec()
	updateState(func(next object) {
		record := queueModelsRecord(next, path, now)
		items := stateMap(record, "items")
		items[escalationKey("migrate the users table")] = object{"text": "migrate the users table", "model": "opus", "agent": "general-purpose", "setup": "sonnet/high", "at": float64(now - 600)}
		items[escalationKey("an item from last week")] = object{"text": "an item from last week", "model": "opus", "agent": "general-purpose", "at": float64(now - 8*86400), "outcome": "done"}
		stateMap(record, "setups")["sonnet/high"] = object{"items": float64(4), "escalated": float64(1)}
	})
	body := buildDigest(cfg, readState(), currentUsage(now), nil, now).body
	for _, want := range []string{
		"- " + T("digest.escalated", formatTime(float64(now-86400)), 1, 0, 0, "migrate the users table"),
		"- " + T("queue.setupItems", "Sonnet · high", 4) + T("queue.setupEscalated", 1, 0),
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the digest does not say %q:\n%s", want, body)
		}
	}
	printed := queueCommand(t, cfg, project, "status")
	if want := "  " + T("queue.escalatedOpen", "migrate the users table → Opus"); !strings.Contains(printed, want) {
		t.Fatalf("noctis queue status does not say %q:\n%s", want, printed)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeQueueFile(t, project, strings.Replace(string(content), "- [ ] migrate the users table", "- [x] migrate the users table", 1))
	if printed := queueCommand(t, cfg, project, "status"); !strings.Contains(printed, T("queue.setupItems", "Sonnet · high", 4)) || strings.Contains(printed, "→ Opus") {
		t.Fatalf("noctis queue status names an item that went up and is ticked in the file as one with a stronger model now:\n%s", printed)
	}
	writeQueueFile(t, project, string(content))
	var facts object
	if err := jsonUnmarshalObject([]byte(queueCommand(t, cfg, project, "status", "--json")), &facts); err != nil {
		t.Fatal(err)
	}
	models := getMap(facts, "models")
	if setups := getList(models, "setups"); len(setups) != 1 || getString(toObject(setups[0]), "setup") != "sonnet/high" {
		t.Fatalf("noctis queue status --json gives the setups as %v", models)
	}
	if escalated := getList(models, "escalated"); len(escalated) != 2 || getString(toObject(escalated[1]), "text") != "migrate the users table" || getString(toObject(escalated[1]), "outcome") != "" {
		t.Fatalf("noctis queue status --json gives the items that went up as %v", models)
	}
}

func TestTheDeepAgentRunsOnOpusAtMaxAndMayEdit(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(repoRoot(), "agents", deepAgentName+".md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, want := range []string{"\nname: " + deepAgentName + "\n", "\nmodel: opus\n", "\neffort: max\n"} {
		if !strings.Contains(text, want) {
			t.Fatalf("agents/deep.md does not say %q:\n%s", strings.TrimSpace(want), text)
		}
	}
	if strings.Contains(text, "disallowedTools") || strings.Contains(text, "\ntools:") {
		t.Fatalf("agents/deep.md limits the tools of the agent that finishes a stuck item:\n%s", text)
	}
}
