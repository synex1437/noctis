package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

type languageSample struct {
	lang   string
	prompt string
}

var languageCorpus = []languageSample{
	{"en", "Please fix the failing test in the auth module and then run the whole suite again"},
	{"en", "Please add the tests and keep going with the refactor of the module"},
	{"en", "please fix the bug in the parser"},
	{"en", "why is the build failing on CI"},
	{"en", "add a --verbose flag to the CLI that prints each request"},
	{"en", "refactor the user service into smaller modules"},
	{"en", "can you explain what this regex does"},
	{"en", "run the tests and tell me what fails"},
	{"en", "the login page is blank after the last deploy"},
	{"en", "write a README section about installing on Windows"},
	{"en", "rename getUserById to findUser everywhere"},
	{"en", "looks good, commit it with a short message"},
	{"en", "there's a race condition in the worker pool, can you find it"},
	{"en", "the bug is in src/auth/token.ts around line 42"},
	{"en", "make the error messages more helpful"},
	{"en", "how do I mock the http client in these tests"},
	{"en", "move the config parsing into its own package"},
	{"en", "try again but keep the public API the same"},
	{"en", "what does this function return when the list is empty"},
	{"en", "update the dependencies and check nothing breaks"},
	{"en", "split this file into three smaller ones"},
	{"en", "add logging around the retry loop"},
	{"en", "is it safe to delete the old migration files"},
	{"en", "check why the docker image is so big"},
	{"en", "review my changes before I push"},

	{"tr", "Testleri ekle ve modülü düzeltmeye devam et lütfen"},
	{"tr", "Dosyadaki hatayı düzelt ve sonra testleri yeniden çalıştır lütfen"},
	{"tr", "merhaba dünya nasılsın"},
	{"tr", "auth.js dosyasındaki hatayı düzelt"},
	{"tr", "testleri çalıştır ve kırmızı olanları düzelt"},
	{"tr", "bu fonksiyonu daha okunabilir hale getir"},
	{"tr", "build neden CI’da kırılıyor"},
	{"tr", "login sayfası deploy'dan sonra boş geliyor"},
	{"tr", "projeye typescript desteği ekle"},
	{"tr", "şu regex ne yapıyor açıklar mısın"},
	{"tr", "commit mesajını daha kısa yaz"},
	{"tr", "bu değişiklikleri incele, bir sorun var mı"},
	{"tr", "veritabanı sorgusu çok yavaş, optimize eder misin"},
	{"tr", "README'ye kurulum adımlarını ekle"},
	{"tr", "getUserById fonksiyonunun adını findUser yap"},
	{"tr", "hata mesajlarını daha anlaşılır yap"},
	{"tr", "tamam, devam et ama public API'yi değiştirme"},
	{"tr", "worker havuzunda bir race condition var, bulabilir misin"},
	{"tr", "bağımlılıkları güncelle ve bir şey bozulmuş mu kontrol et"},
	{"tr", "bu dosyayı üç parçaya böl"},
	{"tr", "retry döngüsüne log ekle"},
	{"tr", "eski migration dosyalarını silmek güvenli mi"},
	{"tr", "docker imajı neden bu kadar büyük"},
	{"tr", "push etmeden önce değişikliklerimi gözden geçir"},
	{"tr", "testleri calistir ve hatalari duzelt"},

	{"de", "Bitte füge die Tests hinzu und mach das Refactoring nicht zu groß"},
	{"de", "Bitte behebe den Fehler in der Datei und führe die Tests danach erneut aus"},
	{"de", "Warum schlägt der Build auf CI fehl?"},
	{"de", "Füge ein --verbose Flag zur CLI hinzu"},
	{"de", "Kannst du die Funktion parseConfig vereinfachen"},
	{"de", "Der Test in auth_test.go ist rot, schau dir das bitte an"},
	{"de", "Die Login-Seite ist nach dem letzten Deploy leer"},
	{"de", "Schreib eine README für das Projekt"},
	{"de", "Benenne getUserById überall in findUser um"},
	{"de", "Sieht gut aus, committe es mit einer kurzen Nachricht"},
	{"de", "Im Worker-Pool gibt es eine Race Condition, kannst du sie finden"},
	{"de", "Mach die Fehlermeldungen verständlicher"},
	{"de", "Wie mocke ich den HTTP-Client in diesen Tests"},
	{"de", "Verschiebe das Config-Parsing in ein eigenes Paket"},
	{"de", "Versuch es nochmal, aber lass die öffentliche API gleich"},
	{"de", "Was gibt diese Funktion zurück, wenn die Liste leer ist"},
	{"de", "Aktualisiere die Abhängigkeiten und prüfe, ob noch alles läuft"},
	{"de", "Teil diese Datei in drei kleinere auf"},
	{"de", "Füge Logging um die Retry-Schleife hinzu"},
	{"de", "Ist es sicher, die alten Migrationen zu löschen"},
	{"de", "Warum ist das Docker-Image so groß"},
	{"de", "Schau dir meine Änderungen an, bevor ich pushe"},
	{"de", "Das funktioniert nicht, der Server startet nicht mehr"},
	{"de", "Was ist so schwer daran"},
	{"de", "Lass die Tests laufen und sag mir, was fehlschlägt"},

	{"fr", "Est-ce que tu peux ajouter les tests pour le module et corriger le bug"},
	{"fr", "Corrige l'erreur dans le fichier et relance les tests s'il te plaît"},
	{"fr", "Pourquoi le build échoue sur la CI ?"},
	{"fr", "Ajoute une option --verbose à la CLI"},
	{"fr", "Tu peux simplifier la fonction parseConfig"},
	{"fr", "Le test dans auth_test.go est rouge, regarde s’il te plaît"},
	{"fr", "La page de connexion est vide depuis le dernier déploiement"},
	{"fr", "Écris un README pour le projet"},
	{"fr", "Renomme getUserById en findUser partout"},
	{"fr", "Ça me va, fais un commit avec un message court"},
	{"fr", "Il y a une race condition dans le pool de workers, tu peux la trouver"},
	{"fr", "Rends les messages d'erreur plus clairs"},
	{"fr", "Comment je mocke le client HTTP dans ces tests"},
	{"fr", "Déplace le parsing de la config dans son propre package"},
	{"fr", "Réessaie mais garde la même API publique"},
	{"fr", "Qu'est-ce que cette fonction renvoie quand la liste est vide"},
	{"fr", "Mets à jour les dépendances et vérifie que rien ne casse"},
	{"fr", "Découpe ce fichier en trois plus petits"},
	{"fr", "Ajoute des logs autour de la boucle de retry"},
	{"fr", "Est-ce qu'on peut supprimer les anciennes migrations sans risque"},
	{"fr", "Pourquoi l'image Docker est si grosse"},
	{"fr", "Relis mes changements avant que je pousse"},
	{"fr", "Ça ne marche pas, le serveur ne démarre plus"},
	{"fr", "tu as raison, on continue"},
	{"fr", "Lance les tests et dis-moi ce qui échoue"},

	{"es", "Por favor agrega las pruebas y no cambies el esquema de la base"},
	{"es", "Por favor corrige el error en el archivo y vuelve a ejecutar las pruebas"},
	{"es", "¿Por qué falla el build en CI?"},
	{"es", "Agrega una opción --verbose a la CLI"},
	{"es", "¿Puedes simplificar la función parseConfig?"},
	{"es", "El test en auth_test.go está en rojo, revísalo por favor"},
	{"es", "La página de login está en blanco desde el último deploy"},
	{"es", "Escribe un README para el proyecto"},
	{"es", "Cambia el nombre de getUserById a findUser en todo el código"},
	{"es", "Se ve bien, haz commit con un mensaje corto"},
	{"es", "Hay una condición de carrera en el pool de workers, ¿puedes encontrarla?"},
	{"es", "Haz que los mensajes de error sean más claros"},
	{"es", "¿Cómo hago un mock del cliente HTTP en estos tests?"},
	{"es", "Mueve el parseo de la configuración a su propio paquete"},
	{"es", "Inténtalo de nuevo pero mantén la misma API pública"},
	{"es", "¿Qué devuelve esta función cuando la lista está vacía?"},
	{"es", "Actualiza las dependencias y comprueba que nada se rompa"},
	{"es", "Divide este archivo en tres más pequeños"},
	{"es", "Agrega logs alrededor del bucle de reintentos"},
	{"es", "no le hagas caso al linter"},
	{"es", "¿Por qué la imagen de Docker es tan grande?"},
	{"es", "Revisa mis cambios antes de que haga push"},
	{"es", "No funciona, el servidor ya no arranca"},
	{"es", "no uses dos loops"},
	{"es", "Ejecuta los tests y dime qué falla"},

	{"pt", "adicione os testes para o parser"},
	{"pt", "Por favor corrija o erro no arquivo e rode os testes de novo"},
	{"pt", "Por que o build está falhando no CI?"},
	{"pt", "Adicione uma opção --verbose na CLI"},
	{"pt", "Você pode simplificar a função parseConfig?"},
	{"pt", "O teste em auth_test.go está vermelho, dá uma olhada"},
	{"pt", "A página de login está em branco desde o último deploy"},
	{"pt", "Escreva um README para o projeto"},
	{"pt", "Renomeie getUserById para findUser em todo o código"},
	{"pt", "Está bom, faz o commit com uma mensagem curta"},
	{"pt", "Tem uma race condition no pool de workers, consegue achar?"},
	{"pt", "Deixe as mensagens de erro mais claras"},
	{"pt", "Como eu faço mock do cliente HTTP nesses testes?"},
	{"pt", "Mova o parsing da configuração para um pacote próprio"},
	{"pt", "Tenta de novo, mas mantém a mesma API pública"},
	{"pt", "O que essa função retorna quando a lista está vazia?"},
	{"pt", "Atualize as dependências e veja se nada quebrou"},
	{"pt", "Divida esse arquivo em três menores"},
	{"pt", "Adicione logs em volta do loop de retry"},
	{"pt", "o erro está no if do handler"},
	{"pt", "Por que a imagem do Docker está tão grande?"},
	{"pt", "Revise minhas mudanças antes de eu dar push"},
	{"pt", "Não funciona, o servidor não sobe mais"},
	{"pt", "no final do arquivo"},
	{"pt", "Roda os testes e me diz o que falha"},

	{"it", "aggiungi i test per il parser"},
	{"it", "Per favore correggi l'errore nel file e rilancia i test"},
	{"it", "Perché la build fallisce in CI?"},
	{"it", "Aggiungi un'opzione --verbose alla CLI"},
	{"it", "Puoi semplificare la funzione parseConfig?"},
	{"it", "Il test in auth_test.go è rosso, dagli un'occhiata"},
	{"it", "La pagina di login è vuota dopo l'ultimo deploy"},
	{"it", "Scrivi un README per il progetto"},
	{"it", "Rinomina getUserById in findUser ovunque"},
	{"it", "Va bene, fai il commit con un messaggio breve"},
	{"it", "C'è una race condition nel pool dei worker, riesci a trovarla?"},
	{"it", "Rendi i messaggi di errore più chiari"},
	{"it", "Come faccio a mockare il client HTTP in questi test?"},
	{"it", "Sposta il parsing della configurazione in un package a parte"},
	{"it", "Riprova ma mantieni la stessa API pubblica"},
	{"it", "Cosa restituisce questa funzione quando la lista è vuota?"},
	{"it", "Aggiorna le dipendenze e controlla che non si rompa niente"},
	{"it", "Dividi questo file in tre più piccoli"},
	{"it", "no, mettilo in cima"},
	{"it", "È sicuro cancellare le vecchie migrazioni?"},
	{"it", "Perché l'immagine Docker è così grande?"},
	{"it", "Rivedi le mie modifiche prima che faccia push"},
	{"it", "Non funziona, il server non parte più"},
	{"it", "no, non va bene, rifai i test"},
	{"it", "Lancia i test e dimmi cosa fallisce"},

	{"nl", "voeg de tests toe voor de parser"},
	{"nl", "Kun je de fout in het bestand oplossen en de tests opnieuw draaien"},
	{"nl", "Waarom faalt de build op CI?"},
	{"nl", "Voeg een --verbose optie toe aan de CLI"},
	{"nl", "Kun je de functie parseConfig vereenvoudigen"},
	{"nl", "De test in auth_test.go is rood, kijk er even naar"},
	{"nl", "De loginpagina is leeg sinds de laatste deploy"},
	{"nl", "Schrijf een README voor het project"},
	{"nl", "Hernoem getUserById overal naar findUser"},
	{"nl", "Ziet er goed uit, commit het met een korte boodschap"},
	{"nl", "Er zit een race condition in de worker pool, kun je hem vinden?"},
	{"nl", "Maak de foutmeldingen duidelijker"},
	{"nl", "Hoe mock ik de HTTP-client in deze tests?"},
	{"nl", "Verplaats het parsen van de config naar een eigen package"},
	{"nl", "Probeer het opnieuw maar houd de publieke API hetzelfde"},
	{"nl", "Wat geeft deze functie terug als de lijst leeg is?"},
	{"nl", "Werk de dependencies bij en controleer of alles nog werkt"},
	{"nl", "Splits dit bestand op in drie kleinere"},
	{"nl", "Voeg logging toe rond de retry-lus"},
	{"nl", "Is het veilig om de oude migraties te verwijderen?"},
	{"nl", "Waarom is de Docker-image zo groot?"},
	{"nl", "Bekijk mijn wijzigingen voordat ik push"},
	{"nl", "Het werkt niet, de server start niet meer op"},
	{"nl", "Kijk even of we iets gemist hebben"},
	{"nl", "Draai de tests en vertel me wat er faalt"},

	{"pl", "dodaj testy do modułu i popraw błędy"},
	{"pl", "napraw błąd w logowaniu"},
	{"pl", "Dlaczego build nie przechodzi na CI?"},
	{"pl", "Dodaj opcję --verbose do CLI"},
	{"pl", "Możesz uprościć funkcję parseConfig?"},
	{"pl", "Test w auth_test.go jest czerwony, zerknij na to"},
	{"pl", "Strona logowania jest pusta od ostatniego deployu"},
	{"pl", "Napisz README dla projektu"},
	{"pl", "Zmień nazwę getUserById na findUser wszędzie"},
	{"pl", "Wygląda dobrze, zrób commit z krótkim opisem"},
	{"pl", "W puli workerów jest race condition, znajdziesz go?"},
	{"pl", "Popraw komunikaty błędów, żeby były jaśniejsze"},
	{"pl", "Jak zamockować klienta HTTP w tych testach?"},
	{"pl", "Przenieś parsowanie konfiguracji do osobnego pakietu"},
	{"pl", "Spróbuj jeszcze raz, ale nie zmieniaj publicznego API"},
	{"pl", "Co zwraca ta funkcja, gdy lista jest pusta?"},
	{"pl", "Zaktualizuj zależności i sprawdź, czy nic się nie zepsuło"},
	{"pl", "Podziel ten plik na trzy mniejsze"},
	{"pl", "no to do roboty"},
	{"pl", "Czy można bezpiecznie usunąć stare migracje?"},
	{"pl", "Dlaczego obraz Dockera jest taki duży?"},
	{"pl", "Przejrzyj moje zmiany zanim zrobię push"},
	{"pl", "Nie działa, serwer już się nie uruchamia"},
	{"pl", "on to zapisuje do pliku"},
	{"pl", "Uruchom testy i powiedz mi, co nie przechodzi"},

	{"ru", "Добавь тесты и продолжай рефакторинг модуля"},
	{"ru", "Пожалуйста, исправь ошибку в файле и запусти тесты ещё раз"},
	{"ru", "Почему билд падает на CI?"},
	{"ru", "Добавь флаг --verbose в CLI"},
	{"ru", "Можешь упростить функцию parseConfig?"},
	{"ru", "Тест в auth_test.go красный, посмотри пожалуйста"},
	{"ru", "Страница логина пустая после последнего деплоя"},
	{"ru", "Напиши README для проекта"},
	{"ru", "Переименуй getUserById в findUser везде"},
	{"ru", "Выглядит хорошо, сделай commit с коротким сообщением"},
	{"ru", "В пуле воркеров есть race condition, найдёшь?"},
	{"ru", "Сделай сообщения об ошибках понятнее"},
	{"ru", "Как замокать HTTP-клиент в этих тестах?"},
	{"ru", "Перенеси парсинг конфига в отдельный пакет"},
	{"ru", "Попробуй ещё раз, но не меняй публичный API"},
	{"ru", "Что возвращает эта функция, если список пустой?"},
	{"ru", "Обнови зависимости и проверь, что ничего не сломалось"},
	{"ru", "Раздели этот файл на три поменьше"},
	{"ru", "Добавь логирование вокруг цикла retry"},
	{"ru", "Безопасно ли удалить старые миграции?"},
	{"ru", "Почему docker-образ такой большой?"},
	{"ru", "Посмотри мои изменения перед push"},
	{"ru", "Не работает, сервер больше не запускается"},
	{"ru", "Объясни, что делает эта регулярка"},
	{"ru", "Запусти тесты и скажи, что падает"},

	{"ja", "テストを追加してからリファクタリングを続けてください"},
	{"ja", "ファイルのエラーを修正してテストをもう一度実行してください"},
	{"ja", "CIでビルドが失敗するのはなぜ？"},
	{"ja", "CLIに--verboseオプションを追加して"},
	{"ja", "parseConfig関数をもっとシンプルにできる？"},
	{"ja", "auth_test.goのテストが赤いので見てください"},
	{"ja", "前回のデプロイからログイン画面が真っ白です"},
	{"ja", "プロジェクトのREADMEを書いて"},
	{"ja", "getUserByIdをfindUserに全部リネームして"},
	{"ja", "いいですね、短いメッセージでコミットして"},
	{"ja", "ワーカープールに競合状態があるので探してもらえますか"},
	{"ja", "エラーメッセージをもっと分かりやすくして"},
	{"ja", "このテストでHTTPクライアントをモックするには？"},
	{"ja", "設定のパース処理を別パッケージに移して"},
	{"ja", "もう一度試して、でも公開APIは変えないで"},
	{"ja", "リストが空のときこの関数は何を返す？"},
	{"ja", "依存関係を更新して何も壊れていないか確認して"},
	{"ja", "このファイルを3つに分けて"},
	{"ja", "リトライのループの前後にログを追加して"},
	{"ja", "古いマイグレーションを消しても大丈夫？"},
	{"ja", "Dockerイメージがなんでこんなに大きいの？"},
	{"ja", "プッシュする前に変更をレビューして"},
	{"ja", "動かない、サーバーが起動しなくなった"},
	{"ja", "この正規表現が何をしているか説明して"},
	{"ja", "テストを実行して、何が失敗するか教えて"},

	{"zh", "帮我修复 login 函数的 bug"},
	{"zh", "请修复文件里的错误然后重新运行测试"},
	{"zh", "为什么 CI 上的构建失败了？"},
	{"zh", "给 CLI 加一个 --verbose 选项"},
	{"zh", "能不能把 parseConfig 函数简化一下"},
	{"zh", "auth_test.go 里的测试挂了，帮我看看"},
	{"zh", "上次部署之后登录页面是空白的"},
	{"zh", "给这个项目写一个 README"},
	{"zh", "把 getUserById 全部重命名为 findUser"},
	{"zh", "看起来不错，用简短的信息提交一下"},
	{"zh", "worker 池里有竞态条件，能帮我找出来吗"},
	{"zh", "让错误信息更清楚一点"},
	{"zh", "在这些测试里怎么 mock HTTP 客户端？"},
	{"zh", "把配置解析移到单独的包里"},
	{"zh", "再试一次，但是不要改公共 API"},
	{"zh", "列表为空的时候这个函数返回什么？"},
	{"zh", "更新依赖，然后检查有没有东西坏掉"},
	{"zh", "把这个文件拆成三个小文件"},
	{"zh", "在重试循环前后加上日志"},
	{"zh", "删除旧的迁移文件安全吗？"},
	{"zh", "为什么 Docker 镜像这么大？"},
	{"zh", "我 push 之前帮我审查一下改动"},
	{"zh", "不行，服务器启动不了了"},
	{"zh", "解释一下这个正则表达式是做什么的"},
	{"zh", "跑一下测试，告诉我哪些失败了"},

	{"ko", "auth 모듈에 테스트 추가해줘"},
	{"ko", "파일의 오류를 고치고 테스트를 다시 실행해 주세요"},
	{"ko", "CI에서 빌드가 왜 실패해?"},
	{"ko", "CLI에 --verbose 옵션 추가해줘"},
	{"ko", "parseConfig 함수 좀 더 단순하게 만들 수 있어?"},
	{"ko", "auth_test.go 테스트가 빨간색이야, 한번 봐줘"},
	{"ko", "지난 배포 이후로 로그인 페이지가 비어 있어요"},
	{"ko", "이 프로젝트 README 작성해줘"},
	{"ko", "getUserById를 전부 findUser로 이름 바꿔줘"},
	{"ko", "좋아, 짧은 메시지로 커밋해줘"},
	{"ko", "워커 풀에 레이스 컨디션이 있는데 찾아줄 수 있어?"},
	{"ko", "에러 메시지를 더 알기 쉽게 바꿔줘"},
	{"ko", "이 테스트에서 HTTP 클라이언트를 어떻게 모킹해?"},
	{"ko", "설정 파싱 코드를 별도 패키지로 옮겨줘"},
	{"ko", "다시 해봐, 근데 공개 API는 바꾸지 마"},
	{"ko", "리스트가 비어 있으면 이 함수는 뭘 반환해?"},
	{"ko", "의존성 업데이트하고 깨진 거 없는지 확인해줘"},
	{"ko", "이 파일을 세 개로 나눠줘"},
	{"ko", "재시도 루프 앞뒤에 로그 추가해줘"},
	{"ko", "오래된 마이그레이션 지워도 안전해?"},
	{"ko", "도커 이미지가 왜 이렇게 커?"},
	{"ko", "푸시하기 전에 내 변경사항 리뷰해줘"},
	{"ko", "안 돼, 서버가 더 이상 안 떠"},
	{"ko", "이 정규식이 뭐 하는 건지 설명해줘"},
	{"ko", "테스트 돌리고 뭐가 실패하는지 알려줘"},

	{"ar", "أضف اختبارات لوحدة auth"},
	{"ar", "من فضلك أصلح الخطأ في الملف ثم شغّل الاختبارات مرة أخرى"},
	{"ar", "لماذا يفشل البناء على CI؟"},
	{"ar", "أضف خيار --verbose إلى CLI"},
	{"ar", "هل يمكنك تبسيط الدالة parseConfig؟"},
	{"ar", "الاختبار في auth_test.go أحمر، ألقِ نظرة عليه"},
	{"ar", "صفحة تسجيل الدخول فارغة منذ آخر نشر"},
	{"ar", "اكتب ملف README للمشروع"},
	{"ar", "أعد تسمية getUserById إلى findUser في كل مكان"},
	{"ar", "يبدو جيدًا، اعمل commit برسالة قصيرة"},
	{"ar", "هناك race condition في مجمع العمال، هل يمكنك إيجادها؟"},
	{"ar", "اجعل رسائل الخطأ أوضح"},
	{"ar", "كيف أعمل mock لعميل HTTP في هذه الاختبارات؟"},
	{"ar", "انقل تحليل الإعدادات إلى حزمة منفصلة"},
	{"ar", "حاول مرة أخرى لكن لا تغيّر الواجهة العامة"},
	{"ar", "ماذا تُرجع هذه الدالة عندما تكون القائمة فارغة؟"},
	{"ar", "حدّث الاعتماديات وتأكد أن لا شيء تعطل"},
	{"ar", "قسّم هذا الملف إلى ثلاثة ملفات أصغر"},
	{"ar", "أضف سجلات حول حلقة إعادة المحاولة"},
	{"ar", "هل من الآمن حذف ملفات الترحيل القديمة؟"},
	{"ar", "لماذا صورة Docker كبيرة جدًا؟"},
	{"ar", "راجع تغييراتي قبل أن أعمل push"},
	{"ar", "لا يعمل، الخادم لم يعد يبدأ"},
	{"ar", "اشرح لي ماذا يفعل هذا التعبير النمطي"},
	{"ar", "شغّل الاختبارات وأخبرني ما الذي يفشل"},
}

