package main

import (
	"os"
	"testing"
)

func TestAPausedGuardStillRefusesAPromptInTheWindowASessionWasHandedOffFrom(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 20, 40)
	t.Setenv(handoffEnv, "")
	sid, at := "h4-handed-off", float64(nowSec()-60)
	updateState(func(state object) {
		// The relaunch that took the session over is alive: this test process stands in for it.
		stateMap(state, "handedOff")[sid] = object{"at": at, "pid": float64(os.Getpid()), "mode": "window", "model": "claude-opus-5"}
		state["disabledUntil"] = float64(nowSec() + 3600)
	})
	if !guardPaused(cfg, readState(), nowSec()) {
		t.Fatal("the guard is not paused, so this test proves nothing")
	}
	output := hookOutput(t, onUserPromptSubmit, promptInput(sid, project, "go on with the parser tests"), cfg)
	if want := T("handoff.blocked", formatTime(at), "claude-opus-5", sid); getString(output, "decision") != "block" || getString(output, "reason") != want {
		t.Fatalf("while the guard is paused, a prompt in the window the session was handed off from went ahead, so two processes drive one session: %v", output)
	}
	if output := hookOutput(t, onUserPromptSubmit, promptInput("h4-own-session", project, "go on with the parser tests"), cfg); output != nil {
		t.Fatalf("while the guard is paused, a prompt in a session nobody took over was not let through: %v", output)
	}
	t.Setenv(handoffEnv, sid)
	if output := hookOutput(t, onUserPromptSubmit, promptInput(sid, project, "go on with the parser tests"), cfg); output != nil {
		t.Fatalf("the relaunch that took the session over was refused as if it were the old window: %v", output)
	}
}
