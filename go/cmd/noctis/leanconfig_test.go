package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func leanSandbox(t *testing.T) {
	t.Helper()
	dir := sandboxFiles(t)
	files.pluginRoot = dir
	mustWriteJSON(filepath.Join(dir, "config.default.json"), shippedDefaults(t))
}

func TestACompactionSettingOfTheWrongTypeKeepsTheShippedValueAndIsNamed(t *testing.T) {
	leanSandbox(t)
	shipped := leanPolicy{on: true, compactAt: 70}
	for _, bad := range []struct {
		key   string
		value any
	}{
		{"lean", "yes"},
		{"lean", float64(1)},
		{"compactAtPercent", float64(150)},
		{"compactAtPercent", float64(-5)},
		{"compactAtPercent", "abc"},
		{"compactAtPercent", "0x10"},
		{"compactAtPercent", []any{float64(80)}},
		{"compactAtPercent", true},
		{"keepTurns", float64(-1)},
		{"keepTurns", 2.5},
		{"keepTurns", nil},
		{"maxToolResultChars", float64(50)},
		{"instructions", float64(5)},
	} {
		mustWriteJSON(files.config, object{"compaction": object{bad.key: bad.value}})
		cfg := loadConfig()
		if got := leanPolicyOf(cfg); got != shipped {
			t.Fatalf("compaction.%s=%v gave %+v, want the shipped %+v", bad.key, bad.value, got, shipped)
		}
		if names := repairedCompaction(cfg); !slices.Contains(names, "compaction."+bad.key) {
			t.Fatalf("compaction.%s=%v was set aside without telling anyone: repaired %v", bad.key, bad.value, names)
		}
		if got, want := getMap(cfg, "compaction")[bad.key], getMap(shippedDefaults(t), "compaction")[bad.key]; !slices.Equal([]string{jsonText(got)}, []string{jsonText(want)}) {
			t.Fatalf("compaction.%s=%v was repaired to %v, want the shipped %v", bad.key, bad.value, got, want)
		}
	}
}

func jsonText(value any) string {
	return string(marshalCompact(value))
}

func TestACompactionValueThatIsNotAnObjectKeepsTheShippedLeanSettings(t *testing.T) {
	leanSandbox(t)
	t.Setenv("CLAUDE_AUTOCOMPACT_PCT_OVERRIDE", "")
	for _, bad := range []any{float64(85), "85", []any{float64(1)}, true, false, nil} {
		mustWriteJSON(files.config, object{"compaction": bad})
		cfg := loadConfig()
		if got := leanPolicyOf(cfg); got != (leanPolicy{on: true, compactAt: 70}) {
			t.Fatalf("compaction=%v gave %+v, want the shipped lean settings", bad, got)
		}
		if names := repairedCompaction(cfg); !slices.Contains(names, "compaction") {
			t.Fatalf("compaction=%v was set aside without telling anyone: repaired %v", bad, names)
		}
		if got := compactionGuardPercent(cfg); got != 85 {
			t.Fatalf("compaction=%v moved the compaction guard to %v, want the shipped 85", bad, got)
		}
	}
}

func TestCompactionValuesOfTheRightShapeAreKept(t *testing.T) {
	leanSandbox(t)
	for _, good := range []struct {
		own  object
		want leanPolicy
	}{
		{object{"compactAtPercent": " 75 "}, leanPolicy{on: true, compactAt: 75}},
		{object{"compactAtPercent": float64(0)}, leanPolicy{on: true}},
		{object{"compactAtPercent": false}, leanPolicy{on: true}},
		{object{"compactAtPercent": nil}, leanPolicy{on: true}},
		{object{"compactAtPercent": "0"}, leanPolicy{on: true}},
		{object{"lean": false}, leanPolicy{compactAt: 70}},
		{object{"keepTurns": "3", "maxToolResultChars": float64(800), "instructions": "keep the plan"}, leanPolicy{on: true, compactAt: 70}},
	} {
		mustWriteJSON(files.config, object{"compaction": good.own})
		cfg := loadConfig()
		if got := leanPolicyOf(cfg); got != good.want {
			t.Fatalf("compaction=%v gave %+v, want %+v", good.own, got, good.want)
		}
		if names := repairedCompaction(cfg); len(names) != 0 {
			t.Fatalf("compaction=%v was taken for a mistake: repaired %v", good.own, names)
		}
	}
}

func TestDoctorAndStatusNameARepairedCompactionSetting(t *testing.T) {
	leanSandbox(t)
	t.Setenv("NOCTIS_LANG", "en")
	mustWriteJSON(files.config, object{"compaction": object{"keepTurns": "many", "lean": "yes"}})
	cfg := loadConfig()
	doctor := strings.Join(doctorLines(cfg), "\n")
	if !strings.Contains(doctor, "compaction.lean, compaction.keepTurns") {
		t.Fatalf("the doctor did not name the repaired compaction keys:\n%s", doctor)
	}
	status := describeState(cfg, readState(), usageView{}, nowSec())
	if !strings.Contains(status, "compaction.lean, compaction.keepTurns") {
		t.Fatalf("status did not name the repaired compaction keys:\n%s", status)
	}
	mustWriteJSON(files.config, object{"compaction": object{"compactAtPercent": float64(75)}})
	cfg = loadConfig()
	doctor = strings.Join(doctorLines(cfg), "\n")
	if !strings.Contains(doctor, "lean compaction: on, compacts early at 75% of the context") {
		t.Fatalf("the doctor does not say how lean compaction is set:\n%s", doctor)
	}
	if status := describeState(cfg, readState(), usageView{}, nowSec()); !strings.Contains(status, "Compaction   : lean compaction on, compacts early at 75% of the context") {
		t.Fatalf("status does not say how lean compaction is set:\n%s", status)
	}
}

func TestTheModuleAndTheConfigShipTheSameLeanDefaults(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(repoRoot(), "hooks", "lean.js"))
	if err != nil {
		t.Fatal(err)
	}
	found := regexp.MustCompile(`SHIPPED = Object\.freeze\(\{ lean: (true|false), compactAtPercent: (\d+), keepTurns: (\d+), maxToolResultChars: (\d+), instructions: "([^"]*)" \}\)`).FindStringSubmatch(string(source))
	if found == nil {
		t.Fatal("hooks/lean.js no longer spells SHIPPED the way this test reads it")
	}
	module := object{"lean": found[1] == "true", "compactAtPercent": found[2], "keepTurns": found[3], "maxToolResultChars": found[4], "instructions": found[5]}
	shipped := getMap(shippedDefaults(t), "compaction")
	for _, key := range leanKeys {
		for name, values := range map[string]object{"hooks/lean.js": module, "builtinLean": builtinLean} {
			got, want := values[key], shipped[key]
			if number, ok := leanNumber(got); ok {
				got = number
			}
			if jsonText(got) != jsonText(want) {
				t.Errorf("%s ships compaction.%s=%v, config.default.json %v", name, key, got, want)
			}
		}
	}
}
