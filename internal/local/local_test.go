package local

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile creates a file at dir/name with the given content, creating parent
// directories as needed.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatalf("writeFile %s: %v", name, err)
	}
}

// --- BuildProjectGraph ---

func TestBuildProjectGraph_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	graph, err := BuildProjectGraph(context.Background(), dir, "emptyproject")
	if err != nil {
		t.Fatalf("BuildProjectGraph: %v", err)
	}
	if graph == nil {
		t.Fatal("expected non-nil graph")
	}
	if graph.Name != "emptyproject" {
		t.Errorf("Name = %q, want %q", graph.Name, "emptyproject")
	}
	if graph.Stats.TotalFiles != 0 {
		t.Errorf("TotalFiles = %d, want 0", graph.Stats.TotalFiles)
	}
	if len(graph.Domains) != 0 {
		t.Errorf("Domains = %v, want empty", graph.Domains)
	}
}

func TestBuildProjectGraph_DetectsGoLanguage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cmd/main.go", "package main\n")
	writeFile(t, dir, "internal/server.go", "package internal\n")

	graph, err := BuildProjectGraph(context.Background(), dir, "goproject")
	if err != nil {
		t.Fatalf("BuildProjectGraph: %v", err)
	}

	if graph.Language != "Go" {
		t.Errorf("Language = %q, want %q", graph.Language, "Go")
	}
	if graph.Stats.TotalFiles != 2 {
		t.Errorf("TotalFiles = %d, want 2", graph.Stats.TotalFiles)
	}
}

func TestBuildProjectGraph_CreatesDomainsFromDirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cmd/main.go", "package main\n")
	writeFile(t, dir, "internal/server.go", "package internal\n")
	writeFile(t, dir, "README.md", "My project\n")

	graph, err := BuildProjectGraph(context.Background(), dir, "proj")
	if err != nil {
		t.Fatalf("BuildProjectGraph: %v", err)
	}

	domainNames := make(map[string]bool)
	for _, d := range graph.Domains {
		domainNames[d.Name] = true
	}
	for _, want := range []string{"cmd", "internal", "Root"} {
		if !domainNames[want] {
			t.Errorf("expected domain %q; got domains %v", want, domainNames)
		}
	}
}

func TestBuildProjectGraph_KeyFilesCappedAt8(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 10; i++ {
		writeFile(t, dir, fmt.Sprintf("pkg/file%02d.go", i), "package pkg\n")
	}

	graph, err := BuildProjectGraph(context.Background(), dir, "proj")
	if err != nil {
		t.Fatalf("BuildProjectGraph: %v", err)
	}

	for _, d := range graph.Domains {
		if d.Name == "pkg" && len(d.KeyFiles) > 8 {
			t.Errorf("KeyFiles for domain %q = %d, want ≤8", d.Name, len(d.KeyFiles))
		}
	}
}

func TestBuildProjectGraph_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so the walk exits on the first callback

	_, err := BuildProjectGraph(ctx, dir, "proj")
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}

func TestBuildProjectGraph_IgnoresHiddenFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".env", "SECRET=abc\n")
	writeFile(t, dir, "main.go", "package main\n")

	graph, err := BuildProjectGraph(context.Background(), dir, "proj")
	if err != nil {
		t.Fatalf("BuildProjectGraph: %v", err)
	}

	// .env is hidden and must be excluded; only main.go counts.
	if graph.Stats.TotalFiles != 1 {
		t.Errorf("TotalFiles = %d, want 1 (.env should be ignored)", graph.Stats.TotalFiles)
	}
}

func TestBuildProjectGraph_IgnoresNodeModules(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "index.js", "const x = 1;\n")
	writeFile(t, dir, "node_modules/dep/index.js", "module.exports = {};\n")

	graph, err := BuildProjectGraph(context.Background(), dir, "proj")
	if err != nil {
		t.Fatalf("BuildProjectGraph: %v", err)
	}

	// node_modules is in ignoreDirs; only index.js should count.
	if graph.Stats.TotalFiles != 1 {
		t.Errorf("TotalFiles = %d, want 1 (node_modules should be ignored)", graph.Stats.TotalFiles)
	}
}

// --- detectLanguages ---

