package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var tieredItems = []string{"migrate the users table", "write the release notes", "tag the release", "update the changelog", "announce the release"}

func tieredQueue(items, ticked int) string {
	var queue strings.Builder
	queue.WriteString("# q\n")
	for index, item := range tieredItems[:items] {
		mark := " "
		if index < ticked {
			mark = "x"
		}
		fmt.Fprintf(&queue, "- [%s] %s\n", mark, item)
	}
	return queue.String()
}

func checkTierSandbox(t *testing.T, items int, repository bool) (cfg object, project, counter string) {
	t.Helper()
	cfg, project, _ = queueCheckSandbox(t, "")
	trustQueueFile(writeQueueFile(t, project, tieredQueue(items, 0)), true)
	if repository {
		writeRepoFile(t, project, "state.txt", "good\n")
		gitIn(t, project, "init", "-q")
		gitIn(t, project, "add", "-A")
		gitIn(t, project, "commit", "-q", "-m", "start")
	}
	return cfg, project, filepath.Join(t.TempDir(), "runs.txt")
}

func countingCheck(counter, word, then string) string {
	command := shellFor(fmt.Sprintf("echo %s >> '%s'", word, counter), fmt.Sprintf(`echo %s>> "%s"`, word, counter))
	if then != "" {
		command += shellFor("; ", " & ") + then
	}
	return command
}

func checkRuns(counter, word string) int {
	content, _ := os.ReadFile(counter)
	runs := 0
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == word {
			runs++
		}
	}
	return runs
}

func tickTiered(t *testing.T, project string, items, ticked int) {
	t.Helper()
	writeQueueFile(t, project, tieredQueue(items, ticked))
}

func checkEntries(sid string) []object {
	entries := []object{}
	for _, line := range tailFileLines(files.decisions, 1000) {
		var entry object
		if jsonUnmarshalObject([]byte(line), &entry) == nil && getString(entry, "sid") == sid && getString(entry, "action") == "verify-queue" {
			entries = append(entries, entry)
		}
	}
	return entries
}

func continues(t *testing.T, output object, open int) {
	t.Helper()
	if want := fmt.Sprintf("Queue continues: %d open", open); getString(output, "decision") != "block" || !strings.Contains(getString(output, "reason"), want) {
		t.Fatalf("the stop did not say %q: %v", want, output)
	}
}

func TestATickThatChangesNothingElseSkipsTheCheckThatPassedOnTheSameTree(t *testing.T) {
	cfg, project, counter := checkTierSandbox(t, 3, true)
	section(cfg, "queue")["verifyCommand"] = countingCheck(counter, "full", "")
	writeRepoFile(t, project, "src/a.txt", "first\n")
	tickTiered(t, project, 3, 1)
	continues(t, stopHookOutput(t, stopInput("tier1", project), cfg), 2)
	tickTiered(t, project, 3, 2)
	continues(t, stopHookOutput(t, stopInput("tier1", project), cfg), 1)
	if runs := checkRuns(counter, "full"); runs != 1 {
		t.Fatalf("the check ran %d times; a tick that changed nothing but the queue file should have skipped it", runs)
	}
	entries := checkEntries("tier1")
	if len(entries) != 2 || !getBool(entries[1], "skipped", false) || !strings.Contains(getString(entries[1], "reason"), "working tree is as it was") {
		t.Fatalf("the skip is not in the decision journal: %v", entries)
	}
	content, _ := readQueueText(filepath.Join(project, "TASKS.md"))
	if unverified, ticked := queueUnverified(cfg, readState(), filepath.Join(project, "TASKS.md"), content); unverified != 0 || ticked != 2 {
		t.Fatalf("after the skip %d of %d ticked items count as unchecked, want 0", unverified, ticked)
	}
	writeRepoFile(t, project, "src/a.txt", "second\n")
	tickTiered(t, project, 3, 3)
	if done := stopHookOutput(t, stopInput("tier1", project), cfg); getString(done, "systemMessage") != T("queue.doneMessage", "TASKS.md") {
		t.Fatalf("the queue did not finish after its last check passed: %v", done)
	}
	if runs := checkRuns(counter, "full"); runs != 2 {
		t.Fatalf("a changed file did not run the check again (%d runs)", runs)
	}
}

