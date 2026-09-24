package main

import (
	"os"
	"strings"
	"testing"
)

func relaunchedQueueSession(t *testing.T, sid string, waitStartedAt, claimedWaitStartedAt float64) (object, string) {
	t.Helper()
	cfg, project, _ := projectQueueSandbox(t)
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	now := float64(nowSec())
	updateState(func(state object) {
		stateMap(state, "waits")[sid] = object{"startedAt": waitStartedAt, "until": now - 60, "resumeAt": now - 30, "inHook": false, "hit": "five_hour", "label": "5h"}
		stateMap(state, "handedOff")[sid] = object{"at": now - 20, "model": "opus", "mode": "headless", "pid": float64(os.Getppid()), "waitStartedAt": claimedWaitStartedAt}
	})
	return cfg, project
}

func TestARelaunchedSessionIsGivenTheNextQueueItemAtStop(t *testing.T) {
	startedAt := float64(nowSec() - 5*3600)
	cfg, project := relaunchedQueueSession(t, "hs1", startedAt, startedAt)
	t.Setenv(handoffEnv, "hs1")
	output := stopHookOutput(t, stopInput("hs1", project), cfg)
	if getString(output, "decision") != "block" || !strings.Contains(getString(output, "reason"), "migrate the users table") {
		t.Fatalf("the session the runner relaunched finished its first item and the Stop hook let it stop with TASKS.md still open, because the runner still holds the wait it is resuming: %v", output)
	}
}

func TestAPauseInsideTheRelaunchedSessionStillLetsItStop(t *testing.T) {
	startedAt := float64(nowSec() - 5*3600)
	cfg, project := relaunchedQueueSession(t, "hs2", float64(nowSec()-10), startedAt)
	t.Setenv(handoffEnv, "hs2")
	if output := stopHookOutput(t, stopInput("hs2", project), cfg); output != nil {
		t.Fatalf("the relaunched session paused again at a new limit, and the Stop hook still drove its queue: %v", output)
	}
}

func TestTheOldWindowIsNotDrivenWhileTheRelaunchRuns(t *testing.T) {
	startedAt := float64(nowSec() - 5*3600)
	cfg, project := relaunchedQueueSession(t, "hs3", startedAt, startedAt)
	t.Setenv(handoffEnv, "")
	if output := stopHookOutput(t, stopInput("hs3", project), cfg); output != nil {
		t.Fatalf("the window the session was relaunched from was driven by the queue while the relaunch runs: %v", output)
	}
}
