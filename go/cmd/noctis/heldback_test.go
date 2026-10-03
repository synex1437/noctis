package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const heldBackItems = "\n- add a login endpoint with rate limiting to the auth service\n- rewrite the payment module so it uses the new billing client\n- migrate all tables to the new schema with a rollback script\n- remove the legacy logging from the worker and the scheduler\n"

const heldBackItemsTr = "\n- auth servisine hız sınırlamalı bir giriş uç noktası ekle\n- ödeme modülünü yeni faturalama istemcisini kullanacak şekilde yeniden yaz\n- tüm tabloları geri alma betiğiyle yeni şemaya taşı\n- eski loglamayı worker ve zamanlayıcıdan kaldır\n"

func journaledReason(sid, action string) string {
	reason := ""
	for _, line := range tailFileLines(files.decisions, 1000) {
		var entry object
		if jsonUnmarshalObject([]byte(line), &entry) == nil && getString(entry, "sid") == sid && getString(entry, "action") == action {
			reason = getString(entry, "reason")
		}
	}
	return reason
}

func activeStop(sid, cwd string) object {
	input := stopInput(sid, cwd)
	input["stop_hook_active"] = true
	return input
}

func expectNoChecklist(t *testing.T, cfg object, sid, project, prompt string) {
	t.Helper()
	output := startQueue(t, cfg, sid, project, prompt)
	if record := getMap(getMap(readState(), "autoQueues"), sid); record != nil {
		t.Errorf("%s: a prompt that holds the listed work back got a checklist: %v", sid, record)
	}
	if context := contextOf(output); strings.Contains(context, "multi-step job") || strings.Contains(context, "several separate jobs") {
		t.Errorf("%s: Claude was told to work through a job the prompt holds back: %q", sid, context)
	}
	if stop := stopHookOutput(t, activeStop(sid, project), cfg); getString(stop, "decision") == "block" {
		t.Errorf("%s: the Stop hook drove Claude through work the prompt holds back: %v", sid, stop)
	}
}

func TestAPromptThatForbidsTheListedWorkGetsNoChecklist(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	for _, tc := range []struct{ sid, prompt, quoted string }{
		{"hb-en", "Do NOT implement any of the following right now and do not touch any files; just estimate how long each would take and give me a table." + heldBackItems, "do not implement any of the following"},
		{"hb-tr", "Aşağıdaki maddelerin HİÇBİRİNİ şimdi yapma, dosyalara dokunma; sadece her biri için ne kadar süreceğini tahmin et ve bana tablo halinde yaz." + heldBackItemsTr, "hiçbirini şimdi yapma"},
	} {
		expectNoChecklist(t, cfg, tc.sid, project, tc.prompt)
		if reason := journaledReason(tc.sid, "no-auto-queue"); !strings.Contains(strings.ToLower(reason), tc.quoted) {
			t.Errorf("%s: noctis why does not say which words held the job back: %q", tc.sid, reason)
		}
	}
}

func TestTheSameListWithoutTheHoldBackIsStillAJob(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	for sid, prompt := range map[string]string{
		"hb-go-en": "Please do all of the following today, one after the other, and tell me when you are done." + heldBackItems,
		"hb-go-tr": "Aşağıdaki maddelerin hepsini sırayla yap ve bitince bana haber ver." + heldBackItemsTr,
	} {
		startQueue(t, cfg, sid, project, prompt)
		if record := getMap(getMap(readState(), "autoQueues"), sid); numberOr(record, "items", 0) != 4 {
			t.Errorf("%s: the four listed jobs did not become the checklist: %v", sid, record)
		}
	}
}

