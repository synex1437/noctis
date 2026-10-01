package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

var (
	listItemLine   = lazyRegexp(`^\s*(?:` + bulletMarker + `|\(?\d{1,2}[.)]|[a-z][.)])\s+(\S.*)$`)
	codeFence      = lazyRegexp("(?s)```.*?```")
	sentenceSplit  = lazyRegexp(`[.!;]\s+`)
	wordSplit      = lazyRegexp(`\s+`)
	checkboxPrefix = lazyRegexp(`^\[\s*([xX✓✔]?)\s*\]\s*`)

	logLikeLine = lazyRegexp(`(?i)^\s*(?:at\s+\S+\s*\(|\S+\.\w{1,5}:\d+|traceback|panic:|error(?:\[|:)|warning:|exception|npm err!|\$\s|>\s|\[\w+\]\s|\d{4}-\d\d-\d\d[t ]\d\d:\d\d|goroutine \d+|caused by:|stack trace)`)
)

const (
	autoQueueMinChars     = 240
	autoQueueMinItems     = 3
	autoQueueMinLines     = 4
	autoQueueMinSentences = 4
	autoQueueMaxItems     = 40
	autoQueueMinWords     = 4
)

// descriptiveStarters holds, per language, the first words of a list line
// that describes rather than asks: "The app runs on …", "Bu modül …". A
// line is read with the words of the prompt's own language, so the Dutch
// "en" does not throw out the Turkish "En iyi skoru …" and the Polish "to"
// does not throw out "To keep things simple, …".
var descriptiveStarters = map[string]func(string) bool{
	"en": lazyWordSet(`i i'm i've we we're we've our my the this that these those there here it it's its currently
		note context background fyi for as because since when if so but and however today yesterday`),
	"tr": lazyWordSet(`ben biz bizim benim bu şu o burada burda not bağlam mevcut halihazırda hâlihazırda çünkü eğer ama fakat ancak ve bugün dün`),
	"de": lazyWordSet(`ich wir unser unsere mein meine der die das es hier dort aktuell derzeit momentan hinweis kontext weil da wenn als aber und`),
	"fr": lazyWordSet(`je j'ai nous notre nos mon ma le la les ce cette il elle ici actuellement contexte note parce car si mais et en`),
	"es": lazyWordSet(`yo nosotros nuestro nuestra mi tu el la los las este esta esto es hay aquí actualmente nota contexto porque si pero y en`),
	"pt": lazyWordSet(`eu nós nosso nossa meu minha o a os as este esta isto há atualmente nota contexto porque`),
	"it": lazyWordSet(`io noi nostro nostra mio mia il lo la le i gli questo questa c'è qui attualmente nota contesto perché ma`),
	"nl": lazyWordSet(`ik wij we ons onze mijn het die dit dat er hier momenteel opmerking omdat als maar en`),
	"pl": lazyWordSet(`ja my nasz nasza mój moja to ten ta tu tutaj obecnie uwaga kontekst bo ponieważ jeśli ale i`),
	"ru": lazyWordSet(`я мы наш наша мой моя это этот эта тут здесь сейчас примечание контекст потому если но и`),
	"id": lazyWordSet(`saya kami kita aku ini itu tersebut yang ada di saat sekarang catatan konteks karena jika kalau tapi tetapi namun dan jadi`),
}

func descriptiveStarter(lang, word string) bool {
	starters, ok := descriptiveStarters[lang]
	if !ok {
		starters = descriptiveStarters["en"]
	}
	return starters(word)
}

const verbFinalImperativeWords = `yükle indir kaydet gönder planla sırala filtrele grupla ayıkla çöz gider biçimlendir paketle derle yedekle arşivle kapsa
	sadeleştir yeniden ekleyin yazın oluşturun düzeltin güncelleyin kaldırın silin taşıyın yapın kurun çalıştırın değiştirin
	ekle yaz oluştur düzelt güncelle kaldır sil taşı dağıt yap ayarla kur çalıştır üret dönüştür değiştir iyileştir temizle
	belgele incele birleştir ayır çıkar bağla etkinleştir sağla destekle yükselt geliştir tasarla hazırla düzenle tamamla bitir
	çevir yayınla araştır ölç doğrula`

var imperativeWords = lazyWordSet(`add build create write fix update refactor implement remove delete rename move migrate deploy test
	check verify make set configure install run generate convert replace change improve optimize optimise clean document
	review merge split extract wrap integrate connect enable disable ensure handle support upgrade bump port rewrite redesign
	restructure use finish complete prepare design draft polish translate publish release ship investigate debug measure
	profile cache validate sanitize secure harden audit wire hook scaffold bootstrap initialize init extend expose register
	define declare introduce apply adjust tweak tune resolve address tidy format lint annotate stub mock seed backfill import
	export sync fetch load save store persist log trace monitor alert notify send schedule retry throttle paginate sort filter
	group dedupe normalize parse serialize encode decode compress encrypt hash sign authenticate authorize localize style theme
	animate render draw plot chart package bundle compile transpile minify containerize dockerize provision rollback backup
	restore archive prune rotate benchmark fuzz cover refine simplify reorganize reorder unify consolidate
	` + verbFinalImperativeWords + `
	füge erstelle schreibe implementiere entferne aktualisiere baue teste prüfe konfiguriere installiere ersetze verbessere
	ajoute crée écris corrige implémente supprime mets construis teste vérifie configure installe remplace améliore
	añade agrega crea escribe corrige implementa elimina actualiza construye prueba verifica configura instala reemplaza mejora
	adicione crie escreva corrija implemente remova atualize construa teste verifique configure instale substitua melhore
	aggiungi crea scrivi correggi implementa rimuovi aggiorna costruisci testa verifica configura installa sostituisci migliora
	tambahkan tambah buat bikin buatkan tulis tuliskan perbaiki benahi betulkan ubah ganti hapus pindahkan jalankan uji periksa cek
	perbarui pasang instal konfigurasikan dokumentasikan gabungkan pisahkan optimalkan bersihkan rapikan terjemahkan rilis terbitkan
	selidiki ukur validasi siapkan rancang implementasikan terapkan migrasikan aktifkan nonaktifkan tangani dukung tingkatkan
	sederhanakan susun atur simpan muat ekspor impor sinkronkan catat pantau kirim jadwalkan urutkan saring kelompokkan kompilasi
	bungkus kemas sambungkan hubungkan daftarkan definisikan sesuaikan selesaikan lengkapi tinjau`)

