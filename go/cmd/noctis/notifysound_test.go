package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheWindowsNotificationLeavesItsSoundToTheToast(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "notify.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	if strings.Contains(strings.ToLower(text), "beep") {
		t.Fatal("notify.ps1 beeps on its own; the beep ignores Focus Assist and Do not disturb, which silence the toast")
	}
	if !strings.Contains(text, "ToastNotificationManager") || !strings.Contains(text, "MessageBox") {
		t.Fatal("notify.ps1 no longer shows a toast, or no message box when the toast cannot be shown")
	}
}
