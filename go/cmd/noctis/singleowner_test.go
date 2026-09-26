package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func strandedPause(t *testing.T, sid string, fields object) object {
	t.Helper()
	now := float64(nowSec())
	cwd := t.TempDir()
	record := object{"kind": "batch", "window": "five_hour", "label": "5h", "used": float64(95), "threshold": float64(92), "hit": "threshold",
		"startedAt": now - 600, "until": now - 20, "resumeAt": now - 20, "cwd": cwd, "transcript": quietTranscript(t, cwd, now-600), "launchMode": "headless"}
	for key, value := range fields {
		record[key] = value
	}
	updateState(func(state object) { stateMap(state, "waits")[sid] = cloneObject(record) })
	return record
}

func whileTheStateIsLocked(act func(), change func(state object)) {
	done := make(chan struct{})
	updateState(func(state object) {
		go func() {
			defer close(done)
			act()
		}()
		time.Sleep(time.Second)
		change(state)
	})
	<-done
}

func TestAWakeThatFindsItsPauseReplacedLeavesTheNewPausesWakingMarkAlone(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	sid := "wake-replaced"
	now := float64(nowSec())
	gone := object{"kind": "stopfailure", "window": "unknown", "label": "API", "until": now, "resumeAt": now, "startedAt": now - 120, "inHook": false}
	current := object{"kind": "stopfailure", "window": "unknown", "label": "API", "until": now + 600, "resumeAt": now + 600, "startedAt": now - 5, "inHook": false, "waking": now - 5}
	updateState(func(state object) { stateMap(state, "waits")[sid] = cloneObject(current) })
	wakeSameSession(sharedWaitConfig(), sid, gone, now)
	if wait := pendingWait(sid); numberOr(wait, "waking", 0) != now-5 {
		t.Fatalf("a wake whose pause was replaced before it began took the waking mark off the pause in its place: %v", wait)
	}
}

func TestAWakeDoesNotWakeASessionARunnerAlreadyRelaunched(t *testing.T) {
	if os.Getenv("NOCTIS_TEST_WAKE_AFTER_TAKEOVER") != "" {
		cfg, project, _ := limitSandbox(t, nil, 50, 40)
		sid := "wake-taken"
		now := float64(nowSec())
		record := object{"kind": "stopfailure", "window": "unknown", "label": "API", "until": now, "resumeAt": now, "startedAt": now - 300, "inHook": false,
			"cwd": project, "transcript": filepath.Join(project, "transcript.jsonl")}
		updateState(func(state object) {
			stateMap(state, "waits")[sid] = cloneObject(record)
			stateMap(state, "handedOff")[sid] = object{"at": now - 30, "model": "claude-opus-5", "mode": "window", "pid": float64(os.Getppid()), "waitStartedAt": now - 300}
		})
		wakeSameSession(cfg, sid, record, now)
		return
	}
	scratch := t.TempDir()
	child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	child.Env = append(os.Environ(), "NOCTIS_TEST_WAKE_AFTER_TAKEOVER=1", "TMPDIR="+scratch, "TMP="+scratch, "TEMP="+scratch)
	output, err := child.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 2 {
		t.Fatalf("the wake woke a session a runner had already relaunched, so the session ran twice:\n%s", output)
	} else if err != nil {
		t.Fatalf("the wake failed: %v\n%s", err, output)
	}
}

func TestAHookHoldingAPauseARunnerRelaunchedStopsInsteadOfContinuing(t *testing.T) {
	calls := takeoverSandbox(t)
	config := readJSON(files.config)
	section(config, "wait")["heartbeatGraceSeconds"] = float64(1)
	mustWriteJSON(files.config, config)
	sid := "taken-over"
	now := float64(nowSec())
	held := strandedPause(t, sid, object{"inHook": true, "heartbeat": now, "aliveChecks": float64(aliveHookMaxChecks), "holder": "stuck-hook"})
	statusReadingFrom(sid, nowSec(), 3, now+18000, 20, now+3*86400)
	resumeWait(sid, "")
	if launches := launchesOf(calls, sid); launches != 1 {
		t.Fatalf("the runner did not take the session over from its stuck hook: %d launches (journal %v)", launches, journaledFor(sid))
	}
	if hold := holdWait("batch", sid, loadConfig(), fiveHourPlan(now-20), now-20, held, true); hold.stop != T("wait.continuedElsewhere") {
		t.Fatalf("the hook woke after the runner had relaunched its session and let its own window go on too: %+v", hold)
	}
}

