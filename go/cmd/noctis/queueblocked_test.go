package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// q6BlockedQueue trusts a TASKS.md whose open items all wait on each other.
func q6BlockedQueue(t *testing.T, project, content string) {
	t.Helper()
	trustQueueFile(writeQueueFile(t, project, content), true)
}

func TestABlockedQueueTellsTheUserOnceUntilItsBlockedItemsChange(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	if !isWindows {
		bin := t.TempDir()
		for _, notifier := range []string{"notify-send", "osascript"} {
			writeScript(t, filepath.Join(bin, notifier), "#!/bin/sh\nexit 0\n")
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	sid := "q6-cycle"
	q6BlockedQueue(t, project, "# cycle\n- [ ] a #a (after #b)\n- [ ] b #b (after #a)\n")
	notices := 0
	for stop := 0; stop < 3; stop++ {
		input := stopInput(sid, project)
		input["stop_hook_active"] = stop > 0
		output := stopHookOutput(t, input, cfg)
		if getString(output, "decision") == "block" {
			t.Fatalf("stop %d: a queue whose items all wait on each other was driven: %v", stop, output)
		}
		if getString(output, "systemMessage") == T("queue.blockedMessage", 2) {
			notices++
		} else if output != nil {
			t.Fatalf("stop %d: unexpected output: %v", stop, output)
		}
	}
	if notices != 1 {
		t.Fatalf("the same blocked queue was announced at %d of 3 stops, want once", notices)
	}
	if told := loggedTimes("notify: " + pluginName + " — " + T("queue.blockedNotify", 2, "TASKS.md")); told != 1 {
		t.Fatalf("the same blocked queue went through the notify path %d times, want once", told)
	}
	if errors := strings.Count(string(readFileOrEmpty(files.errors)), "queue blocked for "+sid); errors != 1 {
		t.Fatalf("the same blocked queue wrote %d error lines, want one", errors)
	}
	if reason := journaledReason(sid, "allow-stop"); reason != "queue blocked" {
		t.Fatalf("noctis why no longer says why the stop was let go: %q", reason)
	}
	q6BlockedQueue(t, project, "# cycle\n- [ ] a #a (after #b)\n- [ ] b #b (after #a)\n- [ ] c (after #a)\n")
	if output := stopHookOutput(t, stopInput(sid, project), cfg); getString(output, "systemMessage") != T("queue.blockedMessage", 3) {
		t.Fatalf("a queue blocked on other items than the ones already announced was not announced: %v", output)
	}
}
