package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestATimedOutStatusLineChainTakesItsChildrenWithIt(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "outlived")
	t.Setenv(lingerEnv, marker)
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	chain := "'" + strings.ReplaceAll(executable, "'", `'\''`) + "'; echo late"
	if isWindows {
		chain = `"` + executable + `" & echo late`
	}
	started := time.Now()
	if output := runChain(chain, []byte(`{}`)); output != "" {
		t.Fatalf("a chain that ran past its time limit still printed %q", output)
	}
	time.Sleep(time.Until(started.Add(lingerFor + 1500*time.Millisecond)))
	if statSafe(marker) != nil {
		t.Fatal("the chained status line command was cut off at its time limit, but the program it started ran on and finished afterwards: every slow status-line tick leaves one more process behind")
	}
}
