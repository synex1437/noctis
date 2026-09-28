package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// environAfterMain names a file TestMain writes the environment to once main returns: what noctis
// would hand a program it starts.
const environAfterMain = "NOCTIS_TEST_ENVIRON_AFTER_MAIN"

func TestNoctisHandsOnNoGOMAXPROCSTheLauncherSetButKeepsTheUsers(t *testing.T) {
	for _, run := range []struct {
		name  string
		given []string
		want  string // GOMAXPROCS in what noctis hands on; "" for none
	}{
		{"the launcher's", []string{"GOMAXPROCS=1", launcherGOMAXPROCS + "=1"}, ""},
		{"the user's", []string{"GOMAXPROCS=3"}, "3"},
	} {
		t.Run(run.name, func(t *testing.T) {
			work := t.TempDir()
			dump := filepath.Join(work, "environ")
			child := exec.Command(os.Args[0])
			child.Dir = work
			child.Env = append(withoutEnv(os.Environ(), "HOME", "USERPROFILE", "home", claudeConfigEnv, "GOMAXPROCS", launcherGOMAXPROCS),
				append(run.given, `NOCTIS_TEST_MAIN_ARGS=["--version"]`, environAfterMain+"="+dump)...)
			if output, err := child.CombinedOutput(); err != nil || strings.TrimSpace(string(output)) != pluginVersion {
				t.Fatalf("noctis --version exited with %v and said %q", err, output)
			}
			environ, err := os.ReadFile(dump)
			if err != nil {
				t.Fatal(err)
			}
			got, marked := "", false
			for _, entry := range strings.Split(string(environ), "\n") {
				if value, ok := strings.CutPrefix(entry, "GOMAXPROCS="); ok {
					got = value
				}
				marked = marked || strings.HasPrefix(entry, launcherGOMAXPROCS+"=")
			}
			if got != run.want || marked {
				t.Fatalf("with %s GOMAXPROCS noctis hands on GOMAXPROCS=%q (want %q) and the launcher's marker: %v", run.name, got, run.want, marked)
			}
		})
	}
}
