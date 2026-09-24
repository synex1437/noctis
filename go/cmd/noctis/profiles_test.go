package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestASecondScopedHitStillRestoresTheEffort(t *testing.T) {
	sandboxFiles(t)
	mustWriteJSON(files.settings, object{"model": "fable", "env": object{"CLAUDE_CODE_EFFORT_LEVEL": "max"}})
	cfg := object{
		"models":     object{"primary": "fable", "fallback": "opus"},
		"roles":      object{"fallback": object{"model": "opus", "effort": "high"}},
		"fable":      object{"revertOnReset": true},
		"thresholds": object{"weeklyFable": float64(95)},
	}
	now := nowSec()
	persistModelSwitch(cfg, float64(now+3600), now)
	persistModelSwitch(cfg, float64(now+3600), now+5)
	usage := usageView{hasAny: true, fable: &window{used: 1, resetsAt: float64(now + 10)}}
	if note := maybeRevertDefaultModel(cfg, readState(), usage, now+20); note == "" {
		t.Fatal("the scoped window cleared but nothing was reverted")
	}
	settings := readJSON(files.settings)
	if got := getString(settings, "model"); got != "fable" {
		t.Errorf("model after the revert = %q, want fable", got)
	}
	if got := getString(getMap(settings, "env"), "CLAUDE_CODE_EFFORT_LEVEL"); got != "max" {
		t.Errorf("a second session hitting the same cap made the revert forget effort max; settings.json says %q", got)
	}
}

func TestTheRevertGivesBackTheModelTheSwitchReplaced(t *testing.T) {
	sandboxFiles(t)
	mustWriteJSON(files.settings, object{"model": "fable", "env": object{"CLAUDE_CODE_EFFORT_LEVEL": "max"}})
	cfg := object{
		"models":     object{"primary": "opus", "fallback": "opus"},
		"roles":      object{"fallback": object{"model": "opus", "effort": "max"}},
		"fable":      object{"revertOnReset": true},
		"thresholds": object{"weeklyFable": float64(95)},
	}
	now := nowSec()
	persistModelSwitch(cfg, float64(now+3600), now)
	if got := settingsModel(); got != "opus" {
		t.Fatalf("at the Fable cap the default model should move to the fallback, settings.json says %q", got)
	}
	usage := usageView{hasAny: true, fable: &window{used: 1, resetsAt: float64(now + 10)}}
	note := maybeRevertDefaultModel(cfg, readState(), usage, now+20)
	if got := settingsModel(); got != "fable" {
		t.Errorf("someone who moved up to Fable was left on %q after the Fable quota reset", got)
	}
	if !strings.Contains(note, "fable") {
		t.Errorf("the revert notice names the wrong model: %q", note)
	}
}

func TestOpus55IsPricedAsOpus55(t *testing.T) {
	cases := map[string]modelPrice{
		"claude-opus-5-5":          {4, 20, 5, 0.2},
		"claude-opus-5-5-20260922": {4, 20, 5, 0.2},
		"claude-opus-5":            {5, 25, 6.25, 0.5},
		"claude-fable-5-1":         {10, 50, 12.5, 0.25},
		"claude-sonnet-5":          {2, 10, 2.5, 0.2},
		"claude-haiku-4-5":         {1, 5, 1.25, 0.1},
	}
	for model, want := range cases {
		got, ok := priceFor(object{}, model)
		if !ok || got != want {
			t.Errorf("%s priced as %+v (found %v), want %+v", model, got, ok, want)
		}
	}
}

func TestNoProfilePromisesAnEffortItsModelCannotTake(t *testing.T) {
	check := func(where string, roles object) {
		for _, role := range roleNames {
			spec := getMap(roles, role)
			if effort := getString(spec, "effort"); effort != "" && !modelTakesEffort(getString(spec, "model")) {
				t.Errorf("%s gives %s the effort %s, but %s takes no effort level", where, role, effort, getString(spec, "model"))
			}
		}
	}
	for name, profile := range roleProfiles {
		check("profile "+name, profile)
	}
	check("config.default.json", section(readJSON(filepath.Join(repoRoot(), "config.default.json")), "roles"))
}

func TestAHaikuEffortFromAFlagIsNotStored(t *testing.T) {
	for role, value := range map[string]string{"digest": "haiku:high", "research": "claude-haiku-4-5:low", "code": "haiku:max"} {
		spec, err := parseRoleFlag(role, value, false)
		if err != nil {
			t.Fatalf("%s %s: %v", role, value, err)
		}
		if effort := getString(spec, "effort"); effort != "" {
			t.Errorf("%s stored effort %s for a model that takes none", role, effort)
		}
	}
	spec, err := parseRoleFlag("research", "sonnet:high", false)
	if err != nil || getString(spec, "effort") != "high" {
		t.Fatalf("a model that takes effort lost it: %v %v", spec, err)
	}
}

