package main

import (
	"os"
	"strings"
	"testing"
)

func expectItems(t *testing.T, name, prompt string, want []string) {
	t.Helper()
	got := autoQueueItems(prompt)
	if len(got) != len(want) {
		t.Errorf("%s: want %d items, got %d: %q", name, len(want), len(got), got)
		return
	}
	for index, item := range want {
		if got[index] != item {
			t.Errorf("%s: item %d is %q, want %q", name, index+1, got[index], item)
		}
	}
}

func bullets(items []string) string {
	return "\n- " + strings.Join(items, "\n- ") + "\n"
}

func TestAListItemIsReadWithTheStartersOfThePromptsLanguage(t *testing.T) {
	turkish := []string{
		"En iyi skoru deneme/skor.json dosyasına kaydeden özelliği ekle",
		"Bu fonksiyonu daha okunur olacak şekilde yeniden yaz",
		"Şu hatalı testi düzelt ve nedenini commit mesajına yaz",
		"O eski yapılandırma dosyasını depodan sil",
		"Mevcut testleri yeni API'ye göre güncelle",
		"Bugün eklenen uç noktaları README dosyasında belgele",
		"Eğer önbellek bozuksa derlemeden önce temizle",
		"Ve sürüm notlarını CHANGELOG dosyasına ekle",
		"Ama önce veritabanı şemasını yedekle",
		"Ancak göç betiğini ayrı bir dosyaya taşı",
		"Bizim loglama katmanını tek bir pakette birleştir",
		"Not alma ekranına otomatik kaydetme ekle",
		"Önce giriş sayfasının testlerini yaz",
		"Tüm uyarıları derleme çıktısından temizle",
	}
	expectItems(t, "turkish", "Aşağıdaki işleri sırayla yap:"+bullets(turkish), turkish)

	english := []string{
		"To keep things simple, move the date helpers into one file",
		"Ten more unit tests for the date parser, covering leap years",
		"A CSV export for the monthly report page",
		"Die early with a clear error when the config file is missing",
		"If the cache is stale, delete it before the rebuild starts",
		"For the login page, add rate limiting per IP address",
		"As a follow-up, bump the version in package.json",
	}
	expectItems(t, "english", "Please work through this list for the release:"+bullets(english), english)

	german := []string{
		"Die Konfiguration in eine eigene Datei auslagern",
		"Den Parser für leere Zeilen reparieren",
		"Das Logging vereinheitlichen und alte Aufrufe entfernen",
	}
	expectItems(t, "german", "Bitte die folgenden Punkte für das nächste Release erledigen, der Reihe nach und ohne Rückfrage:"+bullets(append(german, "Es gibt noch Probleme beim Testen der Oberfläche")), german)

	dutch := []string{
		"Het logbestand naar de nieuwe map verplaatsen",
		"Dit scherm met caching een stuk sneller maken",
		"Dat oude script helemaal uit de repo verwijderen",
	}
	expectItems(t, "dutch", "Werk de volgende punten voor de volgende release af, in deze volgorde en zonder tussendoor te vragen:"+bullets(append(dutch, "Er zijn nog vragen over de nieuwe API")), dutch)
}

func TestADescriptiveListItemStaysOutOfTheChecklistAndIsNamed(t *testing.T) {
	cfg, project := queueTrustSandbox(t, false)
	steps := []string{
		"add a login endpoint with rate limiting to the auth service",
		"rewrite the payment module so it uses the new billing client",
		"remove the legacy logging from the worker and the scheduler",
	}
	notes := []string{
		"The app currently runs on Express 4 behind nginx",
		"It should also keep working with the old mobile client",
	}
	prompt := "Please do the following for the next release, one after the other:" + bullets(append(append([]string{}, steps[:2]...), append(notes, steps[2])...))
	output := startQueue(t, cfg, "dropped-named", project, prompt)
	record := getMap(getMap(readState(), "autoQueues"), "dropped-named")
	if numberOr(record, "items", 0) != 3 {
		t.Fatalf("the three steps did not become the checklist: %v", record)
	}
	message := getString(output, "systemMessage")
	for _, note := range notes {
		if !strings.Contains(message, note) {
			t.Errorf("the user is not told that %q stayed out of the checklist: %q", note, message)
		}
	}
	content, err := os.ReadFile(getString(record, "path"))
	if err != nil {
		t.Fatal(err)
	}
	for _, note := range notes {
		if !strings.Contains(string(content), "- "+note) || strings.Contains(string(content), "- [ ] "+note) {
			t.Errorf("the checklist does not keep %q as a line that is not driven:\n%s", note, content)
		}
	}
	if view := queueSnapshotOf(getString(record, "path"), string(content)); view.total != 3 {
		t.Errorf("the lines left out count as open items: %d open, want 3", view.total)
	}
	if context := contextOf(output); !strings.Contains(context, "2 listed line") {
		t.Errorf("Claude is not told which listed lines are not on the checklist: %q", context)
	}
}

