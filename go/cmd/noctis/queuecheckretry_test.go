package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestACheckKilledOnceRunsAgainAtOnceAndItsPassCounts(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, shellFor(
		"echo run >> runs.txt; if [ -f once ]; then echo ok; else touch once; exit 137; fi",
		"echo run>> runs.txt & if exist once (echo ok) else (type nul> once & exit 137)"))
	tickFirstQueueItem(t, project)
	output := stopHookOutput(t, stopInput("qr1", frontend), cfg)
	if reason := getString(output, "reason"); getString(output, "decision") != "block" || !strings.Contains(reason, "Queue continues: 1 open") || strings.Contains(reason, "Queue check failed") {
		t.Fatalf("a check killed once that passed on its second run did not let the queue go on: %v", output)
	}
	if runs := queueCheckRuns(project); runs != 2 {
		t.Fatalf("the check ran %d times, want 2: the run that was killed, then the one that passed", runs)
	}
	if record := queueCheckRecord(readState(), filepath.Join(project, "TASKS.md")); numberOr(record, "failures", 0) != 0 || len(getList(record, "ticked")) != 1 {
		t.Fatalf("the pass on the second run is not recorded as a pass: %v", record)
	}
}

func TestACheckKilledOnEveryRunCountsOnceItRanTwice(t *testing.T) {
	command := shellFor("echo run >> runs.txt; exit 137", "echo run>> runs.txt & exit 137")
	cfg, project, frontend := queueCheckSandbox(t, command)
	section(cfg, "queue")["escalate"] = "off"
	tickFirstQueueItem(t, project)
	first := stopHookOutput(t, stopInput("qr2", frontend), cfg)
	if reason := getString(first, "reason"); getString(first, "decision") != "block" || !strings.Contains(reason, "Queue check failed") || !strings.Contains(reason, "exited with status 137") {
		t.Fatalf("a check killed on both runs did not count as a failed attempt: %v", first)
	}
	if runs := queueCheckRuns(project); runs != 2 {
		t.Fatalf("the check ran %d times before it counted, want 2", runs)
	}
	second := stopHookOutput(t, stopAgain("qr2", frontend), cfg)
	if want := T("queue.heldMessage", command, 2, "TASKS.md"); getString(second, "systemMessage") != want {
		t.Fatalf("the second failed attempt in a row did not hold the queue: %v", second)
	}
	if runs := queueCheckRuns(project); runs != 3 {
		t.Fatalf("the check ran %d times, want 3: a run after a failed attempt counts at once", runs)
	}
}

func TestACheckCutByItsTimeoutRunsAgainAtTheNextStopWhileTheQueueGoesOn(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, shellFor("echo run >> runs.txt; sleep 30", "echo run>> runs.txt & ping -n 31 127.0.0.1 > nul"))
	section(cfg, "queue")["verifyTimeoutSeconds"] = float64(1)
	tickFirstQueueItem(t, project)
	path := filepath.Join(project, "TASKS.md")
	first := stopHookOutput(t, stopInput("qr3", frontend), cfg)
	if reason := getString(first, "reason"); getString(first, "decision") != "block" || !strings.Contains(reason, "Queue continues: 1 open") || strings.Contains(reason, "Queue check failed") {
		t.Fatalf("a check cut short by its timeout counted at once, or the queue did not go on: %v", first)
	}
	record := queueCheckRecord(readState(), path)
	if numberOr(record, "failures", 0) != 0 || numberOr(record, "retried", 0) == 0 || numberOr(record, "rerun", 0) == 0 {
		t.Fatalf("the run cut short is not recorded as one to run again: %v", record)
	}
	content, _ := readQueueText(path)
	if line, want := digestCheck(cfg, readState(), path, content), T("digest.checkCut", formatTime(numberOr(record, "retried", 0))); line != want {
		t.Fatalf("the digest says %q of a check cut short, want %q", line, want)
	}
	second := stopHookOutput(t, stopAgain("qr3", frontend), cfg)
	if reason := getString(second, "reason"); getString(second, "decision") != "block" || !strings.Contains(reason, "Queue check failed") || !strings.Contains(reason, "did not finish within 1 s") {
		t.Fatalf("the check cut short again at the next stop did not count as a failed attempt: %v", second)
	}
	if runs := queueCheckRuns(project); runs != 2 {
		t.Fatalf("the check ran %d times over two stops, want 2", runs)
	}
	if record := queueCheckRecord(readState(), path); numberOr(record, "failures", 0) != 1 || numberOr(record, "retried", 0) != 0 || numberOr(record, "rerun", 0) != 0 {
		t.Fatalf("the second run cut short is not one failed attempt: %v", record)
	}
}