func TestTheSkipNeedsARepositoryAndCanBeTurnedOff(t *testing.T) {
	for _, setting := range []struct {
		name       string
		repository bool
		skip       bool
	}{{"outside a repository", false, true}, {"queue.verifySkipUnchanged off", true, false}} {
		t.Run(setting.name, func(t *testing.T) {
			cfg, project, counter := checkTierSandbox(t, 3, setting.repository)
			section(cfg, "queue")["verifyCommand"] = countingCheck(counter, "full", "")
			section(cfg, "queue")["verifySkipUnchanged"] = setting.skip
			tickTiered(t, project, 3, 1)
			continues(t, stopHookOutput(t, stopInput("tier2", project), cfg), 2)
			tickTiered(t, project, 3, 2)
			continues(t, stopHookOutput(t, stopInput("tier2", project), cfg), 1)
			if runs := checkRuns(counter, "full"); runs != 2 {
				t.Fatalf("the check ran %d times, want 2: nothing may be skipped %s", runs, setting.name)
			}
		})
	}
}

func TestAFailedCheckRunsAgainOnATreeItOncePassedOn(t *testing.T) {
	cfg, project, counter := checkTierSandbox(t, 3, true)
	section(cfg, "queue")["verifyCommand"] = countingCheck(counter, "full", shellFor("grep -q good state.txt", "findstr good state.txt"))
	tickTiered(t, project, 3, 1)
	continues(t, stopHookOutput(t, stopInput("tier3", project), cfg), 2)
	writeRepoFile(t, project, "state.txt", "bad\n")
	tickTiered(t, project, 3, 2)
	if failed := stopHookOutput(t, stopInput("tier3", project), cfg); !strings.Contains(getString(failed, "reason"), "Queue check failed") {
		t.Fatalf("the check did not fail on the broken tree: %v", failed)
	}
	writeRepoFile(t, project, "state.txt", "good\n")
	continues(t, stopHookOutput(t, stopAgain("tier3", project), cfg), 1)
	if runs := checkRuns(counter, "full"); runs != 3 {
		t.Fatalf("the check ran %d times, want 3: after a failure it runs again even on a tree it passed on before", runs)
	}
}

func TestThePerItemCheckRunsBetweenItemsAndTheFullCheckEveryFewItemsAndAtTheEnd(t *testing.T) {
	cfg, project, counter := checkTierSandbox(t, 5, false)
	full, each := countingCheck(counter, "full", ""), countingCheck(counter, "each", "")
	section(cfg, "queue")["verifyCommand"] = full
	section(cfg, "queue")["verifyEachCommand"] = each
	section(cfg, "queue")["verifyFullEvery"] = float64(2)
	status := queueCommand(t, cfg, project, "status")
	if !strings.Contains(status, T("queue.verifyFromConfig", full, "queue.verifyCommand")) || !strings.Contains(status, T("queue.verifyEach", each, "queue.verifyEachCommand", 2)) {
		t.Fatalf("noctis queue status does not name both checks and when the full one runs:\n%s", status)
	}
	want := [][2]int{{1, 0}, {1, 1}, {2, 1}, {2, 2}}
	for ticked := 1; ticked <= 4; ticked++ {
		tickTiered(t, project, 5, ticked)
		continues(t, stopHookOutput(t, stopInput("tier4", project), cfg), 5-ticked)
		if got := [2]int{checkRuns(counter, "each"), checkRuns(counter, "full")}; got != want[ticked-1] {
			t.Fatalf("after %d ticked items the per-item and the full check ran %v times, want %v", ticked, got, want[ticked-1])
		}
		path := filepath.Join(project, "TASKS.md")
		content, _ := readQueueText(path)
		if unverified, _ := queueUnverified(cfg, readState(), path, content); unverified != 0 {
			t.Fatalf("after %d ticked items %d count as unchecked although a check passed on each", ticked, unverified)
		}
	}
	if entry := checkEntries("tier4")[0]; getString(entry, "tier") != eachQueueCheck || getString(entry, "command") != each {
		t.Fatalf("the per-item run is journaled as %v", entry)
	}
	tickTiered(t, project, 5, 5)
	if done := stopHookOutput(t, stopInput("tier4", project), cfg); getString(done, "systemMessage") != T("queue.doneMessage", "TASKS.md") {
		t.Fatalf("the queue did not finish: %v", done)
	}
	if got := [2]int{checkRuns(counter, "each"), checkRuns(counter, "full")}; got != [2]int{2, 3} {
		t.Fatalf("the last item ran the per-item and the full check %v times in all, want [2 3]: the full check runs before the queue ends", got)
	}
}

