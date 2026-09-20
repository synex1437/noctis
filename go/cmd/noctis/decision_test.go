package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func usageFrom(fiveUsed, weekUsed float64, resetsAt float64) usageView {
	return usageView{
		hasAny:   true,
		fiveHour: &window{used: fiveUsed, resetsAt: resetsAt},
		sevenDay: &window{used: weekUsed, resetsAt: resetsAt + 86400},
	}
}

func testConfig() object {
	return object{
		"thresholds": object{"session5h": float64(92), "weeklyAll": float64(89), "weeklyFable": float64(95)},
		"budget":     object{},
	}
}

func TestEvaluateStopsAtTheThreshold(t *testing.T) {
	cfg := testConfig()
	cases := []struct {
		name          string
		five, week    float64
		wantWait      bool
		wantWindow    string
		wantWarnAtAll bool
	}{
		{"well below anything", 10, 20, false, "", false},
		{"inside the warn band but not at the wall", 88, 20, false, "", true},
		{"exactly at the five-hour threshold", 92, 20, true, "five_hour", false},
		{"past the five-hour threshold", 96, 20, true, "five_hour", false},
		{"weekly threshold with a calm five-hour", 10, 89, true, "seven_day", false},
		{"both blown picks the one that resets last", 95, 95, true, "seven_day", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := evaluate(cfg, usageFrom(testCase.five, testCase.week, 1000), "claude-opus-5", 0, false)
			if (result.wait != nil) != testCase.wantWait {
				t.Fatalf("wait = %v, want %v (five %v, week %v)", result.wait, testCase.wantWait, testCase.five, testCase.week)
			}
			if testCase.wantWait && result.wait.window != testCase.wantWindow {
				t.Fatalf("stopped on %q, want %q", result.wait.window, testCase.wantWindow)
			}
			if !testCase.wantWait && (result.warnWindow != nil) != testCase.wantWarnAtAll {
				t.Fatalf("warn = %v, want %v", result.warnWindow, testCase.wantWarnAtAll)
			}
		})
	}
}

func TestEvaluateTreatsADisabledThresholdAsOff(t *testing.T) {
	cfg := object{"thresholds": object{"session5h": nil, "weeklyAll": float64(89)}, "budget": object{}}
	result := evaluate(cfg, usageFrom(99, 10, 1000), "claude-opus-5", 0, false)
	if result.wait != nil {
		t.Fatalf("a disabled five-hour threshold still stopped the session: %v", result.wait)
	}
	if result.warnWindow != nil && result.warnWindow.window == "five_hour" {
		t.Fatalf("a disabled threshold still warned: %v", result.warnWindow)
	}
}

func TestEvaluateStopsEarlyWhenCompactionIsImminent(t *testing.T) {
	cfg := testConfig()
	calm := evaluate(cfg, usageFrom(90, 10, 1000), "claude-opus-5", 30, true)
	if calm.wait != nil {
		t.Fatalf("stopped at 90%% with a nearly empty context window: %v", calm.wait)
	}
	tight := evaluate(cfg, usageFrom(90, 10, 1000), "claude-opus-5", 95, true)
	if tight.wait == nil || tight.wait.hit != "compaction" {
		t.Fatalf("did not stop before an imminent compaction: %v", tight.wait)
	}
}

func TestEdgePollGetsStricterCloserToTheWall(t *testing.T) {
	cfg := testConfig()
	far := edgePollSeconds(cfg, usageFrom(50, 10, 1000))
	near := edgePollSeconds(cfg, usageFrom(90, 10, 1000))
	if near > far {
		t.Fatalf("data is allowed to be staler near the wall (%v s) than far from it (%v s)", near, far)
	}

	bursty := usageFrom(80, 10, 1000)
	bursty.fiveHour.burst = 6
	calm := usageFrom(80, 10, 1000)
	calm.fiveHour.burst = 0
	if edgePollSeconds(cfg, bursty) > edgePollSeconds(cfg, calm) {
		t.Fatalf("a bursty session is polled less often (%v s) than a calm one (%v s)",
			edgePollSeconds(cfg, bursty), edgePollSeconds(cfg, calm))
	}

	if got := edgePollSeconds(cfg, usageFrom(91, 10, 1000)); got > nearEdgePollClose {
		t.Fatalf("two points from the wall the poll interval is %v s, want at most %v s", got, nearEdgePollClose)
	}
}

func TestNearEdgeIsSymmetricAcrossWindows(t *testing.T) {
	cfg := testConfig()
	if nearEdge(cfg, usageFrom(50, 50, 1000)) {
		t.Fatal("a calm session counted as near the edge")
	}
	if !nearEdge(cfg, usageFrom(92-nearEdgeBand, 10, 1000)) {
		t.Fatal("the five-hour window did not count as near the edge at the band boundary")
	}
	if !nearEdge(cfg, usageFrom(10, 89-nearEdgeBand, 1000)) {
		t.Fatal("the weekly window did not count as near the edge at the band boundary")
	}
}

