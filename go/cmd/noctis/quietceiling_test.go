package main

import (
	"path/filepath"
	"testing"
)

type e1QuietUsage struct{ fiveResetsAt, weekResetsAt float64 }

func e1QuietCeilingSandbox(t *testing.T, config object) (object, e1QuietUsage) {
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
	mustWriteJSON(files.config, config)
	now := float64(nowSec())
	return loadConfig(), e1QuietUsage{fiveResetsAt: now + 4*3600, weekResetsAt: now + 3*86400}
}

func (usage e1QuietUsage) write(fiveUsed float64) {
	mustWriteJSON(files.usage, object{
		"updatedAt": float64(nowSec()),
		"five_hour": object{"used": fiveUsed, "resetsAt": usage.fiveResetsAt},
		"seven_day": object{"used": float64(10), "resetsAt": usage.weekResetsAt},
	})
}

func TestTheQuietPathStopsAtThePaidCreditCeiling(t *testing.T) {
	cases := []struct {
		name          string
		config        object
		before, after float64
	}{
		{"the five-hour threshold is off and the window is at 98%", object{"thresholds": object{"session5h": nil}}, 98, 100},
		{"a ceiling of 60 below the five-hour threshold of 92", object{"credits": object{"ceiling": float64(60)}}, 59, 61},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, usage := e1QuietCeilingSandbox(t, c.config)
			usage.write(c.before)
			batch := agentHookInput("PostToolBatch", "e1-quiet", t.TempDir(), object{})
			if output := hookOutput(t, onPostToolBatch, batch, cfg); stoppedBy(output) {
				t.Fatalf("a batch at %v%%, below the ceiling, stopped: %v", c.before, output)
			}
			usage.write(c.after)
			if quietFastPath(batch) {
				t.Fatalf("at %v%%, past the paid-credit ceiling, the next batch took the quiet path and skipped the decision (marker %v)", c.after, readJSON(quietMarkerPath("e1-quiet")))
			}
			result := decide(cfg, readState(), batch, nowSec(), decideOptions{})
			if result.wait == nil || result.wait.hit != "ceiling" {
				t.Fatalf("the full decision at %v%% did not stop at the ceiling: %+v", c.after, result.wait)
			}
		})
	}
}

func TestQuietEligibilityKeepsItsDistanceFromTheCeiling(t *testing.T) {
	sandboxFiles(t)
	now := float64(nowSec())
	calm := func(five float64) decision {
		return decision{usage: usageView{hasAny: true, updatedAt: now,
			fiveHour: &window{used: five, projected: five, resetsAt: now + 3600},
			sevenDay: &window{used: 10, projected: 10, resetsAt: now + 3*86400}}}
	}
	thresholds := func(five any) object {
		return object{"session5h": five, "weeklyAll": float64(95), "weeklyFable": float64(97)}
	}
	cases := []struct {
		name  string
		cfg   object
		five  float64
		quiet bool
	}{
		{"five-hour threshold off, window at 98%", object{"thresholds": thresholds(nil), "credits": object{"ceiling": float64(100)}}, 98, false},
		{"ceiling 60 under a threshold of 92, window at 59%", object{"thresholds": thresholds(float64(92)), "credits": object{"ceiling": float64(60)}}, 59, false},
		{"ceiling 60, window at 30%", object{"thresholds": thresholds(float64(92)), "credits": object{"ceiling": float64(60)}}, 30, true},
		{"five-hour threshold off, paid credits allowed", object{"thresholds": thresholds(nil), "credits": object{"allowPaid": true}}, 98, true},
		{"shipped limits, window at 20%", object{"thresholds": thresholds(float64(92)), "credits": object{"ceiling": float64(100)}}, 20, true},
	}
	for _, c := range cases {
		if got := quietEligible(c.cfg, emptyState(), "e1-eligible", calm(c.five), false); got != c.quiet {
			t.Errorf("%s: quiet eligible = %t, want %t", c.name, got, c.quiet)
		}
	}
}
