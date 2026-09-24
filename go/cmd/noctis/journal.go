package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

var observing bool

func setMode(cfg object) {
	observing = strings.EqualFold(getString(cfg, "mode"), "observe")
}

func journal(sid, event, action, reason string, extra object) {
	entry := object{"at": float64(nowSec()), "sid": sid, "event": event, "action": action, "reason": reason}
	for key, value := range extra {
		entry[key] = value
	}
	if observing {
		entry["observe"] = true
	}
	ensureDir(files.guardDir)
	if err := appendRotating(files.decisions, string(marshalCompact(entry))+"\n"); err != nil {
		warn("decision journal write failed: %v", err)
	}
}

func usageFacts(usage usageView) object {
	facts := object{}
	if usage.fiveHour != nil {
		facts["five"] = usage.fiveHour.used
	}
	if usage.sevenDay != nil {
		facts["week"] = usage.sevenDay.used
	}
	if usage.fable != nil {
		facts["scoped"] = usage.fable.used
	}
	return facts
}

func observed(sid, event, action, reason string, extra object) bool {
	if !observing {
		return false
	}
	journal(sid, event, "would-"+action, reason, extra)
	logInfo("[observe] %s: would %s (%s)", event, action, reason)
	return true
}

type whyLine struct {
	at    float64
	raw   string
	entry object
}

func whyLines(count int) []whyLine {
	merged := []whyLine{}
	last := 0.0
	for _, line := range tailFileLines(files.decisions, count) {
		var entry object
		if jsonUnmarshalObject([]byte(line), &entry) != nil {
			entry = nil
		}
		last = numberOr(entry, "at", last)
		merged = append(merged, whyLine{at: last, raw: line, entry: entry})
	}
	for _, compaction := range leanCompactions(count) {
		entry := compactionJournalEntry(compaction)
		merged = append(merged, whyLine{at: numberOr(entry, "at", 0), raw: string(marshalCompact(entry)), entry: entry})
	}
	sort.SliceStable(merged, func(a, b int) bool { return merged[a].at < merged[b].at })
	if len(merged) > count {
		merged = merged[len(merged)-count:]
	}
	return merged
}

func runWhy() {
	count := 20
	if value, ok := toNumber(flagString("last")); ok && value >= 1 {
		count = int(value)
	}
	lines := whyLines(count)
	if len(lines) == 0 {
		fmt.Println(T("why.empty"))
		return
	}
	if args.present["json"] {
		for _, line := range lines {
			fmt.Println(line.raw)
		}
		return
	}
	for _, line := range lines {
		entry := line.entry
		if entry == nil {
			continue
		}
		facts := []string{}
		for _, key := range []string{"five", "week", "scoped", "ctx"} {
			if value, ok := getNumber(entry, key); ok {
				facts = append(facts, fmt.Sprintf("%s=%s%%", key, formatNumber(value)))
			}
		}
		factText := ""
		if len(facts) > 0 {
			factText = " [" + strings.Join(facts, " ") + "]"
		}
		fmt.Fprintf(os.Stdout, "%s  %-9s %-16s %-22s %s%s\n", formatTime(numberOr(entry, "at", 0)), shortSid(getString(entry, "sid")), getString(entry, "event"), getString(entry, "action"), getString(entry, "reason"), factText)
	}
}
