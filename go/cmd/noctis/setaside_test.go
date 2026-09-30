package main

import (
	"strings"
	"testing"
)

func TestTheLastContinuationBeforeTheQueueGivesUpAsksToSetTheStuckItemAside(t *testing.T) {
	cfg, project, frontend, _ := deferSandbox(t)
	section(cfg, "queue")["maxIdleContinues"] = float64(3)
	stopHookOutput(t, stopInput("sa1", frontend), cfg)
	if reason := getString(stopHookOutput(t, stopAgain("sa1", frontend), cfg), "reason"); !strings.Contains(reason, "Take the next eligible item") || strings.Contains(reason, "is still the next item") {
		t.Fatalf("a continuation before the last one already asked Claude to set the item aside:\n%s", reason)
	}
	last := stopHookOutput(t, stopAgain("sa1", frontend), cfg)
	reason := getString(last, "reason")
	if getString(last, "decision") != "block" || !strings.Contains(reason, `"migrate the users table" is still the next item`) || strings.Contains(reason, "Take the next eligible item") {
		t.Fatalf("the last continuation before the queue gives up did not ask Claude to set the stuck item aside: %v", last)
	}
	if !strings.Contains(reason, "queue defer <a unique part of its text>") || !strings.Contains(reason, "TASKS.md', leave the item unticked") || strings.Count(reason, "queue defer") != 1 {
		t.Fatalf("the continuation does not give the one defer command for the queue file:\n%s", reason)
	}
	if want := T("queue.setAsideMessage", 2, "migrate the users table"); !strings.Contains(getString(last, "systemMessage"), want) {
		t.Fatalf("the user was told %q, want %q", getString(last, "systemMessage"), want)
	}
	if !getBool(journaledEntry("sa1", "continue-queue"), "setAside", false) {
		t.Fatal("the continuation that asks to set the item aside is not journaled with setAside")
	}
	queueCommand(t, cfg, project, "defer", "migrate", "--reason", "the old table has no key to migrate by")
	next := stopHookOutput(t, stopAgain("sa1", frontend), cfg)
	if reason := getString(next, "reason"); getString(next, "decision") != "block" || !strings.Contains(reason, `("connect the payment provider #pay")`) || !strings.Contains(reason, "Take the next eligible item") {
		t.Fatalf("once the stuck item was set aside, the queue did not go on with the next one: %v", next)
	}
}

func TestAStuckItemNotSetAsideStillEndsTheQueueAtTheIdleLimit(t *testing.T) {
	cfg, _, frontend, _ := deferSandbox(t)
	section(cfg, "queue")["maxIdleContinues"] = float64(3)
	stopHookOutput(t, stopInput("sa2", frontend), cfg)
	stopHookOutput(t, stopAgain("sa2", frontend), cfg)
	stopHookOutput(t, stopAgain("sa2", frontend), cfg)
	output := stopHookOutput(t, stopAgain("sa2", frontend), cfg)
	if getString(output, "decision") == "block" || getString(output, "systemMessage") != T("queue.stuckMessage", 4) {
		t.Fatalf("with the stuck item left as it was, the queue did not give up at queue.maxIdleContinues: %v", output)
	}
}

func TestTheLastSendBackOfAFailingQueueCheckSaysHowToSetTheItemAside(t *testing.T) {
	cfg, project, frontend := queueCheckSandbox(t, shellFor("exit 3", "exit 3"))
	section(cfg, "queue")["verifyAttempts"] = float64(3)
	tickFirstQueueItem(t, project)
	first := getString(stopHookOutput(t, stopInput("sa3", frontend), cfg), "reason")
	if !strings.Contains(first, "Queue check failed") || strings.Contains(first, "queue defer") {
		t.Fatalf("a send-back that is not the last one already says how to set the item aside:\n%s", first)
	}
	last := getString(stopHookOutput(t, stopAgain("sa3", frontend), cfg), "reason")
	if !strings.Contains(last, "If the command fails again, the queue is held until it passes") || !strings.Contains(last, "undo the changes of the item you were on") || !strings.Contains(last, "queue defer <a unique part of its text>") || !strings.Contains(last, "TASKS.md' and go on with the next eligible item") {
		t.Fatalf("the last send-back before the hold does not say how to set the item aside:\n%s", last)
	}
	if held := stopHookOutput(t, stopAgain("sa3", frontend), cfg); getString(held, "decision") == "block" {
		t.Fatalf("the third failure with queue.verifyAttempts 3 did not hold the queue: %v", held)
	}
}

func TestTheFirstContinuationNeverAsksToSetAnItemAside(t *testing.T) {
	cfg, _, frontend, _ := deferSandbox(t)
	section(cfg, "queue")["maxIdleContinues"] = float64(1)
	output := stopHookOutput(t, stopInput("sa4", frontend), cfg)
	if reason := getString(output, "reason"); getString(output, "decision") != "block" || !strings.Contains(reason, "Take the next eligible item") || strings.Contains(reason, "is still the next item") {
		t.Fatalf("with queue.maxIdleContinues 1, the first continuation asked Claude to set an item aside before any was tried: %v", output)
	}
}
