package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnInstallCopiesTheHooksModuleItsHooksJSONNames(t *testing.T) {
	box := newCLIBox(t)
	module := []byte("export function register(on, options) {}\n")
	cliWrite(t, filepath.Join(box.root, "hooks", "lean.js"), module)
	hooks := readJSON(filepath.Join(box.root, "hooks", "hooks.json"))
	hooks["modules"] = []any{"./lean.js"}
	cliWrite(t, filepath.Join(box.root, "hooks", "hooks.json"), marshalPretty(hooks))

	run := box.run(t, "install", "--source", box.root, "--config-dir", box.account, "--host", "claude", "--profile", "balanced", "--permissions", "keep")

	box.configured(t, run, box.account, "balanced")
	installed := filepath.Join(box.account, "skills", pluginName, "hooks")
	copied, err := os.ReadFile(filepath.Join(installed, "lean.js"))
	if err != nil || !bytes.Equal(copied, module) {
		t.Fatalf("the installed hooks.json names ./lean.js but the copy has %q (%v):\n%s", copied, err, run)
	}
	if names := getList(readJSON(filepath.Join(installed, "hooks.json")), "modules"); len(names) != 1 || names[0] != "./lean.js" {
		t.Fatalf("the installed hooks.json lost its modules entry: %v", names)
	}
}

func TestAnInstallNeverCopiesAModuleNamedOutsideThePlugin(t *testing.T) {
	box := newCLIBox(t)
	outside := filepath.Join(filepath.Dir(box.root), "outside.js")
	cliWrite(t, outside, []byte("export function register(on) {}\n"))
	hooks := readJSON(filepath.Join(box.root, "hooks", "hooks.json"))
	hooks["modules"] = []any{"../../outside.js"}
	cliWrite(t, filepath.Join(box.root, "hooks", "hooks.json"), marshalPretty(hooks))

	run := box.run(t, "install", "--source", box.root, "--config-dir", box.account, "--host", "claude", "--profile", "balanced", "--permissions", "keep")

	box.configured(t, run, box.account, "balanced")
	err := filepath.WalkDir(box.account, func(path string, entry os.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(path, "outside.js") {
			t.Fatalf("a module named outside the plugin was copied to %s:\n%s", path, run)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
