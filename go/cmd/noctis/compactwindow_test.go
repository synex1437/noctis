package main

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func clearCompactionVariables(t *testing.T) {
	t.Helper()
	for _, name := range []string{autoCompactWindowVar, autoCompactPercentVar, "DISABLE_AUTO_COMPACT", "DISABLE_COMPACT"} {
		t.Setenv(name, "")
	}
}

func compactWindowIn(account string) (any, bool) {
	value, present := readJSON(filepath.Join(account, "settings.json"))["autoCompactWindow"]
	return value, present
}

func compactPercentIn(account string) (any, bool) {
	value, present := getMap(readJSON(filepath.Join(account, "settings.json")), "env")[autoCompactPercentVar]
	return value, present
}

func compactRecordIn(account, key string) (any, bool) {
	value, present := readJSON(filepath.Join(account, pluginName, "config.json"))[key]
	return value, present
}

func TestCompactAtTakesATokenCountAPercentOrOff(t *testing.T) {
	for _, flag := range []struct {
		value           any
		tokens, percent float64
		ok              bool
	}{
		{"280000", 280000, 0, true},
		{"280k", 280000, 0, true},
		{" 280K ", 280000, 0, true},
		{"0.28m", 280000, 0, true},
		{"280", 280000, 0, true},
		{float64(280000), 280000, 0, true},
		{float64(280), 280000, 0, true},
		{"600k", 600000, 0, true},
		{"140k", 140000, 0, true},
		{"100", 100000, 0, true},
		{"67000", 67000, 0, true},
		{"967000", 967000, 0, true},
		{"60%", 0, 60, true},
		{"60 %", 0, 60, true},
		{"60", 0, 60, true},
		{float64(60), 0, 60, true},
		{"62.5%", 0, 62.5, true},
		{"10%", 0, 10, true},
		{"100%", 0, 100, true},
		{"off", 0, 0, true},
		{"OFF", 0, 0, true},
		{"auto", 0, 0, true},
		{"false", 0, 0, true},
		{"0", 0, 0, true},
		{float64(0), 0, 0, true},
		{nil, 0, 0, true},
		{false, 0, 0, true},
		{"66999", 0, 0, false},
		{"967001", 0, 0, false},
		{"1m", 0, 0, false},
		{"1.5k", 0, 0, false},
		{"300000.5", 0, 0, false},
		{"9%", 0, 0, false},
		{"9", 0, 0, false},
		{"101%", 0, 0, false},
		{"-5", 0, 0, false},
		{"nan", 0, 0, false},
		{"inf", 0, 0, false},
		{"lots", 0, 0, false},
		{"", 0, 0, false},
		{true, 0, 0, false},
		{[]any{float64(280000)}, 0, 0, false},
	} {
		aim, ok := compactAtOf(flag.value)
		if ok != flag.ok || ok && (aim.tokens != flag.tokens || aim.percent != flag.percent) {
			t.Errorf("compactAt %#v gave %+v (valid %v), want tokens %v, percent %v (valid %v)", flag.value, aim, ok, flag.tokens, flag.percent, flag.ok)
		}
	}
	for _, aim := range []struct {
		aim  compactAim
		want any
	}{
		{compactAim{tokens: 280000}, float64(280000)},
		{compactAim{percent: 60}, "60%"},
		{compactAim{}, float64(0)},
	} {
		if got := aim.aim.setting(); got != aim.want {
			t.Errorf("%+v is stored as %#v, want %#v", aim.aim, got, aim.want)
		}
	}
}

func TestTheWindowVariableIsReadAsClaudeCodeReadsIt(t *testing.T) {
	for _, value := range []struct {
		text string
		want float64
	}{
		{"500000", 500000},
		{" 500000 ", 500000},
		{"+500000", 500000},
		{"5e5", 500000},
		{"5.5e5", 550000},
		{"500,000", 500000},
		{"500_000", 500000},
		{"500 000", 500000},
		{"500k", 500},
		{"1,0000", 1},
		{"-5", -5},
		{"5.55555e2", math.NaN()},
		{"big", math.NaN()},
		{"", math.NaN()},
	} {
		got := claudeInteger(value.text)
		if got != value.want && !(math.IsNaN(got) && math.IsNaN(value.want)) {
			t.Errorf("%q reads as %v, want %v", value.text, got, value.want)
		}
	}
	for _, value := range []struct {
		text   string
		window float64
		set    bool
	}{
		{"500000", 500000, true},
		{"50000", autoCompactWindowMin, true},
		{"5000000", autoCompactWindowMax, true},
		{"500k", autoCompactWindowMin, true},
		{"0", 0, false},
		{"-5", 0, false},
		{"big", 0, false},
	} {
		window, set := windowVariable(object{autoCompactWindowVar: value.text})
		if window != value.window || set != value.set {
			t.Errorf("%s=%q gives the window %v (set %v), want %v (set %v)", autoCompactWindowVar, value.text, window, set, value.window, value.set)
		}
	}
	for _, value := range []struct {
		text    string
		percent float64
		set     bool
	}{
		{"60", 60, true},
		{" 60", 60, true},
		{"60%", 60, true},
		{"62.5", 62.5, true},
		{"100", 100, true},
		{"0", 0, false},
		{"101", 101, false},
		{"abc", math.NaN(), false},
	} {
		percent, set := percentVariable(object{autoCompactPercentVar: value.text})
		if set != value.set || percent != value.percent && !(math.IsNaN(percent) && math.IsNaN(value.percent)) {
			t.Errorf("%s=%q gives %v (set %v), want %v (set %v)", autoCompactPercentVar, value.text, percent, set, value.percent, value.set)
		}
	}
}

func TestTheCompactionPointFollowsEveryChoiceClaudeCodeOffers(t *testing.T) {
	clearCompactionVariables(t)
	perModel := object{"autoCompactWindow": float64(313000), "modelSettings": object{"claude-opus-5-5": object{"autoCompactWindow": float64(533000)}}}
	autocompact := func(window any) object {
		return object{"autoCompactWindow": float64(313000), "modelSettings": object{"claude-opus-5-5": object{"autoCompactWindow": window}}}
	}
	efforts := object{"autoCompactWindow": float64(313000), "modelSettings": object{"claude-opus-5-5": object{"effortLevel": "xhigh"}, "claude-sonnet-5-5": object{"effortLevel": "high"}}}
	notTaken := object{"autoCompactWindow": float64(313000), "modelSettings": object{"claude-opus-5-5": object{"effortLevel": "xhigh", "autoCompactWindow": float64(50000)}}}
	for _, point := range []struct {
		name     string
		settings object
		model    string
		window   float64
		want     float64
		on       bool
	}{
		{"a 1M window by itself", nil, "", 1e6, 967000, true},
		{"a 200k window by itself", nil, "", 200000, 167000, true},
		{"noctis's 280k in a 1M window", object{"autoCompactWindow": float64(313000)}, "", 1e6, 280000, true},
		{"noctis's 280k in a 200k window", object{"autoCompactWindow": float64(313000)}, "", 200000, 167000, true},
		{"autoCompactWindow 633000 for 600k", object{"autoCompactWindow": float64(633000)}, "", 1e6, 600000, true},
		{"autoCompactWindow below Claude Code's range", object{"autoCompactWindow": float64(50000)}, "", 1e6, 967000, true},
		{"autoCompactWindow auto", object{"autoCompactWindow": "auto"}, "", 1e6, 967000, true},
		{"/autocompact 600k", autocompact(float64(600000)), "claude-opus-5-5[1m]", 1e6, 567000, true},
		{"/autocompact 900k", autocompact(float64(900000)), "claude-opus-5-5", 1e6, 867000, true},
		{"/autocompact 140k", autocompact(float64(140000)), "opus", 1e6, 107000, true},
		{"/autocompact auto", autocompact("auto"), "claude-opus-5-5", 1e6, 967000, true},
		{"the model's own window", perModel, "claude-opus-5-5[1m]", 1e6, 500000, true},
		{"the model's own window by its alias", perModel, "opus", 1e6, 500000, true},
		{"another model keeps the top-level one", perModel, "claude-sonnet-5-5", 1e6, 280000, true},
		{"the effort entries noctis keeps leave the top-level one", efforts, "claude-opus-5-5[1m]", 1e6, 280000, true},
		{"an own window Claude Code does not take leaves the top-level one", notTaken, "claude-opus-5-5[1m]", 1e6, 280000, true},
		{"the variable before autoCompactWindow", object{"autoCompactWindow": float64(313000), "env": object{autoCompactWindowVar: "500000"}}, "", 1e6, 467000, true},
		{"a small variable is raised to Claude Code's floor", object{"env": object{autoCompactWindowVar: "50000"}}, "", 1e6, 67000, true},
		{"a variable that is no number", object{"autoCompactWindow": float64(313000), "env": object{autoCompactWindowVar: "big"}}, "", 1e6, 280000, true},
		{"60% of a 1M window", object{"env": object{autoCompactPercentVar: "60"}}, "", 1e6, 588000, true},
		{"60% of a 200k window", object{"env": object{autoCompactPercentVar: "60"}}, "", 200000, 108000, true},
		{"60% beside the whole window, in a 1M window", object{"autoCompactWindow": percentWindow, "env": object{autoCompactPercentVar: "60"}}, "", 1e6, 588000, true},
		{"60% beside the whole window, in a 200k window", object{"autoCompactWindow": percentWindow, "env": object{autoCompactPercentVar: "60"}}, "", 200000, 108000, true},
		{"a percent that comes sooner than autoCompactWindow", object{"autoCompactWindow": float64(333000), "env": object{autoCompactPercentVar: "50"}}, "", 1e6, 156500, true},
		{"a percent that comes later than autoCompactWindow", object{"autoCompactWindow": float64(313000), "env": object{autoCompactPercentVar: "99"}}, "", 1e6, 280000, true},
		{"DISABLE_AUTO_COMPACT", object{"autoCompactWindow": float64(313000), "env": object{"DISABLE_AUTO_COMPACT": "1"}}, "", 1e6, 0, false},
		{"DISABLE_COMPACT", object{"env": object{"DISABLE_COMPACT": "true"}}, "", 1e6, 0, false},
		{"autoCompactEnabled false", object{"autoCompactEnabled": false}, "", 1e6, 0, false},
	} {
		got, on := compactionPoint(point.settings, point.model, point.window)
		if got != point.want || on != point.on {
			t.Errorf("%s: compaction point %v (on %v), want %v (on %v)", point.name, got, on, point.want, point.on)
		}
	}
}

