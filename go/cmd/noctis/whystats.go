package main

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
)

const (
	whyStatsDays    = 7
	whyStatsMaxDays = 3650
)

var (
	pauseWindowOrder = []string{"five_hour", "seven_day", "fable"}
	pauseCauseOrder  = []string{"compaction", "burst", "projection", "blind", "budget", "ceiling"}
)

type countedPart struct {
	count int
	text  string
}

type queueCheckTally struct {
	runs, passed, failed, cutShort, skipped, perItem, handedUp, held int
	seconds                                                          float64
}

type queueContinueTally struct {
	total, treeMoved, handedUp, setAside, letGo, dayLimits int
	dayLimitSeen                                           map[string]bool
}

type decisionStats struct {
	from, to                               float64
	decisions, observed                    int
	pauses                                 int
	pausesByWindow, pausesByCause          map[string]int
	plannedWait                            float64
	confirmedResets, earlyResets, dataBack int
	secondsEarlier                         float64
	limitHits                              int
	checks                                 queueCheckTally
	continues                              queueContinueTally
}

func eachJournalEntry(visit func(entry object)) {
	for _, file := range []string{files.decisions + ".1", files.decisions} {
		content, err := readFileShared(file)
		if err != nil {
			continue
		}
		for len(content) > 0 {
			var line []byte
			line, content, _ = bytes.Cut(content, []byte{'\n'})
			var entry object
			if len(line) > 0 && jsonUnmarshalObject(line, &entry) == nil && entry != nil {
				visit(entry)
			}
		}
	}
}

func gatherDecisionStats(since, now float64) decisionStats {
	stats := decisionStats{to: now, pausesByWindow: map[string]int{}, pausesByCause: map[string]int{}}
	stats.continues.dayLimitSeen = map[string]bool{}
	oldest := now
	eachJournalEntry(func(entry object) {
		at := numberOr(entry, "at", 0)
		oldest = math.Min(oldest, at)
		if at < since {
			return
		}
		action := getString(entry, "action")
		if strings.HasPrefix(action, "would-") {
			stats.observed++
			return
		}
		stats.decisions++
		stats.add(entry, action, at)
	})
	stats.from = math.Max(since, oldest)
	return stats
}

func (stats *decisionStats) add(entry object, action string, at float64) {
	switch action {
	case "pause":
		stats.addPause(entry, at)
	case "reset-confirmed":
		stats.confirmedResets++
		stats.secondsEarlier += numberOr(entry, "ahead", 0)
	case "early-reset":
		stats.earlyResets++
	case "data-back":
		stats.dataBack++
	case "schedule-resume":
		stats.limitHits++
	case "verify-queue", "hold-queue":
		stats.checks.add(entry, action)
	case "continue-queue":
		stats.continues.add(entry)
	case "allow-stop":
		stats.continues.addLetGo(entry, at)
	}
}

func (stats *decisionStats) addPause(entry object, at float64) {
	stats.pauses++
	if window := getString(entry, "window"); window != "" {
		stats.pausesByWindow[window]++
	}
	if cause := getString(entry, "hit"); cause != "" && cause != "threshold" {
		stats.pausesByCause[cause]++
	}
	if planned := numberOr(entry, "resumeAt", 0) - at; planned > 0 {
		stats.plannedWait += planned
	}
}

func (checks *queueCheckTally) add(entry object, action string) {
	if getBool(entry, "skipped", false) {
		checks.skipped++
		return
	}
	checks.runs++
	checks.seconds += numberOr(entry, "seconds", 0)
	if getString(entry, "tier") == eachQueueCheck {
		checks.perItem++
	}
	reason := getString(entry, "reason")
	switch {
	case action == "hold-queue":
		checks.failed++
		checks.held++
	case reason == "passed":
		checks.passed++
	case strings.HasPrefix(reason, "cut short"):
		checks.cutShort++
	default:
		checks.failed++
		if getBool(entry, "escalated", false) {
			checks.handedUp++
		}
	}
}

func (continues *queueContinueTally) add(entry object) {
	continues.total++
	if getBool(entry, "treeMoved", false) {
		continues.treeMoved++
	}
	if getBool(entry, "escalated", false) {
		continues.handedUp++
	}
	if getBool(entry, "setAside", false) {
		continues.setAside++
	}
}

func (continues *queueContinueTally) addLetGo(entry object, at float64) {
	switch getString(entry, "reason") {
	case "queue not progressing":
		continues.letGo++
	case "daily continue limit":
		sessionDay := getString(entry, "sid") + " " + localDay(int64(at))
		if !continues.dayLimitSeen[sessionDay] {
			continues.dayLimitSeen[sessionDay] = true
			continues.dayLimits++
		}
	}
}

func runWhyStats() {
	days := float64(whyStatsDays)
	if value, ok := toNumber(flagString("days")); ok && value > 0 && !math.IsInf(value, 1) {
		days = math.Min(value, whyStatsMaxDays)
	}
	now := float64(nowSec())
	stats := gatherDecisionStats(now-days*86400, now)
	if args.present["json"] {
		fmt.Println(string(marshalCompact(stats.report(days))))
		return
	}
	if stats.decisions == 0 && stats.observed == 0 {
		fmt.Println(T("why.empty"))
		return
	}
	for _, line := range stats.lines(loadConfig()) {
		fmt.Println(line)
	}
}

