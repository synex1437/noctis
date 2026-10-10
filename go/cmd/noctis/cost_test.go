package main

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type fakeCall struct {
	id, model                            string
	at                                   time.Time
	input, output, cacheRead, cacheWrite float64
	cacheWrite1h                         float64
	tools                                []string
	sidechain                            bool
	agentID                              string
}

func (call fakeCall) line(session string) string {
	content := []any{}
	for _, tool := range call.tools {
		content = append(content, object{"type": "tool_use", "id": tool, "name": "Read", "input": object{}})
	}
	usage := object{"input_tokens": call.input, "output_tokens": call.output, "cache_read_input_tokens": call.cacheRead, "cache_creation_input_tokens": call.cacheWrite}
	if call.cacheWrite1h > 0 {
		usage["cache_creation"] = object{"ephemeral_1h_input_tokens": call.cacheWrite1h, "ephemeral_5m_input_tokens": call.cacheWrite - call.cacheWrite1h}
	}
	raw := object{"type": "assistant", "timestamp": call.at.UTC().Format(time.RFC3339Nano), "sessionId": session, "requestId": "req_" + call.id,
		"message": object{"id": call.id, "model": orDefault(call.model, "claude-opus-5-5"), "role": "assistant", "content": content, "usage": usage}}
	if call.sidechain {
		raw["isSidechain"] = true
	}
	if call.agentID != "" {
		raw["agentId"] = call.agentID
	}
	return string(marshalCompact(raw))
}

func compactLine(session string, at time.Time) string {
	return string(marshalCompact(object{"type": "system", "subtype": "compact_boundary", "timestamp": at.UTC().Format(time.RFC3339Nano), "sessionId": session, "compactMetadata": object{"trigger": "auto", "preTokens": float64(280000)}}))
}

func writeTranscriptFile(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writeLines(t, path, lines...)
}

func writeAgentMeta(t *testing.T, transcript, kind, description string) {
	t.Helper()
	meta := marshalCompact(object{"agentType": kind, "description": description})
	if err := os.WriteFile(strings.TrimSuffix(transcript, ".jsonl")+".meta.json", meta, 0o600); err != nil {
		t.Fatal(err)
	}
}

func costFixture(t *testing.T) (project string, now time.Time) {
	t.Helper()
	sandboxFiles(t)
	project = filepath.Join(files.configDir, "projects", "-work-app")
	now = time.Now().Add(-time.Hour)
	main := []string{
		fakeCall{id: "msg_m1", at: now, input: 10, cacheWrite: 30000, output: 400}.line("s1"),
		fakeCall{id: "msg_m2", at: now.Add(time.Minute), input: 5, cacheRead: 30000, cacheWrite: 2000, output: 300, tools: []string{"toolu_main"}}.line("s1"),
	}
	writeTranscriptFile(t, filepath.Join(project, "s1.jsonl"), main...)
	heavy := filepath.Join(project, "s1", "subagents", "agent-a1heavy.jsonl")
	writeTranscriptFile(t, heavy,
		fakeCall{id: "msg_a1", at: now.Add(2 * time.Minute), input: 3, cacheWrite: 40000, output: 50, tools: []string{"toolu_1"}}.line("s1"),
		fakeCall{id: "msg_a2", at: now.Add(3 * time.Minute), input: 3, cacheRead: 40000, cacheWrite: 40000, output: 60, tools: []string{"toolu_2", "toolu_3"}}.line("s1"),
		compactLine("s1", now.Add(4*time.Minute)),
		fakeCall{id: "msg_a3", at: now.Add(5 * time.Minute), input: 3, cacheRead: 80000, cacheWrite: 40000, output: 70, tools: []string{"toolu_4"}}.line("s1"),
	)
	writeAgentMeta(t, heavy, "general-purpose", "Hunt the parser bug")
	light := filepath.Join(project, "s1", "subagents", "agent-a2light.jsonl")
	writeTranscriptFile(t, light,
		fakeCall{id: "msg_b1", model: "claude-haiku-5-5", at: now.Add(6 * time.Minute), input: 2, cacheWrite: 20000, output: 20, tools: []string{"toolu_5"}}.line("s1"),
	)
	writeAgentMeta(t, light, "Explore", "Find the config loader")
	return project, now
}

