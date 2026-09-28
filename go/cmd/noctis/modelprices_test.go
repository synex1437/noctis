package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestModelIDsThatPutTheVersionFirstArePriced(t *testing.T) {
	cases := map[string]modelPrice{
		"claude-3-5-haiku-20241022":  {0.8, 4, 1, 0.08},
		"claude-3-5-haiku-latest":    {0.8, 4, 1, 0.08},
		"claude-3-7-sonnet-20250219": {3, 15, 3.75, 0.3},
		"claude-haiku-4-5-20251001":  {1, 5, 1.25, 0.1},
		"claude-sonnet-4-5":          {3, 15, 3.75, 0.3},
		"haiku":                      {1, 5, 1.25, 0.1},
		"sonnet":                     {2, 10, 2.5, 0.2},
	}
	for model, want := range cases {
		if got, ok := priceFor(object{}, model); !ok || got != want {
			t.Errorf("%s priced as %+v (found %v), want %+v", model, got, ok, want)
		}
	}
}

func TestAPartialPriceOverrideKeepsTheListPricesItLeavesOut(t *testing.T) {
	cfg := object{"report": object{"pricing": object{
		"opus-5-5":  object{"input": float64(4), "output": float64(20)},
		"haiku-4-5": object{"input": float64(2), "cacheRead": float64(0)},
		"x8-house":  object{"input": float64(1), "output": float64(2), "cacheWrite": float64(3), "cacheRead": float64(4)},
	}}}
	for model, want := range map[string]modelPrice{
		"claude-opus-5-5":  {4, 20, 5, 0.2},
		"claude-haiku-4-5": {2, 5, 1.25, 0},
		"x8-house-model":   {1, 2, 3, 4},
	} {
		if got, ok := priceFor(cfg, model); !ok || got != want {
			t.Errorf("%s priced as %+v (found %v) with a partial override, want %+v", model, got, ok, want)
		}
	}
}

func TestOneHourCacheWritesCostTwiceTheInputPrice(t *testing.T) {
	sandboxFiles(t)
	project := filepath.Join(files.configDir, "projects", "-work-x8")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	usage := object{"input_tokens": float64(0), "output_tokens": float64(0), "cache_creation_input_tokens": float64(1e6),
		"cache_creation": object{"ephemeral_5m_input_tokens": float64(4e5), "ephemeral_1h_input_tokens": float64(6e5)}}
	line := marshalCompact(object{"type": "assistant", "timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"message": object{"id": "msg_x8", "model": "claude-opus-5-5", "role": "assistant", "content": []any{}, "usage": usage}})
	if err := os.WriteFile(filepath.Join(project, "s1.jsonl"), append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	data := collectReport(object{"models": object{"primary": "opus"}}, 1)
	// 400K five-minute writes at $5 and 600K one-hour writes at 2 × $4.
	if got := data.totalCost(); math.Abs(got-6.8) > 1e-9 {
		t.Errorf("a million cache-write tokens, 600K of them for one hour, cost $%v on Opus 5.5; want $6.8", got)
	}
	if bucket := data.byModel["claude-opus-5-5"]; bucket == nil || bucket.cacheWrite != 1e6 {
		t.Errorf("the report must still count all cache-write tokens as written: %+v", bucket)
	}
}
