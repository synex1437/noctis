package main

import "testing"

func TestAChainedStatusLineThatExitsNonZeroStillShowsWhatItPrinted(t *testing.T) {
	// The shape of many status line scripts: the line, then an optional part whose test fails,
	// which leaves the script's exit status at 1.
	chain := `echo my-bar; [ -n "$U6_NEVER_SET" ] && echo extra`
	if isWindows {
		chain = `echo my-bar& exit 1`
	}
	t.Setenv("U6_NEVER_SET", "")
	if got := runChain(chain, []byte(`{}`)); got != "my-bar" {
		t.Fatalf("a chained status line that printed its line and then exited 1 showed %q; want %q", got, "my-bar")
	}
}