func TestDetectLanguages_Empty(t *testing.T) {
	primary, langs := detectLanguages(map[string]int{})
	if primary != "" {
		t.Errorf("primary = %q, want empty", primary)
	}
	if len(langs) != 0 {
		t.Errorf("langs = %v, want empty", langs)
	}
}

func TestDetectLanguages_SingleLanguage(t *testing.T) {
	primary, langs := detectLanguages(map[string]int{".go": 5})
	if primary != "Go" {
		t.Errorf("primary = %q, want %q", primary, "Go")
	}
	if len(langs) != 1 || langs[0] != "Go" {
		t.Errorf("langs = %v, want [Go]", langs)
	}
}

func TestDetectLanguages_PicksMostCommon(t *testing.T) {
	primary, _ := detectLanguages(map[string]int{".go": 10, ".py": 3, ".ts": 1})
	if primary != "Go" {
		t.Errorf("primary = %q, want Go (most files are Go)", primary)
	}
}

func TestDetectLanguages_CapsAt5Languages(t *testing.T) {
	extCounts := map[string]int{
		".go":   10,
		".py":   9,
		".ts":   8,
		".rs":   7,
		".rb":   6,
		".java": 5,
		".kt":   4,
	}
	_, langs := detectLanguages(extCounts)
	if len(langs) > 5 {
		t.Errorf("langs len = %d, want ≤5", len(langs))
	}
}

func TestDetectLanguages_UnknownExtensionsIgnored(t *testing.T) {
	primary, langs := detectLanguages(map[string]int{".xyz": 100, ".go": 1})
	if primary != "Go" {
		t.Errorf("primary = %q, want Go (unknown ext .xyz should be ignored)", primary)
	}
	if len(langs) != 1 {
		t.Errorf("langs = %v, want [Go]", langs)
	}
}

// --- readDescription ---

func TestReadDescription_NoReadme(t *testing.T) {
	dir := t.TempDir()
	desc := readDescription(dir)
	if desc != "" {
		t.Errorf("readDescription = %q, want empty when no README", desc)
	}
}

func TestReadDescription_ReadmeMd(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# MyProject\n\nA great project.\n")

	desc := readDescription(dir)
	if desc != "A great project." {
		t.Errorf("readDescription = %q, want %q", desc, "A great project.")
	}
}

func TestReadDescription_SkipsHeadingLines(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# Heading\n## Subheading\nDescription here.\n")

	desc := readDescription(dir)
	if desc != "Description here." {
		t.Errorf("readDescription = %q, want %q", desc, "Description here.")
	}
}

func TestReadDescription_SkipsBadgeLines(t *testing.T) {
	dir := t.TempDir()
	readme := "# MyProject\n" +
		"[![CI](https://ci.example.com/badge.svg)](https://ci.example.com)\n" +
		"[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)\n" +
		"\n" +
		"A Go application for managing widgets.\n"
	writeFile(t, dir, "README.md", readme)

	desc := readDescription(dir)
	if desc != "A Go application for managing widgets." {
		t.Errorf("readDescription = %q, want %q", desc, "A Go application for managing widgets.")
	}
}

func TestReadDescription_IgnoresLongLines(t *testing.T) {
	dir := t.TempDir()
	longLine := strings.Repeat("x", 300) // > 250 chars, should be skipped
	writeFile(t, dir, "README.md", longLine+"\nShort description.\n")

	desc := readDescription(dir)
	if desc != "Short description." {
		t.Errorf("readDescription = %q, want %q", desc, "Short description.")
	}
}

func TestReadDescription_SkipsHorizontalRuleDashes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# My Project\n\n---\n\nA CLI tool for generating things.\n")

	desc := readDescription(dir)
	if desc != "A CLI tool for generating things." {
		t.Errorf("readDescription = %q, want %q", desc, "A CLI tool for generating things.")
	}
}

func TestReadDescription_SkipsHorizontalRuleAsterisks(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# My Project\n\n***\n\nA great library.\n")

	desc := readDescription(dir)
	if desc != "A great library." {
		t.Errorf("readDescription = %q, want %q", desc, "A great library.")
	}
}

func TestReadDescription_SkipsHorizontalRuleUnderscores(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# My Project\n\n___\n\nA useful tool.\n")

	desc := readDescription(dir)
	if desc != "A useful tool." {
		t.Errorf("readDescription = %q, want %q", desc, "A useful tool.")
	}
}

