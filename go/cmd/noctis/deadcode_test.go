package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

type sourceIndex struct {
	files map[string]*ast.File
	fset  *token.FileSet
}

func indexPackage(t *testing.T) sourceIndex {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	fset := token.NewFileSet()

	pkgs, err := parser.ParseDir(fset, dir, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	files := map[string]*ast.File{}
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			files[name] = file
		}
	}
	if len(files) < 10 {
		t.Fatalf("only %d files indexed; the package layout must have changed", len(files))
	}
	return sourceIndex{files: files, fset: fset}
}

func buildWorld(file *ast.File) string {
	for _, group := range file.Comments {
		for _, comment := range group.List {
			constraint, found := strings.CutPrefix(comment.Text, "//go:build ")
			if !found {
				continue
			}
			switch strings.TrimSpace(constraint) {
			case "windows":
				return "windows"
			case "!windows":
				return "unix"
			}
		}
	}
	return ""
}

func (index sourceIndex) identifierUses() (production, tests map[string]int) {
	return index.identifierUsesIn("")
}

func (index sourceIndex) identifierUsesIn(world string) (production, tests map[string]int) {
	production, tests = map[string]int{}, map[string]int{}
	for name, file := range index.files {
		if only := buildWorld(file); only != "" && world != "" && only != world {
			continue
		}
		counter := production
		if strings.HasSuffix(name, "_test.go") {
			counter = tests
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.FuncDecl:

				if typed.Body != nil {
					ast.Inspect(typed.Body, func(inner ast.Node) bool {
						collectIdentifiers(inner, counter)
						return true
					})
				}
				for _, param := range typed.Type.Params.List {
					collectIdentifiers(param.Type, counter)
				}
				return false
			case *ast.Ident:
				counter[typed.Name]++
			case *ast.SelectorExpr:
				counter[typed.Sel.Name]++
			}
			return true
		})
	}
	return production, tests
}

func collectIdentifiers(node ast.Node, into map[string]int) {
	switch typed := node.(type) {
	case *ast.Ident:
		into[typed.Name]++
	case *ast.SelectorExpr:
		into[typed.Sel.Name]++
	}
}

func TestNoFunctionIsDeadOrTestOnly(t *testing.T) {
	index := indexPackage(t)
	reserved := map[string]bool{"main": true, "init": true, "TestMain": true}

	for _, world := range []string{"unix", "windows"} {
		production, tests := index.identifierUsesIn(world)
		for name, file := range index.files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			if only := buildWorld(file); only != "" && only != world {
				continue
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || reserved[fn.Name.Name] {
					continue
				}
				label := fn.Name.Name
				switch {
				case production[label] > 0:

				case tests[label] > 0:
					t.Errorf("on %s, %s is called only from tests: it belongs in a _test.go file, or the "+
						"production path that should call it is missing", world, label)
				default:
					t.Errorf("on %s, %s has no callers at all", world, label)
				}
			}
		}
	}
}

func (index sourceIndex) fieldReads() map[string]int {
	reads := map[string]int{}
	for _, file := range index.files {
		ast.Inspect(file, func(node ast.Node) bool {
			if literal, ok := node.(*ast.CompositeLit); ok {
				for _, element := range literal.Elts {
					pair, ok := element.(*ast.KeyValueExpr)
					if !ok {
						ast.Inspect(element, func(inner ast.Node) bool {
							if selector, ok := inner.(*ast.SelectorExpr); ok {
								reads[selector.Sel.Name]++
							}
							return true
						})
						continue
					}

					ast.Inspect(pair.Value, func(inner ast.Node) bool {
						if selector, ok := inner.(*ast.SelectorExpr); ok {
							reads[selector.Sel.Name]++
						}
						return true
					})
				}
				return false
			}
			if selector, ok := node.(*ast.SelectorExpr); ok {
				reads[selector.Sel.Name]++
			}
			return true
		})
	}
	return reads
}

func TestNoStructFieldIsWrittenAndNeverRead(t *testing.T) {
	index := indexPackage(t)
	reads := index.fieldReads()

	for name, file := range index.files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			spec, ok := node.(*ast.TypeSpec)
			if !ok {
				return true
			}
			structType, ok := spec.Type.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range structType.Fields.List {

				if field.Tag != nil {
					continue
				}
				for _, fieldName := range field.Names {
					if label := fieldName.Name; reads[label] == 0 {
						t.Errorf("%s.%s is set but never read: it reads as a value something "+
							"depends on, and nothing does", spec.Name.Name, label)
					}
				}
			}
			return true
		})
	}
}

func TestNoParameterIsPassedAndDiscarded(t *testing.T) {
	index := indexPackage(t)

	for name, file := range index.files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Type.Params == nil {
				continue
			}
			used := map[string]int{}
			ast.Inspect(fn.Body, func(inner ast.Node) bool {
				collectIdentifiers(inner, used)
				return true
			})
			for _, param := range fn.Type.Params.List {
				for _, paramName := range param.Names {
					label := paramName.Name
					if label == "_" || used[label] > 0 {
						continue
					}
					t.Errorf("%s takes %q and never uses it: every caller passing it believes it "+
						"is steering something. Drop it, or rename it to _ if the signature is fixed",
						fn.Name.Name, label)
				}
			}
		}
	}
}
