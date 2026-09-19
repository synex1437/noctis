package main

import (
	"regexp"
	"sync"
)

type lazyRe struct {
	once    sync.Once
	pattern string
	re      *regexp.Regexp
}

func lazyRegexp(pattern string) *lazyRe {
	return &lazyRe{pattern: pattern}
}

func (l *lazyRe) get() *regexp.Regexp {
	l.once.Do(func() { l.re = regexp.MustCompile(l.pattern) })
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