var englishPromptsWithWordsOtherLanguagesUse = []string{
	"do I need to use a mutex here",
	"I do not know why this fails",
	"I need to do a quick check first",
	"what do I do now",
	"I wonder if I should do it",
	"as a test, do it",
	"no no no, undo that",
	"how do I run only one test",
	"I'm stuck, what should I do",
	"as soon as possible",
	"keep it as simple as possible",
	"do it as before",
	"no, use one per test",
	"no, one per line",
	"no, I meant per session",
	"no, per user",
	"no non-null checks",
	"e.g. no retries",
	"comment on each function",
	"put a comment on each exported type",
	"comment as needed",
	"comment on line 12 as well",
	"no z-index hacks",
	"no .com links",
	"no com port found",
	"don’t touch the migrations",
	"i’m sure it’s fine",
	"do we need this import at all",
	"no, I meant the other file",
	"do the same for the other two handlers",
	"as far as I can tell it works",
	"I do want the logs though",
	"no need to run the tests again",
	"do not commit yet",
	"do what you think is best",
	"i think the bug is in the parser",
	"no, do it in the worker instead",
	"as I said, do not push",
	"do I have to restart the server",
	"no more console.log calls please",
	"do a dry run first",
	"i need a function that does x",
	"as is, it does not compile",
	"do both, I don’t mind",
	"no idea why, do a git bisect",
	"i'd do it with a map",
	"do it in go",
	"no, the one in utils",
	"so do we keep it or not",
	"do you know what i mean",
	"no, as in the whole module",
	"can i do this with a regex",
	"i'm not sure i follow",
	"do it on a branch",
	"no i do not want that",
	"do i run it with sudo",
	"i do see the error in the logs",
	"as a rule, no globals",
	"why do the tests die on ci",
	"no, do it as a separate commit",
}

