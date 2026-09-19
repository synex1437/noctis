package main

import (
	"fmt"
	"os"
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

func runWhy() {
	count := 20
	if value, ok := toNumber(flagString("last")); ok && value >= 1 {
		count = int(value)
	}
	lines := tailFileLines(files.decisions, count)
	if len(lines) == 0 {
		fmt.Println(T("why.empty"))
		return
	}
	if args.present["json"] {
		fmt.Println(strings.Join(lines, "\n"))
		return
	}
	for _, line := range lines {
		var entry object
		if err := jsonUnmarshalObject([]byte(line), &entry); err != nil || entry == nil {
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
