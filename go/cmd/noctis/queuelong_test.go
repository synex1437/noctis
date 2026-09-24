package main

import (
	"strings"
	"testing"
	"time"
)

func TestAQueueItemWithThousandsOfContinuationLinesIsReadInOnePass(t *testing.T) {
	var content strings.Builder
	content.WriteString("# q\n- [ ] (P1) migrate every endpoint #api (after 2)\n")
	for content.Len() < queueMaxBytes-128 {
		content.WriteString("  and keep the old routes working until the clients move\n")
	}
	content.WriteString("- [x] document the flags\n")
	started := time.Now()
	entries, plain := parseQueueEntries(content.String())
	elapsed := time.Since(started)
	if plain || len(entries) != 2 {
		t.Fatalf("parsed %d entries (plain %t), want the long item and the done one", len(entries), plain)
	}
	long := entries[0]
	if long.ordinal != 1 || long.priority != 1 || !long.tags["api"] || len(long.after) != 1 || long.after[0] != "2" ||
		!strings.HasPrefix(long.text, "(P1) migrate every endpoint #api (after 2) and keep the old routes") ||
		!strings.HasSuffix(long.text, "until the clients move") {
		t.Fatalf("the long item lost its annotations or its continuation text: ordinal %d, priority %d, tags %v, after %v", long.ordinal, long.priority, long.tags, long.after)
	}
	if done := entries[1]; done.ordinal != 2 || !done.checked || done.text != "document the flags" {
		t.Fatalf("the item after the long one is wrong: %+v", done)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("reading a 1 MiB queue item took %v; the hooks read the queue at every session start and stop", elapsed)
	}
}
