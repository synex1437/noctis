package main

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type costUsage struct {
	input, output, cacheRead, cacheWrite, cacheWrite1h float64
}

func costUsageOf(usage object) costUsage {
	written := numberOr(usage, "cache_creation_input_tokens", 0)
	return costUsage{
		input:        numberOr(usage, "input_tokens", 0),
		output:       numberOr(usage, "output_tokens", 0),
		cacheRead:    numberOr(usage, "cache_read_input_tokens", 0),
		cacheWrite:   written,
		cacheWrite1h: math.Min(numberOr(getMap(usage, "cache_creation"), "ephemeral_1h_input_tokens", 0), written),
	}
}

func (usage costUsage) context() float64 {
	return usage.input + usage.cacheRead + usage.cacheWrite
}

func (usage *costUsage) widen(other costUsage) {
	usage.input = math.Max(usage.input, other.input)
	usage.output = math.Max(usage.output, other.output)
	usage.cacheRead = math.Max(usage.cacheRead, other.cacheRead)
	usage.cacheWrite = math.Max(usage.cacheWrite, other.cacheWrite)
	usage.cacheWrite1h = math.Max(usage.cacheWrite1h, other.cacheWrite1h)
}

type agentKey struct {
	session, agent string
}

type agentMeta struct {
	kind, description string
}

type transcriptCall struct {
	costUsage
	model     string
	at        time.Time
	timed     bool
	key       agentKey
	sidechain bool
}

func (call *transcriptCall) day() string {
	if !call.timed {
		return "unknown"
	}
	return call.at.Local().Format("2006-01-02")
}

type transcriptScan struct {
	files       int
	calls       []*transcriptCall
	tools       map[agentKey]int
	compactions map[agentKey]int
	meta        map[agentKey]agentMeta
	projects    map[string]string
	byID        map[string]*transcriptCall
	toolIDs     map[string]bool
	since       time.Time
}

var (
	usageMarker   = []byte(`"usage"`)
	compactMarker = []byte(`"compact_boundary"`)
)

func newTranscriptScan(since time.Time) *transcriptScan {
	return &transcriptScan{tools: map[agentKey]int{}, compactions: map[agentKey]int{}, meta: map[agentKey]agentMeta{}, projects: map[string]string{}, byID: map[string]*transcriptCall{}, toolIDs: map[string]bool{}, since: since}
}

func scanTranscripts(root string, since time.Time) *transcriptScan {
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	scan := newTranscriptScan(since)
	transcripts := walkTranscripts(root, since)
	scan.files = len(transcripts)
	for _, file := range transcripts {
		scan.read(root, file)
	}
	kept := scan.calls[:0]
	for _, call := range scan.calls {
		if call.context()+call.output > 0 {
			kept = append(kept, call)
		}
	}
	scan.calls = kept
	return scan
}

func (scan *transcriptScan) read(root, file string) {
	transcript, err := os.Open(file)
	if err != nil {
		return
	}
	defer transcript.Close()
	owner, project := transcriptOwner(root, file)
	if owner.agent != "" {
		scan.meta[owner] = readAgentMeta(file)
	}
	lines := bufio.NewScanner(transcript)
	lines.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for lines.Scan() {
		line := lines.Bytes()
		hasUsage := bytes.Contains(line, usageMarker)
		if !hasUsage && !bytes.Contains(line, compactMarker) {
			continue
		}
		var raw object
		if jsonUnmarshalObject(line, &raw) != nil || raw == nil {
			continue
		}
		key := lineOwner(raw, owner)
		if _, known := scan.projects[key.session]; !known && project != "" {
			scan.projects[key.session] = project
		}
		switch getString(raw, "type") {
		case "system":
			if getString(raw, "subtype") == "compact_boundary" && scan.inWindow(raw) {
				scan.compactions[key]++
			}
		case "assistant":
			if hasUsage {
				scan.add(raw, key)
			}
		}
	}
}

func (scan *transcriptScan) inWindow(raw object) bool {
	at, err := time.Parse(time.RFC3339Nano, getString(raw, "timestamp"))
	return err != nil || !at.Before(scan.since)
}

