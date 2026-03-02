package detect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile writes content to a file inside dir.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("writeFile %s: %v", name, err)
	}
}

// --- Analyze: Go ---

func TestAnalyze_Go_Minimal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/myapp\n\ngo 1.22\n")

	info := Analyze(dir)

	if info.Language != "Go" {
		t.Errorf("Language = %q, want %q", info.Language, "Go")
	}
	if info.Module != "example.com/myapp" {
		t.Errorf("Module = %q, want %q", info.Module, "example.com/myapp")
	}
	if info.Version != "1.22" {
		t.Errorf("Version = %q, want %q", info.Version, "1.22")
	}
	if info.ProjectName != "myapp" {
		t.Errorf("ProjectName = %q, want %q", info.ProjectName, "myapp")
	}
	if info.BuildCmd != "go build ./..." {
		t.Errorf("BuildCmd = %q, want %q", info.BuildCmd, "go build ./...")
	}
}

func TestAnalyze_Go_WithMakefile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/proj\n\ngo 1.21\n")
	writeFile(t, dir, "Makefile", "build:\n\tgo build ./...\n\nlint:\n\tgo vet ./...\n\ntest:\n\tgo test ./...\n")

	info := Analyze(dir)

	if info.BuildCmd != "make build" {
		t.Errorf("BuildCmd = %q, want %q", info.BuildCmd, "make build")
	}
	if info.LintCmd != "make lint" {
		t.Errorf("LintCmd = %q, want %q", info.LintCmd, "make lint")
	}
}

func TestAnalyze_Go_WithTestFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/proj\n\ngo 1.22\n")
	writeFile(t, dir, "main_test.go", "package main\n")

	info := Analyze(dir)

	if info.TestCmd != "go test ./..." {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "go test ./...")
	}
}

func TestAnalyze_Go_WithTestFilesAndMaketest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/proj\n\ngo 1.22\n")
	writeFile(t, dir, "main_test.go", "package main\n")
	writeFile(t, dir, "Makefile", "test:\n\tgo test ./...\n")

	info := Analyze(dir)

	if info.TestCmd != "make test" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "make test")
	}
}

func TestAnalyze_Go_NoTestFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/proj\n\ngo 1.22\n")

	info := Analyze(dir)

	if info.TestCmd != "" {
		t.Errorf("TestCmd = %q, want empty when no test files exist", info.TestCmd)
	}
}

func TestAnalyze_Go_WithGolangCI(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/proj\n\ngo 1.22\n")
	writeFile(t, dir, ".golangci.yml", "linters:\n  enable-all: true\n")

	info := Analyze(dir)

	if info.LintCmd != "golangci-lint run" {
		t.Errorf("LintCmd = %q, want %q", info.LintCmd, "golangci-lint run")
	}
}

// --- Analyze: Node.js ---

func TestAnalyze_Node(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{
		"name": "my-app",
		"engines": {"node": "18.x"},
		"scripts": {
			"build": "webpack",
			"test": "jest",
			"lint": "eslint ."
		}
	}`)

	info := Analyze(dir)

	if info.Language != "Node.js" {
		t.Errorf("Language = %q, want %q", info.Language, "Node.js")
	}
	if info.ProjectName != "my-app" {
		t.Errorf("ProjectName = %q, want %q", info.ProjectName, "my-app")
	}
	if info.Version != "18.x" {
		t.Errorf("Version = %q, want %q", info.Version, "18.x")
	}
	if info.BuildCmd != "npm run build" {
		t.Errorf("BuildCmd = %q, want %q", info.BuildCmd, "npm run build")
	}
	if info.TestCmd != "npm test" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "npm test")
	}
	if info.LintCmd != "npm run lint" {
		t.Errorf("LintCmd = %q, want %q", info.LintCmd, "npm run lint")
	}
}

func TestAnalyze_Node_NvmrcFallback(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name": "proj"}`)
	writeFile(t, dir, ".nvmrc", "20.5.0\n")

	info := Analyze(dir)

	if info.Version != "20.5.0" {
		t.Errorf("Version = %q, want %q", info.Version, "20.5.0")
	}
}

func TestAnalyze_Node_ESLintConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name": "proj"}`)
	writeFile(t, dir, ".eslintrc.json", `{}`)

	info := Analyze(dir)

	if info.LintCmd != "npx eslint ." {
		t.Errorf("LintCmd = %q, want %q", info.LintCmd, "npx eslint .")
	}
}

