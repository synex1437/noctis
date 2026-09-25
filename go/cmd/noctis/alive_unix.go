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
		return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
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

func processStarted(pid int) string {
	if !validPid(pid) {
		return ""
	}
	if _, err := os.Stat("/proc/self/stat"); err == nil {
		stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
		if err != nil {
			return ""
		}
		end := bytes.LastIndexByte(stat, ')')
		if end < 0 {
			return ""
		}
		if fields := strings.Fields(string(stat[end+1:])); len(fields) > 19 {
			return fields[19]
		}
		return ""
	}
	command := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=")
	command.Env = append(os.Environ(), "LC_ALL=C", "TZ=UTC")
	out, err := runWithTimeout(command, 5*time.Second)
	if err != nil {
		return ""
	}
	return strings.Join(strings.Fields(string(out)), " ")
}
