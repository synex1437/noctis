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
		"Ändere nichts anderes als die README.",
		"Ändere keine Dateien bis auf die README.",
		"No toques nada excepto el README.",
		"Raak geen bestanden aan behalve de README.",
		"Non toccare nient'altro.",
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
		"Mejor no toques nada.",
		"No projeto não mexa em nada.",
		"No hagas nada excepto estimar cada punto.",
		"Ne touche à rien sauf si je te le dis.",
		"Hier ist die Liste. Ändere nichts anderes als zu schätzen.",
		"このスクリプトは何も変更しないでください。",
		"今日は何も変更しない。",
		"لا تنفذ أي شيء قبل مراجعة التغييرات.",
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
		"Не трогай файлы, связанные с миграциями.",
		"Ne touche pas aux fichiers de configuration.",
		"Ändere die Dateien nicht, die schon getestet sind.",
		"Ändere die Dateien nicht im Ordner tests.",
		"No modifiques ningún archivo de configuración.",
		"Не меняй никакие файлы конфигурации.",
		"Jangan ubah file konfigurasi.",
		"로그인 버그를 수정하지 마세요.",
		"ログイン画面を変更しないでください。",
		"不要修改登录页面。",
		"Ändere keine Dateien am Login-Modul.",
		"No toques los archivos de la próxima versión.",
		"Не трогай файлы на сервере.",
		"Ne pas modifier les fichiers de migration.",
		"No tocar los archivos de configuración.",
		"Не трогать файлы миграций.",
		"Nie ruszać plików konfiguracyjnych.",
		"De bestanden niet aanpassen in de map tests.",
		"Keine Änderungen am Login-Modul vornehmen.",
		"Tidak usah mengubah file konfigurasi.",
		"يرجى عدم تعديل ملفات الترحيل.",
		"ログイン画面の変更禁止。",
		"禁止修改登录页面。",
		"로그인 페이지 수정 금지.",
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
		"No toques los archivos del proyecto Atlas.",
		"No modifiques ninguna línea del código.",
		"لا تعدل ملفات المشروع.",
		"Не трогай файлы.",
		"Jangan ubah file di proyek ini.",
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
		"Der Login-Knopf geht nicht, sie können noch nicht anfangen.",
		"O botão de salvar não faz nada quando clico.",
		"A API não faz nada.",
		"El botón de guardar no hace nada.",
		"Le bouton ne fait rien.",
		"Il pulsante non fa niente.",
		"الصفحة الجديدة لا تغير شيئا.",
		"このスクリプトは何も変更しない。",
		"Het script kan niets aanpassen.",
		"Das Tool kann keine Änderungen vornehmen.",
	} {
		if items, held := heldJob(lead + heldBackItems); items != 4 || held != "" {
			t.Errorf("%q: a statement held the job back: %d items, held %q", lead, items, held)
		}
	}
}

func TestTheWiderPhraseListsHoldTheJobBack(t *testing.T) {
	for _, lead := range []string{
		"Implementeer nog niets.",
		"Raak de bestanden niet aan.",
		"Nie ruszaj plików.",
		"Не меняй ни строчки.",
		"Ничего не надо делать.",
		"Не надо ничего делать.",
		"Non toccare il codice.",
		"N'écrivez pas de code.",
		"Ne mettez rien en œuvre pour l'instant.",
		"Ne code rien.",
		"Ne commencez pas encore.",
		"Fang bitte noch nicht an.",
		"Setz das noch nicht um.",
		"Ändere die Dateien nicht.",
		"Jangan ubah kode dulu.",
		"Jangan sentuh file apa pun.",
		"Jangan ubah filenya dulu.",
		"これらを実装しないでください。",
		"コードには触れないでください。",
		"이것들을 구현하지 마세요.",
		"파일을 수정하지 마세요.",
		"这些先不要实现。",
		"不要实现这些。",
	} {
		if items, held := heldJob(lead + heldBackItems); items != 4 || !strings.HasPrefix(held, "the prompt forbids") {
			t.Errorf("%q is not read as a prohibition: %d items, held %q", lead, items, held)
		}
	}
}