var promptsLeftToTheSessionLanguage = map[string]bool{
	"write a README section about installing on Windows": true,
	"rename getUserById to findUser everywhere":          true,
	"retry döngüsüne log ekle":                           true,
	"testleri calistir ve hatalari duzelt":               true,
	"Benenne getUserById überall in findUser um":         true,
	"Mach die Fehlermeldungen verständlicher":            true,
	"Renomme getUserById en findUser partout":            true,
	"Rends les messages d'erreur plus clairs":            true,
	"tu as raison, on continue":                          true,
	"¿Puedes simplificar la función parseConfig?":        true,
	"no le hagas caso al linter":                         true,
	"Revisa mis cambios antes de que haga push":          true,
	"no uses dos loops":                                  true,
	"Por que o build está falhando no CI?":               true,
	"Tenta de novo, mas mantém a mesma API pública":      true,
	"no final do arquivo":                                true,
	"Puoi semplificare la funzione parseConfig?":         true,
	"Rinomina getUserById in findUser ovunque":           true,
	"Riprova ma mantieni la stessa API pubblica":         true,
	"no, mettilo in cima":                                true,
	"È sicuro cancellare le vecchie migrazioni?":         true,
	"Hernoem getUserById overal naar findUser":           true,
	"Maak de foutmeldingen duidelijker":                  true,
	"Napisz README dla projektu":                         true,
	"Podziel ten plik na trzy mniejsze":                  true,
	"no to do roboty":                                    true,
	"on to zapisuje do pliku":                            true,
	"as soon as possible":                                true,
	"no, I meant per session":                            true,
	"no, per user":                                       true,
	"no non-null checks":                                 true,
	"e.g. no retries":                                    true,
	"comment on each function":                           true,
	"put a comment on each exported type":                true,
	"comment as needed":                                  true,
	"no z-index hacks":                                   true,
	"no .com links":                                      true,
	"no com port found":                                  true,
	"do not commit yet":                                  true,
	"do a dry run first":                                 true,
	"as a rule, no globals":                              true,
}

