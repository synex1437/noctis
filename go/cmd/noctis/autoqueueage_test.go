package main

import (
	"os"
	"testing"
	"time"
)

func agedChecklist(t *testing.T, sid string, started time.Duration, touched time.Duration, waiting bool) string {
	t.Helper()
	_, project := queueTrustSandbox(t, false)
	path := checklistFor(t, sid, project)
	at := time.Now().Add(-touched)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
	now := float64(nowSec())
	updateState(func(state object) {
		getMap(stateMap(state, "autoQueues"), sid)["at"] = now - started.Seconds()
		if waiting {
			stateMap(state, "waits")[sid] = object{"kind": "stop", "window": "seven_day", "until": now + 2*86400, "resumeAt": now + 2*86400, "startedAt": now - 86400, "cwd": project}
		}
	})
	updateState(func(object) {})
	return path
}

func checklistKept(sid, path string) bool {
	return getMap(getMap(readState(), "autoQueues"), sid) != nil && statSafe(path) != nil
}

const week = 7 * 24 * time.Hour

func TestAChecklistIsKeptWhileItsSessionWaits(t *testing.T) {
	if path := agedChecklist(t, "aq1", week+24*time.Hour, week+24*time.Hour, true); !checklistKept("aq1", path) {
		t.Fatal("a checklist started 8 days ago was deleted while its session was still waiting on a weekly pause")
	}
}

func TestAChecklistClaudeStillTicksIsKept(t *testing.T) {
	if path := agedChecklist(t, "aq2", week+24*time.Hour, time.Hour, false); !checklistKept("aq2", path) {
		t.Fatal("a checklist started 8 days ago and ticked an hour ago was deleted")
	}
}

func TestAChecklistNobodyTouchedForAWeekIsRemoved(t *testing.T) {
	path := agedChecklist(t, "aq3", week+24*time.Hour, week+24*time.Hour, false)
	if getMap(getMap(readState(), "autoQueues"), "aq3") != nil || statSafe(path) != nil {
		t.Fatal("a checklist nobody waited on or ticked for 8 days was kept")
	}
}
