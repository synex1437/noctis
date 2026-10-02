package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const tickQueue = "# q\n- [ ] migrate the users table\n- [ ] write the release notes\n- [ ] tag the release\n- [ ] announce the release\n"

// tickedQueue is tickQueue with its first count items ticked.
func tickedQueue(count int) string {
	return strings.Replace(tickQueue, "- [ ] ", "- [x] ", count)
}

func TestEachTickNotesWhatTheItemTook(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, "")
	path := writeQueueFile(t, project, tickQueue)
	trustQueueFile(path, true)
	gitIn(t, project, "init", "-q")
	gitIn(t, project, "add", "-A")
	gitIn(t, project, "commit", "-q", "-m", "start")
	head := strings.TrimSpace(gitIn(t, project, "rev-parse", "HEAD"))[:12]
	continues := func(input object) {
		t.Helper()
		if output := stopHookOutput(t, input, cfg); getString(output, "decision") != "block" {
			t.Fatalf("the stop was not continued: %v", output)
		}
	}

	runsOn("tk1", "claude-sonnet-5-5", 42000)
	continues(stopInput("tk1", frontend))
	for range 2 {
		hookOutput(t, onPreCompact, agentHookInput("PreCompact", "tk1", frontend, object{"trigger": "auto"}), cfg)
	}
	writeQueueFile(t, project, tickedQueue(1))
	continues(stopAgain("tk1", frontend))
	continues(stopAgain("tk1", frontend))
	runsOn("tk1", "claude-sonnet-5-5", 90000)
	writeQueueFile(t, project, tickedQueue(2))
	continues(stopAgain("tk1", frontend))
	runsOn("tk1", "claude-sonnet-5-5", 60000)
	writeQueueFile(t, project, tickedQueue(3))
	continues(stopAgain("tk1", frontend))

	state := readState()
	ticks := getList(setupFacts(state, path), "ticks")
	if len(ticks) != 3 {
		t.Fatalf("three items ticked at three stops left %d tick records: %v", len(ticks), ticks)
	}
	setup := sessionSetup(cfg, state, "tk1")
	for index, want := range []struct{ tokens, compactions, idle float64 }{{42000, 2, 0}, {90000, 0, 1}, {60000, 0, 0}} {
		tick := toObject(ticks[index])
		if numberOr(tick, "items", 0) != 1 || numberOr(tick, "contextTokens", -1) != want.tokens || numberOr(tick, "compactions", -1) != want.compactions || numberOr(tick, "idle", -1) != want.idle {
			t.Fatalf("tick %d is %v, want 1 item, %v tokens, %v compactions and %v continues without progress", index+1, tick, want.tokens, want.compactions, want.idle)
		}
		if setup == "" || getString(tick, "setup") != setup || getString(tick, "head") != head {
			t.Fatalf("tick %d is %v, want setup %q and head %q", index+1, tick, setup, head)
		}
	}
	if counted := getMap(state, "queueCompactions"); len(counted) != 0 {
		t.Fatalf("the compactions of a ticked item are still counted for the item in hand: %v", counted)
	}
	want := T("queue.ticks", 3, approxCount(60000), approxCount(90000), 2, 1)
	if line := ticksLine(state, path); line != want {
		t.Fatalf("the tick summary reads %q, want %q", line, want)
	}
	if printed := queueCommand(t, cfg, project, "status"); !strings.Contains(printed, "  "+want) {
		t.Fatalf("noctis queue status does not sum up the ticks:\n%s", printed)
	}
}

