package main

import (
	"strings"
	"testing"
)

func TestPauseNoticesPromiseOnlyWhatTheGuardWillDo(t *testing.T) {
	previous := locale
	t.Cleanup(func() { locale = previous })
	locale = "en"
	at := float64(nowSec() + 3600)
	threshold := &waitPlan{window: "five_hour", label: "5h", used: 93, threshold: 92, until: at, hit: "threshold"}
	ceiling := &waitPlan{window: "five_hour", label: "5h", used: 100, threshold: 100, until: at, hit: "ceiling"}
	relaunching := object{"resume": object{"mode": "window"}}
	manual := object{"resume": object{"mode": "none"}}

	if text := alreadyOverNotice(threshold); !strings.Contains(text, T("session.typedHint")) || strings.Contains(text, "/noctis:pause") {
		t.Fatalf("below the ceiling the session start does not say that prompts you type still go ahead: %q", text)
	}
	if text := alreadyOverNotice(ceiling); strings.Contains(text, "/noctis:pause") {
		t.Fatalf("at the 100 %% ceiling, where a pause is ignored, the session start still advises one: %q", text)
	}
	if text := savedStop(relaunching, "prompt", threshold, at, ""); !strings.Contains(text, "auto-resume") || !strings.Contains(text, "/noctis:pause 120") {
		t.Fatalf("a relaunching pause lost its auto-resume time or its advice: %q", text)
	}
	if text := savedStop(relaunching, "prompt", ceiling, at, ""); strings.Contains(text, "/noctis:pause") || strings.Contains(text, "noctis off") {
		t.Fatalf("a pause at the 100 %% ceiling advises a pause the guard ignores there: %q", text)
	}
	for _, kind := range []string{"prompt", "batch"} {
		if text := savedStop(manual, kind, threshold, at, ""); strings.Contains(text, "auto-resume") || !strings.Contains(text, "resume it yourself") {
			t.Fatalf("with resume.mode none a %s pause promises what will not happen: %q", kind, text)
		}
	}
}
