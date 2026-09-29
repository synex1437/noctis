package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A deferred item stays in the queue file unticked, as it was, but Claude does not take it until the
// deferral ends: it waits on something outside the session, such as an account, a key or an answer.
// noctis queue defer records it in noctis's state with the reason the user hears when the queue
// stops on it, so the file and its trust stay as they are. The items that wait for it stay blocked.

type deferral struct {
	reason string
	until  float64 // 0 holds until noctis queue undefer
}

// deferredItem is an open item a deferral holds back, as a queue view names it.
type deferredItem struct {
	text   string
	reason string
	until  float64
}

// queueDeferrals are the live deferrals of the queue at path by item digest. state may be nil; it
// is read then.
func queueDeferrals(state object, path string) map[string]deferral {
	if state == nil {
		state = peekState()
	}
	records := getMap(getMap(state, "queueDefer"), queueTrustKey(path))
	if len(records) == 0 {
		return nil
	}
	now := float64(nowSec())
	live := map[string]deferral{}
	for digest, raw := range records {
		record, _ := raw.(object)
		if record == nil {
			continue
		}
		if until := numberOr(record, "until", 0); until <= 0 || until > now {
			live[digest] = deferral{reason: getString(record, "reason"), until: until}
		}
	}
	return live
}

// deferredNames names a view's deferred items with their reasons, for Claude (note) and the user
// (notice).
func deferredNames(view queueView) (note, notice string) {
	shown := []string{}
	for _, item := range view.deferredItems[:min(len(view.deferredItems), queueUnmatchedNamed)] {
		why := truncateText(item.reason, 80)
		if item.until > 0 {
			why += ", " + T("queue.deferredUntil", formatTime(item.until))
		}
		shown = append(shown, fmt.Sprintf(`"%s" (%s)`, truncateText(item.text, 80), why))
	}
	note = strings.Join(shown, "; ")
	notice = note
	if more := view.deferred - len(shown); more > 0 {
		note, notice = fmt.Sprintf("%s and %d more", note, more), T("queue.unmatchedMore", note, more)
	}
	return note, notice
}

// deferredRule tells Claude about the deferred items of the queue at path, or is "" when it has
// none.
func deferredRule(view queueView, path string) string {
	if view.deferred == 0 {
		return ""
	}
	return fmt.Sprintf(" %d item(s) are deferred (%s queue status names them and why): skip them and leave them unticked; when the user says what one waits on is ready, run %s queue undefer <a unique part of its text> --file %s and do it.", view.deferred, pluginName, pluginName, shellQuote(path))
}

// queueDeferHint tells Claude how to set an item aside that waits on something outside the session,
// so the queue goes on instead of stalling on it.
func queueDeferHint(path string) string {
	return fmt.Sprintf(` If an item cannot be done now because it waits on something outside this session (an account, a key, a file or an answer only the user can give), do not tick it and do not stall on it: run %s queue defer <a unique part of its text> --reason "<what it waits on>" --file %s and go on with the next item; the user hears the reason when the queue stops on it.`, pluginName, shellQuote(path))
}

// deferralDigest names the set of live deferrals of a view, so a notice about them repeats only
// when the set changes.
func deferralDigest(view queueView) string {
	texts := []string{}
	for _, item := range view.deferredItems {
		texts = append(texts, item.text+"\x00"+item.reason)
	}
	sort.Strings(texts)
	return queueItemDigest(strings.Join(texts, "\n"))[:12]
}

// parseDeferUntil reads --until: a duration from now (90m, 2h, 3d) or a time (RFC 3339).
func parseDeferUntil(text string, now time.Time) (float64, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, true
	}
	if days, found := strings.CutSuffix(text, "d"); found {
		if count, err := strconv.ParseFloat(days, 64); err == nil && count > 0 {
			return float64(now.Add(time.Duration(count * 24 * float64(time.Hour))).Unix()), true
		}
	}
	if span, err := time.ParseDuration(text); err == nil && span > 0 {
		return float64(now.Add(span).Unix()), true
	}
	if moment, err := time.Parse(time.RFC3339, text); err == nil && moment.After(now) {
		return float64(moment.Unix()), true
	}
	return 0, false
}

// matchQueueItems finds the open items of content a reference names: an item number (as (after N)
// counts them), a #tag, or a part of the text that only one open item has.
func matchQueueItems(content, reference string) ([]queueEntry, []queueEntry) {
	entries, _ := parseQueueEntries(content)
	reference = strings.TrimSpace(reference)
	open := []queueEntry{}
	for _, entry := range entries {
		if !entry.checked && entry.text != "" {
			open = append(open, entry)
		}
	}
	if number, err := strconv.Atoi(reference); err == nil {
		for _, entry := range entries {
			if entry.ordinal == number {
				return []queueEntry{entry}, open
			}
		}
		return nil, open
	}
	matched := []queueEntry{}
	if tag, found := strings.CutPrefix(reference, "#"); found && tag != "" {
		for _, entry := range open {
			if entry.tags[foldTag(tag)] {
				matched = append(matched, entry)
			}
		}
		return matched, open
	}
	lower := strings.ToLower(reference)
	for _, entry := range open {
		if lower != "" && strings.Contains(strings.ToLower(entry.text), lower) {
			matched = append(matched, entry)
		}
	}
	return matched, open
}

