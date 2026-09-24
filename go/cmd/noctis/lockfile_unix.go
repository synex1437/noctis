//go:build !windows

package main

import (
	"errors"
	"os"
	"strings"
	"syscall"
	"time"
)

func holdLock(handle *os.File) {
	_ = syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func removeStaleLock(lockFile string) error {
	handle, err := os.Open(lockFile)
	if err != nil {
		return err
	}
	defer handle.Close()
	opened, err := handle.Stat()
	if err != nil {
		return err
	}
	content := make([]byte, 64)
	read, _ := handle.Read(content)
	age := time.Since(opened.ModTime())
	owner := strings.TrimSpace(string(content[:read]))
	if !holderStale(owner, age) {
		return errLockLive
	}
	if errors.Is(syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB), syscall.EWOULDBLOCK) && !holderHung(owner, age) {
		return errLockLive
	}
	current, err := os.Stat(lockFile)
	if err != nil {
		return err
	}
	if !os.SameFile(opened, current) {
		return errLockLive
	}
	return os.Remove(lockFile)
}

func holderHung(owner string, age time.Duration) bool {
	pid, parsed := lockOwnerPid(owner)
	return parsed && age > lockLiveHolderMs*time.Millisecond && processAlive(pid)
}

func releaseLock(handle *os.File, lockFile string) {
	defer handle.Close()
	mine, err := handle.Stat()
	if err != nil {
		return
	}
	current, err := os.Stat(lockFile)
	if err != nil || !os.SameFile(mine, current) {
		return
	}
	if err := os.Remove(lockFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		warn("lock release failed: %v", err)
	}
}
