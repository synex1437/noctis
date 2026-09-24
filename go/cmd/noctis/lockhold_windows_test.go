//go:build windows

package main

import (
	"os"
	"testing"
)

func holdForTest(*testing.T, *os.File) {}

func letGoForTest(handle *os.File) {
	_ = handle.Close()
	_ = os.Remove(handle.Name())
}
