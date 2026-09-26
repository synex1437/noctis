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
	if strings.Contains(prompt, "fresh general-purpose subagent") {
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
	updateState(func(state object) { getMap(getMap(state, "checkpoints"), sid)["consumed"] = false })

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

func TestAResumedBigSessionIsToldToGiveQueueItemsToSubagents(t *testing.T) {
	cases := []struct {
		name     string
		tokens   float64
		subagent bool
	}{
		{"160k tokens of context", 160000, true},
		{"40k tokens of context", 40000, false},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := relaunchSandboxWith(t, fakeClaude("exit 0"))
			sid := fmt.Sprintf("resumedbig%d", index+1)
			parkBigPause(t, sid, "headless", 20, tc.tokens)

			resumeWait(sid, "")

			prompt := launchPromptOf(calls)
			if !strings.Contains(prompt, "add a login page") {
				t.Fatalf("the resume prompt does not carry the checklist: %q", prompt)
			}
			if strings.Contains(prompt, "fresh general-purpose subagent") != tc.subagent {
				t.Fatalf("subagent advice = %t, want %t: %q", !tc.subagent, tc.subagent, prompt)
			}
		})
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