var leadIns = lazyWordSet(`please lütfen bitte veuillez merci por favor per favore alsjeblieft proszę пожалуйста
	first firstly then next finally lastly afterwards after that also and now
	önce ilk sonra ardından daha son olarak en ayrıca ve şimdi
	zuerst dann danach schließlich zuletzt außerdem und
	d'abord ensuite puis enfin aussi et
	primero luego después finalmente también y
	primeiro depois finalmente também e
	prima poi dopo infine anche
	eerst daarna vervolgens ook en
	najpierw potem następnie także i
	сначала затем потом наконец также и
	tolong mohon silakan coba pertama lalu kemudian setelah itu terakhir akhirnya selanjutnya juga dan sekarang`)

func imperativeLike(unit string) bool {
	fields := wordSplit.Split(strings.TrimSpace(unit), -1)
	if leadsWithImperative(fields) {
		return true
	}
	last := stepWord(fields[len(fields)-1])
	return imperativeWords(last) || imperativeWords(strings.TrimSuffix(strings.TrimSuffix(last, "in"), "iniz"))
}

func stepWord(word string) string {
	return strings.ToLower(strings.Trim(word, ",.:;!()\"'“”‘’«»"))
}

// leadsWithImperative tells words whose first one, after lead-ins such as
// "please" or "then", is a work verb.
func leadsWithImperative(fields []string) bool {
	for index := 0; index < len(fields) && index < 4; index++ {
		word := stepWord(fields[index])
		if imperativeWords(word) {
			return true
		}
		if !leadIns(word) {
			break
		}
	}
	return false
}

// finalVerbs holds the verbs that close a step in the languages that put the
// verb last: Turkish imperatives ("Bu fonksiyonu yeniden yaz") and the German
// and Dutch infinitives of a to-do list ("Die Konfiguration auslagern").
var finalVerbs = map[string]func(string) bool{
	"tr": lazyWordSet(verbFinalImperativeWords + ` et al aç kapat başlat durdur geçir dene koy bul ertele sabitle kilitle artır azalt`),
	"de": lazyWordSet(`hinzufügen ergänzen erstellen anlegen schreiben implementieren umsetzen entfernen löschen aktualisieren bauen
		testen prüfen überprüfen konfigurieren installieren ersetzen verbessern reparieren beheben korrigieren umbenennen verschieben
		migrieren einbauen anpassen ändern dokumentieren vereinheitlichen auslagern aufteilen zusammenführen optimieren bereinigen
		aufräumen einrichten speichern übersetzen veröffentlichen erweitern einführen abschließen fertigstellen überarbeiten
		vereinfachen absichern validieren umstellen erledigen`),
	"nl": lazyWordSet(`toevoegen maken aanmaken schrijven implementeren verwijderen bijwerken updaten bouwen testen controleren
		configureren installeren vervangen verbeteren repareren oplossen corrigeren hernoemen verplaatsen migreren aanpassen
		wijzigen refactoren documenteren opschonen opruimen opsplitsen samenvoegen optimaliseren instellen opslaan vertalen
		publiceren uitbreiden invoeren afronden herschrijven vereenvoudigen`),
}

var turkishPoliteEndings = []string{"yiniz", "yınız", "yunuz", "yünüz", "iniz", "ınız", "unuz", "ünüz", "yin", "yın", "yun", "yün", "in", "ın", "un", "ün"}

// endsInVerb tells a step that ends in its verb, as Turkish, German and Dutch
// steps do, from a line that only starts like a description. A polite word
// after the verb is passed over: "… kontrol et lütfen".
func endsInVerb(unit, lang string) bool {
	fields := wordSplit.Split(strings.TrimSpace(unit), -1)
	for len(fields) > 1 && trailingPoliteWords(stepWord(fields[len(fields)-1])) {
		fields = fields[:len(fields)-1]
	}
	raw := strings.Trim(fields[len(fields)-1], ",.:;!()\"'“”‘’«»")
	last := strings.ToLower(raw)
	initial, _ := utf8.DecodeRuneInString(raw)
	for _, verbLast := range []string{"tr", "de", "nl"} {
		verbs := finalVerbs[verbLast]
		switch {
		case lang != "" && lang != verbLast:
		case verbLast == "de" && unicode.IsUpper(initial):
			// A German word written with a capital is a noun: "beim Testen".
		case verbs(last), verbLast == "tr" && (turkishPoliteImperative(last, verbs) || turkishReminder(fields, verbs)):
			return true
		}
	}
	return false
}

// turkishPoliteImperative reads "ekleyin", "düzeltiniz" or "edin" as the verb
// it asks for.
func turkishPoliteImperative(word string, verbs func(string) bool) bool {
	for _, ending := range turkishPoliteEndings {
		stem, found := strings.CutSuffix(word, ending)
		if !found || utf8.RuneCountInString(stem) < 2 {
			continue
		}
		if verbs(stem) || strings.HasSuffix(stem, "d") && verbs(strings.TrimSuffix(stem, "d")+"t") {
			return true
		}
	}
	return false
}

// trailingPoliteWords may close a step after its verb.
var trailingPoliteWords = lazyWordSet(`please pls plz lütfen bitte alsjeblieft`)

// turkishReminder reads "yedek almayı unutma" or "güncellemeyi de
// unutmayın", "don't forget to take a backup", as the step it names when
// that is a work verb, and not "… eski olduğunu unutma", a fact to keep in
// mind.
func turkishReminder(fields []string, verbs func(string) bool) bool {
	last := stepWord(fields[len(fields)-1])
	if last != "unutma" && !turkishPoliteImperative(last, func(stem string) bool { return stem == "unutma" }) {
		return false
	}
	index := len(fields) - 2
	if index > 0 && (stepWord(fields[index]) == "de" || stepWord(fields[index]) == "da") {
		index--
	}
	if index < 0 {
		return false
	}
	for _, ending := range []string{"mayı", "meyi"} {
		if stem, found := strings.CutSuffix(stepWord(fields[index]), ending); found && verbs(stem) {
			return true
		}
	}
	return false
}

// mainClauseImperative tells "If the cache is stale, delete it" or "For the
// login page, add rate limiting": a step whose first clause sets the scene.
func mainClauseImperative(unit string) bool {
	_, rest, found := strings.Cut(unit, ", ")
	return found && leadsWithImperative(wordSplit.Split(strings.TrimSpace(rest), -1))
}