func TestTheContextShareIsOfWhereClaudeCodeCompacts(t *testing.T) {
	clearCompactionVariables(t)
	for _, share := range []struct {
		name     string
		settings object
		fill     contextFill
		want     float64
	}{
		{"noctis's 280k", object{"autoCompactWindow": float64(313000)}, contextFill{percent: 25.2, tokens: 252000, window: 1e6, known: true}, 90},
		{"Claude Code's own", nil, contextFill{percent: 48.35, tokens: 483500, window: 1e6, known: true}, 50},
		{"60%", object{"env": object{autoCompactPercentVar: "60"}}, contextFill{percent: 29.4, tokens: 294000, window: 1e6, known: true}, 50},
		{"no token count", object{"autoCompactWindow": float64(313000)}, contextFill{percent: 72, known: true}, 72},
		{"compaction off", object{"env": object{"DISABLE_AUTO_COMPACT": "1"}}, contextFill{percent: 30, tokens: 300000, window: 1e6, known: true}, 30},
	} {
		got, known := pointShare(share.settings, "claude-opus-5-5", share.fill)
		if !known || math.Abs(got-share.want) > 1e-9 {
			t.Errorf("%s: share %v (known %v), want %v", share.name, got, known, share.want)
		}
	}
	if _, known := pointShare(nil, "", contextFill{}); known {
		t.Fatal("a session that reported no context has a share")
	}
}

func TestTheGuardWatchesTheTokenCompactionPointOfTheWindow(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	mustWriteJSON(files.settings, object{"autoCompactWindow": float64(333000)})
	cfg := loadConfig()
	for _, fill := range []struct {
		tokens, window float64
		near           bool
	}{
		{284999, 1e6, false},
		{285000, 1e6, true},
		{158649, 200000, false},
		{158650, 200000, true},
	} {
		session := contextFill{percent: 100 * fill.tokens / fill.window, tokens: fill.tokens, window: fill.window, known: true}
		if got := compactionNear(cfg, "claude-opus-5-5[1m]", session); got != fill.near {
			t.Errorf("%v tokens of a %v window: compaction near %v, want %v", fill.tokens, fill.window, got, fill.near)
		}
	}
	if !compactionNear(cfg, "", contextFill{percent: 85, known: true}) || compactionNear(cfg, "", contextFill{percent: 84, known: true}) {
		t.Fatal("a session that reported no token counts lost the percent guard")
	}
	if compactionNear(cfg, "", contextFill{percent: 2, tokens: 20000, window: 30000, known: true}) {
		t.Fatal("a window too small to hold a compaction point counts as near it")
	}
	if compactionNear(cfg, "", contextFill{}) {
		t.Fatal("a session whose fill is unknown counts as near the compaction point")
	}
	mustWriteJSON(files.settings, object{"autoCompactWindow": float64(333000), "env": object{"DISABLE_AUTO_COMPACT": "1"}})
	if compactionNear(cfg, "", contextFill{percent: 99, tokens: 990000, window: 1e6, known: true}) {
		t.Fatal("with Claude Code's compaction off the guard waits for a compaction that never comes")
	}
}

func TestAnOwnContextPercentKeepsThePercentGuard(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	mustWriteJSON(files.settings, object{"autoCompactWindow": float64(333000)})
	mustWriteJSON(files.config, object{"compaction": object{"contextPercent": float64(60)}})
	cfg := loadConfig()
	if compactionNear(cfg, "", contextFill{percent: 59, tokens: 590000, window: 1e6, known: true}) {
		t.Fatal("compaction.contextPercent 60 was overruled by the token point at 59%")
	}
	if !compactionNear(cfg, "", contextFill{percent: 60, tokens: 600000, window: 1e6, known: true}) {
		t.Fatal("compaction.contextPercent 60 did not hold the five-hour window at 60%")
	}
}

func TestEvaluatePausesTheFiveHourWindowBeforeATokenCompaction(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	mustWriteJSON(files.settings, object{"autoCompactWindow": float64(313000)})
	near := evaluateSession(testConfig(), usageFrom(90, 10, 1000), "claude-opus-5-5[1m]", contextFill{percent: 27, tokens: 270000, window: 1e6, known: true})
	if near.wait == nil || near.wait.hit != "compaction" || near.wait.window != "five_hour" {
		t.Fatalf("270k tokens of a 1M window that compacts at 280k did not pause before the compaction: %+v", near.wait)
	}
	far := evaluateSession(testConfig(), usageFrom(90, 10, 1000), "claude-opus-5-5[1m]", contextFill{percent: 20, tokens: 200000, window: 1e6, known: true})
	if far.wait != nil {
		t.Fatalf("200k tokens, well before the compaction point, paused the session: %+v", far.wait)
	}
}

