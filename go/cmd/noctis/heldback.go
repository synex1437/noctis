package main

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// A prompt can list work only to ask about it: "do not implement any of the
// following, just estimate each". Such a prompt is not a job, so it gets no
// checklist and the Stop hook does not drive Claude through its items.

type heldToken struct {
	raw, word string
	stop      rune
}

var heldReplacer = sync.OnceValue(func() *strings.Replacer {
	return strings.NewReplacer("’", "'", "‘", "'", "´", "'", "`", "'", "ʼ", "'", "′", "'", "＇", "'", "ı", "i", "\u0307", "")
})

func heldNormal(text string) string {
	return heldReplacer().Replace(strings.ToLower(text))
}

func normalWordSet(words string) func(string) bool {
	var once sync.Once
	var set map[string]bool
	return func(word string) bool {
		once.Do(func() { set = wordSet(heldNormal(words)) })
		return set[word]
	}
}

const (
	sentenceStops   = ".;!?\n。；！？؛؟"
	clauseStops     = ",:()—–…，：、،"
	heldApostrophes = "'’‘´`ʼ′＇"
)

func heldTokens(text string) []heldToken {
	tokens, word := []heldToken{}, []rune{}
	flush := func() {
		if raw := strings.TrimRight(string(word), heldApostrophes+"-"); raw != "" {
			tokens = append(tokens, heldToken{raw: raw, word: heldNormal(raw)})
		}
		word = word[:0]
	}
	for _, r := range text {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r):
			word = append(word, r)
		case (strings.ContainsRune(heldApostrophes, r) || r == '-') && len(word) > 0:
			word = append(word, r)
		default:
			flush()
			if strings.ContainsRune(sentenceStops+clauseStops, r) {
				tokens = append(tokens, heldToken{stop: r})
			}
		}
	}
	flush()
	return tokens
}

func heldSentences(tokens []heldToken) [][]heldToken {
	sentences, start := [][]heldToken{}, 0
	for index, token := range tokens {
		if token.stop != 0 && strings.ContainsRune(sentenceStops, token.stop) {
			if index > start {
				sentences = append(sentences, tokens[start:index+1])
			}
			start = index + 1
		}
	}
	if start < len(tokens) {
		sentences = append(sentences, tokens[start:])
	}
	return sentences
}

func heldClauses(sentence []heldToken, joins func(string) bool) [][]heldToken {
	clauses, start := [][]heldToken{}, 0
	for index, token := range sentence {
		if token.stop != 0 || joins(token.word) {
			if index > start {
				clauses = append(clauses, sentence[start:index])
			}
			start = index + 1
		}
	}
	if start < len(sentence) {
		clauses = append(clauses, sentence[start:])
	}
	return clauses
}

func heldWords(tokens []heldToken) []string {
	words := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token.stop == 0 {
			words = append(words, token.word)
		}
	}
	return words
}

func heldQuote(tokens []heldToken) string {
	words := []string{}
	for _, token := range tokens {
		if token.stop == 0 {
			words = append(words, token.raw)
		}
	}
	quote := strings.Join(words, " ")
	if utf8.RuneCountInString(quote) > 90 {
		quote = string([]rune(quote)[:90]) + "…"
	}
	return quote
}

func wordAt(words []string, index int) string {
	if index < 0 || index >= len(words) {
		return ""
	}
	return words[index]
}

var (
	englishWorkVerbs = lazyWordSet(`implement implementing do doing start starting begin beginning execute executing carry carrying
		apply applying build building code coding make making change changing modify modifying edit editing touch touching
		write writing fix fixing work working act acting proceed proceeding perform performing develop developing
		alter altering update updating rewrite rewriting refactor refactoring tackle tackling handle handling`)
	negationFillers = lazyWordSet(`yet now actually even really please go ahead and try to bother attempt i we you want need`)
	workParticles   = lazyWordSet(`on with to at upon out`)
	objectEnders    = lazyWordSet(`and but or so because until till before unless while just only instead then since as though although`)
	conditionWords  = lazyWordSet(`if unless when whenever otherwise except once after`)
	conditionLeads  = lazyWordSet(`even only especially please that`)
	timeWords       = lazyWordSet(`yet now right away for the moment at this that point stage today tonight session time one
		immediately please all whatsoever either just anymore still currently here me us`)
	restrictWords    = lazyWordSet(`to in inside within across throughout on`)
	wholeDeterminers = lazyWordSet(`the these those this that my our your any all a an every each single`)
	wholePronouns    = lazyWordSet(`these those them it this that all`)
	weakPronouns     = lazyWordSet(`it this that`)
	wholeAdjectives  = lazyWordSet(`listed following above below remaining existing actual source whole entire single`)
	wholeNouns       = lazyWordSet(`following above below file files code codebase repo repository project change changes item items task tasks
		step steps work point points list job jobs thing things bullet bullets ticket tickets todo todos`)
	wholeQuantifiers = lazyWordSet(`any all none either each one`)
	talkVerbs        = lazyWordSet(`estimate plan explain describe outline summarize summarise review assess evaluate analyze analyse compare
		prioritize prioritise rank rate score size scope critique discuss brainstorm advise recommend suggest propose answer`)
	tellVerbs = lazyWordSet(`give tell show send write`)
	onlyWords = lazyWordSet(`just only simply merely solely`)
	talkNouns = lazyWordSet(`plan plans estimate estimates estimation estimations review explanation summary outline overview assessment
		analysis breakdown table opinion opinions thoughts recommendation recommendations proposal proposals feedback advice ranking sizing quote`)
	noChangeNouns = lazyWordSet(`changes edits implementation coding`)
	noChangeLeads = lazyWordSet(`make making with please and but so`)
	clauseJoins   = lazyWordSet(`and then but or so also plus`)
	requestLeads  = lazyWordSet(`please kindly can could would will you i we i'd we'd want need like to just only simply first now then also
		and let's lets me us go ahead quickly briefly carefully pls`)
	questionAuxiliary = lazyWordSet(`do does did is are was were have has should shall may might must am`)
	extraWorkVerbs    = lazyWordSet(`do start begin execute carry proceed perform tackle handle work`)
)