func (scan *transcriptScan) add(raw object, key agentKey) {
	message := getMap(raw, "message")
	usage := getMap(message, "usage")
	if usage == nil {
		return
	}
	at, err := time.Parse(time.RFC3339Nano, getString(raw, "timestamp"))
	if err == nil && at.Before(scan.since) {
		return
	}
	for _, block := range getList(message, "content") {
		entry := toObject(block)
		if getString(entry, "type") != "tool_use" {
			continue
		}
		if id := getString(entry, "id"); id != "" {
			if scan.toolIDs[id] {
				continue
			}
			scan.toolIDs[id] = true
		}
		scan.tools[key]++
	}
	counts := costUsageOf(usage)
	id := orDefault(getString(message, "id"), getString(raw, "requestId"))
	if earlier := scan.byID[id]; id != "" && earlier != nil {
		earlier.widen(counts)
		return
	}
	call := &transcriptCall{costUsage: counts, model: orDefault(getString(message, "model"), "unknown"), at: at, timed: err == nil, key: key, sidechain: getBool(raw, "isSidechain", false)}
	if id != "" {
		scan.byID[id] = call
	}
	scan.calls = append(scan.calls, call)
}

func transcriptOwner(root, file string) (agentKey, string) {
	relative, err := filepath.Rel(root, file)
	if err != nil {
		relative = file
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	name := strings.TrimSuffix(parts[len(parts)-1], ".jsonl")
	project := ""
	if len(parts) > 1 {
		project = parts[0]
	}
	if strings.HasPrefix(name, "agent-") {
		for i := len(parts) - 2; i >= 1; i-- {
			if parts[i] == "subagents" {
				return agentKey{session: parts[i-1], agent: strings.TrimPrefix(name, "agent-")}, project
			}
		}
	}
	return agentKey{session: name}, project
}

func lineOwner(raw object, owner agentKey) agentKey {
	key := owner
	if session := getString(raw, "sessionId"); session != "" {
		key.session = session
	}
	if key.agent == "" && getBool(raw, "isSidechain", false) {
		key.agent = orDefault(getString(raw, "agentId"), "sidechain")
	}
	return key
}

func readAgentMeta(transcript string) agentMeta {
	content, err := os.ReadFile(strings.TrimSuffix(transcript, ".jsonl") + ".meta.json")
	if err != nil {
		return agentMeta{}
	}
	var meta object
	if jsonUnmarshalObject(content, &meta) != nil {
		return agentMeta{}
	}
	return agentMeta{kind: getString(meta, "agentType"), description: getString(meta, "description")}
}

func bucketFor(buckets map[string]*tokenBucket, model string) *tokenBucket {
	bucket := buckets[model]
	if bucket == nil {
		bucket = &tokenBucket{}
		buckets[model] = bucket
	}
	return bucket
}

type costLedger map[string]*tokenBucket

func (ledger costLedger) add(model string, usage costUsage) {
	bucketFor(ledger, model).add(usage)
}

func (ledger costLedger) tokens() tokenBucket {
	sum := tokenBucket{}
	for _, bucket := range ledger {
		sum.input += bucket.input
		sum.output += bucket.output
		sum.cacheRead += bucket.cacheRead
		sum.cacheWrite += bucket.cacheWrite
		sum.cacheWrite1h += bucket.cacheWrite1h
		sum.calls += bucket.calls
	}
	return sum
}

func (ledger costLedger) parts(prices map[string]reportPrice) costParts {
	sum := costParts{}
	for model, bucket := range ledger {
		if price, ok := prices[model]; ok {
			sum = sum.plus(bucket.parts(price))
		}
	}
	return sum
}

func ledgerAt(ledgers map[string]costLedger, key string) costLedger {
	ledger := ledgers[key]
	if ledger == nil {
		ledger = costLedger{}
		ledgers[key] = ledger
	}
	return ledger
}

type costAgent struct {
	key          agentKey
	meta         agentMeta
	spend        costLedger
	tools        int
	compactions  int
	first        time.Time
	startContext float64
	peakContext  float64
	cost         float64
}

type costSession struct {
	id, project string
	main, sub   costLedger
	agents      int
	compactions int
	peakContext float64
	first, last time.Time
	cost        float64
}

type costKind struct {
	agents int
	spend  costLedger
	cost   float64
}

type costData struct {
	days      float64
	since     time.Time
	files     int
	prices    map[string]reportPrice
	unpriced  []string
	all       costLedger
	main      costLedger
	sub       costLedger
	byDay     map[string]costLedger
	subByDay  map[string]costLedger
	sessions  map[string]*costSession
	agents    map[agentKey]*costAgent
	kinds     map[string]*costKind
	totalCost float64
}

func collectCost(cfg object, days float64, session string) costData {
	since := time.Now().Add(-time.Duration(days*24) * time.Hour)
	scan := scanTranscripts(filepath.Join(files.configDir, "projects"), since)
	data := costData{days: days, since: since, files: scan.files, prices: map[string]reportPrice{}, all: costLedger{}, main: costLedger{}, sub: costLedger{},
		byDay: map[string]costLedger{}, subByDay: map[string]costLedger{}, sessions: map[string]*costSession{}, agents: map[agentKey]*costAgent{}, kinds: map[string]*costKind{}}
	for _, call := range scan.calls {
		if session != "" && !strings.HasPrefix(call.key.session, session) {
			continue
		}
		data.add(call, scan)
	}
	for model := range data.all {
		if price, ok := priceFor(cfg, model); ok {
			data.prices[model] = price
		} else {
			data.unpriced = append(data.unpriced, model)
		}
	}
	sort.Strings(data.unpriced)
	data.totalCost = data.all.parts(data.prices).total()
	for key, agent := range data.agents {
		agent.tools, agent.compactions = scan.tools[key], scan.compactions[key]
		agent.cost = agent.spend.parts(data.prices).total()
		kind := data.kinds[agent.kindName()]
		if kind == nil {
			kind = &costKind{spend: costLedger{}}
			data.kinds[agent.kindName()] = kind
		}
		kind.agents++
		for model, bucket := range agent.spend {
			mergeBucket(bucketFor(kind.spend, model), bucket)
		}
		data.sessions[key.session].agents++
	}
	for _, kind := range data.kinds {
		kind.cost = kind.spend.parts(data.prices).total()
	}
	for id, entry := range data.sessions {
		entry.compactions = scan.compactions[agentKey{session: id}]
		entry.cost = entry.main.parts(data.prices).total() + entry.sub.parts(data.prices).total()
	}
	return data
}

func (data *costData) add(call *transcriptCall, scan *transcriptScan) {
	usage, day := call.costUsage, call.day()
	data.all.add(call.model, usage)
	ledgerAt(data.byDay, day).add(call.model, usage)
	entry := data.sessions[call.key.session]
	if entry == nil {
		entry = &costSession{id: call.key.session, project: scan.projects[call.key.session], main: costLedger{}, sub: costLedger{}}
		data.sessions[call.key.session] = entry
	}
	if call.timed {
		if entry.first.IsZero() || call.at.Before(entry.first) {
			entry.first = call.at
		}
		if call.at.After(entry.last) {
			entry.last = call.at
		}
	}
	if call.key.agent == "" {
		data.main.add(call.model, usage)
		entry.main.add(call.model, usage)
		entry.peakContext = math.Max(entry.peakContext, usage.context())
		return
	}
	data.sub.add(call.model, usage)
	ledgerAt(data.subByDay, day).add(call.model, usage)
	entry.sub.add(call.model, usage)
	agent := data.agents[call.key]
	if agent == nil {
		agent = &costAgent{key: call.key, meta: scan.meta[call.key], spend: costLedger{}}
		data.agents[call.key] = agent
	}
	agent.spend.add(call.model, usage)
	agent.peakContext = math.Max(agent.peakContext, usage.context())
	if call.timed && (agent.first.IsZero() || call.at.Before(agent.first)) {
		agent.first, agent.startContext = call.at, usage.context()
	}
}

func mergeBucket(into, from *tokenBucket) {
	into.input += from.input
	into.output += from.output
	into.cacheRead += from.cacheRead
	into.cacheWrite += from.cacheWrite
	into.cacheWrite1h += from.cacheWrite1h
	into.calls += from.calls
	if from.longPrompt != nil {
		if into.longPrompt == nil {
			into.longPrompt = &tokenBucket{}
		}
		mergeBucket(into.longPrompt, from.longPrompt)
	}
}

func (agent *costAgent) kindName() string {
	return orDefault(agent.meta.kind, "?")
}

func (data costData) share(cost float64) float64 {
	if data.totalCost <= 0 {
		return 0
	}
	return cost / data.totalCost
}

func (data costData) sortedAgents() []*costAgent {
	list := make([]*costAgent, 0, len(data.agents))
	for _, agent := range data.agents {
		list = append(list, agent)
	}
	sort.Slice(list, func(a, b int) bool {
		if list[a].cost != list[b].cost {
			return list[a].cost > list[b].cost
		}
		return list[a].key.agent < list[b].key.agent
	})
	return list
}

func (data costData) sortedSessions() []*costSession {
	list := make([]*costSession, 0, len(data.sessions))
	for _, entry := range data.sessions {
		list = append(list, entry)
	}
	sort.Slice(list, func(a, b int) bool {
		if list[a].cost != list[b].cost {
			return list[a].cost > list[b].cost
		}
		return list[a].id < list[b].id
	})
	return list
}

func (data costData) sortedKinds() []string {
	names := sortedKeys(data.kinds)
	sort.SliceStable(names, func(a, b int) bool { return data.kinds[names[a]].cost > data.kinds[names[b]].cost })
	return names
}

func (data costData) sortedModels() []string {
	names := sortedKeys(data.all)
	cost := func(model string) float64 { return costLedger{model: data.all[model]}.parts(data.prices).total() }
	sort.SliceStable(names, func(a, b int) bool { return cost(names[a]) > cost(names[b]) })
	return names
}

func percentShare(part float64) string {
	switch {
	case part <= 0:
		return "0%"
	case part < 0.01:
		return fmt.Sprintf("%.1f%%", part*100)
	}
	return fmt.Sprintf("%.0f%%", part*100)
}

func shortKey(id string) string {
	if len([]rune(id)) > 8 {
		return string([]rune(id)[:8])
	}
	return id
}

func clipText(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if cellWidth(text) <= limit {
		return text
	}
	width := 0
	for index, char := range text {
		if width += runeCells(char); width > limit-1 {
			return text[:index] + "…"
		}
	}
	return text
}

func runeCells(char rune) int {
	if char >= 0x1100 && (char <= 0x115f || char >= 0x2e80 && char <= 0xa4cf || char >= 0xac00 && char <= 0xd7a3 || char >= 0xf900 && char <= 0xfaff ||
		char >= 0xfe30 && char <= 0xfe4f || char >= 0xff00 && char <= 0xff60 || char >= 0xffe0 && char <= 0xffe6 || char >= 0x1f300 && char <= 0x1faff || char >= 0x20000 && char <= 0x3fffd) {
		return 2
	}
	return 1
}

func cellWidth(text string) int {
	width := 0
	for _, char := range text {
		width += runeCells(char)
	}
	return width
}

func padCells(text string, width int) string {
	return text + strings.Repeat(" ", max(0, width-cellWidth(text)))
}

func costTable(rows ...[]string) []string {
	widths := []int{14}
	for _, row := range rows {
		for column, cell := range row {
			if column == len(widths) {
				widths = append(widths, 12)
			}
			widths[column] = max(widths[column], cellWidth(cell))
		}
	}
	lines := make([]string, len(rows))
	for index, row := range rows {
		var line strings.Builder
		line.WriteString("  " + padCells(row[0], widths[0]))
		for column := 1; column < len(row); column++ {
			line.WriteString(" " + strings.Repeat(" ", widths[column]-cellWidth(row[column])) + row[column])
		}
		lines[index] = line.String()
	}
	return lines
}

func costText(data costData, top int) string {
	if len(data.all) == 0 {
		return T("cost.none", formatNumber(data.days), files.configDir)
	}
	parts, tokens := data.all.parts(data.prices), data.all.tokens()
	lines := []string{T("cost.header", formatNumber(data.days), data.since.Local().Format("2006-01-02 15:04"), data.files, len(data.agents)), ""}
	lines = append(lines, costTable(
		[]string{"", T("cost.input"), T("cost.cacheWrite"), T("cost.cacheRead"), T("cost.output"), T("cost.total")},
		[]string{T("cost.tokens"), formatTokens(tokens.input), formatTokens(tokens.cacheWrite), formatTokens(tokens.cacheRead), formatTokens(tokens.output), formatTokens(tokens.total())},
		[]string{T("cost.price"), formatUSD(parts.input), formatUSD(parts.cacheWrite), formatUSD(parts.cacheRead), formatUSD(parts.output), formatUSD(parts.total())},
		[]string{T("cost.share"), percentShare(data.share(parts.input)), percentShare(data.share(parts.cacheWrite)), percentShare(data.share(parts.cacheRead)), percentShare(data.share(parts.output))},
	)...)
	main, sub := data.main.parts(data.prices), data.sub.parts(data.prices)
	lines = append(lines, "", T("cost.split", formatUSD(main.total()), percentShare(data.share(main.total())), data.main.tokens().calls, formatUSD(sub.total()), percentShare(data.share(sub.total())), data.sub.tokens().calls, len(data.agents)))
	if withoutReads := parts.total() - parts.cacheRead; withoutReads > 0 {
		lines = append(lines, T("cost.withoutReads", percentShare((sub.total()-sub.cacheRead)/withoutReads)))
	}
	lines = append(lines, "", T("cost.byDay"))
	for _, day := range sortedKeys(data.byDay) {
		dayCost := data.byDay[day].parts(data.prices).total()
		subCost := data.subByDay[day].parts(data.prices).total()
		subShare := 0.0
		if dayCost > 0 {
			subShare = subCost / dayCost
		}
		lines = append(lines, fmt.Sprintf("  %-12s %8d · %10s · %10s %5s", day, data.byDay[day].tokens().calls, formatUSD(dayCost), formatUSD(subCost), percentShare(subShare)))
	}
	lines = append(lines, "", T("cost.byModel"))
	for _, model := range data.sortedModels() {
		bucket := data.all[model]
		costShown := "-"
		if price, ok := data.prices[model]; ok {
			costShown = formatUSD(bucket.cost(price))
		}
		lines = append(lines, fmt.Sprintf("  %-24s %8d · %8s %8s %9s %8s · %s", model, bucket.calls, formatTokens(bucket.input), formatTokens(bucket.cacheWrite), formatTokens(bucket.cacheRead), formatTokens(bucket.output), costShown))
	}
	if len(data.unpriced) > 0 {
		lines = append(lines, "  "+T("report.unpriced", strings.Join(data.unpriced, ", ")))
	}
	lines = append(lines, "", T("cost.bySession"))
	for _, entry := range data.sortedSessions() {
		lines = append(lines, fmt.Sprintf("  %-8s %s %8d · %4d · %10s %10s %10s · %5s", shortKey(entry.id), padCells(clipText(entry.project, 22), 22), entry.main.tokens().calls+entry.sub.tokens().calls, entry.agents,
			formatUSD(entry.main.parts(data.prices).total()), formatUSD(entry.sub.parts(data.prices).total()), formatUSD(entry.cost), percentShare(data.share(entry.cost))))
	}
	if len(data.kinds) > 0 {
		lines = append(lines, "", T("cost.byKind"))
		for _, name := range data.sortedKinds() {
			kind := data.kinds[name]
			lines = append(lines, fmt.Sprintf("  %s %4d · %8d · %10s · %5s", padCells(clipText(name, 24), 24), kind.agents, kind.spend.tokens().calls, formatUSD(kind.cost), percentShare(data.share(kind.cost))))
		}
	}
	if agents := data.sortedAgents(); len(agents) > 0 {
		lines = append(lines, "", T("cost.top", min(top, len(agents))))
		for _, agent := range agents[:min(top, len(agents))] {
			lines = append(lines, fmt.Sprintf("  %-8s %s %5d %6d · %7s %7s · %2d · %9s %5s · %s", shortKey(agent.key.agent), padCells(clipText(agent.kindName(), 18), 18), agent.tools, agent.spend.tokens().calls,
				formatTokens(agent.startContext), formatTokens(agent.peakContext), agent.compactions, formatUSD(agent.cost), percentShare(data.share(agent.cost)), clipText(agent.meta.description, 48)))
		}
	}
	return strings.Join(lines, "\n")
}

func ledgerJSON(ledger costLedger, prices map[string]reportPrice) object {
	tokens, parts := ledger.tokens(), ledger.parts(prices)
	return object{"calls": tokens.calls, "inputTokens": tokens.input, "cacheWriteTokens": tokens.cacheWrite, "cacheReadTokens": tokens.cacheRead, "outputTokens": tokens.output,
		"cost": object{"input": parts.input, "cacheWrite": parts.cacheWrite, "cacheRead": parts.cacheRead, "output": parts.output, "total": parts.total()}}
}

func costJSON(data costData, top int) object {
	days := object{}
	for day, ledger := range data.byDay {
		entry := ledgerJSON(ledger, data.prices)
		entry["subagents"] = ledgerJSON(ledgerAt(data.subByDay, day), data.prices)
		days[day] = entry
	}
	models := object{}
	for model := range data.all {
		models[model] = ledgerJSON(costLedger{model: data.all[model]}, data.prices)
	}
	sessions := []any{}
	for _, entry := range data.sortedSessions() {
		sessions = append(sessions, object{"session": entry.id, "project": entry.project, "agents": entry.agents, "compactions": entry.compactions, "peakContext": entry.peakContext,
			"main": ledgerJSON(entry.main, data.prices), "subagents": ledgerJSON(entry.sub, data.prices), "cost": entry.cost, "share": data.share(entry.cost)})
	}
	kinds := object{}
	for name, kind := range data.kinds {
		entry := ledgerJSON(kind.spend, data.prices)
		entry["agents"], entry["share"] = kind.agents, data.share(kind.cost)
		kinds[name] = entry
	}
	agents := []any{}
	for _, agent := range data.sortedAgents()[:min(top, len(data.agents))] {
		agents = append(agents, object{"agent": agent.key.agent, "session": agent.key.session, "type": agent.meta.kind, "description": agent.meta.description,
			"toolCalls": agent.tools, "apiCalls": agent.spend.tokens().calls, "startContext": agent.startContext, "peakContext": agent.peakContext, "compactions": agent.compactions,
			"cost": agent.cost, "share": data.share(agent.cost)})
	}
	return object{"days": data.days, "since": data.since.UTC().Format(time.RFC3339), "transcripts": data.files, "subagentCount": len(data.agents), "unpriced": data.unpriced,
		"total": ledgerJSON(data.all, data.prices), "main": ledgerJSON(data.main, data.prices), "subagents": ledgerJSON(data.sub, data.prices),
		"byDay": days, "byModel": models, "sessions": sessions, "byType": kinds, "topSubagents": agents}
}

func runCost() {
	cfg := loadConfig()
	days := 7.0
	if value, ok := toNumber(flagString("days")); ok && value > 0 {
		days = math.Min(value, lookbackMaxDays)
	}
	top := 10
	if value, ok := toNumber(flagString("top")); ok && value >= 1 {
		top = int(math.Min(value, 1000))
	}
	data := collectCost(cfg, days, flagString("session"))
	if args.present["json"] {
		fmt.Println(string(marshalPretty(costJSON(data, top))))
		return
	}
	fmt.Println(costText(data, top))
}
