package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func handedCheckpointTo(t *testing.T, cfg object, project, sid, checkpoint string) bool {
	t.Helper()
	start := hookOutput(t, onSessionStart, agentHookInput("SessionStart", sid, project, object{"source": "startup"}), cfg)
	return strings.Contains(contextOf(start), checkpoint)
}

func checkpointSpent(sid string) bool {
	return getBool(getMap(getMap(readState(), "checkpoints"), sid), "consumed", false)
}

func TestANewSessionIsNotHandedTheCheckpointOfASessionThatWillStillBeResumed(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 20, 40)
	now := nowSec()
	sid := "ho-parked"
	checkpoint := buildCheckpoint(agentHookInput("PostToolBatch", sid, project, nil), "paused", "opus", cfg)
	registerWait(sid, object{"kind": "batch", "window": "seven_day", "label": "weekly", "used": float64(90), "until": float64(now + 86400),
		"resumeAt": float64(now + 86490), "inHook": false, "cwd": project, "transcript": filepath.Join(project, "transcript.jsonl"),
		"checkpoint": checkpoint, "startedAt": float64(now), "scheduled": object{"method": "manual", "at": float64(now + 86490)}}, cfg)
	if handedCheckpointTo(t, cfg, project, "ho-new", checkpoint) || checkpointSpent(sid) {
		t.Fatal("a new session in the same folder was handed the checkpoint of a session its runner relaunches at the weekly reset")
	}
	clearWait(sid, nil)
	updateState(func(state object) {
		stateMap(state, "handedOff")[sid] = object{"at": float64(now), "pid": float64(os.Getpid()), "mode": "headless"}
	})
	if handedCheckpointTo(t, cfg, project, "ho-during", checkpoint) || checkpointSpent(sid) {
		t.Fatal("a new session was handed the checkpoint of a session its runner is relaunching right now")
	}
	updateState(func(state object) { delete(stateMap(state, "handedOff"), sid) })
	if !handedCheckpointTo(t, cfg, project, "ho-later", checkpoint) || !checkpointSpent(sid) {
		t.Fatal("with nothing left to resume it, the checkpoint was not handed to the next session in the folder")
	}
	cleared := "ho-cleared"
	clearedCheckpoint := buildCheckpoint(agentHookInput("PostToolBatch", cleared, project, nil), "paused", "opus", cfg)
	registerWait(cleared, object{"kind": "batch", "window": "five_hour", "label": "5h", "used": float64(93), "until": float64(now + 3000),
		"resumeAt": float64(now + 3090), "inHook": true, "heartbeat": float64(now - 600), "holder": "999999-gone", "cwd": project,
		"transcript": filepath.Join(project, "transcript.jsonl"), "checkpoint": clearedCheckpoint, "startedAt": float64(now - 900)}, cfg)
	usage := readJSON(files.usage)
	usage["sessions"] = object{cleared: object{"cwd": project, "updatedAt": float64(now), "model": "claude-opus-5-5"}}
	mustWriteJSON(files.usage, usage)
	start := hookOutput(t, onSessionStart, agentHookInput("SessionStart", "ho-after-clear", project, object{"source": "clear"}), cfg)
	if !strings.Contains(contextOf(start), clearedCheckpoint) {
		t.Fatalf("/clear released the interrupted wait of the session it replaced but did not hand its checkpoint over: %v", start)
	}
}

func TestTheCheckpointOfASessionTheUserWentOnWithIsNotHandedOver(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 20, 40)
	now := nowSec()
	sid := "ho-interrupted"
	checkpoint := buildCheckpoint(agentHookInput("PostToolBatch", sid, project, nil), "paused", "opus", cfg)
	registerWait(sid, object{"kind": "batch", "window": "five_hour", "label": "5h", "used": float64(93), "until": float64(now + 3000),
		"resumeAt": float64(now + 3090), "inHook": true, "heartbeat": float64(now - 600), "holder": "999999-gone", "cwd": project,
		"transcript": filepath.Join(project, "transcript.jsonl"), "checkpoint": checkpoint, "startedAt": float64(now - 900)}, cfg)
	hookOutput(t, onUserPromptSubmit, agentHookInput("UserPromptSubmit", sid, project, object{"prompt": "/noctis:status"}), cfg)
	if checkpointSpent(sid) {
		t.Fatal("checking the status from the paused session counted as going on with its work")
	}
	hookOutput(t, onUserPromptSubmit, agentHookInput("UserPromptSubmit", sid, project, object{"prompt": "go on with the parser tests"}), cfg)
	if handedCheckpointTo(t, cfg, project, "ho-next", checkpoint) {
		t.Fatal("the user went on in the paused session after interrupting its wait, yet a new session in the same folder was handed its checkpoint")
	}
	paused := "ho-guard-off"
	offCheckpoint := buildCheckpoint(agentHookInput("PostToolBatch", paused, project, nil), "paused", "opus", cfg)
	updateState(func(state object) { state["disabledUntil"] = float64(now + 3600) })
	hookOutput(t, onUserPromptSubmit, agentHookInput("UserPromptSubmit", paused, project, object{"prompt": "go on with the parser tests"}), cfg)
	updateState(func(state object) { state["disabledUntil"] = float64(0) })
	if handedCheckpointTo(t, cfg, project, "ho-after-off", offCheckpoint) {
		t.Fatal("the user went on in the paused session with the guard off, yet a new session in the same folder was handed its checkpoint")
	}
}