func TestPromptsThatAskOnlyForAPlanOrAnEstimateGetNoChecklist(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	prompts := []string{
		"Don't start on any of these yet. Just give me a plan with the order you would do them in and the risks you see." + heldBackItems,
		"Estimate how long each of these would take and rank them by risk, so I can plan the sprint with the team." + heldBackItems,
		"Without implementing anything, review the list below and tell me which items depend on each other." + heldBackItems,
		"I only want an estimate for these for now, so please make no code changes in this session." + heldBackItems,
		"Never touch the code for this one; only plan how you would approach each item on the list below." + heldBackItems,
		"Bunların hiçbirini henüz uygulama, sadece bir plan çıkar ve riskleri listele." + heldBackItemsTr,
		"Aşağıdaki işleri sadece planla, koda dokunma." + heldBackItemsTr,
		"Bu maddeleri uygulamadan her biri için kaba bir süre tahmini ver." + heldBackItemsTr,
		"Bitte setze noch nichts davon um und fass keine Dateien an, ich brauche nur eine Schätzung pro Punkt." + heldBackItems,
		"N'implémente rien pour l'instant et ne touche à aucun fichier : donne-moi juste une estimation pour chaque point." + heldBackItems,
		"No implementes nada todavía y no toques ningún archivo; solo estima cuánto tardaría cada punto." + heldBackItems,
		"Não implemente nada ainda e não mexa em nenhum arquivo; apenas estime quanto tempo cada item levaria." + heldBackItems,
		"Non implementare niente per ora e non toccare nessun file; stima solo quanto tempo richiederebbe ogni punto." + heldBackItems,
		"Implementeer nog niets en raak geen bestanden aan; geef alleen een schatting per punt." + heldBackItems,
		"Nie implementuj jeszcze niczego i nie ruszaj plików; tylko oszacuj, ile zajmie każdy punkt." + heldBackItems,
		"Пока ничего не реализуй и не трогай файлы; только оцени, сколько времени займёт каждый пункт." + heldBackItems,
		"以下の項目はまだ実装しないでください。ファイルにも触れないで、それぞれの所要時間の見積もりだけ表にしてください。" + heldBackItems,
		"先不要实现下面的任何一项，也不要修改任何文件，只估算每项需要多长时间并列成表格。" + heldBackItems,
		"아래 항목은 아직 구현하지 마세요. 파일도 건드리지 말고 각 항목에 걸리는 시간 견적만 표로 주세요." + heldBackItems,
		"لا تنفذ أيًا من البنود التالية الآن ولا تلمس أي ملف؛ فقط قدّر المدة التي يستغرقها كل بند." + heldBackItems,
		"Bunların her biri için kaba bir süre tahmini yapabilir misin? Liste aşağıda." + heldBackItemsTr,
		"Aşağıdakileri planlayabilir misiniz? Sıralama ve riskler önemli." + heldBackItemsTr,
	}
	for index, prompt := range prompts {
		sid := fmt.Sprintf("hb-plan-%d", index)
		expectNoChecklist(t, cfg, sid, project, prompt)
		if journaledReason(sid, "no-auto-queue") == "" {
			t.Errorf("%s: the skipped job was not journaled", sid)
		}
	}
}

func TestAProseRequestThatHoldsItsJobsBackIsNotSplitIntoAChecklist(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	prompt := "Don't implement any of this yet, only estimate it for me. The signup page has been slow since the last release. " +
		"Fix the slow query behind the signup page, add an index on the users email column, and update the changelog with both changes so the release notes stay honest."
	if !severalJobsLikely(prompt) {
		t.Fatal("the prompt no longer reads as several jobs, so it tests nothing")
	}
	expectNoChecklist(t, cfg, "hb-split", project, prompt)
	if journaledReason("hb-split", "no-auto-queue") == "" {
		t.Error("the prose request that holds its jobs back was not journaled")
	}
}

