package main

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

const deferQueue = "# q\n- [x] set up the repository\n- [ ] migrate the users table\n- [ ] connect the payment provider #pay\n- [ ] charge the first customer (after #pay)\n- [ ] write the release notes\n"

const deferReason = "waits for the provider's API key"

// deferSandbox is a project whose trusted TASKS.md holds deferQueue.
func deferSandbox(t *testing.T) (object, string, string, string) {
	t.Helper()
	cfg, project, frontend := queueCheckSandbox(t, "")
	// The session's effort comes from the config, not from the shell the tests run in.
	t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "")
	path := writeQueueFile(t, project, deferQueue)
	trustQueueFile(path, true)
	return cfg, project, frontend, path
}

// queueCommand runs noctis queue with argv in this process, as the command line would, and returns
// what it printed.
func queueCommand(t *testing.T, cfg object, project string, argv ...string) string {
	t.Helper()
	previous := args
	t.Cleanup(func() { args = previous })
	args = parseArgs(append([]string{"queue"}, argv...))
	return capturedStdout(t, func() { runQueueTrust(cfg, project, argv[0]) })
}

func deferTicked(items ...string) string {
	content := deferQueue
	for _, item := range items {
		content = strings.Replace(content, "- [ ] "+item, "- [x] "+item, 1)
	}
	return content
}

func TestADeferredItemIsSkippedAndWhatWaitsForItStaysBlocked(t *testing.T) {
	cfg, project, frontend, path := deferSandbox(t)
	printed := queueCommand(t, cfg, project, "defer", "payment", "--reason", deferReason)
	if want := T("queue.deferredItem", "TASKS.md", "connect the payment provider #pay", deferReason); !strings.Contains(printed, want) {
		t.Fatalf("noctis queue defer printed %q, want %q", printed, want)
	}
	view := queueSnapshot(path)
	if view.total != 3 || view.deferred != 1 || view.blocked != 1 || len(view.items) != 2 {
		t.Fatalf("the snapshot counts %d open, %d deferred, %d blocked, eligible %q; want 3, 1, 1 and the two items Claude can take", view.total, view.deferred, view.blocked, view.items)
	}
	output := stopHookOutput(t, stopInput("df1", frontend), cfg)
	reason := getString(output, "reason")
	if getString(output, "decision") != "block" || !strings.Contains(reason, `("migrate the users table")`) {
		t.Fatalf("the queue did not go on with the first item Claude can take: %v", output)
	}
	if !strings.Contains(reason, "1 item(s) are deferred") {
		t.Fatalf("the continuation did not tell Claude to leave the deferred item: %q", reason)
	}
	if content, _ := os.ReadFile(path); string(content) != deferQueue {
		t.Fatalf("deferring an item changed the queue file:\n%s", content)
	}
	if trusted, changed, _ := queueTrustGap(cfg, path); !trusted || len(changed) > 0 {
		t.Fatalf("deferring an item ended the file's trust (changed %q)", changed)
	}
}

func TestTheJournalNamesTheItemADeferralSetsAsideAndWhatItWaitsOn(t *testing.T) {
	cfg, project, _, _ := deferSandbox(t)
	queueCommand(t, cfg, project, "defer", "payment", "--reason", "waits for the Stripe account")
	if entry := journaledEntry("", "defer"); getString(entry, "reason") != "connect the payment provider #pay: waits for the Stripe account" || getString(entry, "waitsOn") != "waits for the Stripe account" {
		t.Fatalf("the journal entry of the deferral is %v, which does not name the item and what it waits on", entry)
	}
	defer func(previous parsedArgs) { args = previous }(args)
	args = parseArgs([]string{"why"})
	if printed := capturedStdout(t, runWhy); !strings.Contains(printed, "connect the payment provider #pay: waits for the Stripe account") {
		t.Fatalf("noctis why does not say which item was set aside and what it waits on:\n%s", printed)
	}
}