func englishWorkVerb(word string) bool {
	return !talkVerbs(word) && (imperativeWords(word) || extraWorkVerbs(word))
}

func englishNegationEnd(words []string, index int) int {
	switch words[index] {
	case "don't", "dont", "never", "shouldn't", "mustn't", "avoid":
		return index + 1
	case "do", "should", "must":
		if wordAt(words, index+1) == "not" {
			return index + 2
		}
	case "not":
		if next := wordAt(words, index+1); next == "asking" || next == "expecting" || next == "requesting" {
			return index + 2
		}
	case "no":
		if wordAt(words, index+1) == "need" && wordAt(words, index+2) == "to" {
			return index + 3
		}
	case "refrain":
		if wordAt(words, index+1) == "from" {
			return index + 2
		}
	case "let's", "lets":
		if wordAt(words, index+1) == "not" {
			return index + 2
		}
	case "let":
		if wordAt(words, index+1) == "us" && wordAt(words, index+2) == "not" {
			return index + 3
		}
	case "hold":
		if prev := wordAt(words, index-1); wordAt(words, index+1) == "off" && prev != "don't" && prev != "dont" && prev != "not" && prev != "never" {
			if particle := wordAt(words, index+2); (particle == "on" || particle == "with") && englishWorkVerbs(wordAt(words, index+3)) {
				return index + 3
			}
			return index + 2
		}
	}
	return -1
}

var laterWords = lazyWordSet(`tomorrow tonight later monday tuesday wednesday thursday friday saturday sunday next further`)

// untilLater tells "until tomorrow" or "till next week", a time the work
// waits for, from "until the tests pass", a step of the work.
func untilLater(words []string, index int) bool {
	return (words[index] == "until" || words[index] == "till") && laterWords(wordAt(words, index+1))
}

// notYetEnd reads a clause that is only "not now" or "not yet", as in
// "Implement these, but not yet", and returns where it ends, or -1.
func notYetEnd(words []string, index int) int {
	if words[index] != "not" {
		return -1
	}
	if prev := wordAt(words, index-1); prev != "" && !clauseJoins(prev) && prev != "please" && prev != "just" {
		return -1
	}
	end := index + 1
	for end < len(words) && end <= index+3 && notYetWords(words[end]) {
		end++
	}
	if last := words[end-1]; last != "now" && last != "yet" && last != "today" && last != "tonight" {
		return -1
	}
	if next := wordAt(words, end); next != "" && next != "please" {
		return -1
	}
	return end
}

var notYetWords = lazyWordSet(`now yet today tonight right just`)

// referenceHead reads a reference to the listed work at the start of words
// and returns what follows it; weak marks a bare "it", "this" or "that",
// which names the whole job only when a time word follows ("do not do it
// yet"), not in "don't change it".
func referenceHead(words []string) (rest []string, ok, weak bool) {
	if len(words) == 0 {
		return nil, false, false
	}
	index := 0
	if wholeDeterminers(words[0]) || wholePronouns(words[0]) {
		index = 1
	}
	for index+1 < len(words) && wholeAdjectives(words[index]) && (wholeAdjectives(words[index+1]) || wholeNouns(words[index+1])) {
		index++
	}
	if index < len(words) && wholeNouns(words[index]) {
		return words[index+1:], true, false
	}
	if index == 1 && wholePronouns(words[0]) {
		return words[1:], true, weakPronouns(words[0])
	}
	return nil, false, false
}

func wholeWorkTail(words []string) bool {
	for index, word := range words {
		if restrictWords(word) {
			rest, ok, _ := referenceHead(words[index+1:])
			return ok && wholeWorkTail(rest)
		}
		if untilLater(words, index) {
			return true
		}
		if !timeWords(word) {
			return false
		}
	}
	return true
}

// wholeWorkObject reports whether the words after a work verb name the whole
// job ("any of the following", "anything", "the code", "these yet") rather
// than one part of it ("the migrations folder", "anything else").
func wholeWorkObject(words []string) bool {
	if len(words) > 0 && workParticles(words[0]) {
		words = words[1:]
	}
	if len(words) == 0 {
		return true
	}
	first := words[0]
	switch {
	case first == "anything" || first == "everything":
		if next := wordAt(words, 1); next == "else" || next == "other" {
			return false
		}
		return wholeWorkTail(words[1:])
	case wholeQuantifiers(first) && wordAt(words, 1) == "of":
		rest, ok, _ := referenceHead(words[2:])
		return ok && wholeWorkTail(rest)
	case wholeDeterminers(first) || wholePronouns(first) || wholeNouns(first):
		rest, ok, weak := referenceHead(words)
		return ok && !(weak && len(rest) == 0) && wholeWorkTail(rest)
	}
	return (timeWords(first) || untilLater(words, 0)) && wholeWorkTail(words)
}