func TestAFailingPerItemCheckIsTheOneThatRunsAgain(t *testing.T) {
	cfg, project, counter := checkTierSandbox(t, 3, false)
	each := countingCheck(counter, "each", shellFor("test -f flag.txt", "type flag.txt"))
	section(cfg, "queue")["verifyCommand"] = countingCheck(counter, "full", "")
	section(cfg, "queue")["verifyEachCommand"] = each
	tickTiered(t, project, 3, 1)
	failed := stopHookOutput(t, stopInput("tier5", project), cfg)
	if !strings.Contains(getString(failed, "reason"), "Queue check failed: `"+each+"`") {
		t.Fatalf("the failing per-item check did not send Claude back: %v", failed)
	}
	writeRepoFile(t, project, "flag.txt", "fixed\n")
	continues(t, stopHookOutput(t, stopAgain("tier5", project), cfg), 2)
	if got := [2]int{checkRuns(counter, "each"), checkRuns(counter, "full")}; got != [2]int{2, 0} {
		t.Fatalf("after the fix the per-item and the full check ran %v times, want [2 0]", got)
	}
}

func TestQueueCheckCommandsComeFromTheFileThenTheConfig(t *testing.T) {
	cfg, project, _ := checkTierSandbox(t, 1, false)
	path := filepath.Join(project, "TASKS.md")
	for _, setting := range []struct {
		name               string
		fileLines          string
		configFull         string
		configEach         string
		wantFull, wantEach string
	}{
		{"the configured check alone", "", "make test", "", "make test", ""},
		{"a lone per-item check is the check", "", "", "make quick", "make quick", ""},
		{"both configured", "", "make test", "make quick", "make test", "make quick"},
		{"the same command twice is one check", "", "make test", "make test", "make test", ""},
		{"the file's per-item line next to the configured check", "noctis-verify-each: `go vet ./...`\n", "make test", "make quick", "make test", "go vet ./..."},
		{"the file's lines win", "noctis-verify: `go test ./...`\nnoctis-verify-each: `go vet ./...`\n", "make test", "make quick", "go test ./...", "go vet ./..."},
	} {
		t.Run(setting.name, func(t *testing.T) {
			trustQueueFile(writeQueueFile(t, project, setting.fileLines+tieredQueue(1, 0)), true)
			section(cfg, "queue")["verifyCommand"] = setting.configFull
			section(cfg, "queue")["verifyEachCommand"] = setting.configEach
			if full, each := queueCheckCommandsOf(cfg, path, ""); full != setting.wantFull || each != setting.wantEach {
				t.Fatalf("the checks are (%q, %q), want (%q, %q)", full, each, setting.wantFull, setting.wantEach)
			}
		})
	}
}

func TestTheFullCheckIsDueOnceEnoughTicksWaitForItAndWhenTheQueueEnds(t *testing.T) {
	cfg := object{"queue": object{"verifyFullEvery": float64(3)}}
	one, two, three := queueItemDigest("one"), queueItemDigest("two"), queueItemDigest("three")
	for _, setting := range []struct {
		name     string
		record   object
		ticked   []any
		ending   bool
		hasEach  bool
		due      bool
		wantTier string
	}{
		{"nothing new", object{"ticked": []any{one}}, []any{one}, false, true, false, eachQueueCheck},
		{"a new tick with no per-item check", object{"ticked": []any{one}}, []any{one, two}, false, false, true, fullQueueCheck},
		{"a new tick with a per-item check", object{"ticked": []any{one}}, []any{one, two}, false, true, true, eachQueueCheck},
		{"a tick the per-item check passed", object{"ticked": []any{one}, "eachTicked": []any{two}}, []any{one, two}, false, true, false, eachQueueCheck},
		{"the queue ends on ticks only the per-item check passed", object{"ticked": []any{one}, "eachTicked": []any{two}}, []any{one, two}, true, true, true, fullQueueCheck},
		{"enough ticks wait for the full check", object{"eachTicked": []any{one, two}}, []any{one, two, three}, false, true, true, fullQueueCheck},
		{"a failed per-item check", object{"failures": float64(1), "tier": eachQueueCheck}, []any{one}, false, true, true, eachQueueCheck},
		{"a failed full check", object{"failures": float64(1)}, []any{one}, false, true, true, fullQueueCheck},
	} {
		t.Run(setting.name, func(t *testing.T) {
			if due := queueCheckDue(setting.record, setting.ticked, setting.ending); due != setting.due {
				t.Fatalf("due is %v, want %v", due, setting.due)
			}
			if tier := queueCheckTier(cfg, setting.record, setting.ticked, setting.hasEach, setting.ending); tier != setting.wantTier {
				t.Fatalf("the tier is %q, want %q", tier, setting.wantTier)
			}
		})
	}
	section(cfg, "queue")["verifyFullEvery"] = float64(0)
	if tier := queueCheckTier(cfg, object{}, []any{one, two, three}, true, false); tier != eachQueueCheck {
		t.Fatalf("with queue.verifyFullEvery 0 the full check ran before the queue ends")
	}
}

