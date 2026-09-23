//go:build windows

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunnerLauncherWriterStandIn(t *testing.T) {
	launcher := os.Getenv("NOCTIS_TEST_RUNNER_LAUNCHER")
	if launcher == "" {
		return
	}
	files.runnerLauncher = launcher
	ensureRunnerLauncher()
}

func copyOfThisBinary(t *testing.T, dir string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	copied := filepath.Join(dir, "noctis.exe")
	target, err := os.Create(copied)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(target, source)
	if closeErr := target.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		t.Fatal(copyErr)
	}
	return copied
}

func TestATaskStartsNoctisFromAProfileNamedInTurkishWhateverTheConsoleCodePage(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "Çağrı Müller")
	if err := os.Mkdir(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	noctis := copyOfThisBinary(t, profile)
	launcher := filepath.Join(profile, "runner.cmd")
	writer := exec.Command(noctis, "-test.run=^TestRunnerLauncherWriterStandIn$")
	writer.Env = append(os.Environ(), "NOCTIS_TEST_RUNNER_LAUNCHER="+launcher)
	if output, err := writer.CombinedOutput(); err != nil {
		t.Fatalf("the copy of noctis in %s did not write its launcher: %v\n%s", profile, err, output)
	}
	written, err := os.ReadFile(launcher)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(written, []byte(`\Çağrı Müller\noctis.exe" %*`)) {
		t.Fatalf("the launcher does not start the copy of noctis in %s:\n%s", profile, written)
	}

	type consoleRun struct{ name, commandLine string }
	arguments := []string{"-test.run", "TestRunnerLauncherWriterStandIn"}
	runs := []consoleRun{{"as the task runs it", "cmd.exe " + windowsTaskArgument(launcher, arguments)}}
	for _, codePage := range []string{"437", "850", "857", "866", "932"} {
		runs = append(runs, consoleRun{"code page " + codePage,
			fmt.Sprintf(`cmd.exe /d /s /c "chcp %s & call "%s" %s"`, codePage, launcher, strings.Join(arguments, " "))})
	}
	for _, run := range runs {
		t.Run(run.name, func(t *testing.T) {
			shell := exec.Command("cmd.exe")
			shell.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x00000010, CmdLine: run.commandLine}
			var complaints bytes.Buffer
			shell.Stderr = &complaints
			output, err := runWithTimeout(shell, time.Minute)
			if err != nil || !strings.Contains(string(output), "PASS") {
				t.Fatalf("in a console of its own, %s did not start noctis from %s (%v):\n%s%s", run.commandLine, profile, err, output, complaints.Bytes())
			}
		})
	}
}
