package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func autoQueueItems(prompt string) []string {
	return promptJobOf(prompt).items
}

func TestAutoQueueItems(t *testing.T) {
	pad := "Here is some context about the project so that the prompt is long enough for the detector to consider it: " + strings.Repeat("the codebase is a Node service with a Postgres database and a React front end. ", 2)
	cases := []struct {
		name   string
		prompt string
		want   int
	}{
		{"bullet list", pad + "\n- add input validation to the signup form\n- write tests for the payments module\n- update the README for the new CLI flags\n", 3},
		{"numbered list with checked item", pad + "\n1. [x] add input validation to the signup form\n2. [ ] write tests for the payments module\n3. [ ] update the README for the new CLI flags\n4. [ ] deploy the service to staging\n", 3},
		{"list of questions", pad + "\nCould you walk me through it?\n- where is the session cookie set?\n- why does the redirect loop happen?\n- is the CSRF token validated on every request?\n", 0},
		{"bug report with pasted log", pad + "\nI'm getting an error when I run the integration tests.\nHere is the exact output from the terminal:\nTypeError: cannot read properties of undefined\n    at Object.<anonymous> (test/payments.test.js:12:5)\n    at Module._compile (node:internal/modules/cjs/loader:1256:14)\nCan you help me figure out what is wrong?\n", 0},
		{"repro steps", pad + "\nSteps to reproduce:\n1. open the settings page in the app\n2. click the save button twice quickly\n3. watch the spinner never disappear\nExpected: the form saves once.\n", 0},
		{"context lines then one task", pad + "\nOur app is a marketplace for used bikes.\nUsers can post listings with photos.\nSellers get paid through Stripe Connect.\nPlease fix the checkout bug in the cart page.\n", 0},
		{"line per step", pad + "\nBuild a REST API for the todo app using Express.\nAdd JWT authentication with refresh tokens.\nWrite integration tests for every endpoint.\nDeploy the service to Fly.io with a health check.\n", 4},
		{"paragraph with sequence words", "Build a REST API for the todo app using Express and Postgres. Add JWT authentication with refresh tokens to it. Then write integration tests for every endpoint we expose. After that wire up a GitHub Actions workflow for the tests. Finally deploy the service to Fly.io with a health check endpoint.", 5},
		{"paragraph without sequence words", "Build a REST API for the todo app using Express and Postgres. Add JWT authentication with refresh tokens to it. Write integration tests for every endpoint we expose. Wire up a GitHub Actions workflow for the tests. Deploy the service to Fly.io with a health check endpoint.", 0},
		{"short prompt", "- add tests\n- fix lint\n- deploy", 0},
		{"turkish list", pad + "\n- kayıt formuna girdi doğrulaması ekle ve hataları göster\n- ödeme modülü için birim testlerini yaz\n- yeni CLI bayrakları için README dosyasını güncelle\n", 3},
		{"turkish question", pad + "\n- kayıt formuna girdi doğrulaması ekle ve hataları göster\n- ödeme modülü için birim testlerini yaz\n- yeni CLI bayrakları için README dosyasını güncelle\nBunları nasıl yapmalıyım sence?", 0},
	}
	for _, tc := range cases {
		got := autoQueueItems(tc.prompt)
		if len(got) != tc.want {
			t.Errorf("%s: want %d items, got %d: %q", tc.name, tc.want, len(got), got)
		}
	}
}

func TestCleanItem(t *testing.T) {
	if got := cleanItem("[x] already done thing"); got != "" {
		t.Errorf("checked item should be skipped, got %q", got)
	}
	if got := cleanItem("[ ] open thing to do"); got != "open thing to do" {
		t.Errorf("unchecked box should be stripped, got %q", got)
	}
	if got := cleanItem("[noctis] keep the tag"); got != "[noctis] keep the tag" {
		t.Errorf("bracketed word must survive, got %q", got)
	}
}

func TestCleanItemShortensALongItemWithoutRewritingItsBytes(t *testing.T) {
	long := "fix\xffthe parser " + strings.Repeat("and the lexer ", 30)
	got := cleanItem(long)
	if !strings.HasSuffix(got, "…") || !strings.HasPrefix(long, strings.TrimSuffix(got, "…")) {
		t.Fatalf("the shortened item is not the start of the text: %q", got)
	}
	if runes := utf8.RuneCountInString(got); runes != 201 {
		t.Fatalf("the shortened item has %d runes, want 201", runes)
	}
	prompt := "- fix\xffA1 " + strings.Repeat("the parser module ", 14) + "\n- fix\xffB2 " + strings.Repeat("the lexer module ", 14) + "\n- fix\xffC3 " + strings.Repeat("the build module ", 14) + "\n"
	items := autoQueueItems(prompt)
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d: %q", len(items), items)
	}
	for _, item := range items {
		if !strings.HasPrefix(item, "fix\xff") {
			t.Fatalf("item %q does not start with the prompt's own bytes", item)
		}
	}
}