func (stats decisionStats) report(days float64) object {
	return object{
		"days": days, "from": stats.from, "to": stats.to, "decisions": float64(stats.decisions), "observed": float64(stats.observed),
		"pauses": object{"count": float64(stats.pauses), "plannedSeconds": math.Round(stats.plannedWait),
			"windows": countsObject(stats.pausesByWindow), "causes": countsObject(stats.pausesByCause)},
		"resumes": object{"resetConfirmed": float64(stats.confirmedResets), "secondsEarlier": math.Round(stats.secondsEarlier),
			"earlyReset": float64(stats.earlyResets), "dataBack": float64(stats.dataBack)},
		"limitHits": float64(stats.limitHits),
		"checks": object{"runs": float64(stats.checks.runs), "seconds": math.Round(stats.checks.seconds), "passed": float64(stats.checks.passed),
			"failed": float64(stats.checks.failed), "cutShort": float64(stats.checks.cutShort), "skipped": float64(stats.checks.skipped),
			"perItem": float64(stats.checks.perItem), "handedUp": float64(stats.checks.handedUp), "held": float64(stats.checks.held)},
		"continues": object{"count": float64(stats.continues.total), "treeMoved": float64(stats.continues.treeMoved),
			"handedUp": float64(stats.continues.handedUp), "setAside": float64(stats.continues.setAside),
			"letGo": float64(stats.continues.letGo), "dayLimits": float64(stats.continues.dayLimits)},
	}
}

func countsObject(counts map[string]int) object {
	out := object{}
	for key, count := range counts {
		out[key] = float64(count)
	}
	return out
}

func (stats decisionStats) lines(cfg object) []string {
	lines := []string{T("why.statsHead", formatTime(stats.from), formatTime(stats.to), stats.decisions)}
	if stats.observed > 0 {
		lines = append(lines, T("why.statsObserved", stats.observed))
	}
	lines = append(lines, T("why.statsPauses", stats.pauses))
	lines = withParts(lines, stats.pauseParts(cfg))
	lines = append(lines, T("why.statsResumes", stats.confirmedResets+stats.earlyResets+stats.dataBack))
	lines = withParts(lines, shownParts(
		countedPart{stats.confirmedResets, T("why.statsConfirmed", stats.confirmedResets, spentText(stats.secondsEarlier))},
		countedPart{stats.earlyResets, T("why.statsEarly", stats.earlyResets)},
		countedPart{stats.dataBack, T("why.statsDataBack", stats.dataBack)}))
	lines = append(lines, T("why.statsLimitHits", stats.limitHits))
	checks := stats.checks
	lines = append(lines, T("why.statsChecks", checks.runs))
	lines = withParts(lines, shownParts(
		countedPart{checks.runs, T("why.statsTook", spentText(checks.seconds))},
		countedPart{checks.passed, T("why.statsPassed", checks.passed)},
		countedPart{checks.failed, T("why.statsFailed", checks.failed)},
		countedPart{checks.cutShort, T("why.statsCutShort", checks.cutShort)},
		countedPart{checks.skipped, T("why.statsSkipped", checks.skipped)},
		countedPart{checks.perItem, T("why.statsPerItem", checks.perItem)},
		countedPart{checks.handedUp, T("why.statsHandedUp", checks.handedUp)},
		countedPart{checks.held, T("why.statsHeld", checks.held)}))
	continues := stats.continues
	lines = append(lines, T("why.statsContinues", continues.total))
	return withParts(lines, shownParts(
		countedPart{continues.treeMoved, T("why.statsTreeMoved", continues.treeMoved)},
		countedPart{continues.handedUp, T("why.statsHandedUp", continues.handedUp)},
		countedPart{continues.setAside, T("why.statsSetAside", continues.setAside)},
		countedPart{continues.letGo, T("why.statsLetGo", continues.letGo)},
		countedPart{continues.dayLimits, T("why.statsDayLimit", continues.dayLimits)}))
}

func spentText(seconds float64) string {
	if rounded := math.Round(seconds); rounded < 60 {
		return T("why.statsSeconds", int(rounded))
	}
	return durationText(seconds)
}

func (stats decisionStats) pauseParts(cfg object) []string {
	parts := shownParts(countedPart{stats.pauses, T("why.statsPlanned", spentText(stats.plannedWait))})
	for _, window := range orderedKeys(stats.pausesByWindow, pauseWindowOrder) {
		label := windowLabel(window)
		if window == "fable" {
			label = scopedLabel(cfg)
		}
		parts = append(parts, T("why.statsPart", label, stats.pausesByWindow[window]))
	}
	for _, cause := range orderedKeys(stats.pausesByCause, pauseCauseOrder) {
		label := cause
		if slices.Contains(pauseCauseOrder, cause) {
			label = T("hit." + cause)
		}
		parts = append(parts, T("why.statsPart", label, stats.pausesByCause[cause]))
	}
	return parts
}

func orderedKeys(counts map[string]int, known []string) []string {
	keys := []string{}
	for _, key := range known {
		if counts[key] > 0 {
			keys = append(keys, key)
		}
	}
	others := []string{}
	for key := range counts {
		if !slices.Contains(known, key) {
			others = append(others, key)
		}
	}
	sort.Strings(others)
	return append(keys, others...)
}

func shownParts(parts ...countedPart) []string {
	shown := []string{}
	for _, part := range parts {
		if part.count > 0 {
			shown = append(shown, part.text)
		}
	}
	return shown
}

func withParts(lines, parts []string) []string {
	if len(parts) == 0 {
		return lines
	}
	return append(lines, "   "+strings.Join(parts, " · "))
}
