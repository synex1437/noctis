package main

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

var observing bool

func setMode(cfg object) {
	observing = strings.EqualFold(getString(cfg, "mode"), "observe")
}

func journal(sid, event, action, reason string, extra object) {
	if sid == selftestSession {
		return
	}
	entry := object{"at": float64(nowSec()), "sid": sid, "event": event, "action": action, "reason": reason}
	for key, value := range extra {
		if _, own := entry[key]; !own {
			entry[key] = value
		}
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
	lines := tailFileLines(files.decisions, count)
	if len(lines) < count {
		lines = nil
		for _, file := range []string{files.decisions + ".1", files.decisions} {
			content, _ := readFileShared(file)
			for _, line := range strings.Split(string(content), "\n") {
				if line != "" {
					lines = append(lines, line)
				}
			}
		}
		lines = lines[max(0, len(lines)-count):]
	}
	for _, line := range lines {
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

const whyMaxLast = 1e6

func runWhy() {
	if args.present["stats"] {
		runWhyStats()
		return
	}
	count := 20
	if value, ok := toNumber(flagString("last")); ok && value >= 1 && !math.IsInf(value, 1) {
		count = int(math.Min(value, whyMaxLast))
	}
	lines := whyLines(count)
	if args.present["json"] {
		for _, line := range lines {
			fmt.Println(line.raw)
		}
		return
	}
	if len(lines) == 0 {
		fmt.Println(T("why.empty"))
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
		for _, key := range []string{"wakeAt", "dueAt", "checkAt"} {
			if value, ok := getNumber(entry, key); ok && value > 0 {
				facts = append(facts, key+"="+formatTime(value))
			}
		}
		factText := ""
		if len(facts) > 0 {
			factText = " [" + strings.Join(facts, " ") + "]"
		}
		fmt.Fprintf(os.Stdout, "%s  %-9s %-16s %-22s %s%s\n", formatTime(numberOr(entry, "at", 0)), shortSid(getString(entry, "sid")), getString(entry, "event"), getString(entry, "action"), getString(entry, "reason"), factText)
	}
}
