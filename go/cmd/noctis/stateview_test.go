package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime/debug"
	"strings"
	"testing"
)

// Every read checks that the parses kept for the next read still hold what their bytes say, so a
// test that runs code changing a map peekState lent it, or a kept write that differs from the
// parse of its bytes, fails here with the file named.
func init() {
	sharedParseCheck = func(file string, parsed parsedFile) {
		var fresh any
		if json.Unmarshal(bytes.TrimPrefix(parsed.raw, utf8BOM), &fresh) != nil {
			return
		}
		want, _ := fresh.(object)
		if !reflect.DeepEqual(want, parsed.data) {
			panic(fmt.Sprintf("the kept parse of %s no longer matches its bytes; some code changed a map it was only lent:\nkept  %s\nbytes %s", file, marshalCompact(parsed.data), parsed.raw))
		}
	}
}

func mapIdentity(value object) uintptr {
	return reflect.ValueOf(value).Pointer()
}

func TestPeekStateLendsTheKeptParseAndReadStateCopiesIt(t *testing.T) {
	sandboxFiles(t)
	updateState(func(state object) {
		stateMap(state, "sessionLocale")["s1"] = object{"lang": "tr", "at": float64(nowSec())}
	})
	first, second := peekState(), peekState()
	if mapIdentity(getMap(first, "sessionLocale")) != mapIdentity(getMap(second, "sessionLocale")) {
		t.Fatal("two peeks at the same state.json copied its maps; peekState should lend the kept parse")
	}
	owned := readState()
	if mapIdentity(getMap(owned, "sessionLocale")) == mapIdentity(getMap(first, "sessionLocale")) {
		t.Fatal("readState handed out the kept parse itself; its caller may change it")
	}
	getMap(owned, "sessionLocale")["s1"] = object{"lang": "de"}
	delete(getMap(owned, "sessionLocale"), "s1")
	if sessionLanguage(peekState(), "s1") != "tr" {
		t.Fatal("changing what readState returned changed what the next peek sees")
	}
	for key := range emptyState() {
		if _, present := first[key]; !present {
			t.Fatalf("peekState left out %q; it must have the shape readState has", key)
		}
	}
}