func TestAPromptThatHoldsTheWorkOffOrAsksForAnEstimateInOtherWordsGetsNoChecklist(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	prompts := []string{
		"Please hold off on these until Monday. Give me a summary of the risks." + heldBackItems,
		"Hold off on implementing these:" + heldBackItems,
		heldBackItems + "I need a rough estimate for each of the above before the meeting.",
		"Give me a time estimate for each of the following, so I can plan the sprint with the team." + heldBackItems,
		"Let's discuss these before doing anything." + heldBackItems,
		"I'm not asking you to do these. I want your estimate for each one." + heldBackItems,
		"This is a read-only session. For the items below, give me a written plan for each." + heldBackItems,
		"Don't start on this until tomorrow; for now write me a short overview." + heldBackItems,
		"Bunları şu an yapmanı istemiyorum, her biri için kaç saat süreceğini söyle bana." + heldBackItemsTr,
		"Bitte setze das noch nicht um, ich brauche zuerst eine Einschätzung für jeden Punkt." + heldBackItems,
		"Implementiere das bitte noch nicht. Sag mir nur, wie lange jeder Punkt ungefähr dauert." + heldBackItems,
		"No implementes estas tareas todavía; dame una estimación de cuánto tardaría cada una." + heldBackItems,
		"Пока не надо ничего делать, просто оцени, сколько времени займёт каждый пункт." + heldBackItems,
		"Implement these, but not yet. First give me a summary of the risks." + heldBackItems,
		"Here is what the next release needs:" + heldBackItems + "Please hold off on these for now; I only want them written down.",
	}
	for index, prompt := range prompts {
		sid := fmt.Sprintf("hb-q1-%d", index)
		expectNoChecklist(t, cfg, sid, project, prompt)
		if journaledReason(sid, "no-auto-queue") == "" {
			t.Errorf("%s: the skipped job was not journaled (%q)", sid, strings.SplitN(prompt, "\n", 2)[0])
		}
	}
}

func TestAJobPromptWithAConstraintStillGetsItsChecklist(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	for index, prompt := range []string{
		"Do all of these in order. Don't commit anything and don't push; I will review the diff myself before it goes anywhere." + heldBackItems,
		"Work through the list below. Do not change any other files and don't touch the migrations folder." + heldBackItems,
		"Please do these today and don't stop until they are all done; don't ask me for confirmation between items." + heldBackItems,
		"Read the README first. Don't forget to update the changelog for each change, and never write to the production database." + heldBackItems,
		"Review the following changes and fix any issues you find; don't make any changes to the public API." + heldBackItems,
		"If a test fails, don't change it; fix the code instead. Don't start the next item until the tests pass." + heldBackItems,
		"Aşağıdakileri sırayla yap. Hiçbir şeyi commit etme ve testleri silme; bitince bana haber ver." + heldBackItemsTr,
		"Şu maddeleri uygula ama migration klasörüne dokunma." + heldBackItemsTr,
		"Her maddeyi uygulamadan önce testini yaz, sonra uygula ve testleri çalıştır." + heldBackItemsTr,
		"Sadece şu dört maddeyi yap, başka bir şeye dokunma." + heldBackItemsTr,
		"The config format is fine as it is. Don't change it, and don't create any new files for these." + heldBackItems,
		"Review each change carefully before committing, and keep the public API as it is without changing behavior." + heldBackItems,
		"No breaking changes and no new dependencies, please. Don't do all of them at once; one commit per item." + heldBackItems,
		"Her değişikliği commit etmeden önce incele ve testlerin geçtiğinden emin ol." + heldBackItemsTr,
		"Kullanıcı şimdi uygulamadan çıkınca oturum kapanmıyor; bu yüzden şu maddeleri sırayla hallet." + heldBackItemsTr,
		"Do these in order, but hold off on the deploy until I review the diff." + heldBackItems,
		"Don't hold off on these any longer; do them now, one after the other." + heldBackItems,
		"Here is the list for today. Let me know if anything is unclear about any of these." + heldBackItems,
	} {
		sid := fmt.Sprintf("hb-job-%d", index)
		startQueue(t, cfg, sid, project, prompt)
		if record := getMap(getMap(readState(), "autoQueues"), sid); numberOr(record, "items", 0) != 4 {
			t.Errorf("prompt %d: a job with a constraint lost its checklist: %v (journal: %q)", index, record, journaledReason(sid, "no-auto-queue"))
		}
	}
}

