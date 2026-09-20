//go:build !windows

package main

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}

func processName(pid int) string {
	out, err := runWithTimeout(exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm="), 5*time.Second)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
