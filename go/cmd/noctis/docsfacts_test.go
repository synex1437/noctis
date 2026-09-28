package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// The docs quote what noctis prints and count what it installs; these tests hold them to the code.

func docsFactsFile(t *testing.T, relative string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(), filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// docsFactsStatusLine renders the usage windows of the status line the way the docs show them: the
// 5-hour window at 41 % resetting at 14:35 today, the weekly one at 23 % and on pace, resetting on
// Monday 21.09 at 09:00.
func docsFactsStatusLine(t *testing.T, code string) string {
	t.Helper()
	previous := locale
	t.Cleanup(func() { locale = previous })
	locale = code
	now := time.Now()
	fiveReset := time.Date(now.Year(), now.Month(), now.Day(), 14, 35, 0, 0, time.Local)
	weekReset := time.Date(2026, time.September, 21, 9, 0, 0, 0, time.Local)
	view := usageView{
		fiveHour: &window{used: 41, resetsAt: float64(fiveReset.Unix())},
		sevenDay: &window{used: 23, resetsAt: float64(weekReset.Unix())},
	}
	return usageBadgeAt(view, object{}, weekReset.Unix()-3*86400)
}

// A percentage written before its number ("%41") is how Turkish prints it; English prints "41%".
var docsFactsPercentFirst = regexp.MustCompile("(^|[\\s(·`])%\\d")

func TestTheDocsShowTheStatusLineTheWayItPrintsInTheirLanguage(t *testing.T) {
	english := docsFactsStatusLine(t, "en")
	if english != "5h 41%→14:35 · Wk 23%▲→Mon 21.09 09:00" {
		t.Fatalf("the English status line reads %q; update this test and the docs that quote it", english)
	}
	for _, doc := range []string{"README.md", "docs/GUIDE.md"} {
		if !strings.Contains(docsFactsFile(t, doc), english) {
			t.Errorf("%s does not show the status line as English prints it: %q", doc, english)
		}
	}
	turkish := docsFactsStatusLine(t, "tr")
	for _, doc := range []string{"README.tr.md", "docs/GUIDE.tr.md"} {
		if !strings.Contains(docsFactsFile(t, doc), turkish) {
			t.Errorf("%s does not show the status line as Turkish prints it: %q", doc, turkish)
		}
	}
	for _, doc := range []string{"README.md", "docs/GUIDE.md", "docs/REFERENCE.md", "docs/HOSTS.md", "docs/TESTING.md"} {
		for number, line := range strings.Split(docsFactsFile(t, doc), "\n") {
			if docsFactsPercentFirst.MatchString(line) {
				t.Errorf("%s:%d writes a percentage the Turkish way, which the English status line never prints: %s", doc, number+1, line)
			}
		}
	}
	images, _ := filepath.Glob(filepath.Join(repoRoot(), "docs", "*.svg"))
	text := regexp.MustCompile(`>([^<]+)<`)
	for _, image := range images {
		if filepath.Base(image) == "languages.svg" {
			continue // shows the notices in several languages, Turkish among them
		}
		raw, err := os.ReadFile(image)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range text.FindAllStringSubmatch(string(raw), -1) {
			if docsFactsPercentFirst.MatchString(match[1]) {
				t.Errorf("docs/%s writes a percentage the Turkish way: %s", filepath.Base(image), match[1])
			}
		}
	}
}

func TestTheGuidesCountTheFilesACloneInstallCopies(t *testing.T) {
	copied := t.TempDir()
	if err := copyPluginTree(repoRoot(), copied); err != nil {
		t.Fatal(err)
	}
	count := 0
	filepath.WalkDir(copied, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			count++
		}
		return nil
	})
	// The copy holds the same number of files on every system. placeBinary then puts the binary at
	// bin/noctis.exe on Windows, a file the copy does not hold; elsewhere it replaces the bin/noctis
	// launcher, which is counted already.
	windows := count + 1
	for doc, want := range map[string]string{
		"docs/GUIDE.md":    fmt.Sprintf("That copy is %d files (%d on Windows)", count, windows),
		"docs/GUIDE.tr.md": fmt.Sprintf("Bu kopya %d dosyadır (Windows'ta %d)", count, windows),
	} {
		if !strings.Contains(docsFactsFile(t, doc), want) {
			t.Errorf("%s does not say %q", doc, want)
		}
	}
}

// docsFactsCommands returns the backquoted names in the part of text that starts at from and ends at
// the next to.
func docsFactsCommands(t *testing.T, text, from, to string) map[string]bool {
	t.Helper()
	start := strings.Index(text, from)
	if start < 0 {
		t.Fatalf("docs/REFERENCE.md no longer says %q", from)
	}
	part := text[start+len(from):]
	if end := strings.Index(part, to); end >= 0 {
		part = part[:end]
	}
	names := map[string]bool{}
	for _, match := range regexp.MustCompile("`([a-z-]+)`").FindAllStringSubmatch(part, -1) {
		names[match[1]] = true
	}
	return names
}

