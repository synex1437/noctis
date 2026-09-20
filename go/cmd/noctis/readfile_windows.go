//go:build windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func extendedPath(path string) string {
	if len(path) < 248 || strings.HasPrefix(path, `\\?\`) || strings.HasPrefix(path, `\\.\`) {
		return path
	}
	full, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if strings.HasPrefix(full, `\\`) {
		return `\\?\UNC\` + full[2:]
	}
	if len(full) > 1 && full[1] == ':' {
		return `\\?\` + full
	}
	return path
}

func readFileShared(path string) ([]byte, error) {
	wide, err := syscall.UTF16PtrFromString(extendedPath(path))
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	handle, err := syscall.CreateFile(wide, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(handle), path)
	defer file.Close()
	var buffer bytes.Buffer
	if info, statErr := file.Stat(); statErr == nil {
		if size := info.Size(); size > 0 && size < 1<<31 {
			buffer.Grow(int(size) + 1)
		}
	}
	if _, err := buffer.ReadFrom(file); err != nil {
		return nil, &os.PathError{Op: "read", Path: path, Err: err}
	}
	return buffer.Bytes(), nil
}