// q2TestItems, q2TestItemsTr and q2DocItems list jobs whose work is tests or docs.
const (
	q2TestItems   = "\n- config loader: cover the defaults and the environment overrides\n- retry client: test the backoff with a fake server and a fake clock\n- CSV exporter: check empty files, quoted fields and malformed rows\n- date parser: add cases around the daylight saving switch\n"
	q2TestItemsTr = "\n- yapılandırma yükleyicisinin varsayılan değerlerini test et\n- yeniden deneme istemcisinin bekleme süresini sahte sunucuyla sına\n- CSV dışa aktarıcısını boş ve bozuk satırlarla dene\n- tarih ayrıştırıcısına yaz saati geçişi için test ekle\n"
	q2DocItems    = "\n- document the new --dry-run flag in the CLI reference\n- add a troubleshooting section for the login timeout\n- update the install guide for the renamed packages\n- fix the broken links in the contributing guide\n"
)

func TestARuleInsideAStepOrOnTheCodeOfATestsOrDocsJobKeepsTheChecklist(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	for index, prompt := range []string{
		"Please do the following for the schema change, one after the other:\n- add a created_at column to the users table with a migration\n" +
			"- backfill created_at for the existing users from the audit log\n- update the docs for the schema change, don't change the code here\n" +
			"- add a test that runs the migration twice without errors\n",
		"Write tests for all of these modules; don't change the existing code, only add new test files." + q2TestItems,
		"Add tests for all of these. Do not touch the source code, only the test files." + q2TestItems,
		"Don't touch any code, this is docs only." + q2DocItems,
		"Bunların hepsini sırayla yap, şimdilik deploy yapma, bitince bana haber ver." + heldBackItemsTr,
		"Aşağıdakilerin hepsini sırayla yap ama henüz commit yapma, önce ben bakacağım." + heldBackItemsTr,
		"Aşağıdaki modüllerin her biri için test yaz, mevcut kodu değiştirme, sadece test dosyası ekle." + q2TestItemsTr,
		"Write the tests for these, but don't touch the code for now." + q2TestItems,
		"Testleri yaz ama şimdilik koda dokunma." + q2TestItemsTr,
	} {
		sid := fmt.Sprintf("hb-q2-%d", index)
		startQueue(t, cfg, sid, project, prompt)
		if record := getMap(getMap(readState(), "autoQueues"), sid); numberOr(record, "items", 0) != 4 {
			t.Errorf("%s: a rule on how to do the work lost the checklist: %v items (journal: %q)", sid, record["items"], journaledReason(sid, "no-auto-queue"))
		}
	}
}

func TestAListedLineThatHoldsBackTheWholeListStillGetsNoChecklist(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	for index, prompt := range []string{
		"Here is the backlog for the next sprint:" + heldBackItems + "- don't implement any of these yet, I only want an estimate for each\n",
		"Gelecek sprintin işleri:" + heldBackItemsTr + "- bunların hiçbirini henüz yapma, sadece her biri için süre tahmini ver\n",
		"Don't change any code yet; first tell me which tests each of these would need." + heldBackItems,
	} {
		sid := fmt.Sprintf("hb-q2-held-%d", index)
		expectNoChecklist(t, cfg, sid, project, prompt)
		if journaledReason(sid, "no-auto-queue") == "" {
			t.Errorf("%s: the skipped job was not journaled", sid)
		}
	}
}

func TestTheHeldBackPhrasesAreWrittenAsTheyAreMatched(t *testing.T) {
	for _, list := range [][]string{heldForbidPhrases, heldAskPhrases} {
		for _, phrase := range list {
			if heldNormal(phrase) != phrase {
				t.Errorf("phrase %q is matched against lower-case text but is not written that way", phrase)
			}
		}
	}
}

func TestAHugePromptIsReadQuickly(t *testing.T) {
	for _, prompt := range []string{
		strings.Repeat("Do not touch the migrations folder, and never write to the production database. ", 1200) + heldBackItems,
		"Here is the backlog. " + strings.Repeat("and after our call ", 8000) + "don't implement any of these yet." + heldBackItems,
	} {
		started := time.Now()
		job := promptJobOf(prompt)
		heldBackWork(job.text, job.prose, job.lead)
		heldBackWork(prompt, prompt, true)
		if elapsed := time.Since(started); elapsed > 2*time.Second {
			t.Errorf("reading a %d-byte prompt took %s", len(prompt), elapsed)
		}
	}
}
