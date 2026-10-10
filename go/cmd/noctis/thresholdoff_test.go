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
		mustWriteJSON(files.settings, object{"model": "claude-opus-5-5"})
		updateState(func(state object) { state["modelSwitched"] = switched["modelSwitched"] })
		if text := maybeRevertDefaultModel(cfg, readState(), usageView{hasAny: true, fable: &window{used: 10, resetsAt: float64(now + 7*86400)}}, now); text == "" || settingsModel() != "claude-fable-5-1" {
			t.Fatalf("weeklyFable=%s: a model switch whose Fable window has reset was never reverted (settings.json model %q)", name, settingsModel())
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

func statusThresholdsLine(t *testing.T, thresholds object, label string) string {
	t.Helper()
	mustWriteJSON(files.config, object{"thresholds": thresholds})
	for _, line := range strings.Split(describeState(loadConfig(), object{}, usageView{}, nowSec()), "\n") {
		if strings.HasPrefix(line, label) {
			return line
		}
	}
	t.Fatalf("status has no %s line", label)
	return ""
}

func TestStatusShowsASwitchedOffThresholdAsOff(t *testing.T) {
	dir := sandboxFiles(t)
	files.pluginRoot = dir
	mustWriteJSON(filepath.Join(dir, "config.default.json"), shippedDefaults(t))
	previous := locale
	t.Cleanup(func() { locale = previous })
	locale = "en"
	for _, off := range []any{nil, float64(0), false} {
		for _, tc := range []struct{ key, want string }{
			{"session5h", "Thresholds   : 5h off · weekly ≥97% · Fable ≥97%"},
			{"weeklyAll", "Thresholds   : 5h ≥92% · weekly off · Fable ≥97%"},
			{"weeklyFable", "Thresholds   : 5h ≥92% · weekly ≥97% · Fable off"},
			{"weeklyScoped", "Thresholds   : 5h ≥92% · weekly ≥97% · Fable off"},
		} {
			if line := statusThresholdsLine(t, object{tc.key: off}, "Thresholds"); line != tc.want {
				t.Errorf("thresholds.%s = %v switches that window off, but status shows\n  %q\nwant\n  %q", tc.key, off, line, tc.want)
			}
		}
	}
	for _, tc := range []struct {
		thresholds object
		want       string
	}{
		{object{}, "Thresholds   : 5h ≥92% · weekly ≥97% · Fable ≥97%"},
		{object{"session5h": "90%", "weeklyAll": float64(88.5)}, "Thresholds   : 5h ≥92% · weekly ≥88.5% · Fable ≥97%"},
	} {
		if line := statusThresholdsLine(t, tc.thresholds, "Thresholds"); line != tc.want {
			t.Errorf("thresholds %v: status shows\n  %q\nwant\n  %q", tc.thresholds, line, tc.want)
		}
	}
	locale = "tr"
	if line, want := statusThresholdsLine(t, object{"session5h": false}, "Eşikler"), "Eşikler      : 5sa kapalı · hafta ≥%97 · Fable ≥%97"; line != want {
		t.Errorf("in Turkish a switched-off 5-hour threshold shows\n  %q\nwant\n  %q", line, want)
	}
}