func TestAnInfinitiveOrAFormalProhibitionHoldsTheJobBack(t *testing.T) {
	for _, lead := range []string{
		"Merci de ne rien modifier.",
		"Veuillez ne pas toucher au code.",
		"Ne pas modifier les fichiers.",
		"Ne rien implémenter pour l'instant.",
		"N'implémente pas pour l'instant.",
		"Ne pas encore implémenter.",
		"Ne toucher à rien.",
		"Ne rien faire pour le moment.",
		"No tocar nada.",
		"Favor de no modificar nada.",
		"Por favor, no implementar nada todavía.",
		"No tocar los archivos.",
		"Favor não mexer em nada.",
		"Por favor, não alterar nada ainda.",
		"Não implementar nada por enquanto.",
		"Peço para não mexer em nada.",
		"Ничего не трогать.",
		"Просьба ничего не менять.",
		"Прошу не трогать файлы.",
		"Пока ничего не реализовывать.",
		"Proszę nic nie zmieniać.",
		"Nie ruszać plików.",
		"Na razie nic nie implementować.",
		"Nie zmieniać kodu.",
		"Graag niets aanpassen.",
		"Gelieve niets te wijzigen.",
		"De bestanden niet aanpassen.",
		"Geen code aanpassen.",
		"Bitte keine Änderungen vornehmen.",
		"Nichts verändern.",
		"Tidak usah mengubah apa pun.",
		"Jangan melakukan perubahan apa pun.",
		"يرجى عدم تعديل الملفات.",
		"الرجاء عدم تنفيذ أي شيء الآن.",
		"これらは実装しないこと。",
		"コード変更禁止。",
		"禁止修改任何文件。",
		"不得修改代码。",
		"코드 수정 금지.",
	} {
		if items, held := heldJob(lead + heldBackItems); items != 4 || !strings.HasPrefix(held, "the prompt forbids") {
			t.Errorf("%q is not read as a prohibition: %d items, held %q", lead, items, held)
		}
	}
}

func TestAProhibitionInAPurposeClauseOrAfterSeemsKeepsTheChecklist(t *testing.T) {
	for _, lead := range []string{
		"Travaille sur une copie pour ne rien modifier dans la base.",
		"Travaille sur une copie afin de ne pas toucher au code.",
		"Crea una rama nueva para no tocar nada en main.",
		"Trabaja en una copia para no modificar los archivos.",
		"Usa uma branch nova para não mexer em nada.",
		"Usa uma cópia pra não mexer nos arquivos.",
		"Lavora su una copia per non toccare il codice.",
		"Lavora su un branch separato in modo da non toccare il codice.",
		"Работай в отдельной ветке, чтобы ничего не менять.",
		"Pracuj na kopii, żeby nic nie zmieniać.",
		"Pracuj na kopii, aby nie ruszać plików.",
		"Haz todo en orden, de forma que no toques nada en producción.",
		"Faça tudo em ordem, para que não mexa em nada na produção.",
		"Le bouton Enregistrer semble ne rien faire.",
		"El botón parece no hacer nada.",
		"O botão parece não fazer nada.",
	} {
		if items, held := heldJob(lead + heldBackItems); items != 4 || held != "" {
			t.Errorf("%q: a purpose clause or a statement held the job back: %d items, held %q", lead, items, held)
		}
	}
}

func TestAStatementWithCanOrShouldBeforeAnInfinitiveKeepsTheChecklist(t *testing.T) {
	for _, lead := range []string{
		"Ça risque de ne rien changer.",
		"Le script risque de ne rien changer.",
		"Cette option permet de ne rien modifier.",
		"Le correctif devrait ne rien changer.",
		"Le bouton peut ne rien faire.",
		"El botón puede no hacer nada.",
		"El cambio podría no hacer nada.",
		"El script suele no tocar nada.",
		"O botão pode não fazer nada.",
		"A atualização deve não alterar nada.",
		"Il pulsante potrebbe non fare niente.",
		"Il pulsante può non fare nulla.",
		"Скрипт может ничего не менять.",
		"Кнопка может ничего не делать.",
		"Skrypt może nic nie zmieniać.",
		"Przycisk może nic nie robić.",
	} {
		if items, held := heldJob(lead + heldBackItems); items != 4 || held != "" {
			t.Errorf("%q: a statement held the job back: %d items, held %q", lead, items, held)
		}
	}
}

func TestAnInfinitiveAfterAMustOrAnAskStillHoldsTheJobBack(t *testing.T) {
	for _, lead := range []string{
		"Vous devez ne rien modifier.",
		"Tu dois ne rien toucher.",
		"Claude peut ne rien modifier.",
		"Il faut ne rien modifier.",
		"Il est important de ne rien modifier.",
		"Je te demande de ne rien modifier.",
		"Debe no tocar nada.",
		"Usted debe no tocar nada.",
		"Debes no tocar nada.",
		"Hay que no tocar nada.",
		"Te pido no tocar nada.",
		"Você deve não mexer em nada.",
		"Ti chiedo di non toccare nulla.",
		"Ты должен ничего не трогать.",
		"Нужно ничего не трогать.",
		"Важно, чтобы ничего не менять.",
		"Musisz nic nie zmieniać.",
		"Trzeba nic nie zmieniać.",
		"Ważne, żeby nic nie zmieniać.",
		"Proszę, żeby nic nie zmieniać.",
	} {
		if items, held := heldJob(lead + heldBackItems); items != 4 || !strings.HasPrefix(held, "the prompt forbids") {
			t.Errorf("%q is not read as a prohibition: %d items, held %q", lead, items, held)
		}
	}
}

