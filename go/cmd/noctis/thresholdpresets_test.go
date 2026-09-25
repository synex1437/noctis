package main

import "testing"

func TestTheShippedThresholdsAreTheBalancedPreset(t *testing.T) {
	want := map[string]float64{"session5h": 92, "weeklyAll": 95, "weeklyFable": 97}
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
