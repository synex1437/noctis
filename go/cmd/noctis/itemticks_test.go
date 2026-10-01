package main

import (
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
