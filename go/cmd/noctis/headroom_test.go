package main

import (
	"strings"
	"testing"
)

func TestAWorkflowNeedsFifteenPointsOfRoomOnTheShippedConfig(t *testing.T) {
	shipped := shippedDefaults(t)
	if got, _ := getNumber(getMap(shipped, "credits"), "fanOutHeadroom"); got != 15 || fanOutHeadroomDefault != 15 {
		t.Fatalf("config.default.json asks a workflow for %v points of room and the built-in fallback for %v, want 15 for both", got, fanOutHeadroomDefault)
	}
	cfg := object{"thresholds": getMap(shipped, "thresholds"), "credits": getMap(shipped, "credits"), "workflow": object{"gate": true}}
	reset := float64(nowSec() + 3600)
	for _, at := range []struct {
		fiveHour, weekly float64
		launches         bool
	}{
		{77, 80, true},
		{67, 70, true},
		{77.5, 30, false},
		{30, 80.5, false},
	} {
		refusal := gateWorkflowLaunch(cfg, decision{usage: usageFrom(at.fiveHour, at.weekly, reset)}, nil)
		if launches := refusal == ""; launches != at.launches {
			t.Errorf("5-hour at %v %%, weekly at %v %%: the workflow launches %v, want %v (%q)", at.fiveHour, at.weekly, launches, at.launches, refusal)
		}
		if refusal != "" && !strings.Contains(refusal, "A workflow needs 15 points of room") {
			t.Errorf("5-hour at %v %%, weekly at %v %%: the refusal does not ask for 15 points: %q", at.fiveHour, at.weekly, refusal)
		}
	}
}

func TestAConfigStillOnTheFormerHeadroomMovesToFifteenOnce(t *testing.T) {
	leanSandbox(t)
	mustWriteJSON(files.config, object{"credits": object{"allowPaid": false, "ceiling": float64(100), "fanOutHeadroom": float64(25)}})
	if got := fanOutHeadroom(loadConfig()); got != 15 {
		t.Fatalf("a config.json noctis wrote before 8.6.4 asks a workflow for %v points of room, want 15", got)
	}
	if _, _, err := mergeConfig(files.config, shippedDefaults(t)); err != nil {
		t.Fatal(err)
	}
	stored := readJSON(files.config)
	if got, _ := getNumber(getMap(stored, "credits"), "fanOutHeadroom"); got != 15 {
		t.Fatalf("the merge left fanOutHeadroom at %v in config.json, want 15", got)
	}
	if got, _ := getNumber(stored, configVersionKey); got != configVersion {
		t.Fatalf("the merge recorded %s as %v, want %v", configVersionKey, stored[configVersionKey], configVersion)
	}

	getMap(stored, "credits")["fanOutHeadroom"] = float64(25)
	mustWriteJSON(files.config, stored)
	if got := fanOutHeadroom(loadConfig()); got != 25 {
		t.Fatalf("25 points of room, set again after the move, read as %v", got)
	}
	if _, _, err := mergeConfig(files.config, shippedDefaults(t)); err != nil {
		t.Fatal(err)
	}
	if got, _ := getNumber(getMap(readJSON(files.config), "credits"), "fanOutHeadroom"); got != 25 {
		t.Fatalf("a merge moved 25 points of room, set after the move, to %v", got)
	}
}

func TestAHeadroomOtherThanTheFormerDefaultStaysAsItIs(t *testing.T) {
	leanSandbox(t)
	for _, own := range []float64{0, 10, 24, 26, 40} {
		mustWriteJSON(files.config, object{"credits": object{"fanOutHeadroom": own}})
		if got := fanOutHeadroom(loadConfig()); got != own {
			t.Errorf("%v points of room read as %v", own, got)
		}
		if _, _, err := mergeConfig(files.config, shippedDefaults(t)); err != nil {
			t.Fatal(err)
		}
		if got, _ := getNumber(getMap(readJSON(files.config), "credits"), "fanOutHeadroom"); got != own {
			t.Errorf("a merge changed %v points of room to %v", own, got)
		}
	}

	mustWriteJSON(files.config, object{"credits": object{"allowPaid": true}})
	if _, _, err := mergeConfig(files.config, shippedDefaults(t)); err != nil {
		t.Fatal(err)
	}
	stored := readJSON(files.config)
	if got, _ := getNumber(getMap(stored, "credits"), "fanOutHeadroom"); got != 15 || !getBool(getMap(stored, "credits"), "allowPaid", false) {
		t.Fatalf("a config.json with no headroom of its own did not take 15 beside its own allowPaid: %v", stored["credits"])
	}
}