func TestEnsureKeepsClaudeCodeAtTheCompactionPoint(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	t.Setenv("NOCTIS_LANG", "en")
	mustWriteJSON(files.settings, object{"model": "opus"})
	mustWriteJSON(files.config, object{})
	compactAt := func(value any) {
		t.Helper()
		config := readJSON(files.config)
		config["compaction"] = object{"compactAt": value}
		mustWriteJSON(files.config, config)
	}

	note := syncCompaction(loadConfig())

	if !strings.Contains(note, "from its next start, Claude Code compacts by ~280k tokens of context even in a 1M window (autoCompactWindow 313000 in settings.json, which it reads only as it starts)") {
		t.Fatalf("a first install did not say where Claude Code compacts: %q", note)
	}
	if settings := readJSON(files.settings); settings["autoCompactWindow"] != float64(313000) || settings["model"] != "opus" {
		t.Fatalf("ensure wrote %v", settings)
	}
	if record := readJSON(files.config)[managedWindowKey]; record != float64(313000) {
		t.Fatalf("ensure did not record the value for the undo: %v", record)
	}
	if note := syncCompaction(loadConfig()); note != "" {
		t.Fatalf("ensure spoke again with nothing to change: %q", note)
	}

	mustWriteJSON(files.settings, object{"model": "opus"})
	if note := syncCompaction(loadConfig()); note != "" {
		t.Fatalf("ensure spoke after the person removed the value: %q", note)
	}
	if _, present := readJSON(files.settings)["autoCompactWindow"]; present {
		t.Fatal("ensure wrote autoCompactWindow again after the person removed it")
	}

	compactAt("60%")
	note = syncCompaction(loadConfig())
	if !strings.Contains(note, "from its next start, Claude Code compacts at 60% of each model's whole window, for every model (CLAUDE_AUTOCOMPACT_PCT_OVERRIDE and autoCompactWindow 1000000 in settings.json)") || !strings.Contains(note, "~588k tokens of context in a 1M window, ~108k in a 200k one") {
		t.Fatalf("ensure did not move the point to 60%%: %q", note)
	}
	settings := readJSON(files.settings)
	if getMap(settings, "env")[autoCompactPercentVar] != "60" || settings["autoCompactWindow"] != percentWindow {
		t.Fatalf("compactAt 60%% left settings.json at %v", settings)
	}
	if config := readJSON(files.config); config[managedWindowKey] != percentWindow || config[managedPercentKey] != "60" {
		t.Fatalf("compactAt 60%% left the records at %v", config)
	}
	if note := syncCompaction(loadConfig()); note != "" {
		t.Fatalf("ensure spoke again with nothing to change: %q", note)
	}

	compactAt(float64(600000))
	note = syncCompaction(loadConfig())
	settings = readJSON(files.settings)
	if !strings.Contains(note, "~600k tokens") || settings["autoCompactWindow"] != float64(633000) || getMap(settings, "env")[autoCompactPercentVar] != nil {
		t.Fatalf("compactAt 600000 left settings.json at %v: %q", settings, note)
	}

	compactAt("off")
	syncCompaction(loadConfig())
	if _, present := readJSON(files.settings)["autoCompactWindow"]; present {
		t.Fatal("compactAt off left the autoCompactWindow ensure wrote")
	}
	if _, kept := readJSON(files.config)[managedWindowKey]; kept {
		t.Fatal("compactAt off kept an undo record for a value that is gone")
	}
}

func TestEnsureRecordsAndSaysNothingWhileSettingsCannotBeWritten(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	t.Setenv("NOCTIS_LANG", "en")
	mustWriteJSON(files.settings, object{"model": "opus"})
	mustWriteJSON(files.config, object{"compaction": object{"compactAt": "60%"}})
	syncCompaction(loadConfig())
	config := readJSON(files.config)
	config["compaction"] = object{"compactAt": float64(280000)}
	mustWriteJSON(files.config, config)
	blocked := fmt.Sprintf("%s.%d.tmp", files.settings, os.Getpid())
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}

	if note := syncCompaction(loadConfig()); note != "" {
		t.Fatalf("ensure said where Claude Code compacts though settings.json could not be written: %q", note)
	}
	if config := readJSON(files.config); config[managedPercentKey] != "60" || config[managedWindowKey] != percentWindow || config[takenPercentKey] != nil {
		t.Fatalf("the records no longer say what settings.json holds: %v", config)
	}

	// Once settings.json can be written, the next start makes the move: the percent is still noctis's own.
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	note := syncCompaction(loadConfig())
	settings := readJSON(files.settings)
	if !strings.Contains(note, "~280k tokens") || settings["autoCompactWindow"] != float64(313000) || getMap(settings, "env")[autoCompactPercentVar] != nil {
		t.Fatalf("the next start left settings.json at %v: %q", settings, note)
	}
}

func TestEnsureKeepsItsRecordWhileItCannotTakeItsWindowBack(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	mustWriteJSON(files.settings, object{"model": "opus"})
	mustWriteJSON(files.config, object{"compaction": object{"compactAt": float64(280000)}})
	syncCompaction(loadConfig())
	config := readJSON(files.config)
	config["compaction"] = object{"compactAt": "off"}
	mustWriteJSON(files.config, config)
	blocked := fmt.Sprintf("%s.%d.tmp", files.settings, os.Getpid())
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}

	syncCompaction(loadConfig())
	if config := readJSON(files.config); config[managedWindowKey] != float64(313000) {
		t.Fatalf("ensure forgot the window it wrote though settings.json still holds it: %v", config)
	}

	// Once settings.json can be written, the next start takes the window back: it is still noctis's own.
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	syncCompaction(loadConfig())
	if settings, config := readJSON(files.settings), readJSON(files.config); settings["autoCompactWindow"] != nil || config[managedWindowKey] != nil {
		t.Fatalf("the next start left settings.json at %v and config.json at %v", settings, config)
	}
}

func TestEnsureLeavesTheCompactionPointItWasNotAskedFor(t *testing.T) {
	for _, account := range []struct {
		name             string
		settings, config object
	}{
		{"a window of the person's own", object{"autoCompactWindow": float64(400000)}, object{}},
		{"the window variable", object{"env": object{autoCompactWindowVar: "500000"}}, object{}},
		{"compactAt off", object{}, object{"compaction": object{"compactAt": "off"}}},
		{"a percent of the person's own under compactAt off", object{"env": object{autoCompactPercentVar: "50"}}, object{"compaction": object{"compactAt": "off"}}},
		{"a percent and a window of the person's own", object{"autoCompactWindow": float64(400000), "env": object{autoCompactPercentVar: "50"}}, object{"compaction": object{"compactAt": "60%"}}},
		{"a percent of the person's own beside the window variable", object{"env": object{autoCompactPercentVar: "50", autoCompactWindowVar: "500000"}}, object{}},
	} {
		t.Run(account.name, func(t *testing.T) {
			leanSandbox(t)
			clearCompactionVariables(t)
			mustWriteJSON(files.settings, account.settings)
			mustWriteJSON(files.config, account.config)
			before := string(marshalCompact(readJSON(files.settings)))
			if note := syncCompaction(loadConfig()); note != "" {
				t.Fatalf("ensure said %q", note)
			}
			if after := string(marshalCompact(readJSON(files.settings))); after != before {
				t.Fatalf("settings.json went from %s to %s", before, after)
			}
			for _, key := range []string{managedWindowKey, managedPercentKey} {
				if _, recorded := readJSON(files.config)[key]; recorded {
					t.Fatalf("ensure recorded %s for a value it did not write", key)
				}
			}
		})
	}
}

func TestEnsureAddsThePercentAndKeepsAWindowOfThePersonsOwn(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	mustWriteJSON(files.settings, object{"autoCompactWindow": float64(400000)})
	mustWriteJSON(files.config, object{"compaction": object{"compactAt": "60%"}})

	syncCompaction(loadConfig())

	settings := readJSON(files.settings)
	if settings["autoCompactWindow"] != float64(400000) || getMap(settings, "env")[autoCompactPercentVar] != "60" {
		t.Fatalf("compactAt 60%% over the person's own window left settings.json at %v", settings)
	}
	if point, _ := compactionPoint(settings, "", 1e6); point != 228000 {
		t.Fatalf("60%% of the person's 400000 window puts the point at %v, want Claude Code's 228000", point)
	}
	config := readJSON(files.config)
	if _, recorded := config[managedWindowKey]; recorded || config[managedPercentKey] != "60" {
		t.Fatalf("the records are %v", config)
	}
}

func TestEnsureLeavesAnEnvThatIsNotAnObject(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	for _, env := range []any{"CLAUDE_CODE_EFFORT_LEVEL=max", []any{"CLAUDE_CODE_EFFORT_LEVEL=max"}} {
		mustWriteJSON(files.settings, object{"model": "opus", "env": env})
		mustWriteJSON(files.config, object{"compaction": object{"compactAt": "60%"}})
		before := string(marshalCompact(readJSON(files.settings)))

		if note := syncCompaction(loadConfig()); note != "" {
			t.Errorf("ensure said %q", note)
		}
		if after := string(marshalCompact(readJSON(files.settings))); after != before {
			t.Errorf("settings.json went from %s to %s", before, after)
		}
		if record, recorded := readJSON(files.config)[managedPercentKey]; recorded {
			t.Errorf("ensure recorded %v for a percent settings.json does not hold", record)
		}
	}
}

