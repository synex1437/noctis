package main

import (
	"slices"
	"sort"
	"strings"
	"testing"
)

func htmlReportPage(t *testing.T) string {
	t.Helper()
	sandboxFiles(t)
	data := reportData{days: 7, transcripts: 1, byModel: map[string]*tokenBucket{"claude-opus-5-5": {input: 200, output: 100, cacheRead: 2000, calls: 2}}, byDay: map[string]*tokenBucket{}, events: map[string]int{}}
	return reportHTML(object{"models": object{"primary": "fable"}}, data)
}

func TestTheHTMLReportNamesItsPageAndColumnsInTheUsersLanguage(t *testing.T) {
	speakTurkish(t)
	page := htmlReportPage(t)
	head := page[strings.Index(page, "<thead>"):strings.Index(page, "</thead>")]
	for _, column := range []string{"giriş", "çıkış", "önbellek okuma", "önbellek yazma", "çağrı", "maliyet"} {
		if !strings.Contains(head, "<th>"+column+"</th>") {
			t.Errorf("the Turkish HTML report has no %q column: %s", column, head)
		}
	}
	if !strings.Contains(page, "<title>noctis raporu</title>") {
		t.Errorf("the Turkish HTML report keeps an English page title: %s", page[strings.Index(page, "<title>"):strings.Index(page, "</title>")])
	}

	codes := []string{"en", "tr"}
	for code := range extraCatalogBuilders {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	for _, code := range codes {
		locale = code
		if columns := strings.Split(T("report.columns"), "|"); len(columns) != 7 || slices.Contains(columns, "") {
			t.Errorf("the %s catalog names %d report columns, want the 7 the table has (model, in, out, cache read, cache write, calls, cost): %q", code, len(columns), T("report.columns"))
		}
	}
}
