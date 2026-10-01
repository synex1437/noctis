package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
)

const (
	worktreeContentBytes     = 32 << 20
	worktreeTreesKept        = 16
	stopsLeftAfterEscalation = 2
)

type worktreeDigest struct {
	hash.Hash
	cwd     string
	entries int
	bytes   int64
}

func worktreeFingerprint(cwd, leaveOut string) string {
	raw, ok := gitStatusUncached(cwd)
	if !ok {
		return ""
	}
	digest := &worktreeDigest{Hash: sha1.New(), cwd: cwd, entries: treeStatLimit, bytes: worktreeContentBytes}
	io.WriteString(digest, gitHead(cwd))
	for _, line := range strings.Split(raw, "\n") {
		names := statusLinePaths(line)
		if len(names) == 0 || sameFileAs(cwd, names[len(names)-1], leaveOut) {
			continue
		}
		fmt.Fprintf(digest, "\x00%s", line)
		for _, name := range names {
			digest.addPath(name)
		}
	}
	if digest.entries < 0 {
		return ""
	}
	return hex.EncodeToString(digest.Sum(nil)[:8])
}

func sameFileAs(cwd, name, target string) bool {
	if target == "" || !strings.EqualFold(filepath.Base(filepath.FromSlash(name)), filepath.Base(target)) {
		return false
	}
	here, err := os.Stat(filepath.Join(cwd, filepath.FromSlash(name)))
	if err != nil {
		return false
	}
	there, err := os.Stat(target)
	return err == nil && os.SameFile(here, there)
}

func (digest *worktreeDigest) addPath(name string) {
	path := filepath.Join(digest.cwd, filepath.FromSlash(name))
	info, err := os.Lstat(path)
	switch {
	case err != nil:
		io.WriteString(digest, "\x00gone")
	case info.IsDir():
		digest.addFolder(path)
	default:
		digest.addEntry(name, path, info)
	}
}

func (digest *worktreeDigest) addFolder(folder string) {
	_ = filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil || path == folder:
			return nil
		case digest.entries < 0:
			return filepath.SkipAll
		case entry.IsDir() && entry.Name() == ".git":
			return filepath.SkipDir
		case entry.IsDir():
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		relative, _ := filepath.Rel(digest.cwd, path)
		digest.addEntry(filepath.ToSlash(relative), path, info)
		return nil
	})
}

func (digest *worktreeDigest) addEntry(name, path string, info fs.FileInfo) {
	if digest.entries--; digest.entries < 0 {
		return
	}
	fmt.Fprintf(digest, "\x00%s\x00%d\x00%d\x00", name, info.Size(), info.Mode())
	if info.Mode()&fs.ModeSymlink != 0 {
		target, _ := os.Readlink(path)
		io.WriteString(digest, target)
		return
	}
	if info.Mode().IsRegular() && info.Size() <= digest.bytes && digest.addContent(path, info.Size()) {
		digest.bytes -= info.Size()
		return
	}
	fmt.Fprintf(digest, "%d", info.ModTime().UnixNano())
}

func (digest *worktreeDigest) addContent(path string, size int64) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	_, err = io.CopyN(digest, file, size)
	return err == nil
}

func queueIdleLimit(cfg object) float64 {
	limit := math.Max(1, numberOr(section(cfg, "queue"), "maxIdleContinues", 3))
	if blockCap := stopBlockCap(); blockCap > 0 {
		limit = math.Max(1, math.Min(limit, math.Floor(blockCap)))
	}
	return limit
}

func treeProgress(guard object, cwd, queuePath string, allowance float64) bool {
	tree := worktreeFingerprint(cwd, queuePath)
	if tree == "" {
		return false
	}
	seen := getList(guard, "trees")
	for _, earlier := range seen {
		if earlier == tree {
			return false
		}
	}
	if len(seen) >= worktreeTreesKept {
		seen = seen[len(seen)-worktreeTreesKept+1:]
	}
	guard["trees"] = append(seen, tree)
	moves := numberOr(guard, "treeMoves", 0)
	if len(seen) == 0 || moves >= allowance {
		return false
	}
	guard["treeMoves"] = moves + 1
	return true
}

func forgetTreeProgress(guard object) {
	delete(guard, "trees")
	delete(guard, "treeMoves")
}
