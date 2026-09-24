//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestANewSessionIsNotHandedTheCheckpointOfARelaunchThatWentOnByItself(t *testing.T) {
	calls := takeoverSandbox(t)
	config := releaseConfig()
	config["wait"] = object{"earlyResetPollMinutes": float64(5), "heartbeatGraceSeconds": float64(1)}
	config["resume"] = object{"mode": "headless", "prompt": "carry on"}
	config["wake"] = object{"sameSession": false}
	mustWriteJSON(files.config, config)
	gate := t.TempDir()
	running, release := filepath.Join(gate, "running"), filepath.Join(gate, "release")
	writeScript(t, filepath.Join(filepath.Dir(calls), "claude"), fakeClaude(
		"touch '"+running+"'",
		"while [ ! -f '"+release+"' ]; do sleep 0.05; done",
		answerLine,
	))
	sid := "relaunch-went-on"
	parkForRelaunch(t, sid, "batch", "headless")
	cwd := getString(waitOf(sid), "cwd")
	done := make(chan struct{})
	go func() { resumeWait(sid, ""); close(done) }()
	released := false
	defer func() {
		if !released {
			_ = os.WriteFile(release, nil, 0o600)
			<-done
		}
	}()
	for deadline := time.Now().Add(20 * time.Second); statSafe(running) == nil; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the relaunch never started")
		}
	}
	cfg := loadConfig()
	hookOutput(t, onStopFailure, agentHookInput("StopFailure", sid, cwd, object{"error": "rate_limit", "last_assistant_message": "API Error: Request rejected (429) · Number of request tokens has exceeded your per-minute rate limit"}), cfg)
	checkpoint := getMap(getMap(readState(), "checkpoints"), sid)
	if waitOf(sid) == nil || checkpoint == nil || getBool(checkpoint, "consumed", true) {
		t.Fatalf("the relaunched session's 429 should leave a retry and an unused checkpoint: wait %v, checkpoint %v", waitOf(sid), checkpoint)
	}
	hookOutput(t, onNotification, agentHookInput("Notification", sid, cwd, object{"notification_type": "quota_auto_resume_fired"}), cfg)
	if waitOf(sid) != nil || getMap(getMap(readState(), "handedOff"), sid) == nil {
		t.Fatalf("Claude Code's own auto-continue should cancel the retry while the relaunch still runs: wait %v, hand-off %v", waitOf(sid), getMap(getMap(readState(), "handedOff"), sid))
	}
	if handedCheckpointTo(t, cfg, cwd, "beside-the-relaunch", getString(checkpoint, "path")) {
		t.Fatal("a new session was handed the checkpoint of a relaunched session that Claude Code's auto-continue had just resumed")
	}
	released = true
	_ = os.WriteFile(release, nil, 0o600)
	<-done
	if !handedCheckpointTo(t, cfg, cwd, "after-the-relaunch", getString(checkpoint, "path")) {
		t.Fatal("once the relaunch had ended, with nothing left to resume the session, its checkpoint was not handed to the next session")
	}
}
