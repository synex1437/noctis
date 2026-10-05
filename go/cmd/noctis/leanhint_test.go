package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

type leanBox struct {
	root, home, account string
}

func newLeanBox(t *testing.T) leanBox {
	t.Helper()
	box := leanBox{root: cliPluginTree(t), home: t.TempDir(), account: t.TempDir()}
	cliWrite(t, filepath.Join(box.account, "settings.json"), []byte("{}\n"))
	return box
}

func (box leanBox) run(t *testing.T, input string, argv ...string) cliRun {
	t.Helper()
	return startNoctisCLIAt(t, box.home, input, map[string]string{"NOCTIS_PLUGIN_ROOT": box.root, "CLAUDE_CONFIG_DIR": box.account, "NOCTIS_UPDATE_URL": "off", "NOCTIS_USAGE_URL": "http://127.0.0.1:9/usage", "NOCTIS_NO_EARLY_TRIGGER": "1"}, argv...)()
}

func (box leanBox) statusline(t *testing.T, sid string, percent int) string {
	t.Helper()
	return box.statuslineOf(t, sid, percent, "")
}

// statuslineTokens reports a context of tokens in a window of window tokens, as Claude Code does.
func (box leanBox) statuslineTokens(t *testing.T, sid string, tokens, window float64) string {
	t.Helper()
	payload, _ := json.Marshal(object{"session_id": sid, "model": object{"id": "claude-opus-5-5", "display_name": "Opus 5.5"}, "cwd": forwardSlashes(box.home), "context_window": object{"used_percentage": 100 * tokens / window, "context_window_size": window, "current_usage": object{"input_tokens": tokens}}})
	run := box.run(t, string(payload), "statusline")
	if run.code != 0 {
		t.Fatalf("the status line failed:\n%s", run)
	}
	return run.stdout
}

func (box leanBox) settings(t *testing.T, settings object) {
	t.Helper()
	cliWrite(t, filepath.Join(box.account, "settings.json"), marshalPretty(settings))
}

func (box leanBox) compacted(t *testing.T, sid string) {
	t.Helper()
	payload, _ := json.Marshal(object{"hook_event_name": "SessionStart", "session_id": sid, "cwd": box.home, "source": "compact"})
	if run := box.run(t, string(payload), "hook"); run.code != 0 {
		t.Fatalf("the SessionStart hook after a compaction failed:\n%s", run)
	}
}

func (box leanBox) statuslineOf(t *testing.T, sid string, percent int, version string) string {
	t.Helper()
	payload := `{"session_id":"` + sid + `","model":{"id":"claude-opus-5-5","display_name":"Opus 5.5"},"cwd":"` + forwardSlashes(box.home) + `","context_window":{"used_percentage":` + formatNumber(float64(percent)) + `}}`
	if version != "" {
		payload = `{"version":"` + version + `",` + payload[1:]
	}
	run := box.run(t, payload, "statusline")
	if run.code != 0 {
		t.Fatalf("the status line failed:\n%s", run)
	}
	return run.stdout
}

func (box leanBox) prompt(t *testing.T, sid string) string {
	t.Helper()
	payload, _ := json.Marshal(object{"hook_event_name": "UserPromptSubmit", "session_id": sid, "cwd": box.home, "prompt": "carry on with the next step of the refactor"})
	run := box.run(t, string(payload), "hook")
	if run.code != 0 {
		t.Fatalf("the prompt hook failed:\n%s", run)
	}
	var answer object
	if strings.HasPrefix(strings.TrimSpace(run.stdout), "{") {
		if err := json.NewDecoder(strings.NewReader(run.stdout)).Decode(&answer); err != nil {
			t.Fatalf("the prompt hook answered something that is not JSON: %q", run.stdout)
		}
	}
	return getString(answer, "systemMessage")
}

func (box leanBox) config(t *testing.T, compaction object) {
	t.Helper()
	cliWrite(t, filepath.Join(box.account, pluginName, "config.json"), marshalPretty(object{"compaction": compaction}))
}

func (box leanBox) moduleRanIn(t *testing.T, sid string) {
	t.Helper()
	cliWrite(t, filepath.Join(box.account, pluginName, "lean.json"), marshalPretty(object{"sessions": object{sid: float64(1790000000)}}))
}

