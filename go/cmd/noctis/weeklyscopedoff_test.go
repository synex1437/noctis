package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"
)

func TestWeeklyScopedSwitchedOffSwitchesTheScopedGuardOff(t *testing.T) {
	dir := sandboxFiles(t)
	files.pluginRoot = dir
	mustWriteJSON(filepath.Join(dir, "config.default.json"), shippedDefaults(t))
	now := nowSec()
	fable := usageView{hasAny: true, fable: &window{used: 98, resetsAt: float64(now + 3*86400)}}
	for _, off := range []any{nil, float64(0), false} {
		name := fmt.Sprint(off)
		mustWriteJSON(files.config, object{"thresholds": object{"weeklyScoped": off}})
		cfg := loadConfig()
		if limit, guarded := scopedThresholdEnabled(cfg); guarded {
			t.Errorf("weeklyScoped=%s: the scoped guard stays on, at weeklyFable's %v", name, limit)
		}
		if result := evaluate(cfg, fable, "claude-fable-5-1", 0, false); result.fableHit || result.wait != nil {
			t.Errorf("weeklyScoped=%s: a Fable session at 98%% was still switched or paused (fableHit %t, wait %+v)", name, result.fableHit, result.wait)
		}
		if scopedQuotaOut(cfg, object{}, fable, now) {
			t.Errorf("weeklyScoped=%s: with the scoped guard off, a Fable window at 98%% was taken for an exhausted quota", name)
		}
		if !slices.Contains(unguardedWindows(cfg), "weeklyFable") {
			t.Errorf("weeklyScoped=%s: status and doctor do not point out the unguarded scoped window: %v", name, unguardedWindows(cfg))
		}
	}
	for _, set := range []struct {
		thresholds object
		want       float64
	}{
		{object{"weeklyScoped": float64(90)}, 90},
		{object{"weeklyFable": float64(93)}, 93},
		{object{}, 97},
	} {
		mustWriteJSON(files.config, object{"thresholds": set.thresholds})
		if limit, guarded := scopedThresholdEnabled(loadConfig()); !guarded || limit != set.want {
			t.Errorf("thresholds %v: scoped guard at %v (guarded %t), want %v", set.thresholds, limit, guarded, set.want)
		}
	}
}
