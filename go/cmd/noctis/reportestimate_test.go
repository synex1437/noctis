package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTheReportLabelsTheKeptOffGapAnAPIListPriceEstimate(t *testing.T) {
	home := t.TempDir()
	cliWrite(t, filepath.Join(home, ".claude", "noctis", "config.json"), []byte(`{"models":{"primary":"opus"}}`))
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	call := func(id, model string, sidechain bool, input, output float64) string {
		return string(marshalCompact(object{"type": "assistant", "timestamp": stamp, "isSidechain": sidechain,
			"message": object{"id": id, "role": "assistant", "model": model, "content": []any{}, "usage": object{"input_tokens": input, "output_tokens": output}}}))
	}
	cliWrite(t, filepath.Join(home, ".claude", "projects", "-work-app", "s1.jsonl"), []byte(call("m1", "claude-opus-4-5", false, 1200, 300)+"\n"+call("m2", "claude-haiku-4-5", true, 8000, 900)+"\n"))

	cases := []struct {
		name, lang string
		argv       []string
		want       string
	}{
		{"the text report", "en", []string{"report", "--days", "1"}, "\nAPI list-price estimate, not plan quota saved: those calls cost ≈ $0.05 less than on the primary model ($0.013 instead of $0.063)\n"},
		{"the Turkish text report", "tr", []string{"report", "--days", "1"}, "\nAPI liste fiyatlarıyla tahmin, plan kotası tasarrufu değil: o çağrılar birincil modelde olacağından ≈ $0.05 daha ucuza geldi ($0.013 tuttu, birincilde $0.063 olurdu)\n"},
		{"the HTML report", "en", []string{"report", "--days", "1", "--html"}, "<p>API list-price estimate, not plan quota saved: those calls cost ≈ $0.05 less than on the primary model ($0.013 instead of $0.063)</p>"},
	}
	for _, tc := range cases {
		run := startNoctisCLIAt(t, home, "", map[string]string{"NOCTIS_LANG": tc.lang}, tc.argv...)()
		if run.code != 0 || !strings.Contains(run.stdout, tc.want) {
			t.Errorf("%s must call what subagents kept off the primary model an API list-price estimate, not a saving; want %q in:\n%s", tc.name, tc.want, run)
		}
	}

	run := startNoctisCLIAt(t, home, "", nil, "report", "--days", "1", "--json")()
	var report object
	if err := json.NewDecoder(strings.NewReader(run.stdout)).Decode(&report); err != nil {
		t.Fatalf("report --json: %v\n%s", err, run)
	}
	kept := getMap(report, "keptOffPrimary")
	if kept["savedVsPrimary"] != 0.05 || kept["costOnPrimary"] != 0.0625 || kept["cost"] != 0.0125 || kept["basis"] != "API list-price estimate, not plan quota saved" {
		t.Errorf("report --json keptOffPrimary is %s; want savedVsPrimary 0.05, costOnPrimary 0.0625 and cost 0.0125 under their names, and a basis that calls them an API list-price estimate, not plan quota saved", marshalCompact(kept))
	}
}

func TestTheReportLeavesOutTheEstimateWhenTheSubagentModelCostsMore(t *testing.T) {
	home := t.TempDir()
	cliWrite(t, filepath.Join(home, ".claude", "noctis", "config.json"), []byte(`{"models":{"primary":"haiku"}}`))
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	call := func(id, model string, sidechain bool, input, output float64) string {
		return string(marshalCompact(object{"type": "assistant", "timestamp": stamp, "isSidechain": sidechain,
			"message": object{"id": id, "role": "assistant", "model": model, "content": []any{}, "usage": object{"input_tokens": input, "output_tokens": output}}}))
	}
	cliWrite(t, filepath.Join(home, ".claude", "projects", "-work-app", "s1.jsonl"), []byte(call("m1", "claude-haiku-4-5", false, 1200, 300)+"\n"+call("m2", "claude-opus-4-5", true, 4000, 900)+"\n"))

	for _, argv := range [][]string{{"report", "--days", "1"}, {"report", "--days", "1", "--html"}} {
		run := startNoctisCLIAt(t, home, "", nil, argv...)()
		if run.code != 0 || !strings.Contains(run.stdout, "Kept off haiku: 4.9K tokens in 1 subagent calls on other models") {
			t.Fatalf("%v must still say what the subagents ran on other models:\n%s", argv, run)
		}
		if strings.Contains(run.stdout, "$-") || strings.Contains(run.stdout, "API list-price estimate") {
			t.Errorf("%v must leave the estimate out when the subagents' model costs more than the primary one, not print a negative saving:\n%s", argv, run)
		}
	}

	run := startNoctisCLIAt(t, home, "", nil, "report", "--days", "1", "--json")()
	var report object
	if err := json.NewDecoder(strings.NewReader(run.stdout)).Decode(&report); err != nil {
		t.Fatalf("report --json: %v\n%s", err, run)
	}
	if kept := getMap(report, "keptOffPrimary"); numberOr(kept, "savedVsPrimary", 0) >= 0 || kept["basis"] == nil {
		t.Errorf("report --json must keep the gap as it is, below zero, with its basis: %s", marshalCompact(kept))
	}
}