func objectEnd(words []string, from int) int {
	end := from
	for end < len(words) && words[end] != "" && (!objectEnders(words[end]) || untilLater(words, end) || anythingBut(words, end)) {
		end++
	}
	return end
}

func anythingBut(words []string, index int) bool {
	prev := wordAt(words, index-1)
	if words[index] != "but" || prev != "anything" && prev != "everything" || !wholeDeterminers(wordAt(words, index+1)) {
		return false
	}
	for _, word := range words[index+1 : min(index+5, len(words))] {
		if talkNouns(word) || listNouns(word) {
			return false
		}
	}
	return true
}

func sentenceWords(sentence []heldToken) []string {
	words := make([]string, len(sentence))
	for index, token := range sentence {
		words[index] = token.word
	}
	return words
}

// englishForbids finds "do not implement any of these", "don't touch any
// files", "never start on them yet", "hold off on these" or "but not yet" in a
// sentence, unless a condition comes first ("if a test fails, don't change it").
// A listed line is read with listOnly: only an object that names the list
// itself counts there. codeIsPart keeps "the existing code" to a rule in a
// prompt whose work is tests or docs.
func englishForbids(sentence []heldToken, listOnly, codeIsPart bool) string {
	whole := func(object []string) bool {
		if listOnly {
			return listWorkObject(object)
		}
		return wholeWorkObject(object) && !(codeIsPart && codeObject(object))
	}
	words := sentenceWords(sentence)
	conditioned := false
	for index, word := range words {
		if sentence[index].stop == ':' {
			conditioned = false
		}
		conditioned = conditioned || opensCondition(words, index)
		if conditioned {
			continue
		}
		if end := notYetEnd(words, index); end > 0 && !listOnly {
			return heldQuote(sentence[index:end])
		}
		end := englishNegationEnd(words, index)
		if end < 0 {
			continue
		}
		verb := end
		for verb < len(words) && negationFillers(words[verb]) {
			verb++
		}
		switch {
		case !englishWorkVerbs(wordAt(words, verb)):
			if word != "hold" {
				continue
			}
			// "hold off on these": the hold is the verb, what follows it the object.
			verb = index + 1
		case (words[verb] == "start" || words[verb] == "begin") && wordAt(words, verb+1) == "to" && englishWorkVerbs(wordAt(words, verb+2)):
			verb += 2
		case (words[verb] == "start" || words[verb] == "starting" || words[verb] == "begin") && englishWorkVerbs(wordAt(words, verb+1)):
			verb++
		}
		last := objectEnd(words, verb+1)
		if whole(words[verb+1 : last]) {
			return heldQuote(sentence[index:last])
		}
	}
	return ""
}

func opensCondition(words []string, index int) bool {
	word, next, prev := words[index], wordAt(words, index+1), wordAt(words, index-1)
	switch {
	case word == "in" && next == "case":
		index++
	case !conditionWords(word), word == "once" && (next == "again" || next == "more"), word == "after" && next == "all":
		return false
	}
	return (prev == "" || clauseJoins(prev) || conditionLeads(prev)) && !leadIn(words, index)
}

var (
	sayVerbs        = lazyWordSet(`say says said tell tells told state stated note noted instruct instructed specify specified ask asked`)
	clarityWords    = lazyWordSet(`clear obvious explicit`)
	clarityLeads    = lazyWordSet(`it that this i`)
	pastNegations   = lazyWordSet(`wasn't weren't didn't haven't hasn't hadn't wasnt werent didnt havent hasnt hadnt`)
	pastAuxiliaries = lazyWordSet(`was were did have has had`)
	asideVerbs      = lazyWordSet(`missed wondering wondered forgot forgotten`)
	asideObjects    = lazyWordSet(`it this that why`)
	addressees      = lazyWordSet(`you you've you're`)
	meetingOwners   = lazyWordSet(`our the my your this that today's yesterday's last`)
	meetingNouns    = lazyWordSet(`call calls meeting meetings chat talk discussion conversation sync standup stand-up catch-up catchup`)
	talkingVerbs    = lazyWordSet(`talking speaking discussing meeting chatting`)
	talkedVerbs     = lazyWordSet(`talked spoke met discussed chatted agreed decided synced`)
	spareVerbs      = lazyWordSet(`have get got find`)
	spareNouns      = lazyWordSet(`time chance moment minute sec second`)
	breakSizes      = lazyWordSet(`short quick long brief little late early`)
	breakNouns      = lazyWordSet(`lunch breakfast dinner supper brunch coffee break breaks weekend holiday holidays vacation nap lunchtime`)
	readVerbs       = lazyWordSet(`read see get receive open check`)
	readObjects     = lazyWordSet(`message note email mail prompt list`)
)

const leadInMaxWords = 8

