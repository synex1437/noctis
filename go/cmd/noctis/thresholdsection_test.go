package main

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestAThresholdsValueThatIsNotAnObjectKeepsTheShippedThresholds(t *testing.T) {
	dir := sandboxFiles(t)
	files.pluginRoot = dir
	mustWriteJSON(filepath.Join(dir, "config.default.json"), shippedDefaults(t))
	for _, bad := range []any{float64(90), "90", []any{float64(92)}, true, false, nil} {
		mustWriteJSON(files.config, object{"thresholds": bad})
		cfg := loadConfig()
		if got := thresholdOf(cfg, "session5h"); got != 92 {
			t.Fatalf("thresholds=%v left the five-hour pause point at %v, want the shipped 92", bad, got)
		}
		if names := repairedThresholds(cfg); !slices.Contains(names, "thresholds") {
			t.Fatalf("thresholds=%v was set aside without telling anyone: repaired %v", bad, names)
		}
		usage := usageView{hasAny: true, fiveHour: &window{used: 96, resetsAt: float64(nowSec() + 3600)}}
		if result := evaluate(cfg, usage, "claude-opus-5", 0, false); result.wait == nil {
			t.Fatalf("thresholds=%v: 96%% of the five-hour window did not stop the run", bad)
		}
	}
}
