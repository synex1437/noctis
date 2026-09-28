package main

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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
		"claude-sonnet-5-5":        {2, 10, 2.5, 0.2},
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

func TestTheShippedDefaultsAreTheSynexProfileProjected(t *testing.T) {
	defaults := readJSON(filepath.Join(repoRoot(), "config.default.json"))
	roles := section(defaults, "roles")
	profile := roleProfiles["synex"]
	for _, role := range roleNames {
		want, got := getMap(profile, role), getMap(roles, role)
		if getString(want, "model") != getString(got, "model") || getString(want, "effort") != getString(got, "effort") {
			t.Errorf("config.default.json %s = %v, the SYNEX profile says %v", role, got, want)
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
	earlier := cloneObject(retiredProfiles["synex"][0])
	earlier["profile"] = "noctis"
	earlier["digest"] = object{"model": "haiku", "effort": "high"}
	earlier["planning"] = object{"model": "fable", "effort": "max"}
	if got := retunedProfile(earlier); got != "synex" {
		t.Fatalf("the 5.5 noctis assignment was not recognised as an earlier SYNEX profile (%q)", got)
	}
	synex := cloneObject(earlier)
	synex["profile"] = "synex"
	if got := retunedProfile(synex); got != "synex" {
		t.Errorf("the synex name hid the earlier profile (%q)", got)
	}
	current := cloneObject(roleProfiles["synex"])
	current["profile"] = "synex"
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
	if !strings.Contains(issues, "--profile synex") {
		t.Errorf("the self-check did not say how to adopt the re-tuned profile: %q", issues)
	}
	if strings.Contains(strings.Join(selfCheckIssues(object{"roles": current}), "; "), "--profile") {
		t.Error("the self-check nags a configuration that already has the current profile")
	}
}

func TestTheProfilesAreCodeSearchBalancedAndSynexAndOnlySynexRunsAtMax(t *testing.T) {
	if names := slices.Sorted(maps.Keys(roleProfiles)); strings.Join(names, " ") != "balanced code search synex" {
		t.Fatalf("setup offers the profiles %v, want code, search, balanced and synex", names)
	}
	for name, profile := range roleProfiles {
		for _, role := range roleNames {
			effort := getString(getMap(profile, role), "effort")
			if want := name == "synex" && (role == "code" || role == "fallback"); (effort == "max") != want {
				t.Errorf("the %s profile gives %s the effort %q; only SYNEX runs code and its fallback at max", name, role, effort)
			}
		}
	}
	for _, pick := range []struct{ profile, role string }{{"code", "code"}, {"search", "research"}, {"search", "code"}} {
		if spec := getMap(roleProfiles[pick.profile], pick.role); getString(spec, "model") != "opus" || getString(spec, "effort") != "xhigh" {
			t.Errorf("the %s profile runs %s on %v, want Opus 5.5 at xhigh", pick.profile, pick.role, spec)
		}
	}
}

func TestTheNoctisProfileNameStillMeansSynex(t *testing.T) {
	if got := profileAlias("noctis"); got != "synex" {
		t.Fatalf("--profile noctis now means %q; it named the profile that is SYNEX now", got)
	}
	roles := cloneObject(roleProfiles["synex"])
	roles["profile"] = "noctis"
	if got := describeRoles(roles); !strings.HasPrefix(got, "SYNEX: ") {
		t.Fatalf("a configuration that stored the noctis profile is described as %q, want it named SYNEX", got)
	}
}

func TestSetupNamesTheProfilesInEveryLanguage(t *testing.T) {
	locales := append([]string{"en", "tr"}, slices.Sorted(maps.Keys(extraCatalogBuilders))...)
	for _, code := range locales {
		for _, key := range []string{"roles.intro", "roles.profileQuestion", "roles.unknownProfile"} {
			text := catalogFor(code)[key]
			for _, title := range []string{"Code", "Search", "Balanced", "SYNEX"} {
				if !strings.Contains(text, title) {
					t.Errorf("%s %s does not name the %s profile: %q", code, key, title, text)
				}
			}
			if strings.Contains(text, "economy") || strings.Contains(text, "noctis") {
				t.Errorf("%s %s still names a profile that is gone: %q", code, key, text)
			}
		}
	}
}

// The profiles follow one rule from what Anthropic published for Opus 5.5 and Sonnet 5.5: a role at
// xhigh or max runs on Opus, since there Sonnet costs about as much per task, trails Opus on most
// coding benchmarks and, at max, starts review subagents of its own; a role below that may run on
// Sonnet at high, half Opus's price per token, and not lower, where Sonnet may stop to check in on
// a long task; planning stays on Opus, file search and digests on Haiku 4.5, and the fallback is
// the code role.
func TestEveryProfileKeepsSonnetToHighAndOpusToTheTopRoles(t *testing.T) {
	for name, profile := range roleProfiles {
		for _, role := range roleNames {
			spec := getMap(profile, role)
			model, effort := getString(spec, "model"), getString(spec, "effort")
			switch {
			case role == "planning" && model != "opus":
				t.Errorf("the %s profile plans on %s; planning stays on Opus 5.5", name, model)
			case (role == "digest" || role == "explore") && model != "haiku":
				t.Errorf("the %s profile runs %s on %s; file search and digests run on Haiku 4.5", name, role, model)
			case (effort == "xhigh" || effort == "max") && model != "opus":
				t.Errorf("the %s profile runs %s on %s at %s; a role at xhigh or max runs on Opus 5.5", name, role, model, effort)
			case model == "sonnet" && effort != "high":
				t.Errorf("the %s profile runs %s on Sonnet 5.5 at %q; Sonnet roles run at high", name, role, effort)
			}
		}
		code, fallback := getMap(profile, "code"), getMap(profile, "fallback")
		if getString(code, "model") != getString(fallback, "model") || getString(code, "effort") != getString(fallback, "effort") {
			t.Errorf("the %s profile falls back to %v, not to its code role %v", name, fallback, code)
		}
	}
	for _, pick := range []struct{ profile, role string }{{"code", "research"}, {"balanced", "code"}, {"balanced", "research"}} {
		if spec := getMap(roleProfiles[pick.profile], pick.role); getString(spec, "model") != "sonnet" || getString(spec, "effort") != "high" {
			t.Errorf("the %s profile runs %s on %v, want Sonnet 5.5 at high", pick.profile, pick.role, spec)
		}
	}
}

func TestEveryEarlierAssignmentOfAProfileIsNamedAsReTuned(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("NOCTIS_LANG", "en")
	for name, versions := range retiredProfiles {
		if _, current := roleProfiles[name]; !current || len(versions) == 0 {
			t.Fatalf("retiredProfiles holds %q with %d versions; want a profile setup offers, with at least one", name, len(versions))
		}
		for index, earlier := range versions {
			if sameRoles(earlier, roleProfiles[name]) {
				t.Errorf("earlier %s assignment %d is the current one", name, index+1)
			}
			roles := cloneObject(earlier)
			roles["profile"] = name
			if got := retunedProfile(roles); got != name {
				t.Errorf("earlier %s assignment %d is not named as re-tuned (%q)", name, index+1, got)
			}
			if issues := strings.Join(selfCheckIssues(object{"roles": roles}), "; "); !strings.Contains(issues, "--profile "+name) {
				t.Errorf("the self-check does not say how to adopt the re-tuned %s profile: %q", name, issues)
			}
		}
		current := cloneObject(roleProfiles[name])
		current["profile"] = name
		if got := retunedProfile(current); got != "" {
			t.Errorf("the current %s profile is named as re-tuned (%q)", name, got)
		}
	}
	for _, name := range []string{"code", "balanced"} {
		if spec := getMap(retiredProfiles[name][0], "research"); getString(spec, "model") != "opus" {
			t.Errorf("the %s profile's assignment before Sonnet 5.5 researched on %v, want Opus", name, spec)
		}
	}
}

func TestAClaudeCodeTooOldForSonnet55IsNamedWhereARoleRunsOnSonnet(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("NOCTIS_LANG", "en")
	balanced := cloneObject(roleProfiles["balanced"])
	balanced["profile"] = "balanced"
	synex := cloneObject(roleProfiles["synex"])
	synex["profile"] = "synex"
	if notice := oldSonnetNotice(balanced); notice != "" {
		t.Fatalf("with no Claude Code version seen the notice names one: %q", notice)
	}
	now := nowSec()
	recordStatusline(object{"session_id": "old", "version": "2.1.283"}, now-60, false)
	notice := oldSonnetNotice(balanced)
	for _, want := range []string{"2.1.283", "2.1.284", "claude update", T("roles.code"), T("roles.research"), T("roles.fallback")} {
		if !strings.Contains(notice, want) {
			t.Fatalf("the notice for a last session on Claude Code 2.1.283 does not name %q: %q", want, notice)
		}
	}
	if strings.Contains(notice, T("roles.planning")) {
		t.Errorf("the notice names planning, which runs on Opus: %q", notice)
	}
	for label, roles := range map[string]object{"SYNEX": synex, "a full Sonnet id": {"code": object{"model": "claude-sonnet-5-5", "effort": "high"}}} {
		if got := oldSonnetNotice(roles); got != "" {
			t.Errorf("%s puts no role on the sonnet alias, yet: %q", label, got)
		}
	}
	mustWriteJSON(files.config, object{"roles": balanced})
	cfg := loadConfig()
	if doctor := strings.Join(doctorLines(cfg), "\n"); !strings.Contains(doctor, "!!  "+notice) {
		t.Errorf("the doctor does not flag the old Claude Code for the sonnet roles:\n%s", doctor)
	}
	recordStatusline(object{"session_id": "new", "version": "2.1.284"}, now, false)
	if got := oldSonnetNotice(balanced); got != "" {
		t.Errorf("the last session runs Claude Code 2.1.284 and the notice is still given: %q", got)
	}
	if doctor := strings.Join(doctorLines(cfg), "\n"); strings.Contains(doctor, "2.1.284") {
		t.Errorf("the doctor still names a version on Claude Code 2.1.284:\n%s", doctor)
	}
}

func TestSetupNamesAClaudeCodeTooOldForTheSonnetRolesItJustSet(t *testing.T) {
	box := newCLIBox(t)
	box.env["NOCTIS_LANG"] = "en"
	now := float64(nowSec())
	cliWrite(t, filepath.Join(box.account, pluginName, "usage.json"), []byte(`{"version": "2.1.283", "sessions": {"old": {"version": "2.1.283", "updatedAt": `+strconv.FormatFloat(now, 'f', 0, 64)+`}}}`))
	balanced := box.run(t, "setup", "--config-dir", box.account, "--profile", "balanced", "--permissions", "keep")
	box.configured(t, balanced, box.account, "balanced")
	if !strings.Contains(balanced.stdout, "Sonnet 5.5 needs Claude Code 2.1.284") || !strings.Contains(balanced.stdout, "2.1.283") {
		t.Fatalf("setup put Balanced on sonnet for a Claude Code 2.1.283 account and did not say it gets an older Sonnet there:\n%s", balanced)
	}
	synex := box.run(t, "setup", "--config-dir", box.account, "--profile", "synex", "--permissions", "keep")
	box.configured(t, synex, box.account, "synex")
	if strings.Contains(synex.stdout, "2.1.284") {
		t.Fatalf("SYNEX runs nothing on sonnet, yet setup named the Claude Code Sonnet 5.5 needs:\n%s", synex)
	}
}
