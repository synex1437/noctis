package main

import "testing"

// On macOS processName is what `ps -o comm=` prints: the full path of the program, which may hold
// spaces when the plugin sits under ~/Library/Application Support.
func TestOwnHelperProcessKnowsItsProgramInAFolderWithASpace(t *testing.T) {
	for _, name := range []string{
		"/Users/dev/Library/Application Support/noctis/bin/noctis",
		"/Users/dev/Library/Application Support/dev tools/plugins/noctis/7.5.2/bin/darwin-arm64/noctis\n",
		"/Volumes/Work Disk/noctis/bin/noctis",
	} {
		if !ownHelperProcess(name) {
			t.Errorf("%q is our own helper: without it the old window is never closed, sleepers are not ours to stop and a re-arm starts a second one", name)
		}
	}
	for _, name := range []string{
		"/opt/noctis helpers/bin/node",
		"/Users/dev/Library/Application Support/noctis tools/bin/other",
		"/Applications/Some App.app/Contents/MacOS/Some App",
	} {
		if ownHelperProcess(name) {
			t.Errorf("%q runs another program (from a folder whose name starts with noctis) and must not be stopped as ours", name)
		}
	}
}
