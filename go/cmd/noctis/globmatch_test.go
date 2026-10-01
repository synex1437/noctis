package main

import (
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

// globMatchByTrying is globMatch as 8.2.0 shipped it: a star tries
// every share of the rest of the name. Its answers are the reference here; its
// time grows with the number of ways to share the name among the stars.
func globMatchByTrying(pattern, name string) bool {
	p, n := []rune(pattern), []rune(name)
	var match func(i, j int) bool
	match = func(i, j int) bool {
		for i < len(p) {
			switch p[i] {
			case globAny:
				for k := j; k <= len(n); k++ {
					if match(i+1, k) {
						return true
					}
				}
				return false
			case globOne:
				if j >= len(n) {
					return false
				}
			case globSet:
				if end := bracketEnd(p, i); end >= 0 {
					if j >= len(n) {
						return false
					}
					i = end
				} else if j >= len(n) || n[j] != '[' {
					return false
				}
			default:
				if j >= len(n) || p[i] != n[j] {
					return false
				}
			}
			i++
			j++
		}
		return j == len(n)
	}
	return match(0, 0)
}

func TestGlobMatchAnswersAsTryingEveryShareOfTheName(t *testing.T) {
	pieces := []string{string(globAny), string(globOne), string(globSet), "[", "]", "!", "^", ":", ".", "=", "a", "b"}
	letters := []string{"a", "b", "[", "]", ".", ":"}
	random := rand.New(rand.NewPCG(83, 1))
	for range 100000 {
		var pattern, name strings.Builder
		for range random.IntN(9) {
			pattern.WriteString(pieces[random.IntN(len(pieces))])
		}
		for range random.IntN(7) {
			name.WriteString(letters[random.IntN(len(letters))])
		}
		if got, want := globMatch(pattern.String(), name.String()), globMatchByTrying(pattern.String(), name.String()); got != want {
			t.Fatalf("globMatch(%q, %q) = %v; trying every share of the name gives %v", pattern.String(), name.String(), got, want)
		}
	}
}

func TestARunOfStarsBeforeANameIsReadAtOnce(t *testing.T) {
	stars := strings.Repeat(string(globAny), 64)
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{stars + "00000000", "noctis.exe", false},
		{stars, "noctis.exe", true},
		{stars + ".exe", "noctis.exe", true},
		{"n" + stars + "s" + stars, "noctis", true},
		{stars + "x" + stars + "y", "noctis.exe", false},
	}
	answers := make(chan []bool, 1)
	go func() {
		got := []bool{}
		for _, c := range cases {
			got = append(got, globMatch(c.pattern, c.name))
		}
		// The command the fuzzer found: 31 stars before "noctis.exe" took the guard some 20 seconds.
		deniedNoctisCommand("000000000 *******************************00000000 noCtis --0 stAte-grite", true)
		answers <- got
	}()
	select {
	case got := <-answers:
		for i, c := range cases {
			if got[i] != c.want {
				t.Errorf("globMatch(%d stars and %q, %q) = %v, want %v", strings.Count(c.pattern, string(globAny)), strings.ReplaceAll(c.pattern, string(globAny), ""), c.name, got[i], c.want)
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a run of stars still takes the glob match one try for each way to share the name among them")
	}
}
