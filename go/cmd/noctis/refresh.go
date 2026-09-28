package main

import (
	"os"
	"strconv"
	"time"
)

// hostWaitsOn names the commands the host waits on before it goes on: a hook holds Claude's next
// step and the status line its next repaint. Neither makes the host wait on the usage endpoint
// longer than refreshAside allows.
var hostWaitsOn = map[string]bool{"hook": true, "statusline": true}

// refreshWait bounds how long a hook waits for a fetch it handed off: when it would otherwise
// decide on no reading, or on one older than the stale limit, and when its caller makes a
// one-time choice near an edge. The endpoint answers well inside it when it is up; one that hangs
// costs the hook this instead of the fetch timeout, and its answer still lands for the next hook.
const refreshWait = 1500 * time.Millisecond

// refresherAdoptWait bounds how long a refresher waits for the hook that started it to hand it
// fable.lock (handLockTo), which that hook does as soon as the refresher exists.
const refresherAdoptWait = 2 * time.Second

// refreshAside hands a due fetch to a detached `noctis refresh`. The hook takes fable.lock in the
// refresher's name, so one fetch is in flight at a time and fable.lock stands for it until its
// answer is written. The hook decides on the reading it has and waits for the answer only up to
// wait, or refreshWait when that reading is missing or stale. A hook that waits asks the OAuth
// endpoint itself first, which spares it starting a process, and hands the fetch on only when no
// answer came within wait. It reports false, having started nothing, when no refresher could be
// started, and the caller then fetches itself.
func refreshAside(cfg, cached object, now int64, reason string, wait time.Duration) (object, bool) {
	if fetchedAt := numberOr(cached, "fetchedAt", 0); fetchedAt == 0 || float64(now)-fetchedAt > usageStaleSeconds(cfg) {
		wait = max(wait, refreshWait)
	}
	handle, acquired := openFileLock(files.fableLock)
	if !acquired {
		if wait > 0 && awaitLockRelease(files.fableLock, wait) {
			return orObject(readJSON(files.fable), cached), true
		}
		logInfo("fable refresh skipped (%s): another process is fetching", reason)
		return cached, true
	}
	if latest := readJSON(files.fable); latest != nil && numberOr(latest, "fetchedAt", 0) > numberOr(cached, "fetchedAt", 0) {
		releaseLock(handle, files.fableLock)
		return latest, true
	}
	if wait > 0 && getString(section(cfg, "fable"), "source") == "oauth" {
		if answer, answered := fetchOauthFable(cfg, cached, now, reason, wait); answered {
			releaseLock(handle, files.fableLock)
			return answer, true
		}
		logInfo("fable refresh (%s) got no answer within %s; a refresher asks again and leaves its answer for the next hook", reason, wait)
		wait = 0
	}
	refresher := startDetached(refresherArgs(reason))
	if refresher == nil {
		releaseLock(handle, files.fableLock)
		return nil, false
	}
	handLockTo(handle, refresher.Pid)
	if wait <= 0 {
		reapWhenDone(refresher)
		return cached, true
	}
	if !awaitExit(refresher, wait) {
		logInfo("fable refresh (%s) still running after %s; its answer is left for the next hook", reason, wait)
		return cached, true
	}
	return orObject(readJSON(files.fable), cached), true
}

func refresherArgs(reason string) []string {
	arguments := []string{"refresh", "--account", files.configDir, "--reason", reason}
	if host := currentHost().id; host != "claude" {
		arguments = append(arguments, "--host", host)
	}
	return arguments
}

// runRefresh is the detached fetch a hook hands off (refreshAside). The hook took fable.lock in
// this process's name, so it fetches without asking for the lock again and lets it go at the end.
func runRefresh() {
	cfg := loadedConfig()
	release, adopted := adoptFileLock(files.fableLock)
	if !adopted {
		return
	}
	defer release()
	fetchFable(cfg, orObject(readJSON(files.fable), object{}), nowSec(), orDefault(flagString("reason"), "hook"))
}

// handLockTo leaves a lock taken with openFileLock to the process pid, which removes it when it is
// done (adoptFileLock). The file keeps its name, so the lock is never free in between.
func handLockTo(handle *os.File, pid int) {
	if err := handle.Truncate(0); err == nil {
		_, _ = handle.WriteAt([]byte(strconv.Itoa(pid)), 0)
	}
	_ = handle.Close()
}

// adoptFileLock takes over lockFile once the process that took it has handed it to this one.
func adoptFileLock(lockFile string) (func(), bool) {
	mine := strconv.Itoa(os.Getpid())
	for deadline := time.Now().Add(refresherAdoptWait); ; time.Sleep(2 * time.Millisecond) {
		owner, _, present := lockHolder(lockFile)
		if !present {
			return func() {}, false
		}
		if owner == mine {
			break
		}
		if time.Now().After(deadline) {
			logInfo("%s was never handed over (held by %q); nothing fetched", lockFile, owner)
			return func() {}, false
		}
	}
	handle, err := os.OpenFile(lockFile, os.O_RDWR, 0)
	if err != nil {
		return func() {}, false
	}
	holdLock(handle)
	return func() { releaseLock(handle, lockFile) }, true
}

// awaitLockRelease waits up to wait for another process to let lockFile go. It gives up at once
// on a lock no fetch stands behind: one whose holder is gone, or one that names no process (an
// empty file is only a hand-over in progress while it is fresh), since no answer is coming.
func awaitLockRelease(lockFile string, wait time.Duration) bool {
	for deadline := time.Now().Add(wait); ; time.Sleep(10 * time.Millisecond) {
		owner, age, present := lockHolder(lockFile)
		if !present {
			return true
		}
		pid, parsed := lockOwnerPid(owner)
		if parsed && !processAlive(pid) || !parsed && (owner != "" || age > time.Second) {
			return false
		}
		if holderStale(owner, age) || time.Now().After(deadline) {
			return false
		}
	}
}

func awaitExit(process *os.Process, wait time.Duration) bool {
	exited := make(chan struct{})
	go func() {
		_, _ = process.Wait()
		close(exited)
	}()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-exited:
		return true
	case <-timer.C:
		return false
	}
}

func orObject(value, fallback object) object {
	if value == nil {
		return fallback
	}
	return value
}