func TestTheQueueStopsOnDeferredItemsAndSaysWhyOnce(t *testing.T) {
	cfg, project, frontend, path := deferSandbox(t)
	queueCommand(t, cfg, project, "defer", "payment", "--reason", deferReason)
	writeQueueFile(t, project, deferTicked("migrate the users table", "write the release notes"))
	_, names := deferredNames(queueSnapshot(path))
	if want := `"connect the payment provider #pay" (` + deferReason + `)`; names != want {
		t.Fatalf("the deferred item is named %q, want %q", names, want)
	}
	output := stopHookOutput(t, stopInput("df2", frontend), cfg)
	if getString(output, "decision") == "block" {
		t.Fatalf("with only a deferred item and what waits for it left, the session was kept going: %v", output)
	}
	if got, want := getString(output, "systemMessage"), T("queue.deferredMessage", 1, "TASKS.md", names, pluginName); got != want {
		t.Fatalf("the stop said %q, want %q", got, want)
	}
	if again := stopHookOutput(t, stopAgain("df2", frontend), cfg); again != nil {
		t.Fatalf("the user was told about the same deferral a second time: %v", again)
	}
	if told := loggedTimes("notify: " + pluginName + " — " + T("queue.deferredNotify", 1, "TASKS.md", names)); told != 1 {
		t.Fatalf("the user was notified %d times of the deferred item, want once", told)
	}
	if done := loggedTimes(T("queue.doneNotify", "TASKS.md")); done != 0 {
		t.Fatal("a queue with a deferred item still open was announced as finished")
	}
}

func TestUndeferTakesTheItemUpAgain(t *testing.T) {
	cfg, project, frontend, _ := deferSandbox(t)
	queueCommand(t, cfg, project, "defer", "payment", "--reason", deferReason)
	writeQueueFile(t, project, deferTicked("migrate the users table", "write the release notes"))
	stopHookOutput(t, stopInput("df3", frontend), cfg)
	printed := queueCommand(t, cfg, project, "undefer", "payment", "provider")
	if want := T("queue.undeferred", "TASKS.md", "connect the payment provider #pay"); !strings.Contains(printed, want) {
		t.Fatalf("noctis queue undefer printed %q, want %q", printed, want)
	}
	output := stopHookOutput(t, stopInput("df3", frontend), cfg)
	if getString(output, "decision") != "block" || !strings.Contains(getString(output, "reason"), `("connect the payment provider #pay")`) {
		t.Fatalf("the item taken up again did not become Claude's next item: %v", output)
	}
}

func TestUndeferAllClearsEveryDeferralOfTheFile(t *testing.T) {
	cfg, project, _, path := deferSandbox(t)
	queueCommand(t, cfg, project, "defer", "#pay", "--reason", deferReason)
	queueCommand(t, cfg, project, "defer", "5", "--reason", "waits for the copy")
	if view := queueSnapshot(path); view.deferred != 2 {
		t.Fatalf("a #tag and an item number deferred %d items, want 2", view.deferred)
	}
	if printed := queueCommand(t, cfg, project, "undefer", "--all"); !strings.Contains(printed, T("queue.undeferredAll", "TASKS.md")) {
		t.Fatalf("noctis queue undefer --all printed %q", printed)
	}
	if view := queueSnapshot(path); view.deferred != 0 || view.total != 4 {
		t.Fatalf("after undefer --all, %d items are deferred and %d open, want 0 and 4", view.deferred, view.total)
	}
}

func TestADeferralUntilATimeEndsByItself(t *testing.T) {
	cfg, project, _, path := deferSandbox(t)
	defer func(previous int64) { timeOffset = previous }(timeOffset)
	printed := queueCommand(t, cfg, project, "defer", "payment", "--reason", deferReason, "--until", "2h")
	record := getMap(getMap(getMap(readState(), "queueDefer"), queueTrustKey(path)), queueItemDigest("connect the payment provider #pay"))
	until := numberOr(record, "until", 0)
	if later := until - float64(nowSec()); later < 2*3600-60 || later > 2*3600 {
		t.Fatalf("an item deferred for 2h is held until %v, %v s from now", until, later)
	}
	if want := deferReason + ", " + T("queue.deferredUntil", formatTime(until)); !strings.Contains(printed, want) {
		t.Fatalf("noctis queue defer --until printed %q, want it to name %q", printed, want)
	}
	if view := queueSnapshot(path); view.deferred != 1 {
		t.Fatalf("an item deferred for two hours is not deferred now: %+v", view)
	}
	timeOffset += 3 * 3600
	if view := queueSnapshot(path); view.deferred != 0 || view.total != 4 {
		t.Fatalf("three hours later the deferral still holds: %d deferred, %d open", view.deferred, view.total)
	}
	state := readState()
	pruneDeferrals(state, nowSec())
	if records := getMap(state, "queueDefer"); len(records) != 0 {
		t.Fatalf("the ended deferral is still kept: %v", records)
	}
}

