package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stopCapSandbox(t *testing.T, host string, maxIdle float64, items int) (object, string) {
	t.Helper()
	cfg, project := queueTrustSandbox(t, false)
	previous := activeHost
	t.Cleanup(func() { activeHost = previous })
	activeHost = host
	section(cfg, "queue")["maxIdleContinues"] = maxIdle
	lines := []string{"# release"}
	for item := 1; item <= items; item++ {
		lines = append(lines, fmt.Sprintf("- [ ] release step %d", item))
	}
	trustQueueFile(writeQueueFile(t, project, strings.Join(lines, "\n")+"\n"), true)
	return cfg, project
}

func stopsInARow(t *testing.T, cfg object, sid, project string, stops int, before func(stop int)) (int, object) {
	t.Helper()
	blocked := 0
	var last object
	for stop := 0; stop < stops; stop++ {
		if before != nil {
			before(stop)
		}
		input := stopInput(sid, project)
		input["stop_hook_active"] = stop > 0
		last = stopHookOutput(t, input, cfg)
		if getString(last, "decision") != "block" {
			break
		}
		blocked++
	}
	return blocked, last
}

func gaveUpOn(sid string) bool {
	return numberOr(getMap(getMap(readState(), "stopGuard"), sid), "gaveUp", 0) > 0
}

func TestTheQueueGivesUpBeforeClaudeCodeEndsTheTurnItself(t *testing.T) {
	cfg, project := stopCapSandbox(t, "claude", 12, 3)
	t.Setenv("CLAUDE_CODE_STOP_HOOK_BLOCK_CAP", "")
	blocked, last := stopsInARow(t, cfg, "cap1", project, 9, nil)
	if blocked > 8 || !gaveUpOn("cap1") || getString(last, "systemMessage") == "" {
		t.Fatalf("with queue.maxIdleContinues 12 noctis blocked %d stops in a row without progress and gave up: %v (last output %v); Claude Code overrides the 9th such block and ends the turn itself, so noctis must give up with its own notice by then", blocked, gaveUpOn("cap1"), last)
	}
}

func TestALowerStopBlockCapFromTheEnvironmentIsRespected(t *testing.T) {
	cfg, project := stopCapSandbox(t, "claude", 4, 3)
	t.Setenv("CLAUDE_CODE_STOP_HOOK_BLOCK_CAP", "3")
	if blocked, _ := stopsInARow(t, cfg, "cap2", project, 5, nil); blocked > 3 || !gaveUpOn("cap2") {
		t.Fatalf("with CLAUDE_CODE_STOP_HOOK_BLOCK_CAP=3 noctis blocked %d stops in a row without progress (gave up: %v); Claude Code overrides the 4th", blocked, gaveUpOn("cap2"))
	}
}

func TestAFractionalStopBlockCapCountsWholeBlocks(t *testing.T) {
	cfg, project := stopCapSandbox(t, "claude", 4, 3)
	t.Setenv("CLAUDE_CODE_STOP_HOOK_BLOCK_CAP", "2.5")
	if blocked, _ := stopsInARow(t, cfg, "cap3", project, 5, nil); blocked > 2 || !gaveUpOn("cap3") {
		t.Fatalf("with CLAUDE_CODE_STOP_HOOK_BLOCK_CAP=2.5 noctis blocked %d stops in a row (gave up: %v); Claude Code overrides the 3rd", blocked, gaveUpOn("cap3"))
	}
}

func TestAStopBlockCapThatIsOffLeavesTheSettingAlone(t *testing.T) {
	for _, value := range []string{"0", "-1"} {
		cfg, project := stopCapSandbox(t, "claude", 12, 3)
		t.Setenv("CLAUDE_CODE_STOP_HOOK_BLOCK_CAP", value)
		if blocked, _ := stopsInARow(t, cfg, "cap4"+value, project, 9, nil); blocked != 9 {
			t.Fatalf("with CLAUDE_CODE_STOP_HOOK_BLOCK_CAP=%s Claude Code has no cap, yet noctis blocked only %d of 9 stops with queue.maxIdleContinues 12", value, blocked)
		}
	}
}

func TestAnUnreadableStopBlockCapCountsAsClaudeCodesDefault(t *testing.T) {
	cfg, project := stopCapSandbox(t, "claude", 12, 3)
	t.Setenv("CLAUDE_CODE_STOP_HOOK_BLOCK_CAP", "eight")
	if blocked, _ := stopsInARow(t, cfg, "cap5", project, 9, nil); blocked > 8 || !gaveUpOn("cap5") {
		t.Fatalf("with CLAUDE_CODE_STOP_HOOK_BLOCK_CAP=eight Claude Code keeps its cap of 8, yet noctis blocked %d stops in a row", blocked)
	}
}

