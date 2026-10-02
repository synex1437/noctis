package main

import (
	"strings"
	"testing"
)

const arabicItems = "\n- أضف نقطة دخول لتسجيل الدخول مع حد للطلبات في خدمة المصادقة\n- أعد كتابة وحدة الدفع لتستخدم عميل الفوترة الجديد\n- انقل كل الجداول إلى المخطط الجديد مع سكربت للتراجع\n- أزل التسجيل القديم من العامل ومن المجدول\n"

func expectQuestionLeftAlone(t *testing.T, cfg object, sid, project, prompt string) {
	t.Helper()
	output := startQueue(t, cfg, sid, project, prompt)
	if record := getMap(getMap(readState(), "autoQueues"), sid); record != nil {
		t.Errorf("%s: the question %q got a %v-item checklist (%q)", sid, prompt[strings.LastIndex(prompt, "\n")+1:], record["items"], getString(output, "systemMessage"))
	}
	if stop := stopHookOutput(t, activeStop(sid, project), cfg); getString(stop, "decision") == "block" {
		t.Errorf("%s: the Stop hook drives Claude through the items the user only asked about: %.160q", sid, getString(stop, "reason"))
	}
}

func TestAnArabicQuestionAboutTheListGetsNoChecklist(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	question := "هذه قائمة المهام للأسبوع القادم:" + arabicItems + "كم من الوقت تحتاج كل مهمة من هذه المهام برأيك؟"
	expectQuestionLeftAlone(t, cfg, "ar-q-latin", project, strings.ReplaceAll(question, "؟", "?"))
	expectQuestionLeftAlone(t, cfg, "ar-q", project, question)
	if !endsQuestion(question) {
		t.Errorf("a prompt ending in the Arabic question mark is not read as a question")
	}
}

func TestAListOfArabicQuestionsGetsNoChecklist(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	prompt := "لدي بعض الأسئلة عن المشروع قبل أن نبدأ العمل عليه الأسبوع القادم، أريد إجابات قصيرة فقط:\n" +
		"- هل نحتاج إلى قاعدة بيانات جديدة لهذا المشروع أم تكفي القاعدة الحالية؟\n" +
		"- هل يمكن استخدام نفس الخادم للواجهة الأمامية والخلفية معا؟\n" +
		"- هل المكتبة التي نستخدمها للمصادقة ما زالت مدعومة من مطوريها؟\n" +
		"- هل توجد اختبارات كافية لوحدة الدفع قبل أن نغير أي شيء فيها؟\n"
	for sid, text := range map[string]string{"ar-ql-latin": strings.ReplaceAll(prompt, "؟", "?"), "ar-ql": prompt} {
		startQueue(t, cfg, sid, project, text)
		if record := getMap(getMap(readState(), "autoQueues"), sid); record != nil {
			content, _ := readQueueText(getString(record, "path"))
			t.Errorf("%s: four Arabic questions became a %v-step checklist:\n%s", sid, record["items"], content)
		}
	}
	if stepLike("هل نحتاج إلى قاعدة بيانات جديدة لهذا المشروع أم تكفي القاعدة الحالية؟", "ar") {
		t.Errorf("a listed Arabic question reads as a step")
	}
}