func TestTicksNoCheckPassedAreCountedInTheStatusAndTheDigest(t *testing.T) {
	cfg, project, _, path, _ := digestSandbox(t, "21:00")
	now := nowSec()
	none := T("queue.unverifiedNone", 1)
	if status := queueCommand(t, cfg, project, "status"); !strings.Contains(status, "  "+none) {
		t.Fatalf("queue status of a queue nothing checks does not count its ticked item:\n%s", status)
	}
	if body := buildDigest(cfg, readState(), currentUsage(now), nil, now).body; !strings.Contains(body, "- "+none) {
		t.Fatalf("the digest does not count the ticked item no check passed:\n%s", body)
	}
	if status := describeState(cfg, readState(), currentUsage(now), now); !strings.Contains(status, "  "+none) {
		t.Fatalf("noctis status does not count the ticked item no check passed:\n%s", status)
	}

	section(cfg, "queue")["verifyCommand"] = "make check"
	updateState(func(next object) {
		stateMap(next, "queueVerify")[queueTrustKey(path)] = object{"ticked": []any{queueItemDigest("set up the repository")}, "at": float64(now - 60)}
	})
	content, _ := readQueueText(path)
	if text := unverifiedText(cfg, readState(), path, content); text != "" {
		t.Fatalf("every ticked item passed the check, yet the count says %q", text)
	}

	writeQueueFile(t, project, strings.Replace(digestTasks, "- [ ] migrate the users table", "- [x] migrate the users table", 1))
	wait := T("queue.unverifiedWait", 1, 2, pluginName)
	if status := queueCommand(t, cfg, project, "status"); !strings.Contains(status, "  "+wait) {
		t.Fatalf("queue status does not count the item ticked since the check passed:\n%s", status)
	}
	facts := object{}
	if err := jsonUnmarshal([]byte(queueCommand(t, cfg, project, "status", "--json")), &facts); err != nil {
		t.Fatal(err)
	}
	if check := getMap(facts, "check"); numberOr(check, "unverified", -1) != 1 || numberOr(check, "ticked", -1) != 2 {
		t.Fatalf("noctis queue status --json gives the check as %v", check)
	}
	if body := buildDigest(cfg, readState(), currentUsage(now), nil, now).body; !strings.Contains(body, "- "+wait) {
		t.Fatalf("the digest does not count the item ticked since the check passed:\n%s", body)
	}
	if status := describeState(cfg, readState(), currentUsage(now), now); !strings.Contains(status, "  "+wait) {
		t.Fatalf("noctis status does not count the item ticked since the check passed:\n%s", status)
	}
}

