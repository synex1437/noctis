package main

import (
	"math"
	"path/filepath"
	"sort"
)

// What the ticked items of a queue took. At each stop that finds items of a queue file ticked since the
// note before (noteQueuePace), noctis notes how large the session's context was at the tick, how many
// compactions and continues without progress the items went through, how long they took, the setup
// that did them and the commit the repository was at. noctis queue status gives the medians and --json
// each record, so whether items finish in a small context is read from the user's own queue rather than
// guessed. Nothing leaves the machine.

const (
	// itemTicksKept bounds the tick records a queue file keeps.
	itemTicksKept = 50
	// itemTicksSummed is how many tick records queue status needs before it sums them up.
	itemTicksSummed = 3
)

// tickFacts is what the session is like at a stop that finds items ticked: its context in tokens (-1
// when its status line reported none), the continues without progress it went through and the commit
// its repository is at ("" when noctis cannot read it without starting git).
type tickFacts struct {
	tokens, idle float64
	head         string
}

// tickFactsOf reads the tick facts of session sid on the queue file at path. It reads files, so it runs
// before state is written.
func tickFactsOf(state object, sid, path string) tickFacts {
	facts := tickFacts{tokens: -1, idle: numberOr(getMap(getMap(state, "stopGuard"), sid), "idle", 0)}
	if tokens, known := sessionContextTokens(sid); known {
		facts.tokens = tokens
	}
	if head, ok := gitHeadFromFiles(filepath.Dir(path)); ok && len(head) >= 12 {
		facts.head = head[:12]
	}
	return facts
}

// tickRecord is one stop that found items ticked.
type tickRecord struct {
	at, items, seconds, tokens, compactions, idle float64
	setup, head                                   string
}

// noteTicks records the items of the queue file at path ticked between the pace notes before and
// after, with the facts of the session that ticked them. The compactions counted on an item now ticked
// go with it, and are not counted again.
func noteTicks(state object, path, content string, before, after paceNote, setup string, facts tickFacts, now int64) {
	ticked := after.done - before.done
	if ticked <= 0 {
		return
	}
	compactions, key := 0.0, queueTrustKey(path)
	if counted := getMap(getMap(state, "queueCompactions"), key); counted != nil {
		entries, _ := parseQueueEntries(content)
		for _, entry := range entries {
			if entry.checked && entry.text != "" && queueItemDigest(entry.text) == getString(counted, "item") {
				compactions = numberOr(counted, "count", 0)
				delete(getMap(state, "queueCompactions"), key)
				break
			}
		}
	}
	record := queueModelsRecord(state, path, now)
	ticks := append(getList(record, "ticks"), []any{float64(now), ticked, math.Max(0, after.at-before.at), facts.tokens, compactions, facts.idle, setup, facts.head})
	record["ticks"] = ticks[max(0, len(ticks)-itemTicksKept):]
}

// ticksOf is the tick records of the queue file at path, oldest first.
func ticksOf(state object, path string) []tickRecord {
	records := []tickRecord{}
	for _, raw := range getList(queueModelsOf(state, path), "ticks") {
		values, ok := raw.([]any)
		if !ok || len(values) != 8 {
			continue
		}
		numbers, valid := [6]float64{}, true
		for index := range numbers {
			if numbers[index], valid = toNumber(values[index]); !valid {
				break
			}
		}
		setup, _ := values[6].(string)
		head, _ := values[7].(string)
		if valid && numbers[1] > 0 {
			records = append(records, tickRecord{at: numbers[0], items: numbers[1], seconds: numbers[2], tokens: numbers[3], compactions: numbers[4], idle: numbers[5], setup: setup, head: head})
		}
	}
	return records
}

// medianOf is the median of values, which it sorts; values is not empty.
func medianOf(values []float64) float64 {
	sort.Float64s(values)
	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return (values[middle-1] + values[middle]) / 2
}

// ticksLine sums up the tick records of the queue file at path for noctis queue status: how many items
// they cover, the context at the tick (median and most) and the compactions and continues without
// progress along the way. It is "" while there are fewer than itemTicksSummed records.
func ticksLine(state object, path string) string {
	records := ticksOf(state, path)
	if len(records) < itemTicksSummed {
		return ""
	}
	tokens := []float64{}
	items, compactions, idle := 0.0, 0.0, 0.0
	for _, record := range records {
		items, compactions, idle = items+record.items, compactions+record.compactions, idle+record.idle
		if record.tokens >= 0 {
			tokens = append(tokens, record.tokens)
		}
	}
	if len(tokens) == 0 {
		return T("queue.ticksNoContext", int(items), int(compactions), int(idle))
	}
	most := 0.0
	for _, value := range tokens {
		most = math.Max(most, value)
	}
	return T("queue.ticks", int(items), approxCount(medianOf(tokens)), approxCount(most), int(compactions), int(idle))
}

// tickFactsList is the tick records of the queue file at path, for noctis queue status --json.
func tickFactsList(state object, path string) []any {
	list := []any{}
	for _, record := range ticksOf(state, path) {
		entry := object{"at": record.at, "items": record.items, "seconds": record.seconds, "compactions": record.compactions, "idle": record.idle, "setup": record.setup}
		if record.tokens >= 0 {
			entry["contextTokens"] = record.tokens
		}
		if record.head != "" {
			entry["head"] = record.head
		}
		list = append(list, entry)
	}
	return list
}
