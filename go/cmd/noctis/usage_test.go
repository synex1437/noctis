package main

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func soonRFC3339() string {
	return time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
}

func TestBothPayloadShapesAgree(t *testing.T) {
	scoped := scopedPatternForTest()
	soon := soonRFC3339()
	for _, key := range percentKeys {
		t.Run(key, func(t *testing.T) {
			arrayForm := object{"limits": []any{
				object{"kind": "session", key: 87.0, "resets_at": soon},
			}}
			flatForm := object{"five_hour": object{key: 87.0, "resets_at": soon}}

			fromArray := getMap(parseUsagePayload(arrayForm, scoped), "five_hour")
			fromFlat := getMap(parseUsagePayload(flatForm, scoped), "five_hour")
			if fromArray == nil {
				t.Fatalf("array form dropped a %s of 87", key)
			}
			if fromFlat == nil {
				t.Fatalf("flat form dropped a %s of 87", key)
			}
			if numberOr(fromArray, "used", -1) != numberOr(fromFlat, "used", -2) {
				t.Errorf("the two shapes disagree: array=%v flat=%v",
					fromArray["used"], fromFlat["used"])
			}
		})
	}
}

func TestUnusableKeyDoesNotHideAGoodOne(t *testing.T) {
	scoped := scopedPatternForTest()
	soon := soonRFC3339()
	broken := []any{nil, "not a number", []any{1}, object{}}

	for i, bad := range broken {
		t.Run(fmt.Sprintf("case%d", i), func(t *testing.T) {
			entry := object{"kind": "session", "percent": bad, "utilization": 87.0, "resets_at": soon}
			win := getMap(parseUsagePayload(object{"limits": []any{entry}}, scoped), "five_hour")
			if win == nil {
				t.Fatalf("percent=%#v was unusable and took utilization=87 down with it", bad)
			}
			if got := numberOr(win, "used", -1); got != 87 {
				t.Errorf("used = %v, want 87", got)
			}
		})
	}
}

func TestBareUsedIsNotReadAsAPercentage(t *testing.T) {
	scoped := scopedPatternForTest()
	entry := object{"kind": "session", "used": 45000.0, "resets_at": soonRFC3339()}
	if win := getMap(parseUsagePayload(object{"limits": []any{entry}}, scoped), "five_hour"); win != nil {
		t.Errorf("a token count was read as %v%%; percentKeys must stay percentages only",
			win["used"])
	}
}

func TestRememberedRefreshErrorIsNotLive(t *testing.T) {
	now := nowSec()
	cases := []struct {
		name  string
		fable object
		want  string
	}{
		{"backing off right now", object{"error": "http-429", "backoffUntil": float64(now + 300)}, "http-429"},
		{"backoff just expired", object{"error": "http-429", "backoffUntil": float64(now - 1)}, ""},
		{"source switched off, error frozen", object{"error": "no-token", "backoffUntil": float64(now - 86400)}, ""},
		{"no backoff recorded at all", object{"error": "http-500"}, ""},
		{"healthy file", object{"fetchedAt": float64(now)}, ""},
		{"nil", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := liveRefreshError(tc.fable, now); got != tc.want {
				t.Errorf("liveRefreshError = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOneAnswerForWhichModelASessionIsOn(t *testing.T) {
	dir := sandboxFiles(t)
	files.settings = filepath.Join(dir, "settings.json")
	files.usage = filepath.Join(dir, "usage.json")
	mustWriteJSON(files.settings, object{})

	cfg := object{"models": object{"primary": "fable", "fallback": "opus"}}
	state := object{"modelSwitched": object{"at": float64(nowSec()), "from": "fable", "to": "opus"}}

	perSession := resolveSessionModel(cfg, state, readJSON(files.usage), "S")
	global := defaultModel(cfg, state)
	if perSession != global {
		t.Errorf("the two resolvers disagree: session=%q default=%q", perSession, global)
	}
	if perSession != "opus" {
		t.Errorf("model = %q, want the fallback the plugin switched to (opus)", perSession)
	}
}

func TestModelOverrideOutlivesTheLongestWait(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	sevenDaysAgo := float64(now) - 7*86400
	state := object{
		"modelOverrides": object{"parked": object{"at": sevenDaysAgo, "model": "opus"}},
		"stopGuard":      object{"parked": object{"at": sevenDaysAgo}},
	}
	pruneState(state, now)

	if getMap(getMap(state, "modelOverrides"), "parked") == nil {
		t.Error("a weekly park lost its model override; it comes back not knowing which model it is on")
	}
	if getMap(getMap(state, "stopGuard"), "parked") != nil {
		t.Error("transient per-session records must still expire, or state grows for as long as the plugin is installed")
	}
}

func TestTwoReadingsInTheSameSecondAreNotTakenForABurst(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	reset, weekReset := float64(now+2*3600), float64(now+3*86400)
	statusReading(now-120, 20, reset, 10, weekReset)
	statusReading(now-120, 86, reset, 10, weekReset)
	statusReading(now-60, 88, reset, 10, weekReset)
	if burst := currentUsage(now).fiveHour.burst; burst != 2 {
		t.Errorf("readings of 20 and 86 %% in the same second, then 88 %% a minute later, made a burst of %v points, want the 2 points from 86 to 88", burst)
	}
	if plan := evaluate(releaseConfig(), currentUsage(now), "claude-opus-5", 0, false).wait; plan != nil {
		t.Errorf("88 %% with a pause point of 92 %% paused on a %s after two readings of the same second", plan.hit)
	}
}