func TestAPercentOfThePersonsOwnDecidesInsteadOfTheTokenCount(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	t.Setenv("NOCTIS_LANG", "en")
	mustWriteJSON(files.settings, object{})
	mustWriteJSON(files.config, object{})
	syncCompaction(loadConfig())
	if window := readJSON(files.settings)["autoCompactWindow"]; window != float64(313000) {
		t.Fatalf("a first install wrote autoCompactWindow %v", window)
	}

	settings := readJSON(files.settings)
	settings["env"] = object{autoCompactPercentVar: "60"}
	mustWriteJSON(files.settings, settings)
	note := syncCompaction(loadConfig())

	if !strings.Contains(note, "your CLAUDE_AUTOCOMPACT_PCT_OVERRIDE=60 decides where Claude Code compacts, as a percent of each model's whole window: from Claude Code's next start, autoCompactWindow 1000000 in settings.json makes it hold for every model") {
		t.Fatalf("ensure did not say the person's percent decides: %q", note)
	}
	settings = readJSON(files.settings)
	if settings["autoCompactWindow"] != percentWindow || getMap(settings, "env")[autoCompactPercentVar] != "60" {
		t.Fatalf("beside the person's percent settings.json is %v", settings)
	}
	for _, window := range []struct{ size, point float64 }{{1e6, 588000}, {2e5, 108000}} {
		if point, _ := compactionPoint(settings, "", window.size); point != window.point {
			t.Fatalf("the person's 60%% puts the point at %v in a %v window, want %v", point, window.size, window.point)
		}
	}
	if record := readJSON(files.config)[managedWindowKey]; record != percentWindow {
		t.Fatalf("ensure recorded %v for the window it wrote", record)
	}
	if note := syncCompaction(loadConfig()); note != "" {
		t.Fatalf("ensure spoke again with nothing to change: %q", note)
	}

	// Once the person's percent is gone, compactAt decides again.
	settings = readJSON(files.settings)
	delete(settings, "env")
	mustWriteJSON(files.settings, settings)
	if note := syncCompaction(loadConfig()); !strings.Contains(note, "~280k tokens") || readJSON(files.settings)["autoCompactWindow"] != float64(313000) {
		t.Fatalf("without the person's percent ensure left settings.json at %v: %q", readJSON(files.settings), note)
	}

	// A percent set in the environment Claude Code runs in decides as well.
	t.Setenv(autoCompactPercentVar, "70")
	if note := syncCompaction(loadConfig()); !strings.Contains(note, "your CLAUDE_AUTOCOMPACT_PCT_OVERRIDE=70 decides") {
		t.Fatalf("ensure did not give way to the percent in the environment: %q", note)
	}
	if window := readJSON(files.settings)["autoCompactWindow"]; window != percentWindow {
		t.Fatalf("beside the percent in the environment autoCompactWindow is %v", window)
	}

	// The person takes the window away: ensure does not write it again, and the doctor says it is missing.
	t.Setenv(autoCompactPercentVar, "")
	mustWriteJSON(files.settings, object{"env": object{autoCompactPercentVar: "60"}})
	if note := syncCompaction(loadConfig()); note != "" {
		t.Fatalf("ensure spoke after the person took the window away: %q", note)
	}
	if _, present := readJSON(files.settings)["autoCompactWindow"]; present {
		t.Fatal("ensure wrote autoCompactWindow again after the person took it away")
	}
	if doctor := strings.Join(doctorLines(loadConfig()), "\n"); !strings.Contains(doctor, "!!  compaction point: compaction.compactAt is 280000, but settings.json has no autoCompactWindow") {
		t.Fatalf("the doctor does not say the window is missing:\n%s", doctor)
	}
}

// Claude Code compacts at CLAUDE_AUTOCOMPACT_PCT_OVERRIDE only for a model it has a window for: by itself it
// keeps none for some (Haiku 4.5, older models, a name a provider gives one), and compacts those only once
// the context is full. The whole window beside the percent gives every model one, and narrows none.
func TestEnsurePutsTheWholeWindowBesideAPercent(t *testing.T) {
	for _, account := range []struct {
		name             string
		settings, config object
		says             string
		managedPercent   any
	}{
		{"compactAt 60%", object{}, object{"compaction": object{"compactAt": "60%"}}, "compacts at 60% of each model's whole window, for every model", "60"},
		{"a percent of the person's own under compactAt 60%", object{"env": object{autoCompactPercentVar: "50"}}, object{"compaction": object{"compactAt": "60%"}}, "Claude Code takes CLAUDE_AUTOCOMPACT_PCT_OVERRIDE's percent of each model's whole window, for every model (autoCompactWindow 1000000 in settings.json)", nil},
		{"a percent of the person's own under a token count", object{"env": object{autoCompactPercentVar: "50"}}, object{}, "your CLAUDE_AUTOCOMPACT_PCT_OVERRIDE=50 decides where Claude Code compacts", nil},
	} {
		t.Run(account.name, func(t *testing.T) {
			leanSandbox(t)
			clearCompactionVariables(t)
			t.Setenv("NOCTIS_LANG", "en")
			mustWriteJSON(files.settings, account.settings)
			mustWriteJSON(files.config, account.config)

			if note := syncCompaction(loadConfig()); !strings.Contains(note, account.says) {
				t.Fatalf("ensure said %q", note)
			}
			settings, config := readJSON(files.settings), readJSON(files.config)
			if settings["autoCompactWindow"] != percentWindow || config[managedWindowKey] != percentWindow {
				t.Fatalf("settings.json is %v and the records %v", settings, config)
			}
			if config[managedPercentKey] != account.managedPercent {
				t.Fatalf("ensure recorded %v for the percent, want %v", config[managedPercentKey], account.managedPercent)
			}
			if note := syncCompaction(loadConfig()); note != "" {
				t.Fatalf("ensure spoke again with nothing to change: %q", note)
			}
			if doctor := strings.Join(doctorLines(loadConfig()), "\n"); strings.Contains(doctor, "!!  compaction point") {
				t.Fatalf("the doctor finds the compaction point missing:\n%s", doctor)
			}

			// compactAt off takes back the window and leaves the person's percent.
			config["compaction"] = object{"compactAt": "off"}
			mustWriteJSON(files.config, config)
			syncCompaction(loadConfig())
			settings, config = readJSON(files.settings), readJSON(files.config)
			if _, present := settings["autoCompactWindow"]; present || config[managedWindowKey] != nil || config[managedPercentKey] != nil {
				t.Fatalf("compactAt off left settings.json at %v and the records at %v", settings, config)
			}
			if want := getMap(account.settings, "env")[autoCompactPercentVar]; getMap(settings, "env")[autoCompactPercentVar] != want {
				t.Fatalf("compactAt off left %s at %v, want %v", autoCompactPercentVar, getMap(settings, "env")[autoCompactPercentVar], want)
			}
		})
	}
}

func TestTheWholeWindowFollowsAMoveBetweenAPercentAndATokenCount(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	t.Setenv("NOCTIS_LANG", "en")
	mustWriteJSON(files.settings, object{})
	mustWriteJSON(files.config, object{})
	compactAt := func(value any) {
		t.Helper()
		config := readJSON(files.config)
		config["compaction"] = object{"compactAt": value}
		mustWriteJSON(files.config, config)
	}
	holds := func(window, percent any) {
		t.Helper()
		settings := readJSON(files.settings)
		if settings["autoCompactWindow"] != window || getMap(settings, "env")[autoCompactPercentVar] != percent {
			t.Fatalf("settings.json is %v, want autoCompactWindow %v and %s %v", settings, window, autoCompactPercentVar, percent)
		}
	}

	for round := 0; round < 2; round++ {
		compactAt(float64(967000))
		syncCompaction(loadConfig())
		holds(float64(1000000), nil)
		compactAt("60%")
		if note := syncCompaction(loadConfig()); !strings.Contains(note, "compacts at 60% of each model's window (CLAUDE_AUTOCOMPACT_PCT_OVERRIDE in settings.json): by ~588k tokens") {
			t.Fatalf("a move from 967000 to 60%% said %q", note)
		}
		holds(percentWindow, "60")
		compactAt(float64(280000))
		syncCompaction(loadConfig())
		holds(float64(313000), nil)
		compactAt("75%")
		if note := syncCompaction(loadConfig()); !strings.Contains(note, "compacts at 75% of each model's whole window, for every model") {
			t.Fatalf("a move from 280000 to 75%% said %q", note)
		}
		holds(percentWindow, "75")
	}
}