func TestPeekStateOnAMissingOrUnreadableFileIsTheEmptyState(t *testing.T) {
	sandboxFiles(t)
	if !reflect.DeepEqual(peekState(), emptyState()) {
		t.Fatalf("with no state.json peekState gave %v", peekState())
	}
	if err := os.WriteFile(files.state, []byte(`[1, 2]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := peekState(); getMap(got, "waits") == nil {
		t.Fatalf("a state.json that is not an object gave %v", got)
	}
}

func TestAWriteKeepsWhatParsingItsBytesGivesBack(t *testing.T) {
	sandboxFiles(t)
	// sessionLocale keeps any entry with a recent "at", so the odd values stay through pruneState.
	returned := updateState(func(state object) {
		stateMap(state, "sessionLocale")["odd"] = object{
			"at":     nowSec(),
			"count":  7,
			"big":    int64(1) << 60,
			"names":  []string{"a", "b"},
			"broken": "caf\xe9",
			"nested": object{"list": []any{float64(1), "x", nil, object{}}, "none": nil},
			"nilMap": object(nil),
			"html":   "<b>&</b>\u2028",
		}
	})
	raw, err := os.ReadFile(files.state)
	if err != nil {
		t.Fatal(err)
	}
	cached, seen := parsedEntry(files.state)
	if !seen || !bytes.Equal(cached.raw, raw) {
		t.Fatal("the write did not keep its bytes for the next read")
	}
	var fresh any
	if err := json.Unmarshal(raw, &fresh); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fresh, any(cached.data)) {
		t.Fatalf("the kept write differs from the parse of its bytes:\nkept  %s\nparse %s", marshalCompact(cached.data), marshalCompact(fresh))
	}
	odd := getMap(getMap(peekState(), "sessionLocale"), "odd")
	if odd["count"] != float64(7) || odd["big"] != float64(int64(1)<<60) || odd["broken"] != "caf\ufffd" {
		t.Fatalf("values read back after a write are not what a parse gives: %#v %#v %#v", odd["count"], odd["big"], odd["broken"])
	}
	if names, ok := odd["names"].([]any); !ok || len(names) != 2 {
		t.Fatalf("a []string read back after a write is %#v; a parse gives []any", odd["names"])
	}
	getMap(getMap(returned, "sessionLocale"), "odd")["count"] = "changed after the write"
	if getMap(getMap(peekState(), "sessionLocale"), "odd")["count"] != float64(7) {
		t.Fatal("changing the state updateState returned changed the kept parse")
	}
}

func TestAStateWriteNeedsNoParseToBeReadBack(t *testing.T) {
	sandboxFiles(t)
	updateState(func(state object) {
		stateMap(state, "sessionLocale")["s1"] = object{"lang": "tr", "at": float64(nowSec())}
	})
	kept, _ := parsedEntry(files.state)
	peeked := peekState()
	if mapIdentity(getMap(peeked, "sessionLocale")) != mapIdentity(getMap(kept.data, "sessionLocale")) {
		t.Fatal("reading state.json right after a write parsed it again instead of using what the write kept")
	}
}

// The status line clears a dead hand-off on every refresh, from the state it only looks at
// otherwise: the clearing has to happen on a copy, or the kept parse it was lent changes with it.
func TestAStatusLineRepairClearsADeadHandOffFromACopyOfItsOwn(t *testing.T) {
	sandboxFiles(t)
	now := float64(nowSec())
	// Written as a hand-off is left behind when its relaunch process dies after the last write:
	// updateState would prune it on the way in.
	if err := writeJSONAtomic(files.state, object{"handedOff": object{
		"gone": object{"at": now - handoffGraceSeconds - 60, "pid": float64(deadPid(t)), "model": "claude-opus-5"},
		"live": object{"at": now, "pid": float64(os.Getpid()), "model": "claude-opus-5"},
	}}); err != nil {
		t.Fatal(err)
	}
	lent := peekState()
	if getMap(getMap(lent, "handedOff"), "gone") == nil {
		t.Fatal("the dead hand-off did not reach state.json")
	}
	repairOrphanWaits()
	if getMap(getMap(lent, "handedOff"), "gone") == nil {
		t.Fatal("the repair cleared the dead hand-off from the state peekState had lent")
	}
	stored := getMap(peekState(), "handedOff")
	if stored["gone"] != nil || stored["live"] == nil {
		t.Fatalf("after the repair state.json holds the hand-offs %v; want the live one only", stored)
	}
	if journaledCount("gone", "handoff-gone") != 1 {
		t.Fatal("the dead hand-off was cleared without a journal entry")
	}
}

func TestAWriteThatCouldNotBeEncodedKeepsNothing(t *testing.T) {
	sandboxFiles(t)
	// A value that cannot be encoded writes nothing, so an empty usage.json (a write cut short, a
	// full disk) must still read as broken afterwards.
	if err := os.WriteFile(files.usage, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	keepWritten(files.usage, nil, object{"five": float64(12)})
	if read := readJSONShared(files.usage); read.ok {
		t.Fatalf("an empty usage.json read back as a value that was never written: %v", read.data)
	}
}

func TestKnownLocalesAreTheCatalogs(t *testing.T) {
	built := baseCatalog()
	for code := range built {
		if !baseLocales[code] {
			t.Fatalf("baseCatalog builds %q but baseLocales does not list it, so knownLocale refuses it", code)
		}
	}
	for code := range baseLocales {
		if built[code] == nil {
			t.Fatalf("baseLocales lists %q but baseCatalog has no such catalog", code)
		}
	}
	for code := range extraCatalogBuilders {
		if !knownLocale(code) {
			t.Fatalf("knownLocale(%q) = false for a built-in catalog", code)
		}
	}
	for _, code := range []string{"", "xx", "c.", "EN"} {
		if knownLocale(code) {
			t.Fatalf("knownLocale(%q) = true", code)
		}
	}
}

func TestSafeNameMatchesItsCharacterClass(t *testing.T) {
	pattern := regexp.MustCompile(`[^A-Za-z0-9_.-]`)
	regexpName := func(text string) string {
		value := pattern.ReplaceAllString(text, "_")
		if strings.Trim(value, ".") == "" {
			value = strings.Repeat("_", len(value))
		}
		if len(value) > 80 {
			value = value[:72] + "-" + hashKey(text)
		}
		return value
	}
	for _, text := range []string{
		"", "abc", "0c3f9d2e-6b1a-4c5e-9f00-1234567890ab", "a/b\\c:d", "şükrü ğ", "日本語", "..", ".", "...a",
		"bad\xff\xfebytes", "\xe2\x82", "tab\tnew\nline", "emoji 🙂 x", strings.Repeat("é", 60), strings.Repeat("a", 81),
		"__proto__", "UPPER_lower.dot-dash",
	} {
		if got, want := safeName(text), regexpName(text); got != want {
			t.Fatalf("safeName(%q) = %q; the character class gives %q", text, got, want)
		}
	}
}

func TestTheSessionLocaleIsSettledByTheFirstTextAndNotBefore(t *testing.T) {
	sandboxFiles(t)
	previous := locale
	t.Cleanup(func() { locale = previous; settleLocaleLater(nil) })
	t.Setenv("NOCTIS_LANG", "")
	locale = "en"
	updateState(func(state object) {
		stateMap(state, "sessionLocale")["s1"] = object{"lang": "tr", "at": float64(nowSec())}
	})
	settled := 0
	settleLocaleLater(func() {
		settled++
		applySessionLocale(object{"locale": "auto"}, peekState(), "s1")
	})
	if locale != "en" || settled != 0 {
		t.Fatal("the session locale was settled before any text was looked up")
	}
	if got := localeNow(); got != "tr" {
		t.Fatalf("the first lookup gave locale %q; want the session's tr", got)
	}
	_ = T("win.week")
	if settled != 1 {
		t.Fatalf("the session locale was settled %d times; want once", settled)
	}
	settleLocaleLater(func() { locale = "de" })
	settleLocaleLater(nil)
	if localeNow() != "tr" {
		t.Fatal("a dropped settle still ran")
	}
}

func TestSessionLocaleIsWantedOnlyWhenNothingNamesOne(t *testing.T) {
	t.Setenv("NOCTIS_LANG", "")
	for cfgLocale, want := range map[string]bool{"": true, "auto": true, " AUTO ": true, "tr": false, "en": false} {
		if got := sessionLocaleWanted(object{"locale": cfgLocale}); got != want {
			t.Fatalf("sessionLocaleWanted(locale %q) = %v; want %v", cfgLocale, got, want)
		}
	}
	t.Setenv("NOCTIS_LANG", "de")
	if sessionLocaleWanted(object{"locale": "auto"}) {
		t.Fatal("NOCTIS_LANG names the locale, yet the session's language was still wanted")
	}
}

func claudeToolPayload(event, sid, cwd, tool string, toolInput object) object {
	return object{"hook_event_name": event, "session_id": sid, "cwd": cwd, "tool_name": tool, "tool_input": toolInput, "transcript_path": filepath.Join(cwd, "transcript.jsonl")}
}

func stateParsed() bool {
	_, seen := parsedEntry(files.state)
	return seen
}

func TestHooksThatHaveNothingToSayLeaveTheStateUnread(t *testing.T) {
	_, project := agentSessionSandbox(t, 20)
	t.Setenv("NOCTIS_LANG", "")
	updateState(func(state object) {
		stateMap(state, "sessionLocale")["quiet-1"] = object{"lang": "tr", "at": float64(nowSec())}
	})
	for _, payload := range []object{
		claudeToolPayload("PreToolUse", "quiet-1", project, "Edit", object{"file_path": filepath.Join(project, "main.go"), "old_string": "a", "new_string": "b"}),
		claudeToolPayload("PreToolUse", "quiet-1", project, "MultiEdit", object{"file_path": "notes.md", "edits": []any{}}),
		claudeToolPayload("PreToolUse", "quiet-1", project, "Write", object{"file_path": filepath.Join(project, "new.txt"), "content": "x"}),
		claudeToolPayload("PreToolUse", "quiet-1", project, "WebSearch", object{"query": "go maps"}),
		claudeToolPayload("PreToolUse", "quiet-1", project, "WebFetch", object{"url": "https://example.com"}),
	} {
		dropParsed(files.state)
		if output := hostHook(t, "claude", payload); permissionOf(output) == "deny" {
			t.Fatalf("%s was denied: %v", getString(payload, "tool_name"), output)
		}
		if stateParsed() {
			t.Fatalf("a %s of %v read state.json though the hook had nothing to decide from it", getString(payload, "tool_name"), getMap(payload, "tool_input"))
		}
	}
}

func TestAnEditOfTheSessionChecklistStillReadsTheState(t *testing.T) {
	_, project := agentSessionSandbox(t, 20)
	checklist := writeSessionQueue("list-1", object{"cwd": project, "at": float64(nowSec()), "items": float64(1), "words": jobWordDigests("fix the login bug")}, []string{"- [ ] fix the login bug"})
	if checklist == "" {
		t.Fatal("the checklist was not written")
	}
	dropParsed(files.state)
	output := hostHook(t, "claude", claudeToolPayload("PreToolUse", "list-1", project, "Edit", object{"file_path": checklist, "old_string": "- [ ] fix the login bug", "new_string": "- [ ] fix the login bug\n- [ ] rewrite the billing module"}))
	if !stateParsed() {
		t.Fatal("an edit of the session's own checklist did not read the state to check the jobs it adds")
	}
	if permissionOf(output) != "deny" {
		t.Fatalf("a job the prompt never named went into the checklist: %v", output)
	}
}

func TestWithTheRouterOffAResearchToolIsNotHeldByAnOldRoute(t *testing.T) {
	cfg, project := agentSessionSandbox(t, 20)
	if getBool(section(cfg, "router"), "enabled", false) {
		t.Fatal("the shipped config turns the router on; this test needs it off")
	}
	updateState(func(state object) {
		stateMap(state, "routes")["route-1"] = object{"at": float64(nowSec()), "denies": float64(0), "signal": "research"}
	})
	if output := hookOutput(t, onPreToolUse, claudeToolPayload("PreToolUse", "route-1", project, "WebSearch", object{"query": "x"}), cfg); permissionOf(output) == "deny" {
		t.Fatalf("with the router off a route left in the state still denied WebSearch: %v", output)
	}
	router := section(cfg, "router")
	router["enabled"] = true
	cfg["router"] = router
	if output := hookOutput(t, onPreToolUse, claudeToolPayload("PreToolUse", "route-1", project, "WebSearch", object{"query": "x"}), cfg); permissionOf(output) != "deny" {
		t.Fatalf("with the router on the routed prompt's WebSearch went ahead: %v", output)
	}
}

func TestAStatusLineWithNothingNewWritesNothing(t *testing.T) {
	sandboxFiles(t)
	input := object{"session_id": "line-1", "cwd": t.TempDir(), "model": object{"id": "claude-opus-5-5"}, "version": "2.1.300",
		"rate_limits": object{"five_hour": object{"used_percentage": float64(12), "resets_at": float64(nowSec() + 3600)}}}
	now := nowSec()
	recordStatusline(input, now, true)
	before, err := os.Stat(files.usage)
	if err != nil {
		t.Fatal(err)
	}
	recordStatusline(input, now, true)
	after, err := os.Stat(files.usage)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("a status line refresh with nothing new rewrote usage.json")
	}
	if _, err := os.Stat(files.usageBackup); err == nil {
		t.Fatal("a refresh with nothing new wrote a usage.json backup")
	}
	input["rate_limits"] = object{"five_hour": object{"used_percentage": float64(13), "resets_at": float64(nowSec() + 3600)}}
	recordStatusline(input, now, true)
	changed, err := os.Stat(files.usage)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(after, changed) {
		t.Fatal("a refresh with a new reading did not write usage.json")
	}
	if used := numberOr(getMap(readJSON(files.usage), "five_hour"), "used", 0); used != 13 {
		t.Fatalf("usage.json holds %v%%; want the new 13%%", used)
	}
}

func TestAWaitPutsTheDefaultCollectorBack(t *testing.T) {
	previous := debug.SetGCPercent(shortLivedGCPercent)
	t.Cleanup(func() { debug.SetGCPercent(previous); collectorTuned = false })
	collectorTuned = true
	sleepUntilPaced(0, func(float64) float64 { return 1 }, nil)
	if got := debug.SetGCPercent(previous); got != 100 {
		t.Fatalf("after a short-lived command started to wait, GOGC was %d; want the default 100", got)
	}
	if collectorTuned {
		t.Fatal("the collector still counts as tuned after it was put back")
	}
}