func TestAnalyze_Node_BiomeConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name": "proj"}`)
	writeFile(t, dir, "biome.json", `{}`)

	info := Analyze(dir)

	if info.LintCmd != "npx biome check ." {
		t.Errorf("LintCmd = %q, want %q", info.LintCmd, "npx biome check .")
	}
}

func TestAnalyze_Node_PrettierConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name": "proj"}`)
	writeFile(t, dir, ".prettierrc", `{}`)

	info := Analyze(dir)

	if !strings.Contains(info.CodeStyle, "Prettier") {
		t.Errorf("CodeStyle = %q, want it to mention Prettier", info.CodeStyle)
	}
}

func TestAnalyze_Node_YarnLock(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{
		"name": "my-app",
		"scripts": {
			"build": "webpack",
			"test": "jest",
			"lint": "eslint ."
		}
	}`)
	writeFile(t, dir, "yarn.lock", "# yarn lockfile v1\n")

	info := Analyze(dir)

	if info.BuildCmd != "yarn run build" {
		t.Errorf("BuildCmd = %q, want %q", info.BuildCmd, "yarn run build")
	}
	if info.LintCmd != "yarn run lint" {
		t.Errorf("LintCmd = %q, want %q", info.LintCmd, "yarn run lint")
	}
	if info.TestCmd != "yarn test" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "yarn test")
	}
}

func TestAnalyze_Node_PnpmLock(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{
		"name": "my-app",
		"scripts": {
			"build": "vite build",
			"test": "vitest",
			"lint": "eslint ."
		}
	}`)
	writeFile(t, dir, "pnpm-lock.yaml", "lockfileVersion: '6.0'\n")

	info := Analyze(dir)

	if info.BuildCmd != "pnpm run build" {
		t.Errorf("BuildCmd = %q, want %q", info.BuildCmd, "pnpm run build")
	}
	if info.LintCmd != "pnpm run lint" {
		t.Errorf("LintCmd = %q, want %q", info.LintCmd, "pnpm run lint")
	}
	if info.TestCmd != "pnpm test" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "pnpm test")
	}
}

func TestAnalyze_Node_BunLockb(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{
		"name": "my-app",
		"scripts": {
			"build": "bun build src/index.ts",
			"test": "bun test",
			"lint": "eslint ."
		}
	}`)
	writeFile(t, dir, "bun.lockb", "")

	info := Analyze(dir)

	if info.BuildCmd != "bun run build" {
		t.Errorf("BuildCmd = %q, want %q", info.BuildCmd, "bun run build")
	}
	if info.LintCmd != "bun run lint" {
		t.Errorf("LintCmd = %q, want %q", info.LintCmd, "bun run lint")
	}
	if info.TestCmd != "bun run test" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "bun run test")
	}
}

func TestAnalyze_Node_LockfilePriority(t *testing.T) {
	// bun.lockb takes priority over yarn.lock and pnpm-lock.yaml
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name": "app", "scripts": {"build": "build"}}`)
	writeFile(t, dir, "bun.lockb", "")
	writeFile(t, dir, "yarn.lock", "# yarn lockfile v1\n")

	info := Analyze(dir)

	if info.BuildCmd != "bun run build" {
		t.Errorf("BuildCmd = %q, want bun run build (bun.lockb should take priority)", info.BuildCmd)
	}
}