type detectionTally struct {
	placed, wrong, undecided int
}

func (tally detectionTally) line() string {
	return fmt.Sprintf("placed %3d  wrong %2d  undecided %2d", tally.placed, tally.wrong, tally.undecided)
}

func TestTheLanguageCorpusCoversEveryCatalogLanguage(t *testing.T) {
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, sample := range languageCorpus {
		counts[sample.lang]++
		if seen[sample.prompt] {
			t.Errorf("%q is in the corpus twice", sample.prompt)
		}
		seen[sample.prompt] = true
	}
	for lang := range catalogTable() {
		if counts[lang] != 25 {
			t.Errorf("the corpus has %d %s prompts, want 25", counts[lang], lang)
		}
	}
	if len(counts) != len(catalogTable()) {
		t.Errorf("the corpus has prompts in %d languages, the catalog has %d", len(counts), len(catalogTable()))
	}
	if len(englishPromptsWithWordsOtherLanguagesUse) != 60 {
		t.Errorf("%d English prompts with words other languages use, want 60", len(englishPromptsWithWordsOtherLanguagesUse))
	}
	for _, prompt := range englishPromptsWithWordsOtherLanguagesUse {
		if seen[prompt] {
			t.Errorf("%q is in the corpus twice", prompt)
		}
		seen[prompt] = true
	}
	for prompt := range promptsLeftToTheSessionLanguage {
		if !seen[prompt] {
			t.Errorf("%q may stay undecided but is not in the corpus", prompt)
		}
	}
}

