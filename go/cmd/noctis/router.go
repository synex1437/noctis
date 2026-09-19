package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type wordMatcher struct {
	pattern *lazyRe
}

func newWordMatcher(turkishStems, englishWords []string) wordMatcher {
	alternatives := make([]string, 0, len(turkishStems)+len(englishWords))
	for _, stem := range turkishStems {
		alternatives = append(alternatives, stem+`\p{L}*`)
	}
	alternatives = append(alternatives, englishWords...)
	pattern := lazyRegexp(`(?i)(?:^|[^\p{L}\p{N}_])(` + strings.Join(alternatives, "|") + `)(?:[^\p{L}\p{N}_]|$)`)
	return wordMatcher{pattern: pattern}
}

func (matcher wordMatcher) find(text string) (string, bool) {
	match := matcher.pattern.FindStringSubmatch(text)
	if match == nil {
		return "", false
	}
	return strings.ToLower(match[1]), true
}

var (
	urlPattern        = lazyRegexp(`(?i)\bhttps?://\S+|\bwww\.\S+`)
	codePathPattern   = lazyRegexp(`(?i)(^|[\s"'(])(\.{1,2}[\\/]|[A-Za-z]:\\|~[\\/]|src[\\/]|lib[\\/]|/(usr|etc|home|opt|var)/)|\.(js|mjs|cjs|ts|tsx|jsx|py|java|kt|go|rs|c|cc|cpp|h|hpp|cs|php|rb|swift|scala|sql|sh|bash|zsh|ps1|bat|cmd|json|ya?ml|toml|xml|html?|css|scss|md|txt|env|lock|ini|cfg|csv|ipynb|dockerfile|exe|dll)\b`)
	codeSymbolPattern = lazyRegexp("`|[{};]|=>|==|!=|->|</|#include|\\bdef |\\bconst |\\blet |\\bvar |\\bimport |\\bpublic |\\bprivate |\\bstatic ")
	codeWords         = newWordMatcher(
		[]string{"kod", "program", "yazılım", "hata", "çök", "fonksiyon", "sınıf", "değişken", "sorgu", "derle", "çalıştır", "test", "dosya", "klasör", "dizin", "proje", "uygula", "entegre", "özellik", "düzelt", "optimiz", "performans", "veritabanı", "bileşen", "sunucu", "komut", "eklenti", "refaktör", "metod", "betik", "derleme", "kurulum"},
		[]string{"code", "coding", "codebase", "scripts?", "software", "bugs?", "errors?", "exceptions?", "crash(es|ed)?", "debug(ging)?", "refactor(ing)?", "functions?", "methods?", "class(es)?", "variables?", "api", "apis", "endpoints?", "sql", "quer(y|ies)", "regex", "json", "yaml", "html", "css", "typescript", "javascript", "python", "java", "rust", "golang", "node", "npm", "pnpm", "yarn", "pip", "cargo", "git", "commits?", "branch(es)?", "merge", "rebase", "pull requests?", "deploy(ment)?", "build", "compile", "run", "tests?", "unit", "lint", "files?", "folders?", "director(y|ies)", "repo", "repository", "projects?", "implement(ation)?", "integrate", "features?", "fix(es)?", "patch", "optimi[sz]e", "performance", "migrat(e|ion)", "database", "schema", "components?", "frontend", "backend", "servers?", "docker", "kubernetes", "pipeline", "config", "terminal", "commands?", "plugins?", "hooks?", "agents?", "mcp", "readme", "stack ?trace", "install", "logs?", `loglar\p{L}*`, "kur", `kurma\p{L}*`},
	)
	webWords = newWordMatcher(
		[]string{"güncel", "haber", "kaynak", "literatür", "karşılaştır", "inceleme", "makale", "fiyat", "trend", "piyasa", "avantaj", "dezavantaj", "en iyi", "en yeni", "hangisi daha iyi", "web'?de ara", "internette ara", "google"},
		[]string{"latest", "newest", "recent", "news", "sources?", "literature", "compar(e|ison)", "which is better", "reviews?", "articles?", "papers?", "prices?", "pricing", "trends?", "market", "benchmarks?", "pros and cons", "best"},
	)
	investigateWords    = newWordMatcher([]string{"araştır", "incele"}, []string{"research", "investigate", "look (up|into)"})
	summaryWords        = newWordMatcher([]string{"özet"}, []string{"summari[sz]e", "summary", "tl;?dr"})
	continuationPattern = lazyRegexp(`(?i)^\s*(devam|continue|evet|yes|ok(ay)?|tamam|hayır|no|peki|hmm|dur|stop|bekle|wait)\b`)
	forcedLite          = lazyRegexp(`(?i)^lite:`)
	forcedMain          = lazyRegexp(`(?i)^(fable|main):`)
)

type verdict struct {
	route  bool
	reason string
	signal string
}

func (result verdict) toJSON() object {
	out := object{"route": result.route, "reason": result.reason}
	if result.signal != "" {
		out["signal"] = result.signal
	}
	return out
}

func stripURLs(text string) string {
	return urlPattern.ReplaceAllString(text, " ")
}