func TestAClosingPoliteRequestForTheListIsNotAQuestion(t *testing.T) {
	for _, tc := range []struct {
		closing string
		items   []string
		want    int
	}{
		{"Can you do these?", heldBackItemsList(), 4},
		{"Could you please take care of all of them today?", heldBackItemsList(), 4},
		{"Bunları yapabilir misin?", heldBackItemsTrList(), 4},
		{"Hepsini sırayla halleder misin?", heldBackItemsTrList(), 4},
		{"Can you estimate these?", heldBackItemsList(), 0},
		{"Which of these should I do first?", heldBackItemsList(), 0},
		{"Bunları tahmin edebilir misin?", heldBackItemsTrList(), 0},
		{"Bunlardan hangisi önce yapılmalı?", heldBackItemsTrList(), 0},
	} {
		prompt := "Here is the list for the next release, after the planning call with the team this morning." + bullets(tc.items) + tc.closing
		if got := autoQueueItems(prompt); len(got) != tc.want {
			t.Errorf("%q: want %d items, got %d: %q", tc.closing, tc.want, len(got), got)
		}
	}
}

func heldBackItemsList() []string {
	return strings.Split(strings.TrimSpace(strings.ReplaceAll(heldBackItems, "\n- ", "\n")), "\n")
}

func heldBackItemsTrList() []string {
	return strings.Split(strings.TrimSpace(strings.ReplaceAll(heldBackItemsTr, "\n- ", "\n")), "\n")
}

func TestACommaChainedParagraphIsSplitIntoItsSteps(t *testing.T) {
	expectItems(t, "turkish", "Önce kullanıcı tablosu için geri alınabilir yeni bir göç betiği oluştur, sonra giriş sayfasının uçtan uca testlerini Playwright ile yaz, ardından ödeme servisine kullanıcı başına istek sınırlaması ekle, son olarak README dosyasındaki kurulum adımlarını yeni komutlara göre güncelle.", []string{
		"Önce kullanıcı tablosu için geri alınabilir yeni bir göç betiği oluştur",
		"sonra giriş sayfasının uçtan uca testlerini Playwright ile yaz",
		"ardından ödeme servisine kullanıcı başına istek sınırlaması ekle",
		"son olarak README dosyasındaki kurulum adımlarını yeni komutlara göre güncelle.",
	})
	expectItems(t, "english", "First create a migration for the users table with a rollback script, then write end-to-end tests for the login page with Playwright, after that add per-user rate limiting to the payment service, and finally update the setup steps in the README for the new commands.", []string{
		"First create a migration for the users table with a rollback script",
		"then write end-to-end tests for the login page with Playwright",
		"after that add per-user rate limiting to the payment service",
		"finally update the setup steps in the README for the new commands.",
	})
	sentences := []string{
		"Run the unit tests, then fix whatever fails in the payment module.",
		"Build the docs, then publish them to the internal wiki page.",
		"Check the logs, then clear the old files in the temp folder.",
		"Tag the build, then deploy it to the staging cluster for review.",
	}
	expectItems(t, "sentences", strings.Join(sentences, " "), []string{
		"Run the unit tests, then fix whatever fails in the payment module",
		"Build the docs, then publish them to the internal wiki page",
		"Check the logs, then clear the old files in the temp folder",
		"Tag the build, then deploy it to the staging cluster for review.",
	})
}