func TestAnEnglishPromptIsNeverReadAsAnotherLanguage(t *testing.T) {
	for _, prompt := range englishPromptsWithWordsOtherLanguagesUse {
		if got := detectLanguage(prompt); got != "en" && got != "" {
			t.Errorf("%q was read as %q, so an English session would switch its notices and status line to that language", prompt, got)
		}
	}
}

func TestTheLanguageCorpusIsNeverReadAsAnotherLanguageAndKeepsEveryPlacedPrompt(t *testing.T) {
	const sharedWords = "en, words other languages use"
	samples := append([]languageSample{}, languageCorpus...)
	groups := make([]string, len(samples))
	for i, sample := range samples {
		groups[i] = sample.lang
	}
	for _, prompt := range englishPromptsWithWordsOtherLanguagesUse {
		samples = append(samples, languageSample{"en", prompt})
		groups = append(groups, sharedWords)
	}
	perGroup := map[string]*detectionTally{}
	var total detectionTally
	for i, sample := range samples {
		tally := perGroup[groups[i]]
		if tally == nil {
			tally = &detectionTally{}
			perGroup[groups[i]] = tally
		}
		switch got := detectLanguage(sample.prompt); got {
		case sample.lang:
			tally.placed++
			total.placed++
		case "":
			tally.undecided++
			total.undecided++
			if !promptsLeftToTheSessionLanguage[sample.prompt] {
				t.Errorf("%s prompt %q is left undecided, so the session keeps whatever language it had; it was detected when this corpus was written", sample.lang, sample.prompt)
			}
		default:
			tally.wrong++
			total.wrong++
			t.Errorf("%s prompt %q was read as %q, so the session would switch its notices and status line to that language", sample.lang, sample.prompt, got)
		}
	}
	names := make([]string, 0, len(perGroup))
	for name := range perGroup {
		names = append(names, name)
	}
	sort.Strings(names)
	lines := make([]string, 0, len(names))
	for _, name := range names {
		lines = append(lines, fmt.Sprintf("  %-31s %s", name, perGroup[name].line()))
	}
	decided := total.placed + total.wrong
	precision := 0.0
	if decided > 0 {
		precision = 100 * float64(total.placed) / float64(decided)
	}
	t.Logf("language detection over %d prompts: precision %.1f%% (%d of %d decided), recall %.1f%% (%d of %d)\n%s",
		len(samples), precision, total.placed, decided, 100*float64(total.placed)/float64(len(samples)), total.placed, len(samples), strings.Join(lines, "\n"))
}

