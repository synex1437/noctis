package main

import (
	"strings"
	"testing"
)

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
		"Por favor, não faz nada ainda; primeiro quero revisar a lista.",
		"Por favor no cambie nada todavía; primero revisaré la lista.",
		"Surtout ne modifie rien, je relis tout demain.",
		"Pendant la démo ne touche à rien.",
		"Durante a demo não mexa em nada, primeiro vou revisar.",
		"Bitte noch nicht anfangen, ich muss erst mit dem Team reden.",
		"Sie können noch nicht anfangen, ich prüfe erst alles.",
		"Ce soir ne modifie rien, je relis tout demain.",
		"La semaine prochaine ne modifie rien, on gèle le code.",
		"L'après-midi ne touche à rien, la démo est à 16 h.",
		"Esta noche no cambie nada; primero revisaré la lista.",
		"Este año no haga nada con la base de datos.",
		"Este fim de semana não mexa em nada, primeiro vou revisar.",
		"Esta tarde não altere nada, a demo é às 16 h.",
	} {
		if items, held := heldJob(lead + heldBackItems); items != 4 || held == "" {
			t.Errorf("%q is no longer read as a prohibition: %d items, held %q", lead, items, held)
		}
	}
}

func TestARuleOnOnePartOfTheWorkInAPhraseLanguageKeepsTheChecklist(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	for _, lead := range []string{
		"Сделай все пункты по порядку, но не трогай файлы миграций.",
		"按顺序完成下面所有任务，不要修改任何迁移文件。",
		"Haz todos estos puntos en orden y no toques los archivos de migración.",
		"Fais tous ces points dans l'ordre et ne touche à aucun fichier de migration.",
		"Faça todos os itens em ordem e não mexa nos arquivos de migração.",
		"Fai tutti i punti in ordine e non toccare i file di migrazione.",
		"Arbeite die Liste der Reihe nach ab und ändere keine Dateien im Migrationsordner.",
		"Werk de lijst op volgorde af en wijzig geen bestanden in de migratiemap.",
		"Zrób wszystkie punkty po kolei i nie ruszaj plików migracji.",
		"نفذ كل البنود بالترتيب ولا تعدل أي ملف في مجلد الترحيل.",
		"Kerjakan semua tugas secara berurutan dan jangan ubah file migrasi.",
		"順番にすべての項目を実装してください。マイグレーションファイルを変更しないでください。",
		"아래 항목을 순서대로 모두 구현해 주세요. 마이그레이션 파일은 수정하지 마세요.",
		"Haz todos estos puntos en orden y no toques los archivos del proyecto de migración.",
		"Fais tous ces points dans l'ordre et ne touche pas aux fichiers qui gèrent les migrations.",
		"Fais tous ces points dans l'ordre et ne touche à aucun test.",
		"Fai tutti i punti in ordine e non toccare il codice del modulo di pagamento.",
		"Kerjakan semua tugas secara berurutan dan jangan ubah file di folder migrasi.",
	} {
		if items, held := heldJob(lead + heldBackItems); items != 4 || held != "" {
			t.Errorf("%q: a rule on one part of the work held the job back: %d items, held %q", lead, items, held)
		}
	}
	startQueue(t, cfg, "narrow-ru", project, "Сделай все пункты по порядку, но не трогай файлы миграций."+heldBackItems)
	if record := getMap(getMap(readState(), "autoQueues"), "narrow-ru"); numberOr(record, "items", 0) != 4 {
		t.Errorf("the 4-step job with a rule on the migration files got no checklist; noctis why: %q", journaledReason("narrow-ru", "no-auto-queue"))
	}
}

