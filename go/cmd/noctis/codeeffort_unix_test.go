//go:build !windows

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestARelaunchOnACodeRoleWithoutAnEffortPassesNone(t *testing.T) {
	for _, c := range []struct {
		name, model, effort string
	}{
		{"a model that takes no effort", "claude-haiku-4-5", "max"},
		{"a code role without an effort", "claude-sonnet-4-6", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			calls := takeoverSandbox(t)
			writeScript(t, filepath.Join(filepath.Dir(calls), "claude"), "#!/bin/sh\nprintf '%s effort=%s\\n' \"$*\" \"${CLAUDE_CODE_EFFORT_LEVEL-unset}\" >> \"$NOCTIS_TEST_CALLS\"\n")
			t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "max")
			config := releaseConfig()
			config["wait"] = object{"earlyResetPollMinutes": float64(5), "heartbeatGraceSeconds": float64(1)}
			config["resume"] = object{"mode": "headless", "prompt": "carry on"}
			config["models"] = object{"primary": c.model, "fallback": "opus", "effort": c.effort}
			config["roles"] = object{"code": object{"model": c.model}, "fallback": object{"model": "opus", "effort": "high"}}
			mustWriteJSON(files.config, config)
			sid := "no-effort-" + c.model
			parkForRelaunch(t, sid, "batch", "headless")
			sessionRunsOn(sid, c.model, getString(waitOf(sid), "cwd"))

			resumeWait(sid, "")

			line := relaunchLine(t, calls, sid)
			if !strings.Contains(line, "--model "+c.model+" ") || strings.Contains(line, "--effort") || !strings.HasSuffix(line, " effort=unset") {
				t.Fatalf("a relaunch on %s (code role without an effort, models.effort %q) should pass no effort level, neither --effort nor CLAUDE_CODE_EFFORT_LEVEL: %s", c.model, c.effort, line)
			}
		})
	}
}
