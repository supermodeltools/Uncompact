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

func TestAnalyze_Node_NodeVersionFileFallback(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name": "proj"}`)
	writeFile(t, dir, ".node-version", "20.11.0\n")

	info := Analyze(dir)

	if info.Version != "20.11.0" {
		t.Errorf("Version = %q, want %q", info.Version, "20.11.0")
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

func TestAnalyze_Node_TypeScript_TsConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name": "my-ts-app"}`)
	writeFile(t, dir, "tsconfig.json", `{"compilerOptions": {"target": "ES2020"}}`)

	info := Analyze(dir)

	if info.Language != "TypeScript" {
		t.Errorf("Language = %q, want %q", info.Language, "TypeScript")
	}
}

func TestAnalyze_Node_TypeScript_TsConfigBase(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name": "my-ts-monorepo"}`)
	writeFile(t, dir, "tsconfig.base.json", `{"compilerOptions": {"target": "ES2020"}}`)

	info := Analyze(dir)

	if info.Language != "TypeScript" {
		t.Errorf("Language = %q, want %q", info.Language, "TypeScript")
	}
}

func TestAnalyze_Node_NoTsConfig_ReportsNodeJS(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name": "plain-js-app"}`)

	info := Analyze(dir)

	if info.Language != "Node.js" {
		t.Errorf("Language = %q, want %q", info.Language, "Node.js")
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

// --- Analyze: Java ---

func TestAnalyze_Java_MavenProjectName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pom.xml", `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>my-maven-app</artifactId>
  <version>1.0.0</version>
</project>`)

	info := Analyze(dir)

	if info.Language != "Java" {
		t.Errorf("Language = %q, want %q", info.Language, "Java")
	}
	if info.ProjectName != "my-maven-app" {
		t.Errorf("ProjectName = %q, want %q", info.ProjectName, "my-maven-app")
	}
}

func TestAnalyze_Java_MavenProjectName_SkipsParentArtifactId(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pom.xml", `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <parent>
    <groupId>org.springframework.boot</groupId>
    <artifactId>spring-boot-starter-parent</artifactId>
    <version>3.0.0</version>
  </parent>
  <groupId>com.example</groupId>
  <artifactId>my-spring-app</artifactId>
  <version>0.0.1-SNAPSHOT</version>
</project>`)

	info := Analyze(dir)

	if info.ProjectName != "my-spring-app" {
		t.Errorf("ProjectName = %q, want %q (should use project artifactId, not parent)", info.ProjectName, "my-spring-app")
	}
}

func TestAnalyze_Java_GradleSettingsProjectName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "build.gradle", "plugins {\n    id 'java'\n}\n")
	writeFile(t, dir, "settings.gradle", "rootProject.name = 'my-gradle-app'\n")

	info := Analyze(dir)

	if info.Language != "Java" {
		t.Errorf("Language = %q, want %q", info.Language, "Java")
	}
	if info.ProjectName != "my-gradle-app" {
		t.Errorf("ProjectName = %q, want %q", info.ProjectName, "my-gradle-app")
	}
}

func TestAnalyze_Java_GradleSettingsKtsProjectName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "build.gradle.kts", "plugins {\n    kotlin(\"jvm\")\n}\n")
	writeFile(t, dir, "settings.gradle.kts", "rootProject.name = \"my-kotlin-app\"\n")

	info := Analyze(dir)

	if info.Language != "Kotlin" {
		t.Errorf("Language = %q, want %q", info.Language, "Kotlin")
	}
	if info.ProjectName != "my-kotlin-app" {
		t.Errorf("ProjectName = %q, want %q", info.ProjectName, "my-kotlin-app")
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

func TestAnalyze_Swift_ProjectNameIsPackageName(t *testing.T) {
	// Regression test: analyzeSwift must use the top-level Package(name:) value,
	// not the last target/product name: it encounters in the file.
	dir := t.TempDir()
	writeFile(t, dir, "Package.swift", `// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "MyLib",
    products: [
        .library(name: "MyLib", targets: ["MyLib"]),
    ],
    targets: [
        .target(name: "MyLib", dependencies: []),
        .testTarget(name: "MyLibTests", dependencies: ["MyLib"]),
    ]
)
`)

	info := Analyze(dir)

	if info.ProjectName != "MyLib" {
		t.Errorf("ProjectName = %q, want %q (package name, not last target name)", info.ProjectName, "MyLib")
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

// --- Analyze: Ruby ---

func TestAnalyze_Ruby_Minimal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Gemfile", "source 'https://rubygems.org'\n\ngem 'rails'\n")

	info := Analyze(dir)

	if info.Language != "Ruby" {
		t.Errorf("Language = %q, want %q", info.Language, "Ruby")
	}
	if info.ProjectName != filepath.Base(dir) {
		t.Errorf("ProjectName = %q, want %q", info.ProjectName, filepath.Base(dir))
	}
}

func TestAnalyze_Ruby_Version(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Gemfile", "source 'https://rubygems.org'\n")
	writeFile(t, dir, ".ruby-version", "3.2.2\n")

	info := Analyze(dir)

	if info.Version != "3.2.2" {
		t.Errorf("Version = %q, want %q", info.Version, "3.2.2")
	}
}

func TestAnalyze_Ruby_RuboCopYml(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Gemfile", "source 'https://rubygems.org'\n")
	writeFile(t, dir, ".rubocop.yml", "AllCops:\n  NewCops: enable\n")

	info := Analyze(dir)

	if info.LintCmd != "bundle exec rubocop" {
		t.Errorf("LintCmd = %q, want %q", info.LintCmd, "bundle exec rubocop")
	}
	if !strings.Contains(info.CodeStyle, "RuboCop") {
		t.Errorf("CodeStyle = %q, want it to mention RuboCop", info.CodeStyle)
	}
}

func TestAnalyze_Ruby_RuboCopYaml(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Gemfile", "source 'https://rubygems.org'\n")
	writeFile(t, dir, ".rubocop.yaml", "AllCops:\n  NewCops: enable\n")

	info := Analyze(dir)

	if info.LintCmd != "bundle exec rubocop" {
		t.Errorf("LintCmd = %q, want %q", info.LintCmd, "bundle exec rubocop")
	}
	if !strings.Contains(info.CodeStyle, "RuboCop") {
		t.Errorf("CodeStyle = %q, want it to mention RuboCop", info.CodeStyle)
	}
}

func TestAnalyze_Ruby_RSpec(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Gemfile", "source 'https://rubygems.org'\n")
	if err := os.MkdirAll(filepath.Join(dir, "spec"), 0755); err != nil {
		t.Fatal(err)
	}

	info := Analyze(dir)

	if info.TestCmd != "bundle exec rspec" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "bundle exec rspec")
	}
}

func TestAnalyze_Ruby_Minitest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Gemfile", "source 'https://rubygems.org'\n")
	if err := os.MkdirAll(filepath.Join(dir, "test"), 0755); err != nil {
		t.Fatal(err)
	}

	info := Analyze(dir)

	if info.TestCmd != "bundle exec rake test" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "bundle exec rake test")
	}
}

func TestAnalyze_Ruby_MakefileBuild(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Gemfile", "source 'https://rubygems.org'\n")
	writeFile(t, dir, "Makefile", "build:\n\tbundle exec rake build\n")

	info := Analyze(dir)

	if info.BuildCmd != "make build" {
		t.Errorf("BuildCmd = %q, want %q", info.BuildCmd, "make build")
	}
}

func TestAnalyze_Ruby_MakefileTestOverridesSpec(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Gemfile", "source 'https://rubygems.org'\n")
	writeFile(t, dir, "Makefile", "test:\n\tbundle exec rspec\n")
	if err := os.MkdirAll(filepath.Join(dir, "spec"), 0755); err != nil {
		t.Fatal(err)
	}

	info := Analyze(dir)

	if info.TestCmd != "make test" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "make test")
	}
}

func TestAnalyze_Ruby_MakefileTestOverridesMinitest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Gemfile", "source 'https://rubygems.org'\n")
	writeFile(t, dir, "Makefile", "test:\n\tbundle exec rake test\n")
	if err := os.MkdirAll(filepath.Join(dir, "test"), 0755); err != nil {
		t.Fatal(err)
	}

	info := Analyze(dir)

	if info.TestCmd != "make test" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "make test")
	}
}

func TestAnalyze_Ruby_MakefileLint(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Gemfile", "source 'https://rubygems.org'\n")
	writeFile(t, dir, "Makefile", "lint:\n\tbundle exec rubocop\n")

	info := Analyze(dir)

	if info.LintCmd != "make lint" {
		t.Errorf("LintCmd = %q, want %q", info.LintCmd, "make lint")
	}
}

// --- Analyze: Java / Kotlin ---

func TestAnalyze_Java_GradleProject(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "build.gradle", "plugins { id 'java' }\n")

	info := Analyze(dir)

	if info.Language != "Java" {
		t.Errorf("Language = %q, want %q", info.Language, "Java")
	}
	if info.BuildCmd != "gradle build" {
		t.Errorf("BuildCmd = %q, want %q", info.BuildCmd, "gradle build")
	}
	if info.TestCmd != "gradle test" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "gradle test")
	}
}

func TestAnalyze_Java_GradleKotlinDSL(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "build.gradle.kts", "plugins { kotlin(\"jvm\") }\n")

	info := Analyze(dir)

	if info.Language != "Kotlin" {
		t.Errorf("Language = %q, want %q", info.Language, "Kotlin")
	}
	if info.BuildCmd != "gradle build" {
		t.Errorf("BuildCmd = %q, want %q", info.BuildCmd, "gradle build")
	}
}

func TestAnalyze_Java_GradleWrapper(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "build.gradle", "plugins { id 'java' }\n")
	writeFile(t, dir, "gradlew", "#!/bin/sh\n")

	info := Analyze(dir)

	if info.BuildCmd != "./gradlew build" {
		t.Errorf("BuildCmd = %q, want %q", info.BuildCmd, "./gradlew build")
	}
	if info.TestCmd != "./gradlew test" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "./gradlew test")
	}
}

func TestAnalyze_Java_MavenWithWrapper(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pom.xml", "<project></project>\n")
	writeFile(t, dir, "mvnw", "#!/bin/sh\n")

	info := Analyze(dir)

	if info.Language != "Java" {
		t.Errorf("Language = %q, want %q", info.Language, "Java")
	}
	if info.BuildCmd != "./mvnw package" {
		t.Errorf("BuildCmd = %q, want %q", info.BuildCmd, "./mvnw package")
	}
	if info.TestCmd != "./mvnw test" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "./mvnw test")
	}
}

func TestAnalyze_Java_VersionFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "build.gradle", "plugins { id 'java' }\n")
	writeFile(t, dir, ".java-version", "21\n")

	info := Analyze(dir)

	if info.Version != "21" {
		t.Errorf("Version = %q, want %q", info.Version, "21")
	}
}

func TestAnalyze_Java_MakefileOverrides(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "build.gradle", "plugins { id 'java' }\n")
	writeFile(t, dir, "Makefile", "build:\n\t./gradlew build\n\ntest:\n\t./gradlew test\n")

	info := Analyze(dir)

	if info.BuildCmd != "make build" {
		t.Errorf("BuildCmd = %q, want %q", info.BuildCmd, "make build")
	}
	if info.TestCmd != "make test" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "make test")
	}
}

// --- Analyze: PHP ---

func TestAnalyze_PHP_ComposerJson(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "composer.json", `{
		"name": "myvendor/mypackage",
		"require": {"php": ">=8.1"},
		"scripts": {
			"test": "phpunit",
			"lint": "phpcs"
		}
	}`)

	info := Analyze(dir)

	if info.Language != "PHP" {
		t.Errorf("Language = %q, want %q", info.Language, "PHP")
	}
	if info.ProjectName != "mypackage" {
		t.Errorf("ProjectName = %q, want %q", info.ProjectName, "mypackage")
	}
	if info.Version != "8.1" {
		t.Errorf("Version = %q, want %q", info.Version, "8.1")
	}
	if info.TestCmd != "composer test" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "composer test")
	}
	if info.LintCmd != "composer lint" {
		t.Errorf("LintCmd = %q, want %q", info.LintCmd, "composer lint")
	}
}

func TestAnalyze_PHP_VendorPrefixStripped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "composer.json", `{"name": "acme/my-lib"}`)

	info := Analyze(dir)

	if info.ProjectName != "my-lib" {
		t.Errorf("ProjectName = %q, want %q (vendor prefix must be stripped)", info.ProjectName, "my-lib")
	}
}

func TestAnalyze_PHP_FallbackPhpunitXml(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "composer.json", `{"name": "myvendor/myapp"}`)
	writeFile(t, dir, "phpunit.xml", "<phpunit></phpunit>\n")

	info := Analyze(dir)

	if info.TestCmd != "phpunit" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "phpunit")
	}
}

func TestAnalyze_PHP_FallbackPhpstan(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "composer.json", `{"name": "myvendor/myapp"}`)
	if err := os.MkdirAll(filepath.Join(dir, "vendor/bin"), 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "vendor/bin/phpstan", "#!/bin/sh\n")

	info := Analyze(dir)

	if info.LintCmd != "./vendor/bin/phpstan analyse" {
		t.Errorf("LintCmd = %q, want %q", info.LintCmd, "./vendor/bin/phpstan analyse")
	}
}

func TestAnalyze_PHP_DefaultBuildCmd(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "composer.json", `{"name": "myvendor/myapp"}`)

	info := Analyze(dir)

	if info.BuildCmd != "composer install" {
		t.Errorf("BuildCmd = %q, want %q", info.BuildCmd, "composer install")
	}
}

// --- Analyze: C#/.NET ---

func TestAnalyze_DotNet_GlobalJsonVersion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global.json", `{"sdk": {"version": "8.0.100"}}`)

	info := Analyze(dir)

	if info.Language != "C#" {
		t.Errorf("Language = %q, want %q", info.Language, "C#")
	}
	if info.Version != "8.0.100" {
		t.Errorf("Version = %q, want %q", info.Version, "8.0.100")
	}
}

func TestAnalyze_DotNet_DirectoryBuildProps(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Directory.Build.props", "<Project></Project>\n")

	info := Analyze(dir)

	if info.Language != "C#" {
		t.Errorf("Language = %q, want %q", info.Language, "C#")
	}
}

func TestAnalyze_DotNet_CsprojFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "MyApp.csproj", "<Project Sdk=\"Microsoft.NET.Sdk\"></Project>\n")

	info := Analyze(dir)

	if info.Language != "C#" {
		t.Errorf("Language = %q, want %q", info.Language, "C#")
	}
}

func TestAnalyze_DotNet_DefaultCommands(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global.json", `{"sdk": {"version": "8.0.100"}}`)

	info := Analyze(dir)

	if info.BuildCmd != "dotnet build" {
		t.Errorf("BuildCmd = %q, want %q", info.BuildCmd, "dotnet build")
	}
	if info.TestCmd != "dotnet test" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "dotnet test")
	}
	if info.LintCmd != "dotnet format --verify-no-changes" {
		t.Errorf("LintCmd = %q, want %q", info.LintCmd, "dotnet format --verify-no-changes")
	}
}

func TestAnalyze_DotNet_MakefileOverrides(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "global.json", `{"sdk": {"version": "8.0.100"}}`)
	writeFile(t, dir, "Makefile", "build:\n\tdotnet build\n\ntest:\n\tdotnet test\n")

	info := Analyze(dir)

	if info.BuildCmd != "make build" {
		t.Errorf("BuildCmd = %q, want %q", info.BuildCmd, "make build")
	}
	if info.TestCmd != "make test" {
		t.Errorf("TestCmd = %q, want %q", info.TestCmd, "make test")
	}
}

func TestAnalyze_DotNet_ProjectNameFromCsproj(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "MyWebApi.csproj", "<Project Sdk=\"Microsoft.NET.Sdk\"></Project>\n")

	info := Analyze(dir)

	if info.ProjectName != "MyWebApi" {
		t.Errorf("ProjectName = %q, want %q", info.ProjectName, "MyWebApi")
	}
}

func TestAnalyze_DotNet_ProjectNameFromSln(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "MySolution.sln", "\n")

	info := Analyze(dir)

	if info.ProjectName != "MySolution" {
		t.Errorf("ProjectName = %q, want %q", info.ProjectName, "MySolution")
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
