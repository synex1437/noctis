package main

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

// A queue item a session keeps stopping on goes once to a stronger model before it is set aside. At
// the continuation that would ask Claude to set the item aside, the Stop hook asks it instead to
// hand the item to a fresh subagent one step up: Opus for a session on Sonnet or Haiku, noctis's
// deep agent (Opus at max effort) for one on Opus below max. The session's own model stays as it is,
// and so does its prompt cache. The escalation lives in noctis's state, not in the queue file, so
// the file and its trust stay as they are. When the stronger model gets stuck on the item as well,
// the next continuation asks Claude to set it aside as before, and the reason Claude gives reaches
// the daily digest. queue.escalate turns this off or names the model; queue.maxEscalationsPerDay
// bounds how many items a day go up.
//
// noctis also notes, for each queue file, how many items each setup (the model and effort the
// session runs on) finished, how many went up and what share of the weekly limit they took, so noctis
// queue status, noctis status and the daily digest can say which setup finishes the queue for less.

const (
	// deepAgentName is noctis's agent for an item escalated from Opus below max effort.
	deepAgentName = "deep"
	// escalationsKept bounds the escalated items a queue file keeps.
	escalationsKept = 50
	// setupHintItems is how many items with a reading of the weekly limit a setup needs before the
	// hint weighs it.
	setupHintItems = 6
	// setupHintUpShare: escalations on at least this share of a setup's items suggest one that
	// starts on the stronger model.
	setupHintUpShare = 0.25
	// setupHintMargin is how much more a weekly limit must cover on a setup for the hint to name it.
	setupHintMargin = 0.15
)

// sessionEffort is the effort a session runs at: CLAUDE_CODE_EFFORT_LEVEL as the session has it, else
// as the settings set it, else the effort noctis set up for the code role.
func sessionEffort(cfg object) string {
	if effort := strings.ToLower(strings.TrimSpace(os.Getenv("CLAUDE_CODE_EFFORT_LEVEL"))); validEfforts[effort] {
		return effort
	}
	if settings := readJSONStrict(files.settings); settings.ok {
		for _, effort := range []string{getString(getMap(settings.data, "env"), "CLAUDE_CODE_EFFORT_LEVEL"), getString(settings.data, "effortLevel")} {
			if effort = strings.ToLower(strings.TrimSpace(effort)); validEfforts[effort] {
				return effort
			}
		}
	}
	return strings.ToLower(getString(section(cfg, "models"), "effort"))
}

// sessionSetup names the model family and effort a session runs on, as "sonnet/high", or is "" when
// its model is none of the known families.
func sessionSetup(cfg, state object, sid string) string {
	family := modelFamily(resolveSessionModel(cfg, state, readJSON(files.usage), sid))
	if family == "" {
		return ""
	}
	if effort := sessionEffort(cfg); effort != "" && modelTakesEffort(family) {
		return family + "/" + effort
	}
	return family
}

// setupTitle shows a setup as "Sonnet · high".
func setupTitle(setup string) string {
	family, effort, _ := strings.Cut(setup, "/")
	if family == "" {
		return setup
	}
	title := strings.ToUpper(family[:1]) + family[1:]
	if effort != "" {
		title += " · " + effort
	}
	return title
}

// setupProfileTitle names the profile whose code role a setup is, with the setup: "Balanced (Sonnet ·
// high)"; the setup alone when no profile has it.
func setupProfileTitle(setup string) string {
	for _, name := range []string{"balanced", "code", "synex"} {
		code := getMap(roleProfiles[name], "code")
		label := getString(code, "model")
		if effort := appliedEffort("code", code); effort != "" {
			label += "/" + effort
		}
		if label == setup {
			return profileTitles[name] + " (" + setupTitle(setup) + ")"
		}
	}
	return setupTitle(setup)
}

// escalationTarget is the stronger model a stuck item goes to and the subagent that takes it there,
// or "" when there is none: one step up from the model the item is tagged for, or else from the
// session's model.
func escalationTarget(cfg, state object, sid, item string) (model, agent string) {
	if !currentHost().agents {
		return "", ""
	}
	setting := strings.ToLower(strings.TrimSpace(orDefault(getString(section(cfg, "queue"), "escalate"), "auto")))
	// A Fable whose quota is out takes no item: the choice falls back to the step up.
	if setting == "fable" && getMap(state, "modelSwitched") != nil {
		setting = "auto"
	}
	from := itemModel(item)
	if from == "" {
		from = modelFamily(resolveSessionModel(cfg, state, readJSON(files.usage), sid))
	}
	target := setting
	switch setting {
	case "auto":
		if from != "haiku" && from != "sonnet" && from != "opus" {
			return "", ""
		}
		target = "opus"
	case "opus", "sonnet", "haiku", "fable":
	default:
		return "", ""
	}
	if target != from {
		return target, "general-purpose"
	}
	if target == "opus" && sessionEffort(cfg) != "max" {
		return target, pluginName + ":" + deepAgentName
	}
	return "", ""
}