func leadIn(words []string, index int) bool {
	if words[index] == "after" && afterBreak(words[index+1:]) {
		return true
	}
	end := index + 1
	for end < len(words) && words[end] != "" && englishNegationEnd(words, end) < 0 {
		if end-index > leadInMaxWords {
			return false
		}
		end++
	}
	opener, clause := words[index], words[index+1:end]
	first := wordAt(clause, 0)
	for at, word := range clause {
		switch {
		case word == "otherwise" || word == "so":
			if (opener == "unless" || opener == "except") && (sayVerbs(wordAt(clause, at-1)) || sayVerbs(wordAt(clause, at-2))) {
				return true
			}
		case clarityWords(word):
			if clarityLeads(first) && pastNegation(clause[:at]) {
				return true
			}
		case asideVerbs(word):
			if addressees(first) && (at == len(clause)-1 || at == len(clause)-2 && asideObjects(clause[at+1])) {
				return true
			}
		}
	}
	switch opener {
	case "after":
		return talkingVerbs(first) || (first == "we" || first == "i") && talkedVerbs(wordAt(clause, 1))
	case "when", "whenever":
		return spareTime(clause) || readsThis(clause)
	}
	return false
}

func afterBreak(words []string) bool {
	at := 0
	if word := wordAt(words, at); meetingOwners(word) || word == "a" || word == "an" {
		at++
	}
	if breakSizes(wordAt(words, at)) {
		at++
	}
	noun := wordAt(words, at)
	return meetingNouns(noun) || breakNouns(noun)
}

func readsThis(clause []string) bool {
	if len(clause) < 3 || len(clause) > 4 || clause[0] != "you" || !readVerbs(clause[1]) {
		return false
	}
	if len(clause) == 3 {
		return clause[2] == "this"
	}
	return (clause[2] == "this" || clause[2] == "my") && readObjects(clause[3])
}

func pastNegation(words []string) bool {
	for at, word := range words {
		if pastNegations(word) || word == "not" && pastAuxiliaries(wordAt(words, at-1)) {
			return true
		}
	}
	return false
}

func spareTime(clause []string) bool {
	switch strings.Join(clause, " ") {
	case "possible", "you can", "you're free", "you are free":
		return true
	}
	if len(clause) < 3 || len(clause) > 4 || !addressees(clause[0]) || !spareVerbs(clause[1]) || !spareNouns(clause[len(clause)-1]) {
		return false
	}
	return len(clause) == 3 || clause[2] == "a" || clause[2] == "the" || clause[2] == "some"
}

var (
	listNouns = lazyWordSet(`these those them following above below item items task tasks step steps point points list job jobs
		bullet bullets ticket tickets todo todos`)
	codeNouns = lazyWordSet(`code codebase source`)
)

// listWorkObject tells an object that names the listed work itself, "any of
// these" or "the following items", from one that names a part of it, "the
// code here", which in a listed step is a rule for that step.
func listWorkObject(words []string) bool {
	if len(words) > 0 && workParticles(words[0]) {
		words = words[1:]
	}
	if wholeQuantifiers(wordAt(words, 0)) && wordAt(words, 1) == "of" {
		words = words[2:]
	}
	rest, ok, weak := referenceHead(words)
	return ok && !weak && listNouns(words[len(words)-len(rest)-1]) && wholeWorkTail(rest)
}

// codeObject tells an object that names the code: "the existing code", "the
// source code for now". In a prompt whose work is tests or docs it keeps the
// work away from the code, a rule, not a hold on the job.
func codeObject(words []string) bool {
	for _, word := range words {
		if codeNouns(word) {
			return true
		}
	}
	return false
}

var testDocWords = normalWordSet(`test tests testing docs documentation document readme changelog
	testi testini testleri testlerini testler belge belgeler belgeleri belgele dokümantasyon doküman`)

// namesTestsOrDocs tells a prompt whose work is tests or docs, where "don't
// touch the existing code" keeps the work to them rather than holding it back.
func namesTestsOrDocs(tokens []heldToken) bool {
	for _, token := range tokens {
		if testDocWords(token.word) {
			return true
		}
	}
	return false
}

// englishAsksOnly finds "just estimate", "only give me a plan", "without
// implementing anything" or "make no code changes" in a sentence that asks
// for no work anywhere else.
func englishAsksOnly(sentence []heldToken) string {
	words := sentenceWords(sentence)
	for index, word := range words {
		next := wordAt(words, index+1)
		hit := false
		switch {
		case onlyWords(word):
			hit = talkVerbs(next) || tellVerbs(next) && (wordAt(words, index+2) == "me" || wordAt(words, index+2) == "us") ||
				(next == "want" || next == "need") && talkNounWithin(words[index+2:], 3)
		case talkNouns(word):
			hit = next == "only"
		case word == "without":
			hit = englishWorkVerbs(next) && strings.HasSuffix(next, "ing") && wholeWorkObject(words[index+2:objectEnd(words, index+2)])
		case word == "no":
			rest := words[index+1:]
			if len(rest) > 0 && (rest[0] == "code" || rest[0] == "file") {
				rest = rest[1:]
			}
			prev := wordAt(words, index-1)
			hit = len(rest) > 0 && noChangeNouns(rest[0]) && (prev == "" || noChangeLeads(prev)) && wholeWorkTail(rest[1:objectEnd(rest, 1)])
		}
		if hit && !sentenceAsksForWork(sentence) {
			return heldQuote(sentence[index:objectEnd(words, index+1)])
		}
	}
	return ""
}

