package main

import (
	"testing"
	"time"
)

func TestARunnerLeavesAWaitReplacedDuringItsGraceToItsOwnRunner(t *testing.T) {
	calls := takeoverSandbox(t)
	config := releaseConfig()
	config["resume"] = object{"mode": "window", "prompt": "carry on"}
	config["wait"] = object{"earlyResetPollMinutes": float64(5), "heartbeatGraceSeconds": float64(2)}
	mustWriteJSON(files.config, config)
	now := float64(nowSec())
	sid := "grace-swap"
	cwd := t.TempDir()
	transcript := quietTranscript(t, cwd, now-600)
	held := object{"kind": "batch", "window": "five_hour", "label": "5h", "used": float64(95), "threshold": float64(92), "hit": "threshold",
		"startedAt": now - 300, "until": now - 10, "resumeAt": now - 5, "cwd": cwd, "transcript": transcript, "launchMode": "headless", "inHook": true, "heartbeat": now - 600}
	replacement := object{"kind": "batch", "window": "seven_day", "label": "Wk", "used": float64(90), "threshold": float64(89), "hit": "threshold",
		"startedAt": now, "until": now + 3600, "resumeAt": now + 3600, "cwd": cwd, "transcript": transcript, "launchMode": "headless", "scheduled": object{"method": "manual", "at": now + 3600}}
	updateState(func(state object) { stateMap(state, "waits")[sid] = cloneObject(held) })
	statusReadingFrom(sid, nowSec(), 3, now+18000, 20, now+3*86400)
	swapped := make(chan struct{})
	go func() {
		time.Sleep(200 * time.Millisecond)
		updateState(func(state object) { stateMap(state, "waits")[sid] = cloneObject(replacement) })
		close(swapped)
	}()

	resumeWait(sid, "")

	<-swapped
	if got := launchesOf(calls, sid); got != 0 {
		t.Fatalf("a runner whose wait was replaced during its grace period relaunched the session %d time(s) for the wait that replaced it", got)
	}
	if stored := getMap(getMap(readState(), "waits"), sid); numberOr(stored, "startedAt", 0) != now || getString(stored, "window") != "seven_day" || getMap(stored, "scheduled") == nil {
		t.Fatalf("the wait that replaced the runner's own was changed: %v", stored)
	}
}

func TestARunnerRewritesOrClearsOnlyTheWaitItWasStartedFor(t *testing.T) {
	sandboxFiles(t)
	sid := "own-wait"
	now := float64(nowSec())
	mine, other := now-60, now-600
	updateState(func(state object) {
		stateMap(state, "waits")[sid] = object{"kind": "batch", "window": "five_hour", "startedAt": mine, "until": now + 3600, "resumeAt": now + 3600, "scheduled": object{"method": "manual", "at": now + 3600}}
	})
	stored := func() object { return getMap(getMap(readState(), "waits"), sid) }
	if rescheduleOwnWait(sid, other, func(record object) { record["resumeAt"] = now + 9999 }) || numberOr(stored(), "resumeAt", 0) != now+3600 {
		t.Fatalf("a runner started for another wait rewrote this one: %v", stored())
	}
	if !rescheduleOwnWait(sid, mine, func(record object) { record["resumeAt"] = now + 4000 }) || numberOr(stored(), "resumeAt", 0) != now+4000 {
		t.Fatalf("the runner of this wait could not reschedule it: %v", stored())
	}
	if clearOwnWait(sid, other) || stored() == nil {
		t.Fatalf("a runner started for another wait cleared this one")
	}
	if !clearOwnWait(sid, mine) || stored() != nil {
		t.Fatalf("the runner of this wait could not clear it: %v", stored())
	}
}