var bugReportsByLanguage = map[string]string{
	"en": "There is a bug on the settings page that has been driving me crazy since yesterday. Steps to reproduce:\n1. Open the settings page with a brand new user account\n2. Change the language to French and save the changes\n3. Reload the page in a private browser window\n4. Look at the top menu, it is still in English\nExpected: the menu in French. Actual: the menu in English.",
	"tr": "Ayarlar sayfasında dünden beri beni çıldırtan bir hata var. Yeniden üretmek için:\n1. Ayarlar sayfasını yepyeni bir kullanıcı hesabıyla aç\n2. Dili Fransızca yap ve değişiklikleri kaydet\n3. Sayfayı tarayıcının gizli penceresinde yeniden yükle\n4. Üstteki menüye bak, hâlâ İngilizce duruyor\nBeklenen: menü Fransızca. Gerçekleşen: menü İngilizce.",
	"de": "Auf der Einstellungsseite gibt es einen Fehler, der mich seit gestern verrückt macht. Schritte zum Reproduzieren:\n1. Öffne die Einstellungsseite mit einem ganz neuen Benutzerkonto\n2. Stelle die Sprache auf Französisch und speichere die Änderungen\n3. Lade die Seite in einem privaten Browserfenster neu\n4. Schau auf das obere Menü, es ist immer noch Englisch\nErwartet: das Menü auf Französisch. Tatsächlich: das Menü auf Englisch.",
	"fr": "Il y a un bug sur la page des paramètres qui me rend fou depuis hier. Étapes pour reproduire :\n1. Ouvre la page des paramètres avec un tout nouveau compte utilisateur\n2. Change la langue en allemand et enregistre les modifications\n3. Recharge la page dans une fenêtre de navigation privée\n4. Regarde le menu du haut, il est toujours en anglais\nRésultat attendu : le menu en allemand. Résultat obtenu : le menu en anglais.",
	"es": "Hay un error en la página de ajustes que me está volviendo loco desde ayer. Pasos para reproducir:\n1. Abre la página de ajustes con una cuenta de usuario nueva\n2. Cambia el idioma a francés y guarda los cambios\n3. Recarga la página en una ventana privada del navegador\n4. Mira el menú de arriba, sigue en inglés\nResultado esperado: el menú en francés. Resultado obtenido: el menú en inglés.",
	"pt": "Tem um bug na página de configurações que está me deixando louco desde ontem. Passos para reproduzir:\n1. Abra a página de configurações com uma conta de usuário nova\n2. Mude o idioma para francês e salve as alterações\n3. Recarregue a página em uma janela anônima do navegador\n4. Olhe o menu de cima, ele continua em inglês\nResultado esperado: o menu em francês. Resultado obtido: o menu em inglês.",
	"it": "C'è un bug nella pagina delle impostazioni che mi fa impazzire da ieri. Passi per riprodurre:\n1. Apri la pagina delle impostazioni con un account utente nuovo\n2. Cambia la lingua in francese e salva le modifiche\n3. Ricarica la pagina in una finestra di navigazione privata\n4. Guarda il menu in alto, è ancora in inglese\nRisultato atteso: il menu in francese. Risultato ottenuto: il menu in inglese.",
	"nl": "Er zit een bug in de instellingenpagina waar ik sinds gisteren gek van word. Stappen om te reproduceren:\n1. Open de instellingenpagina met een gloednieuw gebruikersaccount\n2. Zet de taal op Frans en sla de wijzigingen op\n3. Herlaad de pagina in een privévenster van de browser\n4. Kijk naar het menu bovenaan, het is nog steeds Engels\nVerwacht resultaat: het menu in het Frans. Werkelijk resultaat: het menu in het Engels.",
	"pl": "Na stronie ustawień jest błąd, który doprowadza mnie do szału od wczoraj. Kroki do odtworzenia:\n1. Otwórz stronę ustawień na zupełnie nowym koncie użytkownika\n2. Zmień język na francuski i zapisz zmiany\n3. Odśwież stronę w prywatnym oknie przeglądarki\n4. Spójrz na górne menu, nadal jest po angielsku\nOczekiwany rezultat: menu po francusku. Rzeczywisty rezultat: menu po angielsku.",
	"ru": "На странице настроек есть ошибка, которая сводит меня с ума со вчерашнего дня. Шаги воспроизведения:\n1. Открой страницу настроек под совершенно новой учётной записью\n2. Поменяй язык на французский и сохрани изменения\n3. Перезагрузи страницу в приватном окне браузера\n4. Посмотри на верхнее меню, оно всё ещё на английском\nОжидаемый результат: меню на французском. Фактический результат: меню на английском.",
	"ja": "設定ページのバグで昨日からずっと困っています。新しいアカウントだと毎回起きるので、ほかのユーザーにも影響が出ていると思います。再現手順:\n1. まったく新しい test アカウントで Settings ページを開く\n2. Language を French に変えて Save ボタンを押す\n3. Chrome の Incognito ウィンドウでページを再読み込みする\n4. 上の Menu を見ると、まだ English のままになっている\n期待される結果: Menu がフランス語で表示される。実際の結果: Menu は英語のまま表示される。サーバーのログにはエラーが何も出ていません。",
	"zh": "设置页面有个 bug，从昨天开始一直让我很头疼。用全新的账号每次都会出现，所以我觉得其他用户应该也受到了影响，客服那边已经收到了好几条投诉。复现步骤:\n1. 用一个全新的 test 账号打开 Settings 页面\n2. 把 Language 改成 French，然后点 Save 保存\n3. 在 Chrome 的 Incognito 窗口里重新加载这个页面\n4. 看一下顶部的 Menu，还是 English 的\n预期结果: Menu 显示为法语。实际结果: Menu 仍然显示为英语。服务器的日志里没有任何错误，浏览器的控制台里也什么都没有，缓存也已经清过了。",
	"ko": "설정 페이지에 버그가 있어서 어제부터 정말 미치겠어요. 새 계정으로 할 때마다 매번 생기니까 다른 사용자들도 영향을 받고 있을 거예요. 캐시도 지워 봤는데 똑같아요. 재현 단계:\n1. 완전히 새 사용자 계정으로 설정 페이지를 연다\n2. 언어를 프랑스어로 바꾸고 변경 사항을 저장한다\n3. 브라우저의 비공개 창에서 페이지를 새로 고친다\n4. 위쪽 메뉴를 보면 여전히 영어로 되어 있다\n예상 결과: 메뉴가 프랑스어로 나온다. 실제 결과: 메뉴가 계속 영어로 나온다.",
	"ar": "هناك خطأ في صفحة الإعدادات يزعجني كثيرا منذ الأمس. خطوات إعادة إنتاج المشكلة:\n1. افتح صفحة الإعدادات بحساب مستخدم جديد تماما\n2. غير اللغة إلى الفرنسية واحفظ التغييرات\n3. أعد تحميل الصفحة في نافذة تصفح خاصة في المتصفح\n4. انظر إلى القائمة العلوية، ما زالت باللغة الإنجليزية\nالنتيجة المتوقعة: القائمة بالفرنسية. النتيجة الفعلية: القائمة بالإنجليزية.",
	"id": "Ada bug di halaman pengaturan yang bikin saya pusing sejak kemarin. Langkah untuk mereproduksi:\n1. Buka halaman pengaturan dengan akun pengguna yang benar-benar baru\n2. Ubah bahasa ke Prancis lalu simpan perubahannya\n3. Muat ulang halaman di jendela penyamaran browser\n4. Lihat menu di bagian atas, masih dalam bahasa Inggris\nHasil yang diharapkan: menu dalam bahasa Prancis. Hasil sebenarnya: menu dalam bahasa Inggris.",
}

func TestABugReportWithReproductionStepsGetsNoChecklistInAnyLanguage(t *testing.T) {
	if len(bugReportsByLanguage) != len(catalogTable()) {
		t.Fatalf("bug reports in %d languages, the catalog has %d", len(bugReportsByLanguage), len(catalogTable()))
	}
	cfg, project := queueTrustSandbox(t, false)
	for lang, report := range bugReportsByLanguage {
		sid := "bug-report-" + lang
		output := startQueue(t, cfg, sid, project, report)
		if record := getMap(getMap(readState(), "autoQueues"), sid); record != nil {
			t.Errorf("%s: the bug report became a checklist of %v steps (%q)", lang, record["items"], getString(output, "systemMessage"))
		}
		if stop := stopHookOutput(t, activeStop(sid, project), cfg); getString(stop, "decision") == "block" {
			t.Errorf("%s: the Stop hook drives Claude through the reproduction steps: %.160q", lang, getString(stop, "reason"))
		}
	}
}
