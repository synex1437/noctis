package main

import (
	"math"
	"os"
)

func ownWait(state object, sid string, startedAt float64) object {
	if record := getMap(getMap(state, "waits"), sid); record != nil && numberOr(record, "startedAt", -1) == startedAt {
		return record
	}
	return nil
}

func continuedElsewhere(state object, sid string, startedAt float64, holder string) string {
	if mark := getMap(getMap(state, "continuedBy"), sid); sameWait(mark, startedAt, holder) {
		return getString(mark, "by")
	}
	if handoff := getMap(getMap(state, "handedOff"), sid); handoff != nil && numberOr(handoff, "waitStartedAt", -1) == startedAt {
		return "runner"
	}
	return ""
}

func resumedByAnEarlierRunner(state object, sid string, wait object) bool {
	mark := getMap(getMap(state, "continuedBy"), sid)
	claimedAt := numberOr(mark, "at", 0)
	if getString(mark, "by") != "runner" || claimedAt <= 0 || !sameWait(mark, numberOr(wait, "startedAt", -1), getString(wait, "holder")) {
		return false
	}
	answered, _ := relaunchAnswered(wait, claimedAt)
	return answered
}

func markContinued(state object, sid string, record object, by string) {
	stateMap(state, "continuedBy")[sid] = object{"startedAt": numberOr(record, "startedAt", -1), "holder": getString(record, "holder"), "by": by, "at": float64(nowSec())}
}

func takeWait(sid string, seen object, by string, consume bool) bool {
	startedAt, holder := numberOr(seen, "startedAt", -1), getString(seen, "holder")
	var taken object
	updateState(func(next object) {
		current := getMap(getMap(next, "waits"), sid)
		if !sameWait(current, startedAt, holder) {
			return
		}
		taken = current
		dropWait(next, sid, current, by, consume)
	})
	if taken == nil {
		return false
	}
	cancelScheduled(sid, getMap(taken, "scheduled"))
	return true
}

func dropWait(state object, sid string, wait object, by string, consume bool) {
	delete(stateMap(state, "waits"), sid)
	if entry := getMap(getMap(state, "checkpoints"), sid); consume && entry != nil {
		entry["consumed"] = true
	}
	if by != "" {
		markContinued(state, sid, wait, by)
	}
}

func promptTakesOver(sid string) (handoff, taken object) {
	updateState(func(next object) {
		if handoff = getMap(getMap(next, "handedOff"), sid); handoff != nil {
			return
		}
		if wait := getMap(getMap(next, "waits"), sid); getString(wait, "kind") == "fable" && !hookSleeping(wait) {
			taken = wait
			dropWait(next, sid, wait, "session", true)
		}
	})
	if taken != nil {
		cancelScheduled(sid, getMap(taken, "scheduled"))
	}
	return handoff, taken
}

func clearWait(sid string, state object) bool {
	if state == nil {
		state = readState()
	}
	return takeWait(sid, getMap(getMap(state, "waits"), sid), "", false)
}

func clearWaitAndConsume(sid string, state object) bool {
	if state == nil {
		state = readState()
	}
	return takeWait(sid, getMap(getMap(state, "waits"), sid), "", true)
}

func rescheduleOwnWait(sid string, startedAt float64, change func(record object)) bool {
	stored := false
	updateState(func(next object) {
		if record := ownWait(next, sid, startedAt); record != nil {
			change(record)
			stored = true
		}
	})
	return stored
}

func clearOwnWait(sid string, startedAt float64) bool {
	record := ownWait(readState(), sid, startedAt)
	return record != nil && takeWait(sid, record, "", false)
}

func scheduleOwnRunner(cfg object, sid string, startedAt, atEpoch float64) bool {
	own := false
	withFileLock(scheduleLockFile(sid), func() {
		if own = ownWait(readState(), sid, startedAt) != nil; own {
			scheduleRunnerLocked(cfg, sid, atEpoch, 0)
		}
	})
	return own
}

func releaseHandoff(sid string, startedAt float64) {
	updateState(func(next object) {
		if int(numberOr(getMap(getMap(next, "handedOff"), sid), "pid", 0)) == os.Getpid() {
			delete(stateMap(next, "handedOff"), sid)
		}
		if ownWait(next, sid, startedAt) != nil {
			delete(stateMap(next, "waits"), sid)
		}
	})
}

func wakeGraceSeconds(cfg object) float64 {
	return math.Max(60, numberOr(section(cfg, "wake"), "graceSeconds", 300))
}