func TestTheStatusLineMarksAFullContextWhileTheLeanModuleIsNotRunning(t *testing.T) {
	box := newLeanBox(t)
	box.settings(t, object{"autoCompactWindow": float64(313000)})
	if line := box.statuslineTokens(t, "s1", 252000, 1e6); !strings.Contains(line, "ctx 90%▲") {
		t.Fatalf("at 252k tokens, 90%% of the way to the 280k compaction point, with no lean module in the session the context carries no mark: %q", line)
	}
	if line := box.statuslineTokens(t, "s1", 249000, 1e6); !strings.Contains(line, "ctx 89%") || strings.Contains(line, "▲") {
		t.Fatalf("below earlyAtPercent the context is marked: %q", line)
	}
	if line := box.statusline(t, "s1", 90); !strings.Contains(line, "ctx 90%▲") {
		t.Fatalf("a status line with no token count lost the mark at 90%% of the window: %q", line)
	}
	box.moduleRanIn(t, "s1")
	if line := box.statuslineTokens(t, "s1", 252000, 1e6); !strings.Contains(line, "ctx 90%") || strings.Contains(line, "▲") {
		t.Fatalf("the context is marked in a session the lean module runs in: %q", line)
	}
	for _, compaction := range []object{{"lean": false}, {"earlyAtPercent": float64(0)}} {
		box := newLeanBox(t)
		box.config(t, compaction)
		if line := box.statusline(t, "s2", 95); strings.Contains(line, "▲") {
			t.Fatalf("compaction=%v still marks the context: %q", compaction, line)
		}
	}
}

func TestTheStatusLineCountsTheContextToWhereClaudeCodeCompacts(t *testing.T) {
	for _, account := range []struct {
		name     string
		settings object
		tokens   float64
	}{
		{"noctis's 280k", object{"autoCompactWindow": float64(313000)}, 140000},
		{"autoCompactWindow 633000", object{"autoCompactWindow": float64(633000)}, 300000},
		{"/autocompact 600k", object{"modelSettings": object{"claude-opus-5-5": object{"autoCompactWindow": float64(600000)}}}, 283500},
		{"/autocompact 140k", object{"modelSettings": object{"claude-opus-5-5": object{"autoCompactWindow": float64(140000)}}}, 53500},
		{"60%", object{"env": object{autoCompactPercentVar: "60"}}, 294000},
		{"Claude Code's own", object{}, 483500},
	} {
		t.Run(account.name, func(t *testing.T) {
			box := newLeanBox(t)
			box.settings(t, account.settings)
			if line := box.statuslineTokens(t, "s1", account.tokens, 1e6); !strings.Contains(line, "ctx 50%") {
				t.Fatalf("%v tokens, half of the way to where Claude Code compacts, do not read 50%%: %q", account.tokens, line)
			}
		})
	}
	box := newLeanBox(t)
	box.settings(t, object{"autoCompactWindow": float64(313000), "env": object{"DISABLE_AUTO_COMPACT": "1"}})
	if line := box.statuslineTokens(t, "s1", 300000, 1e6); !strings.Contains(line, "ctx 30%") {
		t.Fatalf("with Claude Code's compaction off the context does not read as its share of the window: %q", line)
	}
}

func TestAOneTimeNoticeNamesCompactWhileTheLeanModuleIsNotRunning(t *testing.T) {
	box := newLeanBox(t)
	box.statusline(t, "s1", 92)
	first := box.prompt(t, "s1")
	if !strings.Contains(first, "/compact") || !strings.Contains(first, "92%") {
		t.Fatalf("the first prompt at 92%% got no notice naming /compact: %q", first)
	}
	if again := box.prompt(t, "s1"); strings.Contains(again, "/compact") {
		t.Fatalf("the notice came twice in one session: %q", again)
	}
	box.compacted(t, "s1")
	if stale := box.prompt(t, "s1"); strings.Contains(stale, "/compact") {
		t.Fatalf("after a compaction the fill from before it brought the notice back: %q", stale)
	}
	box.statusline(t, "s1", 30)
	if low := box.prompt(t, "s1"); strings.Contains(low, "/compact") {
		t.Fatalf("after a compaction a session at 30%% was told to compact: %q", low)
	}
	box.statusline(t, "s1", 93)
	if refilled := box.prompt(t, "s1"); !strings.Contains(refilled, "/compact") || !strings.Contains(refilled, "93%") {
		t.Fatalf("after a compaction the context filled up again and got no notice: %q", refilled)
	}
	box.statusline(t, "s2", 40)
	if low := box.prompt(t, "s2"); strings.Contains(low, "/compact") {
		t.Fatalf("a session at 40%% was told to compact: %q", low)
	}
	box.moduleRanIn(t, "s3")
	box.statusline(t, "s3", 95)
	if running := box.prompt(t, "s3"); strings.Contains(running, "/compact") {
		t.Fatalf("a session the lean module runs in was told to compact by hand: %q", running)
	}
}

