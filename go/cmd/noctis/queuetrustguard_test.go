package main

import (
	"testing"
	"time"
)

func shellToolCall(sid, project, tool, command string) object {
	return object{"hook_event_name": "PreToolUse", "session_id": sid, "cwd": project, "tool_name": tool, "tool_input": object{"command": command}}
}

func TestClaudeMayNotRunQueueTrustItself(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	writeQueueFile(t, project, "# q\n- [ ] migrate the users table\n")
	for _, call := range []struct{ tool, command string }{
		{"Bash", "noctis queue trust"},
		{"Bash", `"${CLAUDE_PLUGIN_ROOT}/bin/noctis" queue trust --file TASKS.md`},
		{"Bash", "cd " + project + " && noctis queue trust"},
		{"Bash", "sudo -u me env NOCTIS_LANG=en noctis queue --file TASKS.md trust"},
		{"Bash", `bash -lc 'noctis queue trust 2>&1 | tail -1'`},
		{"Bash", "git pull; noctis queue trust\n"},
		{"PowerShell", `& "$env:CLAUDE_PLUGIN_ROOT\bin\noctis.exe" queue trust`},
		{"PowerShell", `powershell -NoProfile -Command "& 'C:\Users\me\.claude\plugins\noctis\bin\NOCTIS.EXE' queue trust"`},
	} {
		run := runHostHook(t, "claude", shellToolCall("tg1", project, call.tool, call.command), "tg1", 10*time.Second)
		if permissionOf(run.answer) != "deny" {
			t.Fatalf("Claude's %s call %q ran noctis queue trust without a deny: %v", call.tool, call.command, run.answer)
		}
		if want := T("queue.trustByModel", pluginName); getString(run.answer, "systemMessage") != want {
			t.Fatalf("the deny of %q did not tell the user how to trust the file:\n got %q\nwant %q", call.command, getString(run.answer, "systemMessage"), want)
		}
	}
}

func TestOtherShellCallsPassTheQueueTrustCheck(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	for _, command := range []string{
		"ls -la",
		"noctis queue status",
		"noctis queue untrust",
		`grep -rn "noctis queue trust" docs/`,
		`git commit -m "queue: say that noctis queue trust is typed by the user"`,
		`echo "type !noctis queue trust to let it drive"`,
		"cat trust.txt",
	} {
		if run := runHostHook(t, "claude", shellToolCall("tg2", project, "Bash", command), "tg2", 10*time.Second); run.answer != nil {
			t.Fatalf("the shell call %q got an answer from noctis: %v", command, run.answer)
		}
	}
}

func TestAShellCallOnARoutedPromptIsNotSentToTheSubagent(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	updateState(func(next object) {
		stateMap(next, "routes")["tg3"] = object{"at": float64(nowSec())}
	})
	if run := runHostHook(t, "claude", shellToolCall("tg3", project, "Bash", "go test ./..."), "tg3", 10*time.Second); run.answer != nil {
		t.Fatalf("a Bash call on a prompt routed to the lite agent got an answer from noctis: %v", run.answer)
	}
}

func TestTheShippedHooksSeeClaudesShellCalls(t *testing.T) {
	matches := preToolUseMatches(t)
	for _, tool := range []string{"Bash", "PowerShell"} {
		if !matches(tool) {
			t.Fatalf("hooks/hooks.json does not start the PreToolUse hook for %s, so Claude's own noctis queue trust goes through", tool)
		}
	}
}