// runQueueDefer is noctis queue defer and undefer. Claude may run both: a deferral only narrows what
// the queue asks of it, and the user hears of every one when the queue stops on it.
func runQueueDefer(target, action string) {
	content, _ := readQueueText(target)
	label := filepath.Base(target)
	key := queueTrustKey(target)
	if action == "undefer" && args.present["all"] {
		updateState(func(next object) { delete(stateMap(next, "queueDefer"), key) })
		journal("", "cli", "undefer", label+": all", nil)
		fmt.Println(T("queue.undeferredAll", label))
		return
	}
	// The words after the action are one reference, so an unquoted part of an item's text works too.
	reference := ""
	if len(args.positional) > 2 {
		reference = strings.TrimSpace(strings.Join(args.positional[2:], " "))
	}
	if reference == "" {
		fmt.Fprintln(os.Stderr, T("queue.usage"))
		os.Exit(2)
	}
	matched, _ := matchQueueItems(content, reference)
	if action == "undefer" {
		// Only a deferred item can be taken up again, so a reference that also matches items that
		// are not deferred still names the one that is.
		deferrals := queueDeferrals(nil, target)
		held := []queueEntry{}
		for _, entry := range matched {
			if _, found := deferrals[queueItemDigest(entry.text)]; found {
				held = append(held, entry)
			}
		}
		if len(held) == 0 && len(matched) > 0 {
			named := `"` + reference + `"`
			if len(matched) == 1 {
				named = truncateText(matched[0].text, 120)
			}
			fmt.Fprintln(os.Stderr, T("queue.notDeferred", label, printableItem(named)))
			os.Exit(1)
		}
		matched = held
	}
	if len(matched) == 0 {
		fmt.Fprintln(os.Stderr, T("queue.deferNoMatch", label, printableItem(reference)))
		os.Exit(1)
	}
	if len(matched) > 1 && !strings.HasPrefix(reference, "#") {
		names := []string{}
		for _, entry := range matched[:min(len(matched), queueUnmatchedNamed)] {
			names = append(names, fmt.Sprintf("%d: %s", entry.ordinal, printableItem(truncateText(entry.text, 80))))
		}
		fmt.Fprintln(os.Stderr, T("queue.deferAmbiguous", label, printableItem(reference), len(matched), strings.Join(names, "; ")))
		os.Exit(1)
	}
	if action == "undefer" {
		updateState(func(next object) {
			records := getMap(stateMap(next, "queueDefer"), key)
			for _, entry := range matched {
				delete(records, queueItemDigest(entry.text))
			}
			if len(records) == 0 {
				delete(stateMap(next, "queueDefer"), key)
			}
		})
		for _, entry := range matched {
			journal("", "cli", "undefer", truncateText(entry.text, 200), object{"file": label})
			fmt.Println(T("queue.undeferred", label, printableItem(truncateText(entry.text, 120))))
		}
		return
	}
	reason := strings.TrimSpace(flagString("reason"))
	if reason == "" {
		fmt.Fprintln(os.Stderr, T("queue.deferReason", pluginName))
		os.Exit(2)
	}
	until, ok := parseDeferUntil(flagString("until"), time.Unix(nowSec(), 0))
	if !ok {
		fmt.Fprintln(os.Stderr, T("queue.deferUntilBad", printableItem(flagString("until"))))
		os.Exit(2)
	}
	// A #tag names a group: its (human) items are the user's already and stay out of the deferral.
	// An item named alone must be one Claude could take.
	if strings.HasPrefix(reference, "#") {
		own := []queueEntry{}
		for _, entry := range matched {
			if !entry.human {
				own = append(own, entry)
			}
		}
		if len(own) > 0 {
			matched = own
		}
	}
	for _, entry := range matched {
		switch {
		case entry.checked:
			fmt.Fprintln(os.Stderr, T("queue.deferDone", label, printableItem(truncateText(entry.text, 120))))
			os.Exit(1)
		case entry.human:
			fmt.Fprintln(os.Stderr, T("queue.deferHuman", label, printableItem(truncateText(entry.text, 120))))
			os.Exit(1)
		}
	}
	now := float64(nowSec())
	updateState(func(next object) {
		records := getMap(stateMap(next, "queueDefer"), key)
		if records == nil {
			records = object{}
			stateMap(next, "queueDefer")[key] = records
		}
		for _, entry := range matched {
			record := object{"text": truncateText(entry.text, 300), "reason": truncateText(reason, 300), "at": now}
			if until > 0 {
				record["until"] = until
			}
			records[queueItemDigest(entry.text)] = record
		}
	})
	for _, entry := range matched {
		journal("", "cli", "defer", truncateText(entry.text, 200), object{"file": label, "reason": truncateText(reason, 200), "until": until})
		logInfo("queue item deferred in %s: %q (%s)", target, truncateText(entry.text, 120), truncateText(reason, 120))
		shown := printableItem(truncateText(reason, 200))
		if until > 0 {
			shown += ", " + T("queue.deferredUntil", formatTime(until))
		}
		fmt.Println(T("queue.deferredItem", label, printableItem(truncateText(entry.text, 120)), shown))
	}
}

// printDeferrals names the deferred items of the queue at target.
func printDeferrals(target string, view queueView) {
	if view.deferred > 0 {
		_, names := deferredNames(view)
		fmt.Println(T("queue.deferredStatus", filepath.Base(target), view.deferred, printableItem(names)))
	}
}

// pruneDeferrals drops the deferrals that ended, and any older than 90 days: the item they held may
// have been reworded or removed since, and nothing else would clear them.
func pruneDeferrals(state object, now int64) {
	for key, raw := range stateMap(state, "queueDefer") {
		records, _ := raw.(object)
		for digest, entry := range records {
			record, _ := entry.(object)
			until := numberOr(record, "until", 0)
			if record == nil || (until > 0 && until <= float64(now)) || float64(now)-numberOr(record, "at", 0) > 90*86400 {
				delete(records, digest)
			}
		}
		if len(records) == 0 {
			delete(stateMap(state, "queueDefer"), key)
		}
	}
}