type transcriptEntry struct {
	entryType string
	timestamp string
	isMeta    bool
	isCompact bool
	sidechain bool
	content   []any
}

func parseTranscriptLine(line string) (transcriptEntry, bool) {
	raw := object{}
	if err := jsonUnmarshalObject([]byte(line), &raw); err != nil || raw == nil {
		return transcriptEntry{}, false
	}
	message := getMap(raw, "message")
	if message == nil {
		return transcriptEntry{}, false
	}
	entry := transcriptEntry{
		entryType: getString(raw, "type"),
		timestamp: getString(raw, "timestamp"),
		isMeta:    getBool(raw, "isMeta", false),
		isCompact: getBool(raw, "isCompactSummary", false),
		sidechain: getBool(raw, "isSidechain", false),
	}
	switch content := message["content"].(type) {
	case []any:
		entry.content = content
	case string:
		entry.content = []any{object{"type": "text", "text": content}}
	}
	return entry, true
}

func recentCodingActivity(transcriptPath string, now int64) bool {
	lines, ok := tailLines(transcriptPath, codingTailBytes)
	if !ok {
		return false
	}
	for i := len(lines) - 1; i >= 0; i-- {
		entry, parsed := parseTranscriptLine(lines[i])
		if !parsed || entry.entryType != "assistant" {
			continue
		}
		if at, err := time.Parse(time.RFC3339Nano, entry.timestamp); err == nil && float64(now)-float64(at.Unix()) > codingActivityWindow {
			return false
		}
		for _, block := range entry.content {
			blockMap, _ := block.(object)
			if getString(blockMap, "type") != "tool_use" {
				continue
			}
			name := getString(blockMap, "name")
			if fileTools[name] || name == "Bash" {
				return true
			}
		}
	}
	return false
}

func classifyPrompt(cfg object, learned object, prompt, transcriptPath string, now int64) verdict {
	text := strings.TrimSpace(prompt)
	router := section(cfg, "router")
	if !getBool(router, "enabled", true) {
		return verdict{reason: "router-off"}
	}
	if forcedLite.MatchString(text) {
		return verdict{route: true, reason: "forced"}
	}
	if forcedMain.MatchString(text) {
		return verdict{reason: "forced-main"}
	}
	minLength := int(numberOr(router, "minPromptLength", 15))
	if strings.HasPrefix(text, "/") || strings.HasPrefix(text, "!") || len([]rune(text)) < minLength {
		return verdict{reason: "short-or-command"}
	}
	if continuationPattern.MatchString(text) {
		return verdict{reason: "continuation"}
	}
	withoutURLs := stripURLs(text)
	if codeSymbolPattern.MatchString(withoutURLs) || codePathPattern.MatchString(withoutURLs) {
		return verdict{reason: "code-signal"}
	}
	if _, found := codeWords.find(withoutURLs); found {
		return verdict{reason: "code-signal"}
	}
	blocked := func(signal string) bool {
		record := getMap(learned, signal)
		if record == nil {
			return false
		}
		misroutes := numberOr(record, "misroutes", 0)
		return misroutes >= learnedBlockMisroutes && misroutes > numberOr(record, "ok", 0)
	}
	decide := func(reason, signal string) verdict {
		if blocked(signal) {
			return verdict{reason: "learned-block", signal: signal}
		}
		return verdict{route: true, reason: reason, signal: signal}
	}
	if urlPattern.MatchString(text) {
		return decide("url", "url")
	}
	if signal, found := webWords.find(withoutURLs); found {
		return decide("web-words", signal)
	}
	if _, found := summaryWords.find(withoutURLs); found {
		if len([]rune(withoutURLs)) >= longTextSummaryChars {
			return decide("long-text-summary", "summary")
		}
		return verdict{reason: "summary-needs-context"}
	}
	if signal, found := investigateWords.find(withoutURLs); found {
		if transcriptPath != "" && recentCodingActivity(transcriptPath, now) {
			return verdict{reason: "coding-session"}
		}
		return decide("investigate", signal)
	}
	return verdict{reason: "no-research-signal"}
}

func routeDirective(cfg object) string {
	agent := getString(section(cfg, "router"), "agent")
	if agent == "" {
		agent = "lite"
	}
	return fmt.Sprintf(`[noctis] Non-code research: delegate it in ONE Agent call to "%s:%s" (request verbatim + needed context), relay its answer; no WebSearch/WebFetch here. If code or file changes are needed, ignore this.`, pluginName, agent)
}

func runClassify() {
	prompt := strings.Join(args.positional[1:], " ")
	learned := getMap(readState(), "routerLearned")
	result := classifyPrompt(loadConfig(), learned, prompt, flagString("transcript"), nowSec())
	fmt.Fprintln(os.Stdout, string(marshalCompact(result.toJSON())))
}

func tailLines(file string, maxBytes int64) ([]string, bool) {
	info := statSafe(file)
	if info == nil {
		return nil, false
	}
	content, cut, err := readTailBytes(file, info.Size(), maxBytes)
	if err != nil {
		warn("transcript tail unreadable: %v", err)
		return nil, false
	}
	return strings.Split(string(dropPartialFirstLine(content, cut)), "\n"), true
}
