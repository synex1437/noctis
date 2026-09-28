package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// q7LongStep is longer than the 160 characters of an item a Stop reason quotes.
var q7LongStep = "release step 1: " + strings.Repeat("rebuild the installer and check its signature, ", 5) + "then upload it"

// q7GivenUpQueue drives a trusted TASKS.md of 18 items until the queue gives up: the first item is
// long, items 16 and 17 come after the first 15 eligible ones, and item 18 waits on item 17.
func q7GivenUpQueue(t *testing.T, sid string) (object, string) {
	t.Helper()
	cfg, project := queueTrustSandbox(t, false)
	previous := activeHost
	t.Cleanup(func() { activeHost = previous })
	activeHost = "claude"
	t.Setenv("CLAUDE_CODE_STOP_HOOK_BLOCK_CAP", "")
	section(cfg, "queue")["maxIdleContinues"] = float64(3)
	lines := []string{"# release", "- [ ] " + q7LongStep}
	for item := 2; item <= 17; item++ {
		lines = append(lines, fmt.Sprintf("- [ ] release step %d", item))
	}
	lines = append(lines, "- [ ] release step 18 (after 17)")
	trustQueueFile(writeQueueFile(t, project, strings.Join(lines, "\n")+"\n"), true)
	if _, last := stopsInARow(t, cfg, sid, project, 6, nil); !gaveUpOn(sid) {
		t.Fatalf("the queue did not give up after blocks without progress: %v", last)
	}
	if blocked, last := stopsInARow(t, cfg, sid, project, 2, nil); blocked != 0 {
		t.Fatalf("the give-up did not hold with nothing changed: %v", last)
	}
	return cfg, project
}

func TestEditingAnyOpenItemLiftsAGiveUp(t *testing.T) {
	for index, edit := range []struct{ what, old, new string }{
		{"an open item after the first 15", "- [ ] release step 16\n", "- [ ] release step 16 on the staging box\n"},
		{"an item after its first 160 characters", "then upload it\n", "then upload it to the mirror\n"},
		{"an item that waits on another", "- [ ] release step 18 (after 17)\n", "- [ ] release step 18, the announcement (after 17)\n"},
	} {
		sid := fmt.Sprintf("q7-edit-%d", index)
		cfg, project := q7GivenUpQueue(t, sid)
		queuePath := filepath.Join(project, "TASKS.md")
		content, err := os.ReadFile(queuePath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(content), edit.old) != 1 {
			t.Fatalf("%q is not once in the queue", edit.old)
		}
		writeQueueFile(t, project, strings.Replace(string(content), edit.old, edit.new, 1))
		trustQueueFile(queuePath, true)
		if blocked, last := stopsInARow(t, cfg, sid, project, 1, nil); blocked != 1 {
			t.Errorf("editing %s did not lift the give-up: %v (noctis why: %q)", edit.what, last, journaledReason(sid, "allow-stop"))
		}
	}
}