func TestARuleOnAllTheFilesOrTheCodeStillHoldsTheListBack(t *testing.T) {
	for _, lead := range []string{
		"Пока не трогай файлы, я сначала всё проверю.",
		"不要修改任何文件，我先自己看一下。",
		"No toques los archivos todavía, primero quiero revisarlos yo.",
		"Ne touche à aucun fichier pour l'instant, je dois d'abord les relire.",
		"Não mexa nos arquivos ainda, primeiro vou revisar tudo.",
		"Non toccare il codice per ora, prima devo rivedere tutto.",
		"Schreib noch keinen Code, ich muss das erst mit dem Team besprechen.",
		"Schrijf nog geen code, ik moet het eerst met het team bespreken.",
		"Nie ruszaj kodu na razie, najpierw muszę to przejrzeć.",
		"لا تلمس أي ملف الآن، سأراجع كل شيء أولاً.",
		"Jangan ubah file apa pun dulu, saya mau cek semuanya.",
		"既存のファイルを変更しないでください。まず全体を確認します。",
		"아직 파일을 수정하지 마세요. 먼저 전체를 검토하겠습니다.",
		"No toques los archivos en absoluto, primero quiero revisarlos.",
		"No toques el código de ninguna manera; primero lo reviso yo.",
		"No toques los archivos mientras reviso la lista.",
		"No toques los archivos del proyecto todavía, primero quiero revisarlos.",
		"No toques ningún archivo del repositorio por ahora.",
		"No implementes ninguna funcionalidad por ahora.",
		"Não mexa nos arquivos de jeito nenhum, primeiro vou revisar.",
		"Não mexa no código enquanto eu reviso a lista.",
		"Não mexa nos arquivos do projeto ainda.",
		"Ne touche pas aux fichiers du tout, je dois d'abord les relire.",
		"Ne touche pas au code tant que je n'ai pas relu la liste.",
		"Ne touche pas au code sous aucun prétexte.",
		"Ne modifie aucune ligne de code pour l'instant.",
		"N'implémente aucune fonctionnalité pour l'instant.",
		"Non toccare il codice per nessun motivo, prima devo rivedere tutto.",
		"Non toccare il codice mentre rivedo la lista.",
		"Non toccare i file del progetto per ora.",
		"Schreib keinen Code solange ich die Liste prüfe.",
		"Ändere keine Dateien im Projekt, ich prüfe erst alles.",
		"Wijzig geen bestanden terwijl ik de lijst nakijk.",
		"Wijzig geen bestanden in het project, ik kijk eerst alles na.",
		"Nie ruszaj kodu w żadnym wypadku.",
		"Nie ruszaj plików projektu, najpierw sprawdzę.",
		"Не трогай код совсем.",
		"Не трогай код во время ревью.",
		"Не трогай файлы проекта, я сначала всё проверю.",
		"لا تلمس الملفات على الإطلاق.",
		"لا تعدل الملفات أثناء المراجعة.",
		"لا تعدل الملفات في المشروع الآن.",
		"Jangan ubah file selama saya meninjau.",
		"Jangan ubah file proyek dulu.",
		"当面ファイルを変更しないでください。",
		"プロジェクトのファイルを変更しないでください。",
		"오늘 파일을 수정하지 마세요.",
		"프로젝트 파일을 수정하지 마세요.",
		"不要修改任何现有文件。",
		"不要修改任何源代码。",
	} {
		if items, held := heldJob(lead + heldBackItems); items != 4 || !strings.HasPrefix(held, "the prompt forbids") {
			t.Errorf("%q no longer holds the job back: %d items, held %q", lead, items, held)
		}
	}
}

func TestAStatementThatReadsLikeAProhibitionKeepsTheChecklist(t *testing.T) {
	for _, lead := range []string{
		"O botão não faz nada quando clico em Salvar. Corrija estes problemas em ordem:",
		"Necesito que el nuevo script no cambie nada en producción. Haz estos puntos en orden:",
		"Le nouveau cache ne modifie rien, la page reste lente. Corrige ces problèmes dans l'ordre :",
		"Ce soir le nouveau cache ne modifie rien, la page reste lente. Corrige ces problèmes dans l'ordre :",
		"Die Tests können noch nicht anfangen, solange der Server fehlt. Erledige deshalb diese Punkte der Reihe nach:",
	} {
		if items, held := heldJob(lead + heldBackItems); items != 4 || held != "" {
			t.Errorf("%q: a statement held the job back: %d items, held %q", lead, items, held)
		}
	}
}
