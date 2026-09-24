package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAutoQueueItems(t *testing.T) {
	pad := "Here is some context about the project so that the prompt is long enough for the detector to consider it: " + strings.Repeat("the codebase is a Node service with a Postgres database and a React front end. ", 2)
	cases := []struct {
		name   string
		prompt string
		want   int
	}{
		{"bullet list", pad + "\n- add input validation to the signup form\n- write tests for the payments module\n- update the README for the new CLI flags\n", 3},
		{"numbered list with checked item", pad + "\n1. [x] add input validation to the signup form\n2. [ ] write tests for the payments module\n3. [ ] update the README for the new CLI flags\n4. [ ] deploy the service to staging\n", 3},
		{"list of questions", pad + "\nCould you walk me through it?\n- where is the session cookie set?\n- why does the redirect loop happen?\n- is the CSRF token validated on every request?\n", 0},
		{"bug report with pasted log", pad + "\nI'm getting an error when I run the integration tests.\nHere is the exact output from the terminal:\nTypeError: cannot read properties of undefined\n    at Object.<anonymous> (test/payments.test.js:12:5)\n    at Module._compile (node:internal/modules/cjs/loader:1256:14)\nCan you help me figure out what is wrong?\n", 0},
		{"repro steps", pad + "\nSteps to reproduce:\n1. open the settings page in the app\n2. click the save button twice quickly\n3. watch the spinner never disappear\nExpected: the form saves once.\n", 0},
		{"context lines then one task", pad + "\nOur app is a marketplace for used bikes.\nUsers can post listings with photos.\nSellers get paid through Stripe Connect.\nPlease fix the checkout bug in the cart page.\n", 0},
		{"line per step", pad + "\nBuild a REST API for the todo app using Express.\nAdd JWT authentication with refresh tokens.\nWrite integration tests for every endpoint.\nDeploy the service to Fly.io with a health check.\n", 4},
		{"paragraph with sequence words", "Build a REST API for the todo app using Express and Postgres. Add JWT authentication with refresh tokens to it. Then write integration tests for every endpoint we expose. After that wire up a GitHub Actions workflow for the tests. Finally deploy the service to Fly.io with a health check endpoint.", 5},
		{"paragraph without sequence words", "Build a REST API for the todo app using Express and Postgres. Add JWT authentication with refresh tokens to it. Write integration tests for every endpoint we expose. Wire up a GitHub Actions workflow for the tests. Deploy the service to Fly.io with a health check endpoint.", 0},
		{"short prompt", "- add tests\n- fix lint\n- deploy", 0},
		{"turkish list", pad + "\n- kayıt formuna girdi doğrulaması ekle ve hataları göster\n- ödeme modülü için birim testlerini yaz\n- yeni CLI bayrakları için README dosyasını güncelle\n", 3},
		{"turkish question", pad + "\n- kayıt formuna girdi doğrulaması ekle ve hataları göster\n- ödeme modülü için birim testlerini yaz\n- yeni CLI bayrakları için README dosyasını güncelle\nBunları nasıl yapmalıyım sence?", 0},
	}
	for _, tc := range cases {
		got := autoQueueItems(tc.prompt)
		if len(got) != tc.want {
			t.Errorf("%s: want %d items, got %d: %q", tc.name, tc.want, len(got), got)
		}
	}
}

func TestCleanItem(t *testing.T) {
	if got := cleanItem("[x] already done thing"); got != "" {
		t.Errorf("checked item should be skipped, got %q", got)
	}
	if got := cleanItem("[ ] open thing to do"); got != "open thing to do" {
		t.Errorf("unchecked box should be stripped, got %q", got)
	}
	if got := cleanItem("[noctis] keep the tag"); got != "[noctis] keep the tag" {
		t.Errorf("bracketed word must survive, got %q", got)
	}
}

func TestCleanItemShortensALongItemWithoutRewritingItsBytes(t *testing.T) {
	long := "fix\xffthe parser " + strings.Repeat("and the lexer ", 30)
	got := cleanItem(long)
	if !strings.HasSuffix(got, "…") || !strings.HasPrefix(long, strings.TrimSuffix(got, "…")) {
		t.Fatalf("the shortened item is not the start of the text: %q", got)
	}
	if runes := utf8.RuneCountInString(got); runes != 201 {
		t.Fatalf("the shortened item has %d runes, want 201", runes)
	}
	prompt := "- fix\xffA1 " + strings.Repeat("the parser module ", 14) + "\n- fix\xffB2 " + strings.Repeat("the lexer module ", 14) + "\n- fix\xffC3 " + strings.Repeat("the build module ", 14) + "\n"
	items := autoQueueItems(prompt)
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d: %q", len(items), items)
	}
	for _, item := range items {
		if !strings.HasPrefix(item, "fix\xff") {
			t.Fatalf("item %q does not start with the prompt's own bytes", item)
		}
	}
}