func TestReadDescription_SkipsTableRows(t *testing.T) {
	dir := t.TempDir()
	readme := "# My Project\n\n| CI | Docs |\n| -- | ---- |\n\nLightweight REST framework.\n"
	writeFile(t, dir, "README.md", readme)

	desc := readDescription(dir)
	if desc != "Lightweight REST framework." {
		t.Errorf("readDescription = %q, want %q", desc, "Lightweight REST framework.")
	}
}

func TestReadDescription_SkipsCodeFenceBackticks(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# My Project\n\n```\ncode here\n```\n\nProject description.\n")

	desc := readDescription(dir)
	if desc != "Project description." {
		t.Errorf("readDescription = %q, want %q", desc, "Project description.")
	}
}

func TestReadDescription_SkipsCodeFenceTildes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "README.md", "# My Project\n\n~~~\ncode here\n~~~\n\nProject description.\n")

	desc := readDescription(dir)
	if desc != "Project description." {
		t.Errorf("readDescription = %q, want %q", desc, "Project description.")
	}
}

// --- buildDomains ---

func TestBuildDomains_Empty(t *testing.T) {
	domains := buildDomains(map[string][]string{})
	if len(domains) != 0 {
		t.Errorf("buildDomains with empty map = %v, want empty", domains)
	}
}

func TestBuildDomains_RootLevelFilesNamedRoot(t *testing.T) {
	domains := buildDomains(map[string][]string{
		"": {"README.md", "main.go"},
	})
	if len(domains) != 1 {
		t.Fatalf("buildDomains len = %d, want 1", len(domains))
	}
	if domains[0].Name != "Root" {
		t.Errorf("domain name = %q, want %q", domains[0].Name, "Root")
	}
	if len(domains[0].KeyFiles) != 2 {
		t.Errorf("KeyFiles len = %d, want 2", len(domains[0].KeyFiles))
	}
}

func TestBuildDomains_DomainsAreSortedAlphabetically(t *testing.T) {
	domains := buildDomains(map[string][]string{
		"zzz": {"zzz/a.go"},
		"aaa": {"aaa/b.go"},
		"mmm": {"mmm/c.go"},
	})
	if len(domains) != 3 {
		t.Fatalf("buildDomains len = %d, want 3", len(domains))
	}
	if domains[0].Name != "aaa" || domains[1].Name != "mmm" || domains[2].Name != "zzz" {
		t.Errorf("domains not sorted alphabetically: got %q, %q, %q",
			domains[0].Name, domains[1].Name, domains[2].Name)
	}
}

// --- detectExternalDeps ---

// containsDep is a helper that reports whether name appears in deps.
func containsDep(deps []string, name string) bool {
	for _, d := range deps {
		if d == name {
			return true
		}
	}
	return false
}

func TestDetectExternalDeps_NoFiles(t *testing.T) {
	dir := t.TempDir()
	deps := detectExternalDeps(dir)
	if len(deps) != 0 {
		t.Errorf("expected no deps for empty dir, got %v", deps)
	}
}