var sequenceMarkers = []string{
	" then ", " after that", " afterwards", " finally", " lastly", " next,",
	" sonra ", " ardından", " son olarak", " daha sonra", " en son ",
	" dann ", " danach", " schließlich", " zuletzt",
	" ensuite", " puis ", " enfin",
	" luego ", " después", " finalmente", " por último",
	" depois", " em seguida", " por fim",
	" poi ", " dopo ", " infine",
	" daarna", " vervolgens", " ten slotte",
	" potem", " następnie", " na koniec",
	" затем", " потом", " наконец",
	" lalu ", " kemudian", " setelah itu", " akhirnya", " selanjutnya",
	"それから", "次に", "最後に", "然后", "接着", "最后", "그다음", "그런 다음", "마지막으로", "ثم ", "بعد ذلك", "أخيرا",
}

var bugReportMarkers = []string{
	"steps to reproduce", "to reproduce", "repro steps", "expected:", "expected result", "expected behaviour", "expected behavior",
	"actual:", "actual result", "stack trace", "stacktrace", "traceback",
	"yeniden üret", "beklenen", "hata mesajı", "hata çıktısı",
}

func lazyWordSet(words string) func(string) bool {
	var once sync.Once
	var set map[string]bool
	return func(word string) bool {
		once.Do(func() { set = wordSet(words) })
		return set[word]
	}
}

func wordSet(words string) map[string]bool {
	set := map[string]bool{}
	for _, word := range strings.Fields(words) {
		set[word] = true
	}
	return set
}

func firstWord(text string) string {
	fields := wordSplit.Split(strings.TrimSpace(text), 2)
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(strings.Trim(fields[0], ",.:;!()\"'“”‘’«»"))
}

func endsQuestion(text string) bool {
	trimmed := strings.TrimRight(strings.TrimSpace(text), "\"'”’)»* ")
	return strings.HasSuffix(trimmed, "?") || strings.HasSuffix(trimmed, "？")
}

func stepLike(unit, lang string) bool {
	unit = strings.TrimSpace(unit)
	if unit == "" || endsQuestion(unit) || strings.HasSuffix(unit, ":") || logLikeLine.MatchString(unit) || reportLine(unit) {
		return false
	}
	if len(wordSplit.Split(unit, -1)) < autoQueueMinWords {
		return false
	}
	return !descriptiveStarter(lang, firstWord(unit)) || endsInVerb(unit, lang) || mainClauseImperative(unit) || englishLeadInStep(unit, lang)
}

var (
	// requestLeadIn and needLeadIn open an English step with words the
	// starters read as a description: "I want you to add …", "We need to
	// move …", "I would like you to write …"; "It needs a dark mode toggle".
	requestLeadIn = lazyRegexp(`^(?:i|we)(?:'d|’d|\s+would)?(?:\s+(?:really|also|just|still))?\s+(?:want|need|like)(?:\s+you)?\s+to\s+(\S.*)$`)
	needLeadIn    = lazyRegexp(`^(?:it|this|that)(?:\s+(?:also|still|really))?\s+(?:needs|requires)\s+(\S.*)$`)
	// joinStarters join a step to the one before it: "But first check …".
	joinStarters = lazyWordSet(`and but so however`)
	// sceneDeterminers may open the scene of "For the login page add …";
	// sceneStops end it: a subject, a verb of its own, a clause or a phrase.
	sceneDeterminers = lazyWordSet(`the a an this that these those each every all both any our my your its their`)
	sceneStops       = lazyWordSet(`i we you he she it they me us them who which whose where when while
		is are was were be been being has have had do does did will would can could should must may might
		to of on in at by with from into about than`)
)

// englishLeadInStep tells an English step that opens with a word the
// starters read as a description: a request ("I want you to add a retry"),
// a need of the thing itself ("It needs a dark mode toggle"), a joining
// word ("But first check …") or a scene set without a comma ("For the login
// page add rate limiting", "As a follow-up bump the version").
func englishLeadInStep(unit, lang string) bool {
	if _, own := descriptiveStarters[lang]; own && lang != "en" {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(unit))
	if match := requestLeadIn.FindStringSubmatch(lower); match != nil {
		return leadsWithImperative(wordSplit.Split(match[1], -1))
	}
	if match := needLeadIn.FindStringSubmatch(lower); match != nil {
		rest := wordSplit.Split(match[1], -1)
		return stepWord(rest[0]) != "to" || leadsWithImperative(rest[1:])
	}
	fields := wordSplit.Split(lower, -1)
	if joinStarters(stepWord(fields[0])) {
		return leadsWithImperative(fields[1:])
	}
	return sceneImperative(fields)
}

// sceneImperative tells "For the login page add rate limiting": after "for"
// or "as", perhaps a determiner ("each of the"), and one to three words of a
// scene, a work verb that no noun follower shows to be a noun.
func sceneImperative(fields []string) bool {
	if first := stepWord(fields[0]); first != "for" && first != "as" {
		return false
	}
	index := 1
	if sceneDeterminers(stepWord(wordAt(fields, index))) {
		index++
		if stepWord(wordAt(fields, index)) == "of" && sceneDeterminers(stepWord(wordAt(fields, index+1))) {
			index += 2
		}
	}
	for words := 0; index < len(fields) && words <= 3; index++ {
		word := stepWord(fields[index])
		if words > 0 && imperativeWords(word) {
			return index+1 == len(fields) || !nounFollowers(stepWord(fields[index+1]))
		}
		if sceneStops(word) || sceneDeterminers(word) {
			return false
		}
		words++
	}
	return false
}

// reportFieldLabels hold the labels a report writes before the fields of a
// finding ("Where: file.go:12", "Status: confirmed", "Fix direction: …"), a
// space written as "_"; passedWords report a check as passed.
var (
	reportFieldLabels = normalWordSet(`where status severity impact evidence cause root_cause why what_happens what_happened
		fix_direction suggested_fix proposed_fix outcome verdict confidence finding
		nerede durum önem etki kanıt neden kök_neden ne_oluyor ne_oldu düzeltme_yönü önerilen_düzeltme karar bulgu`)
	passedWords = lazyWordSet(`pass passed passes passing ok clean green`)
)

// reportLine tells a listed line that reports rather than asks: a field of a
// finding under a label such as "Where:" or "Fix direction:", or a check that
// passed ("gofmt -l ./cmd/noctis: pass", "go test ./...: 14 passed, 0 failed").
func reportLine(unit string) bool {
	label, value, found := strings.Cut(unit, ":")
	if !found {
		return false
	}
	if reportFieldLabels(heldNormal(strings.Join(strings.Fields(strings.Trim(label, "*_` ")), "_"))) {
		return true
	}
	words := strings.Fields(strings.TrimLeft(value, "*_` "))
	if len(words) > 1 && (words[0] == "all" || words[0] == "both" || strings.Trim(words[0], "0123456789/") == "") {
		words = words[1:]
	}
	if len(words) == 0 || !passedWords(stepWord(words[0])) {
		return false
	}
	return len(words) == 1 || stepWord(words[0]) != strings.ToLower(words[0]) || strings.HasPrefix(words[1], "(")
}

