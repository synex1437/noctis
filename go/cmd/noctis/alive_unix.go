//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
	if _, err := os.Stat("/proc/self/comm"); err == nil {
		id := strconv.Itoa(pid)
		status, err := os.ReadFile(filepath.Join("/proc", id, "status"))
		if err != nil || !strings.Contains(string(status), "\nTgid:\t"+id+"\n") {
			return ""
		}
		comm, err := os.ReadFile(filepath.Join("/proc", id, "comm"))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(comm))
	}
	out, err := runWithTimeout(exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm="), 5*time.Second)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
