package main

import (
	"strings"
	"testing"
)

func TestTranslationsPutEachValueWhereTheirSentenceNeedsIt(t *testing.T) {
	previous := locale
	t.Cleanup(func() { locale = previous })
	cases := []struct {
		code, key string
		args      []any
		phrases   []string
	}{
		{"zh", "wait.earlyReset", []any{"LABEL", "WAITED"}, []string{"等待 WAITED 后", "LABEL 限额"}},
		{"ko", "wait.earlyReset", []any{"LABEL", "WAITED"}, []string{"WAITED 대기 후", "LABEL 한도"}},
		{"ja", "wait.earlyReset", []any{"LABEL", "WAITED"}, []string{"LABEL の上限", "WAITED の待機"}},
		{"zh", "report.saved", []any{"SAVED", "ACTUAL", "PRIMARY"}, []string{"约 SAVED", "花了 ACTUAL 而不是 PRIMARY"}},
		{"ko", "report.saved", []any{"SAVED", "ACTUAL", "PRIMARY"}, []string{"약 SAVED", "PRIMARY 대신 ACTUAL"}},
		{"ja", "report.saved", []any{"SAVED", "ACTUAL", "PRIMARY"}, []string{"約 SAVED", "PRIMARY ではなく ACTUAL"}},
	}
	for _, tc := range cases {
		locale = tc.code
		text := T(tc.key, tc.args...)
		for _, phrase := range tc.phrases {
			if !strings.Contains(text, phrase) {
				t.Errorf("%s %s: %q does not say %q", tc.code, tc.key, text, phrase)
			}
		}
	}
}
