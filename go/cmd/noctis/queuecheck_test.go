package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func queueCheckSandbox(t *testing.T, command string) (object, string, string) {
	t.Helper()
	cfg, project, frontend := projectQueueSandbox(t)
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	if !isWindows {
		bin := t.TempDir()
		for _, notifier := range []string{"notify-send", "osascript"} {
			writeScript(t, filepath.Join(bin, notifier), "#!/bin/sh\nexit 0\n")
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	section(cfg, "queue")["verifyCommand"] = command
	return cfg, project, frontend
}

func shellFor(unix, windows string) string {
	if isWindows {
		return windows
	}
	return unix
}

func tickFirstQueueItem(t *testing.T, project string) {
	t.Helper()
	writeQueueFile(t, project, "# q\n- [x] migrate the users table\n- [ ] write the release notes\n")
}

func queueCheckRuns(project string) int {
	content, err := os.ReadFile(filepath.Join(project, "runs.txt"))
	if err != nil {
		return 0
	}
	return strings.Count(string(content), "run")
}

func stopAgain(sid, cwd string) object {
	return object{"hook_event_name": "Stop", "session_id": sid, "cwd": cwd, "stop_hook_active": true}
}

func TestAPassingQueueCheckLetsTheQueueContinue(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, "echo checked> verified.txt")
	tickFirstQueueItem(t, project)
	output := stopHookOutput(t, stopInput("qc1", frontend), cfg)
	if reason := getString(output, "reason"); getString(output, "decision") != "block" || !strings.Contains(reason, "Queue continues: 1 open in TASKS.md") || !strings.Contains(reason, "write the release notes") {
		t.Fatalf("a passing queue check did not let the queue continue with its next item: %v", output)
	}
	if _, err := os.Stat(filepath.Join(project, "verified.txt")); err != nil {
		t.Fatalf("queue.verifyCommand did not run in the project folder before the queue went on to its next item: %v", err)
	}
	if _, err := os.Stat(filepath.Join(frontend, "verified.txt")); err == nil {
		t.Fatal("queue.verifyCommand ran in the subfolder Claude had changed into, not in the queue's project folder")
	}
	if actions := journaledFor("qc1"); !slices.Contains(actions, "verify-queue") || !slices.Contains(actions, "continue-queue") {
		t.Fatalf("the passing check is not in the decision journal next to the continuation: %v", actions)
	}
}

func TestAFailingQueueCheckBlocksWithItsOutputTailAndNoNextItem(t *testing.T) {
	command := shellFor("cat check.log; exit 3", "type check.log & exit 3")
	cfg, project, frontend := queueCheckSandbox(t, command)
	lines := []string{}
	for index := 1; index <= 60; index++ {
		lines = append(lines, fmt.Sprintf("check line %03d", index))
	}
	if err := os.WriteFile(filepath.Join(project, "check.log"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tickFirstQueueItem(t, project)
	output := stopHookOutput(t, stopInput("qc2", frontend), cfg)
	reason := getString(output, "reason")
	if getString(output, "decision") != "block" || !strings.HasPrefix(reason, "[noctis]") || !strings.Contains(reason, "`"+command+"`") || !strings.Contains(reason, "exited with status 3") {
		t.Fatalf("a failing queue check did not block with a [noctis] reason naming the command and its exit status: %v", output)
	}
	if !strings.Contains(reason, "check line 060") || !strings.Contains(reason, "check line 021") || strings.Contains(reason, "check line 020") {
		t.Fatalf("the reason does not carry the last 40 lines of the command's output: %q", reason)
	}
	if strings.Contains(reason, "write the release notes") || strings.Contains(reason, queueContinuesPrefix) {
		t.Fatalf("a failing queue check still handed Claude the next item: %q", reason)
	}
	if !strings.Contains(reason, "before you start another item") || !strings.Contains(reason, "unticked until the command passes") {
		t.Fatalf("the reason does not tell Claude to fix the failure first and to leave its item unticked until the command passes: %q", reason)
	}
	if actions := journaledFor("qc2"); !slices.Contains(actions, "verify-queue") || slices.Contains(actions, "continue-queue") {
		t.Fatalf("the failed check is not in the decision journal, or the queue was journaled as continued: %v", actions)
	}
}

func TestExhaustedQueueCheckAttemptsAllowTheStopTellTheUserAndHoldTheQueue(t *testing.T) {
	command := shellFor("echo run >> runs.txt; cat fixed.txt", "echo run>> runs.txt & type fixed.txt")
	cfg, project, frontend := queueCheckSandbox(t, command)
	tickFirstQueueItem(t, project)
	if first := stopHookOutput(t, stopInput("qc3", frontend), cfg); getString(first, "decision") != "block" || strings.Contains(getString(first, "reason"), queueContinuesPrefix) {
		t.Fatalf("the first failure of the queue check did not send Claude back to fix it: %v", first)
	}
	second := stopHookOutput(t, stopAgain("qc3", frontend), cfg)
	if getString(second, "decision") == "block" {
		t.Fatalf("the second failure in a row, with queue.verifyAttempts 2, still kept Claude going: %v", second)
	}
	if want := T("queue.heldMessage", command, 2, "TASKS.md"); getString(second, "systemMessage") != want {
		t.Fatalf("the user was not told the queue is held:\n got %q\nwant %q", getString(second, "systemMessage"), want)
	}
	if told := loggedTimes("notify: " + pluginName + " — " + T("queue.heldNotify", command, 2, "TASKS.md")); told != 1 {
		t.Fatalf("the hold went through the notify path %d times, want once", told)
	}
	if runs := queueCheckRuns(project); runs != 2 {
		t.Fatalf("the check ran %d times over two stops, want 2", runs)
	}
	if later := stopHookOutput(t, stopAgain("qc3", frontend), cfg); later != nil {
		t.Fatalf("a later stop continued the held queue or told the user again: %v", later)
	}
	if later := stopHookOutput(t, stopInput("qc3", frontend), cfg); later != nil {
		t.Fatalf("a later stop continued the held queue or told the user again: %v", later)
	}
	if runs := queueCheckRuns(project); runs != 2 {
		t.Fatalf("stops without a prompt in between ran the check of the held queue again (%d runs)", runs)
	}
	start := hookOutput(t, onSessionStart, object{"hook_event_name": "SessionStart", "source": "compact", "session_id": "qc3-next", "cwd": frontend}, cfg)
	if strings.Contains(contextOf(start), "Queue mode") || strings.Contains(getString(start, "systemMessage"), "TASKS.md") {
		t.Fatalf("a session start handed the held queue to Claude again: %v", start)
	}
	hookOutput(t, onUserPromptSubmit, promptInput("qc3", frontend, "the fixture is back in place, carry on"), cfg)
	if err := os.WriteFile(filepath.Join(project, "fixed.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resumed := stopHookOutput(t, stopInput("qc3", frontend), cfg)
	if getString(resumed, "decision") != "block" || !strings.Contains(getString(resumed, "reason"), "Queue continues: 1 open in TASKS.md") {
		t.Fatalf("after a prompt, the stop whose check passed did not continue the queue: %v", resumed)
	}
	if runs := queueCheckRuns(project); runs != 3 {
		t.Fatalf("the check ran %d times, want 3: once more at the first stop after the prompt", runs)
	}
	if again := hookOutput(t, onSessionStart, object{"hook_event_name": "SessionStart", "source": "compact", "session_id": "qc3-last", "cwd": frontend}, cfg); !strings.Contains(contextOf(again), "Queue mode (TASKS.md: 1 open)") {
		t.Fatalf("once the check passed, a session start did not get the queue directive back: %v", again)
	}
	if actions := journaledFor("qc3"); !slices.Contains(actions, "verify-queue") || !slices.Contains(actions, "hold-queue") || !slices.Contains(actions, "allow-stop") {
		t.Fatalf("the failed checks, the hold and the stops it allowed are not in the decision journal: %v", actions)
	}
}

func TestNoTickSinceTheLastQueueCheckRunsNothing(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, shellFor("echo run >> runs.txt", "echo run>> runs.txt"))
	if output := stopHookOutput(t, stopInput("qc4", frontend), cfg); !strings.Contains(getString(output, "reason"), "Queue continues: 2 open") {
		t.Fatalf("a queue nothing was ticked in did not continue: %v", output)
	}
	if runs := queueCheckRuns(project); runs != 0 {
		t.Fatalf("the check ran %d time(s) although no item was ticked yet", runs)
	}
	tickFirstQueueItem(t, project)
	if output := stopHookOutput(t, stopInput("qc4", frontend), cfg); !strings.Contains(getString(output, "reason"), "Queue continues: 1 open") {
		t.Fatalf("the queue did not continue after a passing check: %v", output)
	}
	if runs := queueCheckRuns(project); runs != 1 {
		t.Fatalf("the check ran %d time(s) at the stop after an item was ticked, want 1", runs)
	}
	for round := 0; round < 2; round++ {
		if output := stopHookOutput(t, stopInput("qc4", frontend), cfg); !strings.Contains(getString(output, "reason"), "Queue continues: 1 open") {
			t.Fatalf("the queue did not continue: %v", output)
		}
	}
	if runs := queueCheckRuns(project); runs != 1 {
		t.Fatalf("stops with no tick since the last check ran it again (%d runs)", runs)
	}
	writeQueueFile(t, project, "# q\n- [x] migrate the users table\n- [x] write the release notes\n- [ ] tag the release\n")
	trustQueueFile(filepath.Join(project, "TASKS.md"), true)
	if output := stopHookOutput(t, stopInput("qc4", frontend), cfg); !strings.Contains(getString(output, "reason"), "Queue continues: 1 open") {
		t.Fatalf("the queue did not continue: %v", output)
	}
	if runs := queueCheckRuns(project); runs != 2 {
		t.Fatalf("the next tick did not run the check again (%d runs)", runs)
	}
}

func TestTheEmptyQueueCheckDefaultRunsNothingAndChangesNothing(t *testing.T) {
	cfg, project, frontend := projectQueueSandbox(t)
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	queue := section(cfg, "queue")
	if command, ok := queue["verifyCommand"].(string); !ok || command != "" || numberOr(queue, "verifyTimeoutSeconds", 0) != 900 || numberOr(queue, "verifyAttempts", 0) != 2 {
		t.Fatalf("the shipped queue settings are not verifyCommand \"\", verifyTimeoutSeconds 900 and verifyAttempts 2: %v", queue)
	}
	tickFirstQueueItem(t, project)
	queuePath := filepath.Join(project, "TASKS.md")
	stale := object{"ticked": []any{}, "failures": float64(3), "held": float64(nowSec() - 60), "at": float64(nowSec() - 60)}
	updateState(func(next object) { stateMap(next, "queueVerify")[queueTrustKey(queuePath)] = stale })
	before := string(marshalCompact(getMap(readState(), "queueVerify")))
	output := stopHookOutput(t, stopInput("qc5", frontend), cfg)
	if reason := getString(output, "reason"); getString(output, "decision") != "block" || !strings.HasPrefix(reason, queueContinuesPrefix+": 1 open in TASKS.md. Take the next eligible item (\"write the release notes\")") {
		t.Fatalf("with the empty default the Stop hook no longer continues the queue as it did: %v", output)
	}
	if actions := journaledFor("qc5"); slices.Contains(actions, "verify-queue") || slices.Contains(actions, "hold-queue") || slices.Contains(actions, "allow-stop") {
		t.Fatalf("with the empty default a check was journaled: %v", actions)
	}
	if after := string(marshalCompact(getMap(readState(), "queueVerify"))); after != before {
		t.Fatalf("with the empty default the Stop hook changed the check records:\nbefore %s\nafter  %s", before, after)
	}
	start := hookOutput(t, onSessionStart, object{"hook_event_name": "SessionStart", "source": "compact", "session_id": "qc5-next", "cwd": frontend}, cfg)
	if !strings.Contains(contextOf(start), "Queue mode (TASKS.md: 1 open)") {
		t.Fatalf("with the empty default a hold left from an earlier command kept the queue directive from a session start: %v", start)
	}
}

func TestObserveModeJournalsADueQueueCheckAndRunsNothing(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, "echo checked> verified.txt")
	previous := observing
	t.Cleanup(func() { observing = previous })
	observing = true
	tickFirstQueueItem(t, project)
	if output := stopHookOutput(t, stopInput("qc6", frontend), cfg); output != nil {
		t.Fatalf("observe mode acted on the queue: %v", output)
	}
	if _, err := os.Stat(filepath.Join(project, "verified.txt")); err == nil {
		t.Fatal("observe mode ran queue.verifyCommand")
	}
	if actions := journaledFor("qc6"); !slices.Contains(actions, "would-verify-queue") {
		t.Fatalf("observe mode did not journal the check it would run: %v", actions)
	}
	if records := getMap(readState(), "queueVerify"); len(records) != 0 {
		t.Fatalf("observe mode wrote a check record: %v", records)
	}
}

func TestAQueueCheckThatOutlivesItsTimeoutIsStoppedWithWhatItStarted(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, shellFor("sleep 30 & echo $! > sleeper.pid; wait", "ping -n 31 127.0.0.1 > nul"))
	section(cfg, "queue")["verifyTimeoutSeconds"] = float64(1)
	tickFirstQueueItem(t, project)
	started := time.Now()
	output := stopHookOutput(t, stopInput("qc7", frontend), cfg)
	if elapsed := time.Since(started); elapsed > 15*time.Second {
		t.Fatalf("the Stop hook waited %s for a check whose timeout is 1 s", elapsed.Round(time.Millisecond))
	}
	if getString(output, "decision") != "block" || !strings.Contains(getString(output, "reason"), "did not finish within 1 s and was stopped") {
		t.Fatalf("a check that ran past its timeout was not reported as a failure: %v", output)
	}
	if isWindows {
		return
	}
	content, err := os.ReadFile(filepath.Join(project, "sleeper.pid"))
	pid, _ := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil || pid <= 0 {
		t.Fatalf("the check did not record the process it started: %v %q", err, content)
	}
	deadline := time.Now().Add(5 * time.Second)
	for processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if processAlive(pid) {
		t.Fatalf("the process the timed-out check started (pid %d) is still running", pid)
	}
}

func TestAQueueCheckNeverRunsPastTheStopHooksOwnTimeout(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, shellFor("sleep 30", "ping -n 31 127.0.0.1 > nul"))
	budgets := hookBudgets["claude"]
	previousBudget, previousEvent := budgets["Stop"], activeEvent
	t.Cleanup(func() { budgets["Stop"], activeEvent = previousBudget, previousEvent })
	budgets["Stop"], activeEvent = hookBudgetSlackSeconds+1, "Stop"
	tickFirstQueueItem(t, project)
	started := time.Now()
	output := stopHookOutput(t, stopInput("qc8", frontend), cfg)
	if elapsed := time.Since(started); elapsed > 15*time.Second {
		t.Fatalf("the check ran %s in a Stop hook with %ss left before its own timeout", elapsed.Round(time.Millisecond), formatNumber(hookBudgetSlackSeconds+1))
	}
	if !strings.Contains(getString(output, "reason"), "did not finish within 1 s") {
		t.Fatalf("the check was not cut to what the Stop hook had left: %v", output)
	}
}

func TestUntickingAfterAFailedCheckIsNotAStopWithoutProgress(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, shellFor("cat fixed.txt", "type fixed.txt"))
	t.Setenv("CLAUDE_CODE_STOP_HOOK_BLOCK_CAP", "")
	section(cfg, "queue")["maxIdleContinues"] = float64(4)
	if output := stopHookOutput(t, stopInput("qc9", frontend), cfg); !strings.Contains(getString(output, "reason"), "Queue continues: 2 open") {
		t.Fatalf("the queue did not continue: %v", output)
	}
	for round := 0; round < 3; round++ {
		if output := stopHookOutput(t, stopAgain("qc9", frontend), cfg); !strings.Contains(getString(output, "reason"), "Queue continues: 2 open") {
			t.Fatalf("a stop while Claude was still on the first item did not continue the queue: %v", output)
		}
	}
	tickFirstQueueItem(t, project)
	if output := stopHookOutput(t, stopAgain("qc9", frontend), cfg); !strings.Contains(getString(output, "reason"), "Queue check failed") {
		t.Fatalf("the failing check did not send Claude back to fix it: %v", output)
	}
	writeQueueFile(t, project, "# q\n- [ ] migrate the users table\n- [ ] write the release notes\n")
	if err := os.WriteFile(filepath.Join(project, "fixed.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	output := stopHookOutput(t, stopAgain("qc9", frontend), cfg)
	if getString(output, "decision") != "block" || !strings.Contains(getString(output, "reason"), "Queue continues: 2 open") {
		t.Fatalf("Claude fixed the failure and left its item unticked as told, yet the stop ended the queue as not progressing instead of checking again: %v", output)
	}
	if actions := journaledFor("qc9"); slices.Contains(actions, "allow-stop") {
		t.Fatalf("the queue was given up on: %v", actions)
	}
}

func TestAQueueCheckStaysInsideTheHookTimeCapNoctisLearned(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, shellFor("sleep 30", "ping -n 31 127.0.0.1 > nul"))
	updateState(func(next object) { next["hookCapSeconds"] = float64(61) })
	tickFirstQueueItem(t, project)
	started := time.Now()
	output := stopHookOutput(t, stopInput("qc10", frontend), cfg)
	if elapsed := time.Since(started); elapsed > 15*time.Second {
		t.Fatalf("the check ran %s although in-hook waits showed hooks end after 61 s, with 60 s of that kept back", elapsed.Round(time.Millisecond))
	}
	if !strings.Contains(getString(output, "reason"), "did not finish within 1 s") {
		t.Fatalf("the check was not cut to the hook time cap noctis learned: %v", output)
	}
}

func TestAWaitInPlaceInTheStopHookKeepsTheQueueCheckTimeFree(t *testing.T) {
	dir, _ := strandedSandbox(t)
	budgets := hookBudgets["claude"]
	previousBudget, previousHost, previousEvent := budgets["Stop"], activeHost, activeEvent
	t.Cleanup(func() { budgets["Stop"], activeHost, activeEvent = previousBudget, previousHost, previousEvent })
	budgets["Stop"], activeHost, activeEvent = 90, "claude", "Stop"
	cfg := waitEngineConfig()
	section(cfg, "wait")["maxInHookMinutes"] = float64(330)
	cfg["queue"] = object{"verifyCommand": "go test ./...", "verifyTimeoutSeconds": float64(55)}
	started := time.Now()
	outcome := enforceWait("stop", object{"session_id": "qc-room", "cwd": dir}, cfg, decision{wait: fiveHourPlan(float64(nowSec() + 10)), model: "claude-opus-5"})
	if elapsed := time.Since(started); outcome.stop == "" || elapsed > 5*time.Second {
		t.Fatalf("a Stop hook with 90 s, 30 s of them kept back and a 55 s queue check still to run, waited %s in place for a reset 10 s away: %+v", elapsed.Round(time.Millisecond), outcome)
	}
	if actions := journaledFor("qc-room"); !slices.Contains(actions, "pause") {
		t.Fatalf("the pause was not journaled: %v", actions)
	}
}

func TestAnIssueIsClosedOnlyOnceTheQueueCheckPasses(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, shellFor("cat fixed.txt", "type fixed.txt"))
	getMap(section(cfg, "queue"), "github")["closeOnDone"] = true
	calls := fakeGhCLI(t, "[]")
	queuePath := writeQueueFile(t, project, "# q\n- [ ] #12 Backend crash\n- [ ] write the release notes\n")
	trustQueueFile(queuePath, true)
	stopHookOutput(t, stopInput("qc-issue", frontend), cfg)
	tickIssueItem(t, queuePath, "#12 Backend crash")
	failed := stopHookOutput(t, stopAgain("qc-issue", frontend), cfg)
	if getString(failed, "decision") != "block" || !strings.Contains(getString(failed, "reason"), "Queue check failed") {
		t.Fatalf("the queue check was meant to fail after #12 was ticked: %v", failed)
	}
	if closes := ghLoggedCalls(calls, "close"); len(closes) != 0 {
		t.Fatalf("#12 was ticked but the queue check failed, and Claude was told to leave it unticked until the check passes; its issue was closed anyway: %q", closes)
	}
	if err := os.WriteFile(filepath.Join(project, "fixed.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	passed := stopHookOutput(t, stopAgain("qc-issue", frontend), cfg)
	if !strings.Contains(getString(passed, "reason"), queueContinuesPrefix) {
		t.Fatalf("the stop whose check passed did not continue the queue: %v", passed)
	}
	if closes := ghLoggedCloses(t, calls, 1); len(closes) != 1 || !strings.HasPrefix(closes[0], "gh issue close 12 ") {
		t.Fatalf("the stop whose queue check passed with #12 ticked ran %q, want one issue close 12", closes)
	}
}

func TestTheLastTickedItemIsCheckedBeforeTheQueueIsDone(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, shellFor("echo run >> runs.txt; cat fixed.txt", "echo run>> runs.txt & type fixed.txt"))
	if first := stopHookOutput(t, stopInput("qc-last", frontend), cfg); !strings.Contains(getString(first, "reason"), queueContinuesPrefix) {
		t.Fatalf("the queue did not start: %v", first)
	}
	writeQueueFile(t, project, "# q\n- [x] migrate the users table\n- [x] write the release notes\n")
	failed := stopHookOutput(t, stopAgain("qc-last", frontend), cfg)
	if getString(failed, "decision") != "block" || !strings.Contains(getString(failed, "reason"), "Queue check failed") || getString(failed, "systemMessage") == T("queue.doneMessage", "TASKS.md") {
		t.Fatalf("the last item was ticked and the queue check fails, yet the stop declared the queue done without sending Claude back to fix it: %v", failed)
	}
	if err := os.WriteFile(filepath.Join(project, "fixed.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	done := stopHookOutput(t, stopAgain("qc-last", frontend), cfg)
	if getString(done, "decision") == "block" || getString(done, "systemMessage") != T("queue.doneMessage", "TASKS.md") {
		t.Fatalf("once the queue check passed with every item ticked, the stop did not declare the queue done: %v", done)
	}
	if runs := queueCheckRuns(project); runs != 2 {
		t.Fatalf("the check ran %d times over the failing and the passing stop, want 2", runs)
	}
}
