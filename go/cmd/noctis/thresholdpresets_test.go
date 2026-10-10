package main

import "testing"

func TestTheShippedThresholdsAreTheBalancedPreset(t *testing.T) {
	want := map[string]float64{"session5h": 92, "weeklyAll": 97, "weeklyFable": 97}
	shipped := getMap(shippedDefaults(t), "thresholds")
	for key, value := range want {
		if got, _ := getNumber(shipped, key); got != value {
			t.Errorf("config.default.json pauses %s at %v %%, want %v %%", key, shipped[key], value)
		}
		if got := numberOr(thresholdPresets["balanced"], key, 0); got != value {
			t.Errorf("--preset balanced pauses %s at %v %%, but a fresh install pauses it at %v %%", key, got, value)
		}
	}
}

func TestEachPresetPausesLaterThanTheOneBeforeIt(t *testing.T) {
	order := []string{"conservative", "balanced", "aggressive"}
	for _, key := range []string{"session5h", "weeklyAll", "weeklyFable"} {
		for i := 1; i < len(order); i++ {
			earlier, later := numberOr(thresholdPresets[order[i-1]], key, 0), numberOr(thresholdPresets[order[i]], key, 0)
			if later <= earlier || later > 100 {
				t.Errorf("--preset %s pauses %s at %v %%, --preset %s at %v %%: each preset should pause later than the one before it, at most at 100 %%", order[i-1], key, earlier, order[i], later)
			}
		}
	}
}

func TestAConfigOnTheFormerWeeklyPausePointMovesToNinetySevenOnce(t *testing.T) {
	leanSandbox(t)
	mustWriteJSON(files.config, object{
		configVersionKey: float64(2),
		"thresholds":     object{"session5h": float64(92), "weeklyAll": float64(95), "weeklyFable": float64(97)},
		"queue":          object{"subagentAboveTokens": float64(100000)},
	})
	if got := thresholdOf(loadConfig(), "weeklyAll"); got != 97 {
		t.Fatalf("a config.json noctis wrote before 8.7.1 pauses the weekly window at %v %%, want 97 %%", got)
	}
	if _, _, err := mergeConfig(files.config, shippedDefaults(t)); err != nil {
		t.Fatal(err)
	}
	stored := readJSON(files.config)
	thresholds := getMap(stored, "thresholds")
	if got := numberOr(thresholds, "weeklyAll", -1); got != 97 || numberOr(stored, configVersionKey, 0) != configVersion {
		t.Fatalf("the merge left weeklyAll at %v and %s at %v in config.json, want 97 and %v", got, configVersionKey, stored[configVersionKey], configVersion)
	}
	if numberOr(thresholds, "session5h", -1) != 92 || numberOr(thresholds, "weeklyFable", -1) != 97 {
		t.Fatalf("the move changed the other pause points: %v", thresholds)
	}
	if got := numberOr(getMap(stored, "queue"), "subagentAboveTokens", -1); got != 100000 {
		t.Fatalf("a hand-off point set on configVersion 2 became %v", got)
	}

	thresholds["weeklyAll"] = float64(95)
	mustWriteJSON(files.config, stored)
	if _, _, err := mergeConfig(files.config, shippedDefaults(t)); err != nil {
		t.Fatal(err)
	}
	if got := thresholdOf(loadConfig(), "weeklyAll"); got != 95 {
		t.Fatalf("95 %%, set again after the move, read as %v %%", got)
	}
}

func TestAWeeklyPausePointOtherThanTheFormerDefaultStaysAsItIs(t *testing.T) {
	leanSandbox(t)
	for _, own := range []any{float64(82), float64(90), float64(94.5), float64(96), float64(98), float64(0), false} {
		mustWriteJSON(files.config, object{"thresholds": object{"weeklyAll": own}})
		if _, _, err := mergeConfig(files.config, shippedDefaults(t)); err != nil {
			t.Fatal(err)
		}
		if got := getMap(readJSON(files.config), "thresholds")["weeklyAll"]; got != own {
			t.Errorf("a merge changed a weekly pause point of %v to %v", own, got)
		}
	}
}
