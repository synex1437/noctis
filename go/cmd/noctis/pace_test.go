package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// setPaceNotes gives the queue file at path these notes of its pace: at, done, used, resetsAt.
func setPaceNotes(path string, notes ...[4]float64) {
	list := []any{}
	for _, note := range notes {
		list = append(list, []any{note[0], note[1], note[2], note[3]})
	}
	updateState(func(next object) {
		stateMap(next, "queuePace")[queueTrustKey(path)] = object{"path": path, "at": float64(nowSec()), "notes": list}
	})
}

func paceNotesIn(path string) []paceNote {
	return paceNotesOf(getMap(getMap(readState(), "queuePace"), queueTrustKey(path)))
}

func TestTheStopHookNotesThePaceOfItsQueueWhenAnItemIsTicked(t *testing.T) {
	cfg, project, frontend, path := deferSandbox(t)
	writeUsage(10, 20, 0)
	stopHookOutput(t, stopInput("pc1", frontend), cfg)
	if notes := paceNotesIn(path); len(notes) != 1 || notes[0].done != 1 || notes[0].used != 20 {
		t.Fatalf("the first stop noted %v, want one note of 1 item done at 20 %%", notes)
	}
	writeUsage(10, 21, 0)
	stopHookOutput(t, stopInput("pc1", frontend), cfg)
	if notes := paceNotesIn(path); len(notes) != 1 {
		t.Fatalf("a stop with no item ticked since added a note: %v", notes)
	}
	writeQueueFile(t, project, deferTicked("migrate the users table"))
	writeUsage(10, 23, 0)
	stopHookOutput(t, stopInput("pc1", frontend), cfg)
	if notes := paceNotesIn(path); len(notes) != 2 || notes[1].done != 2 || notes[1].used != 23 {
		t.Fatalf("a stop after a tick noted %v, want a second note of 2 items done at 23 %%", notes)
	}
	writeQueueFile(t, project, strings.Replace(deferQueue, "- [x] set up the repository", "- [ ] set up the repository", 1))
	trustQueueFile(path, true)
	stopHookOutput(t, stopInput("pc1", frontend), cfg)
	if notes := paceNotesIn(path); len(notes) != 1 || notes[0].done != 0 {
		t.Fatalf("unticking the items did not start the notes over: %v", notes)
	}
}