// escalationKey names an item the way a queue view shows it, so a record written from the next item
// of a view finds the entry of the file it came from.
func escalationKey(text string) string {
	return queueItemDigest(truncateText(text, 160))
}

// queueModelsOf is the record of the queue file at path in state.queueModels: the items that went to
// a stronger model (items) and what each setup finished (setups).
func queueModelsOf(state object, path string) object {
	return getMap(getMap(state, "queueModels"), queueTrustKey(path))
}

func escalationRecord(state object, path, item string) object {
	return getMap(getMap(queueModelsOf(state, path), "items"), escalationKey(item))
}

func escalationsToday(state object, now int64) float64 {
	record := getMap(state, "escalateDay")
	if getString(record, "day") != localDay(now) {
		return 0
	}
	return numberOr(record, "count", 0)
}

// stuckStep is what a continuation does about the item a session keeps stopping on.
type stuckStep struct {
	item, path, sid string
	// idle is how many stops in a row made no progress before this continuation.
	idle float64
	// escalate: this continuation hands the item to model through agent.
	escalate bool
	// escalated: the item went up at an earlier continuation and is still with that model.
	escalated bool
	// setAside: this continuation asks Claude to set the item aside.
	setAside     bool
	model, agent string
	// setup is the model and effort the session runs on, for the setup's record.
	setup string
	// capped: the item would have gone up, but queue.maxEscalationsPerDay items went up today.
	capped bool
}

// stuckItemStep decides what the continuation after a stop without progress does about the next
// item. At the last continuation before the queue would give up, the item goes to a stronger model
// once when there is one and no pause is due; otherwise Claude is asked to set it aside. An item that
// went up stays with its stronger model, and is set aside at that same last continuation.
func stuckItemStep(cfg, state object, sid, path string, idle, maxIdle float64, items []string, pausing bool, now int64) stuckStep {
	step := stuckStep{path: path, sid: sid, idle: idle}
	if len(items) == 0 {
		return step
	}
	step.item = items[0]
	last := idle > 0 && idle+1 >= maxIdle
	record := escalationRecord(state, path, step.item)
	if record != nil && getString(record, "outcome") != "done" {
		// An item that went up stays with its stronger model, also when it got stuck there before and
		// the user took it up again after its deferral.
		step.escalated, step.model, step.agent = true, getString(record, "model"), getString(record, "agent")
		step.setAside = last
		return step
	}
	if !last {
		return step
	}
	step.setAside, step.setup = true, sessionSetup(cfg, state, sid)
	if record != nil || pausing {
		return step
	}
	model, agent := escalationTarget(cfg, state, sid, step.item)
	perDay := numberOr(section(cfg, "queue"), "maxEscalationsPerDay", 5)
	if model == "" || perDay <= 0 {
		return step
	}
	if escalationsToday(state, now) >= perDay {
		step.capped = true
		logInfo("queue item not escalated for %s: queue.maxEscalationsPerDay (%s) reached; set aside instead", sid, formatNumber(perDay))
		return step
	}
	step.escalate, step.setAside, step.model, step.agent = true, false, model, agent
	logInfo("queue item escalated for %s after %s idle continues: %q → %s (%s)", sid, formatNumber(idle), truncateText(step.item, 120), model, agent)
	return step
}

// agentText names the subagent a step hands its item to, for Claude.
func (step stuckStep) agentText() string {
	if step.agent == pluginName+":"+deepAgentName {
		return fmt.Sprintf("the %s subagent (Opus at max effort)", step.agent)
	}
	return fmt.Sprintf("a general-purpose subagent with model \"%s\"", step.model)
}

// modelTitle names the model a step hands its item to, for the user.
func (step stuckStep) modelTitle() string {
	if step.agent == pluginName+":"+deepAgentName {
		return setupTitle("opus/max")
	}
	return setupTitle(step.model)
}

