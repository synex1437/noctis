package main

import "testing"

func liveWait(seconds int64) object {
	return object{"kind": "batch", "window": "five_hour", "until": float64(nowSec() + seconds),
		"resumeAt": float64(nowSec() + seconds), "startedAt": float64(nowSec())}
}

func TestStateWriteReplacesTheFileUnderTheLock(t *testing.T) {
	sandboxFiles(t)
	updateState(func(state object) { stateMap(state, "waits")["a"] = liveWait(3600) })

	result := stateWriteResult(object{
		"document": object{"waits": object{"b": liveWait(7200)}},
		"expect":   fileStamp(files.state),
	})

	if !getBool(result, "ok", false) {
		t.Fatalf("the write was refused: %v", result)
	}
	waits := getMap(readState(), "waits")
	if getMap(waits, "b") == nil || getMap(waits, "a") != nil {
		t.Fatalf("the document did not replace the file: %v", waits)
	}
}

func TestStateWriteRefusesADocumentBuiltFromBytesThatMoved(t *testing.T) {
	sandboxFiles(t)
	updateState(func(state object) { state["hookCapSeconds"] = float64(120) })
	stale := fileStamp(files.state)
	updateState(func(state object) { state["hookCapSeconds"] = float64(300) })
	if fileStamp(files.state) == stale {
		t.Fatalf("the second write left the file byte-identical, so there is no race to refuse")
	}

	result := stateWriteResult(object{
		"document": object{"hookCapSeconds": float64(60)},
		"expect":   stale,
	})

	if getBool(result, "ok", false) {
		t.Fatalf("a write built from bytes the plugin has since replaced was accepted: %v", result)
	}
	if getString(result, "reason") != "conflict" {
		t.Fatalf("want a conflict the caller can retry, got %v", result)
	}
	if numberOr(readState(), "hookCapSeconds", 0) != 300 {
		t.Fatalf("the update made between the read and the write was lost anyway")
	}
}

func TestStateWriteWithoutAStampIsUnconditional(t *testing.T) {
	sandboxFiles(t)
	updateState(func(state object) { stateMap(state, "waits")["a"] = liveWait(3600) })

	result := stateWriteResult(object{"document": object{"waits": object{}}})

	if !getBool(result, "ok", false) {
		t.Fatalf("a document with nothing to lose was refused: %v", result)
	}
	if len(getMap(readState(), "waits")) != 0 {
		t.Fatalf("the waits survived an unconditional replacement")
	}
}

func TestStateWriteKeepsWhatTheStoreWouldHavePruned(t *testing.T) {
	sandboxFiles(t)
	aged := float64(nowSec() - 30*86400)
	result := stateWriteResult(object{"document": object{
		"waits":         object{"expired": object{"kind": "batch", "resumeAt": aged, "until": aged}},
		"routerLearned": object{"old": object{"at": aged}},
	}})

	if !getBool(result, "ok", false) {
		t.Fatalf("the write was refused: %v", result)
	}
	state := readState()
	if getMap(getMap(state, "waits"), "expired") == nil {
		t.Fatalf("the expired wait a scenario wants the plugin to prune later was pruned on the way in")
	}
	if getMap(getMap(state, "routerLearned"), "old") == nil {
		t.Fatalf("the aged router memory a scenario wants the plugin to prune later was pruned on the way in")
	}
}

func TestStateWriteRefusesAPayloadWithNoDocument(t *testing.T) {
	sandboxFiles(t)
	if result := stateWriteResult(object{"expect": "absent"}); getBool(result, "ok", false) {
		t.Fatalf("a payload with no document was accepted: %v", result)
	}
}
