//go:build !windows

package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// effectiveUser is the user noctis runs as. It is a variable so that a test can have the files it writes
// belong to someone else.
var effectiveUser = os.Geteuid

// ownsLocalSettingsRoot tells whether root, its .git and its .claude, where it has one, belong to the user
// noctis runs as, as Claude Code 2.1.289 asks before it keeps a project's local settings at the root of its
// work tree.
func ownsLocalSettingsRoot(root string) bool {
	user := effectiveUser()
	owns := func(info fs.FileInfo) bool {
		stat, known := info.Sys().(*syscall.Stat_t)
		return known && user >= 0 && uint64(stat.Uid) == uint64(user)
	}
	if info, err := os.Stat(root); err != nil || !owns(info) {
		return false
	}
	if info, err := os.Lstat(filepath.Join(root, ".git")); err != nil || !owns(info) {
		return false
	}
	info, err := os.Lstat(filepath.Join(root, ".claude"))
	return errors.Is(err, fs.ErrNotExist) || err == nil && owns(info)
}
