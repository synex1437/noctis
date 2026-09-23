//go:build !windows

package main

import (
	"bytes"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func validPid(pid int) bool {
	return pid > 0 && pid <= math.MaxInt32
}

func processAlive(pid int) bool {
	if !validPid(pid) {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	return !processExited(pid)
}

func processExited(pid int) bool {
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 || end+2 >= len(stat) {
		return false
	}
	state := stat[end+2]
	return state == 'Z' || state == 'X'
}

func processName(pid int) string {
	if !validPid(pid) {
		return ""
	}
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
