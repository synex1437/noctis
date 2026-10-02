package main

import (
	"path/filepath"
	"strings"
	"testing"
)

const pastedPythonBlock = "\n```python\ndef find_user(users, name):\n    for user in users:\n        if user.name == name and user.active:\n            return user\n    return None\n```\n"

func typeIntoSession(t *testing.T, sid, prompt string) {
	t.Helper()
	hostHook(t, "claude", object{"hook_event_name": "UserPromptSubmit", "session_id": sid, "cwd": t.TempDir(), "prompt": prompt})
}

func TestAPastedCodeBlockLeavesTheSessionInTheLanguageTheUserTypes(t *testing.T) {
	queueTrustSandbox(t, false)
	t.Setenv("NOCTIS_LANG", "")
	for _, tc := range []struct{ lang, typed, withCode string }{
		{"zh", "帮我看看为什么登录页面这么慢", "帮我优化一下这个函数，太慢了：" + pastedPythonBlock},
		{"ko", "로그인 페이지가 왜 이렇게 느린지 좀 봐 줄래요?", "이 함수가 너무 느려요. 빠르게 고쳐 주세요:" + pastedPythonBlock},
	} {
		sid := "pasted-code-" + tc.lang
		typeIntoSession(t, sid, tc.typed)
		if got := sessionLanguage(readState(), sid); got != tc.lang {
			t.Fatalf("%s: the prompt without code set the session language to %q", tc.lang, got)
		}
		for _, prompt := range []string{tc.withCode, strings.TrimSpace(pastedPythonBlock)} {
			typeIntoSession(t, sid, prompt)
			if got := sessionLanguage(readState(), sid); got != tc.lang {
				t.Errorf("%s: a prompt with a pasted code block switched the session's notices, status line and toasts to %q: %q", tc.lang, got, prompt)
			}
		}
	}
}

func TestATurnClaudeCodeWritesLeavesTheSessionInTheUsersLanguage(t *testing.T) {
	queueTrustSandbox(t, false)
	t.Setenv("NOCTIS_LANG", "")
	for lang, typed := range map[string]string{
		"tr": "Giriş sayfası neden bu kadar yavaş açılıyor, bir bakar mısın lütfen?",
		"de": "Kannst du dir bitte ansehen, warum die Anmeldeseite so langsam lädt?",
	} {
		for name, turn := range m1AgentTurns() {
			sid := lang + "-" + name
			typeIntoSession(t, sid, typed)
			if got := sessionLanguage(readState(), sid); got != lang {
				t.Fatalf("%s: the prompt the user typed set the session language to %q", sid, got)
			}
			typeIntoSession(t, sid, turn)
			if got := sessionLanguage(readState(), sid); got != lang {
				t.Errorf("%s: a turn Claude Code wrote, not the user, switched the session's notices, status line and toasts to %q", sid, got)
			}
		}
	}
}

func TestTheQueueReasonCopilotSendsBackAsAPromptLeavesTheSessionInTheUsersLanguage(t *testing.T) {
	_, project := copilotSandbox(t)
	t.Setenv("NOCTIS_LANG", "")
	sid := "cp-lang"
	hostHook(t, "copilot", copilotPayload(sid, project, object{"prompt": "Giriş sayfası neden bu kadar yavaş açılıyor, bir bakar mısın lütfen?"}))
	if got := sessionLanguage(readState(), sid); got != "tr" {
		t.Fatalf("the prompt the user typed set the session language to %q", got)
	}
	stop := hostHook(t, "copilot", copilotPayload(sid, project, object{"transcriptPath": filepath.Join(project, "events.jsonl"), "stopReason": "end_turn"}))
	if getString(stop, "decision") != "block" {
		t.Fatalf("the queue did not continue: %v", stop)
	}
	hostHook(t, "copilot", copilotPayload(sid, project, object{"prompt": getString(stop, "reason")}))
	if got := sessionLanguage(readState(), sid); got != "tr" {
		t.Errorf("noctis's own queue continuation, sent back by Copilot as the next prompt, switched the session's notices, status line and toasts to %q", got)
	}
}

func TestNoctisLangOrLocaleWrittenWithARegionPinsItsLanguage(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "en_US.UTF-8")
	auto := object{"locale": "auto"}
	for value, want := range map[string]string{"pt_BR": "pt", "pt-BR": "pt", "de_DE.UTF-8": "de", "zh-CN": "zh", "fr_FR": "fr", "tr_TR.utf8": "tr", "ES-419": "es"} {
		t.Setenv("NOCTIS_LANG", value)
		if got := detectLocale(auto); got != want || sessionLocaleWanted(auto) {
			t.Errorf("NOCTIS_LANG=%s: the interface is %q and the session's prompts still pick it: %v; want %s pinned", value, got, sessionLocaleWanted(auto), want)
		}
	}
	t.Setenv("NOCTIS_LANG", "")
	for value, want := range map[string]string{"pt-BR": "pt", "de_DE": "de", "zh-CN": "zh", " it_IT.UTF-8 ": "it"} {
		cfg := object{"locale": value}
		if got := detectLocale(cfg); got != want || sessionLocaleWanted(cfg) {
			t.Errorf("config locale %q: the interface is %q and the session's prompts still pick it: %v; want %s pinned", value, got, sessionLocaleWanted(cfg), want)
		}
	}
}

func TestANoctisLangOrLocaleNamingNoLanguageNoctisHasCountsAsAuto(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "de_DE.UTF-8")
	auto := object{"locale": "auto"}
	for _, value := range []string{"fi", "sv-SE", "nb_NO.UTF-8"} {
		t.Setenv("NOCTIS_LANG", value)
		if got := detectLocale(auto); got != "de" || !sessionLocaleWanted(auto) {
			t.Errorf("NOCTIS_LANG=%s: the interface is %q and the session's prompts pick it: %v; want LANG's de until the first prompt, then the prompts' language", value, got, sessionLocaleWanted(auto))
		}
		t.Setenv("NOCTIS_LANG", "")
		cfg := object{"locale": value}
		if got := detectLocale(cfg); got != "de" || !sessionLocaleWanted(cfg) {
			t.Errorf("config locale %q: the interface is %q and the session's prompts pick it: %v; want LANG's de until the first prompt, then the prompts' language", value, got, sessionLocaleWanted(cfg))
		}
	}
}