var englishPromptsWithDeveloperTokens = []string{
	"i saw no error in ci",
	"no, i mean ci",
	"i mean in ci",
	"no, in ci",
	"no errors in ci, i checked",
	"no, even in ci",
	"no, ten in ci",
	"per ci run, i see ten retries",
	"i see ten warnings in ci",
	"i.e. no retries",
	"i.e. ci",
	"i.e. per user",
	"i.e. no ai",
	"i.e. no ai in ci",
	"e.g. in ci",
	"no ai, i mean ml",
	"per ai call i pay",
	"no, i said ten",
	"do ten retries on my branch",
	"ten on my branch",
	"no pod on my node",
	"im in the wrong dir",
	"no z-index hacks on my page",
	"no z-index on my modal",
	"z-index on my navbar",
	"a z-index of ten",
	"sort a-z on my list",
	"co-author on my pr",
	"put co-authors on my pr",
	"a non-null ptr in ci",
	"tar -z on my box",
	"the e2e tests fail in ci",
	"run the e2e suite in ci",
	"e2e is red in ci again",
	"plot x and y, plus the ai score",
	"run ai on x and y",
	"no i/o in ci",
	"i/o errors on my box",
	"i/o bound in ci",
	"tests w/o mocks in ci",
	"w/o retries on my branch",
	"w/ retries on my branch",
}

