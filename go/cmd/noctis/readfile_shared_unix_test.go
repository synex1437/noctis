//go:build !windows

package main

import "os"

func openShared(path string) (*os.File, error) {
	return os.Open(path)
}