// promptJob is what a prompt asks for when it reads like a multi-step job.
type promptJob struct {
	items   []string // the steps, in order
	dropped []string // listed lines not read as steps: notes, questions, lines too short to tell
	cut     []string // steps past the first autoQueueMaxItems, kept in the file without checkboxes
	text    string   // the prompt without its code blocks
	prose   string   // the words around the listed steps
	lead    bool     // the steps are lines of their own, so the words around them may be read alone
}

// withoutCodeBlocks drops a prompt's fenced code blocks. Most prompts have no fence, and those are
// handed back as they are, without building the pattern.
func withoutCodeBlocks(prompt string) string {
	if !strings.Contains(prompt, "```") {
		return prompt
	}
	return codeFence.ReplaceAllString(prompt, " ")
}

// agentTurnMarkers are the words Claude Code writes around a user turn it
// makes itself: a subagent's report handed back or a message from another
// session, a background task's notice, a system notice, a Stop hook's
// feedback.
var agentTurnMarkers = []string{"Another Claude session sent a message:", "<agent-message", `<\agent-message`, "[Subagent hand-back]", "<task-notification>", "[SYSTEM NOTIFICATION", "Stop hook feedback:"}

// agentWrittenTurn tells why a prompt is a turn Claude Code wrote, not a
// request the user typed, or returns "". Such a turn is never queued: its
// lines are another agent's words, not work the user asked for.
func agentWrittenTurn(prompt string) string {
	for _, marker := range agentTurnMarkers {
		if strings.Contains(prompt, marker) {
			return `the prompt is a turn Claude Code wrote, not a request the user typed: it holds "` + marker + `"`
		}
	}
	return ""
}

func promptJobOf(prompt string) promptJob {
	text := strings.TrimSpace(withoutCodeBlocks(prompt))
	job := promptJob{text: text}
	if len([]rune(text)) < autoQueueMinChars || hasBugReportMarker(text) {
		return job
	}
	body, closing := text, ""
	lang := detectLanguage(text)
	listed, skipped, lines := []string{}, []string{}, []string{}
	if endsQuestion(text) {
		start := closingAsk(text)
		if start < 0 {
			return job
		}
		body, closing = strings.TrimSpace(text[:start]), strings.TrimSpace(text[start:])
		if match := listItemLine.FindStringSubmatch(closing); match != nil {
			if item := cleanItem(match[1]); item != "" {
				skipped = append(skipped, item)
			}
		}
	}
	logLines, questions := 0, 0
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		if endsQuestion(line) {
			questions++
		}
		if logLikeLine.MatchString(line) {
			logLines++
		}
		if match := listItemLine.FindStringSubmatch(line); match != nil {
			switch item := cleanItem(match[1]); {
			case item == "":
			case stepLike(item, lang):
				listed = append(listed, item)
			case !strings.HasSuffix(item, ":"):
				skipped = append(skipped, item)
			}
			continue
		}
		lines = append(lines, line)
	}
	if logLines >= 2 || questions > len(listed) {
		return job
	}
	items, prose := listed, lines
	if len(items) < autoQueueMinItems {
		items, prose, skipped = items[:0], []string{}, nil
		for _, line := range lines {
			if stepLike(line, lang) && imperativeLike(line) {
				if item := cleanItem(line); item != "" {
					items = append(items, item)
					continue
				}
			}
			prose = append(prose, line)
		}
		if len(items) < autoQueueMinLines || len(items)*2 < len(lines) {
			items = items[:0]
		}
	}
	job.lead = len(items) > 0
	if len(items) == 0 && len(lines) == 1 && hasSequenceMarker(body) {
		items, prose = sentenceSteps(body, lang), []string{body}
	}
	if closing != "" {
		prose = append(prose, closing)
	}
	items = dedupeItems(items)
	if len(items) < autoQueueMinItems {
		return job
	}
	if len(items) > autoQueueMaxItems {
		items, job.cut = items[:autoQueueMaxItems], append([]string{}, items[autoQueueMaxItems:]...)
	}
	job.items, job.dropped, job.prose = items, dedupeItems(skipped), strings.Join(prose, "\n")
	return job
}

// closingAsk finds a closing request for the listed work written as a polite
// question, "Can you do these?" or "Bunları yapabilir misin?", and returns
// where it starts; -1 when the prompt ends in any other question.
func closingAsk(text string) int {
	body := strings.TrimRight(text, "\"'”’)»* \t\r\n")
	_, size := utf8.DecodeLastRuneInString(body)
	head := body[:len(body)-size]
	start := strings.LastIndexFunc(head, func(r rune) bool { return strings.ContainsRune(".!?;\n。！？；", r) })
	if start < 0 {
		return -1
	}
	_, width := utf8.DecodeRuneInString(head[start:])
	if !politeWorkAsk(body[start+width:]) {
		return -1
	}
	return start + width
}

// sentenceSteps reads one paragraph of steps tied by sequence words: a step a
// sentence, or, when the sentences hold too few steps, a step a part of a
// comma chain ("önce X oluştur, sonra Y yaz, ardından Z ekle").
func sentenceSteps(paragraph, lang string) []string {
	units := sentenceSplit.Split(paragraph, -1)
	items := stepsOf(units, lang)
	if len(items) < autoQueueMinSentences {
		chained := []string{}
		for _, unit := range units {
			chained = append(chained, chainedSteps(unit, lang)...)
		}
		items = stepsOf(chained, lang)
	}
	if len(items) < autoQueueMinSentences {
		return nil
	}
	return items
}

func stepsOf(units []string, lang string) []string {
	items := []string{}
	for _, unit := range units {
		if stepLike(unit, lang) && imperativeLike(unit) {
			if item := cleanItem(unit); item != "" {
				items = append(items, item)
			}
		}
	}
	return items
}

var chainConjunctions = []string{"and ", "ve ", "und ", "et ", "y ", "e ", "en ", "i ", "и ", "а "}

