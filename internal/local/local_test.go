package local

import (
	"context"
	"fmt"
	"os"
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

func TestReadDescription_IgnoresLongLines(t *testing.T) {
	dir := t.TempDir()
	longLine := strings.Repeat("x", 300) // > 250 chars, should be skipped
	writeFile(t, dir, "README.md", longLine+"\nShort description.\n")

	desc := readDescription(dir)
	if desc != "Short description." {
		t.Errorf("readDescription = %q, want %q", desc, "Short description.")
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
