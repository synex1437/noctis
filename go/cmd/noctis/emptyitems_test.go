package main

import (
	"os"
	"strings"
	"testing"
)

const emptyItemQueue = "# release\n- [ ] release step 1\n- [ ] \n- [ ] release step 3 (after 2)\n"

func TestAnEmptyChecklistLineIsNotAnItem(t *testing.T) {
	for _, empty := range []string{"- [ ] ", "- [ ]  ", "* [ ]\t", "1. [ ] ", "TODO: "} {
		view := queueSnapshotOf("TASKS.md", strings.Replace(emptyItemQueue, "- [ ] \n", empty+"\n", 1))
		if view.total != 2 || view.blocked != 0 {
			t.Errorf("%q counts as an open item or holds back the item after it: %d open, %d blocked", empty, view.total, view.blocked)
		}
		for _, item := range view.items {
			if strings.TrimSpace(item) == "" {
				t.Errorf("%q is handed on as an item: %q", empty, view.items)
			}
		}
	}
}

func TestTheStopHookSkipsAnEmptyChecklistLineAndNamesItOnce(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	queuePath := writeQueueFile(t, project, emptyItemQueue)
	trustQueueFile(queuePath, true)
	first := stopHookOutput(t, stopInput("empty-line", project), cfg)
	if getString(first, "decision") != "block" {
		t.Fatalf("the queue with two open items was not driven: %v", first)
	}
	reason := getString(first, "reason")
	if !strings.Contains(reason, "2 open") || strings.Contains(reason, `("")`) {
		t.Errorf("Claude is handed the empty line as an item: %q", reason)
	}
	if !strings.Contains(reason, "line 3") {
		t.Errorf("Claude is not told that the empty line at line 3 is not an item: %q", reason)
	}
	if message := getString(first, "systemMessage"); !strings.Contains(message, "empty checklist line") || !strings.Contains(message, "line 3") {
		t.Errorf("the user is not told that the empty line is ignored: %q", message)
	}
	second := stopHookOutput(t, activeStop("empty-line", project), cfg)
	if strings.Contains(getString(second, "systemMessage"), "empty checklist line") || strings.Contains(getString(second, "reason"), "line 3") {
		t.Errorf("the empty line is named again at the next stop: %v", second)
	}
	content, err := os.ReadFile(queuePath)
	if err != nil {
		t.Fatal(err)
	}
	ticked := strings.NewReplacer("- [ ] release step 1", "- [x] release step 1", "- [ ] release step 3", "- [x] release step 3").Replace(string(content))
	if err := os.WriteFile(queuePath, []byte(ticked), 0o644); err != nil {
		t.Fatal(err)
	}
	done := stopHookOutput(t, activeStop("empty-line", project), cfg)
	if getString(done, "decision") == "block" {
		t.Fatalf("a queue whose only open line is empty is still driven: %v", done)
	}
	if message := getString(done, "systemMessage"); !strings.Contains(message, T("queue.doneMessage", "TASKS.md")) {
		t.Errorf("the queue whose real items are all done is not reported finished: %q", message)
	}
}

func TestQueueStatusNamesTheEmptyChecklistLines(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	queuePath := writeQueueFile(t, project, emptyItemQueue)
	previous := args
	t.Cleanup(func() { args = previous })
	args = parseArgs([]string{"queue", "status", "--file", queuePath})
	output := capturedStdout(t, func() { runQueueTrust(cfg, project, "status") })
	if want := T("queue.emptyLines", "TASKS.md", 1, "3"); !strings.Contains(output, want) {
		t.Errorf("noctis queue status does not name the empty line:\n%s\nwant %q", output, want)
	}
}

func TestStartingAFileCountsNoEmptyChecklistLineAsAJob(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	writeJobFile(t, project, "empty.md", "# Plan\n- [ ] \n- [ ] \n")
	refused := startQueue(t, cfg, "empty-start", project, "/noctis:start empty.md")
	if reason := getString(refused, "reason"); getString(refused, "decision") != "block" || !strings.Contains(reason, T("queue.startEmpty", "empty.md")) {
		t.Errorf("a file of empty checklist lines started a queue: %v", refused)
	} else if want := T("queue.emptyLines", "empty.md", 2, "2, 3"); !strings.Contains(reason, want) {
		t.Errorf("the refusal does not name the empty lines: %q, want %q", reason, want)
	}
	writeJobFile(t, project, "one.md", "# Plan\n- [ ] add a login page\n- [ ] \n")
	output := startQueue(t, cfg, "one-start", project, "/noctis:start one.md")
	message := getString(output, "systemMessage")
	if !strings.Contains(message, T("queue.startDetected", 1, "one.md", pluginName)) {
		t.Errorf("/noctis:start counts the empty line as a job: %q", message)
	}
	if want := T("queue.emptyLines", "one.md", 1, "3"); !strings.Contains(message, want) {
		t.Errorf("/noctis:start does not name the empty line by its line in one.md: %q, want %q", message, want)
	}
	stop := stopHookOutput(t, stopInput("one-start", project), cfg)
	if reason := getString(stop, "reason"); getString(stop, "decision") != "block" || !strings.Contains(reason, "1 open") || strings.Contains(reason, `("")`) {
		t.Errorf("the started queue is not driven through its one job alone: %v", stop)
	}
	if strings.Contains(getString(stop, "systemMessage"), "empty checklist line") {
		t.Errorf("the stop names the empty line again, by its line in noctis's copy: %q", getString(stop, "systemMessage"))
	}
}