func TestCostKeepsTheLargestUsageOfAResponseWrittenOverSeveralLines(t *testing.T) {
	sandboxFiles(t)
	project := filepath.Join(files.configDir, "projects", "-work-app")
	at := time.Now().Add(-time.Minute)
	first := fakeCall{id: "msg_1", at: at, input: 10, cacheRead: 5000, output: 1, tools: []string{"toolu_a"}}
	last := first
	last.output, last.tools = 900, []string{"toolu_b"}
	writeTranscriptFile(t, filepath.Join(project, "s1.jsonl"), first.line("s1"), last.line("s1"))

	data := collectCost(object{}, 1, "")

	tokens := data.all.tokens()
	if tokens.calls != 1 || tokens.output != 900 || tokens.input != 10 || tokens.cacheRead != 5000 {
		t.Fatalf("one response written as two lines, the first with a partial output count, was counted as %+v; want one call with 900 output, 10 input and 5000 cache-read tokens", tokens)
	}
	if entry := data.sessions["s1"]; entry == nil || entry.main.tokens().calls != 1 {
		t.Fatalf("the session's own ledger holds %+v; want the one call", entry)
	}
}

func TestCostSplitsSubagentsFromTheMainSessionAndRanksThemByWhatTheyCost(t *testing.T) {
	costFixture(t)

	data := collectCost(object{}, 1, "")

	if calls := data.main.tokens().calls; calls != 2 {
		t.Fatalf("the main session made 2 calls; counted %d", calls)
	}
	if calls := data.sub.tokens().calls; calls != 4 {
		t.Fatalf("the two subagents made 4 calls between them; counted %d", calls)
	}
	agents := data.sortedAgents()
	if len(agents) != 2 || agents[0].key.agent != "a1heavy" || agents[1].key.agent != "a2light" {
		t.Fatalf("the agents ranked by cost are %v; want a1heavy before a2light", agents)
	}
	heavy := agents[0]
	if heavy.tools != 4 || heavy.compactions != 1 || heavy.startContext != 40003 || heavy.peakContext != 120003 {
		t.Fatalf("the heavy agent shows %d tool calls, %d compactions, start %v and peak %v context tokens; want 4, 1, 40003 and 120003", heavy.tools, heavy.compactions, heavy.startContext, heavy.peakContext)
	}
	if heavy.meta.kind != "general-purpose" || heavy.meta.description != "Hunt the parser bug" {
		t.Fatalf("the heavy agent's type and description read %+v; want them from its .meta.json", heavy.meta)
	}
	if kind := data.kinds["Explore"]; kind == nil || kind.agents != 1 || data.kinds["general-purpose"] == nil {
		t.Fatalf("the agents by type are %v; want one Explore and one general-purpose", sortedKeys(data.kinds))
	}
	entry := data.sessions["s1"]
	if entry == nil || entry.agents != 2 || entry.sub.tokens().calls != 4 || entry.main.tokens().calls != 2 {
		t.Fatalf("session s1 shows %+v; want 2 agents, 2 main and 4 subagent calls", entry)
	}
	if math.Abs(data.totalCost-(data.main.parts(data.prices).total()+data.sub.parts(data.prices).total())) > 1e-12 {
		t.Fatalf("the total %v is not the main share plus the subagents' share", data.totalCost)
	}
}

func TestCostCountsOnlyTheDaysAskedFor(t *testing.T) {
	sandboxFiles(t)
	project := filepath.Join(files.configDir, "projects", "-work-app")
	old := fakeCall{id: "msg_old", at: time.Now().AddDate(0, 0, -10), input: 100, output: 100}
	recent := fakeCall{id: "msg_new", at: time.Now().Add(-time.Hour), input: 7, output: 9}
	path := filepath.Join(project, "s1.jsonl")
	writeTranscriptFile(t, path, old.line("s1"), recent.line("s1"))

	data := collectCost(object{}, 7, "")

	if tokens := data.all.tokens(); tokens.calls != 1 || tokens.input != 7 {
		t.Fatalf("over 7 days the calls were counted as %+v; want only the call of an hour ago", tokens)
	}
}

func TestCostShowsOneSessionWhenAskedForItsIDPrefix(t *testing.T) {
	sandboxFiles(t)
	project := filepath.Join(files.configDir, "projects", "-work-app")
	at := time.Now().Add(-time.Hour)
	writeTranscriptFile(t, filepath.Join(project, "aaaa1111.jsonl"), fakeCall{id: "msg_a", at: at, input: 1, output: 1}.line("aaaa1111"))
	writeTranscriptFile(t, filepath.Join(project, "bbbb2222.jsonl"), fakeCall{id: "msg_b", at: at, input: 2, output: 2}.line("bbbb2222"))

	data := collectCost(object{}, 1, "bbbb")

	if len(data.sessions) != 1 || data.sessions["bbbb2222"] == nil {
		t.Fatalf("--session bbbb kept sessions %v; want only bbbb2222", sortedKeys(data.sessions))
	}
}

