package main

import "testing"

func TestAWaitSettingOfTheWrongTypeKeepsTheShippedValue(t *testing.T) {
	leanSandbox(t)
	previousHost, previousEvent := activeHost, activeEvent
	t.Cleanup(func() { activeHost, activeEvent = previousHost, previousEvent })
	activeHost, activeEvent = "claude", "Stop"
	now := nowSec()
	deferralEnd := float64(now + 30*60)
	for _, own := range []any{"5h", "330 minutes", nil, true, object{"minutes": float64(330)}} {
		mustWriteJSON(files.config, object{"wait": object{"maxInHookMinutes": own}})
		if !deferralWaitFits(loadConfig(), object{}, object{}, deferralEnd, now) {
			t.Errorf("wait.maxInHookMinutes=%#v: a 30-minute wait no longer fits in the hook (shipped cap: 330 minutes)", own)
		}
	}
	shipped := getMap(shippedDefaults(t), "wait")
	for _, key := range []string{"resetMarginSeconds", "builtinGraceSeconds", "earlyResetPollMinutes", "heartbeatGraceSeconds"} {
		for _, own := range []any{nil, "90s", false} {
			mustWriteJSON(files.config, object{"wait": object{key: own}})
			if got, want := section(loadConfig(), "wait")[key], shipped[key]; got != want {
				t.Errorf("wait.%s=%#v is read as %#v, not the shipped %v", key, own, got, want)
			}
		}
	}
	for _, own := range []any{float64(5), "330", []any{float64(330)}} {
		mustWriteJSON(files.config, object{"wait": own})
		if wait := section(loadConfig(), "wait"); numberOr(wait, "maxInHookMinutes", 0) != numberOr(shipped, "maxInHookMinutes", -1) || numberOr(wait, "resetMarginSeconds", 0) != numberOr(shipped, "resetMarginSeconds", -1) {
			t.Errorf("a wait section that is %#v instead of an object leaves the waits on %v, not the shipped values", own, wait)
		}
	}
	mustWriteJSON(files.config, object{"wait": object{"maxInHookMinutes": "120", "resetMarginSeconds": float64(30)}})
	if wait := section(loadConfig(), "wait"); numberOr(wait, "maxInHookMinutes", 0) != 120 || numberOr(wait, "resetMarginSeconds", 0) != 30 {
		t.Errorf("wait settings the user gave as numbers were not kept: %v", wait)
	}
}
