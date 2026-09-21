//go:build windows

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	fileRenameInfoExClass     = 22
	fileRenameReplaceIfExists = 0x00000001
	fileRenamePosixSemantics  = 0x00000002
	accessDelete              = 0x00010000
)

type fileRenameInfoEx struct {
	Flags          uint32
	RootDirectory  syscall.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procSetFileInformationByHandle = kernel32.NewProc("SetFileInformationByHandle")
	renameInfoHeaderSize           = unsafe.Offsetof(fileRenameInfoEx{}.FileName)
)

func renameAtomic(from, to string) error {
	if err := renamePosix(from, to); err == nil {
		return nil
	}
	return os.Rename(from, to)
}

func renamePosix(from, to string) error {
	if err := procSetFileInformationByHandle.Find(); err != nil {
		return err
	}
	source, err := syscall.UTF16PtrFromString(extendedPath(from))
	if err != nil {
		return err
	}
	handle, err := syscall.CreateFile(source, accessDelete|syscall.SYNCHRONIZE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(handle)

	absolute, err := filepath.Abs(to)
	if err != nil {
		return err
	}
	target, err := syscall.UTF16FromString(extendedPath(absolute))
	if err != nil {
		return err
	}
	if len(target) < 2 {
		return syscall.EINVAL
	}

	size := renameInfoHeaderSize + uintptr(len(target))*2
	buffer := make([]byte, size)
	info := (*fileRenameInfoEx)(unsafe.Pointer(&buffer[0]))
	info.Flags = fileRenameReplaceIfExists | fileRenamePosixSemantics
	info.RootDirectory = 0
	info.FileNameLength = uint32((len(target) - 1) * 2)
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&buffer[renameInfoHeaderSize])), len(target)), target)

	done, _, callErr := syscall.SyscallN(procSetFileInformationByHandle.Addr(),
		uintptr(handle), fileRenameInfoExClass, uintptr(unsafe.Pointer(&buffer[0])), uintptr(size))
	runtime.KeepAlive(buffer)
	if done == 0 {
		if callErr != 0 {
			return callErr
		}
		return syscall.EINVAL
	}
	return nil
}
