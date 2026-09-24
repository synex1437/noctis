package main

import (
	"os"
	"testing"
)

func TestPathsForPutsTheEnvironmentBackAsItFoundIt(t *testing.T) {
	previous, previousArgs := files, args
	t.Cleanup(func() { files, args = previous, previousArgs })
	args = parseArgs(nil)
	names := []string{"CLAUDE_CONFIG_DIR", "NOCTIS_PLUGIN_ROOT"}
	for _, name := range names {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	pathsFor(t.TempDir(), t.TempDir())
	for _, name := range names {
		if value, set := os.LookupEnv(name); set {
			t.Errorf("%s was not set before pathsFor and is set to %q after it, for every program setup starts next", name, value)
		}
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "/elsewhere/.claude")
	pathsFor(t.TempDir(), t.TempDir())
	if value := os.Getenv("CLAUDE_CONFIG_DIR"); value != "/elsewhere/.claude" {
		t.Errorf("CLAUDE_CONFIG_DIR was /elsewhere/.claude before pathsFor and %q after it", value)
	}
}
