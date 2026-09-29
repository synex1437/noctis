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

func openShared(path string) (*os.File, error) {
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
	return os.NewFile(uintptr(handle), path), nil
}

func readFileShared(path string) ([]byte, error) {
	file, err := openShared(path)
	if err != nil {
		return nil, err
	}
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

// fileWriteExtendedAttributes is FILE_WRITE_EA, which syscall does not export.
const fileWriteExtendedAttributes = 0x00000010

// openAppend opens path for appending and creates it when it is missing, with the rights os.OpenFile
// asks for with O_APPEND, but it also lets another process rename or delete the file while it is
// open. os.OpenFile does not: its handle keeps a process that rotates the log from moving it aside,
// and its open fails with a sharing violation while that process holds the file to move it.
func openAppend(path string) (*os.File, error) {
	wide, err := syscall.UTF16PtrFromString(extendedPath(path))
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	handle, err := syscall.CreateFile(wide,
		syscall.FILE_APPEND_DATA|syscall.FILE_WRITE_ATTRIBUTES|fileWriteExtendedAttributes|syscall.STANDARD_RIGHTS_WRITE|syscall.SYNCHRONIZE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}
