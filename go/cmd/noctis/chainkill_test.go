package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestATimedOutStatusLineChainTakesItsChildrenWithIt(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "beats")
	t.Setenv(lingerEnv, marker)
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	chain := "'" + strings.ReplaceAll(executable, "'", `'\''`) + "'; echo late"
	if isWindows {
		chain = `"` + executable + `" & echo late`
	}
	if output := runChain(chain, []byte(`{}`)); output != "" {
		t.Fatalf("a chain that ran past its time limit still printed %q", output)
	}
	// runChain returns once the stop it sends at the time limit is through. On Windows that stop
	// starts taskkill, which on a runner busy with the other shards has taken about 3 seconds, so the
	// program the chain started is not timed against the limit: it adds a byte to the marker every
	// lingerBeat while it runs, and from here on the marker must not grow.
	time.Sleep(5 * lingerBeat)
	before, _ := os.ReadFile(marker)
	time.Sleep(10 * lingerBeat)
	if after, _ := os.ReadFile(marker); len(after) != len(before) {
		t.Fatalf("the chained status line command was cut off at its time limit, but the program it started ran on (%d beats, then %d a second later): every slow status-line tick leaves one more process behind", len(before), len(after))
	}
}
