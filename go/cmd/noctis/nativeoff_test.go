package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestARunnerThatCannotBeNativeAsksNoSchedulerCommand(t *testing.T) {
	for _, off := range []struct{ why, noTasks, remote, method string }{
		{"NOCTIS_NO_TASKS", "1", "", "manual"},
		{"a cloud session", "", "true", "cloud"},
	} {
		t.Run(off.why, func(t *testing.T) {
			sandboxFiles(t)
			t.Setenv("NOCTIS_NO_SCHEDULE", "1")
			t.Setenv("NOCTIS_NO_TASKS", off.noTasks)
			t.Setenv("CLAUDE_CODE_REMOTE", off.remote)
			if runtime.GOOS != "windows" {
				// A systemd-run on PATH is what has a Linux runner ask systemctl whether systemd runs.
				fakeBin := t.TempDir()
				if err := os.WriteFile(filepath.Join(fakeBin, "systemd-run"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
			}
			recorded := withFakeScheduler(t, nil)
			scheduled := scheduleRunner(waitEngineConfig(), "native-off", float64(nowSec()+600))
			if method := getString(scheduled, "method"); method != off.method {
				t.Fatalf("the runner was scheduled via %q, want %q", method, off.method)
			}
			if len(*recorded) > 0 {
				asked := []string{}
				for _, entry := range *recorded {
					asked = append(asked, strings.Join(entry.args, " "))
				}
				t.Fatalf("with native runners off, scheduling still started %v", asked)
			}
		})
	}
}
