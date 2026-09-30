package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTheProjectsOwnCheckIsFoundFromItsBuildFiles(t *testing.T) {
	wrapper := "./gradlew test"
	if isWindows {
		wrapper = "gradlew.bat test"
	}
	scripts := func(test string) string { return `{"scripts": {"test": ` + test + `}}` }
	for _, c := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"nothing", nil, ""},
		{"make check", map[string]string{"Makefile": "build:\n\tgo build\ncheck: build\n\tgo test ./...\n"}, "make check"},
		{"make test", map[string]string{"Makefile": "test:\n\tpytest\n"}, "make test"},
		{"check before test", map[string]string{"GNUmakefile": "test:\n\ttrue\ncheck:\n\ttrue\n"}, "make check"},
		{"a variable is no rule", map[string]string{"Makefile": "test := unit\ncheck = all\n", "go.mod": "module x\n"}, "go test ./..."},
		{"npm", map[string]string{"package.json": scripts(`"vitest run"`)}, "npm test"},
		{"pnpm", map[string]string{"package.json": scripts(`"vitest run"`), "pnpm-lock.yaml": ""}, "pnpm test"},
		{"yarn", map[string]string{"package.json": scripts(`"jest"`), "yarn.lock": ""}, "yarn test"},
		{"bun", map[string]string{"package.json": scripts(`"bun test"`), "bun.lockb": ""}, "bun run test"},
		{"npm's placeholder", map[string]string{"package.json": scripts(`"echo \"Error: no test specified\" && exit 1"`)}, ""},
		{"make before npm", map[string]string{"Makefile": "check:\n\tnpm test\n", "package.json": scripts(`"jest"`)}, "make check"},
		{"go", map[string]string{"go.mod": "module x\n"}, "go test ./..."},
		{"cargo", map[string]string{"Cargo.toml": "[package]\n"}, "cargo test"},
		{"pytest with uv", map[string]string{"pyproject.toml": "[tool.pytest.ini_options]\n", "uv.lock": ""}, "uv run pytest"},
		{"pytest with poetry", map[string]string{"pyproject.toml": "[tool.pytest.ini_options]\n", "poetry.lock": ""}, "poetry run pytest"},
		{"pytest.ini", map[string]string{"pytest.ini": "[pytest]\n"}, "pytest"},
		{"setup.cfg", map[string]string{"setup.cfg": "[tool:pytest]\n"}, "pytest"},
		{"tox.ini", map[string]string{"tox.ini": "[pytest]\n"}, "pytest"},
		{"pyproject without pytest", map[string]string{"pyproject.toml": "[project]\nname = \"x\"\n"}, ""},
		{"deno", map[string]string{"deno.json": "{}"}, "deno test"},
		{"gradle wrapper", map[string]string{"gradlew": "", "build.gradle": ""}, wrapper},
		{"gradle", map[string]string{"build.gradle.kts": ""}, "gradle test"},
		{"maven", map[string]string{"pom.xml": "<project/>"}, "mvn -q test"},
		{"mix", map[string]string{"mix.exs": ""}, "mix test"},
		{"dotnet solution", map[string]string{"App.sln": ""}, "dotnet test"},
		{"dotnet project", map[string]string{"App.Tests.csproj": ""}, "dotnet test"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range c.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := projectCheckCommand(dir); got != c.want {
				t.Errorf("projectCheckCommand is %q, want %q", got, c.want)
			}
		})
	}
}
