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