func TestDailyBudgetOnlyCountsTodayAndThisWeek(t *testing.T) {
	now := nowSec()
	cfg := object{"thresholds": object{}, "budget": object{"dailyWeeklyPercent": float64(10)}}
	usage := usageFrom(10, 42, 1000)

	if _, _, over := dailyBudgetStatus(cfg, object{}, usage, now); over {
		t.Fatal("reported an over-budget day before the day had a starting point")
	}

	state := object{"budgetDay": object{"day": localDay(now), "weekResetsAt": usage.sevenDay.resetsAt, "startUsed": float64(30)}}
	used, cap, over := dailyBudgetStatus(cfg, state, usage, now)
	if !over || math.Abs(used-12) > 0.001 || cap != 10 {
		t.Fatalf("used %v of %v, over = %v; want 12 of 10, over", used, cap, over)
	}

	stale := object{"budgetDay": object{"day": localDay(now), "weekResetsAt": usage.sevenDay.resetsAt - 999, "startUsed": float64(30)}}
	if _, _, over := dailyBudgetStatus(cfg, stale, usage, now); over {
		t.Fatal("counted usage against a starting point from a week that has already reset")
	}
}

func TestPlanNoticesSaysEachThingOnce(t *testing.T) {
	cfg := testConfig()
	empty := usageView{}
	result := decision{}

	notices, marks := planNotices(cfg, object{}, empty, &result, "s1", nowSec(), true)
	if len(notices) != 1 || len(marks) != 1 || marks[0] != "s1" {
		t.Fatalf("first time: notices %v, marks %v", notices, marks)
	}

	told := object{"notified": object{"s1": float64(nowSec())}}
	notices, marks = planNotices(cfg, told, empty, &result, "s1", nowSec(), true)
	if len(notices) != 0 || len(marks) != 0 {
		t.Fatalf("said it twice: notices %v, marks %v", notices, marks)
	}
}

func TestPlanNoticesDoesNotMarkWhatItDidNotSay(t *testing.T) {
	cfg := object{"thresholds": object{}, "budget": object{}, "configError": "bad json at line 3"}
	result := decision{}
	notices, marks := planNotices(cfg, object{}, usageView{}, &result, "s1", nowSec(), false)

	if len(notices) != 1 {
		t.Fatalf("expected only the config-error notice, got %v", notices)
	}
	for _, mark := range marks {
		if mark == "s1" {
			t.Fatal("marked the session as told about missing usage data on a host that never mentions it")
		}
	}
}

func TestPlanNoticesHardStopBecomesAWait(t *testing.T) {
	now := nowSec()
	usage := usageFrom(10, 42, 1000)
	cfg := object{"thresholds": object{}, "budget": object{"dailyWeeklyPercent": float64(10), "hardStop": true}}
	state := object{"budgetDay": object{"day": localDay(now), "weekResetsAt": usage.sevenDay.resetsAt, "startUsed": float64(30)}}

	result := decision{}
	notices, marks := planNotices(cfg, state, usage, &result, "s1", now, true)
	if result.wait == nil || result.wait.hit != "budget" {
		t.Fatalf("hardStop did not stop the session: %v", result.wait)
	}
	if result.wait.until <= float64(now) {
		t.Fatalf("the budget wait ends in the past: %v", result.wait.until)
	}
	if len(notices) != 0 {
		t.Fatalf("a hard stop should speak through the wait, not an extra notice: %v", notices)
	}
	if len(marks) != 1 {
		t.Fatalf("the budget stop was not recorded: %v", marks)
	}

	soft := object{"thresholds": object{}, "budget": object{"dailyWeeklyPercent": float64(10)}}
	softResult := decision{}
	notices, _ = planNotices(soft, state, usage, &softResult, "s1", now, true)
	if softResult.wait != nil {
		t.Fatalf("a soft budget stopped the session anyway: %v", softResult.wait)
	}
	if len(notices) != 1 {
		t.Fatalf("a soft budget said nothing: %v", notices)
	}
}

