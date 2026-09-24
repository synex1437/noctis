package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestAThresholdSwitchedOffIsOffEverywhereItIsRead(t *testing.T) {
	dir := sandboxFiles(t)
	files.pluginRoot = dir
	mustWriteJSON(filepath.Join(dir, "config.default.json"), shippedDefaults(t))
	now := nowSec()
	for _, off := range []any{nil, float64(0), false} {
		name := fmt.Sprint(off)
		mustWriteJSON(files.config, object{"thresholds": object{"session5h": off, "weeklyAll": off, "weeklyFable": off}})
		cfg := loadConfig()

		usage := usageView{hasAny: true, fable: &window{used: 40, resetsAt: float64(now + 3*86400)}}
		if scopedQuotaOut(cfg, object{}, usage, now) {
			t.Fatalf("weeklyFable=%s: with the Fable guard off, a Fable window at 40%% was taken for an exhausted quota and work moved to the fallback model", name)
		}
		if _, guarded := scopedThresholdEnabled(cfg); guarded {
			t.Fatalf("weeklyFable=%s counts as guarded", name)
		}

		week := &window{used: 40, resetsAt: float64(now + 3*86400 + 43200)}
		if marker := paceMarker(cfg, week, now); marker != "▲" {
			t.Fatalf("weeklyAll=%s: 40%% at the middle of the week shows pace %q; with the guard off the pace is toward the limit (100%%), so it is below pace", name, marker)
		}
		badge := usageBadgeAt(usageView{hasAny: true, fiveHour: &window{used: 97, resetsAt: float64(now + 3600)}, sevenDay: &window{used: 95, resetsAt: float64(now + 86400)}}, cfg, now)
		if strings.Contains(badge, "⚠") {
			t.Fatalf("session5h/weeklyAll=%s: the status badge warns about windows whose guard is off: %s", name, badge)
		}

		switched := object{"modelSwitched": object{"from": "claude-fable-5-1", "to": "claude-opus-5-5", "fableResetsAt": float64(now - 60)}}
		updateState(func(state object) { state["modelSwitched"] = switched["modelSwitched"] })
		if text := maybeRevertDefaultModel(cfg, readState(), usageView{hasAny: true, fable: &window{used: 10, resetsAt: float64(now + 7*86400)}}, now); text == "" {
			t.Fatalf("weeklyFable=%s: a model switch whose Fable window has reset was never reverted", name)
		}

		for _, snapshot := range []usageView{
			{hasAny: true, fiveHour: &window{used: 91, resetsAt: float64(now + 3600)}, sevenDay: &window{used: 88, resetsAt: float64(now + 86400)}},
			{hasAny: true, sevenDay: &window{used: 88, resetsAt: float64(now + 86400)}},
		} {
			if key, _, guarded := blindWindow(cfg, snapshot); guarded {
				t.Fatalf("session5h/weeklyAll=%s: a pause for lack of data was attributed to %s, whose guard is off", name, key)
			}
		}
	}

	mustWriteJSON(files.config, object{"thresholds": object{"session5h": false, "weeklyAll": float64(89)}})
	cfg := loadConfig()
	snapshot := usageView{hasAny: true, fiveHour: &window{used: 91, resetsAt: float64(now + 3600)}, sevenDay: &window{used: 88, resetsAt: float64(now + 86400)}}
	if key, threshold, guarded := blindWindow(cfg, snapshot); key != "seven_day" || threshold != 89 || !guarded {
		t.Fatalf("with the five-hour guard off a pause for lack of data must be the weekly window's at 89, got %s at %v (guarded %t)", key, threshold, guarded)
	}
}