// lead is what the continuation asks of Claude in place of taking the next item, and what the user
// is told, or "" when the step asks nothing else.
func (step stuckStep) lead(path string) (rule, notice string) {
	switch {
	case step.escalate:
		rule = fmt.Sprintf(`The session keeps stopping without ticking an item or committing, and "%s" is still the next item. Hand it once to %s, in the foreground so you get its result, with a brief that stands on its own: the goal and what done means, what you tried, where and why it failed (the errors verbatim), and the files and decisions it needs, so it does not repeat your attempts. Then check its work and tick the item yourself. If it cannot finish the item either, the next stop asks you to set it aside.`, step.item, step.agentText())
		return rule, T("queue.escalateMessage", int(step.idle), truncateText(step.item, 100), step.modelTitle())
	case step.setAside && step.escalated:
		rule = queueSetAsideRule(step.item, path) + fmt.Sprintf(" It already went to a stronger model (%s), which did not finish it either: give the root cause it found as the reason.", step.modelTitle())
		return rule, T("queue.escalatedStuck", truncateText(step.item, 100), step.modelTitle())
	case step.setAside:
		return queueSetAsideRule(step.item, path), T("queue.setAsideMessage", int(step.idle), truncateText(step.item, 100))
	}
	return "", ""
}

// note keeps an item that went up with its stronger model on the continuations after, or is "".
func (step stuckStep) note() string {
	if !step.escalated || step.setAside {
		return ""
	}
	return fmt.Sprintf(" The next item went to a stronger model after a session got stuck on it: keep it with %s in the foreground, give it what the last attempt found, then check its work and tick the item yourself.", step.agentText())
}

// queueModelsRecord is the record of the queue file at path in state, made when there is none.
func queueModelsRecord(state object, path string, now int64) object {
	all := stateMap(state, "queueModels")
	key := queueTrustKey(path)
	record := toObject(all[key])
	if record == nil {
		record = object{}
		all[key] = record
	}
	record["path"], record["at"] = path, float64(now)
	return record
}

func addNumber(record object, key string, amount float64) {
	record[key] = math.Round((numberOr(record, key, 0)+amount)*100) / 100
}

// noteStuckItem records a continuation's step on a stuck item in state: the item that went up and
// the day's count of them, an item that got stuck on its stronger model as well, and the stuck items
// of a setup.
func noteStuckItem(state object, step stuckStep, now int64) {
	if !step.escalate && !step.setAside {
		return
	}
	record := queueModelsRecord(state, step.path, now)
	items := stateMap(record, "items")
	key := escalationKey(step.item)
	switch {
	case step.escalate:
		items[key] = object{"text": truncateText(step.item, 300), "model": step.model, "agent": step.agent, "setup": step.setup, "sid": step.sid, "at": float64(now)}
		pruneEscalations(items)
		if step.setup != "" {
			addNumber(stateMap(stateMap(record, "setups"), step.setup), "escalated", 1)
		}
		day := localDay(now)
		if getString(getMap(state, "escalateDay"), "day") != day {
			state["escalateDay"] = object{"day": day, "count": float64(0)}
		}
		addNumber(stateMap(state, "escalateDay"), "count", 1)
	case step.escalated:
		if entry := getMap(items, key); entry != nil && getString(entry, "outcome") == "" {
			entry["outcome"], entry["closedAt"] = "stuck", float64(now)
		}
	case step.setup != "":
		addNumber(stateMap(stateMap(record, "setups"), step.setup), "stuck", 1)
	}
}

// pruneEscalations keeps the escalationsKept items that went up last.
func pruneEscalations(items object) {
	if len(items) <= escalationsKept {
		return
	}
	keys := sortedKeys(items)
	sort.SliceStable(keys, func(a, b int) bool {
		return numberOr(toObject(items[keys[a]]), "at", 0) < numberOr(toObject(items[keys[b]]), "at", 0)
	})
	for _, key := range keys[:len(items)-escalationsKept] {
		delete(items, key)
	}
}

// noteFinished credits the items ticked between two pace notes of the queue file at path to the setup
// the session runs on, with their share of the weekly limit when both notes read one weekly window,
// and marks the items that went up and are ticked now as done.
func noteFinished(state object, path, content string, before, after paceNote, setup string, now int64) {
	ticked := after.done - before.done
	if ticked <= 0 {
		return
	}
	record := queueModelsRecord(state, path, now)
	finished := 0.0
	if items := getMap(record, "items"); len(items) > 0 {
		entries, _ := parseQueueEntries(content)
		for _, entry := range entries {
			if !entry.checked || entry.text == "" {
				continue
			}
			if escalated := getMap(items, escalationKey(entry.text)); escalated != nil && getString(escalated, "outcome") != "done" {
				escalated["outcome"], escalated["closedAt"] = "done", float64(now)
				finished++
			}
		}
	}
	if setup == "" {
		return
	}
	finished = math.Min(finished, ticked)
	stats := stateMap(stateMap(record, "setups"), setup)
	addNumber(stats, "items", ticked)
	addNumber(stats, "escalatedDone", finished)
	if after.at-before.at > paceGapSeconds || before.used < 0 || after.used < 0 || math.Abs(after.resetsAt-before.resetsAt) >= paceSameWindowSeconds {
		return
	}
	used := math.Max(0, after.used-before.used)
	addNumber(stats, "weekly", used)
	addNumber(stats, "weeklyItems", ticked)
	if finished > 0 {
		addNumber(stats, "escalatedWeekly", used*finished/ticked)
		addNumber(stats, "escalatedWeeklyItems", finished)
	}
}