func TestAQuestionThatEndsInAnEmojiASmileyOrAnExclamationGetsNoChecklist(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	de := "Hier ist unser Backlog für den nächsten Sprint:\n- füge einen Login-Endpunkt mit Ratenbegrenzung zum Auth-Dienst hinzu\n- schreib das Zahlungsmodul auf den neuen Billing-Client um\n- migriere alle Tabellen mit einem Rollback-Skript auf das neue Schema\n- entferne das alte Logging aus dem Worker und dem Scheduler\nWelche davon sollten wir zuerst machen"
	es := "Este es el backlog del próximo sprint:\n- añade un endpoint de login con límite de peticiones al servicio de autenticación\n- reescribe el módulo de pagos para que use el nuevo cliente de facturación\n- migra todas las tablas al nuevo esquema con un script de reversión\n- elimina el logging antiguo del worker y del planificador\n¿Cuál de estas deberíamos hacer primero"
	fr := "Voici le backlog du prochain sprint :\n- ajoute un endpoint de connexion avec limitation de débit au service d'authentification\n- réécris le module de paiement pour qu'il utilise le nouveau client de facturation\n- migre toutes les tables vers le nouveau schéma avec un script de retour arrière\n- supprime l'ancien logging du worker et du planificateur\nLaquelle devrions-nous faire en premier"
	tr := "Gelecek sprintin işleri şunlar:\n- kimlik doğrulama servisine istek sınırlamalı yeni bir giriş uç noktası ekle\n- ödeme modülünü yeni faturalama istemcisini kullanacak şekilde yeniden yaz\n- tüm tabloları geri alma betiğiyle birlikte yeni şemaya taşı\n- işçi ve zamanlayıcıdan eski loglama kodunu kaldır\nSence bunlardan hangisini önce yapmalıyız"
	en := "Here is the backlog for the next sprint:" + heldBackItems + "Which of these should we do first"
	for _, tc := range []struct{ sid, prompt string }{
		{"q-plain-de", de + "?"},
		{"q-plain-es", es + "?"},
		{"q-plain-fr", fr + " ?"},
		{"q-plain-tr", tr + "?"},
		{"q-emoji-en", en + "? 🤔"},
		{"q-emoji-de", de + "? 🤔"},
		{"q-smiley-es", es + "? :)"},
		{"q-bang-fr", fr + " ?!"},
		{"q-emoji-tr", tr + "? 🤔"},
		{"q-shrug-de", de + "? 🤷‍♂️"},
		{"q-grin-tr", tr + "? :D"},
		{"q-wink-es", es + "?? ;-)"},
		{"q-fullwidth-fr", fr + "？！"},
	} {
		expectQuestionLeftAlone(t, cfg, tc.sid, project, tc.prompt)
	}
}

func TestTheMarksAfterAQuestionMarkDoNotHideTheQuestion(t *testing.T) {
	for _, text := range []string{
		"Welche davon sollten wir zuerst machen? 🤔",
		"¿Cuál de estas deberíamos hacer primero? :)",
		"Laquelle devrions-nous faire en premier ?!",
		"Which one first?!?",
		"Is this the right order? :D",
		"Is this the right order? ;-)",
		"Is this the right order? ^^",
		"Is this the right order? 🙏🏽",
		"Is this the right order? 🤷‍♂️",
		"Is this the right order?\u200b",
		"“Is this the right order?”",
		"**Is this the right order?**",
		"كم من الوقت تحتاج كل مهمة؟",
		"这些任务先做哪个？",
		"これはどれから始めますか？😊",
	} {
		if !endsQuestion(text) {
			t.Errorf("%q is not read as a question", text)
		}
	}
	for _, text := range []string{
		"Do these now!",
		"Do these now! 🚀",
		"Fix these today :)",
		"Here is the list 🙏",
		"Is it done? Do it now!",
		"What is left? Fix the rest :D",
		"Bunları bugün bitir.",
	} {
		if endsQuestion(text) {
			t.Errorf("%q is read as a question", text)
		}
	}
}

func TestAClosingPoliteRequestWithAnEmojiOrAnExclamationStillGetsItsChecklist(t *testing.T) {
	for _, tc := range []struct {
		closing string
		items   []string
		want    int
	}{
		{"Can you do these? 🙏", heldBackItemsList(), 4},
		{"Could you please take care of all of them today?! 🙂", heldBackItemsList(), 4},
		{"Can you do these? :)", heldBackItemsList(), 4},
		{"Bunları yapabilir misin? 🙏", heldBackItemsTrList(), 4},
		{"Hepsini sırayla halleder misin? :)", heldBackItemsTrList(), 4},
		{"Can you estimate these? 🙏", heldBackItemsList(), 0},
		{"Which of these should I do first? 🤔", heldBackItemsList(), 0},
		{"Bunlardan hangisi önce yapılmalı? 🤔", heldBackItemsTrList(), 0},
	} {
		prompt := "Here is the list for the next release, after the planning call with the team this morning." + bullets(tc.items) + tc.closing
		if got := autoQueueItems(prompt); len(got) != tc.want {
			t.Errorf("%q: want %d items, got %d: %q", tc.closing, tc.want, len(got), got)
		}
	}
}

func TestAQuestionWithTheArabicMarkBeforeTheListAsksOnlyForTalk(t *testing.T) {
	for _, mark := range []string{"?", "؟"} {
		prompt := "Which of these should we do first" + mark + " Here is the backlog:" + heldBackItems
		if items, held := heldJob(prompt); items != 4 || held == "" {
			t.Errorf("%q: the question before the list is not read as one: %d items, held %q", mark, items, held)
		}
	}
}
