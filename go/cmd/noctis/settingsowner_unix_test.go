//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAWorkTreeSomeoneElseOwnsKeepsTheLocalSettingsWhereTheSessionStarted(t *testing.T) {
	leanSandbox(t)
	clearCompactionVariables(t)
	root := realTempDir(t)
	session := filepath.Join(root, "app")
	mkdirs(t, filepath.Join(root, ".git"), session)
	projectSettingsIn(t, root, "settings.local.json", object{"autoCompactWindow": float64(313000)})
	if got := pointsIn(t, session, "claude-opus-5-5"); !samePoints(got, 280000) {
		t.Fatalf("the root's settings.local.json gave %v, want 280000", got)
	}
	previous := effectiveUser
	t.Cleanup(func() { effectiveUser = previous })
	effectiveUser = func() int { return previous() + 1 }
	if got := pointsIn(t, session, "claude-opus-5-5"); !samePoints(got, 967000) {
		t.Fatalf("the settings.local.json of a work tree someone else owns was read: %v", got)
	}
	effectiveUser = previous
	if os.Geteuid() != 0 {
		return
	}
	// The root's .claude, where it has one, must be the user's too.
	if err := os.Lchown(filepath.Join(root, ".claude"), 4242, 4242); err != nil {
		t.Fatal(err)
	}
	if got := pointsIn(t, session, "claude-opus-5-5"); !samePoints(got, 967000) {
		t.Fatalf("the settings.local.json under a .claude someone else owns was read: %v", got)
	}
}
