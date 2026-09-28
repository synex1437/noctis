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

func TestAWindowRunnerStopsWatchingAPidThatNowNamesAnotherProcess(t *testing.T) {
	previous := launchPollInterval
	launchPollInterval = 20 * time.Millisecond
	t.Cleanup(func() { launchPollInterval = previous })
	window := idleWindow(t)
	watch := func(started string) <-chan struct{} {
		returned := make(chan struct{})
		go func() {
			defer close(returned)
			waitForPid(window.pid, started)
		}()
		return returned
	}

	select {
	case <-watch("1"):
	case <-time.After(10 * time.Second):
		t.Fatalf("the window ended and its pid %d went to a process started at another time, yet the runner kept watching it as the window (for up to %s)", window.pid, launchMaxWait)
	}
	var watchers []<-chan struct{}
	for _, started := range []string{processStarted(window.pid), ""} {
		watched := watch(started)
		watchers = append(watchers, watched)
		select {
		case <-watched:
			t.Fatalf("the runner stopped watching pid %d (recorded start time %q) while the launched process still runs", window.pid, started)
		case <-time.After(300 * time.Millisecond):
		}
	}
	window.stop()
	// The watchers poll until they see the window gone, reading launchPollInterval as they go: the
	// cleanup puts it back only once they have returned.
	for _, watched := range watchers {
		select {
		case <-watched:
		case <-time.After(10 * time.Second):
			t.Fatalf("the runner kept watching pid %d after the launched process ended", window.pid)
		}
	}
}