// chainedSteps splits a sentence at each comma a sequence word follows, as
// long as every part reads as a step of its own; otherwise the sentence stays
// whole.
func chainedSteps(sentence, lang string) []string {
	parts, start := []string{}, 0
	for from := 0; ; {
		comma := strings.Index(sentence[from:], ", ")
		if comma < 0 {
			break
		}
		at := from + comma
		if skip := chainMarker(sentence[at+2:]); skip >= 0 {
			parts = append(parts, strings.TrimSpace(sentence[start:at]))
			start = at + 2 + skip
		}
		from = at + 2
	}
	if len(parts) == 0 {
		return []string{sentence}
	}
	parts = append(parts, strings.TrimSpace(sentence[start:]))
	for _, part := range parts {
		if !stepLike(part, lang) || !imperativeLike(part) {
			return []string{sentence}
		}
	}
	return parts
}

// chainMarker tells whether text starts with a sequence word, perhaps after
// a conjunction ("and finally"), and returns how many bytes of conjunction
// come before it; -1 when it does not.
func chainMarker(text string) int {
	lower := strings.ToLower(text)
	starts := func(offset int) bool {
		candidate := " " + lower[offset:]
		for _, marker := range sequenceMarkers {
			if strings.HasPrefix(candidate, marker) || strings.HasPrefix(candidate, strings.TrimRight(marker, " ,")+" ") {
				return true
			}
		}
		return false
	}
	if starts(0) {
		return 0
	}
	for _, conjunction := range chainConjunctions {
		if strings.HasPrefix(lower, conjunction) && len(lower) == len(text) && starts(len(conjunction)) {
			return len(conjunction)
		}
	}
	return -1
}

func dedupeItems(items []string) []string {
	seen := make(map[string]bool, len(items))
	kept := items[:0]
	for _, item := range items {
		key := strings.ToLower(strings.TrimSpace(item))
		if seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, item)
	}
	return kept
}

func hasBugReportMarker(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range bugReportMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func hasSequenceMarker(text string) bool {
	lower := " " + strings.ToLower(text) + " "
	for _, marker := range sequenceMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func cleanItem(text string) string {
	item := strings.TrimSpace(text)
	item = strings.TrimLeft(item, "-*• ")
	if match := checkboxPrefix.FindStringSubmatch(item); match != nil {
		if match[1] != "" {
			return ""
		}
		item = item[len(match[0]):]
	}
	item = strings.TrimSpace(strings.ReplaceAll(item, "\n", " "))
	if utf8.RuneCountInString(item) > 200 {
		kept := 0
		for index := range item {
			if kept == 200 {
				item = item[:index] + "…"
				break
			}
			kept++
		}
	}
	if len(wordSplit.Split(item, -1)) < 2 {
		return ""
	}
	return item
}

func autoQueueDir() string {
	return filepath.Join(files.guardDir, "queues")
}

func startAutoQueue(sid, cwd string, job promptJob, now int64) string {
	lines := []string{"# " + pluginName + " — job from the prompt at " + localISO(float64(now)), ""}
	for _, item := range job.items {
		lines = append(lines, "- [ ] "+item)
	}
	lines = append(lines, asideLines(autoQueueCutHeading(), job.cut)...)
	lines = append(lines, asideLines("## Not on the checklist: listed lines not read as steps", job.dropped)...)
	words := strings.Join(append(append(append([]string{}, job.items...), job.cut...), job.dropped...), "\n")
	path := writeSessionQueue(sid, object{"cwd": cwd, "at": float64(now), "items": float64(len(job.items)), "words": jobWordDigests(words)}, lines)
	if path == "" {
		return ""
	}
	reason := fmt.Sprintf("%d items", len(job.items))
	if len(job.cut) > 0 {
		reason += fmt.Sprintf("; %d more steps over the %d-item limit left out", len(job.cut), autoQueueMaxItems)
	}
	if len(job.dropped) > 0 {
		reason += fmt.Sprintf("; %d listed lines not read as steps", len(job.dropped))
	}
	journal(sid, "UserPromptSubmit", "auto-queue", reason, nil)
	logInfo("auto queue for %s: %d items in %s", sid, len(job.items), path)
	return path
}

func autoQueueCutHeading() string {
	return fmt.Sprintf("## Not on the checklist: steps over the %d-item limit", autoQueueMaxItems)
}

// leftOutSteps reads the steps a checklist file keeps under the heading for
// the steps over the limit, which are still open when the checklist is done.
func leftOutSteps(content string) []string {
	steps, under := []string{}, false
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		switch {
		case strings.HasPrefix(line, "#"):
			under = line == autoQueueCutHeading()
		case under && strings.HasPrefix(line, "- ") && !checkboxPrefix.MatchString(strings.TrimPrefix(line, "- ")):
			if step := strings.TrimSpace(strings.TrimPrefix(line, "- ")); step != "" {
				steps = append(steps, step)
			}
		}
	}
	return steps
}

// asideLines puts lines of the prompt that the checklist leaves out under a
// heading of their own, as plain bullets, which do not drive the session.
func asideLines(heading string, texts []string) []string {
	if len(texts) == 0 {
		return nil
	}
	lines := []string{"", heading, ""}
	for _, text := range texts {
		lines = append(lines, "- "+text)
	}
	return lines
}

// namedLines quotes the first few lines of a list for a notice, and says how
// many more there are.
func namedLines(texts []string) (note, notice string) {
	shown := []string{}
	for _, text := range texts[:min(len(texts), queueUnmatchedNamed)] {
		shown = append(shown, `"`+truncateText(text, 80)+`"`)
	}
	note = strings.Join(shown, "; ")
	notice = note
	if more := len(texts) - len(shown); more > 0 {
		note, notice = fmt.Sprintf("%s and %d more", note, more), T("queue.unmatchedMore", note, more)
	}
	return note, notice
}

func sessionQueuePath(sid string) string {
	return filepath.Join(autoQueueDir(), safeName(sid)+".md")
}

func writeSessionQueue(sid string, record object, lines []string) string {
	ensureDir(autoQueueDir())
	path := sessionQueuePath(sid)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		warn("auto queue could not be written: %v", err)
		return ""
	}
	record["path"] = path
	updateState(func(state object) {
		stateMap(state, "autoQueues")[sid] = record
		delete(stateMap(state, "queueVerify"), queueTrustKey(path))
	})
	return path
}

func endAutoQueue(sid string, removeFile bool) {
	state := readState()
	record := getMap(getMap(state, "autoQueues"), sid)
	if record == nil {
		return
	}
	if removeFile && isAutoQueue(getString(record, "path")) {
		_ = os.Remove(getString(record, "path"))
	}
	updateState(func(next object) {
		delete(stateMap(next, "autoQueues"), sid)
		delete(stateMap(next, "queueVerify"), queueTrustKey(getString(record, "path")))
	})
}

func queueFileFor(cfg object, cwd, sid string) string {
	if file := queueFile(cfg, cwd); file != "" {
		return file
	}
	return sessionQueueOr(sid, func() string { return "" })
}

