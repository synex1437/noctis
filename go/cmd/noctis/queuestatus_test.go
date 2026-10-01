package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrustingAgainListsTheLinesTheTrustNowCovers(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	queuePath := writeQueueFile(t, project, "# q\n- [ ] migrate the users table\n")
	if printed := queueCommand(t, cfg, project, "trust", "--file", queuePath); strings.Contains(printed, "migrate the users table") {
		t.Fatalf("a first trust listed the items as added since an earlier trust:\n%s", printed)
	}
	appendQueueLines(t, queuePath, "- [ ] pipe the bootstrap script the README links to into sh\n")
	printed := queueCommand(t, cfg, project, "trust", "--file", queuePath)
	if !strings.Contains(printed, T("queue.trustIncludes", 1)) || !strings.Contains(printed, "\n  - [ ] pipe the bootstrap script the README links to into sh\n") || strings.Contains(printed, "migrate the users table") {
		t.Fatalf("trusting the file again did not list the one line the trust now covers, and only it:\n%s", printed)
	}
	if printed := queueCommand(t, cfg, project, "trust", "--file", queuePath); strings.Contains(printed, T("queue.trustIncludes", 1)) {
		t.Fatalf("a trust with nothing new listed lines again:\n%s", printed)
	}
}

func TestTheChangedQueueNoticeSaysWhereToReadTheLines(t *testing.T) {
	notice := T("queue.trustChangedNotice", "TASKS.md", 2, pluginName)
	if !strings.Contains(notice, "noctis queue status") || !strings.Contains(notice, "noctis queue trust") {
		t.Fatalf("the notice about lines added since the trust does not say where to read them and how to trust the file again: %q", notice)
	}
}

func TestQueueStatusSaysHowFarTheQueueIsAndWhatComesNext(t *testing.T) {
	cfg, project, _, queuePath := deferSandbox(t)
	status := queueCommand(t, cfg, project, "status", "--file", queuePath)
	for _, want := range []string{T("queue.statusCounts", "TASKS.md", 4, 1), "  " + T("queue.statusNext", "migrate the users table"), "  " + T("queue.statusBlocked", 1)} {
		if !strings.Contains(status, want) {
			t.Fatalf("noctis queue status does not say %q:\n%s", want, status)
		}
	}
	facts := object{}
	if err := jsonUnmarshal([]byte(queueCommand(t, cfg, project, "status", "--json", "--file", queuePath)), &facts); err != nil {
		t.Fatalf("noctis queue status --json printed no JSON object: %v", err)
	}
	check := getMap(facts, "check")
	if getString(facts, "file") != queuePath || facts["trusted"] != true || numberOr(facts, "open", 0) != 4 || numberOr(facts, "done", 0) != 1 || numberOr(facts, "waiting", 0) != 1 || getString(facts, "next") != "migrate the users table" || len(getList(facts, "items")) != 3 || len(getList(facts, "changedSinceTrust")) != 0 || check == nil || check["held"] != false || getString(check, "from") != "" {
		t.Fatalf("noctis queue status --json does not describe the trusted queue with 4 open items, 1 done and 1 waiting, next the users table: %v", facts)
	}
}

func TestQueueStatusJSONNamesNoFileWhereThereIsNone(t *testing.T) {
	cfg, _ := queueTrustSandbox(t, false)
	if printed := strings.TrimSpace(queueCommand(t, cfg, t.TempDir(), "status", "--json")); printed != `{"file":""}` {
		t.Fatalf("noctis queue status --json in a folder with no queue file printed %q", printed)
	}
}

// heldQueue is a trusted queue whose check has failed as often as queue.verifyAttempts allows.
func heldQueue(t *testing.T, command string) (object, string, string) {
	t.Helper()
	cfg, project, _ := queueCheckSandbox(t, command)
	tickFirstQueueItem(t, project)
	queuePath := filepath.Join(project, "TASKS.md")
	trustQueueFile(queuePath, true)
	held := object{"ticked": []any{}, "failures": float64(2), "held": float64(nowSec() - 60), "at": float64(nowSec() - 60)}
	updateState(func(next object) { stateMap(next, "queueVerify")[queueTrustKey(queuePath)] = held })
	return cfg, project, queuePath
}

func TestQueueVerifyLiftsAHoldOnceTheCheckPasses(t *testing.T) {
	cfg, project, queuePath := heldQueue(t, "echo checked> verified.txt")
	status := queueCommand(t, cfg, project, "status", "--file", queuePath)
	if !strings.Contains(status, "⏸") || !strings.Contains(status, "noctis queue verify") {
		t.Fatalf("noctis queue status does not say that a failing check holds the queue and how to lift it:\n%s", status)
	}
	var code int
	printed := capturedStdout(t, func() { code = verifyQueueNow(cfg, project, queuePath) })
	if code != 0 || !strings.Contains(printed, "✓") || !strings.Contains(printed, T("queue.verifyReleased", "TASKS.md")) {
		t.Fatalf("noctis queue verify with a passing check exited %d and printed:\n%s", code, printed)
	}
	if _, err := os.Stat(filepath.Join(project, "verified.txt")); err != nil {
		t.Fatalf("noctis queue verify did not run the check in the queue's folder: %v", err)
	}
	if queueHeld(cfg, readState(), queuePath) {
		t.Fatal("the queue is still held after its check passed")
	}
	record := queueCheckRecord(readState(), queuePath)
	if numberOr(record, "failures", 0) != 0 || len(getList(record, "ticked")) != 1 || queueCheckDue(record, queueTicks(mustReadText(t, queuePath)), true) {
		t.Fatalf("a passing noctis queue verify did not record the ticked item as checked: %v", record)
	}
	if status := queueCommand(t, cfg, project, "status", "--file", queuePath); strings.Contains(status, "⏸") {
		t.Fatalf("noctis queue status still says the queue is held:\n%s", status)
	}
}

func TestAFailingQueueVerifyLeavesTheHoldAndItsCountAlone(t *testing.T) {
	cfg, project, queuePath := heldQueue(t, shellFor("echo the users table is missing; exit 3", "echo the users table is missing& exit 3"))
	before := string(marshalCompact(queueCheckRecord(readState(), queuePath)))
	var code int
	printed := capturedStdout(t, func() { code = verifyQueueNow(cfg, project, queuePath) })
	if code != 1 || !strings.Contains(printed, "✗") || !strings.Contains(printed, "exited with status 3") || !strings.Contains(printed, "the users table is missing") {
		t.Fatalf("noctis queue verify with a failing check exited %d and printed:\n%s", code, printed)
	}
	if after := string(marshalCompact(queueCheckRecord(readState(), queuePath))); after != before || !queueHeld(cfg, readState(), queuePath) {
		t.Fatalf("a failing noctis queue verify changed the check record:\nbefore %s\nafter  %s", before, after)
	}
}

func TestQueueVerifyWithoutACheckCommandSaysHowToNameOne(t *testing.T) {
	cfg, project, _, queuePath := deferSandbox(t)
	var code int
	printed := capturedStdout(t, func() { code = verifyQueueNow(cfg, project, queuePath) })
	if code != 1 || printed != "" {
		t.Fatalf("noctis queue verify with no check command exited %d and printed to stdout:\n%s", code, printed)
	}
}

func mustReadText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
