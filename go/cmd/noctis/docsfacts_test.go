package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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
