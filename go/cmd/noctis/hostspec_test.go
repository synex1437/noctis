package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCodexAndDroidHearBackTheEventNameTheySent(t *testing.T) {
	for _, host := range []string{"codex", "droid"} {
		batch := translateOutput(host, "PostToolUse", object{
			"systemMessage":      "waited, continuing",
			"hookSpecificOutput": object{"hookEventName": "PostToolBatch", "additionalContext": "re-read the files"},
		})
		specific := getMap(batch, "hookSpecificOutput")
		if getString(specific, "hookEventName") != "PostToolUse" || getString(specific, "additionalContext") != "re-read the files" || getString(batch, "systemMessage") != "waited, continuing" {
			t.Errorf("%s: a tool-batch answer must carry the event name the host sent and keep the rest, got %s", host, marshalCompact(batch))
		}
		spawn := translateOutput(host, "PreToolUse", object{"hookSpecificOutput": object{"hookEventName": "PreToolUse", "additionalContext": "re-read the files"}})
		if getString(getMap(spawn, "hookSpecificOutput"), "hookEventName") != "PreToolUse" {
			t.Errorf("%s: an event the host names like Claude Code keeps its name, got %s", host, marshalCompact(spawn))
		}
	}
}

func TestAntigravityInjectsTheModelNoteNextToTheNotice(t *testing.T) {
	injected := func(event string, out object) string {
		steps := getList(translateOutput("antigravity", event, out), "injectSteps")
		if len(steps) != 1 {
			return ""
		}
		step, _ := steps[0].(object)
		return getString(step, "ephemeralMessage")
	}
	for _, event := range []string{"PreInvocation", "PostInvocation"} {
		both := injected(event, object{
			"systemMessage":      "waited, continuing",
			"hookSpecificOutput": object{"hookEventName": "PostToolBatch", "additionalContext": "the tree changed"},
		})
		if !strings.Contains(both, "waited, continuing") || !strings.Contains(both, "the tree changed") {
			t.Errorf("%s: the injected step must carry the notice and the note for the model, got %q", event, both)
		}
		if only := injected(event, object{"hookSpecificOutput": object{"hookEventName": "PostToolBatch", "additionalContext": "the tree changed"}}); only != "the tree changed" {
			t.Errorf("%s: a note without a notice must still reach the model, got %q", event, only)
		}
		if quiet := translateOutput("antigravity", event, object{}); len(quiet) != 0 {
			t.Errorf("%s: nothing to say must inject nothing, got %s", event, marshalCompact(quiet))
		}
	}
}

func TestEveryHostCapabilityIsConsulted(t *testing.T) {

	descriptive := map[string]bool{
		"id": true, "display": true, "exe": true, "homeEnv": true, "homeDefault": true,
	}

	var capabilities []string
	for _, field := range reflect.VisibleFields(reflect.TypeOf(hostSpec{})) {
		if !descriptive[field.Name] {
			capabilities = append(capabilities, field.Name)
		}
	}
	if len(capabilities) == 0 {
		t.Fatal("no capability fields found on hostSpec; has the struct been renamed?")
	}

	read := readFieldsOfHostSpec(t)
	for _, name := range capabilities {
		if !read[name] {
			t.Errorf("hostSpec.%s is declared and set but nothing ever reads it: either gate the "+
				"behaviour it describes on it, or delete the field", name)
		}
	}
}

func readFieldsOfHostSpec(t *testing.T) map[string]bool {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", dir, err)
	}

	read := map[string]bool{}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			isHostFile := filepath.Base(name) == "host.go"
			ast.Inspect(file, func(node ast.Node) bool {

				if literal, ok := node.(*ast.CompositeLit); ok && isHostFile {
					for _, element := range literal.Elts {
						if pair, ok := element.(*ast.KeyValueExpr); ok {
							ast.Inspect(pair.Value, func(inner ast.Node) bool {
								if selector, ok := inner.(*ast.SelectorExpr); ok {
									read[selector.Sel.Name] = true
								}
								return true
							})
						}
					}
					return false
				}
				if selector, ok := node.(*ast.SelectorExpr); ok {
					read[selector.Sel.Name] = true
				}
				return true
			})
		}
	}
	return read
}