func TestAPercentNoctisTookBackIsNotTakenForThePersonsOwn(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	t.Setenv("NOCTIS_LANG", "en")
	compactAt := func(value any) {
		t.Helper()
		config := readJSON(files.config)
		config["compaction"] = object{"compactAt": value}
		mustWriteJSON(files.config, config)
	}
	mustWriteJSON(files.settings, object{})
	mustWriteJSON(files.config, object{})
	compactAt("60%")
	syncCompaction(loadConfig())
	// The Claude Code that runs the hooks started with the percent ensure wrote, and keeps it in its
	// environment once the value leaves settings.json.
	t.Setenv(autoCompactPercentVar, "60")

	compactAt(float64(280000))
	if note := syncCompaction(loadConfig()); !strings.Contains(note, "~280k tokens") {
		t.Fatalf("a move from 60%% to 280000 did not say where Claude Code compacts: %q", note)
	}
	settings := readJSON(files.settings)
	if settings["autoCompactWindow"] != float64(313000) || getMap(settings, "env")[autoCompactPercentVar] != nil {
		t.Fatalf("a move from 60%% to 280000 left settings.json at %v", settings)
	}
	if taken := readJSON(files.config)[takenPercentKey]; taken != "60" {
		t.Fatalf("ensure did not keep the percent it took back: %v", taken)
	}
	for round := 0; round < 3; round++ {
		if note := syncCompaction(loadConfig()); note != "" {
			t.Fatalf("the percent ensure took back, still in the environment, was taken for the person's own: %q", note)
		}
	}
	if window := readJSON(files.settings)["autoCompactWindow"]; window != float64(313000) {
		t.Fatalf("autoCompactWindow went from 313000 to %v", window)
	}
	if doctor := strings.Join(doctorLines(loadConfig()), "\n"); strings.Contains(doctor, "!!  compaction point") {
		t.Fatalf("the doctor finds the compaction point missing:\n%s", doctor)
	}

	// Asked for again, the percent goes back into settings.json whatever the environment holds.
	compactAt("60%")
	syncCompaction(loadConfig())
	if value := getMap(readJSON(files.settings), "env")[autoCompactPercentVar]; value != "60" {
		t.Fatalf("compactAt 60%% left %s out of settings.json: %v", autoCompactPercentVar, value)
	}
	if _, kept := readJSON(files.config)[takenPercentKey]; kept {
		t.Fatal("ensure kept the record of a percent it wrote again")
	}
}

func TestOldEarlyCompactionSettingsMoveToEarlyAtPercent(t *testing.T) {
	leanSandbox(t)
	for _, old := range []struct {
		own  object
		want float64
	}{
		{object{"compactAtPercent": float64(70)}, 90},
		{object{"compactAtPercent": float64(0)}, 0},
		{object{"compactAtPercent": false}, 0},
		{object{"compactAtPercent": nil}, 0},
		{object{"compactAtPercent": float64(0), "earlyAtPercent": float64(80)}, 80},
		{object{"compactAtPercent": float64(75), "earlyAtPercent": float64(85)}, 85},
	} {
		mustWriteJSON(files.config, object{"compaction": old.own})
		cfg := loadConfig()
		if got := leanPolicyOf(cfg).earlyAt; got != old.want {
			t.Errorf("compaction=%v gave earlyAtPercent %v, want %v", old.own, got, old.want)
		}
		if _, kept := getMap(cfg, "compaction")["compactAtPercent"]; kept {
			t.Errorf("compaction=%v kept compactAtPercent in the loaded config", old.own)
		}
		if names := repairedCompaction(cfg); len(names) != 0 {
			t.Errorf("compaction=%v was taken for a mistake: repaired %v", old.own, names)
		}
		if _, _, err := mergeConfig(files.config, shippedDefaults(t)); err != nil {
			t.Fatal(err)
		}
		stored := getMap(readJSON(files.config), "compaction")
		if _, kept := stored["compactAtPercent"]; kept {
			t.Errorf("compaction=%v kept compactAtPercent in config.json after a merge", old.own)
		}
		if got, _ := getNumber(stored, "earlyAtPercent"); old.want == 0 && got != 0 {
			t.Errorf("compaction=%v turned early compaction back on in config.json: %v", old.own, stored)
		}
	}
}

func TestStatusAndDoctorSayWhereClaudeCodeCompacts(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	t.Setenv("NOCTIS_LANG", "en")
	doctorSays := func(want string) {
		t.Helper()
		if doctor := strings.Join(doctorLines(loadConfig()), "\n"); !strings.Contains(doctor, want) {
			t.Fatalf("the doctor does not say %q:\n%s", want, doctor)
		}
	}
	doctorLacks := func(unwanted string) {
		t.Helper()
		if doctor := strings.Join(doctorLines(loadConfig()), "\n"); strings.Contains(doctor, unwanted) {
			t.Fatalf("the doctor says %q:\n%s", unwanted, doctor)
		}
	}
	statusSays := func(want string) {
		t.Helper()
		if status := describeState(loadConfig(), readState(), usageView{}, nowSec()); !strings.Contains(status, want) {
			t.Fatalf("status does not say %q:\n%s", want, status)
		}
	}

	mustWriteJSON(files.settings, object{"autoCompactWindow": float64(313000)})
	statusSays("of the way to where Claude Code compacts · no compaction trimmed yet\n               Claude Code compacts by ~280k tokens of context in a 1M window, ~167k in a 200k one (autoCompactWindow 313000)\n")
	doctorSays("OK  compaction point: ~280k tokens of context in a 1M window, ~167k in a 200k one (autoCompactWindow 313000)")

	mustWriteJSON(files.settings, object{"env": object{autoCompactWindowVar: "500000"}})
	doctorSays("OK  compaction point: ~467k tokens of context in a 1M window, ~167k in a 200k one (CLAUDE_CODE_AUTO_COMPACT_WINDOW=500000)")
	mustWriteJSON(files.settings, object{"env": object{autoCompactWindowVar: "50k"}})
	doctorSays("(CLAUDE_CODE_AUTO_COMPACT_WINDOW=50k (100000))")

	mustWriteJSON(files.settings, object{"autoCompactWindow": float64(313000), "modelSettings": object{"claude-opus-5-5": object{"autoCompactWindow": float64(600000)}}})
	doctorSays("OK  compaction point: ~567k tokens of context in a 1M window, ~167k in a 200k one (modelSettings.claude-opus-5-5.autoCompactWindow 600000)")
	statusSays("Claude Code compacts by ~567k tokens of context in a 1M window, ~167k in a 200k one (modelSettings.claude-opus-5-5.autoCompactWindow 600000)")

	mustWriteJSON(files.settings, object{"autoCompactWindow": percentWindow, "env": object{autoCompactPercentVar: "60"}})
	doctorSays("OK  compaction point: ~588k tokens of context in a 1M window, ~108k in a 200k one (autoCompactWindow 1000000, CLAUDE_AUTOCOMPACT_PCT_OVERRIDE=60)")
	statusSays("Claude Code compacts by ~588k tokens of context in a 1M window, ~108k in a 200k one (autoCompactWindow 1000000, CLAUDE_AUTOCOMPACT_PCT_OVERRIDE=60)")
	// The person's percent decides in place of the token count compactAt asks for, with the whole window beside it.
	doctorLacks("!!  compaction point")
	mustWriteJSON(files.settings, object{"env": object{autoCompactPercentVar: "60"}})
	doctorSays("OK  compaction point: ~588k tokens of context in a 1M window, ~108k in a 200k one (CLAUDE_AUTOCOMPACT_PCT_OVERRIDE=60)")
	doctorSays("!!  compaction point: compaction.compactAt is 280000, but settings.json has no autoCompactWindow")

	mustWriteJSON(files.settings, object{"env": object{"DISABLE_AUTO_COMPACT": "1"}})
	doctorSays("OK  compaction point: none, Claude Code compacts no session by itself (DISABLE_AUTO_COMPACT)")
	statusSays("Claude Code compacts no session by itself (DISABLE_AUTO_COMPACT)")

	mustWriteJSON(files.settings, object{})
	doctorSays("!!  compaction point: compaction.compactAt is 280000, but settings.json has no autoCompactWindow")
	doctorSays("run /noctis:setup (it writes the setting)")

	mustWriteJSON(files.config, object{"compaction": object{"compactAt": "60%"}})
	doctorSays("!!  compaction point: compaction.compactAt is 60%, but settings.json has no CLAUDE_AUTOCOMPACT_PCT_OVERRIDE")
	mustWriteJSON(files.settings, object{"env": object{autoCompactPercentVar: "60"}})
	doctorSays("!!  compaction point: compaction.compactAt is 60%, but settings.json has no autoCompactWindow")
	mustWriteJSON(files.settings, object{"autoCompactWindow": percentWindow, "env": object{autoCompactPercentVar: "60"}})
	doctorLacks("!!  compaction point")
	mustWriteJSON(files.settings, object{})

	mustWriteJSON(files.config, object{"compaction": object{"compactAt": float64(0)}})
	doctorSays("OK  compaction point: Claude Code's own (compaction.compactAt is off)")
}