func TestARunnerThatFindsItsPauseReplacedWhileItWorksLeavesTheNewPauseAlone(t *testing.T) {
	takeoverSandbox(t)
	config := readJSON(files.config)
	config["resume"] = object{"mode": "none"}
	mustWriteJSON(files.config, config)
	sid := "runner-stale"
	now := float64(nowSec())
	strandedPause(t, sid, nil)
	statusReadingFrom(sid, nowSec(), 3, now+18000, 20, now+3*86400)
	whileTheStateIsLocked(func() { resumeWait(sid, "") }, func(state object) {
		stateMap(state, "waits")[sid] = object{"kind": "batch", "window": "seven_day", "label": "7d", "used": float64(96), "startedAt": now - 2, "until": now + 86400, "resumeAt": now + 86400,
			"holder": "new-pause", "scheduled": object{"method": "manual", "at": now + 86400}}
	})
	if wait := pendingWait(sid); getString(wait, "holder") != "new-pause" {
		t.Fatalf("a runner that read its pause before a new one took its place deleted the new pause: %v", wait)
	}
}

func TestAnInterruptedPauseIsReleasedOnlyWhileItIsStillTheStoredOne(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	sid := "interrupted-stale"
	now := float64(nowSec())
	interrupted := object{"kind": "batch", "window": "five_hour", "label": "5h", "inHook": true, "startedAt": now - 900, "heartbeat": now - 600, "until": now + 3600, "resumeAt": now + 3600,
		"holder": formatNumber(float64(goneRunner(t))) + "-gone"}
	updateState(func(state object) {
		stateMap(state, "waits")[sid] = object{"kind": "prompt", "window": "five_hour", "label": "5h", "inHook": false, "startedAt": now - 5, "until": now + 3600, "resumeAt": now + 3600,
			"holder": "new-pause", "scheduled": object{"method": "manual", "at": now + 3600}}
	})
	releaseInterruptedWait(sid, object{"waits": object{sid: interrupted}})
	if wait := pendingWait(sid); getString(wait, "holder") != "new-pause" {
		t.Fatalf("releasing an interrupted pause read before a new one was stored deleted the new pause: %v", wait)
	}
}

func TestAHookHeartbeatGoesOnlyToThePauseItHolds(t *testing.T) {
	sandboxFiles(t)
	sid := "heartbeat-stale"
	now := float64(nowSec())
	updateState(func(state object) {
		stateMap(state, "waits")[sid] = object{"kind": "batch", "window": "five_hour", "inHook": true, "startedAt": now - 300, "heartbeat": now - 30, "holder": "old-hook", "resumeAt": now + 3600}
	})
	watch := newWaitWatch(object{"wait": object{"earlyResetPollMinutes": float64(0)}}, sid, true)
	watch.startedAt = now - 300
	whileTheStateIsLocked(func() { watch.tick() }, func(state object) {
		stateMap(state, "waits")[sid] = object{"kind": "batch", "window": "seven_day", "inHook": false, "startedAt": now - 5, "heartbeat": now - 5, "holder": "new-pause", "resumeAt": now + 86400}
	})
	if heartbeat := numberOr(pendingWait(sid), "heartbeat", 0); heartbeat != now-5 {
		t.Fatalf("a hook's heartbeat landed on the pause that took the place of its own, so that pause looks held by a live hook: heartbeat %s, want %s", formatNumber(heartbeat), formatNumber(now-5))
	}
}

func TestAManualResumeInAnotherWindowStopsTheHookThatHeldThePause(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 50, 40)
	sid := "resumed-by-hand"
	now := float64(nowSec())
	held := object{"kind": "batch", "window": "five_hour", "label": "5h", "used": float64(95), "until": now, "resumeAt": now, "inHook": true, "startedAt": now - 300, "heartbeat": now,
		"holder": "first-window", "cwd": project}
	updateState(func(state object) { stateMap(state, "waits")[sid] = cloneObject(held) })
	hookOutput(t, onSessionStart, sessionStartInput(sid, "resume", project), cfg)
	if hold := holdWait("batch", sid, cfg, fiveHourPlan(now), now, held, true); hold.stop != T("wait.continuedElsewhere") {
		t.Fatalf("the session was resumed in another window, yet the hook in the first window let it go on too: %+v", hold)
	}
}

func TestAnAgentSpawnHeldWhileTheSessionWentOnElsewhereEndsTheTurn(t *testing.T) {
	cfg, project, _ := limitSandbox(t, object{"wait": object{"resetMarginSeconds": 0, "earlyResetPollMinutes": 0}}, 93, 40)
	sid := "spawn-resumed-elsewhere"
	writeUsage(93, 40, float64(nowSec()+3))
	resumed := make(chan bool, 1)
	go func() {
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			if wait := pendingWait(sid); wait != nil {
				resumed <- takeWait(sid, wait, "resume", true)
				return
			}
		}
		resumed <- false
	}()
	output := hookOutput(t, onPreToolUse, agentSpawn(sid, project, object{"subagent_type": "general-purpose", "prompt": "review the parser"}), cfg)
	if !<-resumed {
		t.Fatalf("the Agent call did not pause in the hook, so there was no pause to resume elsewhere: %v", output)
	}
	if !stoppedBy(output) || getString(output, "stopReason") != T("wait.continuedElsewhere") {
		t.Fatalf("the session went on in another window, yet this window's turn was let go on as well: %v", output)
	}
	if permissionOf(output) != "deny" || reasonOf(output) != T("wait.continuedElsewhere") {
		t.Fatalf("a tool that tools ignoring continue:false would still run was not denied: %v", output)
	}
}

