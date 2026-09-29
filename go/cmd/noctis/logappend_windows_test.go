//go:build windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// holdForMove opens path the way renamePosix does before it moves a file, and returns the handle.
func holdForMove(t *testing.T, path string) syscall.Handle {
	t.Helper()
	wide, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(wide, accessDelete|syscall.SYNCHRONIZE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	return handle
}

func TestALogAWriterHoldsOpenCanStillBeMovedAside(t *testing.T) {
	file := filepath.Join(t.TempDir(), "guard.log")
	writer, err := openAppend(file)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.WriteString("before\n"); err != nil {
		t.Fatal(err)
	}
	if err := renameAtomic(file, file+".1"); err != nil {
		t.Fatalf("the log could not be moved aside while a writer had it open: %v", err)
	}
	if _, err := writer.WriteString("after\n"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if moved, _ := os.ReadFile(file + ".1"); string(moved) != "before\nafter\n" {
		t.Fatalf("the moved log holds %q; want both lines, in order", moved)
	}
}

func TestAWriterOpensTheLogWhileAnotherProcessMovesIt(t *testing.T) {
	file := filepath.Join(t.TempDir(), "guard.log")
	if err := os.WriteFile(file, []byte("history\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	held := holdForMove(t, file)
	defer syscall.CloseHandle(held)
	if plain, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		plain.Close()
		t.Log("os.OpenFile also opened the file held for a move on this Windows")
	}
	writer, err := openAppend(file)
	if err != nil {
		t.Fatalf("a writer could not open the log while another process held it to move it: %v", err)
	}
	defer writer.Close()
	if _, err := writer.WriteString("line\n"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if content, _ := os.ReadFile(file); string(content) != "history\nline\n" {
		t.Fatalf("the log holds %q; want the line after its history", content)
	}
}