func talkNounWithin(words []string, limit int) bool {
	for index := 0; index < len(words) && index < limit; index++ {
		if talkNouns(words[index]) {
			return true
		}
	}
	return false
}

func leadVerb(words []string) (string, int) {
	for index := 0; index < len(words); index++ {
		word := words[index]
		if (word == "don't" || word == "do") && wordAt(words, index+1) == "forget" {
			index++
			continue
		}
		if word == "remember" || word == "forget" || word == "make" && wordAt(words, index+1) == "sure" {
			if word == "make" {
				index++
			}
			continue
		}
		if !requestLeads(word) {
			return word, index
		}
	}
	return "", -1
}

func sentenceAsksForWork(sentence []heldToken) bool {
	for _, clause := range heldClauses(sentence, clauseJoins) {
		words := heldWords(clause)
		if verb, at := leadVerb(words); englishWorkVerb(verb) && !negatedObject(wordAt(words, at+1)) {
			return true
		}
	}
	return false
}

func negatedObject(word string) bool {
	return word == "no" || word == "not" || word == "nothing" || word == "none"
}

// englishTalkLead classifies the words around a list: "estimate each of
// these", "explain what these involve" or a question about them ask for
// talk; "implement these", "can you do these?" ask for work.
func englishTalkLead(sentence []heldToken) (string, bool) {
	question := len(sentence) > 0 && strings.ContainsRune(questionMarks, sentence[len(sentence)-1].stop)
	talk, work := "", false
	for _, clause := range heldClauses(sentence, clauseJoins) {
		words := heldWords(clause)
		if len(words) == 0 {
			continue
		}
		verb, at := leadVerb(words)
		next := wordAt(words, at+1)
		asks := false
		switch {
		case question && politeAsk(words[0]) && wordAt(words, 1) == "you":
			if englishWorkVerb(verb) {
				work = true
			} else {
				asks = talkVerbs(verb)
			}
		case question && (questionAuxiliary(words[0]) || questionWords(words[0]) || politeAsk(words[0])):
			asks = true
		case englishWorkVerb(verb) && !negatedObject(next):
			work = true
		case talkVerbs(verb):
			asks = listReferenceWithin(words[at+1:], len(words)) && !adviceWhileWorking(words)
		case tellVerbs(verb) && (next == "me" || next == "us") || verb == "let" && (next == "me" || next == "us") && wordAt(words, at+2) == "know":
			// "tell me which of these …"; past a plan or an estimate the list
			// may come later: "give me a time estimate for each of the following".
			asks = (listReferenceWithin(words[at+1:], 5) || talkNounWithin(words[at+2:], 4) && listReferenceWithin(words[at+1:], len(words))) &&
				!adviceWhileWorking(words)
		case wantedTalk(words):
			asks = listReferenceWithin(words, len(words)) && !adviceWhileWorking(words)
		}
		if asks && talk == "" {
			talk = heldQuote(clause)
		}
	}
	return talk, work
}

var (
	politeAsk         = lazyWordSet(`can could would will`)
	questionWords     = lazyWordSet(`what how which why where who whose`)
	listReferences    = lazyWordSet(`these those them following list above below items tasks points jobs each every how which what all ones`)
	adviceTimingWords = lazyWordSet(`before after once while when whenever`)
	wantLeads         = lazyWordSet(`i we i'd we'd would really also just still`)
	stepSubjects      = lazyWordSet(`you we i it they you're we're i'm it's they're`)
)

// wantedTalk finds "I need a rough estimate" or "we'd like your plan" in any
// place of a clause: a plan, estimate or other talk the user wants.
func wantedTalk(words []string) bool {
	for index, word := range words {
		if (word == "need" || word == "want" || word == "like") && wantLeads(wordAt(words, index-1)) && talkNounWithin(words[index+1:], 4) {
			return true
		}
	}
	return false
}

func listReferenceWithin(words []string, limit int) bool {
	for index := 0; index < len(words) && index < limit; index++ {
		if listReferences(words[index]) {
			return true
		}
	}
	return false
}

// adviceWhileWorking tells "review each change before committing", advice on
// how to do the work, from "review the following", a request for talk. A
// "before" that names a time ("before the meeting") or holds all of the work
// off ("before doing anything") gives no such advice.
func adviceWhileWorking(words []string) bool {
	for index, word := range words {
		if adviceTimingWords(word) && (word != "before" || workStepAfter(words[index+1:])) {
			return true
		}
	}
	return false
}

// workStepAfter tells whether the words after "before" name a step of the
// work, "committing" or "you push", rather than a time or the whole work.
func workStepAfter(words []string) bool {
	at := 0
	if stepSubjects(wordAt(words, 0)) {
		at = 1
	}
	verb := wordAt(words, at)
	switch {
	case at == 0 && !strings.HasSuffix(verb, "ing"), verb == "morning", verb == "evening":
		return false
	case englishWorkVerbs(verb):
		return !wholeWorkObject(words[at+1 : objectEnd(words, at+1)])
	}
	return true
}