func sessionQueueFile(cfg object, sid string, dirs ...string) string {
	return sessionQueueOr(sid, func() string { return followedQueueFile(cfg, dirs...) })
}

func sessionQueueOr(sid string, project func() string) string {
	record := getMap(getMap(readState(), "autoQueues"), sid)
	own := getString(record, "path")
	if own != "" && statSafe(own) == nil {
		own = ""
	}
	if own != "" && getString(record, "source") != "" {
		return own
	}
	if file := project(); file != "" {
		return file
	}
	return own
}

func isAutoQueue(path string) bool {
	return path != "" && path == filepath.Join(autoQueueDir(), filepath.Base(path))
}

func queueTrusted(cfg object, path string) bool {
	trusted, _, _ := queueTrustGap(cfg, path)
	return trusted
}

func trustedQueueSnapshot(cfg object, path string) (queueView, bool) {
	content, _ := readQueueText(path)
	if trusted, _, _ := queueTrustGapOf(cfg, path, content); !trusted {
		return queueView{}, false
	}
	return queueSnapshotOf(path, content), true
}

func queueNeedsTrust(cfg object, path string) bool {
	return !isAutoQueue(path) && getBool(section(cfg, "queue"), "requireTrust", true)
}

func queueFollowed(cfg object, path string) bool {
	return !queueNeedsTrust(cfg, path) || numberOr(getMap(getMap(readState(), "queueTrust"), queueTrustKey(path)), "at", 0) > 0
}

func queueEditRule(cfg object, path string) string {
	if !queueNeedsTrust(cfg, path) {
		return ""
	}
	return " When an item is done, change only its checkbox to [x]; do not add, edit or remove any other text in the file, since any other change makes noctis wait until the user trusts the file again."
}

func queueTrustGap(cfg object, path string) (bool, []string, bool) {
	content, _ := readQueueText(path)
	return queueTrustGapOf(cfg, path, content)
}

func queueTrustGapOf(cfg object, path, content string) (bool, []string, bool) {
	if !queueNeedsTrust(cfg, path) {
		return true, nil, false
	}
	return queueTrustRecordGap(path, content)
}

// queueTrustRecordGap compares content with the trust the user gave path with noctis queue trust:
// whether it still matches, the lines added or changed since, and whether the record comes from an
// older noctis that kept no lines. It holds whatever queue.requireTrust says.
func queueTrustRecordGap(path, content string) (bool, []string, bool) {
	record := getMap(getMap(readState(), "queueTrust"), queueTrustKey(path))
	if numberOr(record, "at", 0) <= 0 {
		return false, nil, false
	}
	if getList(record, "lines") == nil || getList(record, "open") == nil {
		return false, nil, true
	}
	lines, open := digestSet(getList(record, "lines")), digestSet(getList(record, "open"))
	changed := []string{}
	for _, unit := range queueTrustUnits(content) {
		if !lines[unit.line] || unit.open != "" && !open[unit.open] {
			changed = append(changed, unit.shown)
		}
	}
	return len(changed) == 0, changed, false
}

func digestSet(values []any) map[string]bool {
	set := map[string]bool{}
	for _, value := range values {
		if digest, ok := value.(string); ok {
			set[digest] = true
		}
	}
	return set
}

func queueItemDigest(text string) string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(text), " ")))
	return hex.EncodeToString(sum[:16])
}

type queueTrustUnit struct {
	line, open, shown string
}

func queueTrustUnits(content string) []queueTrustUnit {
	entries, plain := parseQueueEntries(content)
	starts, covered := map[int]queueEntry{}, map[int]bool{}
	for _, entry := range entries {
		starts[entry.first] = entry
		for index := entry.first; index <= entry.last; index++ {
			covered[index] = true
		}
	}
	units := []queueTrustUnit{}
	for index, line := range strings.Split(strings.TrimPrefix(content, "\uFEFF"), "\n") {
		if entry, ok := starts[index]; ok {
			head, _, _ := queueLineParts(line)
			if plain {
				head, _, _ = queueBulletParts(line)
			}
			core := strings.Join(strings.Fields(queueUndone(head)+strings.TrimPrefix(entry.text, head)), " ")
			unit := queueTrustUnit{line: queueItemDigest("- [ ] " + core), shown: "- [x] " + core}
			if !entry.checked {
				text := strings.Join(strings.Fields(entry.text), " ")
				unit.open, unit.shown = queueItemDigest(text), "- [ ] "+text
			}
			units = append(units, unit)
		} else if text := strings.Join(strings.Fields(line), " "); text != "" && !covered[index] {
			units = append(units, queueTrustUnit{line: queueItemDigest(text), shown: text})
		}
	}
	return units
}

func queueTrustKey(path string) string {
	return safeName(filepath.Base(path)) + "-" + hashKey(path)
}

func trustQueueFile(path string, trusted bool) {
	key := queueTrustKey(path)
	lines, open := []any{}, []any{}
	if trusted {
		content, _ := readQueueText(path)
		seenLines, seenOpen := map[string]bool{}, map[string]bool{}
		for _, unit := range queueTrustUnits(content) {
			if !seenLines[unit.line] {
				seenLines[unit.line] = true
				lines = append(lines, unit.line)
			}
			if unit.open != "" && !seenOpen[unit.open] {
				seenOpen[unit.open] = true
				open = append(open, unit.open)
			}
		}
	}
	updateState(func(next object) {
		records := stateMap(next, "queueTrust")
		if trusted {
			now := float64(nowSec())
			records[key] = object{"at": now, "used": now, "path": path, "lines": lines, "open": open}
			return
		}
		delete(records, key)
	})
}

func touchQueueTrust(path string, now int64) {
	key := queueTrustKey(path)
	if record := getMap(getMap(readState(), "queueTrust"), key); record == nil || float64(now)-numberOr(record, "used", 0) < 3600 {
		return
	}
	updateState(func(next object) {
		if record := getMap(getMap(next, "queueTrust"), key); record != nil {
			record["used"] = float64(now)
		}
	})
}

var nestedShells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "fish": true, "pwsh": true, "powershell": true, "cmd": true, "eval": true}

var codeRunners = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "fish": true, "pwsh": true, "powershell": true, "cmd": true, "py": true, "node": true, "perl": true, "ruby": true, "php": true, "osascript": true, "xargs": true, "eval": true, "source": true, "iex": true, "invoke-expression": true, "start-process": true, "start": true, "saps": true}

var heredocReaders = map[string]bool{"cat": true, "tee": true, "git": true, "gh": true}