func TestCostPricesAnHourLongCacheWriteAtTwiceTheInputPrice(t *testing.T) {
	price := modelPrice{input: 4, output: 20, cacheWrite: 5, cacheRead: 0.2}
	bucket := tokenBucket{input: 1000, output: 100, cacheRead: 10000, cacheWrite: 1000, cacheWrite1h: 400}

	parts := bucket.partsAt(price)

	want := costParts{input: 1000 * 4 / 1e6, cacheWrite: (600*5 + 400*8) / 1e6, cacheRead: 10000 * 0.2 / 1e6, output: 100 * 20 / 1e6}
	if math.Abs(parts.input-want.input) > 1e-12 || math.Abs(parts.cacheWrite-want.cacheWrite) > 1e-12 || math.Abs(parts.cacheRead-want.cacheRead) > 1e-12 || math.Abs(parts.output-want.output) > 1e-12 {
		t.Fatalf("the parts read %+v; want %+v", parts, want)
	}
	if math.Abs(bucket.cost(reportPrice{modelPrice: price})-want.total()) > 1e-12 {
		t.Fatalf("the cost %v is not the sum of its parts %v", bucket.cost(reportPrice{modelPrice: price}), want.total())
	}
}

func TestCostCommandPrintsJSONWithEachPartAndTheTopSubagents(t *testing.T) {
	costFixture(t)
	defer func(previous parsedArgs) { args = previous }(args)
	args = parseArgs([]string{"cost", "--days", "1", "--top", "1", "--json"})

	output := capturedStdout(t, runCost)

	report := object{}
	if err := jsonUnmarshalObject([]byte(output), &report); err != nil {
		t.Fatalf("noctis cost --json printed no JSON object: %v\n%s", err, output)
	}
	total := getMap(getMap(report, "total"), "cost")
	sum := numberOr(total, "input", 0) + numberOr(total, "cacheWrite", 0) + numberOr(total, "cacheRead", 0) + numberOr(total, "output", 0)
	if numberOr(total, "total", 0) <= 0 || math.Abs(sum-numberOr(total, "total", 0)) > 1e-9 {
		t.Fatalf("the total cost %v is not the sum %v of its four parts:\n%s", total["total"], sum, output)
	}
	top := getList(report, "topSubagents")
	if len(top) != 1 || getString(toObject(top[0]), "agent") != "a1heavy" || numberOr(toObject(top[0]), "toolCalls", 0) != 4 || numberOr(toObject(top[0]), "peakContext", 0) != 120003 {
		t.Fatalf("--top 1 listed %v; want the heavy agent with 4 tool calls and a 120003-token peak", top)
	}
	if numberOr(report, "subagentCount", 0) != 2 || numberOr(getMap(report, "subagents"), "calls", 0) != 4 || numberOr(getMap(report, "main"), "calls", 0) != 2 {
		t.Fatalf("the report counts %v subagents, %v subagent calls and %v main calls; want 2, 4 and 2", report["subagentCount"], getMap(report, "subagents")["calls"], getMap(report, "main")["calls"])
	}
}

func TestCostCommandPrintsATableWithTheTopSubagents(t *testing.T) {
	costFixture(t)
	t.Setenv("NOCTIS_LANG", "en")
	defer func(previous parsedArgs) { args = previous }(args)
	args = parseArgs([]string{"cost", "--days", "1"})

	output := capturedStdout(t, runCost)

	for _, want := range []string{"a1heavy", "Hunt the parser bug", "general-purpose", "a2light", "Explore", "claude-opus-5-5", "claude-haiku-5-5"} {
		if !strings.Contains(output, want) {
			t.Fatalf("noctis cost printed no %q:\n%s", want, output)
		}
	}
}

func TestCostSaysSoWhenThereIsNothingToCount(t *testing.T) {
	sandboxFiles(t)
	t.Setenv("NOCTIS_LANG", "en")
	defer func(previous parsedArgs) { args = previous }(args)
	args = parseArgs([]string{"cost"})

	output := capturedStdout(t, runCost)

	if strings.TrimSpace(output) != strings.TrimSpace(T("cost.none", formatNumber(7), files.configDir)) {
		t.Fatalf("with no transcripts noctis cost printed:\n%s", output)
	}
}