func TestDetectExternalDeps_GoMod_SingleLineRequire(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", `module example.com/myapp

go 1.22

require github.com/gin-gonic/gin v1.9.1
require github.com/stretchr/testify v1.8.4
`)
	deps := detectExternalDeps(dir)
	if !containsDep(deps, "gin") {
		t.Errorf("expected 'gin' in deps, got %v", deps)
	}
	if !containsDep(deps, "testify") {
		t.Errorf("expected 'testify' in deps, got %v", deps)
	}
}

func TestDetectExternalDeps_GoMod_BlockRequire(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", `module example.com/myapp

go 1.22

require (
	github.com/pkg/errors v0.9.1
	golang.org/x/sync v0.5.0
	github.com/uber/zap v1.26.0 // indirect
)
`)
	deps := detectExternalDeps(dir)
	if !containsDep(deps, "errors") {
		t.Errorf("expected 'errors' in deps, got %v", deps)
	}
	if !containsDep(deps, "sync") {
		t.Errorf("expected 'sync' in deps, got %v", deps)
	}
	if !containsDep(deps, "zap") {
		t.Errorf("expected 'zap' (indirect) in deps, got %v", deps)
	}
}

func TestDetectExternalDeps_GoMod_SkipsOwnModule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", `module github.com/myorg/myapp

go 1.22

require (
	github.com/myorg/myapp v0.0.0
	github.com/some/dep v1.0.0
)
`)
	deps := detectExternalDeps(dir)
	// "myapp" is the own module's last segment — must be excluded
	if containsDep(deps, "myapp") {
		t.Errorf("own module 'myapp' should not appear in deps, got %v", deps)
	}
	if !containsDep(deps, "dep") {
		t.Errorf("expected 'dep' in deps, got %v", deps)
	}
}

func TestDetectExternalDeps_GoMod_LastSegmentUsed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", `module example.com/myapp

require github.com/foo/bar/baz v1.0.0
`)
	deps := detectExternalDeps(dir)
	// Only the last segment "baz" should appear, not "bar" or "foo"
	if !containsDep(deps, "baz") {
		t.Errorf("expected last segment 'baz' in deps, got %v", deps)
	}
	if containsDep(deps, "bar") || containsDep(deps, "foo") {
		t.Errorf("only last path segment should appear, got %v", deps)
	}
}

func TestDetectExternalDeps_PackageJSON_Dependencies(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{
  "name": "my-app",
  "dependencies": {
    "react": "^18.0.0",
    "axios": "^1.4.0"
  }
}`)
	deps := detectExternalDeps(dir)
	if !containsDep(deps, "react") {
		t.Errorf("expected 'react' in deps, got %v", deps)
	}
	if !containsDep(deps, "axios") {
		t.Errorf("expected 'axios' in deps, got %v", deps)
	}
}

func TestDetectExternalDeps_PackageJSON_DevDependencies(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{
  "devDependencies": {
    "jest": "^29.0.0",
    "typescript": "^5.0.0"
  }
}`)
	deps := detectExternalDeps(dir)
	if !containsDep(deps, "jest") {
		t.Errorf("expected 'jest' in deps, got %v", deps)
	}
	if !containsDep(deps, "typescript") {
		t.Errorf("expected 'typescript' in deps, got %v", deps)
	}
}

func TestDetectExternalDeps_PackageJSON_Deduplication(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{
  "dependencies": {
    "lodash": "^4.0.0"
  },
  "devDependencies": {
    "lodash": "^4.0.0"
  }
}`)
	deps := detectExternalDeps(dir)
	count := 0
	for _, d := range deps {
		if d == "lodash" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("'lodash' should appear exactly once, but found %d times in %v", count, deps)
	}
}

