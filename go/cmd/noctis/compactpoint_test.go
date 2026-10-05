package main

import "testing"

// Claude Code 2.1.289 files a model's own autoCompactWindow under the model's canonical id, which its
// alias and its dated, [1m], Bedrock and Vertex spellings share, and names a session's model by the id of
// its provider: us.anthropic.claude-opus-5-5 on Bedrock, anthropic.claude-opus-5-5 on Mantle,
// claude-haiku-4-5@20251001 on Vertex.
func TestAModelsOwnWindowAppliesUnderEverySpellingClaudeCodeGivesTheModel(t *testing.T) {
	clearCompactionVariables(t)
	own := object{"autoCompactWindow": float64(313000), "modelSettings": object{"claude-opus-5-5": object{"autoCompactWindow": float64(140000)}}}
	for _, model := range []string{
		"claude-opus-5-5",
		"claude-opus-5-5[1m]",
		"opus",
		"claude-opus-5-5-20260101",
		"us.anthropic.claude-opus-5-5",
		"eu.anthropic.claude-opus-5-5[1m]",
		"global.anthropic.claude-opus-5-5",
		"anthropic.claude-opus-5-5",
		"us.anthropic.claude-opus-5-5-20260101-v1:0",
		"claude-opus-5-5-v1",
		"claude-opus-5-5@20260101",
		"arn:aws:bedrock:us-east-1:123456789012:inference-profile/us.anthropic.claude-opus-5-5",
	} {
		if got, _ := compactionPoint(own, model, 1e6); got != 107000 {
			t.Errorf("%s: compaction point %v, want 107000 from the model's own window", model, got)
		}
	}
	for _, model := range []string{"claude-sonnet-5-5", "us.anthropic.claude-sonnet-5-5", "claude-opus-5", "us.anthropic.claude-opus-5-v1:0"} {
		if got, _ := compactionPoint(own, model, 1e6); got != 280000 {
			t.Errorf("%s: compaction point %v, want 280000 from autoCompactWindow", model, got)
		}
	}

	spelled := object{"autoCompactWindow": float64(313000), "modelSettings": object{
		"claude-haiku-4-5@20251001":      object{"autoCompactWindow": float64(173000)},
		"us.anthropic.claude-sonnet-5-5": object{"autoCompactWindow": float64(633000)},
	}}
	for model, want := range map[string]float64{
		"claude-haiku-4-5-20251001":                   140000,
		"us.anthropic.claude-haiku-4-5-20251001-v1:0": 140000,
		"claude-sonnet-5-5[1m]":                       600000,
		"claude-opus-5-5":                             280000,
	} {
		if got, _ := compactionPoint(spelled, model, 1e6); got != want {
			t.Errorf("%s, with entries under a provider's spelling: compaction point %v, want %v", model, got, want)
		}
	}
	if entries := modelCompactions(spelled); len(entries) != 2 {
		t.Errorf("status and doctor name %d models with a window of their own, want 2: %v", len(entries), entries)
	}
}

// Claude Code keeps min(the model's reply limit, 20000) tokens of the window for the summary, and
// CLAUDE_CODE_MAX_OUTPUT_TOKENS sets that limit: 4096 keeps 4096.
func TestALowReplyLimitMovesThePointAsItMovesClaudeCodes(t *testing.T) {
	clearCompactionVariables(t)
	t.Setenv(maxOutputTokensVar, "")
	for _, point := range []struct {
		name     string
		settings object
		window   float64
		want     float64
	}{
		{"4096 with autoCompactWindow 313000", object{"autoCompactWindow": float64(313000), "env": object{maxOutputTokensVar: "4096"}}, 1e6, 295904},
		{"4096 in a 1M window", object{"env": object{maxOutputTokensVar: "4096"}}, 1e6, 982904},
		{"4096 and 60%", object{"env": object{maxOutputTokensVar: "4096", autoCompactPercentVar: "60"}}, 1e6, 597542},
		{"16,000 in a 200k window", object{"env": object{maxOutputTokensVar: "16,000"}}, 200000, 171000},
		{"a limit above 20000 keeps 20000", object{"autoCompactWindow": float64(313000), "env": object{maxOutputTokensVar: "64000"}}, 1e6, 280000},
		{"a limit that is no number keeps 20000", object{"autoCompactWindow": float64(313000), "env": object{maxOutputTokensVar: "big"}}, 1e6, 280000},
		{"a limit of 0 keeps 20000", object{"autoCompactWindow": float64(313000), "env": object{maxOutputTokensVar: "0"}}, 1e6, 280000},
	} {
		if got, _ := compactionPoint(point.settings, "claude-opus-5-5", point.window); got != point.want {
			t.Errorf("%s: compaction point %v, want %v", point.name, got, point.want)
		}
	}
	t.Setenv(maxOutputTokensVar, "8192")
	if got, _ := compactionPoint(object{"autoCompactWindow": float64(313000)}, "claude-opus-5-5", 1e6); got != 291808 {
		t.Errorf("CLAUDE_CODE_MAX_OUTPUT_TOKENS=8192 in Claude Code's environment: compaction point %v, want 291808", got)
	}
}

// Claude Code reads autoCompactWindow, top-level or a model's own, as a whole number from 100000 to
// 1000000 and sets any other value aside: a model's own entry it sets aside leaves the top-level window.
func TestAWindowClaudeCodeSetsAsideDoesNotCount(t *testing.T) {
	clearCompactionVariables(t)
	entry := func(window any) object {
		return object{"autoCompactWindow": float64(313000), "modelSettings": object{"claude-opus-5-5": object{"autoCompactWindow": window}}}
	}
	for _, point := range []struct {
		name     string
		settings object
		want     float64
	}{
		{"the model's own 50000", entry(float64(50000)), 280000},
		{"the model's own 2000000", entry(float64(2e6)), 280000},
		{"the model's own 140000.5", entry(140000.5), 280000},
		{"the model's own \"140000\"", entry("140000"), 280000},
		{"the model's own null", entry(nil), 280000},
		{"an own entry beside a set-aside one under the canonical id", object{"autoCompactWindow": float64(313000), "modelSettings": object{
			"claude-opus-5-5":              object{"autoCompactWindow": float64(50000)},
			"us.anthropic.claude-opus-5-5": object{"autoCompactWindow": float64(173000)},
		}}, 140000},
		{"autoCompactWindow 313000.5", object{"autoCompactWindow": 313000.5}, 967000},
	} {
		if got, _ := compactionPoint(point.settings, "claude-opus-5-5", 1e6); got != point.want {
			t.Errorf("%s: compaction point %v, want %v", point.name, got, point.want)
		}
	}
	if entries := modelCompactions(entry(float64(50000))); len(entries) != 0 {
		t.Errorf("status and doctor name a window Claude Code sets aside: %v", entries)
	}
}
