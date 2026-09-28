package main

import (
	"bytes"
	"os"
	"testing"
)

func TestAReaderLeavesABrokenStateJSONAloneWhileAWriterHoldsTheLock(t *testing.T) {
	for _, backup := range []string{"with a backup", "without a backup"} {
		t.Run(backup, func(t *testing.T) {
			sandboxFiles(t)
			stamp := float64(nowSec())
			updateState(func(state object) { stateMap(state, "notified")["kept"] = stamp })
			updateState(func(state object) { stateMap(state, "notified")["later"] = stamp })
			if backup == "without a backup" {
				if err := os.Remove(files.stateBackup); err != nil {
					t.Fatal(err)
				}
			}
			broken := []byte(`{"notified":`)
			cliWrite(t, files.state, broken)

			withFileLock(files.stateLock, func() {
				notified := getMap(peekState(), "notified")
				if backup == "with a backup" && (notified["kept"] != stamp || notified["later"] != nil) {
					t.Errorf("a reader that could not take state.lock did not read the backup: %v", notified)
				}
				if backup == "without a backup" && len(notified) != 0 {
					t.Errorf("a reader that found no usable backup did not start empty: %v", notified)
				}
				if content, err := os.ReadFile(files.state); err != nil || !bytes.Equal(content, broken) {
					t.Errorf("a reader that does not hold state.lock rewrote or removed state.json while a writer held the lock: %q, %v", content, err)
				}
			})

			state := readState()
			if backup == "with a backup" {
				if notified := getMap(state, "notified"); notified["kept"] != stamp {
					t.Fatalf("once the lock was free the reader did not recover the backup: %v", notified)
				}
				if restored := readJSONStrict(files.state); !restored.ok || restored.data == nil {
					t.Fatalf("once the lock was free the reader did not restore state.json from the backup: %s", restored.err)
				}
			} else if _, err := os.Stat(files.state); !os.IsNotExist(err) {
				t.Fatalf("once the lock was free the reader did not clear the unusable state.json: %v", err)
			}
		})
	}
}

func TestAReaderLeavesABrokenUsageJSONAloneWhileTheStatusLineHoldsTheLock(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	statusReadingFrom("kept", now, 20, float64(now+3600), 10, float64(now+3*86400))
	statusReadingFrom("later", now+1, 21, float64(now+3600), 10, float64(now+3*86400))
	broken := []byte(`{"five_hour":`)
	cliWrite(t, files.usage, broken)

	withFileLock(files.usageLock, func() {
		if used := numberOr(getMap(readUsageRepaired(files.guardDir), "five_hour"), "used", 0); used != 20 {
			t.Errorf("a reader that could not take usage.lock did not read the backup's 20 %%: %v", used)
		}
		if content, err := os.ReadFile(files.usage); err != nil || !bytes.Equal(content, broken) {
			t.Errorf("a reader that does not hold usage.lock rewrote or removed usage.json while the status line held the lock: %q, %v", content, err)
		}
	})

	if used := numberOr(getMap(readUsageRepaired(files.guardDir), "five_hour"), "used", 0); used != 20 {
		t.Fatalf("once the lock was free the reader did not recover the backup's 20 %%: %v", used)
	}
	if restored := readJSONStrict(files.usage); !restored.ok || restored.data == nil {
		t.Fatalf("once the lock was free the reader did not restore usage.json from the backup: %s", restored.err)
	}
}

func TestAUsageJSONThatCannotBeOpenedIsLeftAlone(t *testing.T) {
	sandboxFiles(t)
	now := nowSec()
	statusReadingFrom("kept", now, 20, float64(now+3600), 10, float64(now+3*86400))
	statusReadingFrom("later", now+1, 21, float64(now+3600), 10, float64(now+3*86400))
	if err := os.Remove(files.usage); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(files.usage, 0o700); err != nil {
		t.Fatal(err)
	}

	usage := readUsageRepaired(files.guardDir)

	if info, err := os.Stat(files.usage); err != nil || !info.IsDir() {
		t.Fatalf("a reader removed or replaced a usage.json it could not open: %v", err)
	}
	if used := numberOr(getMap(usage, "five_hour"), "used", 0); used != 20 {
		t.Fatalf("the reader did not read the backup's 20 %% in place of a usage.json it could not open: %v", used)
	}
}