func TestTheCompactionsOfAnItemLongerThanTheQueueViewShowsGoWithItsTick(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, "")
	long := "migrate the users table to the new schema: add the tenant_id column, backfill it from the accounts table in batches of 10000 rows, add the index concurrently, then drop the old foreign key"
	queue := strings.Replace(tickQueue, "migrate the users table", long, 1)
	path := writeQueueFile(t, project, queue)
	trustQueueFile(path, true)
	stopHookOutput(t, stopInput("tk3", frontend), cfg)
	for range 2 {
		hookOutput(t, onPreCompact, agentHookInput("PreCompact", "tk3", frontend, object{"trigger": "auto"}), cfg)
	}
	if context := sessionContext(t, compactStart("tk3", frontend), cfg); !strings.Contains(context, "This item has now gone through 2 compactions") {
		t.Fatalf("the two compactions are not counted on the long item in hand:\n%s", context)
	}
	writeQueueFile(t, project, strings.Replace(queue, "- [ ] ", "- [x] ", 1))
	stopHookOutput(t, stopAgain("tk3", frontend), cfg)
	state := readState()
	ticks := getList(setupFacts(state, path), "ticks")
	if len(ticks) != 1 || numberOr(toObject(ticks[0]), "compactions", -1) != 2 {
		t.Fatalf("an item of %d characters went through 2 compactions, but its tick records are %v", len([]rune(long)), ticks)
	}
	if counted := getMap(state, "queueCompactions"); len(counted) != 0 {
		t.Fatalf("the compactions of the ticked long item are still counted for the item in hand: %v", counted)
	}
}

func TestAnItemUntickedForAFailedCheckAndTickedAgainIsCountedOnce(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, shellFor("test -f fixed.txt", "type fixed.txt"))
	path := writeQueueFile(t, project, tickQueue)
	trustQueueFile(path, true)
	runsOn("tk4", "claude-sonnet-5-5", 42000)
	continues(t, stopHookOutput(t, stopInput("tk4", frontend), cfg), 4)
	writeQueueFile(t, project, tickedQueue(1))
	if failed := getString(stopHookOutput(t, stopAgain("tk4", frontend), cfg), "reason"); !strings.Contains(failed, "untick it if you already marked it done") {
		t.Fatalf("setup: the failing check did not send the item back: %q", failed)
	}
	writeQueueFile(t, project, tickQueue)
	writeRepoFile(t, project, "fixed.txt", "ok\n")
	continues(t, stopHookOutput(t, stopAgain("tk4", frontend), cfg), 4)
	writeQueueFile(t, project, tickedQueue(1))
	continues(t, stopHookOutput(t, stopAgain("tk4", frontend), cfg), 3)
	writeQueueFile(t, project, tickedQueue(2))
	continues(t, stopHookOutput(t, stopAgain("tk4", frontend), cfg), 2)
	state := readState()
	items := 0.0
	for _, record := range ticksOf(state, path) {
		items += record.items
	}
	if stats := setupStatsOf(state, path); items != 2 || len(stats) != 1 || stats[0].items != 2 {
		t.Fatalf("two items were done, the first unticked for a failed check and ticked again, but the tick records count %v items and the setups are credited with %+v", items, stats)
	}

	if err := os.Remove(filepath.Join(project, "fixed.txt")); err != nil {
		t.Fatal(err)
	}
	writeQueueFile(t, project, tickedQueue(3))
	stopHookOutput(t, stopAgain("tk4", frontend), cfg)
	writeQueueFile(t, project, tickQueue)
	stopHookOutput(t, stopAgain("tk4", frontend), cfg)
	if notes := paceNotesIn(path); len(notes) != 1 || notes[0].done != 0 {
		t.Fatalf("every item was unticked, also the two that passed the check, but while the check failed the pace notes did not start over: %v", notes)
	}
}

func TestTheTickSummaryWaitsForThreeRecordsAndDoesWithoutTheContext(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, "")
	path := writeQueueFile(t, project, tickQueue)
	trustQueueFile(path, true)
	stopHookOutput(t, stopInput("tk2", frontend), cfg)
	for count := 1; count <= 3; count++ {
		if line := ticksLine(readState(), path); line != "" {
			t.Fatalf("with %d tick record(s) queue status already sums them up: %q", count-1, line)
		}
		writeQueueFile(t, project, tickedQueue(count))
		stopHookOutput(t, stopAgain("tk2", frontend), cfg)
	}
	state := readState()
	if want := T("queue.ticksNoContext", 3, 0, 0); ticksLine(state, path) != want {
		t.Fatalf("with no context known the tick summary reads %q, want %q", ticksLine(state, path), want)
	}
	for _, raw := range getList(setupFacts(state, path), "ticks") {
		if tick := toObject(raw); tick["contextTokens"] != nil || tick["head"] != nil {
			t.Fatalf("a tick outside a git repository with no context known gives %v", tick)
		}
	}
}