func TestAWindowOfAMillionTokensIsNamedInDigits(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	t.Setenv("NOCTIS_LANG", "en")
	previous := args
	t.Cleanup(func() { args = previous })
	args = parseArgs([]string{"setup"})
	mustWriteJSON(files.settings, object{"modelSettings": object{"claude-opus-5-5": object{"autoCompactWindow": float64(1000000)}}})

	for _, lines := range [][]string{compactWindowStatusLines(), compactWindowDoctorLines(loadConfig())} {
		if text := strings.Join(lines, "\n"); !strings.Contains(text, "(modelSettings.claude-opus-5-5.autoCompactWindow 1000000)") {
			t.Errorf("the window /autocompact saved is named as:\n%s", text)
		}
	}
	if note := wireCompaction(object{}, object{"autoCompactWindow": float64(1000000)}, object{}); !strings.Contains(note, "settings.json sets it to 1000000,") {
		t.Errorf("setup names the person's window as %q", note)
	}
}

func TestSetupMakesClaudeCodeCompactAtTheTokenCountAndUninstallTakesItBack(t *testing.T) {
	clearCompactionVariables(t)
	box := newCLIBox(t)

	run := box.run(t, "setup", "--config-dir", box.account, "--profile", "balanced", "--permissions", "keep")

	box.configured(t, run, box.account, "balanced")
	if value, _ := compactWindowIn(box.account); value != float64(313000) {
		t.Fatalf("setup left autoCompactWindow at %v, want 313000:\n%s", value, run)
	}
	if value, _ := compactRecordIn(box.account, managedWindowKey); value != float64(313000) {
		t.Fatalf("setup did not record the value it wrote for the undo: %v", value)
	}
	if !strings.Contains(run.stdout, "autoCompactWindow=313000: from its next start, Claude Code compacts by ~280k tokens of context even in a 1M window") {
		t.Fatalf("setup did not say where Claude Code compacts:\n%s", run)
	}
	// Claude Code reads the window only as it starts, so setup asks for a restart, not just a plugin reload.
	if !strings.Contains(run.stdout, "Restart Claude Code (/exit, then claude --continue)") {
		t.Fatalf("setup did not ask for the restart that makes Claude Code read the window:\n%s", run)
	}

	uninstall := box.run(t, "install", "--uninstall", "--config-dir", box.account, "--host", "claude")

	if uninstall.code != 0 {
		t.Fatalf("uninstall failed:\n%s", uninstall)
	}
	if value, present := compactWindowIn(box.account); present {
		t.Fatalf("uninstall left the autoCompactWindow setup wrote at %v:\n%s", value, uninstall)
	}
}

func TestSetupCompactAtMovesThePointAndOffTakesBackOnlyItsOwn(t *testing.T) {
	clearCompactionVariables(t)
	box := newCLIBox(t)
	compactAt := func() any {
		return getMap(readJSON(filepath.Join(box.account, pluginName, "config.json")), "compaction")["compactAt"]
	}

	moved := box.run(t, "setup", "--config-dir", box.account, "--profile", "balanced", "--permissions", "keep", "--compact-at", "500k")

	box.configured(t, moved, box.account, "balanced")
	if value, _ := compactWindowIn(box.account); value != float64(533000) {
		t.Fatalf("--compact-at 500k left autoCompactWindow at %v, want 533000:\n%s", value, moved)
	}
	if value := compactAt(); value != float64(500000) {
		t.Fatalf("--compact-at 500k left compaction.compactAt at %v", value)
	}

	percent := box.run(t, "setup", "--config-dir", box.account, "--profile", "balanced", "--permissions", "keep", "--compact-at", "60%")

	box.configured(t, percent, box.account, "balanced")
	if value, _ := compactWindowIn(box.account); value != percentWindow {
		t.Fatalf("--compact-at 60%% left autoCompactWindow at %v, want %v:\n%s", value, percentWindow, percent)
	}
	if value, _ := compactRecordIn(box.account, managedWindowKey); value != percentWindow {
		t.Fatalf("--compact-at 60%% did not record the window it wrote for the undo: %v", value)
	}
	if value, _ := compactPercentIn(box.account); value != "60" {
		t.Fatalf("--compact-at 60%% left %s at %v:\n%s", autoCompactPercentVar, value, percent)
	}
	if value, _ := compactRecordIn(box.account, managedPercentKey); value != "60" {
		t.Fatalf("--compact-at 60%% did not record the value it wrote for the undo: %v", value)
	}
	if value := compactAt(); value != "60%" {
		t.Fatalf("--compact-at 60%% left compaction.compactAt at %v", value)
	}
	if !strings.Contains(percent.stdout, "CLAUDE_AUTOCOMPACT_PCT_OVERRIDE=60: from its next start, Claude Code compacts at that percent of each model's window, by ~588k tokens of context in a 1M window and ~108k in a 200k one") {
		t.Fatalf("--compact-at 60%% did not say where Claude Code compacts:\n%s", percent)
	}
	if !strings.Contains(percent.stdout, "autoCompactWindow=1000000: the percent holds for every model, of its whole window") {
		t.Fatalf("--compact-at 60%% did not say why it wrote the whole window:\n%s", percent)
	}

	off := box.run(t, "setup", "--config-dir", box.account, "--profile", "balanced", "--permissions", "keep", "--compact-at", "off")

	box.configured(t, off, box.account, "balanced")
	if value, present := compactPercentIn(box.account); present {
		t.Fatalf("--compact-at off left the %s setup wrote at %v:\n%s", autoCompactPercentVar, value, off)
	}
	if value, present := compactWindowIn(box.account); present {
		t.Fatalf("--compact-at off left the autoCompactWindow setup wrote at %v:\n%s", value, off)
	}
	for _, key := range []string{managedWindowKey, managedPercentKey} {
		if value, present := compactRecordIn(box.account, key); present {
			t.Fatalf("--compact-at off kept the undo record %s for a value that is gone: %v", key, value)
		}
	}
	if !strings.Contains(off.stdout, "the compaction setting setup had written is removed") {
		t.Fatalf("--compact-at off did not say it took the value back:\n%s", off)
	}

	again := box.run(t, "setup", "--config-dir", box.account, "--profile", "balanced", "--permissions", "keep")

	box.configured(t, again, box.account, "balanced")
	if value, present := compactWindowIn(box.account); present {
		t.Fatalf("a later setup wrote autoCompactWindow %v again after --compact-at off:\n%s", value, again)
	}
	if value, present := compactPercentIn(box.account); present {
		t.Fatalf("a later setup wrote %s %v again after --compact-at off:\n%s", autoCompactPercentVar, value, again)
	}

	bad := box.run(t, "setup", "--config-dir", box.account, "--profile", "balanced", "--permissions", "keep", "--compact-at", "5%")
	if bad.code == 0 || !strings.Contains(bad.stdout+bad.stderr, `a percent of each model's window from 10 to 100 (such as 60%), or off, not "5%"`) {
		t.Fatalf("--compact-at 5%% was not refused with what it takes:\n%s", bad)
	}
}