// setupStat is what one setup did on a queue file.
type setupStat struct {
	setup                                 string
	items, escalated, escalatedDone       float64
	stuck, weekly, weeklyItems            float64
	escalatedWeekly, escalatedWeeklyItems float64
}

// perLimit is how many items a whole weekly limit covers on the setup, or 0 while that is not known.
func (stat setupStat) perLimit() float64 {
	if stat.weeklyItems < paceMinItems || stat.weekly <= 0 {
		return 0
	}
	return 100 * stat.weeklyItems / stat.weekly
}

// setupStatsOf is what each setup did on the queue file at path, the one with the most items first.
func setupStatsOf(state object, path string) []setupStat {
	stats := []setupStat{}
	for setup, raw := range getMap(queueModelsOf(state, path), "setups") {
		record := toObject(raw)
		stat := setupStat{setup: setup}
		for key, into := range map[string]*float64{"items": &stat.items, "escalated": &stat.escalated, "escalatedDone": &stat.escalatedDone, "stuck": &stat.stuck, "weekly": &stat.weekly, "weeklyItems": &stat.weeklyItems, "escalatedWeekly": &stat.escalatedWeekly, "escalatedWeeklyItems": &stat.escalatedWeeklyItems} {
			*into = numberOr(record, key, 0)
		}
		if stat.items > 0 || stat.escalated > 0 || stat.stuck > 0 {
			stats = append(stats, stat)
		}
	}
	sort.Slice(stats, func(a, b int) bool {
		if stats[a].items != stats[b].items {
			return stats[a].items > stats[b].items
		}
		return stats[a].setup < stats[b].setup
	})
	return stats
}

// setupLines says what each setup finished on the queue file at path, what went to a stronger model
// and which setup finishes the queue for less once the numbers say so.
func setupLines(state object, path string) []string {
	stats := setupStatsOf(state, path)
	lines := []string{}
	for _, stat := range stats {
		line := T("queue.setupItems", setupTitle(stat.setup), int(stat.items))
		if covered := stat.perLimit(); covered > 0 {
			line = T("queue.setupItemsShare", setupTitle(stat.setup), int(stat.items), int(math.Round(covered)))
		}
		if stat.escalated > 0 {
			line += T("queue.setupEscalated", int(stat.escalated), int(stat.escalatedDone))
		}
		if stat.stuck > 0 {
			line += T("queue.setupStuck", int(stat.stuck))
		}
		lines = append(lines, line)
	}
	if hint := setupHint(stats); hint != "" {
		lines = append(lines, hint)
	}
	return lines
}

// setupHint names the setup that finishes the queue for less, or is "": from what two setups took of
// the weekly limit per item on it once both have enough items, or else from how often the items of
// the one setup got stuck.
func setupHint(stats []setupStat) string {
	measured := []setupStat{}
	for _, stat := range stats {
		if stat.weeklyItems >= setupHintItems && stat.perLimit() > 0 {
			measured = append(measured, stat)
		}
	}
	if len(measured) >= 2 {
		sort.SliceStable(measured, func(a, b int) bool { return measured[a].weeklyItems > measured[b].weeklyItems })
		first, second := measured[0], measured[1]
		cheaper, dearer := first, second
		if second.perLimit() > first.perLimit() {
			cheaper, dearer = second, first
		}
		if cheaper.perLimit() < dearer.perLimit()*(1+setupHintMargin) {
			return ""
		}
		return T("queue.hintCheaper", int(math.Round(cheaper.perLimit())), setupTitle(cheaper.setup), int(math.Round(dearer.perLimit())), setupTitle(dearer.setup), setupProfileTitle(cheaper.setup))
	}
	if len(stats) == 0 {
		return ""
	}
	stat := stats[0]
	if stat.items < setupHintItems*4/3 {
		return ""
	}
	family, effort, _ := strings.Cut(stat.setup, "/")
	switch {
	case (family == "sonnet" || family == "haiku") && stat.escalated >= stat.items*setupHintUpShare:
		up := "code"
		if family == "haiku" {
			up = "balanced"
		}
		return T("queue.hintUp", int(stat.escalated), int(stat.items), setupTitle(stat.setup), setupProfileTitle(profileCodeSetup(up)))
	case family == "opus" && stat.escalated == 0 && stat.stuck == 0:
		down := "balanced"
		if effort == "max" {
			down = "code"
		}
		return T("queue.hintDown", int(stat.items), setupTitle(stat.setup), setupProfileTitle(profileCodeSetup(down)))
	}
	return ""
}

