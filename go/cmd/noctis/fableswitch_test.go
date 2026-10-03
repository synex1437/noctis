package main

import (
	"testing"
)

func fableFailureSandbox(t *testing.T, used float64, noSwitchPoint bool) {
	t.Helper()
	takeoverSandbox(t)
	previousHost := activeHost
	t.Cleanup(func() { activeHost = previousHost })
	activeHost = "claude"
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	config := releaseConfig()
	config["models"] = object{"primary": "claude-fable-5", "fallback": "claude-opus-5"}
	config["resume"] = object{"mode": "window", "prompt": "carry on"}
	if noSwitchPoint {
		getMap(config, "thresholds")["weeklyFable"] = nil
	}
	mustWriteJSON(files.config, config)
	mustWriteJSON(files.settings, object{"model": "claude-fable-5"})
	now := float64(nowSec())
	writeFableBucket(used, now, now+3*86400)
}

func failedFableTurn(t *testing.T, sid, cwd string, fields object) object {
	t.Helper()
	failure := agentHookInput("StopFailure", sid, cwd, fields)
	now := float64(nowSec())
	failure["transcript_path"] = writeTranscriptAt(t, cwd, []string{userPromptLine(now-60, "fix the parser")}, now-60)
	return failure
}

func modelSwitchesOf(sid string) []object {
	switches := []object{}
	for _, line := range tailFileLines(files.decisions, 1000) {
		var entry object
		if jsonUnmarshalObject([]byte(line), &entry) == nil && getString(entry, "sid") == sid && getString(entry, "action") == "switch-model" {
			switches = append(switches, entry)
		}
	}
	return switches
}

func TestAMoveOffFableAfterAFailedTurnIsJournaledAsAModelSwitch(t *testing.T) {
	fableFailureSandbox(t, 99, false)
	sid, cwd := "fable-failed-turn", t.TempDir()

	hookOutput(t, onStopFailure, failedFableTurn(t, sid, cwd, claudeFailure("rate_limit", "", "API Error: Fable limit reached")), loadConfig())

	if wait := pendingWait(sid); getString(wait, "window") != "fable" || getString(wait, "modelOverride") != "claude-opus-5" {
		t.Fatalf("the failed turn was not parked as a relaunch on the fallback model: %v", wait)
	}
	switches := modelSwitchesOf(sid)
	if len(switches) != 1 {
		t.Fatalf("noctis moved the session off Fable after its turn failed at Fable's limit, and the journal has %d switch-model row(s) for it, want 1: %v", len(switches), journaledFor(sid))
	}
	if row := switches[0]; getString(row, "event") != "StopFailure" || getString(row, "to") != "claude-opus-5" || numberOr(row, "scoped", 0) != 99 || getString(row, "reason") != "Fable 99%" {
		t.Errorf("the switch-model row does not say what the move was: %v", row)
	}
}

func TestAFailureThatNamesNoLimitMovesTheSessionOffFableOnlyPastItsSwitchPointOrWhenItRepeats(t *testing.T) {
	unnamed := claudeFailure("rate_limit", "429 Too Many Requests", "API Error: Rate limit reached")
	timedOut := claudeFailure("unknown", "", "API Error: Request timed out")
	cases := []struct {
		name          string
		fable         float64
		noSwitchPoint bool
		batchBetween  bool
		first         object
		filed         []string
	}{
		{"below the switch point, failing again before the session got anything done", 92, false, false, unnamed, []string{"unknown", "fable"}},
		{"below the switch point, failing again after a batch of tools", 92, false, true, unnamed, []string{"unknown", "unknown"}},
		{"below the switch point, failing this way after a failure of another kind", 92, false, false, timedOut, []string{"unknown", "unknown", "fable"}},
		{"at 99 % with the switch point turned off, failing again before the session got anything done", 99, true, false, unnamed, []string{"unknown", "fable"}},
		{"past the switch point", 96, false, false, unnamed, []string{"fable"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fableFailureSandbox(t, tc.fable, tc.noSwitchPoint)
			sid, cwd := "fable-unnamed-limit", t.TempDir()
			for attempt, want := range tc.filed {
				if attempt > 0 && tc.batchBetween {
					hookOutput(t, onPostToolBatch, agentHookInput("PostToolBatch", sid, cwd, nil), loadConfig())
				}
				failure := unnamed
				if attempt == 0 {
					failure = tc.first
				}
				hookOutput(t, onStopFailure, failedFableTurn(t, sid, cwd, failure), loadConfig())

				if got := getString(pendingWait(sid), "window"); got != want {
					t.Fatalf("failure %d, %q naming no limit with Fable at %v %%, was filed under %q, want %q (journal %v)", attempt+1, getString(failure, "error"), tc.fable, got, want, journaledFor(sid))
				}
				if moved, want := len(modelSwitchesOf(sid)) > 0, want == "fable"; moved != want {
					t.Fatalf("failure %d, %q naming no limit with Fable at %v %%: moved off Fable %t, want %t (settings model %q)", attempt+1, getString(failure, "error"), tc.fable, moved, want, settingsModel())
				}
			}
		})
	}
}
