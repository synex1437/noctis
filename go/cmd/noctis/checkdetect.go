package main

import (
	"os"
	"path/filepath"
	"strings"
)

// makeCheckRule matches a Makefile rule named check or test, not a variable of that name.
var makeCheckRule = lazyRegexp(`(?m)^(check|test)[ \t]*:([^=]|$)`)

// projectCheckCommand is the command the project in dir most likely checks itself with, going by its
// build files, or "". noctis only names it: a queue's check is a command the user wrote into the queue
// file and trusted, or set as queue.verifyCommand, never this guess.
func projectCheckCommand(dir string) string {
	read := func(name string) string {
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || len(content) > 1<<20 {
			return ""
		}
		return string(content)
	}
	has := func(names ...string) bool {
		for _, name := range names {
			if statSafe(filepath.Join(dir, name)) != nil {
				return true
			}
		}
		return false
	}
	for _, name := range []string{"GNUmakefile", "makefile", "Makefile"} {
		rules := map[string]bool{}
		for _, match := range makeCheckRule.FindAllStringSubmatch(read(name), -1) {
			rules[match[1]] = true
		}
		switch {
		case rules["check"]:
			return "make check"
		case rules["test"]:
			return "make test"
		}
	}
	if test := getString(getMap(readJSON(filepath.Join(dir, "package.json")), "scripts"), "test"); strings.TrimSpace(test) != "" && !strings.Contains(test, "no test specified") {
		switch {
		case has("pnpm-lock.yaml"):
			return "pnpm test"
		case has("yarn.lock"):
			return "yarn test"
		case has("bun.lock", "bun.lockb"):
			return "bun run test"
		}
		return "npm test"
	}
	switch {
	case has("go.mod"):
		return "go test ./..."
	case has("Cargo.toml"):
		return "cargo test"
	case has("pytest.ini", "conftest.py") || strings.Contains(read("pyproject.toml"), "[tool.pytest") || strings.Contains(read("setup.cfg"), "[tool:pytest]") || strings.Contains(read("tox.ini"), "[pytest]"):
		switch {
		case has("uv.lock"):
			return "uv run pytest"
		case has("poetry.lock"):
			return "poetry run pytest"
		}
		return "pytest"
	case has("deno.json", "deno.jsonc"):
		return "deno test"
	case has("gradlew") && isWindows:
		return "gradlew.bat test"
	case has("gradlew"):
		return "./gradlew test"
	case has("build.gradle", "build.gradle.kts"):
		return "gradle test"
	case has("pom.xml"):
		return "mvn -q test"
	case has("mix.exs"):
		return "mix test"
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".sln", ".csproj", ".fsproj":
			return "dotnet test"
		}
	}
	return ""
}