func TestUpdateStateWritesNothingWhenNothingChanged(t *testing.T) {
	sandboxFiles(t)

	stamp := float64(nowSec())
	updateState(func(state object) { stateMap(state, "notified")["s1"] = stamp })

	before, err := os.Stat(files.state)
	if err != nil {
		t.Fatalf("the first update did not create the state file: %v", err)
	}
	firstContent, _ := os.ReadFile(files.state)
	os.Remove(files.stateBackup)
	time.Sleep(10 * time.Millisecond)

	for i := 0; i < 5; i++ {
		updateState(func(state object) { stateMap(state, "notified")["s1"] = stamp })
	}

	after, err := os.Stat(files.state)
	if err != nil {
		t.Fatalf("state file disappeared: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("five no-op updates rewrote state.json (mtime %v → %v)", before.ModTime(), after.ModTime())
	}
	if _, err := os.Stat(files.stateBackup); err == nil {
		t.Fatal("a no-op update still wrote a backup copy")
	}
	nowContent, _ := os.ReadFile(files.state)
	if string(nowContent) != string(firstContent) {
		t.Fatal("a no-op update changed the contents")
	}
}

func TestUpdateStateWritesWhenSomethingChanged(t *testing.T) {
	sandboxFiles(t)
	stamp := float64(nowSec())
	updateState(func(state object) { stateMap(state, "notified")["s1"] = stamp })
	before, _ := os.Stat(files.state)
	time.Sleep(10 * time.Millisecond)

	updateState(func(state object) { stateMap(state, "notified")["s2"] = stamp })

	after, _ := os.Stat(files.state)
	if after.ModTime().Equal(before.ModTime()) {
		t.Fatal("a real change was elided along with the no-ops")
	}
	if numberOr(getMap(readState(), "notified"), "s2", 0) != stamp {
		t.Fatal("the change is not readable back")
	}
}

func TestHealTargetPrefersThePluginRoot(t *testing.T) {
	dir := sandboxFiles(t)
	previousRoot := files.pluginRoot
	t.Cleanup(func() { files.pluginRoot = previousRoot })

	files.pluginRoot = filepath.Join(dir, "absent")
	running, _ := os.Executable()
	if got := healTargetBinary(); got != running {
		t.Fatalf("with no plugin root the target was %q, want the running executable %q", got, running)
	}

	files.pluginRoot = filepath.Join(dir, "plugin")
	binDir := filepath.Join(files.pluginRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(binDir, binaryFileName())
	if err := os.WriteFile(installed, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := healTargetBinary(); got != installed {
		t.Fatalf("target was %q, want the plugin root copy %q", got, installed)
	}
}

func TestTornStateReadDoesNotRollBackToTheBackup(t *testing.T) {
	sandboxFiles(t)
	stamp := float64(nowSec())
	updateState(func(state object) { stateMap(state, "notified")["old"] = stamp })

	updateState(func(state object) { stateMap(state, "waits")["fresh"] = object{"resumeAt": stamp + 600} })

	whole, err := os.ReadFile(files.state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files.state, whole[:len(whole)/2], 0o600); err != nil {
		t.Fatal(err)
	}

	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = os.WriteFile(files.state, whole, 0o600)
	}()

	state, _ := readStoredState()
	if getMap(getMap(state, "waits"), "fresh") == nil {
		t.Fatal("a torn read rolled state back and lost a parked session")
	}
}

func TestRealCorruptionStillRecoversFromTheBackup(t *testing.T) {
	sandboxFiles(t)
	stamp := float64(nowSec())
	updateState(func(state object) { stateMap(state, "waits")["kept"] = object{"resumeAt": stamp + 600} })
	updateState(func(state object) { stateMap(state, "notified")["x"] = stamp })

	if err := os.WriteFile(files.state, []byte("{this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, _ := readStoredState()
	if getMap(getMap(state, "waits"), "kept") == nil {
		t.Fatal("real corruption was not recovered from the backup")
	}
	if restored := readJSONStrict(files.state); !restored.ok || restored.data == nil {
		t.Fatal("state.json was not repaired on disk")
	}
}

func TestQueueTagDependencyWaitsForEveryTaggedItem(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "TASKS.md")
	build := func(checked int) {
		lines := []string{"# release"}
		for i := 0; i < 3; i++ {
			box := " "
			if i < checked {
				box = "x"
			}
			lines = append(lines, "- ["+box+"] write test "+string(rune('1'+i))+" #tests")
		}
		lines = append(lines, "- [ ] ship the release (after #tests)", "")
		if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	eligible := func() bool {
		for _, item := range queueSnapshot(file).items {
			if strings.Contains(item, "ship the release") {
				return true
			}
		}
		return false
	}

	for _, checked := range []int{0, 1, 2} {
		build(checked)
		if eligible() {
			t.Fatalf("with %d of 3 tagged items checked the dependent item was already eligible", checked)
		}
	}
	build(3)
	if !eligible() {
		t.Fatal("with every tagged item checked the dependent item is still blocked")
	}
}

func TestQueueUnknownTagDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "TASKS.md")
	if err := os.WriteFile(file, []byte("- [ ] ship it (after #nothing-has-this-tag)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(queueSnapshot(file).items) != 1 {
		t.Fatal("an unknown tag blocked the item; a typo must not deadlock the queue")
	}
}

func TestChecklistPlainBulletIsNotGluedOntoTheItemAbove(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "TASKS.md")
	content := "- [ ] ship the release\n- some note about the release\n- [ ] write the changelog\n"
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, item := range queueSnapshot(file).items {
		if strings.Contains(item, "some note about the release") {
			t.Fatalf("a plain bullet was folded into a checklist item: %q", item)
		}
	}
}