var (
	turkishNegatedWork = normalWordSet(`yapma yapmayın yapmayınız dokunma dokunmayın dokunmayınız başlama başlamayın başlamayınız
		uygulama uygulamayın uygulamayınız değiştirme değiştirmeyin değiştirmeyiniz elleme ellemeyin ellemeyiniz kurcalama kurcalamayın
		düzenleme düzenlemeyin kodlama kodlamayın uğraşma uğraşmayın girişme girişmeyin yazma yazmayın`)
	turkishWithoutWork = normalWordSet(`uygulamadan kodlamadan yapmadan dokunmadan değiştirmeden ellemeden düzenlemeden`)
	turkishWholeWork   = normalWordSet(`hiçbirini hiçbirine hiçbiri hiçbir hiçbirinde hiçbirşey herhangi bunları bunlara bunlardan bunu buna
		onları onlara şunları şunlara maddeleri maddelere maddelerin maddelerden işleri işlere görevleri görevlere adımları
		dosyalara dosyaları dosyaya dosyayı dosyalarda koda kodu kod kodlara kodları kodda kodlarda projeye projeyi projede depoya
		depoda repoya repoda uygulamaya kodlamaya işe`)
	turkishPlaceNouns = normalWordSet(`dosyalara dosyaları dosyaya dosyayı dosyalarda koda kodu kod kodlara kodları kodda kodlarda projeye
		projeyi projede depoya depoda repoya repoda`)
	turkishNow         = normalWordSet(`şimdi şimdilik henüz`)
	turkishChangeNouns = normalWordSet(`değişiklik değişiklikler değişikliği değişiklikleri`)
	turkishExcept      = normalWordSet(`dışında dışındaki haricinde hariç başka diğer`)
	turkishLeadEnds    = normalWordSet(`için diye deyişle göre kadar rağmen dolayı yüzünden nedeniyle sebebiyle`)
	turkishBefore      = normalWordSet(`önce evvel`)
	turkishOnly        = normalWordSet(`sadece yalnızca yalnız`)
	turkishJoins       = normalWordSet(`ve ama fakat ancak sonra ardından veya`)
	turkishTalkVerbs   = normalWordSet(`planla planlayın incele inceleyin açıkla açıklayın değerlendir değerlendirin öner önerin özetle özetleyin
		karşılaştır karşılaştırın söyle söyleyin anlat anlatın önceliklendir önceliklendirin puanla puanlayın yorumla yorumlayın
		listele listeleyin belirt belirtin`)
	turkishTalkNouns = normalWordSet(`tahmin tahmini tahminini tahminleri analiz analizi plan planı planını değerlendirme değerlendirmesi
		öneri önerini önerilerini rapor raporu özet özeti tablo liste listesi yorum yorumunu süre süreyi`)
	turkishGenericEnds = normalWordSet(`et edin ver verin çıkar çıkarın hazırla hazırlayın yaz yazın göster gösterin istiyorum lazım gerekiyor yeter yeterli`)
	turkishMakeEnds    = normalWordSet(`yap yapın`)
	turkishWorkEnds    = normalWordSet(verbFinalImperativeWords + ` yap yapın uygula uygulayın başla başlayın hallet halledin gerçekleştir
		gerçekleştirin tamamla tamamlayın bitir bitirin`)
	turkishAskWords = normalWordSet(`misin misiniz musun musunuz müsün müsünüz`)
)

// turkishUnwantedWork and turkishNotWanted read "bunları yapmanı istemiyorum",
// "I don't want you to do these".
var (
	turkishUnwantedWork = normalWordSet(`yapmanı yapmanızı uygulamanı uygulamanızı başlamanı başlamanızı dokunmanı dokunmanızı
		değiştirmeni değiştirmenizi düzenlemeni düzenlemenizi kodlamanı kodlamanızı`)
	turkishNotWanted = normalWordSet(`istemiyorum istemiyoruz`)
)

var (
	turkishCode       = normalWordSet(`kod koda kodu kodlara kodları kodda kodlarda`)
	turkishNowFillers = normalWordSet(`lütfen sakın daha hiç sen siz de da bir şey`)
	turkishListWork   = normalWordSet(`hiçbirini hiçbirine hiçbiri hiçbirinde bunları bunlara bunlardan onları onlara şunları şunlara
		maddeleri maddelere maddelerin maddelerden işleri işlere görevleri görevlere adımları`)
)

func turkishWholeWorkIn(words []string, strong, codeIsPart bool) bool {
	if turkishExceptIn(words) {
		return false
	}
	broad, named := false, false
	for index, word := range words {
		switch {
		case codeIsPart && turkishCode(word):
			named = true
		case turkishWholeWork(word) && !(turkishPlaceNouns(word) && index > 0 && strings.HasSuffix(words[index-1], "ki")):
			return true
		case turkishNow(word) || turkishChangeNouns(word):
			broad = true
		case !turkishNowFillers(word):
			named = true
		}
	}
	return !strong && broad && !named
}

// turkishListWorkIn tells words that name the listed work itself, "bunların
// hiçbirini", not a part of it such as "kodu" or "dosyalara".
func turkishListWorkIn(words []string) bool {
	if turkishExceptIn(words) {
		return false
	}
	for _, word := range words {
		if turkishListWork(word) {
			return true
		}
	}
	return false
}