func TestAProhibitionWithoutItsAccentsOrWithAWordInsideIsQuotedAsWritten(t *testing.T) {
	for _, tc := range []struct{ lead, quote string }{
		{"No implementes nada todavia.", "No implementes nada"},
		{"Nao mexa em nada ainda.", "Nao mexa em nada"},
		{"Nie wdrazaj jeszcze.", "Nie wdrazaj jeszcze"},
		{"NO TOQUES NADA TODAVÍA.", "NO TOQUES NADA"},
		{"No toques todavía nada.", "No toques todavía nada"},
		{"Ändere bitte nichts.", "Ändere bitte nichts"},
		{"Ничего пока не трогай.", "Ничего пока не трогай"},
		{"这些先不要实现。", "先不要实现"},
	} {
		if items, held := heldJob(tc.lead + heldBackItems); items != 4 || !strings.Contains(held, `"`+tc.quote+`"`) {
			t.Errorf("%q: want the quote %q: %d items, held %q", tc.lead, tc.quote, items, held)
		}
	}
}

func TestAProhibitionUntilALaterTimeStillHoldsTheJobBack(t *testing.T) {
	for _, lead := range []string{
		"No cambies nada hasta mañana.",
		"No toques los archivos esta semana.",
		"Não altere nada até amanhã.",
		"N'implémente rien avant demain.",
		"Non modificare nulla fino a domani.",
		"Ändere nichts bis morgen.",
		"Ändere keine Dateien bis auf weiteres.",
		"Verander niets tot morgen.",
		"Nic nie zmieniaj do jutra.",
		"Ничего не меняй до завтра.",
		"Не трогай файлы до понедельника.",
		"Jangan ubah apa pun sampai besok.",
		"Nie ruszaj plików do jutra.",
		"Não mexa nos arquivos amanhã.",
		"Ne touche pas aux fichiers demain.",
		"Non toccare i file domani.",
		"Не трогай файлы завтра.",
		"Jangan ubah file besok.",
		"لا تعدل الملفات غدا.",
		"No toques los archivos el lunes.",
		"Ändere keine Dateien am Montag.",
		"No toques los archivos la próxima semana.",
		"Non toccare i file la prossima settimana.",
		"Ändere keine Dateien nächste Woche.",
		"Raak de bestanden niet aan volgende week.",
		"Nie ruszaj plików w przyszłym tygodniu.",
		"Не трогай файлы на следующей неделе.",
	} {
		if items, held := heldJob(lead + heldBackItems); items != 4 || !strings.HasPrefix(held, "the prompt forbids") {
			t.Errorf("%q is not read as a prohibition: %d items, held %q", lead, items, held)
		}
	}
}

func TestAnArabicPhraseIsMatchedOnlyAsAWholeWordOrWithAnEnding(t *testing.T) {
	for _, tc := range []struct {
		lead string
		held bool
	}{
		{"لا تفعل شيئا الآن.", true},
		{"لا تفعل شيء الآن.", true},
		{"لا تعدل ملفاتي.", true},
		{"ولا تنفذ أي بند.", true},
		{"لا تنفذ أيضا الخطوة الثالثة قبل الاجتماع.", false},
		{"لا تبدأ العملية الجديدة.", false},
	} {
		if items, held := heldJob(tc.lead + heldBackItems); items != 4 || (held != "") != tc.held {
			t.Errorf("%q: want held %v: %d items, held %q", tc.lead, tc.held, items, held)
		}
	}
}

func TestARequestForOnlyAnEstimateInAPhraseLanguageHoldsTheJobBack(t *testing.T) {
	for _, lead := range []string{
		"Ich brauche nur eine Schätzung für diese Punkte.",
		"Ik wil alleen een schatting voor deze punten.",
		"Potrzebuję tylko wyceny tych punktów.",
		"Мне нужна только оценка по этим пунктам.",
		"Solo quiero una estimación de estos puntos.",
		"Só quero uma estimativa destes itens.",
		"Je veux juste une estimation pour ces points.",
		"Voglio solo una stima per questi punti.",
		"Saya hanya butuh perkiraan untuk tugas-tugas ini.",
	} {
		if items, held := heldJob(lead + heldBackItems); items != 4 || !strings.HasPrefix(held, "the prompt asks only") {
			t.Errorf("%q is not read as a request for an estimate: %d items, held %q", lead, items, held)
		}
	}
}
