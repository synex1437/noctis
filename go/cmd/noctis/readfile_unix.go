//go:build !windows

package main

import "os"

func readFileShared(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// openAppend opens path for appending and creates it when it is missing.
func openAppend(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}
