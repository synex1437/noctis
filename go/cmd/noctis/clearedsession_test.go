package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// h2Window is the session one window in the test folder last showed in its status line.
type h2Window struct {
	sid, model string
	seen       float64
}

// h2FolderOfWindows writes the status line sessions of several windows open in one folder.
func h2FolderOfWindows(project string, windows ...h2Window) {
	usage := readJSON(files.usage)
	sessions := object{}
	for _, window := range windows {
		sessions[window.sid] = object{"model": window.model, "cwd": project, "transcript": filepath.Join(project, window.sid+".jsonl"), "updatedAt": window.seen, "context": nil}
	}
	usage["sessions"] = sessions
	mustWriteJSON(files.usage, usage)
}

// h2SleepingWait is a wait a hook sleeps on in project: a pause held in its hook by holder or,
// when waking is set, a same-session wake, which runs without a holder.
func h2SleepingWait(project, holder string, waking float64) object {
	now := float64(nowSec())
	wait := object{"kind": "batch", "window": "five_hour", "label": "5h", "used": float64(93), "until": now + 1800, "resumeAt": now + 1830,
		"inHook": true, "heartbeat": now - 5, "holder": holder, "cwd": project, "startedAt": now - 300}
	if waking > 0 {
		wait["kind"], wait["inHook"], wait["waking"] = "stopfailure", false, waking
		delete(wait, "holder")
	}
	return wait
}

func TestAClearInOneWindowLeavesTheLiveWaitOfAnotherWindowAlone(t *testing.T) {
	for _, other := range []struct {
		name   string
		waking bool
	}{{"a pause held in its hook", false}, {"a same-session wake still sleeping", true}} {
		t.Run(other.name, func(t *testing.T) {
			cfg, project, _ := limitSandbox(t, nil, 20, 40)
			now := float64(nowSec())
			h2FolderOfWindows(project, h2Window{"h2-other", "claude-opus-5", now - 120}, h2Window{"h2-cleared", "claude-fable-5-1", now - 5})
			wait := h2SleepingWait(project, strconv.Itoa(os.Getpid())+"-h2", 0)
			if other.waking {
				wait = h2SleepingWait(project, "", now-60)
			}
			updateState(func(state object) { stateMap(state, "waits")["h2-other"] = wait })
			hookOutput(t, onSessionStart, agentHookInput("SessionStart", "h2-fresh", project, object{"source": "clear"}), cfg)
			state := readState()
			if getMap(getMap(state, "waits"), "h2-other") == nil {
				t.Fatalf("/clear in one window released %s of another window in the same folder", other.name)
			}
			if model := getString(getMap(getMap(state, "modelOverrides"), "h2-fresh"), "model"); model != "claude-fable-5-1" {
				t.Fatalf("the session after /clear took the model %q, want claude-fable-5-1, the model of the session this window cleared", model)
			}
		})
	}
}

func TestAClearReleasesTheInterruptedWaitOfTheSessionItReplacedAndNoOther(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 20, 40)
	now := float64(nowSec())
	// Both hooks are gone: this window's was stopped before /clear, the other window was closed
	// while its hook slept, and its wait is left for the runner.
	h2FolderOfWindows(project, h2Window{"h2-closed", "claude-opus-5", now - 200}, h2Window{"h2-mine", "claude-fable-5-1", now - 3})
	updateState(func(state object) {
		stateMap(state, "waits")["h2-closed"] = h2SleepingWait(project, strconv.Itoa(deadPid(t))+"-h2", 0)
		stateMap(state, "waits")["h2-mine"] = h2SleepingWait(project, strconv.Itoa(deadPid(t))+"-h2", 0)
	})
	hookOutput(t, onSessionStart, agentHookInput("SessionStart", "h2-mine-next", project, object{"source": "clear"}), cfg)
	waits := getMap(readState(), "waits")
	if waits["h2-mine"] != nil {
		t.Fatal("/clear kept the interrupted wait of the session it replaced, so a runner would resume the cleared session")
	}
	if waits["h2-closed"] == nil {
		t.Fatal("/clear released the wait another window left for its runner when it was closed")
	}
}

func TestTheEndOfAClearedSessionLetsGoOfTheWaitItsHookSleptOn(t *testing.T) {
	cfg, project, _ := limitSandbox(t, nil, 20, 40)
	now := float64(nowSec())
	// Another window refreshed its status line after this one was interrupted, so the start of the
	// new session cannot tell which session this window cleared; the end of the cleared one can.
	h2FolderOfWindows(project, h2Window{"h2-interrupted", "claude-opus-5", now - 60}, h2Window{"h2-busy", "claude-opus-5", now - 2})
	updateState(func(state object) {
		stateMap(state, "waits")["h2-interrupted"] = h2SleepingWait(project, strconv.Itoa(deadPid(t))+"-h2", 0)
		stateMap(state, "waits")["h2-woken"] = h2SleepingWait(project, "", now-30)
		stateMap(state, "waits")["h2-closed"] = h2SleepingWait(project, strconv.Itoa(deadPid(t))+"-h2", 0)
	})
	hookOutput(t, onSessionEnd, agentHookInput("SessionEnd", "h2-closed", project, object{"reason": "prompt_input_exit"}), cfg)
	hookOutput(t, onSessionEnd, agentHookInput("SessionEnd", "h2-woken", project, object{"reason": "clear"}), cfg)
	hookOutput(t, onSessionStart, agentHookInput("SessionStart", "h2-after", project, object{"source": "clear"}), cfg)
	hookOutput(t, onSessionEnd, agentHookInput("SessionEnd", "h2-interrupted", project, object{"reason": "clear"}), cfg)
	waits := getMap(readState(), "waits")
	if waits["h2-interrupted"] != nil {
		t.Fatal("the session this window cleared kept the wait of its interrupted hook")
	}
	if waits["h2-woken"] != nil {
		t.Fatal("the session this window cleared kept its same-session wake, which would wake a conversation the person cleared")
	}
	if waits["h2-closed"] == nil {
		t.Fatal("a window that was closed let go of the wait its runner is to resume")
	}
}
