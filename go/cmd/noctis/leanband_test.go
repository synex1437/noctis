package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestTheLeanModuleKeepsTheGuardsCompactionBandAndBuiltinPausePoints(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(repoRoot(), "hooks", "lean.js"))
	if err != nil {
		t.Fatal(err)
	}
	band := regexp.MustCompile(`const COMPACTION_BAND = (\d+)`).FindStringSubmatch(string(source))
	points := regexp.MustCompile(`const BUILTIN_THRESHOLDS = Object\.freeze\(\{ session5h: (\d+), weeklyAll: (\d+) \}\)`).FindStringSubmatch(string(source))
	windows := regexp.MustCompile(`const WINDOW_THRESHOLDS = new Map\(\[\["five_hour", "session5h"\], \["seven_day", "weeklyAll"\]\]\)`).MatchString(string(source))
	if band == nil || points == nil || !windows {
		t.Fatal("hooks/lean.js no longer spells COMPACTION_BAND, BUILTIN_THRESHOLDS and WINDOW_THRESHOLDS the way this test reads them")
	}
	for name, pair := range map[string][2]string{
		"COMPACTION_BAND":              {band[1], fmt.Sprint(compactionBand)},
		"BUILTIN_THRESHOLDS.session5h": {points[1], fmt.Sprint(builtinThresholds["session5h"])},
		"BUILTIN_THRESHOLDS.weeklyAll": {points[2], fmt.Sprint(builtinThresholds["weeklyAll"])},
	} {
		if pair[0] != pair[1] {
			t.Errorf("hooks/lean.js has %s = %s, the guard %s", name, pair[0], pair[1])
		}
	}
}

func TestTheGuardPausesBeforeACompactionWhereTheLeanModulePutsItsEarlyCompactionOff(t *testing.T) {
	shipped := object{"session5h": float64(92), "weeklyAll": float64(89)}
	lower := object{"session5h": float64(90), "weeklyAll": float64(85)}
	cases := []struct {
		own, shipped any
		window       string
		used         float64
		pauses       bool
	}{
		{object{"session5h": float64(80)}, shipped, "five_hour", 74, true},
		{object{"session5h": float64(80)}, shipped, "five_hour", 73.9, false},
		{object{"session5h": " 80 "}, shipped, "five_hour", 74, true},
		{object{"session5h": "abc"}, shipped, "five_hour", 86, true},
		{object{"session5h": "abc"}, shipped, "five_hour", 85.9, false},
		{object{"session5h": "abc"}, nil, "five_hour", 86, true},
		{object{"session5h": float64(150)}, lower, "five_hour", 84, true},
		{object{"session5h": float64(150)}, lower, "five_hour", 83.9, false},
		{object{}, lower, "five_hour", 84, true},
		{"not an object", lower, "five_hour", 84, true},
		{nil, lower, "five_hour", 84, true},
		{nil, object{"session5h": float64(150)}, "five_hour", 86, true},
		{object{"session5h": float64(0)}, shipped, "five_hour", 99, false},
		{object{"session5h": false}, shipped, "five_hour", 99, false},
		{object{"session5h": nil}, shipped, "five_hour", 99, false},
		{object{"session5h": "0"}, shipped, "five_hour", 99, false},
		{nil, nil, "five_hour", 99, false},
		{"not an object", nil, "five_hour", 99, false},
		{object{"weeklyAll": float64(70)}, shipped, "seven_day", 64, true},
		{object{"weeklyAll": float64(70)}, shipped, "seven_day", 63.9, false},
	}
	for _, testCase := range cases {
		sandboxFiles(t)
		files.pluginRoot = t.TempDir()
		defaults := object{}
		if testCase.shipped != nil {
			defaults["thresholds"] = testCase.shipped
		}
		mustWriteJSON(filepath.Join(files.pluginRoot, "config.default.json"), defaults)
		if testCase.own != nil {
			mustWriteJSON(files.config, object{"thresholds": testCase.own})
		}
		usage := usageFrom(0, 0, float64(nowSec()+3600))
		if testCase.window == "five_hour" {
			usage.fiveHour.used = testCase.used
		} else {
			usage.sevenDay.used = testCase.used
		}
		result := evaluate(loadConfig(), usage, "claude-opus-5", 90, true)
		if paused := result.wait != nil; paused != testCase.pauses {
			t.Errorf("own %v, shipped %v, %s at %v: the guard paused %v, want %v (tests/lean.test.ts PAUSE_POINTS holds the same cases)", testCase.own, testCase.shipped, testCase.window, testCase.used, paused, testCase.pauses)
		}
	}
}