func TestThePaceSaysWhatAnItemTakesAndWhenTheItemsLeftAreDone(t *testing.T) {
	cfg, _, _, path := deferSandbox(t)
	now := nowSec()
	start, reset := float64(now-4*3600), float64(now+3*86400)
	setPaceNotes(path, [4]float64{start, 1, 10, reset}, [4]float64{start + 3600, 2, 12, reset}, [4]float64{start + 7200, 3, 14, reset + 30}, [4]float64{start + 3*3600, 4, 16, reset})
	usage := usageView{sevenDay: &window{used: 18, resetsAt: reset}}
	pace := paceOf(cfg, readState(), path, 10, usage, now)
	if pace.items != 3 || pace.weeklyItems != 3 || pace.secondsPerItem != 3600 || pace.weeklyPerItem != 2 || pace.weeklyNeeded != 20 || pace.weeklyRoom != 77 {
		t.Fatalf("the pace is %+v, want 3 items at an hour and 2 %% each, 20 %% needed of the 77 %% left", pace)
	}
	if want := float64(now + 10*3600); pace.finishAt != want {
		t.Fatalf("the items left are done at %s, want %s", formatTime(pace.finishAt), formatTime(want))
	}
	lines := paceLines(pace)
	want := []string{
		T("queue.pace", durationText(3600), 3, 50),
		T("queue.paceLeft", 10, T("badge.percent", 20), T("badge.percent", 77), formatTime(pace.finishAt)),
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the pace reads\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	facts := toObject(paceFacts(pace))
	if numberOr(facts, "weeklyPerItem", 0) != 2 || numberOr(facts, "weeklyNeeded", 0) != 20 || numberOr(facts, "secondsPerItem", 0) != 3600 || numberOr(facts, "left", 0) != 10 {
		t.Fatalf("noctis queue status --json gives the pace as %v", facts)
	}
	if lines := paceLines(paceOf(cfg, readState(), path, 0, usage, now)); len(lines) != 1 {
		t.Fatalf("with no item left the pace says %q", lines)
	}
}

func TestThePaceWaitsForTheWeeklyResetWhenTheItemsLeftPassThePausePoint(t *testing.T) {
	cfg, _, _, path := deferSandbox(t)
	section(cfg, "thresholds")["weeklyAll"] = float64(95)
	now := nowSec()
	start, reset := float64(now-4*3600), float64(now+3*86400)
	setPaceNotes(path, [4]float64{start, 1, 80, reset}, [4]float64{start + 3600, 2, 82, reset}, [4]float64{start + 7200, 3, 84, reset}, [4]float64{start + 3*3600, 4, 86, reset})
	pace := paceOf(cfg, readState(), path, 6, usageView{sevenDay: &window{used: 90, resetsAt: reset}}, now)
	// Two items fit before 95 %: 92 and 94. The third would pass it, so it starts at the reset.
	if want := reset + 4*3600; pace.finishAt != want {
		t.Fatalf("the items left are done at %s, want %s: two now, four after the reset", formatTime(pace.finishAt), formatTime(want))
	}
	section(cfg, "thresholds")["weeklyAll"] = false
	pace = paceOf(cfg, readState(), path, 6, usageView{sevenDay: &window{used: 90, resetsAt: reset}}, now)
	// Five items fit before 100 %: 92, 94, 96, 98 and 100. The sixth starts at the reset.
	if want := reset + 3600; pace.finishAt != want || pace.weeklyRoom != 10 {
		t.Fatalf("with the weekly threshold off the items are done at %s with %v %% room, want %s with 10 %%: five before 100 %%", formatTime(pace.finishAt), pace.weeklyRoom, formatTime(want))
	}
	if pace := paceOf(cfg, readState(), path, 6, usageView{}, now); pace.finishAt != float64(now+6*3600) || pace.weeklyRoom != -1 {
		t.Fatalf("without a weekly reading the items are done at %s (room %v), want an hour an item from now", formatTime(pace.finishAt), pace.weeklyRoom)
	} else if lines := paceLines(pace); len(lines) != 2 || lines[1] != T("queue.paceLeftTime", 6, formatTime(pace.finishAt)) {
		t.Fatalf("without a weekly reading the pace reads %q", lines)
	}
}

func TestThePaceLeavesOutLongGapsAndItemsAcrossAWeeklyReset(t *testing.T) {
	cfg, _, _, path := deferSandbox(t)
	now := nowSec()
	start, reset := float64(now-40*3600), float64(now+3*86400)
	setPaceNotes(path,
		[4]float64{start, 1, 10, reset - 7*86400},
		[4]float64{start + 1800, 2, 12, reset - 7*86400},
		// A new weekly window: the item counts for the time, not for the limit.
		[4]float64{start + 3600, 3, 1, reset},
		// Ten hours later: idle time, left out.
		[4]float64{start + 11*3600, 4, 30, reset},
		[4]float64{start + 12*3600, 5, 33, reset},
		[4]float64{start + 13*3600, 7, 39, reset},
		// A reading missing: the item counts for the time only.
		[4]float64{start + 14*3600, 8, -1, 0},
	)
	pace := paceOf(cfg, readState(), path, 1, usageView{}, now)
	if pace.items != 6 || pace.weeklyItems != 4 || pace.weeklyPerItem != 2.75 {
		t.Fatalf("the pace is %+v, want 6 items for the time and 4 for the limit at 2.75 %% each", pace)
	}
	if want := float64(1800+1800+3600+3600+3600) / 6; pace.secondsPerItem != want {
		t.Fatalf("an item takes %v s, want %v", pace.secondsPerItem, want)
	}
	setPaceNotes(path, [4]float64{start, 1, 10, reset}, [4]float64{start + 3600, 3, 14, reset})
	if pace := paceOf(cfg, readState(), path, 1, usageView{}, now); paceLines(pace) != nil || paceFacts(pace) != nil {
		t.Fatalf("two items give a pace: %+v", pace)
	}
}

func TestQueueStatusAndTheDigestGiveThePace(t *testing.T) {
	cfg, project, _, path := deferSandbox(t)
	queueCommand(t, cfg, project, "status")
	if printed := queueCommand(t, cfg, project, "status"); strings.Contains(printed, T("queue.paceSoon", paceMinItems, paceGapSeconds/3600, 0)) {
		t.Fatalf("noctis queue status speaks of a pace no session noted:\n%s", printed)
	}
	now := nowSec()
	setPaceNotes(path, [4]float64{float64(now - 3600), 1, 10, float64(now + 86400)})
	if printed := queueCommand(t, cfg, project, "status"); !strings.Contains(printed, T("queue.paceSoon", paceMinItems, paceGapSeconds/3600, 0)) {
		t.Fatalf("noctis queue status does not say when the pace will be known:\n%s", printed)
	}
	writeUsage(10, 30, 0)
	start, reset := float64(now-4*3600), float64(now+3*86400)
	setPaceNotes(path, [4]float64{start, 1, 10, reset}, [4]float64{start + 3600, 2, 12, reset}, [4]float64{start + 7200, 3, 14, reset}, [4]float64{start + 3*3600, 4, 16, reset})
	printed := queueCommand(t, cfg, project, "status")
	if want := "  " + T("queue.pace", durationText(3600), 3, 50); !strings.Contains(printed, want) {
		t.Fatalf("noctis queue status does not give the pace %q:\n%s", want, printed)
	}
	if !strings.Contains(printed, T("badge.percent", 8)+" ") {
		t.Fatalf("noctis queue status does not say the 4 items left need 8 %% of the weekly limit:\n%s", printed)
	}
	facts := object{}
	if err := jsonUnmarshal([]byte(queueCommand(t, cfg, project, "status", "--json")), &facts); err != nil {
		t.Fatal(err)
	}
	if pace := getMap(facts, "pace"); numberOr(pace, "items", 0) != 3 || numberOr(pace, "left", 0) != 4 || numberOr(pace, "weeklyNeeded", 0) != 8 {
		t.Fatalf("noctis queue status --json gives the pace as %v", pace)
	}
	section(cfg, "alarm")["digestAt"] = "21:00"
	report := buildDigest(cfg, readState(), currentUsage(now), nil, now)
	if want := "- " + T("queue.pace", durationText(3600), 3, 50); !strings.Contains(report.body, want) {
		t.Fatalf("the digest does not give the pace %q:\n%s", want, report.body)
	}
	status := describeState(cfg, readState(), currentUsage(now), now)
	if want := T("status.queue", filepath.Base(path)) + "\n  " + T("queue.pace", durationText(3600), 3, 50); !strings.Contains(status, want) {
		t.Fatalf("noctis status does not give the pace %q:\n%s", want, status)
	}
}

func TestPaceNotesAreDroppedAMonthAfterTheLastOne(t *testing.T) {
	sandboxFiles(t)
	state := emptyState()
	now := nowSec()
	stateMap(state, "queuePace")["old"] = object{"path": "/q/OLD.md", "at": float64(now - queueTrustTTLSeconds - 60), "notes": []any{}}
	stateMap(state, "queuePace")["new"] = object{"path": "/q/NEW.md", "at": float64(now - 86400), "notes": []any{}}
	pruneState(state, now)
	if paces := stateMap(state, "queuePace"); paces["old"] != nil || paces["new"] == nil {
		t.Fatalf("pruning kept %v, want only the notes of the file noted a day ago", paces)
	}
}

func TestThePaceStopsAtACreditCeilingUnderTheWeeklyPausePoint(t *testing.T) {
	cfg, _, _, path := deferSandbox(t)
	cfg["credits"] = object{"ceiling": float64(90)}
	now := nowSec()
	start, reset := float64(now-4*3600), float64(now+3*86400)
	setPaceNotes(path, [4]float64{start, 1, 80, reset}, [4]float64{start + 3600, 2, 82, reset}, [4]float64{start + 7200, 3, 84, reset}, [4]float64{start + 3*3600, 4, 86, reset})
	usage := usageView{sevenDay: &window{used: 86, resetsAt: reset}}
	for _, weekly := range []any{false, float64(95)} {
		section(cfg, "thresholds")["weeklyAll"] = weekly
		if plan := evaluate(cfg, usageView{sevenDay: &window{used: 90, resetsAt: reset}}, "", 0, false).wait; plan == nil {
			t.Fatalf("setup: at 90 %% of the weekly limit with credits.ceiling 90 and weeklyAll %v the guard did not stop", weekly)
		}
		pace := paceOf(cfg, readState(), path, 6, usage, now)
		if want := reset + 4*3600; pace.finishAt != want || pace.weeklyRoom != 4 {
			t.Errorf("work stops at 90 %% of the weekly limit (weeklyAll %v, credits.ceiling 90), now at 86 %%; the pace gives %v %% room and the six items left done %s, want 4 %% room and %s: %q",
				weekly, pace.weeklyRoom, formatTime(pace.finishAt), formatTime(want), paceLines(pace))
		}
	}
	cfg["credits"] = object{"ceiling": float64(90), "allowPaid": true}
	section(cfg, "thresholds")["weeklyAll"] = float64(95)
	if pace := paceOf(cfg, readState(), path, 6, usage, now); pace.weeklyRoom != 9 {
		t.Errorf("with paid credits allowed the ceiling does not stop work, so the room is 95 - 86 = 9 %%, not %v %%", pace.weeklyRoom)
	}
}