func TestUninstallTakesBackThePercentSetupWrote(t *testing.T) {
	clearCompactionVariables(t)
	box := newCLIBox(t)
	run := box.run(t, "setup", "--config-dir", box.account, "--profile", "balanced", "--permissions", "keep", "--compact-at", "60")
	box.configured(t, run, box.account, "balanced")
	if value, _ := compactPercentIn(box.account); value != "60" {
		t.Fatalf("--compact-at 60 left %s at %v:\n%s", autoCompactPercentVar, value, run)
	}

	uninstall := box.run(t, "install", "--uninstall", "--config-dir", box.account, "--host", "claude")

	if uninstall.code != 0 {
		t.Fatalf("uninstall failed:\n%s", uninstall)
	}
	if value, present := compactPercentIn(box.account); present {
		t.Fatalf("uninstall left the %s setup wrote at %v:\n%s", autoCompactPercentVar, value, uninstall)
	}
}

func TestASetupThatCannotWriteSettingsLeavesTheRecordsAsTheyWere(t *testing.T) {
	sandboxFiles(t)
	clearCompactionVariables(t)
	recordClaudeWithAuto(t)
	account := recordAccount(t, object{"theme": "dark"})
	settingsFile := filepath.Join(account, "settings.json")
	configFile := filepath.Join(account, pluginName, "config.json")
	recordSetup(t, account, "high", "--permissions", "keep", "--compact-at", "60%")
	before := cliRead(t, configFile)
	blocked := fmt.Sprintf("%s.%d.tmp", settingsFile, os.Getpid())
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	previous := args
	t.Cleanup(func() { args = previous })
	args = parseArgs([]string{"setup", "--permissions", "keep", "--compact-at", "280k"})

	var err error
	capturedStdout(t, func() {
		err = wireSettings(account, filepath.Join(account, "bin", binaryFileName()), readJSON(configFile), configFile, shippedDefaults(t), false)
	})

	if err == nil {
		t.Fatal("setup reported success though settings.json could not be written")
	}
	cliUnchanged(t, configFile, before)
	// Run again once settings.json can be written, setup makes the move: the percent is still its own.
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	recordSetup(t, account, "high", "--permissions", "keep", "--compact-at", "280k")
	if settings := readJSON(settingsFile); settings["autoCompactWindow"] != float64(313000) || getMap(settings, "env")[autoCompactPercentVar] != nil {
		t.Fatalf("setup run again left settings.json at %v", settings)
	}
}

func TestSetupLeavesACompactionSettingItDidNotWrite(t *testing.T) {
	clearCompactionVariables(t)
	for _, own := range []struct {
		name, key, flag string
		value           any
		says            string
	}{
		{"autoCompactWindow", "autoCompactWindow", "280k", float64(400000), "autoCompactWindow left as it is: settings.json sets it to 400000"},
		{"the variable", "env", "280k", object{"CLAUDE_CODE_EFFORT_LEVEL": "medium", autoCompactWindowVar: "500000"}, "CLAUDE_CODE_AUTO_COMPACT_WINDOW=500000 decides when Claude Code compacts"},
	} {
		t.Run(own.name, func(t *testing.T) {
			box := newCLIBox(t)
			settings := readJSON(filepath.Join(box.account, "settings.json"))
			settings[own.key] = own.value
			cliWrite(t, filepath.Join(box.account, "settings.json"), marshalPretty(settings))
			window, _ := compactWindowIn(box.account)
			percent, _ := compactPercentIn(box.account)

			for _, argv := range [][]string{{"--compact-at", own.flag}, {"--compact-at", "off"}} {
				run := box.run(t, append([]string{"setup", "--config-dir", box.account, "--profile", "balanced", "--permissions", "keep"}, argv...)...)
				box.configured(t, run, box.account, "balanced")
				if value, _ := compactWindowIn(box.account); value != window {
					t.Fatalf("setup %v changed autoCompactWindow %v into %v:\n%s", argv, window, value, run)
				}
				if value, _ := compactPercentIn(box.account); value != percent {
					t.Fatalf("setup %v changed %s %v into %v:\n%s", argv, autoCompactPercentVar, percent, value, run)
				}
				for _, key := range []string{managedWindowKey, managedPercentKey} {
					if value, present := compactRecordIn(box.account, key); present {
						t.Fatalf("setup %v recorded %s for a value it did not write: %v", argv, key, value)
					}
				}
				if argv[1] != "off" && !strings.Contains(run.stdout, own.says) {
					t.Fatalf("setup did not say %q:\n%s", own.says, run)
				}
			}
		})
	}
}

func TestSetupWritesThePercentBesideTheWindowVariableAndSaysWhereItCompacts(t *testing.T) {
	clearCompactionVariables(t)
	box := newCLIBox(t)
	settingsFile := filepath.Join(box.account, "settings.json")
	settings := readJSON(settingsFile)
	settings["env"] = object{autoCompactWindowVar: "500000"}
	cliWrite(t, settingsFile, marshalPretty(settings))

	run := box.run(t, "setup", "--config-dir", box.account, "--profile", "balanced", "--permissions", "keep", "--compact-at", "60%")

	box.configured(t, run, box.account, "balanced")
	if value, present := compactWindowIn(box.account); present {
		t.Fatalf("setup wrote autoCompactWindow %v beside the window variable:\n%s", value, run)
	}
	if value, _ := compactPercentIn(box.account); value != "60" {
		t.Fatalf("setup left %s at %v:\n%s", autoCompactPercentVar, value, run)
	}
	if value, present := compactRecordIn(box.account, managedWindowKey); present {
		t.Fatalf("setup recorded %v for a window it did not write", value)
	}
	// 60% of the 500000 tokens the variable leaves a 1M window, less the summary's share.
	for _, says := range []string{"by ~288k tokens of context in a 1M window and ~108k in a 200k one", "CLAUDE_CODE_AUTO_COMPACT_WINDOW=500000 decides when Claude Code compacts"} {
		if !strings.Contains(run.stdout, says) {
			t.Fatalf("setup did not say %q:\n%s", says, run)
		}
	}
}

func TestSetupPutsTheWholeWindowBesideAPercentOfThePersonsOwn(t *testing.T) {
	clearCompactionVariables(t)
	for _, flag := range []struct{ value, says string }{
		{"280k", "autoCompactWindow=1000000: your CLAUDE_AUTOCOMPACT_PCT_OVERRIDE=50 decides when Claude Code compacts, as a percent of each model's whole window, for every model"},
		{"60%", "CLAUDE_AUTOCOMPACT_PCT_OVERRIDE left as it is: it is 50 already, and setup leaves your value\n   autoCompactWindow=1000000: the percent holds for every model, of its whole window"},
	} {
		t.Run(flag.value, func(t *testing.T) {
			box := newCLIBox(t)
			first := box.run(t, "setup", "--config-dir", box.account, "--profile", "balanced", "--permissions", "keep")
			box.configured(t, first, box.account, "balanced")
			if value, _ := compactWindowIn(box.account); value != float64(313000) {
				t.Fatalf("setup left autoCompactWindow at %v:\n%s", value, first)
			}
			settingsFile := filepath.Join(box.account, "settings.json")
			settings := readJSON(settingsFile)
			env := getMap(settings, "env")
			if env == nil {
				env = object{}
			}
			env[autoCompactPercentVar] = "50"
			settings["env"] = env
			cliWrite(t, settingsFile, marshalPretty(settings))

			again := box.run(t, "setup", "--config-dir", box.account, "--profile", "balanced", "--permissions", "keep", "--compact-at", flag.value)

			box.configured(t, again, box.account, "balanced")
			if value, _ := compactWindowIn(box.account); value != percentWindow {
				t.Fatalf("setup left autoCompactWindow at %v beside the person's percent:\n%s", value, again)
			}
			if value, _ := compactRecordIn(box.account, managedWindowKey); value != percentWindow {
				t.Fatalf("setup recorded %v for the window it wrote", value)
			}
			if value, present := compactRecordIn(box.account, managedPercentKey); present {
				t.Fatalf("setup recorded %v for the person's percent", value)
			}
			if !strings.Contains(again.stdout, flag.says) {
				t.Fatalf("setup did not say %q:\n%s", flag.says, again)
			}

			uninstall := box.run(t, "install", "--uninstall", "--config-dir", box.account, "--host", "claude")

			if uninstall.code != 0 {
				t.Fatalf("uninstall failed:\n%s", uninstall)
			}
			if value, present := compactWindowIn(box.account); present {
				t.Fatalf("uninstall left the autoCompactWindow setup wrote at %v:\n%s", value, uninstall)
			}
			if value, _ := compactPercentIn(box.account); value != "50" {
				t.Fatalf("uninstall took away the person's %s: %v\n%s", autoCompactPercentVar, value, uninstall)
			}
		})
	}
}

