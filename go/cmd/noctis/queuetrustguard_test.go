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

func TestClaudeCannotRunQueueTrustThroughQuotesEscapesOrAnotherRunner(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	writeQueueFile(t, project, "# q\n- [ ] migrate the users table\n")
	for _, call := range []struct{ tool, command string }{
		{"Bash", `noctis queue t'r'ust`},
		{"Bash", `noctis queue tru""st`},
		{"Bash", `noct\is queue trust`},
		{"Bash", `noctis queue tr\ust`},
		{"PowerShell", `cmd /c no^ctis queue trust`},
		{"PowerShell", "no`ctis queue trust"},
		{"Bash", `bash -c noctis\ queue\ trust`},
		{"Bash", `echo 'noctis queue trust' | sh`},
		{"Bash", `source <(echo 'noctis queue trust')`},
		{"Bash", `$(which noctis) queue trust`},
		{"Bash", `echo "$(noctis queue trust)"`},
		{"Bash", "echo \"`noctis queue trust`\""},
		{"Bash", `echo trust | xargs noctis queue`},
		{"Bash", `env -S 'noctis queue trust'`},
		{"PowerShell", `iex 'noctis queue trust'`},
		{"PowerShell", `Invoke-Expression "noctis queue trust"`},
		{"PowerShell", `Start-Process noctis -ArgumentList 'queue','trust'`},
		{"PowerShell", `& (Get-Command noctis) queue trust`},
		{"Bash", `python3 -c "import subprocess; subprocess.run(['noctis','queue','trust'])"`},
		{"Bash", `node -e "require('child_process').execSync('noctis queue trust')"`},
		{"Bash", `x='noctis queue trust'; eval "$x"`},
		{"PowerShell", `python -c "import os; os.system(r'C:\Users\me\.claude\plugins\noctis\bin\noctis.exe queue trust')"`},
	} {
		run := runHostHook(t, "claude", shellToolCall("tg4", project, call.tool, call.command), "tg4", 10*time.Second)
		if permissionOf(run.answer) != "deny" || getString(run.answer, "systemMessage") != T("queue.trustByModel", pluginName) {
			t.Errorf("Claude's %s call %q runs noctis queue trust, and noctis did not deny it: %v", call.tool, call.command, run.answer)
		}
	}
}

func TestShellCallsThatOnlyQuoteTheCommandStillPass(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	for _, call := range []struct{ tool, command string }{
		{"Bash", `cd ~/noctis && node tests/lab.js "queue trust"`},
		{"Bash", `grep -rn "noctis queue trust" . | head -5`},
		{"PowerShell", `Select-String -Path README.md -Pattern "noctis queue trust"`},
		{"Bash", `bash -c 'noctis queue status && echo trust'`},
		{"Bash", "git commit -m \"$(cat <<'EOF'\nqueue: the hook denies noctis queue trust run by Claude\n\nThe user sees \"Claude tried to run\nnoctis queue trust itself\" and runs it after reading the file.\nEOF\n)\""},
		{"Bash", "gh pr create --title \"queue trust\" --body \"$(cat <<'EOF'\nRun `noctis queue trust` yourself, after reading TASKS.md.\nEOF\n)\""},
	} {
		if run := runHostHook(t, "claude", shellToolCall("tg5", project, call.tool, call.command), "tg5", 10*time.Second); run.answer != nil {
			t.Errorf("the %s call %q only quotes the command, and noctis answered it: %v", call.tool, call.command, run.answer)
		}
	}
}

func TestClaudeMayNotRunStateWriteThroughAShellCall(t *testing.T) {
	_, project := queueTrustSandbox(t, false)
	for _, call := range []struct{ tool, command string }{
		{"Bash", `echo '{"waits":{}}' | noctis state-write`},
		{"Bash", `noctis state-write < doc.json`},
		{"Bash", `"${CLAUDE_PLUGIN_ROOT}/bin/noctis" state-w'r'ite`},
		{"Bash", `sh -c 'noctis state-write < /tmp/doc.json'`},
		{"Bash", `echo state-write | xargs noctis`},
		{"PowerShell", `Get-Content doc.json | & "$env:CLAUDE_PLUGIN_ROOT\bin\noctis.exe" state-write`},
	} {
		run := runHostHook(t, "claude", shellToolCall("sw1", project, call.tool, call.command), "sw1", 10*time.Second)
		if permissionOf(run.answer) != "deny" || getString(run.answer, "systemMessage") != T("queue.stateWriteByModel", pluginName) {
			t.Errorf("Claude's %s call %q writes noctis state directly, and noctis did not deny it: %v", call.tool, call.command, run.answer)
		}
	}
}
