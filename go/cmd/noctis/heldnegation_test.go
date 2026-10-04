package main

import (
	"fmt"
	"testing"
)

func heldJob(prompt string) (int, string) {
	job := promptJobOf(prompt)
	return len(job.items), heldBackWork(job.text, job.prose, job.lead)
}

func TestAProhibitionWrittenWithAnotherApostropheGetsNoChecklist(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	for _, apostrophe := range []string{"'", "’", "‘", "´", "ʼ", "`", "′", "＇"} {
		lead := "Here is the backlog for next sprint. Don" + apostrophe + "t implement any of these yet."
		if items, held := heldJob(lead + heldBackItems); items != 4 || held == "" {
			t.Errorf("%q (U+%04X): %q is not read as a prohibition: %d items, held %q", apostrophe, []rune(apostrophe)[0], lead, items, held)
		}
	}
	french := "Voici le backlog du prochain sprint. N´implémente rien pour l´instant."
	if items, held := heldJob(french + heldBackItems); items != 4 || held == "" {
		t.Errorf("%q is not read as a prohibition: %d items, held %q", french, items, held)
	}
	expectNoChecklist(t, cfg, "acute", project, "Here is the backlog for next sprint. Don´t implement any of these yet."+heldBackItems)
}

func TestALeadInBeforeAProhibitionIsNotAConditionOnIt(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	intro := "Here is the backlog for next sprint. "
	for _, lead := range []string{
		"Once again, don't implement any of these yet.",
		"In any case, don't implement any of these yet.",
		"Just in case it wasn't clear, don't implement any of these yet.",
		"After our call I changed my mind: don't implement any of these yet.",
		"As we agreed when we met, do not start on any of these yet.",
		"Whether or not you have time, do not start on any of these yet.",
		"Whether the tests pass or not, don't implement any of these yet.",
		"After all, it is only a draft, so don't implement any of these yet.",
		"Ask me if anything is unclear, but don't implement any of these yet.",
		"After our call, don't implement any of these yet.",
		"After we talked yesterday, I would rather wait, so don't implement any of these yet.",
		"Unless I say otherwise, don't implement any of these yet.",
		"Unless I tell you so, do not start on any of these.",
		"When you have time, read through these, but don't implement any of them yet.",
		"Whenever you get a chance, look these over, but do not start on any of them yet.",
		"In case it wasn't clear, don't implement any of these yet.",
		"In case you missed it, don't implement any of these yet.",
		"After lunch, don't implement any of these yet.",
		"After a quick coffee break, don't implement any of these yet.",
		"After our call with the whole design team and the product people yesterday, don't implement any of these yet.",
		"When you read this, don't implement any of these yet.",
		"When you see my message, don't implement any of these yet.",
	} {
		if items, held := heldJob(intro + lead + heldBackItems); items != 4 || held == "" {
			t.Errorf("%q is not read as a prohibition: %d items, held %q", lead, items, held)
		}
	}
	expectNoChecklist(t, cfg, "once-again", project, intro+"Once again, don't implement any of these yet."+heldBackItems)
}

func TestAConditionBeforeAProhibitionStillMakesItARuleOnTheWork(t *testing.T) {
	for _, lead := range []string{
		"Work through these in order. If the build breaks, don't touch any of these until it is green again.",
		"Please do these in order; if anything is unclear, don't implement it yet and ask me first.",
		"Do them in order, and once the tests pass, don't change anything.",
		"Do these one by one. When you are done with a step, don't change it anymore.",
		"Do these in order. Except for the docs, don't touch any files.",
		"Do these in order but if a test fails don't change any of these files.",
		"Do these in order. Even if a test fails, don't touch any of the files.",
		"Work through these in order. In case the build breaks, don't touch any of these until it is green again.",
		"Do these in order. After the migration, don't touch any of these files.",
		"Do these in order. Unless a test fails, don't change any of these files.",
		"Do these in order. If the build fails or otherwise breaks, don't touch any of these files.",
		"Do these in order. When you have the results, don't change any of these files.",
		"Do these in order. If you missed a step, don't change any of these files.",
		"Do these in order. If the test output wasn't clear, don't touch any of these files and ask me first.",
		"Do these in order. After the rest of the tasks are done, don't touch any of these files.",
		"Do these in order. When you read the logs, don't change any of these files.",
	} {
		if items, held := heldJob(lead + heldBackItems); items != 4 || held != "" {
			t.Errorf("%q: a conditional rule held the job back: %d items, held %q", lead, items, held)
		}
	}
}

