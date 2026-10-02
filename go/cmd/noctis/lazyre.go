package main

import (
	"regexp"
	"sync"
)

type lazyRe struct {
	once    sync.Once
	pattern string
	// source builds pattern on first use, for one assembled from word lists: a process that never
	// matches it does not assemble it as it starts.
	source func() string
	re     *regexp.Regexp
}

func lazyRegexp(pattern string) *lazyRe {
	return &lazyRe{pattern: pattern}
}

func lazyRegexpOf(source func() string) *lazyRe {
	return &lazyRe{source: source}
}

func (l *lazyRe) get() *regexp.Regexp {
	l.once.Do(func() {
		if l.source != nil {
			l.pattern = l.source()
		}
		l.re = regexp.MustCompile(l.pattern)
	})
	return l.re
}

func (l *lazyRe) MatchString(s string) bool            { return l.get().MatchString(s) }
func (l *lazyRe) FindString(s string) string           { return l.get().FindString(s) }
func (l *lazyRe) FindStringSubmatch(s string) []string { return l.get().FindStringSubmatch(s) }
func (l *lazyRe) FindAllStringSubmatch(s string, n int) [][]string {
	return l.get().FindAllStringSubmatch(s, n)
}
func (l *lazyRe) ReplaceAllString(s, repl string) string { return l.get().ReplaceAllString(s, repl) }
func (l *lazyRe) Split(s string, n int) []string         { return l.get().Split(s, n) }
func (l *lazyRe) Match(b []byte) bool                    { return l.get().Match(b) }
func (l *lazyRe) ReplaceAllLiteralString(s, repl string) string {
	return l.get().ReplaceAllLiteralString(s, repl)
}
