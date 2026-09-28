package main

import (
	"os"
	"strconv"
	"testing"
	"time"
)

func TestALauncherThatHasNotWrittenItsPidYetIsWaitedFor(t *testing.T) {
	sandboxFiles(t)
	ensureDir(files.launches)
	_, _, pidFile := launchFiles("pid-not-written-yet")
	// A shell's redirection creates the pid file before it writes the pid into it.
	if err := os.WriteFile(pidFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	pid := deadPid(t)
	written := make(chan struct{})
	go func() {
		defer close(written)
		time.Sleep(300 * time.Millisecond)
		_ = os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0o600)
	}()
	defer func() { <-written }()
	if !waitForLaunchedSession("pid-not-written-yet", pidFile, "terminal") {
		t.Fatal("a pid file the launcher had not written yet was taken for a launcher that never started, so the session would be launched a second time headless")
	}
}

func TestALauncherThatNeverWritesItsPidDidNotStart(t *testing.T) {
	sandboxFiles(t)
	ensureDir(files.launches)
	_, _, pidFile := launchFiles("pid-never-written")
	if err := os.WriteFile(pidFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	previous := launchPidTimeout
	launchPidTimeout = 300 * time.Millisecond
	t.Cleanup(func() { launchPidTimeout = previous })
	if waitForLaunchedSession("pid-never-written", pidFile, "terminal") {
		t.Fatal("a pid file that stayed empty was taken for a session that started")
	}
}