func TestTheDocsSayWhereSetupMakesClaudeCodeCompactAndHowToTakeItBack(t *testing.T) {
	tokens := builtinLean["compactAt"].(float64)
	window := formatNumber(compactWindowFor(tokens))
	low, high := autoCompactWindowMin-compactWindowFor(0), autoCompactWindowMax-compactWindowFor(0)
	for doc, says := range map[string][]string{
		"docs/GUIDE.md":         {"Set to `" + window + "`", "`\"autoCompactWindow\": " + window + "`", "`--compact-at off`", "`--compact-at 60%`", approxCount(low) + " to " + approxCount(high), "set yourself", "/autocompact"},
		"docs/GUIDE.tr.md":      {"`" + window + "` yapılır", "`\"autoCompactWindow\": " + window + "`", "`--compact-at off`", "`--compact-at 60%`", formatNumber(low/1000) + " binden " + formatNumber(high/1000) + " bin", "kendi koyduğunuz", "/autocompact"},
		"docs/REFERENCE.md":     {"| `compaction.compactAt` | " + formatNumber(tokens) + " |", "(" + window + ")", "`--compact-at <tokens>\\|<percent>%\\|off`", "from " + formatNumber(low) + " to " + formatNumber(high), "`managedAutoCompactWindow`", "`managedAutoCompactPercent`", "`" + takenPercentKey + "`", "| `" + autoCompactWindowVar + "` |", "| `" + autoCompactPercentVar + "` |", "| `compaction.earlyAtPercent` | 90 |"},
		"skills/setup/SKILL.md": {"`autoCompactWindow` → `" + window + "`", "`--compact-at <tokens>|<percent>%|off`", "from " + formatNumber(low) + " to " + formatNumber(high), "one the user set stays"},
	} {
		text := docsFactsFile(t, doc)
		for _, want := range says {
			if !strings.Contains(text, want) {
				t.Errorf("%s does not say %q", doc, want)
			}
		}
	}
}

func TestTheStepsToUndoSetupByHandNameBothCompactionSettings(t *testing.T) {
	for lang, table := range catalogTable() {
		for _, setting := range []string{"autoCompactWindow", "env." + autoCompactPercentVar} {
			if !strings.Contains(table["install.undoByHand"], setting) {
				t.Errorf("%s install.undoByHand leaves out %s: %s", lang, setting, table["install.undoByHand"])
			}
		}
	}
}

func TestAConfigMergeWithNothingToAddLeavesConfigJSONAlone(t *testing.T) {
	leanSandbox(t)
	defaults := shippedDefaults(t)
	if _, err := mergeConfigLocked(defaults); err != nil {
		t.Fatal(err)
	}
	// Were config.json written again, the write would fail here.
	if err := os.MkdirAll(fmt.Sprintf("%s.%d.tmp", files.config, os.Getpid()), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := mergeConfigLocked(defaults); err != nil {
		t.Fatalf("a session start wrote config.json again though the merge added nothing: %v", err)
	}
}

func TestASessionStartMergesConfigJSONOnlyUnderTheLockTheCompactionRecordIsWrittenUnder(t *testing.T) {
	leanSandbox(t)
	mustWriteJSON(files.config, object{"compaction": object{"compactAt": float64(280000)}})
	before, _ := os.ReadFile(files.config)
	// Another session holds the lock while it records the window it wrote to settings.json.
	if err := os.WriteFile(files.settingsLock, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		_, err := mergeConfigLocked(shippedDefaults(t))
		finished <- err
	}()
	time.Sleep(300 * time.Millisecond)
	if now, _ := os.ReadFile(files.config); !bytes.Equal(now, before) {
		t.Errorf("config.json was merged while another session held the lock:\n%s", now)
	}
	config := readJSON(files.config)
	config[managedWindowKey] = float64(313000)
	mustWriteJSON(files.config, config)
	if err := os.Remove(files.settingsLock); err != nil {
		t.Fatal(err)
	}

	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if config := readJSON(files.config); config[managedWindowKey] != float64(313000) || getMap(config, "wait") == nil {
		t.Fatalf("the merge lost the record the other session wrote, or merged nothing: %v", config)
	}
}

func TestClaudeCodesGlobalConfigIsReadAgainOnlyOnceItChanges(t *testing.T) {
	dir := sandboxFiles(t)
	clearCompactionVariables(t)
	t.Setenv(claudeConfigEnv, dir)
	t.Setenv("CLAUDE_CODE_CUSTOM_OAUTH_URL", "")
	forget := func() {
		compactionSwitch.Lock()
		compactionSwitch.seen = ""
		compactionSwitch.Unlock()
	}
	forget()
	t.Cleanup(forget)
	global := filepath.Join(dir, ".claude.json")
	// A global config of a megabyte, as a long-used account has; "false" and "true " are as long.
	write := func(file, enabled string, at time.Time) {
		t.Helper()
		content := `{"projects":{"/p":{"note":"` + strings.Repeat("x", 1<<20) + `"}},"autoCompactEnabled":` + enabled + `}`
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(file, at, at); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Now().Add(-time.Hour).Truncate(time.Second)
	write(global, "false", at)
	if !globalCompactionOff() {
		t.Fatal("autoCompactEnabled false in Claude Code's global config does not turn its compaction off")
	}
	if point, on := compactionPoint(nil, "", 1e6); on || point != 0 {
		t.Fatalf("with compaction turned off in the global config, the point is %v (on %v)", point, on)
	}
	if why, off := autoCompactOff(object{"autoCompactEnabled": true}); off {
		t.Fatalf("the global config decided over the switch in settings.json: %s", why)
	}

	write(global, "true ", at)
	if !globalCompactionOff() {
		t.Fatal("the global config was read again in the same process though its size and modification time are the same")
	}
	forget()
	if !globalCompactionOff() {
		t.Fatal("the next process read the global config again though its size and modification time are the same")
	}

	write(global, "true ", at.Add(time.Second))
	if globalCompactionOff() {
		t.Fatal("a global config modified since was not read again")
	}
	forget()
	if globalCompactionOff() {
		t.Fatal("the next process kept the answer from before the global config was modified")
	}

	legacy := filepath.Join(dir, ".config.json")
	write(legacy, "false", at)
	if !globalCompactionOff() {
		t.Fatal("a legacy .config.json in the account's directory is not read before .claude.json")
	}
	if err := os.Remove(legacy); err != nil {
		t.Fatal(err)
	}
	if globalCompactionOff() {
		t.Fatal("the answer for .config.json still counts once it is gone")
	}

	// A write in the same instant as the one read leaves the size and modification time as they were, so
	// no answer is kept for a modification time not yet two seconds past (here one ahead of the clock, which
	// no slow runner lets pass).
	just := time.Now().Add(time.Minute)
	write(global, "false", just)
	if !globalCompactionOff() {
		t.Fatal("a global config modified a moment ago is not read")
	}
	write(global, "true ", just)
	if globalCompactionOff() {
		t.Fatal("the answer for a global config modified a moment ago was kept in this process")
	}
	write(global, "false", just)
	globalCompactionOff()
	write(global, "true ", just)
	forget()
	if globalCompactionOff() {
		t.Fatal("the answer for a global config modified a moment ago was kept for the next process")
	}

	files.guardDir = filepath.Join(dir, "gone")
	forget()
	write(global, "false", at.Add(2*time.Second))
	if !globalCompactionOff() {
		t.Fatal("without noctis's folder, the global config is not read")
	}
	if _, err := os.Stat(files.guardDir); !os.IsNotExist(err) {
		t.Fatalf("keeping the answer made noctis's folder: %v", err)
	}
}
