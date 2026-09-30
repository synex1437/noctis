package main

import (
	"strings"
	"testing"
)

// stopWithBackground is a Stop input whose background_tasks lists tasks, as Claude Code 2.1.286 sends it.
func stopWithBackground(sid, cwd string, again bool, tasks ...object) object {
	listed := []any{}
	for _, task := range tasks {
		listed = append(listed, task)
	}
	return object{"hook_event_name": "Stop", "session_id": sid, "cwd": cwd, "stop_hook_active": again, "background_tasks": listed}
}

func journaledReasonsFor(sid, action string) []string {
	reasons := []string{}
	for _, line := range tailFileLines(files.decisions, 1000) {
		var entry object
		if jsonUnmarshalObject([]byte(line), &entry) == nil && getString(entry, "sid") == sid && getString(entry, "action") == action {
			reasons = append(reasons, getString(entry, "reason"))
		}
	}
	return reasons
}

func stopGuardOf(sid string) object {
	return getMap(getMap(readState(), "stopGuard"), sid)
}

func TestAStopWhileASubagentRunsInTheBackgroundWaitsForItsResult(t *testing.T) {
	cfg, _, frontend := queueCheckSandbox(t, "")
	if first := stopHookOutput(t, stopInput("bw1", frontend), cfg); getString(first, "decision") != "block" || !strings.Contains(getString(first, "reason"), "Queue continues: 2 open") {
		t.Fatalf("the first stop did not continue the queue: %v", first)
	}
	worker := object{"id": "a1", "type": "subagent", "status": "running", "description": "migrate the users table", "agent_type": "noctis:worker"}
	if output := stopHookOutput(t, stopWithBackground("bw1", frontend, true, worker), cfg); output != nil {
		t.Fatalf("a stop while the worker runs in the background was continued: %v", output)
	}
	if reasons := journaledReasonsFor("bw1", "allow-stop"); len(reasons) != 1 || reasons[0] != "background work in flight" {
		t.Fatalf("the stop that waits for the background work is journaled as %q", reasons)
	}
	if guard := stopGuardOf("bw1"); numberOr(guard, "idle", -1) != 0 || numberOr(guard, "forced", -1) != 1 {
		t.Fatalf("the stop that waits for the background work counted as one without progress: %v", guard)
	}
	workflow := object{"id": "w1", "type": "workflow", "status": "pending", "description": "audit the routes", "name": "audit"}
	if output := stopHookOutput(t, stopWithBackground("bw1", frontend, true, workflow), cfg); output != nil {
		t.Fatalf("a stop while a workflow is pending in the background was continued: %v", output)
	}

	// Claude Code wakes the session with the result; the stop after that goes on with the queue.
	if output := stopHookOutput(t, stopWithBackground("bw1", frontend, false), cfg); getString(output, "decision") != "block" {
		t.Fatalf("the stop after the background work came back did not continue the queue: %v", output)
	}
	server := object{"id": "b1", "type": "shell", "status": "running", "description": "dev server", "command": "npm run dev"}
	monitor := object{"id": "m1", "type": "monitor", "status": "running", "description": "watch the log", "server": "logs", "tool": "tail"}
	if output := stopHookOutput(t, stopWithBackground("bw1", frontend, true, server, monitor), cfg); getString(output, "decision") != "block" {
		t.Fatalf("a dev server and a monitor in the background held the queue: %v", output)
	}
	if guard := stopGuardOf("bw1"); numberOr(guard, "idle", -1) != 1 {
		t.Fatalf("a stop without progress beside a dev server did not count as one: %v", guard)
	}
	done := object{"id": "a2", "type": "subagent", "status": "completed", "description": "write the release notes"}
	if output := stopHookOutput(t, stopWithBackground("bw1", frontend, true, done), cfg); getString(output, "decision") != "block" {
		t.Fatalf("a subagent listed as completed held the queue: %v", output)
	}
}

func TestTheQueueCheckWaitsForBackgroundWorkBeforeItRuns(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, shellFor("echo run >> runs.txt", "echo run>> runs.txt"))
	tickFirstQueueItem(t, project)
	worker := object{"id": "a1", "type": "subagent", "status": "running", "description": "write the release notes", "agent_type": "noctis:worker"}
	if output := stopHookOutput(t, stopWithBackground("bw2", frontend, false, worker), cfg); output != nil {
		t.Fatalf("a stop while the worker runs in the background was continued: %v", output)
	}
	if runs := queueCheckRuns(project); runs != 0 {
		t.Fatalf("the check ran %d times while a subagent still worked in the background", runs)
	}
	output := stopHookOutput(t, stopWithBackground("bw2", frontend, false), cfg)
	if reason := getString(output, "reason"); getString(output, "decision") != "block" || !strings.Contains(reason, "Queue continues: 1 open") {
		t.Fatalf("once the background work was back, the queue did not go on: %v", output)
	}
	if runs := queueCheckRuns(project); runs != 1 {
		t.Fatalf("the check ran %d times once the background work was back, want 1", runs)
	}

	tickEveryQueueItem(t, project)
	if output := stopHookOutput(t, stopWithBackground("bw2", frontend, false, worker), cfg); output != nil {
		t.Fatalf("the queue was finished while a subagent still worked in the background: %v", output)
	}
	if runs := queueCheckRuns(project); runs != 1 {
		t.Fatalf("the check of the finished queue ran %d times while a subagent still worked, want 1", runs)
	}
	if done := stopHookOutput(t, stopWithBackground("bw2", frontend, false), cfg); !strings.Contains(getString(done, "systemMessage"), T("queue.doneMessage", "TASKS.md")) {
		t.Fatalf("once the background work was back, the finished queue was not reported: %v", done)
	}
}

func TestAQueueGivesUpAfterThreeStopsWithoutProgressByDefault(t *testing.T) {
	cfg, _, frontend := queueCheckSandbox(t, "")
	if maxIdle := numberOr(section(cfg, "queue"), "maxIdleContinues", 0); maxIdle != 3 {
		t.Fatalf("config.default.json ships queue.maxIdleContinues %v, want 3", maxIdle)
	}
	delete(section(cfg, "queue"), "maxIdleContinues")
	outputs := []object{stopHookOutput(t, stopInput("bw3", frontend), cfg)}
	for range 3 {
		outputs = append(outputs, stopHookOutput(t, stopAgain("bw3", frontend), cfg))
	}
	for index, output := range outputs[:3] {
		if getString(output, "decision") != "block" {
			t.Fatalf("stop %d of 4 was not continued: %v", index+1, output)
		}
	}
	if !strings.Contains(getString(outputs[2], "reason"), "Find the root cause") {
		t.Fatalf("the third stop, the last continuation, did not ask Claude to set the item aside: %v", outputs[2])
	}
	if last := outputs[3]; getString(last, "decision") == "block" || getString(last, "systemMessage") != T("queue.stuckMessage", 2) {
		t.Fatalf("the fourth stop, the third without progress, did not end the queue's continuations: %v", last)
	}
}
