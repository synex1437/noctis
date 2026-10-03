package main

import (
	"testing"
)

func TestAMoveOffFableAfterAFailedTurnIsJournaledAsAModelSwitch(t *testing.T) {
	takeoverSandbox(t)
	previousHost := activeHost
	t.Cleanup(func() { activeHost = previousHost })
	activeHost = "claude"
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	config := releaseConfig()
	config["models"] = object{"primary": "claude-fable-5", "fallback": "claude-opus-5"}
	config["resume"] = object{"mode": "window", "prompt": "carry on"}
	mustWriteJSON(files.config, config)
	mustWriteJSON(files.settings, object{"model": "claude-fable-5"})
	now := float64(nowSec())
	writeFableBucket(99, now, now+3*86400)
	sid, cwd := "fable-failed-turn", t.TempDir()
	failure := agentHookInput("StopFailure", sid, cwd, claudeFailure("rate_limit", "", "API Error: Fable limit reached"))
	failure["transcript_path"] = writeTranscriptAt(t, cwd, []string{userPromptLine(now-60, "fix the parser")}, now-60)

	hookOutput(t, onStopFailure, failure, loadConfig())

	if wait := pendingWait(sid); getString(wait, "window") != "fable" || getString(wait, "modelOverride") != "claude-opus-5" {
		t.Fatalf("the failed turn was not parked as a relaunch on the fallback model: %v", wait)
	}
	switches := []object{}
	for _, line := range tailFileLines(files.decisions, 1000) {
		var entry object
		if jsonUnmarshalObject([]byte(line), &entry) == nil && getString(entry, "sid") == sid && getString(entry, "action") == "switch-model" {
			switches = append(switches, entry)
		}
	}
	if len(switches) != 1 {
		t.Fatalf("noctis moved the session off Fable after its turn failed at Fable's limit, and the journal has %d switch-model row(s) for it, want 1: %v", len(switches), journaledFor(sid))
	}
	if row := switches[0]; getString(row, "event") != "StopFailure" || getString(row, "to") != "claude-opus-5" || numberOr(row, "scoped", 0) != 99 || getString(row, "reason") != "Fable 99%" {
		t.Errorf("the switch-model row does not say what the move was: %v", row)
	}
}
