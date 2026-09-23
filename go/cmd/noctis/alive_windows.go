//go:build windows

package main

import (
	"math"
	"syscall"
	"unsafe"
)

const processQueryLimitedInformation = 0x1000

func validPid(pid int) bool {
	return pid > 0 && uint64(pid) <= math.MaxUint32
}

func processAlive(pid int) bool {
	if !validPid(pid) {
		return false
	}
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return err == syscall.ERROR_ACCESS_DENIED
	}
	defer syscall.CloseHandle(handle)
	var code uint32
	if err := syscall.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == 259
}

func processName(pid int) string {
	if !validPid(pid) {
		return ""
	}
	snapshot, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return ""
	}
	defer syscall.CloseHandle(snapshot)
	var entry syscall.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = syscall.Process32First(snapshot, &entry); err == nil; err = syscall.Process32Next(snapshot, &entry) {
		if entry.ProcessID == uint32(pid) {
			return syscall.UTF16ToString(entry.ExeFile[:])
		}
	}
	return ""
}