func TestADeferralUntilMoreThanNinetyDaysAheadHoldsUntilItEnds(t *testing.T) {
	cfg, project, frontend, path := deferSandbox(t)
	defer func(previous int64) { timeOffset = previous }(timeOffset)
	queueCommand(t, cfg, project, "defer", "payment", "--reason", "waits for the yearly licence renewal", "--until", "120d")
	timeOffset += 91 * 86400
	stopHookOutput(t, stopInput("df90", frontend), cfg)
	if view := queueSnapshot(path); view.deferred != 1 {
		t.Fatalf("an item deferred --until 120d is no longer deferred 91 days later, once a hook wrote the state: %d deferred, %d open", view.deferred, view.total)
	}
	timeOffset += 30 * 86400
	state := readState()
	pruneDeferrals(state, nowSec())
	if view := queueSnapshot(path); view.deferred != 0 || len(getMap(state, "queueDefer")) != 0 {
		t.Fatalf("121 days later the deferral --until 120d still holds: %d deferred, kept %v", view.deferred, getMap(state, "queueDefer"))
	}
}

func TestATagDefersItsGroupButLeavesTheUsersItemsToThem(t *testing.T) {
	cfg, project, _ := queueCheckSandbox(t, "")
	path := writeQueueFile(t, project, "# q\n- [ ] (human) sign the provider's contract #pay\n- [ ] connect the payment provider #pay\n- [ ] write the release notes\n")
	trustQueueFile(path, true)
	queueCommand(t, cfg, project, "defer", "#pay", "--reason", deferReason)
	if view := queueSnapshot(path); view.deferred != 1 || view.human != 1 || view.total != 1 {
		t.Fatalf("deferring #pay left %d deferred, %d marked (human) and %d open, want 1, 1 and 1", view.deferred, view.human, view.total)
	}
}

func TestAnIssueReferenceDefersTheItemsImportedForThatIssue(t *testing.T) {
	_, project, _ := queueCheckSandbox(t, "")
	content := "# q\n## GitHub\n- [ ] (P1) #123 Fix the login redirect\n- [ ] (P2) #12 Add a dark mode\n- [ ] acme/web#7 Update the footer\n- [ ] acme/web#7 Update the header\n- [ ] acme/web#70 Remove the banner\n"
	for reference, want := range map[string][]int{"#123": {1}, "#12": {2}, "acme/web#7": {3, 4}} {
		matched, _ := matchQueueItems(content, reference)
		ordinals := []int{}
		for _, entry := range matched {
			ordinals = append(ordinals, entry.ordinal)
		}
		if !slices.Equal(ordinals, want) {
			t.Errorf("the reference %q names the items %v, want the items of that issue, %v", reference, ordinals, want)
		}
	}
	path := writeQueueFile(t, project, content)
	for reference, items := range map[string][]string{"#123": {"(P1) #123 Fix the login redirect"}, "acme/web#7": {"acme/web#7 Update the footer", "acme/web#7 Update the header"}} {
		run := runNoctisCLI(t, nil, "queue", "defer", reference, "--reason", deferReason, "--file", path)
		for _, item := range items {
			if want := T("queue.deferredItem", "TASKS.md", item, deferReason); run.code != 0 || !strings.Contains(run.stdout, want) {
				t.Errorf("noctis queue defer %q did not defer %q: %s", reference, item, run)
			}
		}
	}
}

func TestQueueStatusNamesTheDeferredItems(t *testing.T) {
	cfg, project, _, path := deferSandbox(t)
	queueCommand(t, cfg, project, "defer", "payment", "--reason", deferReason)
	_, names := deferredNames(queueSnapshot(path))
	if status := queueCommand(t, cfg, project, "status"); !strings.Contains(status, T("queue.deferredStatus", "TASKS.md", 1, names)) {
		t.Fatalf("noctis queue status does not name the deferred item:\n%s", status)
	}
}

func TestADeferralNamesOneOpenItemAndSaysWhy(t *testing.T) {
	_, _, _, path := deferSandbox(t)
	for _, test := range []struct {
		argv []string
		code int
		want string
	}{
		{[]string{"defer", "payment"}, 2, T("queue.deferReason", pluginName)},
		{[]string{"defer", "the", "--reason", "x"}, 1, T("queue.deferAmbiguous", "TASKS.md", "the", 4, "2: migrate the users table; 3: connect the payment provider #pay; 4: charge the first customer (after #pay); 5: write the release notes")},
		{[]string{"defer", "invoices", "--reason", "x"}, 1, T("queue.deferNoMatch", "TASKS.md", "invoices")},
		{[]string{"defer", "1", "--reason", "x"}, 1, T("queue.deferDone", "TASKS.md", "set up the repository")},
		{[]string{"defer", "payment", "--reason", "x", "--until", "yesterday"}, 2, T("queue.deferUntilBad", "yesterday")},
		{[]string{"undefer", "payment"}, 1, T("queue.notDeferred", "TASKS.md", "connect the payment provider #pay")},
	} {
		run := runNoctisCLI(t, nil, append(append([]string{"queue"}, test.argv...), "--file", path)...)
		if run.code != test.code || !strings.Contains(run.stderr, test.want) {
			t.Errorf("noctis queue %s: %s\nwant exit %d and %q", strings.Join(test.argv, " "), run, test.code, test.want)
		}
	}
}

