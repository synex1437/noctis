//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const answerWhereItRuns = `session=""; previous=""
for arg in "$@"; do if [ "$previous" = "--session-id" ]; then session="$arg"; fi; previous="$arg"; done
target="$NOCTIS_TEST_TRANSCRIPT"
if [ -n "$session" ]; then target="$(dirname "$NOCTIS_TEST_TRANSCRIPT")/$session.jsonl"; fi
printf '{"type":"assistant","timestamp":"%s","message":{"role":"assistant","content":[{"type":"text","text":"Reading the handoff note."}]}}\n' "$(date -u +%Y-%m-%dT%H:%M:%S.000Z)" >> "$target"`

func parkBigPause(t *testing.T, sid, mode string, idleMinutes, tokens float64) (project, note string) {
	t.Helper()
	now := float64(nowSec())
	last := now - idleMinutes*60
	project = t.TempDir()
	transcript := writeTranscriptAt(t, project, []string{userPromptLine(last, "fix the parser")}, last)
	t.Setenv("NOCTIS_TEST_TRANSCRIPT", transcript)
	ensureDir(files.checkpoints)
	note = filepath.Join(files.checkpoints, sid+".md")
	if err := os.WriteFile(note, []byte("# noctis checkpoint\n\nLast prompt: fix the parser\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if writeSessionQueue(sid, object{"cwd": project, "at": last, "items": float64(2), "source": filepath.Join(project, "deneme.md")}, []string{"- [ ] add a login page", "- [ ] add a logout button"}) == "" {
		t.Fatal("the session's checklist was not written")
	}
	wait := object{"kind": "batch", "window": "five_hour", "label": "5h", "used": float64(95), "threshold": float64(92), "hit": "threshold",
		"startedAt": last, "until": now - 10, "resumeAt": now - 5, "cwd": project, "transcript": transcript, "launchMode": mode, "contextTokens": tokens}
	updateState(func(state object) {
		stateMap(state, "waits")[sid] = wait
		stateMap(state, "checkpoints")[sid] = object{"path": note, "cwd": project, "at": last, "consumed": false}
	})
	statusReadingFrom(sid, nowSec(), 3, now+18000, 20, now+3*86400)
	return project, note
}

func launchLines(calls string) []string {
	content, _ := os.ReadFile(calls)
	lines := []string{}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.Contains(line, " -- ") {
			lines = append(lines, line)
		}
	}
	return lines
}

func freshLaunchOf(calls string) (session, prompt string) {
	for _, line := range launchLines(calls) {
		fields := strings.Fields(line)
		for index := 0; index+1 < len(fields); index++ {
			if fields[index] == "--session-id" {
				_, prompt, _ = strings.Cut(line, " -- ")
				return fields[index+1], prompt
			}
		}
	}
	return "", ""
}

func launchPromptOf(calls string) string {
	lines := launchLines(calls)
	if len(lines) == 0 {
		return ""
	}
	_, prompt, _ := strings.Cut(lines[len(lines)-1], " -- ")
	return prompt
}

func TestALongPauseOfABigSessionRelaunchesItFreshWithItsHandoffNote(t *testing.T) {
	calls := relaunchSandboxWith(t, fakeClaude("sleep 1", answerWhereItRuns, "exit 1"))
	sid := "bigpause1"
	project, note := parkBigPause(t, sid, "headless", 150, 160000)
	transcript := getString(waitOf(sid), "transcript")

	resumeWait(sid, "")

	if got := launchesOf(calls, sid); got != 0 {
		t.Fatalf("a session paused for 150 minutes with 160k tokens of context was resumed whole (%d --resume launch(es)); want a fresh session", got)
	}
	fresh, prompt := freshLaunchOf(calls)
	if fresh == "" {
		t.Fatalf("no fresh session was started: %q", launchLines(calls))
	}
	for _, part := range []string{note, transcript, sid, "add a login page"} {
		if !strings.Contains(prompt, part) {
			t.Fatalf("the fresh session's prompt does not name %q: %q", part, prompt)
		}
	}
	if strings.Contains(prompt, `hand it to the noctis:worker subagent (subagent_type "noctis:worker"), a fresh context`) {
		t.Fatalf("a fresh session with a clean context was told to hand its items to subagents: %q", prompt)
	}
	state := readState()
	if from := getString(getMap(getMap(state, "freshStarts"), fresh), "from"); from != sid {
		t.Fatalf("the fresh session was not recorded as taking over from %s: %v", sid, getMap(state, "freshStarts"))
	}
	if getMap(getMap(state, "autoQueues"), sid) != nil || getMap(getMap(state, "autoQueues"), fresh) == nil {
		t.Fatalf("the checklist did not move to the fresh session: %v", getMap(state, "autoQueues"))
	}
	if wait := waitOf(sid); wait != nil {
		t.Fatalf("the fresh session answered, yet the pause was kept for a retry: %v (journal %v)", wait, journaledFor(sid))
	}
	if slices.Contains(journaledFor(sid), "launch-no-progress") {
		t.Fatalf("the answer in the fresh session's own transcript was not seen: %v", journaledFor(sid))
	}
	if output := hostHook(t, "claude", permissionRequest(fresh, project, "Read", note, "acceptEdits")); !permissionAllowed(output) {
		t.Fatalf("Claude Code asked to approve the fresh session's Read of the handoff note it was started with, and noctis did not answer: %v", output)
	}
	cfg := loadConfig()
	defer func(previous bool) { promptFromPlugin = previous }(promptFromPlugin)
	if promptFromPlugin = pluginComposedPrompt(cfg, fresh, prompt); !promptFromPlugin {
		t.Fatal("the fresh session's first prompt is not recognised as the plugin's own")
	}
	startQueue(t, cfg, fresh, project, prompt)
	if getMap(getMap(readState(), "autoQueues"), fresh) == nil {
		t.Fatal("the fresh session's first prompt was taken for one the user typed and ended the checklist it took over")
	}
	if output := stopHookOutput(t, stopInput(fresh, project), cfg); !strings.Contains(getString(output, "reason"), "add a login page") {
		t.Fatalf("the fresh session does not go on with the checklist it took over: %v", output)
	}
	if output := stopHookOutput(t, stopInput(sid, project), cfg); output != nil {
		t.Fatalf("the session it took over from still drives the checklist: %v", output)
	}
}

func TestAFreshSessionThatNeverAnswersGivesTheQueueBackAndTheRetryResumes(t *testing.T) {
	calls := relaunchSandboxWith(t, fakeClaude(`echo "Session ID is already in use" >&2`, "exit 1"))
	sid := "bigpause2"
	parkBigPause(t, sid, "headless", 150, 160000)

	resumeWait(sid, "")

	fresh, _ := freshLaunchOf(calls)
	if fresh == "" {
		t.Fatalf("no fresh session was started: %q", launchLines(calls))
	}
	wait := waitOf(sid)
	if wait == nil || !getBool(wait, "freshFailed", false) || numberOr(wait, "launchAttempts", 0) != 1 {
		t.Fatalf("a fresh session that never answered did not leave the pause for a retry without a fresh start: %v", wait)
	}
	state := readState()
	if getMap(getMap(state, "autoQueues"), sid) == nil || getMap(getMap(state, "autoQueues"), fresh) != nil {
		t.Fatalf("the checklist was not given back to the paused session: %v", getMap(state, "autoQueues"))
	}
	if getMap(getMap(state, "freshStarts"), fresh) != nil {
		t.Fatalf("the fresh start that never answered is still recorded: %v", getMap(state, "freshStarts"))
	}
	// The hand-off used the checkpoint up; put it back as unused for the relaunch below.
	unused := copyObject(checkpointRecord(sid))
	unused["consumed"] = false
	updateState(func(state object) { stateMap(state, "checkpoints")[sid] = unused })

	resumeWait(sid, "")

	if got := launchesOf(calls, sid); got != 1 {
		t.Fatalf("the retry after a fresh start that never answered did not resume the session itself (%d --resume launch(es)): %q", got, launchLines(calls))
	}
}

func TestAShortPauseASmallContextOrNoNoteStillResumesTheSessionItself(t *testing.T) {
	cases := []struct {
		name         string
		idle, tokens float64
		note         bool
		resume       object
		workflow     bool
	}{
		{"paused for 20 minutes", 20, 160000, true, nil, false},
		{"40k tokens of context", 150, 40000, true, nil, false},
		{"no handoff note", 150, 160000, false, nil, false},
		{"resume.freshAfterMinutes 0", 150, 160000, true, object{"freshAfterMinutes": float64(0)}, false},
		{"resume.freshAboveTokens 200000", 150, 160000, true, object{"freshAboveTokens": float64(200000)}, false},
		{"a workflow it can resume only itself", 150, 160000, true, nil, true},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := relaunchSandboxWith(t, fakeClaude("exit 0"))
			if tc.resume != nil {
				resume := object{"mode": "window", "prompt": "carry on"}
				mergeInto(resume, tc.resume)
				relaunchConfig(resume)
			}
			sid := fmt.Sprintf("smallpause%d", index+1)
			_, note := parkBigPause(t, sid, "headless", tc.idle, tc.tokens)
			if !tc.note {
				if err := os.Remove(note); err != nil {
					t.Fatal(err)
				}
			}
			if tc.workflow {
				updateState(func(state object) {
					stateMap(state, "workflows")[sid] = []any{object{"name": "migrate-endpoints", "at": float64(nowSec() - 9000)}}
				})
			}

			resumeWait(sid, "")

			if got := launchesOf(calls, sid); got != 1 {
				t.Fatalf("the session was resumed %d time(s), want 1: %q", got, launchLines(calls))
			}
			if fresh, _ := freshLaunchOf(calls); fresh != "" {
				t.Fatalf("a fresh session %s was started", fresh)
			}
			if getMap(getMap(readState(), "autoQueues"), sid) == nil {
				t.Fatal("the resumed session lost its checklist")
			}
		})
	}
}

func TestASessionThatStoppedWithItsContextFullStartsAfreshEvenAfterAShortPause(t *testing.T) {
	calls := relaunchSandboxWith(t, fakeClaude("sleep 1", answerWhereItRuns, "exit 1"))
	sid := "fullcontext1"
	// A minute after the stop, with no fill known: neither resume.freshAfterMinutes nor
	// resume.freshAboveTokens would start the session afresh.
	_, note := parkBigPause(t, sid, "headless", 1, 0)
	updateState(func(state object) {
		wait := getMap(getMap(state, "waits"), sid)
		wait["contextFull"] = true
		delete(wait, "contextTokens")
		stateMap(state, "contextFulls")[sid] = object{"count": float64(1), "lastAt": float64(nowSec())}
	})

	resumeWait(sid, "")

	if got := launchesOf(calls, sid); got != 0 {
		t.Fatalf("a session that stopped with its context full was resumed whole (%d --resume launch(es)), to send the same full context again", got)
	}
	fresh, prompt := freshLaunchOf(calls)
	if fresh == "" {
		t.Fatalf("no fresh session was started: %q", launchLines(calls))
	}
	for _, part := range []string{note, sid, "stopped with its context full", readInParts, "add a login page"} {
		if !strings.Contains(prompt, part) {
			t.Fatalf("the fresh session's prompt does not name %q: %q", part, prompt)
		}
	}
	if entry := journaledEntry(sid, "launch"); !getBool(entry, "contextFull", false) || getString(entry, "fresh") != fresh {
		t.Fatalf("the fresh start was not journaled as one after a full context: %v", entry)
	}
	if contextFulls := getMap(readState(), "contextFulls"); getMap(contextFulls, fresh) == nil || getMap(contextFulls, sid) != nil {
		t.Fatalf("the fresh session did not take the count of full contexts along: %v", contextFulls)
	}
}

// parkFullContext parks sid the way the StopFailure hook does when a turn ends with the context full,
// a minute after the stop.
func parkFullContext(t *testing.T, sid string) {
	t.Helper()
	parkBigPause(t, sid, "headless", 1, 0)
	updateState(func(state object) {
		wait := getMap(getMap(state, "waits"), sid)
		wait["kind"], wait["window"], wait["label"], wait["used"], wait["threshold"] = "stopfailure", "unknown", T("stopfailure.ctxFullLabel"), nil, nil
		wait["contextFull"], wait["retry"], wait["error"] = true, float64(1), "invalid_request"
		delete(wait, "hit")
		delete(wait, "contextTokens")
	})
}

func TestAFreshStartIsNotHeldBackForACompactionOfTheContextItLeavesBehind(t *testing.T) {
	calls := relaunchSandboxWith(t, fakeClaude("sleep 1", answerWhereItRuns, "exit 1"))
	sid := "fullcontext3"
	parkFullContext(t, sid)
	// The 5-hour window is 4 points under its pause point and the status line last saw the context
	// full: resumed whole, the session would compact first, which the guard holds back until the reset.
	now := float64(nowSec())
	recordStatusline(object{"session_id": sid,
		"rate_limits": object{
			"five_hour": object{"used_percentage": float64(88), "resets_at": now + 18000},
			"seven_day": object{"used_percentage": float64(20), "resets_at": now + 3*86400},
		},
		"context_window": object{"context_window_size": float64(200000), "used_percentage": float64(97), "current_usage": object{"input_tokens": float64(194000)}},
	}, nowSec(), true)

	resumeWait(sid, "")

	if fresh, _ := freshLaunchOf(calls); fresh == "" {
		t.Fatalf("the fresh session, which starts with a clean context, was held back until the 5-hour reset for a compaction of the full context it leaves behind: %v (journal %v)", waitOf(sid), journaledFor(sid))
	}
}

func TestAFreshStartIsNotHeldBackForTheCompactionAt280kOfTheMillionTokenContextItLeavesBehind(t *testing.T) {
	calls := relaunchSandboxWith(t, fakeClaude("sleep 1", answerWhereItRuns, "exit 1"))
	clearCompactionVariables(t)
	mustWriteJSON(files.settings, object{"autoCompactWindow": float64(313000)})
	sid := "fullcontext5"
	parkFullContext(t, sid)
	// The session's last status line: 278k tokens of context, by the 280k where Claude Code compacts its
	// 1M window, and the five-hour window at 88 %, inside the band before its 92 % pause point.
	now := nowSec()
	recordStatusline(object{
		"session_id": sid,
		"model":      object{"id": "claude-opus-5-5[1m]"},
		"context_window": object{"used_percentage": float64(28), "context_window_size": float64(1e6),
			"current_usage": object{"input_tokens": float64(2000), "cache_read_input_tokens": float64(276000)}},
		"rate_limits": object{
			"five_hour": object{"used_percentage": float64(88), "resets_at": float64(now + 3*3600)},
			"seven_day": object{"used_percentage": float64(20), "resets_at": float64(now + 3*86400)},
		},
	}, now, true)

	resumeWait(sid, "")

	if fresh, _ := freshLaunchOf(calls); fresh == "" {
		t.Fatalf("the fresh start was held for a compaction only the context it leaves behind was near: %q, wait %v", launchLines(calls), waitOf(sid))
	}
}

func TestAFreshStartAfterAFullContextNeedsNoUsageData(t *testing.T) {
	calls := relaunchSandboxWith(t, fakeClaude("sleep 1", answerWhereItRuns, "exit 1"))
	sid := "fullcontext4"
	parkFullContext(t, sid)
	// Neither the status line nor the usage endpoint has given any usage (an account on an API key,
	// say). The context filled up whatever the limits are, so nothing waits for them to be known.
	if err := os.Remove(files.usage); err != nil {
		t.Fatal(err)
	}

	resumeWait(sid, "")

	if fresh, _ := freshLaunchOf(calls); fresh == "" {
		t.Fatalf("a session that stopped with its context full was not started afresh for want of usage data: %v (journal %v)", waitOf(sid), journaledFor(sid))
	}
}

func TestASessionThatStoppedWithItsContextFullAndHasNoHandoffNoteIsToldWhyWhenResumed(t *testing.T) {
	calls := relaunchSandboxWith(t, fakeClaude("exit 0"))
	sid := "fullcontext2"
	_, note := parkBigPause(t, sid, "headless", 1, 0)
	if err := os.Remove(note); err != nil {
		t.Fatal(err)
	}
	updateState(func(state object) { getMap(getMap(state, "waits"), sid)["contextFull"] = true })

	resumeWait(sid, "")

	if got := launchesOf(calls, sid); got != 1 {
		t.Fatalf("the session was resumed %d time(s), want 1: %q", got, launchLines(calls))
	}
	if prompt := launchPromptOf(calls); !strings.Contains(prompt, contextFullNote) {
		t.Fatalf("the resumed session is not told that it stopped with its context full: %q", prompt)
	}
}

func TestAResumedBigSessionIsToldToGiveQueueItemsToSubagents(t *testing.T) {
	cases := []struct {
		name     string
		tokens   float64
		above    any
		subagent bool
	}{
		{"160k tokens of context", 160000, float64(100000), true},
		{"40k tokens of context", 40000, float64(100000), false},
		{"160k tokens of context and the shipped default", 160000, nil, false},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := relaunchSandboxWith(t, fakeClaude("exit 0"))
			if tc.above != nil {
				config := readJSON(files.config)
				config["queue"], config[configVersionKey] = object{"subagentAboveTokens": tc.above}, configVersion
				mustWriteJSON(files.config, config)
			}
			sid := fmt.Sprintf("resumedbig%d", index+1)
			parkBigPause(t, sid, "headless", 20, tc.tokens)

			resumeWait(sid, "")

			prompt := launchPromptOf(calls)
			if !strings.Contains(prompt, "add a login page") {
				t.Fatalf("the resume prompt does not carry the checklist: %q", prompt)
			}
			if strings.Contains(prompt, `hand it to the noctis:worker subagent (subagent_type "noctis:worker"), a fresh context`) != tc.subagent {
				t.Fatalf("subagent advice = %t, want %t: %q", !tc.subagent, tc.subagent, prompt)
			}
		})
	}
}

func TestAResumedBigSessionGetsNoHandOffWhenNoSubagentCanBeOpened(t *testing.T) {
	calls := relaunchSandboxWith(t, fakeClaude("exit 0"))
	config := readJSON(files.config)
	config["queue"], config[configVersionKey] = object{"subagentAboveTokens": float64(100000)}, configVersion
	mustWriteJSON(files.config, config)
	parkBigPause(t, "hr7", "headless", 20, 160000)
	now := float64(nowSec())
	statusReadingFrom("hr7", nowSec(), 3, now+18000, 85, now+3*86400)

	resumeWait("hr7", "")

	prompt := launchPromptOf(calls)
	if !strings.Contains(prompt, "add a login page") {
		t.Fatalf("the resume prompt does not carry the checklist: %q", prompt)
	}
	if strings.Contains(prompt, "a fresh context") {
		t.Fatalf("with 10 points of weekly room the resumed session is told to hand items to a subagent: %q", prompt)
	}
}

func TestTheWindowOfAFreshSessionIsRecordedUnderThatSession(t *testing.T) {
	tools := map[string]string{}
	for _, tool := range []string{"date", "dirname", "cat"} {
		found, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("%s is not on PATH: %v", tool, err)
		}
		tools[tool] = found
	}
	_, bin, calls := terminalSandbox(t)
	for tool, found := range tools {
		if err := os.Symlink(found, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("NOCTIS_NO_TASKS", "1")
	t.Setenv("NOCTIS_NO_SCHEDULE", "1")
	t.Setenv("NOCTIS_NO_EARLY_TRIGGER", "")
	t.Setenv(handoffEnv, "")
	relaunchConfig(object{"mode": "window", "prompt": "carry on", "terminal": "sh {script}"})
	seen := filepath.Join(t.TempDir(), "state-while-open.json")
	t.Setenv("NOCTIS_TEST_STATE_SEEN", seen)
	writeStub(t, bin, "claude", fakeClaude(
		`i=0; while [ $i -lt 100 ] && ! grep -q '"how"' "$NOCTIS_TEST_STATE"; do sleep 0.05; i=$((i+1)); done`,
		`cat "$NOCTIS_TEST_STATE" > "$NOCTIS_TEST_STATE_SEEN"`,
		answerWhereItRuns, "exit 0"))
	sid := "bigwindow"
	parkBigPause(t, sid, "", 150, 160000)

	resumeWait(sid, "")

	fresh, _ := freshLaunchOf(calls)
	if fresh == "" {
		t.Fatalf("no fresh session was started: %q", launchLines(calls))
	}
	var open object
	if content, err := os.ReadFile(seen); err != nil || jsonUnmarshalObject(content, &open) != nil {
		t.Fatalf("the state while the window was open was not captured: %v", err)
	}
	launched := getMap(open, "launched")
	if getMap(launched, fresh) == nil || getMap(launched, sid) != nil {
		t.Fatalf("the window running %s was recorded as %v, so that session's next relaunch would not close it", fresh, launched)
	}
	if wait := waitOf(sid); wait != nil {
		t.Fatalf("the fresh window answered, yet the pause was kept for a retry: %v (journal %v)", wait, journaledFor(sid))
	}
}

func TestARunnerFollowsTheFreshSessionAnEarlierRunnerStartedForThisPause(t *testing.T) {
	cases := []struct {
		name     string
		answered bool
	}{
		{"the fresh session went on", true},
		{"the fresh session never answered", false},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := relaunchSandboxWith(t, fakeClaude("exit 0"))
			sid := fmt.Sprintf("crashed%d", index+1)
			fresh := fmt.Sprintf("0f000000-0000-4000-8000-00000000000%d", index+1)
			parkBigPause(t, sid, "headless", 150, 160000)
			wait := waitOf(sid)
			startedAt, now := numberOr(wait, "startedAt", 0), float64(nowSec())
			if tc.answered {
				answer := string(marshalCompact(object{"type": "assistant", "timestamp": transcriptStamp(now - 60),
					"message": object{"role": "assistant", "content": []any{object{"type": "text", "text": "Reading the handoff note."}}}}))
				if err := os.WriteFile(filepath.Join(filepath.Dir(getString(wait, "transcript")), fresh+".jsonl"), []byte(answer+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			updateState(func(state object) {
				moveSessionRecords(state, sid, fresh)
				stateMap(state, "freshStarts")[fresh] = object{"from": sid, "at": now - 120, "waitStartedAt": startedAt}
				stateMap(state, "handedOff")[sid] = object{"at": now - 120, "pid": float64(deadPid(t)), "waitStartedAt": startedAt, "fresh": fresh}
				getMap(getMap(state, "checkpoints"), sid)["consumed"] = true
			})

			resumeWait(sid, "")

			state := readState()
			if tc.answered {
				if lines := launchLines(calls); len(lines) != 0 {
					t.Fatalf("the session was relaunched although the fresh session an earlier runner started went on: %q", lines)
				}
				if waitOf(sid) != nil {
					t.Fatal("the pause the fresh session took over was kept")
				}
				if getMap(getMap(state, "autoQueues"), fresh) == nil || getMap(getMap(state, "autoQueues"), sid) != nil {
					t.Fatalf("the checklist was taken from the fresh session that works on it: %v", getMap(state, "autoQueues"))
				}
				return
			}
			if got := launchesOf(calls, sid); got != 1 {
				t.Fatalf("the session whose fresh start never answered was resumed %d time(s), want 1: %q", got, launchLines(calls))
			}
			if getMap(getMap(state, "autoQueues"), sid) == nil || getMap(getMap(state, "freshStarts"), fresh) != nil {
				t.Fatalf("the checklist did not come back from the fresh start that never answered: %v %v", getMap(state, "autoQueues"), getMap(state, "freshStarts"))
			}
			if prompt := launchPromptOf(calls); !strings.Contains(prompt, "add a login page") {
				t.Fatalf("the resume prompt does not carry the checklist it got back: %q", prompt)
			}
		})
	}
}