func TestTheShippedDefaultsAreTheNoctisProfileProjected(t *testing.T) {
	defaults := readJSON(filepath.Join(repoRoot(), "config.default.json"))
	roles := section(defaults, "roles")
	profile := roleProfiles["noctis"]
	for _, role := range roleNames {
		want, got := getMap(profile, role), getMap(roles, role)
		if getString(want, "model") != getString(got, "model") || getString(want, "effort") != getString(got, "effort") {
			t.Errorf("config.default.json %s = %v, the noctis profile says %v", role, got, want)
		}
	}
	models := section(defaults, "models")
	pinned := getMap(section(defaults, "router"), "subagentModels")
	pairs := [][2]string{
		{getString(models, "primary"), getString(getMap(profile, "code"), "model")},
		{getString(models, "effort"), getString(getMap(profile, "code"), "effort")},
		{getString(models, "fallback"), getString(getMap(profile, "fallback"), "model")},
		{getString(pinned, "Plan"), getString(getMap(profile, "planning"), "model")},
		{getString(pinned, "Explore"), getString(getMap(profile, "explore"), "model")},
		{getString(models, "scopedPattern"), scopedFamily(getString(models, "primary"))},
	}
	for _, pair := range pairs {
		if pair[0] != pair[1] {
			t.Errorf("config.default.json projects %q where the profile gives %q", pair[0], pair[1])
		}
	}
	for role, file := range map[string]string{"research": "lite.md", "digest": "digest.md"} {
		content, err := os.ReadFile(filepath.Join(repoRoot(), "agents", file))
		if err != nil {
			t.Fatal(err)
		}
		spec := getMap(profile, role)
		if !regexp.MustCompile(`(?m)^model: ` + regexp.QuoteMeta(getString(spec, "model")) + `$`).Match(content) {
			t.Errorf("agents/%s does not run on %s", file, getString(spec, "model"))
		}
		effortLine := regexp.MustCompile(`(?m)^effort: (.*)$`).FindSubmatch(content)
		shipped := ""
		if effortLine != nil {
			shipped = string(effortLine[1])
		}
		if shipped != appliedEffort(role, spec) {
			t.Errorf("agents/%s ships effort %q, the profile applies %q", file, shipped, appliedEffort(role, spec))
		}
	}
}

func TestTheScopedRuleWatchesFableWhateverTheCodeModel(t *testing.T) {
	dir := t.TempDir()
	for code, want := range map[string]string{"opus": "fable", "sonnet": "fable", "claude-opus-5-5": "fable", "fable": "fable", "claude-mythos-5-1": "mythos"} {
		config := object{}
		applyRoles(filepath.Join(dir, "config.json"), config, object{"code": object{"model": code, "effort": "high"}})
		if got := getString(section(config, "models"), "scopedPattern"); got != want {
			t.Errorf("code on %s watches the %q bucket, want %q", code, got, want)
		}
	}
}

func TestAnEarlierShippedProfileIsNamedNotSwitched(t *testing.T) {
	sandboxFiles(t)
	earlier := cloneObject(retiredProfiles["noctis"])
	earlier["profile"] = "noctis"
	earlier["digest"] = object{"model": "haiku", "effort": "high"}
	earlier["planning"] = object{"model": "fable", "effort": "max"}
	if got := retunedProfile(earlier); got != "noctis" {
		t.Fatalf("the 5.5 noctis assignment was not recognised as an earlier noctis profile (%q)", got)
	}
	synex := cloneObject(earlier)
	synex["profile"] = "synex"
	if got := retunedProfile(synex); got != "noctis" {
		t.Errorf("the synex alias hid the earlier profile (%q)", got)
	}
	current := cloneObject(roleProfiles["noctis"])
	current["profile"] = "noctis"
	edited := cloneObject(earlier)
	edited["code"] = object{"model": "opus", "effort": "high"}
	custom := cloneObject(earlier)
	custom["profile"] = "custom"
	for name, roles := range map[string]object{"current": current, "hand-edited": edited, "custom": custom} {
		if got := retunedProfile(roles); got != "" {
			t.Errorf("%s roles were reported as an earlier profile (%q)", name, got)
		}
	}
	issues := strings.Join(selfCheckIssues(object{"roles": earlier}), "; ")
	if !strings.Contains(issues, "--profile noctis") {
		t.Errorf("the self-check did not say how to adopt the re-tuned profile: %q", issues)
	}
	if strings.Contains(strings.Join(selfCheckIssues(object{"roles": current}), "; "), "--profile") {
		t.Error("the self-check nags a configuration that already has the current profile")
	}
}