func TestOnAClaudeCodeOlderThanTheLeanModuleNeedsTheNoticeNamesTheVersionInsteadOfPromisingSetup(t *testing.T) {
	box := newLeanBox(t)
	promise := "/noctis:setup turns Claude Code's function hooks on from the next session"
	box.statuslineOf(t, "old", 92, "2.1.251")
	old := box.prompt(t, "old")
	for _, want := range []string{"92%", "/compact", "2.1.251", "2.1.281", "--no-lean"} {
		if !strings.Contains(old, want) {
			t.Fatalf("the notice in a session on Claude Code 2.1.251 does not name %q: %q", want, old)
		}
	}
	if strings.Contains(old, promise) {
		t.Fatalf("the notice in a session on Claude Code 2.1.251 promises that setup makes lean compaction run: %q", old)
	}
	for sid, version := range map[string]string{"current": "2.1.281", "newer": "2.2.0", "unreported": "", "unreadable": "unknown"} {
		box.statuslineOf(t, sid, 92, version)
		if notice := box.prompt(t, sid); !strings.Contains(notice, promise) {
			t.Fatalf("a session on Claude Code %q did not get the notice that names /noctis:setup: %q", version, notice)
		}
	}
}

func TestDoctorFlagsALastSessionOnAClaudeCodeOlderThanTheLeanModuleNeeds(t *testing.T) {
	leanSandbox(t)
	t.Setenv("NOCTIS_LANG", "en")
	t.Setenv("CLAUDE_CODE_ENABLE_FUNCTION_HOOKS", "")
	mustWriteJSON(files.settings, object{"env": object{"CLAUDE_CODE_ENABLE_FUNCTION_HOOKS": "1"}})
	now := nowSec()
	flagged := func() (string, string) {
		lines := doctorLines(loadConfig())
		for index, line := range lines {
			if strings.Contains(line, "2.1.281") && strings.Contains(line, "lean module") {
				next := ""
				if index+1 < len(lines) {
					next = lines[index+1]
				}
				return line, next
			}
		}
		return "", strings.Join(lines, "\n")
	}
	if line, _ := flagged(); line != "" {
		t.Fatalf("with no Claude Code version reported the doctor names one: %q", line)
	}
	recordStatusline(object{"session_id": "old", "version": "2.1.251"}, now-120, false)
	recordStatusline(object{"session_id": "quiet"}, now-60, false)
	line, fix := flagged()
	if !strings.HasPrefix(line, "!!  ") || !strings.Contains(line, "2.1.251") || !strings.HasPrefix(fix, "   fix: ") {
		t.Fatalf("the doctor did not flag the last session's Claude Code 2.1.251 for lean compaction, with what to do:\n%s\n%s", line, fix)
	}
	if doctor := strings.Join(doctorLines(loadConfig()), "\n"); !strings.Contains(doctor, "OK  CLAUDE_CODE_ENABLE_FUNCTION_HOOKS: 1") {
		t.Fatalf("the switch itself is set and the doctor no longer says so:\n%s", doctor)
	}
	recordStatusline(object{"session_id": "current", "version": "2.1.281"}, now, false)
	if line, _ := flagged(); line != "" {
		t.Fatalf("the last session runs Claude Code 2.1.281 and the doctor still flags a version: %q", line)
	}
	recordStatusline(object{"session_id": "old", "version": "2.1.251"}, now+60, false)
	if line, _ := flagged(); line == "" {
		t.Fatal("the last session is on Claude Code 2.1.251 again and the doctor no longer flags it")
	}
	mustWriteJSON(filepath.Join(files.guardDir, "lean.json"), object{"sessions": object{"old": float64(now + 60)}})
	if line, _ := flagged(); line != "" {
		t.Fatalf("the lean module ran in the last session and the doctor still flags its Claude Code version: %q", line)
	}
	mustWriteJSON(filepath.Join(files.guardDir, "lean.json"), object{"sessions": object{}})
	mustWriteJSON(files.config, object{"compaction": object{"lean": false}})
	if line, _ := flagged(); line != "" {
		t.Fatalf("lean compaction is off and the doctor flags the Claude Code version: %q", line)
	}
}
