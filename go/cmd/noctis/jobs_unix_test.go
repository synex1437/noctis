//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestKillingThePidAJobRunPrintsEndsTheJobsCommandAndWhatItStarted(t *testing.T) {
	lab := newJobLab(t, "jk-e2e")
	run := lab.noctis("job", "run", "--label", "hung", "--", "sleep 60 & echo $! > sleeper.pid; wait")
	printed := regexp.MustCompile(`\(pid (\d+),`).FindStringSubmatch(run.stdout)
	if run.code != 0 || printed == nil {
		t.Fatalf("noctis job run did not start the job:\n%s", run)
	}
	child := int(numberOr(lab.waitFor("1", "child", 30*time.Second), "child", 0))
	sleeper := 0
	for deadline := time.Now().Add(30 * time.Second); sleeper == 0; time.Sleep(20 * time.Millisecond) {
		data, _ := os.ReadFile(filepath.Join(lab.project, "sleeper.pid"))
		sleeper, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		if sleeper == 0 && time.Now().After(deadline) {
			t.Fatal("the job's command did not start its sleep")
		}
	}
	wrapper, _ := strconv.Atoi(printed[1])
	if err := syscall.Kill(wrapper, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for (processAlive(child) || processAlive(sleeper)) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if processAlive(child) || processAlive(sleeper) {
		t.Fatalf("10 s after kill %d, the pid noctis job run printed, the job's command (pid %d, alive %t) and the sleep it started (pid %d, alive %t) still run; noctis job list says:\n%s",
			wrapper, child, processAlive(child), sleeper, processAlive(sleeper), lab.noctis("job", "list").stdout)
	}
	if job := lab.waitFor("1", "ended", 10*time.Second); numberOr(job, "exit", 0) != -1 {
		t.Fatalf("the killed job's record is %v, want it ended by a signal", job)
	}
	if list := lab.noctis("job", "list"); !strings.Contains(list.stdout, "ended by a signal") {
		t.Fatalf("noctis job list does not say the killed job ended by a signal:\n%s", list)
	}
}
