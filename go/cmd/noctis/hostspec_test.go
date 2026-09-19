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