func TestQueueTrustAndStatusSuggestTheProjectsOwnCheck(t *testing.T) {
	cfg, project, _ := queueCheckSandbox(t, "")
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/tasks\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	suggestion := T("queue.verifySuggest", "go test ./...", "go test ./...", "TASKS.md", pluginName)
	if granted := queueCommand(t, cfg, project, "trust"); !strings.Contains(granted, suggestion) {
		t.Fatalf("noctis queue trust does not suggest the check the project seems to use:\n%s", granted)
	}
	if status := queueCommand(t, cfg, project, "status"); !strings.Contains(status, suggestion) {
		t.Fatalf("noctis queue status does not suggest the check the project seems to use:\n%s", status)
	}
	facts := object{}
	if err := jsonUnmarshal([]byte(queueCommand(t, cfg, project, "status", "--json")), &facts); err != nil {
		t.Fatal(err)
	}
	if suggested := getString(getMap(facts, "check"), "suggested"); suggested != "go test ./..." {
		t.Fatalf("noctis queue status --json suggests %q", suggested)
	}

	section(cfg, "queue")["verifyCommand"] = "make check"
	if status := queueCommand(t, cfg, project, "status"); strings.Contains(status, suggestion) {
		t.Fatalf("with queue.verifyCommand set, queue status still suggests another check:\n%s", status)
	}
	section(cfg, "queue")["verifyCommand"], section(cfg, "queue")["fileVerify"] = "", false
	if status := queueCommand(t, cfg, project, "status"); strings.Contains(status, suggestion) {
		t.Fatalf("with queue.fileVerify off, queue status suggests a line it would never run:\n%s", status)
	}
}

func TestAFailureIsKnownAgainWhateverItsTimingsAndTemporaryPaths(t *testing.T) {
	outcome := "exited with status 1"
	first := checkFailure(outcome, "--- FAIL: TestMigrate (0.01s)\n    users_test.go:42: got 3 rows, want 2\nFAIL\texample.com/users\t0.123s\n12:03:44 wrote /tmp/TestMigrate123/001/out.json at 0xc000123456")
	again := checkFailure(outcome, "--- FAIL: TestMigrate (0.03s)\n    users_test.go:42: got 3 rows, want 2\nFAIL\texample.com/users\t0.456s\n12:04:10 wrote /tmp/TestMigrate987/001/out.json at 0xc000abcdef")
	if first == "" || first != again {
		t.Fatalf("two runs that fail the same way, apart from timings, clock times, addresses and temporary folders, gave %q and %q", first, again)
	}
	if other := checkFailure(outcome, "--- FAIL: TestMigrate (0.01s)\n    users_test.go:42: got 4 rows, want 2\nFAIL\texample.com/users\t0.123s"); other == first {
		t.Fatal("a failure with another count is known as the same one")
	}
	if status := checkFailure("exited with status 2", "--- FAIL: TestMigrate (0.01s)\n    users_test.go:42: got 3 rows, want 2\nFAIL\texample.com/users\t0.123s\n12:03:44 wrote /tmp/TestMigrate123/001/out.json at 0xc000123456"); status == first {
		t.Fatal("a failure with another exit status is known as the same one")
	}
	if silent := checkFailure(outcome, " \n"); silent != "" {
		t.Fatalf("a check that printed nothing has a failure to compare: %q", silent)
	}
}

func TestACheckThatFailsTheSameWayTwiceHoldsTheQueueBeforeItsAttemptsRunOut(t *testing.T) {
	command := shellFor(`echo run >> runs.txt; echo "FAIL TestMigrate in $$ms"; exit 1`, "echo run>> runs.txt & echo FAIL TestMigrate in %RANDOM%ms & exit 1")
	cfg, project, frontend := queueCheckSandbox(t, command)
	section(cfg, "queue")["escalate"] = "off"
	section(cfg, "queue")["verifyAttempts"] = float64(3)
	tickFirstQueueItem(t, project)
	first := stopHookOutput(t, stopInput("qs1", frontend), cfg)
	if getString(first, "decision") != "block" || !strings.Contains(getString(first, "reason"), "Queue check failed") {
		t.Fatalf("the first failed run did not send Claude back to fix it: %v", first)
	}
	second := stopHookOutput(t, stopAgain("qs1", frontend), cfg)
	if want := T("queue.heldMessage", command, 2, "TASKS.md") + " " + T("queue.heldSame"); getString(second, "systemMessage") != want {
		t.Fatalf("a second run that failed the same way did not hold the queue with a word on why (want %q): %v", want, second)
	}
	if record := queueCheckRecord(readState(), filepath.Join(project, "TASKS.md")); numberOr(record, "held", 0) == 0 || numberOr(record, "failures", 0) != 2 {
		t.Fatalf("the hold after the same failure twice is not recorded: %v", record)
	}
}