func TestCostCountsASidechainLineOfAMainTranscriptAsItsAgent(t *testing.T) {
	sandboxFiles(t)
	project := filepath.Join(files.configDir, "projects", "-work-app")
	at := time.Now().Add(-time.Hour)
	writeTranscriptFile(t, filepath.Join(project, "s1.jsonl"),
		fakeCall{id: "msg_m", at: at, input: 1, output: 1}.line("s1"),
		fakeCall{id: "msg_s", at: at, input: 2, output: 2, sidechain: true, agentID: "old"}.line("s1"),
	)

	data := collectCost(object{}, 1, "")

	if data.agents[agentKey{session: "s1", agent: "old"}] == nil || data.main.tokens().calls != 1 || data.sub.tokens().calls != 1 {
		t.Fatalf("a sidechain line in the main transcript was counted as agents %v, %d main and %d subagent calls; want its agent, 1 and 1", data.agents, data.main.tokens().calls, data.sub.tokens().calls)
	}
}

func TestCostSkipsAResponseCopiedIntoAnotherTranscript(t *testing.T) {
	sandboxFiles(t)
	project := filepath.Join(files.configDir, "projects", "-work-app")
	call := fakeCall{id: "msg_1", at: time.Now().Add(-time.Hour), input: 100, output: 50}
	writeTranscriptFile(t, filepath.Join(project, "s1.jsonl"), call.line("s1"))
	writeTranscriptFile(t, filepath.Join(project, "s2.jsonl"), call.line("s2"))

	data := collectCost(object{}, 1, "")

	if tokens := data.all.tokens(); tokens.calls != 1 || tokens.input != 100 {
		t.Fatalf("a response found in two transcripts was counted as %+v; want once", tokens)
	}
}

func TestCostAndReportReadAProjectsFolderThatIsALink(t *testing.T) {
	sandboxFiles(t)
	elsewhere := filepath.Join(t.TempDir(), "transcripts")
	writeTranscriptFile(t, filepath.Join(elsewhere, "-work-app", "s1.jsonl"), fakeCall{id: "msg_1", at: time.Now().Add(-time.Hour), input: 100, output: 50}.line("s1"))
	if err := os.Symlink(elsewhere, filepath.Join(files.configDir, "projects")); err != nil {
		t.Skipf("no symbolic links here: %v", err)
	}

	data := collectCost(object{}, 1, "")
	report := collectReport(object{}, 1)

	if tokens := data.all.tokens(); tokens.calls != 1 || tokens.input != 100 || data.sessions["s1"] == nil || data.sessions["s1"].project != "-work-app" {
		t.Fatalf("a projects folder that is a link gave %+v and sessions %v; want the call of s1 in -work-app", tokens, data.sessions)
	}
	if report.transcripts != 1 || report.byModel["claude-opus-5-5"] == nil || report.byModel["claude-opus-5-5"].input != 100 {
		t.Fatalf("the report read %d transcripts and %v through a projects folder that is a link; want 1 and its call", report.transcripts, report.byModel)
	}
}

func TestCostTableLinesUpItsColumnsInEveryLanguage(t *testing.T) {
	costFixture(t)
	data := collectCost(object{}, 1, "")
	previous := locale
	t.Cleanup(func() { locale = previous })

	for code := range catalogTable() {
		locale = code
		lines := strings.Split(costText(data, 5), "\n")
		header := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, T("cost.cacheWrite")) })
		if header < 0 || header+3 >= len(lines) {
			t.Fatalf("%s: no totals table under the header:\n%s", code, strings.Join(lines, "\n"))
		}
		width := cellWidth(lines[header])
		for _, row := range lines[header+1 : header+3] {
			if cellWidth(row) != width {
				t.Fatalf("%s: a totals row is %d cells wide, its header %d:\n%s\n%s", code, cellWidth(row), width, lines[header], row)
			}
		}
		for _, key := range []string{"cost.input", "cost.cacheWrite", "cost.cacheRead", "cost.output", "cost.total"} {
			if !strings.Contains(lines[header], " "+T(key)) {
				t.Fatalf("%s: the header lost %q:\n%s", code, T(key), lines[header])
			}
		}
	}
}

func TestCostClipsAndPadsWideTextByTheCellsItTakes(t *testing.T) {
	name := "日本語のとても長いプロジェクトの名前"

	clipped := clipText(name, 22)

	if cellWidth(clipped) > 22 || !strings.HasSuffix(clipped, "…") || !strings.HasPrefix(name, strings.TrimSuffix(clipped, "…")) {
		t.Fatalf("clipped %q to %q, %d cells wide; want at most 22 cells ending in …", name, clipped, cellWidth(clipped))
	}
	if padded := padCells(clipped, 22); cellWidth(padded) != 22 {
		t.Fatalf("padded %q to %d cells; want 22", clipped, cellWidth(padded))
	}
	if clipText("short name", 22) != "short name" || cellWidth("Kontext") != 7 || cellWidth("トークン") != 8 {
		t.Fatal("text that fits is clipped, or a cell count is wrong")
	}
}