func TestAStuckItemGoesUpOnlyWhenTheStopBlockCapLeavesRoomForItsTurn(t *testing.T) {
	for _, c := range []struct {
		value  string
		blocks int
		wentUp float64
	}{
		// With queue.maxIdleContinues 3 the third block asks Claude to set the item aside. The stronger
		// model's turn would take a fourth, which Claude Code overrides under a cap of 3.
		{"3", 3, 0},
		{"4", 4, 1},
	} {
		cfg, project := stopCapSandbox(t, "claude", 3, 3)
		t.Setenv("CLAUDE_CODE_STOP_HOOK_BLOCK_CAP", c.value)
		sid := "up" + c.value
		// A session on Sonnet has Opus to go up to at any effort.
		runsOn(sid, "claude-sonnet-5-5", 20000)
		blocked, last := stopsInARow(t, cfg, sid, project, 6, nil)
		if wentUp := escalationsToday(readState(), nowSec()); blocked != c.blocks || !gaveUpOn(sid) || wentUp != c.wentUp {
			t.Fatalf("with CLAUDE_CODE_STOP_HOOK_BLOCK_CAP=%s and queue.maxIdleContinues 3 noctis blocked %d stops in a row (want %d), gave up: %v, items that went up: %v (want %v); last output %v", c.value, blocked, c.blocks, gaveUpOn(sid), wentUp, c.wentUp, last)
		}
		if entry := journaledEntry(sid, "continue-queue"); !getBool(entry, "setAside", false) {
			t.Fatalf("with CLAUDE_CODE_STOP_HOOK_BLOCK_CAP=%s the last continuation did not ask Claude to set the item aside: %v", c.value, entry)
		}
	}
}

func TestAQueueThatProgressesIsNotHeldToTheStopBlockCap(t *testing.T) {
	cfg, project := stopCapSandbox(t, "claude", 4, 14)
	t.Setenv("CLAUDE_CODE_STOP_HOOK_BLOCK_CAP", "")
	queuePath := filepath.Join(project, "TASKS.md")
	blocked, last := stopsInARow(t, cfg, "cap6", project, 12, func(stop int) {
		if stop == 0 {
			return
		}
		content, err := os.ReadFile(queuePath)
		if err != nil {
			t.Fatal(err)
		}
		item := fmt.Sprintf("- [ ] release step %d\n", stop)
		if err := os.WriteFile(queuePath, []byte(strings.Replace(string(content), item, "- [x]"+item[5:], 1)), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	if blocked != 12 {
		t.Fatalf("a queue that ticked an item before every stop was let go after %d stops (last output %v); Claude Code counts only blocks with no tool call in between, and ticking an item is one", blocked, last)
	}
}

func TestOtherHostsKeepTheirIdleSetting(t *testing.T) {
	cfg, project := stopCapSandbox(t, "codex", 12, 3)
	t.Setenv("CLAUDE_CODE_STOP_HOOK_BLOCK_CAP", "3")
	if blocked, _ := stopsInARow(t, cfg, "cap7", project, 9, nil); blocked != 9 {
		t.Fatalf("a Codex session was held to Claude Code's stop block cap: %d of 9 stops blocked with queue.maxIdleContinues 12", blocked)
	}
}

func TestAQueueThatGaveUpStaysLetGoUntilItsItemsChangeOrTheUserTypes(t *testing.T) {
	cfg, project := stopCapSandbox(t, "claude", 3, 3)
	t.Setenv("CLAUDE_CODE_STOP_HOOK_BLOCK_CAP", "")
	if blocked, last := stopsInARow(t, cfg, "sticky", project, 6, nil); !gaveUpOn("sticky") {
		t.Fatalf("the queue did not give up after %d blocks without progress", blocked)
	} else if message := getString(last, "systemMessage"); !strings.Contains(message, "type a prompt") {
		t.Errorf("the give-up does not say what drives the queue again: %q", message)
	}
	for round := 1; round <= 3; round++ {
		if blocked, last := stopsInARow(t, cfg, "sticky", project, 5, nil); blocked != 0 {
			t.Fatalf("round %d: the queue that gave up was driven again with nothing changed: %d block(s), last output %v", round, blocked, last)
		}
	}
	if reason := journaledReason("sticky", "allow-stop"); !strings.Contains(reason, "gave up") {
		t.Errorf("noctis why does not say that the stop was let go because the queue gave up: %q", reason)
	}
	queuePath := filepath.Join(project, "TASKS.md")
	content, err := os.ReadFile(queuePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(queuePath, []byte(strings.Replace(string(content), "- [ ] release step 1\n", "- [x] release step 1\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if blocked, last := stopsInARow(t, cfg, "sticky", project, 1, nil); blocked != 1 {
		t.Fatalf("ticking an item did not lift the give-up: %v", last)
	}
	if blocked, _ := stopsInARow(t, cfg, "sticky", project, 6, nil); !gaveUpOn("sticky") || blocked == 6 {
		t.Fatalf("the queue did not give up again after %d blocks without progress", blocked)
	}
	if blocked, _ := stopsInARow(t, cfg, "sticky", project, 3, nil); blocked != 0 {
		t.Fatalf("the second give-up did not stick: %d block(s)", blocked)
	}
	hookOutput(t, onUserPromptSubmit, promptInput("sticky", project, "keep going with the release please"), cfg)
	if blocked, last := stopsInARow(t, cfg, "sticky", project, 1, nil); blocked != 1 {
		t.Fatalf("a typed prompt did not lift the give-up: %v", last)
	}
}