var englishPromptsReadAsEnglish = []string{
	"i see it in ci",
	"no, i run it in ci",
	"in ci, per run, i see it",
	"i.e. the one in ci",
	"even in ci it fails",
	"the tests die in ci",
	"set it to ten",
	"no, set the limit to ten",
	"add co-authors to my commit",
	"im stuck on the build",
	"plot x and y, plus the ai score",
	"e.g. no ai in the loop",
	"disk i/o on my vm is slow",
	"do it w/o the cache on my branch",
	"run it w/o cache in ci",
	"build it w/o docker in ci",
}

var promptsThatShareDeveloperTokensWithEnglish = []languageSample{
	{"it", "ci sono ancora errori nei test"},
	{"it", "non ci sono test per questa funzione"},
	{"it", "ci vuole troppo tempo per la build"},
	{"it", "ci sono dei warning in CI"},
	{"it", "aggiungi i log ai test e poi fai il commit"},
	{"pl", "ten plik jest za duży"},
	{"pl", "ten commit coś zepsuł"},
	{"pl", "co się dzieje pod spodem"},
	{"pl", "nic nie działa na CI"},
	{"pl", "m.in. testy i build nie działają"},
	{"fr", "ajoute plus de logs"},
	{"fr", "il n'y a plus de place sur le disque"},
	{"fr", "est-ce que ça marche"},
	{"fr", "peux-tu corriger le bug"},
	{"fr", "dis-moi ce qui ne va pas"},
	{"de", "im CI läuft es nicht mehr"},
	{"de", "es gibt einen Bug im Parser"},
	{"de", "z.B. die Tests und der Build"},
	{"es", "p.ej. el test y el build"},
	{"pt", "os testes estão falhando em CI"},
	{"pl", "dodaj testy i/lub logi"},
	{"fr", "ajoute des tests et/ou des logs"},
	{"de", "füge Tests und/oder Logs hinzu"},
	{"nl", "voeg tests en/of logs toe"},
}

func TestAnEnglishPromptWithDeveloperTokensIsNeverReadAsAnotherLanguage(t *testing.T) {
	for _, prompt := range englishPromptsWithDeveloperTokens {
		if got := detectLanguage(prompt); got != "en" && got != "" {
			t.Errorf("%q was read as %q, so an English session would switch its notices and status line to that language", prompt, got)
		}
	}
}

func TestAnEnglishPromptWithDeveloperTokensIsReadAsEnglish(t *testing.T) {
	for _, prompt := range englishPromptsReadAsEnglish {
		if got := detectLanguage(prompt); got != "en" {
			t.Errorf("%q was read as %q, not as English", prompt, got)
		}
	}
}

func TestAPromptThatSharesADeveloperTokenWithEnglishKeepsItsLanguage(t *testing.T) {
	for _, sample := range promptsThatShareDeveloperTokensWithEnglish {
		if got := detectLanguage(sample.prompt); got != sample.lang {
			t.Errorf("%s prompt %q was read as %q", sample.lang, sample.prompt, got)
		}
	}
}
