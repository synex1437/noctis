package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// u3OffBox is an account switched off for two hours an hour ago, with the last hook pulse from
// before that: Claude went on working, and the status line saw the five-hour window move nine
// times since.
func u3OffBox(t *testing.T) leanBox {
	t.Helper()
	box := newLeanBox(t)
	guardDir := filepath.Join(box.account, pluginName)
	now := nowSec()
	cliWrite(t, filepath.Join(guardDir, "state.json"), marshalPretty(object{"disabledUntil": float64(now + 3600), "lastHookAt": float64(now - 4000)}))
	history := []any{}
	for step := int64(0); step < 9; step++ {
		history = append(history, object{"used": float64(10 + step), "resetsAt": float64(now + 7200), "at": float64(now - 900 + step*100)})
	}
	cliWrite(t, filepath.Join(guardDir, "usage.json"), marshalPretty(object{"updatedAt": float64(now - 60), "history": object{"five_hour": history}}))
	return box
}

func TestWhileNoctisIsOffTheStatusLineDoesNotTakeTheHooksThatRunForDeadOnes(t *testing.T) {
	for event, fields := range map[string]string{
		"UserPromptSubmit": `"prompt":"go on with the refactor"`,
		"PostToolBatch":    `"transcript_path":"none.jsonl"`,
		"Stop":             `"stop_hook_active":false`,
		"PreToolUse":       `"tool_name":"Agent","tool_input":{"subagent_type":"Explore","prompt":"map the repo"}`,
	} {
		t.Run(event, func(t *testing.T) {
			box := u3OffBox(t)
			now := nowSec()
			cwd := forwardSlashes(box.home)
			if run := box.run(t, `{"hook_event_name":"`+event+`","session_id":"u3","cwd":"`+cwd+`",`+fields+`}`, "hook"); run.code != 0 {
				t.Fatalf("the hook failed:\n%s", run)
			}
			run := box.run(t, `{"session_id":"u3","model":{"id":"claude-opus-5-5","display_name":"Opus 5.5"},"cwd":"`+cwd+`",`+
				`"rate_limits":{"five_hour":{"used_percentage":19,"resets_at":`+formatNumber(float64(now+7200))+`},"seven_day":{"used_percentage":10,"resets_at":`+formatNumber(float64(now+3*86400))+`}}}`, "statusline")

			if run.code != 0 {
				t.Fatalf("the status line failed:\n%s", run)
			}
			if strings.Contains(run.stdout, "hooks inactive") {
				t.Fatalf("with noctis off and a %s hook just run, the status line says the hooks are inactive: %q", event, strings.TrimSpace(run.stdout))
			}
			if logged, _ := os.ReadFile(filepath.Join(box.account, pluginName, "errors.log")); strings.Contains(string(logged), "hooks appear inactive") {
				t.Fatalf("with noctis off and a %s hook just run, errors.log says the hooks are inactive:\n%s", event, logged)
			}
		})
	}
}

func TestWhileNoctisIsOffTheSelftestProbeIsStillNoHookPulse(t *testing.T) {
	box := u3OffBox(t)
	cwd := forwardSlashes(box.home)
	if run := box.run(t, `{"hook_event_name":"PostToolBatch","session_id":"`+selftestSession+`","cwd":"`+cwd+`"}`, "hook"); run.code != 0 {
		t.Fatalf("the probe failed:\n%s", run)
	}
	state := readJSON(filepath.Join(box.account, pluginName, "state.json"))
	if at := numberOr(state, "lastHookAt", 0); at > float64(nowSec()-3600) {
		t.Fatalf("the selftest's probe, run while noctis is off, was recorded as a hook pulse (lastHookAt %v)", at)
	}
}
