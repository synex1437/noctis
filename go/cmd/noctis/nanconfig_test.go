package main

import (
	"math"
	"strings"
	"testing"
)

func TestNotANumberIsNoNumberAnywhere(t *testing.T) {
	for _, bad := range []any{"NaN", "nan", " NaN ", "Inf", "+Inf", "-Inf", "Infinity", "-infinity", math.NaN(), math.Inf(1), math.Inf(-1)} {
		if number, ok := toNumber(bad); ok {
			t.Errorf("toNumber(%#v) = %v, accepted as a number", bad, number)
		}
	}
	for _, good := range []struct {
		value any
		want  float64
	}{{"42", 42}, {" 7.5 ", 7.5}, {float64(3), 3}, {int(5), 5}, {int64(6), 6}, {"-2", -2}} {
		if number, ok := toNumber(good.value); !ok || number != good.want {
			t.Errorf("toNumber(%#v) = %v, %t; want %v", good.value, number, ok, good.want)
		}
	}
}

func TestACeilingOrAReadingOfNaNFallsBackInsteadOfSwitchingTheGuardOff(t *testing.T) {
	previous := locale
	t.Cleanup(func() { locale = previous })
	locale = "en"
	for _, bad := range []string{"NaN", "Inf", "-Inf"} {
		cfg := creditsConfig()
		cfg["credits"] = object{"allowPaid": false, "ceiling": bad, "fanOutHeadroom": float64(25)}
		if ceiling := creditCeiling(cfg); ceiling != creditCeilingDefault {
			t.Errorf("credits.ceiling %q gives a ceiling of %v, want the default %v", bad, ceiling, creditCeilingDefault)
		}
		cfg["thresholds"] = object{"session5h": nil, "weeklyAll": nil, "weeklyFable": nil}
		if result := evaluate(cfg, usageFrom(100, 40, float64(nowSec()+3600)), "claude-opus-5", 0, false); result.wait == nil || result.wait.hit != "ceiling" {
			t.Errorf("credits.ceiling %q: a window at 100%% did not stop at the ceiling: %+v", bad, result.wait)
		}
		if text := creditsText(cfg); strings.Contains(text, "NaN") || strings.Contains(text, "Inf") || !strings.Contains(text, "100%") {
			t.Errorf("credits.ceiling %q: status says %q", bad, text)
		}
	}
	now := float64(nowSec())
	if used, _, ok := storedWindow(object{"used": "NaN", "resetsAt": now + 3600}); ok {
		t.Errorf("a stored window with used \"NaN\" was read as a reading of %v", used)
	}
	if _, resetsAt, ok := storedWindow(object{"used": float64(50), "resetsAt": "Inf"}); ok {
		t.Errorf("a stored window that resets at \"Inf\" was read as resetting at %v", resetsAt)
	}
}
