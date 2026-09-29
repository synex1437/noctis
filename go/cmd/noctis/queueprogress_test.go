package main

import (
	"fmt"
	"testing"
)

func TestACommitCountsAsProgressOnALongItem(t *testing.T) {
	cfg, project := stopCapSandbox(t, "claude", 2, 3)
	t.Setenv("CLAUDE_CODE_STOP_HOOK_BLOCK_CAP", "")
	gitIn(t, project, "init", "-q")
	writeRepoFile(t, project, "work/0.txt", "0\n")
	gitIn(t, project, "add", "-A")
	gitIn(t, project, "commit", "-q", "-m", "start")
	blocked, last := stopsInARow(t, cfg, "head1", project, 7, func(stop int) {
		if stop == 0 {
			return
		}
		writeRepoFile(t, project, fmt.Sprintf("work/%d.txt", stop), fmt.Sprintf("%d\n", stop))
		gitIn(t, project, "add", "-A")
		gitIn(t, project, "commit", "-q", "-m", fmt.Sprintf("step %d", stop))
	})
	if blocked != 7 || gaveUpOn("head1") {
		t.Fatalf("a session that committed before every stop, with no item ticked, was let go after %d of 7 stops with queue.maxIdleContinues 2 (gave up: %v, last output %v); a new commit is progress on a long item", blocked, gaveUpOn("head1"), last)
	}
	if blocked, _ := stopsInARow(t, cfg, "head2", project, 7, nil); blocked > 2 || !gaveUpOn("head2") {
		t.Fatalf("a session that neither ticked an item nor committed was held for %d stops with queue.maxIdleContinues 2 (gave up: %v); the commit HEAD already named at its first stop is not progress", blocked, gaveUpOn("head2"))
	}
}
