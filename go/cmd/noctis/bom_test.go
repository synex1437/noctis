package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAByteOrderMarkNeitherBreaksAJSONFileNorHidesTheFirstQueueItem(t *testing.T) {
	dir := sandboxFiles(t)
	settings := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settings, []byte("\xef\xbb\xbf{\"model\": \"opus\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	read := readJSONStrict(settings)
	if !read.ok || getString(read.data, "model") != "opus" {
		t.Fatalf("a settings.json saved with a byte order mark (Windows Notepad does this) was read as broken: %+v", read)
	}
	if err := os.WriteFile(files.config, []byte("\xef\xbb\xbf{\"thresholds\": {\"session5h\": 80}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg := loadConfig(); getString(cfg, "configError") != "" || numberOr(section(cfg, "thresholds"), "session5h", 0) != 80 {
		t.Fatalf("a config.json with a byte order mark was not read: error %q", getString(cfg, "configError"))
	}
	queue := filepath.Join(dir, "TASKS.md")
	if err := os.WriteFile(queue, []byte("\xef\xbb\xbf- [ ] write the parser tests\n- [ ] document the flags\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if view := queueSnapshot(queue); view.total != 2 || len(view.items) == 0 || view.items[0] != "write the parser tests" {
		t.Fatalf("a TASKS.md with a byte order mark hid its first item: %+v", view)
	}
}
