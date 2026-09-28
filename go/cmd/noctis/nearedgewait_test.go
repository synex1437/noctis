package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// u8Lab is a refreshLab whose usage endpoint answers at once, with five % in the 5-hour window.
func u8Lab(t *testing.T, five float64) *refreshLab {
	t.Helper()
	lab := newRefreshLab(t, false)
	now := nowSec()
	body := limitsBody(five, float64(now+3*3600), 30, float64(now+3*86400))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	lab.env = append(lab.env, "NOCTIS_USAGE_URL="+server.URL)
	return lab
}

// u8SubagentHook runs one hook of a subagent in the lab and returns what it told the host.
func u8SubagentHook(lab *refreshLab, event string) string {
	lab.t.Helper()
	input := object{"hook_event_name": event, "session_id": "u8-main", "cwd": lab.project, "agent_id": "u8-agent", "agent_type": "general-purpose"}
	if event == "PreToolUse" {
		input["tool_name"], input["tool_input"] = "Read", object{"file_path": "go.mod"}
	} else {
		input["tool_calls"] = []any{object{"tool_name": "Read"}}
	}
	executable, err := os.Executable()
	if err != nil {
		lab.t.Fatal(err)
	}
	command := exec.Command(executable, "hook")
	command.Env, command.Dir = lab.env, lab.project
	command.Stdin = bytes.NewReader(marshalCompact(input))
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		lab.t.Fatalf("the %s hook failed: %v\n%s", event, err, stderr.String())
	}
	return string(output)
}

func TestASubagentNearAPausePointIsCheckedOnTheAnswerItsHookWaitedFor(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolBatch"} {
		t.Run(event, func(t *testing.T) {
			lab := u8Lab(t, 97)
			// 86 % is inside the band below the default 92 % pause point, where a reading older
			// than two minutes is due.
			lab.reading(nowSec()-150, 86)

			output := u8SubagentHook(lab, event)

			if !strings.Contains(output, "usage is 97%") {
				t.Fatalf("near the pause point the usage endpoint answered 97%% at once, yet the subagent's %s hook decided on the 86%% reading it had: %q", event, output)
			}
		})
	}
}

func TestAPausedGuardNearTheCeilingIsCheckedOnTheAnswerItsHookWaitedFor(t *testing.T) {
	lab := u8Lab(t, 100)
	mustWriteJSON(files.state, object{"disabledUntil": float64(nowSec() + 3600)})
	// 95 % is 5 points below the paid-credit ceiling, where a reading older than 15 s is due.
	lab.reading(nowSec()-150, 95)

	output := u8SubagentHook(lab, "PreToolUse")

	if !strings.Contains(output, "paid-credit ceiling") {
		t.Fatalf("with the guard paused 5 points below the ceiling the usage endpoint answered 100%% at once, yet the hook decided on the 95%% reading it had and let the subagent go on: %q", output)
	}
}
