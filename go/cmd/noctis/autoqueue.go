package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
		delete(stateMap(state, "queueVerify"), queueTrustKey(path))
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
	if removeFile && isAutoQueue(getString(record, "path")) {
		_ = os.Remove(getString(record, "path"))
	}
	updateState(func(next object) {
		delete(stateMap(next, "autoQueues"), sid)
		delete(stateMap(next, "queueVerify"), queueTrustKey(getString(record, "path")))
	})
}

func queueFileFor(cfg object, cwd, sid string) string {
	return sessionQueueFile(cfg, sid, cwd)
}

func sessionQueueFile(cfg object, sid string, dirs ...string) string {
	if file := queueFile(cfg, dirs...); file != "" {
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
	return path != "" && path == filepath.Join(autoQueueDir(), filepath.Base(path))
}

func queueTrusted(cfg object, path string) bool {
	trusted, _, _ := queueTrustGap(cfg, path)
	return trusted
}

func queueNeedsTrust(cfg object, path string) bool {
	return !isAutoQueue(path) && getBool(section(cfg, "queue"), "requireTrust", true)
}

func queueEditRule(cfg object, path string) string {
	if !queueNeedsTrust(cfg, path) {
		return ""
	}
	return " When an item is done, change only its checkbox to [x]; do not add, edit or remove any other text in the file, since any other change makes noctis wait until the user trusts the file again."
}

func queueTrustGap(cfg object, path string) (bool, []string, bool) {
	if !queueNeedsTrust(cfg, path) {
		return true, nil, false
	}
	record := getMap(getMap(readState(), "queueTrust"), queueTrustKey(path))
	if numberOr(record, "at", 0) <= 0 {
		return false, nil, false
	}
	if getList(record, "lines") == nil || getList(record, "open") == nil {
		return false, nil, true
	}
	lines, open := digestSet(getList(record, "lines")), digestSet(getList(record, "open"))
	changed := []string{}
	for _, unit := range queueTrustUnits(path) {
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

func queueFileEntries(path string) []queueEntry {
	content, ok := readQueueText(path)
	if !ok {
		return nil
	}
	entries, _ := parseQueueEntries(content)
	return entries
}

type queueTrustUnit struct {
	line, open, shown string
}

func queueTrustUnits(path string) []queueTrustUnit {
	content, ok := readQueueText(path)
	if !ok {
		return nil
	}
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
		seenLines, seenOpen := map[string]bool{}, map[string]bool{}
		for _, unit := range queueTrustUnits(path) {
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

func denyQueueTrustByModel(input object) {
	command := getString(getMap(input, "tool_input"), "command")
	if !strings.Contains(command, "trust") || !runsQueueTrust(command, 0) {
		return
	}
	cfg := loadConfig()
	sid := sessionKey(input)
	applySessionLocale(cfg, readState(), sid)
	journal(sid, "PreToolUse", "deny-queue-trust", truncateText(command, 200), nil)
	logInfo("denied noctis queue trust run by the model in %s", sid)
	emit(object{
		"systemMessage":      T("queue.trustByModel", pluginName),
		"hookSpecificOutput": object{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": "Only the user may trust a queue file: a trusted file drives sessions without asking, so a person reads its items first. Do not run noctis queue trust yourself and do not trust the file another way. Tell the user the file waits for their trust; after reading it they can type !noctis queue trust themselves."},
	})
}

func runsQueueTrust(command string, depth int) bool {
	if depth > 3 {
		return false
	}
	for _, words := range looseShellSegments(command) {
		shell := false
		for index, word := range words {
			if programName(word.text) == pluginName {
				rest := []string{}
				for _, next := range words[index+1:] {
					rest = append(rest, next.text)
				}
				if parsed := parseArgs(rest); len(parsed.positional) >= 2 && parsed.positional[0] == "queue" && parsed.positional[1] == "trust" {
					return true
				}
			}
			if shell && word.quoted && runsQueueTrust(word.text, depth+1) {
				return true
			}
			shell = shell || nestedShells[programName(word.text)]
		}
	}
	return false
}

func programName(word string) string {
	return strings.TrimSuffix(strings.ToLower(word[strings.LastIndexAny(word, `/\`)+1:]), ".exe")
}

func looseShellSegments(command string) [][]shellWord {
	segments, words := [][]shellWord{}, []shellWord{}
	var text strings.Builder
	open, quoted := false, false
	endWord := func() {
		if open {
			words = append(words, shellWord{text: text.String(), quoted: quoted})
		}
		text.Reset()
		open, quoted = false, false
	}
	for i := 0; i < len(command); i++ {
		switch c := command[i]; {
		case c == '\'' || c == '"':
			end := i + 1
			for ; end < len(command) && command[end] != c; end++ {
				if c == '"' && command[end] == '\\' && end+1 < len(command) && strings.IndexByte(`"\`, command[end+1]) >= 0 {
					end++
				}
				text.WriteByte(command[end])
			}
			open, quoted, i = true, true, end
		case c == ' ' || c == '\t':
			endWord()
		case strings.IndexByte("\n\r;&|(){}`", c) >= 0:
			endWord()
			if len(words) > 0 {
				segments, words = append(segments, words), []shellWord{}
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

func autoQueueDirective(path string, count int) string {
	return fmt.Sprintf(`[noctis] This request is a multi-step job (%d items). It was written as a checklist to %s. Work through it in order, mark each item "- [x]" in that file the moment it is done, and do not stop, summarize or ask for confirmation between items; the session continues until every item is ticked.`, count, path)
}
