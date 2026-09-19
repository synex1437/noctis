package main

import (
	"os"
	"regexp"
)

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func scopedPatternForTest() *regexp.Regexp {
	return regexp.MustCompile("(?i)fable")
}
