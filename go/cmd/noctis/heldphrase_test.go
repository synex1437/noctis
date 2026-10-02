package main

import "testing"

func TestAnOtherWordInTheNextSentenceOrInsideAWordDoesNotUndoAProhibition(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	for _, tc := range []struct{ lang, lead string }{
		{"es", "Esta es la lista para la próxima semana. No implementes nada todavía. Más adelante lo revisamos juntos."},
		{"id", "Ini daftar tugas untuk minggu depan. Jangan kerjakan apa pun dulu, melainkan tunggu konfirmasi saya besok."},
		{"id-no-comma", "Ini daftar tugas untuk minggu depan. Jangan kerjakan apa pun dulu melainkan tunggu konfirmasi saya besok."},
		{"it", "Ecco la lista per la prossima settimana. Non implementare nulla, altrimenti rompiamo la demo di venerdì."},
		{"ru", "Вот список задач на следующую неделю. Пока ничего не делай, вдруг заказчик передумает."},
		{"de", "Hier ist die Liste für nächste Woche. Noch nichts implementieren, andere Teams müssen erst zustimmen."},
		{"pl", "To jest lista zadań na przyszły tydzień. Nic nie zmieniaj, więcej szczegółów podam jutro."},
		{"es-jamas", "Esta es la lista para la próxima semana. No toques nada jamás sin preguntarme antes."},
	} {
		if items, held := heldJob(tc.lead + heldBackItems); items != 4 || held == "" {
			t.Errorf("%s: %q is not read as a prohibition: %d items, held %q", tc.lang, tc.lead, items, held)
		}
	}
	expectNoChecklist(t, cfg, "else-es", project, "Esta es la lista para la próxima semana. No implementes nada todavía. Más adelante lo revisamos juntos."+heldBackItems)
}

func TestAProhibitionThatGoesOnToAnythingElseIsStillALimitOnTheWork(t *testing.T) {
	for _, lead := range []string{
		"Haz todos estos puntos en orden y no toques nada más.",
		"Haz todos estos puntos en orden y no toques nada además del README.",
		"Сделай все пункты по порядку и не трогай ничего другого.",
		"Kerjakan semua tugas di bawah ini secara berurutan dan jangan ubah file lain.",
		"Arbeite die Liste der Reihe nach ab und ändere nichts anderes.",
		"按顺序完成下面所有任务，不要修改任何其他文件。",
	} {
		if items, held := heldJob(lead + heldBackItems); items != 4 || held != "" {
			t.Errorf("%q: a limit on the work held the job back: %d items, held %q", lead, items, held)
		}
	}
}

func TestADescriptionThatHoldsTheWordsOfAProhibitionKeepsTheChecklist(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	fr := "\n- fais en sorte que le bouton Enregistrer sauvegarde le formulaire et affiche un message\n- corrige le blocage de la page de connexion quand le mot de passe est faux\n- ajoute un test pour le bouton Enregistrer et un pour la connexion\n- mets à jour le changelog avec les deux corrections\n"
	for _, tc := range []struct{ sid, prompt string }{
		{"desc-ko", "지난 스프린트에서 아직 구현하지 않은 기능이 네 개 남았습니다. 아래 순서대로 하나씩 모두 구현하고, 각 항목이 끝날 때마다 테스트를 실행해 주세요.\n- 인증 서비스에 분당 요청 수 제한이 있는 새 로그인 엔드포인트를 추가하고 실패 응답 형식을 문서에 맞추기\n- 결제 모듈을 새 청구 클라이언트를 사용하도록 다시 작성하고 기존 재시도 로직은 그대로 유지하기\n- 모든 테이블을 롤백 스크립트와 함께 새 스키마로 이전하고 이전 전후의 행 수를 비교하기\n- 워커와 스케줄러에서 예전 로깅 코드를 제거하고 새 구조화 로거로 바꾸기\n"},
		{"desc-ru", "Пока ничего не работает на проде, поэтому сделай по порядку:\n- добавь эндпоинт входа с ограничением частоты в сервис авторизации\n- перепиши модуль оплаты на новый клиент биллинга\n- перенеси все таблицы на новую схему со скриптом отката\n- убери старое логирование из воркера и планировщика\n"},
		{"desc-de", "Refaktoriere die folgenden Module der Reihe nach; am Verhalten darf sich nichts ändern.\n- zieh die Validierung aus dem Login-Handler in eine eigene Funktion\n- teile das Zahlungsmodul in Client und Service auf\n- ersetze die globalen Variablen im Scheduler durch eine Struktur\n- entferne das alte Logging aus dem Worker und dem Scheduler\n"},
		{"desc-fr", "J'ai vidé le cache mais ça ne change rien, la page reste bloquée. Corrige ces problèmes dans l'ordre :" + fr},
		{"desc-it", "Quando clicco Salva il pulsante sembra non fare nulla. Sistema questi problemi uno dopo l'altro:\n- fai in modo che il pulsante Salva salvi il modulo e mostri un messaggio\n- correggi il blocco della pagina di login con la password sbagliata\n- aggiungi un test per il pulsante Salva e uno per il login\n- aggiorna il changelog con entrambe le correzioni\n"},
	} {
		startQueue(t, cfg, tc.sid, project, tc.prompt)
		if record := getMap(getMap(readState(), "autoQueues"), tc.sid); numberOr(record, "items", 0) != 4 {
			t.Errorf("%s: the 4-step job got no checklist; noctis why: %q", tc.sid, journaledReason(tc.sid, "no-auto-queue"))
		}
	}
}

func TestTheImperativeFormsOfThoseProhibitionsStillHoldTheJobBack(t *testing.T) {
	for _, lead := range []string{
		"아래 항목은 아직 구현하지 마세요. 각 항목에 걸리는 시간을 알려 주세요.",
		"아래 항목은 하나도 구현하지 말고 위험한 부분을 정리해 주세요.",
		"아무것도 구현하지 마시고 순서를 정해 주세요.",
		"Пока ничего не делай, только оцени каждый пункт.",
		"Пока ничего не пиши, сначала оцени каждый пункт.",
		"Пока ничего не исправляй, сначала опиши каждый пункт.",
		"Bitte noch nichts ändern, ich brauche zuerst eine Einschätzung.",
		"Am Code bitte nichts ändern, nur schätzen.",
		"Ne change rien pour l'instant, donne-moi juste une estimation.",
		"Non fare nulla per ora, stima solo ogni punto.",
	} {
		if items, held := heldJob(lead + heldBackItems); items != 4 || held == "" {
			t.Errorf("%q is no longer read as a prohibition: %d items, held %q", lead, items, held)
		}
	}
}