// profileCodeSetup is the setup of a profile's code role, as "opus/xhigh".
func profileCodeSetup(profile string) string {
	code := getMap(roleProfiles[profile], "code")
	if effort := appliedEffort("code", code); effort != "" {
		return getString(code, "model") + "/" + effort
	}
	return getString(code, "model")
}

// openEscalations are the items of the queue file at path that are with a stronger model now, open in
// the file under the text they went up with: their text and that model, for the user.
func openEscalations(state object, path string) []string {
	type open struct {
		text string
		at   float64
	}
	content, _ := readQueueText(path)
	entries, _ := parseQueueEntries(content)
	unticked := map[string]bool{}
	for _, entry := range entries {
		if !entry.checked && entry.text != "" {
			unticked[escalationKey(entry.text)] = true
		}
	}
	found := []open{}
	for key, raw := range getMap(queueModelsOf(state, path), "items") {
		record := toObject(raw)
		if getString(record, "outcome") != "" || !unticked[key] {
			continue
		}
		step := stuckStep{model: getString(record, "model"), agent: getString(record, "agent")}
		found = append(found, open{text: digestItem(getString(record, "text")) + " → " + step.modelTitle(), at: numberOr(record, "at", 0)})
	}
	sort.Slice(found, func(a, b int) bool { return found[a].at < found[b].at })
	texts := []string{}
	for _, entry := range found {
		texts = append(texts, entry.text)
	}
	return texts
}

// escalationsSince counts the items of the queue file at path that went up after since, and of them
// the ones done and the ones that got stuck there too, with their texts.
func escalationsSince(state object, path string, since float64) (count, done, stuck int, texts []string) {
	type went struct {
		text string
		at   float64
	}
	found := []went{}
	for _, raw := range getMap(queueModelsOf(state, path), "items") {
		record := toObject(raw)
		if numberOr(record, "at", 0) <= since {
			continue
		}
		count++
		switch getString(record, "outcome") {
		case "done":
			done++
		case "stuck":
			stuck++
		}
		found = append(found, went{text: getString(record, "text"), at: numberOr(record, "at", 0)})
	}
	sort.Slice(found, func(a, b int) bool { return found[a].at < found[b].at })
	for _, entry := range found {
		texts = append(texts, entry.text)
	}
	return count, done, stuck, texts
}

// printSetups prints what each setup finished on the queue file at target and the items that are
// with a stronger model now, for noctis queue status.
func printSetups(target string) {
	state := readState()
	lines := setupLines(state, target)
	if open := openEscalations(state, target); len(open) > 0 {
		lines = append(lines, T("queue.escalatedOpen", strings.Join(open, "; ")))
	}
	for _, line := range lines {
		fmt.Println("  " + line)
	}
}

// setupFacts is what each setup did on the queue file at path and the items that went up, for noctis
// queue status --json.
func setupFacts(state object, path string) object {
	setups := []any{}
	for _, stat := range setupStatsOf(state, path) {
		entry := object{"setup": stat.setup, "items": stat.items, "escalated": stat.escalated, "escalatedDone": stat.escalatedDone, "stuck": stat.stuck}
		if covered := stat.perLimit(); covered > 0 {
			entry["itemsPerWeeklyLimit"] = math.Round(covered)
		}
		setups = append(setups, entry)
	}
	escalated := []any{}
	items := getMap(queueModelsOf(state, path), "items")
	for _, key := range sortedKeys(items) {
		record := toObject(items[key])
		escalated = append(escalated, object{"text": getString(record, "text"), "model": getString(record, "model"), "agent": getString(record, "agent"), "at": numberOr(record, "at", 0), "outcome": getString(record, "outcome")})
	}
	sort.SliceStable(escalated, func(a, b int) bool {
		return numberOr(toObject(escalated[a]), "at", 0) < numberOr(toObject(escalated[b]), "at", 0)
	})
	return object{"setups": setups, "escalated": escalated, "hint": setupHint(setupStatsOf(state, path))}
}
