//go:build !windows

package main

import "os"

func renameAtomic(from, to string) error {
	return os.Rename(from, to)
}
