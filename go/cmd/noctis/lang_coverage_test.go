package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var callSitePattern = regexp.MustCompile(`\bT\("([a-zA-Z0-9_.]+)"`)

func keysUsedInSource(t *testing.T) map[string][]string {
	t.Helper()
	entries, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	used := map[string][]string{}
	for _, file := range entries {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range callSitePattern.FindAllStringSubmatch(string(source), -1) {
			used[match[1]] = append(used[match[1]], file)
		}
	}
	if len(used) < 100 {
		t.Fatalf("only %d message keys were found in the source; the scan is broken, not the catalog", len(used))
	}
	return used
}

func TestEnglishCatalogCoversEveryKeyTheCodeUses(t *testing.T) {
	english := catalogTable()["en"]
	if len(english) == 0 {
		t.Fatal("the English catalog is empty")
	}
	var missing []string
	for key, files := range keysUsedInSource(t) {

		if strings.HasSuffix(key, ".") {
			continue
		}
		if _, ok := english[key]; !ok {
			missing = append(missing, key+" (used in "+strings.Join(files, ", ")+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("%d key(s) are asked for but not in the English catalog, so they print as their own name:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

func TestEnglishCatalogHasNoUnusedKeys(t *testing.T) {
	used := keysUsedInSource(t)

	dynamicPrefixes := []string{"help.cmd.", "hit.", "win.", "doctor.", "notice.", "queue.", "role.", "roles.", "host.next."}
	var unused []string
	for key := range catalogTable()["en"] {
		if _, ok := used[key]; ok {
			continue
		}
		dynamic := false
		for _, prefix := range dynamicPrefixes {
			if strings.HasPrefix(key, prefix) {
				dynamic = true
				break
			}
		}
		if !dynamic {
			unused = append(unused, key)
		}
	}
	sort.Strings(unused)
	if len(unused) > 0 {
		t.Fatalf("%d catalog key(s) are never asked for:\n  %s", len(unused), strings.Join(unused, "\n  "))
	}
}

func TestTranslationsKeepTheirPlaceholders(t *testing.T) {
	english := catalogTable()["en"]
	for lang, table := range catalogTable() {
		if lang == "en" {
			continue
		}
		for key, text := range table {
			base, ok := english[key]
			if !ok {
				t.Errorf("%s: key %q is not in the English catalog", lang, key)
				continue
			}
			want := argumentTypes(base)
			for position, verb := range argumentTypes(text) {
				expected, exists := want[position]
				if !exists {
					t.Errorf("%s/%s: uses argument %d, which the call site never passes (English reads %d)\n  en: %s\n  %s: %s",
						lang, key, position, len(want), base, lang, text)
					continue
				}
				if expected != verb {
					t.Errorf("%s/%s: reads argument %d as %%%s, English passes %%%s\n  en: %s\n  %s: %s",
						lang, key, position, verb, expected, base, lang, text)
				}
			}
		}
	}
}

var verbPattern = regexp.MustCompile(`%(\[(\d+)\])?[+\-# 0]*\d*(?:\.\d+)?([a-zA-Z%])`)

func argumentTypes(format string) map[int]string {
	types := map[int]string{}
	next := 1
	for _, match := range verbPattern.FindAllStringSubmatch(format, -1) {
		verb := match[3]
		if verb == "%" {
			continue
		}
		position := next
		if match[2] != "" {
			explicit, err := strconv.Atoi(match[2])
			if err != nil {
				continue
			}
			position = explicit
		}
		types[position] = verb
		next = position + 1
	}
	return types
}

func TestPerLocaleListsAreComplete(t *testing.T) {
	for lang, names := range dayNamesByLocale {
		if len(names) != 7 {
			t.Errorf("dayNamesByLocale[%q] has %d entries, want 7", lang, len(names))
		}
	}
	for lang, units := range durationUnits {
		if len(units) != 3 {
			t.Errorf("durationUnits[%q] has %d entries, want 3", lang, len(units))
		}
	}

	for lang := range catalogTable() {
		if _, ok := dayNamesByLocale[lang]; !ok {
			t.Errorf("catalog language %q has no day names", lang)
		}
		if _, ok := durationUnits[lang]; !ok {
			t.Errorf("catalog language %q has no duration units", lang)
		}
	}
}

func TestEveryLanguageIsComplete(t *testing.T) {
	english := catalogTable()["en"]
	var languages []string
	for lang := range catalogTable() {
		if lang != "en" {
			languages = append(languages, lang)
		}
	}
	sort.Strings(languages)
	for _, lang := range languages {
		table := catalogTable()[lang]
		var missing []string
		for key := range english {
			if _, ok := table[key]; !ok {
				missing = append(missing, key)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			shown := missing
			if len(shown) > 8 {
				shown = shown[:8]
			}
			t.Errorf("%s is missing %d of %d keys (those fall back to English mid-sentence): %s...",
				lang, len(missing), len(english), strings.Join(shown, ", "))
		}
	}
}

func TestTheWorkspaceNoteStatesFactsInsteadOfOrders(t *testing.T) {
	orders := map[string]bool{"run": true, "re-read": true, "reread": true, "read": true, "check": true, "stop": true, "continue": true, "do": true, "don't": true, "make": true, "use": true, "please": true, "always": true, "never": true}
	clauses := regexp.MustCompile(`[.:;,()!]`)
	for lang, table := range catalogTable() {
		note := strings.TrimSpace(strings.TrimPrefix(table["workspace.context"], "[noctis]"))
		if !strings.Contains(note, "git status differs from the checkpoint") {
			t.Errorf("%s: the workspace note must say what changed, got %q", lang, note)
		}
		for _, clause := range clauses.Split(note, -1) {
			if words := strings.Fields(strings.ToLower(clause)); len(words) > 0 && orders[words[0]] {
				t.Errorf("%s: the workspace note reaches Claude next to tool results, where text framed as a command can trip its prompt-injection defenses; %q opens a clause with %q", lang, note, words[0])
			}
		}
	}
}

func catalogTable() map[string]map[string]string {
	table := map[string]map[string]string{}
	for lang, entries := range baseTable() {
		table[lang] = entries
	}
	for lang := range extraCatalogBuilders {
		table[lang] = catalogFor(lang)
	}
	return table
}