func TestAnalyze_Node_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{not valid json`)

	// Should not panic; Language is still Node.js because the file exists.
	info := Analyze(dir)

	if info.Language != "Node.js" {
		t.Errorf("Language = %q, want %q", info.Language, "Node.js")
	}
	// ProjectName falls back to the directory basename when JSON parse fails.
	if info.ProjectName == "" {
		t.Error("ProjectName should not be empty after malformed package.json")
	}
}

// --- Analyze: Unknown ---

func TestAnalyze_Unknown_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	info := Analyze(dir)

	if info.Language != "Unknown" {
		t.Errorf("Language = %q, want %q", info.Language, "Unknown")
	}
	if info.ProjectName == "" {
		t.Error("ProjectName should not be empty for an unknown project")
	}
}

// --- Analyze: multiple stacks present ---

func TestAnalyze_GoTakesPriorityOverNode(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/multi\n\ngo 1.22\n")
	writeFile(t, dir, "package.json", `{"name": "multi-node"}`)

	info := Analyze(dir)

	if info.Language != "Go" {
		t.Errorf("Language = %q, want Go (go.mod should take priority)", info.Language)
	}
}

func TestAnalyze_GoTakesPriorityOverRust(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/multi\n\ngo 1.22\n")
	writeFile(t, dir, "Cargo.toml", "[package]\nname = \"crate\"\n")

	info := Analyze(dir)

	if info.Language != "Go" {
		t.Errorf("Language = %q, want Go (go.mod should take priority)", info.Language)
	}
}

// --- Analyze: Rust ---

func TestAnalyze_Rust(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Cargo.toml", "[package]\nname = \"my-crate\"\nedition = \"2021\"\n\n[dependencies]\n")

	info := Analyze(dir)

	if info.Language != "Rust" {
		t.Errorf("Language = %q, want %q", info.Language, "Rust")
	}
	if info.ProjectName != "my-crate" {
		t.Errorf("ProjectName = %q, want %q", info.ProjectName, "my-crate")
	}
	if info.Version != "edition 2021" {
		t.Errorf("Version = %q, want %q", info.Version, "edition 2021")
	}
	if info.BuildCmd != "cargo build" {
		t.Errorf("BuildCmd = %q, want %q", info.BuildCmd, "cargo build")
	}
	if info.TestCmd != "cargo test" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "cargo test")
	}
	if info.LintCmd != "cargo clippy" {
		t.Errorf("LintCmd = %q, want %q", info.LintCmd, "cargo clippy")
	}
}

// --- Analyze: Python ---

func TestAnalyze_Python_RequirementsTxt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "requirements.txt", "requests==2.31.0\nflask>=3.0\n")

	info := Analyze(dir)

	if info.Language != "Python" {
		t.Errorf("Language = %q, want %q", info.Language, "Python")
	}
}

func TestAnalyze_Python_Pyproject(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pyproject.toml", `[project]
name = "my-project"
requires-python = ">=3.11"

[tool.pytest.ini_options]
testpaths = ["tests"]
`)

	info := Analyze(dir)

	if info.Language != "Python" {
		t.Errorf("Language = %q, want %q", info.Language, "Python")
	}
	if info.ProjectName != "my-project" {
		t.Errorf("ProjectName = %q, want %q", info.ProjectName, "my-project")
	}
	if info.TestCmd != "pytest" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "pytest")
	}
}

func TestAnalyze_Python_RequiresPythonGe(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pyproject.toml", `[project]
name = "my-project"
requires-python = ">=3.9"
`)

	info := Analyze(dir)

	if info.Version != "3.9" {
		t.Errorf("Version = %q, want %q (constraint operator must be stripped)", info.Version, "3.9")
	}
}

func TestAnalyze_Python_RequiresPythonRange(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pyproject.toml", `[project]
name = "my-project"
requires-python = ">=3.8,<4.0"
`)

	info := Analyze(dir)

	if info.Version != "3.8" {
		t.Errorf("Version = %q, want %q (range constraint must be truncated to lower bound)", info.Version, "3.8")
	}
}

func TestAnalyze_Python_RuffLint(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pyproject.toml", "[tool.ruff]\nline-length = 120\n")

	info := Analyze(dir)

	if info.LintCmd != "ruff check ." {
		t.Errorf("LintCmd = %q, want %q", info.LintCmd, "ruff check .")
	}
}

func TestAnalyze_Python_SetupPyOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "setup.py", "from setuptools import setup\nsetup(name='mylib', version='1.0')\n")

	info := Analyze(dir)

	if info.Language != "Python" {
		t.Errorf("Language = %q, want %q", info.Language, "Python")
	}
}

func TestAnalyze_Python_SetupCfgName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "setup.cfg", "[metadata]\nname = my-legacy-project\nversion = 2.0\n\n[options]\ninstall_requires =\n    requests\n")

	info := Analyze(dir)

	if info.Language != "Python" {
		t.Errorf("Language = %q, want %q", info.Language, "Python")
	}
	if info.ProjectName != "my-legacy-project" {
		t.Errorf("ProjectName = %q, want %q", info.ProjectName, "my-legacy-project")
	}
}

func TestAnalyze_Python_SetupCfgPytest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "setup.cfg", "[metadata]\nname = myapp\n\n[tool:pytest]\ntestpaths = tests\n")

	info := Analyze(dir)

	if info.TestCmd != "pytest" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "pytest")
	}
}

func TestAnalyze_Python_PythonVersionFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "requirements.txt", "flask\n")
	writeFile(t, dir, ".python-version", "3.12.0\n")

	info := Analyze(dir)

	if info.Version != "3.12.0" {
		t.Errorf("Version = %q, want %q", info.Version, "3.12.0")
	}
}

// --- Analyze: Swift ---

func TestAnalyze_Swift_Minimal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Package.swift", "// swift-tools-version:5.9\nimport PackageDescription\n")

	info := Analyze(dir)

	if info.Language != "Swift" {
		t.Errorf("Language = %q, want %q", info.Language, "Swift")
	}
	if info.Version != "5.9" {
		t.Errorf("Version = %q, want %q", info.Version, "5.9")
	}
	if info.BuildCmd != "swift build" {
		t.Errorf("BuildCmd = %q, want %q", info.BuildCmd, "swift build")
	}
	if info.TestCmd != "swift test" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "swift test")
	}
	if info.LintCmd != "" {
		t.Errorf("LintCmd = %q, want empty (no swiftlint config)", info.LintCmd)
	}
}

func TestAnalyze_Swift_VersionWithSpace(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Package.swift", "// swift-tools-version: 5.10\nimport PackageDescription\n")

	info := Analyze(dir)

	if info.Version != "5.10" {
		t.Errorf("Version = %q, want %q", info.Version, "5.10")
	}
}

func TestAnalyze_Swift_WithSwiftLintYml(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Package.swift", "// swift-tools-version:5.9\n")
	writeFile(t, dir, ".swiftlint.yml", "disabled_rules:\n  - line_length\n")

	info := Analyze(dir)

	if info.LintCmd != "swiftlint" {
		t.Errorf("LintCmd = %q, want %q", info.LintCmd, "swiftlint")
	}
}

func TestAnalyze_Swift_WithSwiftLintYaml(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Package.swift", "// swift-tools-version:5.9\n")
	writeFile(t, dir, ".swiftlint.yaml", "disabled_rules:\n  - line_length\n")

	info := Analyze(dir)

	if info.LintCmd != "swiftlint" {
		t.Errorf("LintCmd = %q, want %q", info.LintCmd, "swiftlint")
	}
}

// --- Analyze: Elixir ---

func TestAnalyze_Elixir_Minimal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "mix.exs", `defmodule MyApp.MixProject do
  use Mix.Project

  def project do
    [
      app: :my_app,
      version: "0.1.0",
      elixir: "~> 1.15",
      deps: deps()
    ]
  end
end
`)

	info := Analyze(dir)

	if info.Language != "Elixir" {
		t.Errorf("Language = %q, want %q", info.Language, "Elixir")
	}
	if info.ProjectName != "my_app" {
		t.Errorf("ProjectName = %q, want %q", info.ProjectName, "my_app")
	}
	if info.Version != "1.15" {
		t.Errorf("Version = %q, want %q", info.Version, "1.15")
	}
	if info.BuildCmd != "mix compile" {
		t.Errorf("BuildCmd = %q, want %q", info.BuildCmd, "mix compile")
	}
	if info.TestCmd != "mix test" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "mix test")
	}
	if info.LintCmd != "" {
		t.Errorf("LintCmd = %q, want empty (no credo)", info.LintCmd)
	}
}

func TestAnalyze_Elixir_WithCredo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "mix.exs", `defmodule MyApp.MixProject do
  use Mix.Project

  def project do
    [
      app: :my_app,
      elixir: "~> 1.14",
    ]
  end

  defp deps do
    [
      {:credo, "~> 1.7", only: [:dev, :test], runtime: false}
    ]
  end
end
`)

	info := Analyze(dir)

	if info.LintCmd != "mix credo" {
		t.Errorf("LintCmd = %q, want %q", info.LintCmd, "mix credo")
	}
	if info.Version != "1.14" {
		t.Errorf("Version = %q, want %q", info.Version, "1.14")
	}
}

func TestAnalyze_Elixir_NoCredo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "mix.exs", `defmodule MyApp.MixProject do
  use Mix.Project

  def project do
    [app: :my_app, elixir: "~> 1.16"]
  end

  defp deps do
    [{:jason, "~> 1.4"}]
  end
end
`)

	info := Analyze(dir)

	if info.LintCmd != "" {
		t.Errorf("LintCmd = %q, want empty (no credo in deps)", info.LintCmd)
	}
}

// --- LanguageSummary ---

func TestLanguageSummary(t *testing.T) {
	tests := []struct {
		name string
		info RepoInfo
		want string
	}{
		{
			name: "language only",
			info: RepoInfo{Language: "Go"},
			want: "Go",
		},
		{
			name: "language with version",
			info: RepoInfo{Language: "Go", Version: "1.22"},
			want: "Go 1.22",
		},
		{
			name: "language with version and module",
			info: RepoInfo{Language: "Go", Version: "1.22", Module: "example.com/myapp"},
			want: "Go 1.22, module example.com/myapp",
		},
		{
			name: "module only (no language)",
			info: RepoInfo{Module: "example.com/myapp"},
			want: "module example.com/myapp",
		},
		{
			name: "empty",
			info: RepoInfo{},
			want: "Unknown",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.info.LanguageSummary()
			if got != tc.want {
				t.Errorf("LanguageSummary() = %q, want %q", got, tc.want)
			}
		})
	}
}

// --- makefileHasTarget ---

func TestMakefileHasTarget(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Makefile", "build:\n\tgo build ./...\n\ntest:\n\tgo test ./...\n")

	if !makefileHasTarget(dir, "build") {
		t.Error("expected makefileHasTarget to return true for 'build'")
	}
	if !makefileHasTarget(dir, "test") {
		t.Error("expected makefileHasTarget to return true for 'test'")
	}
	if makefileHasTarget(dir, "lint") {
		t.Error("expected makefileHasTarget to return false for 'lint'")
	}
}

func TestMakefileHasTarget_NoMakefile(t *testing.T) {
	dir := t.TempDir()

	if makefileHasTarget(dir, "build") {
		t.Error("expected makefileHasTarget to return false when no Makefile")
	}
}

// --- hasGoTestFiles ---

func TestHasGoTestFiles_Empty(t *testing.T) {
	dir := t.TempDir()

	if hasGoTestFiles(dir) {
		t.Error("expected no test files in empty dir")
	}
}

func TestHasGoTestFiles_WithTestFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main_test.go", "package main\n")

	if !hasGoTestFiles(dir) {
		t.Error("expected hasGoTestFiles to return true after adding a _test.go file")
	}
}

func TestHasGoTestFiles_SkipsVendor(t *testing.T) {
	dir := t.TempDir()
	vendorDir := filepath.Join(dir, "vendor")
	if err := os.MkdirAll(vendorDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, vendorDir, "something_test.go", "package vendor\n")

	if hasGoTestFiles(dir) {
		t.Error("expected hasGoTestFiles to skip the vendor directory")
	}
}

func TestHasGoTestFiles_SkipsGit(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, gitDir, "something_test.go", "package git\n")

	if hasGoTestFiles(dir) {
		t.Error("expected hasGoTestFiles to skip the .git directory")
	}
}

// --- GenerateCLAUDEMD ---

func TestGenerateCLAUDEMD(t *testing.T) {
	info := &RepoInfo{
		ProjectName: "myapp",
		Language:    "Go",
		Version:     "1.22",
		Module:      "example.com/myapp",
		BuildCmd:    "go build ./...",
		LintCmd:     "go vet ./...",
		TestCmd:     "go test ./...",
	}

	content := info.GenerateCLAUDEMD()

	for _, want := range []string{"myapp", "Go 1.22", "go build ./...", "go vet ./...", "go test ./..."} {
		if !strings.Contains(content, want) {
			t.Errorf("GenerateCLAUDEMD() missing %q in output", want)
		}
	}
}

func TestGenerateCLAUDEMD_NoTestCmd(t *testing.T) {
	info := &RepoInfo{
		ProjectName: "proj",
		Language:    "Go",
	}

	content := info.GenerateCLAUDEMD()

	if !strings.Contains(content, "No test suite configured") {
		t.Error("GenerateCLAUDEMD() should mention missing test suite")
	}
}

func TestGenerateCLAUDEMD_WithCodeStyle(t *testing.T) {
	info := &RepoInfo{
		ProjectName: "proj",
		Language:    "Rust",
		CodeStyle:   "Uses cargo fmt.",
	}

	content := info.GenerateCLAUDEMD()

	if !strings.Contains(content, "cargo fmt") {
		t.Error("GenerateCLAUDEMD() should include CodeStyle section")
	}
}