func turkishExceptIn(words []string) bool {
	for index := len(words) - 1; index >= 0 && !turkishLeadEnds(words[index]); index-- {
		if turkishExcept(words[index]) {
			return true
		}
	}
	return false
}

func turkishForbids(clause []heldToken, listOnly, codeIsPart bool) string {
	words := heldWords(clause)
	if len(words) == 0 {
		return ""
	}
	whole := func(object []string, strong bool) bool {
		if listOnly {
			return turkishListWorkIn(object)
		}
		return turkishWholeWorkIn(object, strong, codeIsPart)
	}
	last := words[len(words)-1]
	if turkishNegatedWork(last) && whole(words[:len(words)-1], false) {
		return heldQuote(clause)
	}
	if turkishNotWanted(last) && len(words) > 1 && turkishUnwantedWork(words[len(words)-2]) && whole(words[:len(words)-2], false) {
		return heldQuote(clause)
	}
	if len(words) > 1 && turkishNegatedCompound(words[len(words)-2], last) && whole(words[:len(words)-2], false) {
		return heldQuote(clause)
	}
	if !listOnly && (last == "geçme" || last == "geçmeyin") && len(words) > 1 && (words[len(words)-2] == "uygulamaya" || words[len(words)-2] == "koda" || words[len(words)-2] == "kodlamaya") {
		return heldQuote(clause)
	}
	for index, word := range words {
		if turkishWithoutWork(word) && !turkishBefore(wordAt(words, index+1)) && whole(words, true) {
			return heldQuote(clause)
		}
	}
	return ""
}

var (
	turkishCompoundNouns = normalWordSet(`müdahale implemente`)
	turkishNegatedDo     = normalWordSet(`etme etmeyin etmeyiniz`)
	turkishHandNegated   = normalWordSet(`sürme sürmeyin sürmeyiniz`)
)

func turkishNegatedCompound(noun, verb string) bool {
	return turkishCompoundNouns(noun) && turkishNegatedDo(verb) || noun == "el" && turkishHandNegated(verb)
}

func turkishTalkEnd(words []string) bool {
	if len(words) == 0 {
		return false
	}
	last := words[len(words)-1]
	if turkishTalkVerbs(last) {
		return true
	}
	if turkishMakeEnds(last) {
		return len(words) > 1 && turkishTalkNouns(words[len(words)-2])
	}
	if !turkishGenericEnds(last) {
		return false
	}
	for _, word := range words[:len(words)-1] {
		if turkishTalkNouns(word) {
			return true
		}
	}
	return false
}

func turkishWorkEnd(words []string) bool {
	return len(words) > 0 && !turkishTalkEnd(words) && turkishWorkEnds(words[len(words)-1])
}

// turkishAsksOnly finds "sadece planla", "yalnızca her biri için süre
// tahmini ver" in a sentence that asks for no work anywhere else.
func turkishAsksOnly(sentence []heldToken) string {
	quote, work := "", false
	for _, clause := range heldClauses(sentence, turkishJoins) {
		words := heldWords(clause)
		if turkishWorkEnd(words) {
			work = true
		}
		if quote != "" || !turkishTalkEnd(words) {
			continue
		}
		for _, word := range words {
			if turkishOnly(word) {
				quote = heldQuote(clause)
				break
			}
		}
	}
	if work {
		return ""
	}
	return quote
}

func turkishTalkLead(sentence []heldToken) (string, bool) {
	question := len(sentence) > 0 && strings.ContainsRune(questionMarks, sentence[len(sentence)-1].stop)
	talk, work := "", false
	for _, clause := range heldClauses(sentence, turkishJoins) {
		words := heldWords(clause)
		switch {
		case len(words) == 0:
		case question && turkishAskWords(words[len(words)-1]):
			asksTalk, asksWork := turkishAskedVerb(words[:len(words)-1])
			if asksTalk && talk == "" && turkishAboutTheList(words) {
				talk = heldQuote(clause)
			}
			work = work || asksWork && !asksTalk
		case turkishTalkEnd(words):
			if talk == "" && turkishAboutTheList(words) {
				talk = heldQuote(clause)
			}
		case turkishWorkEnd(words):
			work = true
		}
	}
	return talk, work
}

// turkishAskedVerb reads the verb a question asks for, before "misin":
// "yapabilir" and "halleder" ask for work, "tahmin edebilir" for talk.
func turkishAskedVerb(words []string) (talk, work bool) {
	if len(words) == 0 {
		return false, false
	}
	head := words[:len(words)-1]
	for _, stem := range turkishVerbStems(words[len(words)-1]) {
		talk = talk || turkishTalkEnd(append(append([]string{}, head...), stem))
		work = work || turkishWorkEnds(stem)
	}
	return talk, work
}

// turkishVerbStems undoes the ability and aorist endings of a verb in a
// question: "yapabilir" is "yap", "edebilir" "et", "halleder" "hallet",
// "uygular" "uygula".
func turkishVerbStems(word string) []string {
	stems := []string{word}
	add := func(stem string) {
		if utf8.RuneCountInString(stem) < 2 {
			return
		}
		stems = append(stems, stem)
		if trimmed, found := strings.CutSuffix(stem, "y"); found && utf8.RuneCountInString(trimmed) >= 2 {
			stems = append(stems, trimmed)
		}
		if trimmed, found := strings.CutSuffix(stem, "d"); found {
			stems = append(stems, trimmed+"t")
		}
	}
	for _, ending := range []string{"abilir", "ebilir", "ar", "er", "ir", "ür", "ur", "r"} {
		if stem, found := strings.CutSuffix(word, ending); found {
			add(stem)
		}
	}
	return stems
}

