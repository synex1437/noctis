//go:build windows

package main

import (
	"errors"
	"os"
	"time"
)

func holdLock(*os.File) {}

func removeStaleLock(lockFile string) error {
	owner, age, present := lockHolder(lockFile)
	if !present {
		return os.ErrNotExist
	}
	if !holderStale(owner, age) {
		return errLockLive
	}
	if again, _, stillThere := lockHolder(lockFile); stillThere && again != owner {
		return errLockLive
	}
	return os.Remove(lockFile)
}

func releaseLock(handle *os.File, lockFile string) {
	handle.Close()
	for attempt := 0; ; attempt++ {
		err := os.Remove(lockFile)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return
		}
		if attempt == 5 {
			warn("lock release failed: %v", err)
			return
		}
		time.Sleep(time.Duration(10+attempt*15) * time.Millisecond)
	}
}
