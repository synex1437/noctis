package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

var descriptiveStarters = lazyWordSet(`i i'm i've we we're we've our my the this that these those there here it it's its currently
	note context background fyi for as because since when if so but and however today yesterday
	ben biz bizim benim bu şu o burada burda not bağlam mevcut halihazırda hâlihazırda çünkü eğer ama fakat ancak ve bugün dün
	ich wir unser unsere mein meine der die das es hier dort aktuell derzeit momentan hinweis kontext weil da wenn aber und
	je j'ai nous notre nos mon ma le la les ce cette il elle ici actuellement contexte parce car si mais et
	yo nosotros nuestro nuestra mi el los las este esta esto hay aquí actualmente nota contexto porque pero y
	eu nós nosso nossa meu minha a os este isto há atualmente
	io noi nostro nostra mio mia lo gli questo questa c'è qui attualmente contesto perché
	ik wij ons onze mijn het dit dat er momenteel opmerking omdat als maar en
	ja my nasz nasza mój moja to ten ta tu tutaj obecnie uwaga kontekst bo ponieważ jeśli ale i
	я мы наш наша мой моя это этот эта тут здесь сейчас примечание контекст потому если но и`)

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
	yükle indir kaydet gönder planla sırala filtrele grupla ayıkla çöz gider biçimlendir paketle derle yedekle arşivle kapsa
	sadeleştir yeniden ekleyin yazın oluşturun düzeltin güncelleyin kaldırın silin taşıyın yapın kurun çalıştırın değiştirin
	ekle yaz oluştur düzelt güncelle kaldır sil taşı dağıt yap ayarla kur çalıştır üret dönüştür değiştir iyileştir temizle
	belgele incele birleştir ayır çıkar bağla etkinleştir sağla destekle yükselt geliştir tasarla hazırla düzenle tamamla bitir
	çevir yayınla araştır ölç doğrula
	füge erstelle schreibe implementiere entferne aktualisiere baue teste prüfe konfiguriere installiere ersetze verbessere
	ajoute crée écris corrige implémente supprime mets construis teste vérifie configure installe remplace améliore
	añade agrega crea escribe corrige implementa elimina actualiza construye prueba verifica configura instala reemplaza mejora
	adicione crie escreva corrija implemente remova atualize construa teste verifique configure instale substitua melhore
	aggiungi crea scrivi correggi implementa rimuovi aggiorna costruisci testa verifica configura installa sostituisci migliora`)

var leadIns = lazyWordSet(`please lütfen bitte veuillez merci por favor per favore alsjeblieft proszę пожалуйста
	then next finally lastly afterwards after that also and now
	sonra ardından daha son olarak en ayrıca ve şimdi
	dann danach schließlich zuletzt außerdem und
	ensuite puis enfin aussi et
	luego después finalmente también y
	depois finalmente também e
	poi dopo infine anche
	daarna vervolgens ook en
	potem następnie także i
	затем потом наконец также и`)

func imperativeLike(unit string) bool {
	fields := wordSplit.Split(strings.TrimSpace(unit), -1)
	if len(fields) == 0 {
		return false
	}
	clean := func(word string) string {
		return strings.ToLower(strings.Trim(word, ",.:;!()\"'“”‘’«»"))
	}
	for index := 0; index < len(fields) && index < 4; index++ {
		word := clean(fields[index])
		if imperativeWords(word) {
			return true
		}
		if !leadIns(word) {
			break
		}
	}
	last := clean(fields[len(fields)-1])
	return imperativeWords(last) || imperativeWords(strings.TrimSuffix(strings.TrimSuffix(last, "in"), "iniz"))
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

func stepLike(unit string) bool {
	unit = strings.TrimSpace(unit)
	if unit == "" || endsQuestion(unit) || strings.HasSuffix(unit, ":") || logLikeLine.MatchString(unit) {
		return false
	}
	if len(wordSplit.Split(unit, -1)) < autoQueueMinWords {
		return false
	}
	return !descriptiveStarters(firstWord(unit))
}

func autoQueueItems(prompt string) []string {
	text := strings.TrimSpace(codeFence.ReplaceAllString(prompt, " "))
	if len([]rune(text)) < autoQueueMinChars || endsQuestion(text) || hasBugReportMarker(text) {
		return nil
	}
	listed, lines := []string{}, []string{}
	logLines, questions := 0, 0
	for _, raw := range strings.Split(text, "\n") {
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
			if item := cleanItem(match[1]); item != "" && stepLike(item) {
				listed = append(listed, item)
			}
			continue
		}
		lines = append(lines, line)
	}
	if logLines >= 2 || questions > len(listed) {
		return nil
	}
	items := listed
	if len(items) < autoQueueMinItems {
		items = items[:0]
		for _, line := range lines {
			if stepLike(line) && imperativeLike(line) {
				if item := cleanItem(line); item != "" {
					items = append(items, item)
				}
			}
		}
		if len(items) < autoQueueMinLines || len(items)*2 < len(lines) {
			items = items[:0]
		}
	}
	if len(items) == 0 && len(lines) == 1 && hasSequenceMarker(text) {
		for _, unit := range sentenceSplit.Split(text, -1) {
			if stepLike(unit) && imperativeLike(unit) {
				if item := cleanItem(unit); item != "" {
					items = append(items, item)
				}
			}
		}
		if len(items) < autoQueueMinSentences {
			items = items[:0]
		}
	}
	items = dedupeItems(items)
	if len(items) < autoQueueMinItems {
		return nil
	}
	if len(items) > autoQueueMaxItems {
		items = items[:autoQueueMaxItems]
	}
	return items
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
	if len([]rune(item)) > 200 {
		item = string([]rune(item)[:200]) + "…"
	}
	if len(wordSplit.Split(item, -1)) < 2 {
		return ""
	}
	return item
}

func autoQueueDir() string {
	return filepath.Join(files.guardDir, "queues")
}

func startAutoQueue(sid, cwd string, items []string, now int64) string {
	ensureDir(autoQueueDir())
	path := filepath.Join(autoQueueDir(), safeName(sid)+".md")
	lines := []string{"# " + pluginName + " — job from the prompt at " + localISO(float64(now)), ""}
	for _, item := range items {
		lines = append(lines, "- [ ] "+item)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		warn("auto queue could not be written: %v", err)
		return ""
	}
	updateState(func(state object) {
		stateMap(state, "autoQueues")[sid] = object{"path": path, "cwd": cwd, "at": float64(now), "items": float64(len(items))}
	})
	journal(sid, "UserPromptSubmit", "auto-queue", fmt.Sprintf("%d items", len(items)), nil)
	logInfo("auto queue for %s: %d items in %s", sid, len(items), path)
	return path
}

func endAutoQueue(sid string, removeFile bool) {
	state := readState()
	record := getMap(getMap(state, "autoQueues"), sid)
	if record == nil {
		return
	}
	if removeFile {
		_ = os.Remove(getString(record, "path"))
	}
	updateState(func(next object) { delete(stateMap(next, "autoQueues"), sid) })
}

func queueFileFor(cfg object, cwd, sid string) string {
	if file := queueFile(cfg, cwd); file != "" {
		return file
	}
	record := getMap(getMap(readState(), "autoQueues"), sid)
	if record == nil {
		return ""
	}
	path := getString(record, "path")
	if statSafe(path) == nil {
		return ""
	}
	return path
}

func isAutoQueue(path string) bool {
	return path != "" && filepath.Dir(path) == autoQueueDir()
}

func queueTrusted(cfg object, path string) bool {
	if isAutoQueue(path) || !getBool(section(cfg, "queue"), "requireTrust", true) {
		return true
	}
	return numberOr(getMap(getMap(readState(), "queueTrust"), queueTrustKey(path)), "at", 0) > 0
}

func queueTrustKey(path string) string {
	return safeName(filepath.Base(path)) + "-" + hashKey(path)
}

func trustQueueFile(path string, trusted bool) {
	key := queueTrustKey(path)
	updateState(func(next object) {
		records := stateMap(next, "queueTrust")
		if trusted {
			records[key] = object{"at": float64(nowSec()), "path": path}
			return
		}
		delete(records, key)
	})
}

func autoQueueDirective(path string, count int) string {
	return fmt.Sprintf(`[noctis] This request is a multi-step job (%d items). It was written as a checklist to %s. Work through it in order, mark each item "- [x]" in that file the moment it is done, and do not stop, summarize or ask for confirmation between items; the session continues until every item is ticked.`, count, path)
}