func TestARunnerGivesASessionWokenInPlaceItsGraceBeforeRelaunchingIt(t *testing.T) {
	calls := takeoverSandbox(t)
	sid := "woken-late"
	now := float64(nowSec())
	strandedPause(t, sid, object{"kind": "stopfailure", "used": float64(100), "wakeAttemptedAt": now - 5})
	statusReadingFrom(sid, nowSec(), 3, now+18000, 20, now+3*86400)
	resumeWait(sid, "")
	if launches := launchesOf(calls, sid); launches != 0 {
		t.Fatalf("the runner relaunched a session woken in place 5 s before without giving the wake its grace, so the session ran twice: %d launches (journal %v)", launches, journaledFor(sid))
	}
	if at := numberOr(getMap(pendingWait(sid), "scheduled"), "at", 0); at < now+55 {
		t.Fatalf("the runner did not come back to check on the wake once its grace is over: %v", pendingWait(sid))
	}
}

func TestARunnerDoesNotRelaunchASessionWokenInPlaceWhileItChecked(t *testing.T) {
	calls := takeoverSandbox(t)
	sid := "woken-meanwhile"
	now := float64(nowSec())
	strandedPause(t, sid, object{"kind": "stopfailure", "used": float64(100)})
	statusReadingFrom(sid, nowSec(), 3, now+18000, 20, now+3*86400)
	whileTheStateIsLocked(func() { resumeWait(sid, "") }, func(state object) {
		getMap(getMap(state, "waits"), sid)["wakeAttemptedAt"] = float64(nowSec())
	})
	if launches := launchesOf(calls, sid); launches != 0 {
		t.Fatalf("the runner relaunched a session its wake had just woken in place, so the session ran twice: %d launches (journal %v)", launches, journaledFor(sid))
	}
}

func TestARunnerThatReschedulesAPauseReplacedMeanwhileLeavesTheNewPausesRunnerAlone(t *testing.T) {
	takeoverSandbox(t)
	sid := "rescheduled-stale"
	now := float64(nowSec())
	strandedPause(t, sid, nil)
	statusReadingFrom(sid, nowSec(), 96, now+7200, 20, now+3*86400)
	done := make(chan struct{})
	withFileLock(scheduleLockFile(sid), func() {
		go func() {
			defer close(done)
			resumeWait(sid, "")
		}()
		time.Sleep(time.Second)
		updateState(func(state object) {
			stateMap(state, "waits")[sid] = object{"kind": "batch", "window": "seven_day", "label": "7d", "used": float64(96), "startedAt": now - 100, "until": now + 1800, "resumeAt": now + 1800,
				"holder": "new-pause", "scheduled": object{"method": "manual", "at": now + 1800}}
		})
	})
	<-done
	if at := numberOr(getMap(pendingWait(sid), "scheduled"), "at", 0); at != now+1800 {
		t.Fatalf("a runner that rescheduled its pause after a new one took its place replaced the new pause's runner: runner at %s, want %s", formatNumber(at), formatNumber(now+1800))
	}
}

func TestAnEarlyResumeIsTriggeredOnlyForThePauseFoundClear(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("NOCTIS_NO_EARLY_TRIGGER", "")
	sid := "early-stale"
	now := float64(nowSec())
	strandedPause(t, sid, object{"until": now + 3600, "resumeAt": now + 3600, "scheduled": object{"method": "manual", "at": now + 3600}})
	statusReadingFrom("another-window", nowSec(), 3, now+18000, 20, now+3*86400)
	whileTheStateIsLocked(func() { triggerEarlyResumes(releaseConfig()) }, func(state object) {
		stateMap(state, "waits")[sid] = object{"kind": "batch", "window": "seven_day", "label": "7d", "used": float64(96), "startedAt": now - 2, "until": now + 86400, "resumeAt": now + 86400,
			"holder": "new-pause", "scheduled": object{"method": "manual", "at": now + 86400}}
	})
	if wait := pendingWait(sid); numberOr(wait, "earlyTriggeredAt", 0) != 0 {
		t.Fatalf("an early reset seen for one pause started the runner of the weekly pause that took its place: %v", wait)
	}
}