type protectedCommand struct {
	words   []string
	message string
	journal string
	logName string
	reason  string
}

var protectedNoctis = []protectedCommand{
	{[]string{"queue", "trust"}, "queue.trustByModel", "deny-queue-trust", "noctis queue trust", "Only the user may trust a queue file: a trusted file drives sessions without asking, so a person reads its items first. Do not run noctis queue trust yourself and do not trust the file another way. Tell the user the file waits for their trust; after reading it they can type !noctis queue trust themselves."},
	{[]string{"state-write"}, "queue.stateWriteByModel", "deny-state-write", "noctis state-write", "noctis state-write is for the test harness: it replaces noctis's whole state file, including the queue trust records, without asking. Do not run it, and do not write noctis's state another way. It is not a way to trust a queue file; only the user does that, with !noctis queue trust after reading the file."},
}

var (
	pythonRunner = lazyRegexp(`^python[0-9.]*$`)
	dotSourcing  = lazyRegexp(`(?m)(?:^|[;&|({])[ \t]*\.[ \t]`)
	splitString  = lazyRegexp(`^(?:-[a-z]*s|--split-string)`)
	shellQuotes  = strings.NewReplacer("'", "", `"`, "", "^", "", "`", "")
)

func denyQueueTrustByModel(input object) {
	command := getString(getMap(input, "tool_input"), "command")
	target := deniedNoctisCommand(command, getString(input, "tool_name") == "PowerShell")
	if target == nil {
		return
	}
	cfg := loadConfig()
	sid := sessionKey(input)
	applySessionLocale(cfg, peekState(), sid)
	journal(sid, "PreToolUse", target.journal, truncateText(command, 200), nil)
	logInfo("denied %s run by the model in %s", target.logName, sid)
	emit(object{
		"systemMessage":      T(target.message, pluginName),
		"hookSpecificOutput": object{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": target.reason},
	})
}

func deniedNoctisCommand(command string, powershell bool) *protectedCommand {
	dialect := bashDialect
	if powershell {
		dialect = powershellDialect
	}
	reading := readShellCommand(command, dialect, false)
	plain := strings.ReplaceAll(strings.ToLower(shellQuotes.Replace(reading.text)), `\`, "")
	glob := false
	for _, segment := range reading.segments {
		for _, word := range segment {
			glob = glob || word.pattern != ""
		}
	}
	if !glob && !strings.Contains(plain, pluginName) {
		return nil
	}
	live := []protectedCommand{}
	for _, target := range protectedNoctis {
		if glob || strings.Contains(plain, target.words[len(target.words)-1]) {
			live = append(live, target)
		}
	}
	if len(live) == 0 {
		return nil
	}
	if target := namesProtected(reading.segments, live); target != nil {
		return target
	}
	words := shellTokens(reading.text)
	if !runsCodeAnotherWay(reading.text, words, powershell, false) {
		return nil
	}
	return protectedFollowsNoctis(words, live)
}

// namesProtected finds a segment that runs the noctis binary directly with a
// protected subcommand. The reader has already applied the shell's quoting,
// escapes, expansions and here-documents, so each word is what the program
// would receive; a word built at run time stays unknown and is not matched.
// A noctis job runs what follows --: several words are in the segment and
// are looked at with it, and a single one is a command line of its own.
func namesProtected(segments [][]shellArgument, live []protectedCommand) *protectedCommand {
	for _, words := range segments {
		for index, word := range words {
			if !namesNoctis(word) {
				continue
			}
			parsed := parseArgs(argumentWords(words[index+1:]))
			for i := range live {
				if startsWithWords(parsed.positional, live[i].words) {
					return &live[i]
				}
			}
			if len(parsed.rest) == 1 && startsWithWords(parsed.positional, []string{"job"}) {
				if target := deniedNoctisCommand(words[len(words)-1].text, false); target != nil {
					return target
				}
			}
		}
	}
	return nil
}

func plainWord(word string) string {
	return strings.ReplaceAll(strings.ToLower(shellQuotes.Replace(word)), `\`, "")
}

func wordPrograms(word string) (string, string) {
	lower := strings.ToLower(shellQuotes.Replace(word))
	return programName(strings.ReplaceAll(lower, `\`, "")), programName(strings.ReplaceAll(lower, `\`, "/"))
}

func programName(word string) string {
	return strings.TrimSuffix(strings.ToLower(word[strings.LastIndexAny(word, `/\`)+1:]), ".exe")
}

type shellToken struct {
	plain, path string
}

func shellTokens(text string) []shellToken {
	words := []shellToken{}
	for _, field := range strings.FieldsFunc(strings.ToLower(shellQuotes.Replace(text)), func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || strings.ContainsRune("|;&()<>{}[],$", r)
	}) {
		parts := []string{field}
		if !strings.HasPrefix(field, "-") {
			parts = strings.Split(field, "=")
		}
		for _, part := range parts {
			if part != "" {
				words = append(words, shellToken{plain: strings.ReplaceAll(part, `\`, ""), path: strings.ReplaceAll(part, `\`, "/")})
			}
		}
	}
	return words
}

func (word shellToken) names(match func(string) bool) bool {
	return match(programName(word.plain)) || match(programName(word.path))
}

func runsCodeAnotherWay(text string, words []shellToken, powershell, lenient bool) bool {
	if substitutes(text, powershell, lenient) || dotSourcing.get().MatchString(text) {
		return true
	}
	readers := xargsRunsOnlyReaders(text, words)
	for index, word := range words {
		if word.names(func(name string) bool {
			return codeRunners[name] && (name != "xargs" || !readers) || pythonRunner.get().MatchString(name)
		}) {
			return true
		}
		if word.names(func(name string) bool { return name == "env" }) {
			for _, next := range words[index+1:] {
				if splitString.get().MatchString(next.plain) {
					return true
				}
			}
		}
	}
	return false
}

var (
	xargsReaders    = wordSet("echo printf cat grep egrep fgrep rg wc ls head tail sort uniq cut file stat du basename dirname realpath readlink touch rm mkdir rmdir")
	xargsValueFlags = wordSet("-a -d -E -I -L -n -P -s -J -R -S --arg-file --delimiter --max-args --max-procs --max-chars --process-slot-var")
)

func xargsRunsOnlyReaders(command string, tokens []shellToken) bool {
	named := 0
	for _, token := range tokens {
		if token.names(func(name string) bool { return name == "xargs" }) {
			named++
		}
	}
	for _, words := range pipelineWords(command) {
		for index, word := range words {
			if plain, path := wordPrograms(word); plain == "xargs" || path == "xargs" {
				named--
				if !xargsRunsReader(words[index+1:]) {
					return false
				}
			}
		}
	}
	return named == 0
}

func xargsRunsReader(words []string) bool {
	for i := 0; i < len(words); i++ {
		switch word := words[i]; {
		case word == "--":
			return i+1 == len(words) || xargsReaders[plainWord(words[i+1])]
		case strings.HasPrefix(word, "-"):
			if xargsValueFlags[word] {
				i++
			}
		default:
			return xargsReaders[plainWord(word)]
		}
	}
	return true
}

func pipelineWords(command string) [][]string {
	segments, words := [][]string{}, []string{}
	var text strings.Builder
	open := false
	endWord := func() {
		if open {
			words = append(words, text.String())
		}
		text.Reset()
		open = false
	}
	for i := 0; i < len(command); i++ {
		switch c := command[i]; {
		case c == '\'' || c == '"':
			end := i + 1
			for ; end < len(command) && command[end] != c; end++ {
				if c == '"' && command[end] == '\\' && end+1 < len(command) {
					end++
				}
				text.WriteByte(command[end])
			}
			open, i = true, end
		case c == '\\' && i+1 < len(command):
			i++
			text.WriteByte(command[i])
			open = true
		case c == ' ' || c == '\t':
			endWord()
		case strings.IndexByte("\n\r;&|()", c) >= 0:
			endWord()
			if len(words) > 0 {
				segments, words = append(segments, words), []string{}
			}
		default:
			text.WriteByte(c)
			open = true
		}
	}
	endWord()
	if len(words) > 0 {
		segments = append(segments, words)
	}
	return segments
}

func substitutes(text string, powershell, lenient bool) bool {
	quote := byte(0)
	for i := 0; i < len(text); i++ {
		c := text[i]
		if quote == '\'' {
			if c == '\'' {
				quote = 0
			}
			continue
		}
		next := byte(0)
		if i+1 < len(text) {
			next = text[i+1]
		}
		switch {
		case c == '\\' && !powershell:
			i++
		case c == '`':
			return true
		case c == '\'' && quote == 0:
			quote = '\''
		case c == '"':
			quote ^= '"'
		case c == '$' && next == '(':
			if !lenient || !flagArgument(text[:i]) {
				return true
			}
		case (c == '<' || c == '>') && next == '(' && quote == 0 && !powershell:
			return true
		case c == '&' && quote == 0 && strings.HasPrefix(strings.TrimLeft(text[i+1:], " \t"), "(") && (i == 0 || !strings.ContainsRune("&<>", rune(text[i-1]))):
			return true
		}
	}
	return false
}