func TestACheckThatFailsAnotherWayEachTimeKeepsItsAttempts(t *testing.T) {
	// Each run prints one line more than the run before it. cmd's %RANDOM% would not do: cmd seeds it with
	// the clock in seconds, so two runs started within the same second print the same number.
	command := shellFor(`echo run >> runs.txt; echo "FAIL TestStep$(wc -l < runs.txt)"; exit 1`, "echo run>> runs.txt & type runs.txt & echo FAIL TestStep & exit 1")
	cfg, project, frontend := queueCheckSandbox(t, command)
	section(cfg, "queue")["escalate"] = "off"
	section(cfg, "queue")["verifyAttempts"] = float64(3)
	tickFirstQueueItem(t, project)
	for round := 1; round <= 2; round++ {
		input := stopAgain("qs2", frontend)
		if round == 1 {
			input = stopInput("qs2", frontend)
		}
		if output := stopHookOutput(t, input, cfg); getString(output, "decision") != "block" || !strings.Contains(getString(output, "reason"), "Queue check failed") {
			t.Fatalf("failed run %d, each failing another way, did not send Claude back: %v", round, output)
		}
	}
	third := stopHookOutput(t, stopAgain("qs2", frontend), cfg)
	if want := T("queue.heldMessage", command, 3, "TASKS.md"); getString(third, "systemMessage") != want {
		t.Fatalf("the third failed run did not hold the queue as queue.verifyAttempts says: %v", third)
	}
}

func TestAPerItemCheckCutShortIsTheOneNamedUntilItRunsAgain(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, "make check")
	each := shellFor("sleep 30", "ping -n 31 127.0.0.1 > nul")
	section(cfg, "queue")["verifyEachCommand"] = each
	section(cfg, "queue")["verifyTimeoutSeconds"] = float64(1)
	tickFirstQueueItem(t, project)
	continues(t, stopHookOutput(t, stopInput("qr6", frontend), cfg), 1)
	path := filepath.Join(project, "TASKS.md")
	if command := recordedCheckCommand(cfg, queueCheckRecord(readState(), path), path, ""); command != each {
		t.Fatalf("after the per-item check was cut short the record names %q, want the per-item check", command)
	}
}

func TestAQueueInDotClaudeOrDocsIsSuggestedTheCheckOfTheProjectFolderItRunsIn(t *testing.T) {
	for _, folder := range []string{".claude", "docs"} {
		cfg, project, _ := queueCheckSandbox(t, "")
		for _, dir := range []string{".claude", "docs"} {
			if err := os.MkdirAll(filepath.Join(project, dir), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		for file, content := range map[string]string{"go.mod": "module example.com/tasks\n", "docs/package.json": `{"scripts": {"test": "vitepress build"}}`} {
			if err := os.WriteFile(filepath.Join(project, file), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Rename(filepath.Join(project, "TASKS.md"), filepath.Join(project, folder, "TASKS.md")); err != nil {
			t.Fatal(err)
		}
		suggestion := T("queue.verifySuggest", "go test ./...", "go test ./...", "TASKS.md", pluginName)
		if granted := queueCommand(t, cfg, project, "trust"); !strings.Contains(granted, suggestion) {
			t.Fatalf("noctis queue trust does not suggest the check of the project %s/TASKS.md is in:\n%s", folder, granted)
		}
		if status := queueCommand(t, cfg, project, "status"); !strings.Contains(status, suggestion) {
			t.Fatalf("noctis queue status does not suggest the check of the project %s/TASKS.md is in:\n%s", folder, status)
		}
	}
}