func repeatedStepsQueue(ticked int) string {
	var queue strings.Builder
	queue.WriteString("# q\n")
	for index, item := range []string{"add endpoint A", "update the docs", "add endpoint B", "update the docs"} {
		mark := " "
		if index < ticked {
			mark = "x"
		}
		fmt.Fprintf(&queue, "- [%s] %s\n", mark, item)
	}
	return queue.String()
}

func TestAnItemWithTheSameTextAsAnEarlierOneIsCheckedWhenItIsTicked(t *testing.T) {
	cfg, project, counter := checkTierSandbox(t, 1, false)
	section(cfg, "queue")["verifyCommand"] = countingCheck(counter, "full", "")
	trustQueueFile(writeQueueFile(t, project, repeatedStepsQueue(0)), true)
	for ticked := 1; ticked <= 3; ticked++ {
		writeQueueFile(t, project, repeatedStepsQueue(ticked))
		continues(t, stopHookOutput(t, stopInput("repeat1", project), cfg), 4-ticked)
	}
	writeQueueFile(t, project, repeatedStepsQueue(4))
	finished := stopHookOutput(t, stopInput("repeat1", project), cfg)
	if runs := checkRuns(counter, "full"); runs != 4 {
		t.Fatalf("the last item, with the same text as the second, was ticked and the queue ended after %d check runs, want 4: %v", runs, finished)
	}
	content, _ := readQueueText(filepath.Join(project, "TASKS.md"))
	if unverified, ticked := queueUnverified(cfg, readState(), filepath.Join(project, "TASKS.md"), content); unverified != 0 || ticked != 4 {
		t.Fatalf("after the last check %d of %d ticked items count as unchecked, want 0 of 4", unverified, ticked)
	}
}

func TestAnItemTickedAgainAfterItWasUntickedIsCheckedAgain(t *testing.T) {
	cfg, project, counter := checkTierSandbox(t, 3, false)
	section(cfg, "queue")["verifyCommand"] = countingCheck(counter, "full", "")
	tickTiered(t, project, 3, 1)
	continues(t, stopHookOutput(t, stopInput("again1", project), cfg), 2)
	tickTiered(t, project, 3, 3)
	if finished := stopHookOutput(t, stopInput("again1", project), cfg); getString(finished, "systemMessage") != T("queue.doneMessage", "TASKS.md") {
		t.Fatalf("the first run did not finish: %v", finished)
	}
	tickTiered(t, project, 3, 0)
	stopHookOutput(t, stopInput("again1", project), cfg)
	tickTiered(t, project, 3, 1)
	continues(t, stopHookOutput(t, stopInput("again1", project), cfg), 2)
	if runs := checkRuns(counter, "full"); runs != 3 {
		t.Fatalf("an item ticked again after the list was unticked ran the check %d times in all, want 3", runs)
	}
	content, _ := readQueueText(filepath.Join(project, "TASKS.md"))
	if unverified, ticked := queueUnverified(cfg, readState(), filepath.Join(project, "TASKS.md"), content); unverified != 0 || ticked != 1 {
		t.Fatalf("after the check %d of %d ticked items count as unchecked, want 0 of 1", unverified, ticked)
	}
}