var (
	askOpeners      = lazyWordSet(`so ok okay and now then also hey well`)
	askFillers      = lazyWordSet(`please kindly just also maybe now`)
	askWorkVerbs    = lazyWordSet(`get take knock sort finish complete wrap`)
	askInspectVerbs = lazyWordSet(`check verify investigate audit measure profile benchmark debug trace monitor test review compare`)
)

// politeWorkAsk tells a closing "Can you do these?" or "Bunları yapabilir
// misin?", which asks for the listed work, from a question about it such as
// "Can you estimate these?" or "Bunları tahmin edebilir misin?".
func politeWorkAsk(sentence string) bool {
	words := heldWords(heldTokens(sentence))
	for len(words) > 1 && (words[len(words)-1] == "lütfen" || words[len(words)-1] == "please") {
		words = words[:len(words)-1]
	}
	if len(words) > 1 && turkishAskWords(words[len(words)-1]) {
		talk, work := turkishAskedVerb(words[:len(words)-1])
		return work && !talk
	}
	at := 0
	for at < len(words) && askOpeners(words[at]) {
		at++
	}
	if !politeAsk(wordAt(words, at)) || wordAt(words, at+1) != "you" {
		return false
	}
	for at += 2; at < len(words) && askFillers(words[at]); at++ {
	}
	if wordAt(words, at) == "go" && wordAt(words, at+1) == "ahead" {
		at += 2
		if wordAt(words, at) == "and" {
			at++
		}
	}
	verb, next := wordAt(words, at), wordAt(words, at+1)
	switch {
	case verb == "" || talkVerbs(verb) || askInspectVerbs(verb) || tellVerbs(verb) && (next == "me" || next == "us"):
		return false
	case verb == "mind":
		return englishWorkVerb(next)
	}
	return englishWorkVerb(verb) || askWorkVerbs(verb) || imperativeWords(verb)
}

var (
	turkishListReferences = normalWordSet(`aşağıdaki aşağıdakileri aşağıdakilerin yukarıdaki bunlar bunları bunların bunlara şunları şunların maddeler
		maddeleri maddelerin her hepsi hepsini hepsinin hangisi hangileri hangisinin ne kaç listeyi listedeki işleri işlerin görevleri görevlerin`)
	turkishWorkTiming = normalWordSet(`önce sonra iken ederken yaparken`)
)

// turkishAboutTheList tells "aşağıdakilerin her biri için süre tahmin et",
// talk about the list, from "her değişikliği commit etmeden önce incele",
// advice on how to do the work.
func turkishAboutTheList(words []string) bool {
	about := false
	for _, word := range words {
		if turkishWorkTiming(word) {
			return false
		}
		about = about || turkishListReferences(word)
	}
	return about
}

func heldBackWork(text, prose string, lead bool) string {
	tokens := heldTokens(text)
	codeIsPart := namesTestsOrDocs(tokens)
	proseTokens := heldTokens(prose)
	sentences := heldSentences(proseTokens)
	for _, sentence := range sentences {
		if quote := englishForbids(sentence, false, codeIsPart); quote != "" {
			return forbidsReason(quote)
		}
		for _, clause := range heldClauses(sentence, turkishJoins) {
			if quote := turkishForbids(clause, false, codeIsPart); quote != "" {
				return forbidsReason(quote)
			}
		}
	}
	phrases := newHeldText(prose, proseTokens)
	if phrase := phrases.find(&heldPatterns().forbids, true); phrase != "" {
		return forbidsReason(phrase)
	}
	for _, sentence := range heldSentences(tokens) {
		if quote := englishForbids(sentence, true, false); quote != "" {
			return forbidsReason(quote)
		}
		for _, clause := range heldClauses(sentence, turkishJoins) {
			if quote := turkishForbids(clause, true, false); quote != "" {
				return forbidsReason(quote)
			}
		}
	}
	for _, sentence := range sentences {
		if quote := englishAsksOnly(sentence); quote != "" {
			return asksReason(quote)
		}
		if quote := turkishAsksOnly(sentence); quote != "" {
			return asksReason(quote)
		}
	}
	if phrase := phrases.find(&heldPatterns().asks, false); phrase != "" {
		return asksReason(phrase)
	}
	if !lead {
		return ""
	}
	talk, work := "", false
	for _, sentence := range sentences {
		for _, classify := range []func([]heldToken) (string, bool){englishTalkLead, turkishTalkLead} {
			said, asked := classify(sentence)
			if talk == "" {
				talk = said
			}
			work = work || asked
		}
	}
	if talk != "" && !work {
		return asksReason(talk)
	}
	return ""
}

func forbidsReason(quote string) string {
	return `the prompt forbids the listed work: "` + quote + `"`
}

func asksReason(quote string) string {
	return `the prompt asks only for a plan, estimate, review or explanation: "` + quote + `"`
}
