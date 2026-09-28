package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// r11ProcessState is the state letter of pid in /proc (Z for a zombie), and false once the pid is
// gone: reaped.
func r11ProcessState(pid int) (byte, bool) {
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, false
	}
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 || end+2 >= len(stat) {
		return '?', true
	}
	return stat[end+2], true
}

func r11AwaitReaped(t *testing.T, what string, pid int) {
	t.Helper()
	state := byte('?')
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		current, present := r11ProcessState(pid)
		if !present {
			return
		}
		state = current
	}
	if state == 'Z' {
		t.Fatalf("%s (pid %d) exited and stays a zombie of this process: a hook that waits for hours gathers one for every hand-off", what, pid)
	}
	t.Fatalf("%s (pid %d) is still there after 10 s (state %c)", what, pid, state)
}

func TestProcessesALongLivedNoctisHandsOffAreReapedWhenTheyExit(t *testing.T) {
	sandboxFiles(t)
	// The detached noctis processes here are this test binary, which runs `noctis --version`.
	t.Setenv("NOCTIS_TEST_MAIN_ARGS", `["--version"]`)

	t.Run("a detached noctis", func(t *testing.T) {
		pid := detachedSelf([]string{"--version"})
		if pid <= 0 {
			t.Fatal("no detached noctis was started")
		}
		r11AwaitReaped(t, "a detached noctis", pid)
	})
	t.Run("a refresher a fetch was handed to", func(t *testing.T) {
		now := nowSec()
		if _, handled := refreshAside(loadedConfig(), object{"fetchedAt": float64(now)}, now, "r11", 0); !handled {
			t.Fatal("no refresher was started")
		}
		owner, _, present := lockHolder(files.fableLock)
		pid, err := strconv.Atoi(owner)
		if !present || err != nil {
			t.Fatalf("fable.lock was not handed to a refresher (holder %q)", owner)
		}
		r11AwaitReaped(t, "the refresher", pid)
	})
	t.Run("a desktop notifier", func(t *testing.T) {
		bin := t.TempDir()
		record := filepath.Join(bin, "pid")
		writeScript(t, filepath.Join(bin, "notify-send"), "#!/bin/sh\necho $$ > '"+record+"'\n")
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		if reason := notify(object{}, pluginName, "r11"); reason != "" {
			t.Fatalf("no notification was shown: %s", reason)
		}
		if !awaitMarker(record, 10*time.Second, 20*time.Millisecond) {
			t.Fatal("the notifier never ran")
		}
		time.Sleep(50 * time.Millisecond)
		written, _ := os.ReadFile(record)
		pid, err := strconv.Atoi(strings.TrimSpace(string(written)))
		if err != nil {
			t.Fatalf("the notifier wrote no pid: %q", written)
		}
		r11AwaitReaped(t, "the notifier", pid)
	})
}