func flagArgument(before string) bool {
	before = strings.TrimSuffix(before, `"`)
	fields := strings.Fields(before)
	if len(fields) == 0 {
		return false
	}
	last := fields[len(fields)-1]
	previous := ""
	if len(fields) > 1 {
		previous = fields[len(fields)-2]
	}
	switch {
	case strings.HasSuffix(before, " ") || strings.HasSuffix(before, "\t"):
		return strings.HasPrefix(last, "-")
	case strings.HasSuffix(last, "="):
		return strings.HasPrefix(last, "-") || strings.HasPrefix(previous, "-")
	}
	return false
}

func protectedFollowsNoctis(words []shellToken, live []protectedCommand) *protectedCommand {
	xargs := false
	present := map[string]bool{}
	for _, word := range words {
		xargs = xargs || word.names(func(name string) bool { return name == "xargs" })
		present[word.plain] = true
	}
	for index, word := range words {
		if !word.names(func(name string) bool { return name == pluginName }) {
			continue
		}
		for i := range live {
			if _, ok := afterWords(words[index+1:], live[i].words); ok {
				return &live[i]
			}
			if xargs && missingWordsAppear(words[index+1:], live[i].words, present) {
				return &live[i]
			}
		}
	}
	return nil
}

func afterWords(words []shellToken, targets []string) ([]shellToken, bool) {
	for _, want := range targets {
		next, ok := afterWord(words, want)
		if !ok {
			return nil, false
		}
		words = next
	}
	return words, true
}

func missingWordsAppear(words []shellToken, targets []string, present map[string]bool) bool {
	matched := 0
	for matched < len(targets) {
		next, ok := afterWord(words, targets[matched])
		if !ok {
			break
		}
		words, matched = next, matched+1
	}
	if matched == 0 && len(targets) > 1 {
		return false
	}
	for _, want := range targets[matched:] {
		if !present[want] {
			return false
		}
	}
	return true
}

func afterWord(words []shellToken, want string) ([]shellToken, bool) {
	for i := 0; i < len(words); i++ {
		word := words[i].plain
		switch {
		case word == want:
			return words[i+1:], true
		case strings.HasPrefix(word, "--"):
			name, _, given := strings.Cut(word[2:], "=")
			if !given && !switchFlags[name] && i+1 < len(words) && !strings.HasPrefix(words[i+1].plain, "--") {
				i++
			}
		case strings.HasPrefix(word, "-"):
			if i+1 < len(words) && words[i+1].plain != want && !strings.HasPrefix(words[i+1].plain, "-") {
				i++
			}
		default:
			return nil, false
		}
	}
	return nil, false
}

func autoQueueDirective(path string, count int) string {
	return fmt.Sprintf(`[noctis] This request is a multi-step job (%d items). It was written as a checklist to %s. Work through it in order, mark each item "- [x]" in that file the moment it is done, and do not stop, summarize or ask for confirmation between items; the session continues until every item is ticked.`, count, path)
}

// autoQueueAsides tells Claude about the listed lines the checklist leaves
// out, which are in the file as plain lines.
func autoQueueAsides(job promptJob) string {
	text := ""
	if len(job.cut) > 0 {
		text += fmt.Sprintf(` The prompt lists %d steps, more than the %d a checklist takes: the other %d are at the end of the file under "%s" as plain lines, which do not drive the session. Do not add them to the checklist; when it is done, tell the user that these %d steps are still open.`, len(job.items)+len(job.cut), autoQueueMaxItems, len(job.cut), strings.TrimPrefix(autoQueueCutHeading(), "## "), len(job.cut))
	}
	if len(job.dropped) > 0 {
		note, _ := namedLines(job.dropped)
		text += fmt.Sprintf(` The checklist leaves out %d listed line(s) that noctis did not read as steps (notes, questions or lines too short to tell): %s. They are in the file under a heading of their own as plain lines, which do not drive the session; if one of them is a step the user asked for, turn it into a "- [ ] " line there.`, len(job.dropped), note)
	}
	return text
}
