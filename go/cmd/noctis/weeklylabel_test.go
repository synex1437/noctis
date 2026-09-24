package main

import (
	"strings"
	"testing"
)

func TestTheWeeklyLabelReadsAsPartOfEachSentence(t *testing.T) {
	previous := locale
	t.Cleanup(func() { locale = previous })
	broken := map[string][]string{
		"de": {"wöchentlich-", "Wochen Limit"},
		"nl": {"wekelijks-"},
		"es": {"de semanal"},
		"pt": {"de semanal"},
		"it": {"di settimanale"},
		"pl": {"okno tygodniowy", "użycia tygodniowy", "na tygodniowy", "Zużycie tygodniowy"},
		"ru": {"окно недельный", "использовании недельный", "Ожидание недельный", "Использование недельный"},
		"ar": {"نافذة أسبوعي"},
	}
	for code, fragments := range broken {
		locale = code
		label := windowLabel("seven_day")
		texts := []string{
			T("wait.saved", label, "95", "10:00", ""),
			T("wait.resumed", label, "95", "5 min"),
			T("wait.reason", label, "95", T("hit.threshold", "89"), "10:00"),
			T("wait.dataReady", label, ""),
			T("wait.ready", label, ""),
			T("wait.notifyReset", label),
			T("wait.earlyResetNotify", label),
			T("wait.earlyReset", label, "5 min"),
			T("wait.cancelled", label),
			T("session.alreadyOver", label, "99", "10:00"),
		}
		for _, text := range texts {
			for _, fragment := range fragments {
				if strings.Contains(text, fragment) {
					t.Errorf("%s: %q splices the weekly label as %q", code, text, fragment)
				}
			}
		}
	}
}