func TestDetectExternalDeps_PackageJSON_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{ this is not valid JSON `)
	// Must not panic; just returns empty
	deps := detectExternalDeps(dir)
	if len(deps) != 0 {
		t.Errorf("invalid JSON should produce no deps, got %v", deps)
	}
}

func TestDetectExternalDeps_RequirementsTxt_VersionSpecifiers(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "requirements.txt", `requests>=2.28.0
flask==2.3.0
numpy~=1.24.0
click!=8.0.0
scipy>1.0
`)
	deps := detectExternalDeps(dir)
	for _, want := range []string{"requests", "flask", "numpy", "click", "scipy"} {
		if !containsDep(deps, want) {
			t.Errorf("expected %q in deps, got %v", want, deps)
		}
	}
}

func TestDetectExternalDeps_RequirementsTxt_Extras(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "requirements.txt", `requests[security]>=2.28.0
uvicorn[standard]
`)
	deps := detectExternalDeps(dir)
	if !containsDep(deps, "requests") {
		t.Errorf("expected 'requests' (extras stripped) in deps, got %v", deps)
	}
	if !containsDep(deps, "uvicorn") {
		t.Errorf("expected 'uvicorn' (extras stripped) in deps, got %v", deps)
	}
}

func TestDetectExternalDeps_RequirementsTxt_PEP508DirectURL(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "requirements.txt", `my-package @ https://files.pythonhosted.org/packages/my-package-1.0.tar.gz
flask==2.3.0
`)
	deps := detectExternalDeps(dir)
	if !containsDep(deps, "my-package") {
		t.Errorf("expected 'my-package' (URL stripped) in deps, got %v", deps)
	}
	if containsDep(deps, "https://files.pythonhosted.org/packages/my-package-1.0.tar.gz") {
		t.Errorf("expected URL to be stripped from deps, got %v", deps)
	}
	if !containsDep(deps, "flask") {
		t.Errorf("expected 'flask' in deps, got %v", deps)
	}
}

func TestDetectExternalDeps_RequirementsTxt_SkipsCommentsAndFlags(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "requirements.txt", `# This is a comment
-r other-requirements.txt
-i https://pypi.org/simple
flask==2.3.0
`)
	deps := detectExternalDeps(dir)
	if containsDep(deps, "#") || containsDep(deps, "-r") || containsDep(deps, "-i") {
		t.Errorf("comment and flag lines should be skipped, got %v", deps)
	}
	if !containsDep(deps, "flask") {
		t.Errorf("expected 'flask' in deps, got %v", deps)
	}
}

func TestDetectExternalDeps_CargoToml_Dependencies(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Cargo.toml", `[package]
name = "my-crate"
version = "0.1.0"

[dependencies]
serde = "1.0"
tokio = { version = "1", features = ["full"] }

[dev-dependencies]
mockall = "0.11"

[build-dependencies]
cc = "1.0"
`)
	deps := detectExternalDeps(dir)
	for _, want := range []string{"serde", "tokio", "mockall", "cc"} {
		if !containsDep(deps, want) {
			t.Errorf("expected %q in deps, got %v", want, deps)
		}
	}
	// [package] section keys must not appear
	if containsDep(deps, "name") || containsDep(deps, "version") {
		t.Errorf("keys from non-dep sections should not appear, got %v", deps)
	}
}

func TestDetectExternalDeps_CargoToml_SkipsHashLines(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Cargo.toml", `[dependencies]
# this is a comment = "value"
serde = "1.0"
`)
	deps := detectExternalDeps(dir)
	if containsDep(deps, "# this is a comment") {
		t.Errorf("comment lines should be skipped, got %v", deps)
	}
	if !containsDep(deps, "serde") {
		t.Errorf("expected 'serde' in deps, got %v", deps)
	}
}

func TestDetectExternalDeps_CargoToml_StopsAtNextSection(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Cargo.toml", `[dependencies]
serde = "1.0"

[profile.release]
opt-level = 3
`)
	deps := detectExternalDeps(dir)
	if containsDep(deps, "opt-level") {
		t.Errorf("keys under [profile.release] should not be collected, got %v", deps)
	}
	if !containsDep(deps, "serde") {
		t.Errorf("expected 'serde' in deps, got %v", deps)
	}
}

func TestDetectExternalDeps_Gemfile_SingleQuotes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Gemfile", `source 'https://rubygems.org'

gem 'rails', '~> 7.0'
gem 'puma'
`)
	deps := detectExternalDeps(dir)
	if !containsDep(deps, "rails") {
		t.Errorf("expected 'rails' in deps, got %v", deps)
	}
	if !containsDep(deps, "puma") {
		t.Errorf("expected 'puma' in deps, got %v", deps)
	}
}

func TestDetectExternalDeps_Gemfile_DoubleQuotes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Gemfile", `source "https://rubygems.org"

gem "sidekiq", "~> 7.0"
gem "devise"
`)
	deps := detectExternalDeps(dir)
	if !containsDep(deps, "sidekiq") {
		t.Errorf("expected 'sidekiq' in deps, got %v", deps)
	}
	if !containsDep(deps, "devise") {
		t.Errorf("expected 'devise' in deps, got %v", deps)
	}
}

func TestDetectExternalDeps_Gemfile_SkipsNonGemLines(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "Gemfile", `source 'https://rubygems.org'
ruby '3.2.0'
group :development do
  gem 'rubocop'
end
`)
	deps := detectExternalDeps(dir)
	// 'source', 'ruby', 'group' lines must not produce deps
	if containsDep(deps, "https://rubygems.org") || containsDep(deps, "3.2.0") {
		t.Errorf("non-gem lines should be skipped, got %v", deps)
	}
	if !containsDep(deps, "rubocop") {
		t.Errorf("expected 'rubocop' in deps, got %v", deps)
	}
}

func TestDetectExternalDeps_PyprojectToml_PoetryDependencies(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pyproject.toml", `[tool.poetry]
name = "myapp"

[tool.poetry.dependencies]
python = "^3.11"
fastapi = "^0.100.0"
pydantic = "^2.0"

[tool.poetry.dev-dependencies]
pytest = "^7.4"
`)
	deps := detectExternalDeps(dir)
	// python must be skipped
	if containsDep(deps, "python") {
		t.Errorf("'python' key should be skipped, got %v", deps)
	}
	for _, want := range []string{"fastapi", "pydantic", "pytest"} {
		if !containsDep(deps, want) {
			t.Errorf("expected %q in deps, got %v", want, deps)
		}
	}
}

func TestDetectExternalDeps_PyprojectToml_ProjectDependencies(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pyproject.toml", `[build-system]
requires = ["setuptools"]

[project]
name = "myapp"
dependencies = [
    "httpx>=0.24",
    "rich",
    "typer[all]>=0.9",
]
`)
	deps := detectExternalDeps(dir)
	for _, want := range []string{"httpx", "rich", "typer"} {
		if !containsDep(deps, want) {
			t.Errorf("expected %q in deps, got %v", want, deps)
		}
	}
}

func TestDetectExternalDeps_PyprojectToml_SkipsPythonKey(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pyproject.toml", `[tool.poetry.dependencies]
python = ">=3.9"
requests = "^2.28"
`)
	deps := detectExternalDeps(dir)
	if containsDep(deps, "python") {
		t.Errorf("'python' key should always be skipped, got %v", deps)
	}
	if !containsDep(deps, "requests") {
		t.Errorf("expected 'requests' in deps, got %v", deps)
	}
}

func TestDetectExternalDeps_RequirementsTxt_EnvMarkers(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "requirements.txt", `requests; python_version >= "3.0"
Flask; python_version < "3.8"
numpy
`)
	deps := detectExternalDeps(dir)
	if !containsDep(deps, "requests") {
		t.Errorf("expected 'requests' (env marker stripped) in deps, got %v", deps)
	}
	if !containsDep(deps, "Flask") {
		t.Errorf("expected 'Flask' (env marker stripped) in deps, got %v", deps)
	}
	if !containsDep(deps, "numpy") {
		t.Errorf("expected 'numpy' in deps, got %v", deps)
	}
	for _, d := range deps {
		if strings.Contains(d, "python_version") {
			t.Errorf("env marker text should be stripped, but got %q in deps", d)
		}
	}
}

func TestDetectExternalDeps_RequirementsTxt_InlineComments(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "requirements.txt", `requests  # needed for API
flask>=2.0  # web framework
numpy
`)
	deps := detectExternalDeps(dir)
	if !containsDep(deps, "requests") {
		t.Errorf("expected 'requests' (inline comment stripped) in deps, got %v", deps)
	}
	if !containsDep(deps, "flask") {
		t.Errorf("expected 'flask' (inline comment stripped) in deps, got %v", deps)
	}
	if !containsDep(deps, "numpy") {
		t.Errorf("expected 'numpy' in deps, got %v", deps)
	}
	for _, d := range deps {
		if strings.Contains(d, "#") {
			t.Errorf("inline comment text should be stripped, but got %q in deps", d)
		}
	}
}

func TestDetectExternalDeps_PyprojectToml_EnvMarkers(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pyproject.toml", `[project]
dependencies = [
    "requests; python_version >= '3.0'",
    "Flask; python_version < '3.8'",
    "numpy",
]
`)
	deps := detectExternalDeps(dir)
	if !containsDep(deps, "requests") {
		t.Errorf("expected 'requests' (env marker stripped) in deps, got %v", deps)
	}
	if !containsDep(deps, "Flask") {
		t.Errorf("expected 'Flask' (env marker stripped) in deps, got %v", deps)
	}
	for _, d := range deps {
		if strings.Contains(d, "python_version") {
			t.Errorf("env marker text should be stripped, but got %q in deps", d)
		}
	}
}

func TestDetectExternalDeps_PyprojectToml_InlineComments(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pyproject.toml", `[project]
dependencies = [
    "requests # needed for API",
    "flask>=2.0 # web framework",
]
`)
	deps := detectExternalDeps(dir)
	if !containsDep(deps, "requests") {
		t.Errorf("expected 'requests' (inline comment stripped) in deps, got %v", deps)
	}
	if !containsDep(deps, "flask") {
		t.Errorf("expected 'flask' (inline comment stripped) in deps, got %v", deps)
	}
	for _, d := range deps {
		if strings.Contains(d, "#") {
			t.Errorf("inline comment text should be stripped, but got %q in deps", d)
		}
	}
}

// TestDetectExternalDeps_PackageJSON_RuntimeDepsPreferredOverDevDeps verifies
// that runtime dependencies are preferred over devDependencies when filling the
// 15-dep cap. Without this, packages like @babel/*, @types/*, @eslint/* would
// dominate because they sort alphabetically before runtime deps.
func TestDetectExternalDeps_PackageJSON_RuntimeDepsPreferredOverDevDeps(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{
  "dependencies": {
    "react": "^18.0.0",
    "express": "^4.18.0",
    "axios": "^1.4.0"
  },
  "devDependencies": {
    "@babel/core": "^7.0.0",
    "@babel/preset-env": "^7.0.0",
    "@babel/preset-react": "^7.0.0",
    "@eslint/js": "^8.0.0",
    "@types/node": "^20.0.0",
    "@types/react": "^18.0.0",
    "@types/express": "^4.0.0",
    "@vitejs/plugin-react": "^4.0.0",
    "@testing-library/react": "^14.0.0",
    "@testing-library/jest-dom": "^6.0.0",
    "typescript": "^5.0.0",
    "vite": "^5.0.0",
    "eslint": "^8.0.0"
  }
}`)
	deps := detectExternalDeps(dir)

	// All runtime deps must appear even though devDeps sort before them.
	for _, want := range []string{"react", "express", "axios"} {
		if !containsDep(deps, want) {
			t.Errorf("expected runtime dep %q in deps, got %v", want, deps)
		}
	}
	if len(deps) > 15 {
		t.Errorf("deps len = %d, want ≤15", len(deps))
	}
}

