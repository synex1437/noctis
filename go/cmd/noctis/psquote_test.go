package main

import (
	"strings"
	"testing"
)

// r7PowerShellLiteral reads the single-quoted string at the start of script the way PowerShell's
// tokenizer does: the apostrophe and the typographic single quotes U+2018 to U+201B each open and
// close the string, and two of them in a row stand for the second one. It returns the string's value
// and the script after it.
func r7PowerShellLiteral(script string) (value, rest string, ok bool) {
	isQuote := func(r rune) bool { return strings.ContainsRune("'\u2018\u2019\u201a\u201b", r) }
	runes := []rune(script)
	if len(runes) == 0 || !isQuote(runes[0]) {
		return "", script, false
	}
	var literal strings.Builder
	for index := 1; index < len(runes); index++ {
		if isQuote(runes[index]) {
			if index+1 < len(runes) && isQuote(runes[index+1]) {
				index++
				literal.WriteRune(runes[index])
				continue
			}
			return literal.String(), string(runes[index+1:]), true
		}
		literal.WriteRune(runes[index])
	}
	return "", "", false
}

func TestPowerShellQuotingSurvivesTypographicQuotes(t *testing.T) {
	for _, text := range []string{
		`C:\Users\O’Neil\.claude`,
		`C:\Users\O’Neil\.claude\noctis\runner.cmd resume --account "C:\Users\O’Neil\.claude"`,
		`’; Remove-Item C:\ -Recurse; ’`,
		`‘$(Remove-Item ~ -Recurse)‘`,
		`‚ + (whoami) + ‚`,
		`‛); Stop-Computer; (‛`,
		`'’‘‚‛`,
		`it's`,
	} {
		quoted := psQuote(text)
		value, rest, ok := r7PowerShellLiteral(quoted)
		if !ok || value != text || rest != "" {
			t.Errorf("psQuote(%q) = %q: PowerShell reads the string %q and then runs %q as script", text, quoted, value, rest)
		}
	}
}