func TestTheReferenceNamesEveryCommandTheHostOrNoctisStarts(t *testing.T) {
	reference := docsFactsFile(t, "docs/REFERENCE.md")
	missing := func(listed, want map[string]bool) []string {
		out := []string{}
		for name := range want {
			if !listed[name] {
				out = append(out, name)
			}
		}
		sort.Strings(out)
		return out
	}
	started := docsFactsCommands(t, reference, "the commands a host or noctis itself starts (", ")")
	if gone := missing(started, startedByHost); len(gone) > 0 {
		t.Errorf("docs/REFERENCE.md leaves %v out of the commands a host or noctis itself starts", gone)
	}
	plumbing := docsFactsCommands(t, reference, "Called by the plugin itself: ", "\n")
	if gone := missing(plumbing, plumbingCommands); len(gone) > 0 {
		t.Errorf("docs/REFERENCE.md leaves %v out of the commands the plugin calls itself", gone)
	}
}

func TestTheReferenceGivesTheShippedDefaultRolesProfile(t *testing.T) {
	var defaults object
	if err := json.Unmarshal([]byte(docsFactsFile(t, "config.default.json")), &defaults); err != nil {
		t.Fatal(err)
	}
	shipped := getString(section(defaults, "roles"), "profile")
	row := regexp.MustCompile("(?m)^\\| `roles\\.profile / [^|]*\\| ([^ |]+) /").FindStringSubmatch(docsFactsFile(t, "docs/REFERENCE.md"))
	if row == nil {
		t.Fatal("docs/REFERENCE.md has no roles.profile row in its configuration table")
	}
	if shipped == "" || row[1] != shipped {
		t.Errorf("docs/REFERENCE.md gives %q as the default roles profile; config.default.json ships %q", row[1], shipped)
	}
}

// docsFactsModel names a role's assignment the way the guides and the setup picture do: the model and
// its version, then the effort after separator (" · " in the guides, "·" in the picture).
func docsFactsModel(t *testing.T, assignment object, separator string) string {
	t.Helper()
	name, known := map[string]string{"opus": "Opus 5.5", "sonnet": "Sonnet 5.5", "haiku": "Haiku 4.5"}[getString(assignment, "model")]
	if !known {
		t.Fatalf("the docs have no name for the model %q", getString(assignment, "model"))
	}
	if effort := getString(assignment, "effort"); effort != "" {
		return name + separator + effort
	}
	return name
}

func TestTheGuidesAndTheSetupPictureGiveEachProfileTheModelsItSets(t *testing.T) {
	for name, roles := range roleProfiles {
		// One column of the guides' table holds both digests and file search.
		if docsFactsModel(t, getMap(roles, "digest"), " · ") != docsFactsModel(t, getMap(roles, "explore"), " · ") {
			t.Fatalf("%s gives digests and file search different models; the guides show them in one column", name)
		}
	}
	for _, doc := range []string{"docs/GUIDE.md", "docs/GUIDE.tr.md"} {
		text := docsFactsFile(t, doc)
		for name, roles := range roleProfiles {
			cells := []string{}
			for _, role := range []string{"code", "research", "planning", "digest", "fallback"} {
				cells = append(cells, docsFactsModel(t, getMap(roles, role), " · "))
			}
			want := "| " + strings.Join(cells, " | ") + " |"
			row := regexp.MustCompile("(?m)^\\| \\*\\*" + regexp.QuoteMeta(profileTitles[name]) + "\\*\\*[^|]*(\\|.*)$").FindStringSubmatch(text)
			if row == nil {
				t.Errorf("%s has no row for the %s profile in its roles table", doc, profileTitles[name])
			} else if row[1] != want {
				t.Errorf("%s gives the %s profile\n  %s\nwhile it sets\n  %s", doc, profileTitles[name], row[1], want)
			}
		}
	}
	picture := docsFactsFile(t, "docs/install.svg")
	for name, roles := range roleProfiles {
		line := regexp.MustCompile(">\\d " + regexp.QuoteMeta(profileTitles[name]) + "</text>\\s*<text [^>]*>([^<]*)</text>").FindStringSubmatch(picture)
		if line == nil {
			t.Errorf("docs/install.svg shows no line for the %s profile", profileTitles[name])
			continue
		}
		for role, label := range map[string]string{"code": "code", "research": "research", "digest": "digests"} {
			if want := docsFactsModel(t, getMap(roles, role), "·") + " " + label; !strings.Contains(line[1], want) {
				t.Errorf("docs/install.svg shows the %s profile as %q, without %q", profileTitles[name], line[1], want)
			}
		}
	}
}

func TestTheDocsNameTheClaudeCodeWhoseSonnetIsSonnet55(t *testing.T) {
	for _, doc := range []string{"README.md", "README.tr.md", "docs/GUIDE.md", "docs/GUIDE.tr.md", "docs/REFERENCE.md", "skills/setup/SKILL.md"} {
		if !strings.Contains(docsFactsFile(t, doc), sonnetClaudeMin) {
			t.Errorf("%s does not name Claude Code %s, the first whose sonnet alias is Sonnet 5.5", doc, sonnetClaudeMin)
		}
	}
}
