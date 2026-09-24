package main

import (
	"strings"
	"testing"
)

func TestTheFableBadgeWarnsNearItsPausePointOnlyWhileItIsGuarded(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	usage := usageView{hasAny: true, fable: &window{used: 93, resetsAt: float64(now + 86400)}}
	if badge := usageBadgeAt(usage, object{"thresholds": object{"weeklyFable": float64(95)}}, now); !strings.Contains(badge, "⚠Fable") {
		t.Fatalf("Fable at 93 %% with its pause point at 95 %% shows no warning: %q", badge)
	}
	if badge := usageBadgeAt(usage, object{"thresholds": object{"weeklyFable": float64(95)}}, now); strings.Count(badge, "⚠") != 1 {
		t.Fatalf("the Fable warning appears more than once: %q", badge)
	}
	if badge := usageBadgeAt(usage, object{"thresholds": object{"weeklyFable": false}}, now); strings.Contains(badge, "⚠") {
		t.Fatalf("Fable's guard is off, yet its badge warns: %q", badge)
	}
	low := usageView{hasAny: true, fable: &window{used: 40, resetsAt: float64(now + 86400)}}
	if badge := usageBadgeAt(low, object{"thresholds": object{"weeklyFable": float64(95)}}, now); strings.Contains(badge, "⚠") {
		t.Fatalf("Fable at 40 %% warns: %q", badge)
	}
}
