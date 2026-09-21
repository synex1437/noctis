//go:build !windows

package main

import "os"

func openShared(path string) (*os.File, error) {
	return os.Open(path)
}

func readFileShared(path string) ([]byte, error) {
	return os.ReadFile(path)
}