func TestDetectExternalDeps_CapAt15(t *testing.T) {
	dir := t.TempDir()
	// Write a requirements.txt with 20 distinct packages.
	lines := ""
	for i := 0; i < 20; i++ {
		lines += fmt.Sprintf("package%02d\n", i)
	}
	writeFile(t, dir, "requirements.txt", lines)

	deps := detectExternalDeps(dir)
	if len(deps) > 15 {
		t.Errorf("deps len = %d, want ≤15 (maxExternalDeps cap)", len(deps))
	}
}

func TestDetectExternalDeps_CrossManifestDeduplication(t *testing.T) {
	dir := t.TempDir()
	// "requests" appears in both requirements.txt and pyproject.toml
	writeFile(t, dir, "requirements.txt", "requests>=2.28\n")
	writeFile(t, dir, "pyproject.toml", `[tool.poetry.dependencies]
requests = "^2.28"
`)
	deps := detectExternalDeps(dir)
	count := 0
	for _, d := range deps {
		if d == "requests" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("'requests' should appear exactly once across manifests, but found %d times in %v", count, deps)
	}
}

// --- git-tracked dotfiles ---

// TestBuildProjectGraph_IncludesGitTrackedDotfiles verifies that dotfiles
// explicitly committed to git (e.g. .eslintrc.json, .prettierrc) are counted
// in local mode, while untracked dotfiles (e.g. .env) are still excluded.
func TestBuildProjectGraph_IncludesGitTrackedDotfiles(t *testing.T) {
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")

	writeFile(t, dir, ".eslintrc.json", `{"semi": false}`)
	writeFile(t, dir, ".env", "SECRET=value\n")
	writeFile(t, dir, "main.go", "package main\n")

	// Track .eslintrc.json and main.go; leave .env untracked.
	run("add", ".eslintrc.json", "main.go")
	run("commit", "-m", "initial")

	graph, err := BuildProjectGraph(context.Background(), dir, "proj")
	if err != nil {
		t.Fatalf("BuildProjectGraph: %v", err)
	}

	// main.go + .eslintrc.json = 2 files; .env must be excluded.
	if graph.Stats.TotalFiles != 2 {
		t.Errorf("TotalFiles = %d, want 2 (.eslintrc.json tracked, .env untracked)", graph.Stats.TotalFiles)
	}

	// Verify .eslintrc.json appears somewhere in the domain key files.
	found := false
	for _, d := range graph.Domains {
		for _, f := range d.KeyFiles {
			if f == ".eslintrc.json" {
				found = true
			}
		}
	}
	if !found {
		t.Error(".eslintrc.json (git-tracked dotfile) not found in any domain key files")
	}
}