func TestTheDirectiveTellsClaudeHowToDeferAnItem(t *testing.T) {
	cfg, project, _, _ := deferSandbox(t)
	start := object{"hook_event_name": "SessionStart", "source": "startup", "session_id": "df8", "cwd": project}
	context := getString(getMap(hookOutput(t, onSessionStart, start, cfg), "hookSpecificOutput"), "additionalContext")
	if !strings.Contains(context, pluginName+` queue defer <a unique part of its text> --reason "<what it waits on>" --file '`) || !strings.Contains(context, "TASKS.md' and go on") {
		t.Fatalf("the queue directive does not tell Claude how to defer an item: %q", context)
	}
	queueCommand(t, cfg, project, "defer", "payment", "--reason", deferReason)
	context = getString(getMap(hookOutput(t, onSessionStart, start, cfg), "hookSpecificOutput"), "additionalContext")
	if !strings.Contains(context, "1 item(s) are deferred") {
		t.Fatalf("the queue directive does not tell Claude which items are deferred: %q", context)
	}
}

func TestTheContinuationOffersDeferOnlyAfterAStopWithoutProgress(t *testing.T) {
	cfg, _, frontend, _ := deferSandbox(t)
	first := getString(stopHookOutput(t, stopInput("df9", frontend), cfg), "reason")
	if strings.Contains(first, "queue defer") {
		t.Fatalf("a continuation after progress repeats the defer hint: %q", first)
	}
	again := getString(stopHookOutput(t, stopAgain("df9", frontend), cfg), "reason")
	if !strings.Contains(again, pluginName+" queue defer") {
		t.Fatalf("after a stop without progress the continuation does not offer noctis queue defer: %q", again)
	}
}

func TestClaudeMayDeferAnItemItself(t *testing.T) {
	_, project, _, _ := deferSandbox(t)
	call := shellToolCall("df10", project, "Bash", `noctis queue defer payment --reason "`+deferReason+`"`)
	if run := runHostHook(t, "claude", call, "df10", 10*time.Second); run.answer != nil {
		t.Fatalf("noctis answered Claude's own noctis queue defer: %v", run.answer)
	}
}

func TestParseDeferUntil(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		text string
		want time.Time
		ok   bool
	}{
		{"", time.Time{}, true},
		{"90m", now.Add(90 * time.Minute), true},
		{"3d", now.Add(72 * time.Hour), true},
		{"1.5d", now.Add(36 * time.Hour), true},
		{"2026-10-01T09:00:00Z", time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), true},
		{"2026-09-01T09:00:00Z", time.Time{}, false},
		{"-2h", time.Time{}, false},
		{"0d", time.Time{}, false},
		{"soon", time.Time{}, false},
	} {
		got, ok := parseDeferUntil(test.text, now)
		want := 0.0
		if !test.want.IsZero() {
			want = float64(test.want.Unix())
		}
		if got != want || ok != test.ok {
			t.Errorf("parseDeferUntil(%q) = %v, %t; want %v, %t", test.text, got, ok, want, test.ok)
		}
	}
}

func TestOldDeferralsArePruned(t *testing.T) {
	now := int64(2_000_000_000)
	state := object{"queueDefer": object{
		"a": object{
			"kept":    object{"reason": "r", "at": float64(now - 86400)},
			"ended":   object{"reason": "r", "at": float64(now - 86400), "until": float64(now - 60)},
			"too old": object{"reason": "r", "at": float64(now - 91*86400)},
		},
		"b": object{"ended": object{"reason": "r", "at": float64(now - 60), "until": float64(now - 1)}},
	}}
	pruneDeferrals(state, now)
	records := getMap(state, "queueDefer")
	if len(records) != 1 || len(getMap(records, "a")) != 1 || getMap(getMap(records, "a"), "kept") == nil {
		t.Fatalf("pruning kept %v, want only a's live deferral", records)
	}
}