func TestAnythingButOneAreaIsARuleThatKeepsTheChecklist(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	for index, lead := range []string{
		"Work through these four items in order. Don't touch anything but the auth service and the payment module.",
		"Do these in order and don't stop until all are done. Don't change anything but the files under src/.",
		"Do all of these now. Never edit anything but the code under src.",
	} {
		sid := fmt.Sprintf("anything-but-%d", index)
		startQueue(t, cfg, sid, project, lead+heldBackItems)
		if record := getMap(getMap(readState(), "autoQueues"), sid); numberOr(record, "items", 0) != 4 {
			t.Errorf("%q: the job lost its checklist: %s", lead, journaledReason(sid, "no-auto-queue"))
		}
	}
	for _, lead := range []string{
		"Here is the backlog. Don't implement anything, but estimate each of these.",
		"Here is the backlog. Don't do anything yet but tell me how long each would take.",
		"Here is the backlog. Don't do anything but estimate each of these.",
		"Here is the backlog. Don't do anything but a rough estimate for each of these.",
		"Here is the backlog. Don't implement anything but the first two items.",
	} {
		if items, held := heldJob(lead + heldBackItems); items != 4 || held == "" {
			t.Errorf("%q no longer holds the list back: %d items", lead, items)
		}
	}
}

func TestATurkishReasonClauseThatSaysOtherOrExceptStillHoldsTheListBack(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	intro := "Gelecek sprintin işleri şunlar. "
	for _, lead := range []string{
		"Başka bir deyişle bunların hiçbirini henüz yapma.",
		"Başka ekiplerin de onayı gerektiği için bunların hiçbirini henüz yapma.",
		"Müşteri dışında kimse görmediği için bunların hiçbirini henüz yapma.",
	} {
		if items, held := heldJob(intro + lead + heldBackItemsTr); items != 4 || held == "" {
			t.Errorf("%q is not read as a prohibition: %d items, held %q", lead, items, held)
		}
	}
	expectNoChecklist(t, cfg, "tr-baska", project, intro+"Başka ekiplerin de onayı gerektiği için bunların hiçbirini henüz yapma."+heldBackItemsTr)
	for _, lead := range []string{
		"Sadece şu dört maddeyi yap, başka bir şeye dokunma.",
		"Şu maddeleri sırayla yap; bunlar için README dışında hiçbir dosyaya dokunma.",
		"Diğer dosyalara dokunma.",
	} {
		if items, held := heldJob(lead + heldBackItemsTr); items != 4 || held != "" {
			t.Errorf("%q: a limit on the work held the job back: %d items, held %q", lead, items, held)
		}
	}
}

func TestLetUsNotBeforeTheWorkIsAProhibition(t *testing.T) {
	for _, lead := range []string{
		"Let's not implement any of these yet.",
		"Lets not implement any of these yet.",
		"Let’s not start on any of these yet.",
		"Let us not implement any of these yet.",
	} {
		if items, held := heldJob("Here is the backlog for next sprint. " + lead + heldBackItems); items != 4 || held == "" {
			t.Errorf("%q is not read as a prohibition: %d items, held %q", lead, items, held)
		}
	}
}

func TestATurkishCompoundVerbOrAChangeNounStillHoldsTheListBack(t *testing.T) {
	for _, lead := range []string{
		"Koda müdahale etme.",
		"Bunlara el sürme.",
		"Hiçbirini implemente etme.",
		"Değişiklik yapma.",
		"Şimdilik değişiklik yapma.",
		"Kodda değişiklik yapma.",
	} {
		if items, held := heldJob(lead + heldBackItemsTr); items != 4 || held == "" {
			t.Errorf("%q is not read as a prohibition: %d items, held %q", lead, items, held)
		}
	}
	for _, lead := range []string{
		"README'de değişiklik yapma.",
		"Testleri yaz ama kodda değişiklik yapma.",
	} {
		if items, held := heldJob(lead + heldBackItemsTr); items != 4 || held != "" {
			t.Errorf("%q: a rule on one part of the work held the job back: %d items, held %q", lead, items, held)
		}
	}
}
