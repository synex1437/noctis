package main

import (
	"os"
	"strings"
	"testing"
)

func TestNoPauseIsAdvisedWhereThePaidCreditCeilingIgnoresIt(t *testing.T) {
	cases := []struct {
		name     string
		readings [][3]float64
		ignored  bool
	}{
		{"a first reading of the weekly window at 99 %", [][3]float64{{30, 30, 99}}, false},
		{"the weekly window at 99 % after a 76-point jump in nine minutes", [][3]float64{{600, 30, 23}, {30, 30, 99}}, true},
		{"a first reading of the weekly window at 100 %", [][3]float64{{30, 30, 100}}, true},
		{"the 5-hour window at 85 % after an 18-point jump in nine minutes", [][3]float64{{600, 67, 20}, {30, 85, 20}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, project, _ := limitSandbox(t, object{"wait": object{"maxInHookMinutes": float64(0)}}, 20, 20)
			if err := os.Remove(files.usage); err != nil {
				t.Fatal(err)
			}
			now := nowSec()
			fiveReset, weekReset := float64(now+7200), float64(now+3*86400)
			for _, reading := range c.readings {
				statusReading(now-int64(reading[0]), reading[1], fiveReset, reading[2], weekReset)
			}
			advises := func(text string) bool {
				return strings.Contains(text, "/noctis:pause") || strings.Contains(text, "noctis off")
			}
			start := getString(hookOutput(t, onSessionStart, agentHookInput("SessionStart", "ca", project, object{"source": "startup"}), cfg), "systemMessage")
			if !strings.Contains(start, "⏸") || advises(start) || strings.Contains(start, T("session.typedHint")) == c.ignored {
				t.Errorf("session start, with a pause ignored=%v: %q", c.ignored, start)
			}
			prompt := promptInput("ca", project, "start with the parser refactor")
			answer := hookOutput(t, onUserPromptSubmit, prompt, cfg)
			switch {
			case c.ignored && (getString(answer, "decision") != "block" || advises(getString(answer, "reason"))):
				t.Errorf("at the paid-credit ceiling the prompt was not held, or was told that a pause lifts the stop: %v", answer)
			case !c.ignored && (getString(answer, "decision") == "block" || advises(getString(answer, "systemMessage"))):
				t.Errorf("below the paid-credit ceiling the prompt you typed was held, or was pointed at a pause: %v", answer)
			}
			updateState(func(state object) { state["disabledUntil"] = float64(now + 3600) })
			paused := hookOutput(t, onUserPromptSubmit, prompt, cfg)
			switch {
			case c.ignored && getString(paused, "decision") != "block":
				t.Errorf("with noctis off the prompt went on toward the paid-credit ceiling: %v", paused)
			case c.ignored && advises(getString(paused, "reason")):
				t.Errorf("with noctis off already on, the refusal still advises a pause: %v", paused)
			case !c.ignored && paused != nil:
				t.Errorf("with noctis off the prompt was held although nothing says the next turn crosses 100 %%: %v", paused)
			}
		})
	}
}
