// Package local provides local-only (no API) project graph generation.
// It scans the repository file tree to produce a ProjectGraph without
// making any external network calls, enabling Uncompact to work without
// an API key configured.
package local

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/supermodeltools/uncompact/internal/api"
)

// ignoreDirs are directory names excluded from analysis.
var ignoreDirs = map[string]bool{
	".git":         true,
	".svn":         true,
	".hg":          true,
	"node_modules": true,
	"vendor":       true,
	"__pycache__":  true,
	".cache":       true,
	"dist":         true,
	"build":        true,
	"target":       true,
	".tox":         true,
	"venv":         true,
	".venv":        true,
	"coverage":     true,
	".nyc_output":  true,
	"out":          true,
	".next":        true,
	".nuxt":        true,
	".turbo":       true,
}

// extToLanguage maps common file extensions to language names.
var extToLanguage = map[string]string{
	".go":    "Go",
	".js":    "JavaScript",
	".ts":    "TypeScript",
	".tsx":   "TypeScript",
	".jsx":   "JavaScript",
	".py":    "Python",
	".rb":    "Ruby",
	".rs":    "Rust",
	".java":  "Java",
	".kt":    "Kotlin",
	".swift": "Swift",
	".cs":    "C#",
	".cpp":   "C++",
	".c":     "C",
	".h":     "C",
	".php":   "PHP",
	".scala": "Scala",
	".elm":   "Elm",
	".ex":    "Elixir",
	".exs":   "Elixir",
	".sh":    "Shell",
	".bash":  "Shell",
	".zig":   "Zig",
	".lua":   "Lua",
	".r":     "R",
	".jl":    "Julia",
}

// BuildProjectGraph generates a ProjectGraph from local repository analysis
// without calling any external APIs. It scans the file tree, groups files by
// top-level directory to form domains, and reads README for a description.
func BuildProjectGraph(ctx context.Context, rootDir, projectName string) (*api.ProjectGraph, error) {
	extCounts, dirFiles, totalFiles, err := collectFiles(ctx, rootDir)
	if err != nil {
		return nil, err
	}

	lang, languages := detectLanguages(extCounts)
	desc := readDescription(rootDir)
	domains := buildDomains(dirFiles)

	graph := &api.ProjectGraph{
		Name:        projectName,
		Language:    lang,
		Description: desc,
		Domains:     domains,
		Stats: api.Stats{
			TotalFiles: totalFiles,
			Languages:  languages,
		},
		UpdatedAt: time.Now(),
	}
	graph.CriticalFiles = computeTopFiles(graph.Domains, 10)
	return graph, nil
}

// ReadClaudeMD reads the contents of CLAUDE.md from the project root.
// Returns empty string if not found or unreadable.
func ReadClaudeMD(rootDir string) string {
	data, err := os.ReadFile(filepath.Join(rootDir, "CLAUDE.md"))
	if err != nil {
		return ""
	}
	content := strings.TrimSpace(string(data))
	// Limit to a reasonable size to avoid bloating the context bomb.
	const maxRunes = 3000
	runes := []rune(content)
	if len(runes) > maxRunes {
		content = string(runes[:maxRunes]) + "\n\n*(CLAUDE.md truncated — showing first 3000 chars)*"
	}
	return content
}

// collectFiles walks the file tree and returns extension counts, files per top-level
// directory, and the total file count. Returns an error if the walk is interrupted
// (e.g. due to context cancellation).
func collectFiles(ctx context.Context, rootDir string) (extCounts map[string]int, dirFiles map[string][]string, total int, err error) {
	extCounts = make(map[string]int)
	dirFiles = make(map[string][]string)

	walkErr := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if d.IsDir() {
			name := d.Name()
			if ignoreDirs[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip hidden files (e.g. .env, .npmrc, .netrc).
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}

		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != "" {
			extCounts[ext]++
		}
		total++

		// Group by top-level directory; root-level files use an empty string key.
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		dir := ""
		if len(parts) > 1 {
			dir = parts[0]
		}
		dirFiles[dir] = append(dirFiles[dir], rel)

		return nil
	})

	return extCounts, dirFiles, total, walkErr
}

// detectLanguages returns the primary language and an ordered list of languages
// detected from file extension counts.
func detectLanguages(extCounts map[string]int) (primary string, languages []string) {
	langCounts := make(map[string]int)
	for ext, count := range extCounts {
		if lang, ok := extToLanguage[ext]; ok {
			langCounts[lang] += count
		}
	}

	type lc struct {
		lang  string
		count int
	}
	var sorted []lc
	for lang, count := range langCounts {
		sorted = append(sorted, lc{lang, count})
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].count != sorted[j].count {
			return sorted[i].count > sorted[j].count
		}
		return sorted[i].lang < sorted[j].lang
	})

	for _, item := range sorted {
		languages = append(languages, item.lang)
	}
	if len(languages) > 0 {
		primary = languages[0]
	}
	if len(languages) > 5 {
		languages = languages[:5]
	}
	return primary, languages
}

// readDescription attempts to extract a one-line project description from README.md.
func readDescription(rootDir string) string {
	for _, name := range []string{"README.md", "readme.md", "README.txt"} {
		data, err := os.ReadFile(filepath.Join(rootDir, name))
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		// Skip the first heading, return the next non-empty non-heading line
		// (which is typically the one-liner description following the title).
		pastHeading := false
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") {
				pastHeading = true
				continue
			}
			if line != "" && len(line) < 250 {
				if pastHeading {
					return line
				}
			}
		}
		break
	}
	return ""
}

// buildDomains groups files by top-level directory and creates one Domain per group.
func buildDomains(dirFiles map[string][]string) []api.Domain {
	const maxKeyFiles = 8

	var dirs []string
	for dir := range dirFiles {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	var domains []api.Domain
	for _, dir := range dirs {
		files := dirFiles[dir]
		sort.Strings(files)

		keyFiles := files
		if len(keyFiles) > maxKeyFiles {
			keyFiles = keyFiles[:maxKeyFiles]
		}

		name := dir
		if name == "" {
			name = "Root"
		}

		desc := fmt.Sprintf("%d file(s)", len(files))

		domains = append(domains, api.Domain{
			Name:        name,
			Description: desc,
			KeyFiles:    keyFiles,
		})
	}
	return domains
}

// computeTopFiles picks the most-referenced key files across all domains.
func computeTopFiles(domains []api.Domain, n int) []api.CriticalFile {
	counts := make(map[string]int)
	for _, d := range domains {
		for _, f := range d.KeyFiles {
			counts[f]++
		}
	}
	files := make([]api.CriticalFile, 0, len(counts))
	for path, count := range counts {
		files = append(files, api.CriticalFile{Path: path, RelationshipCount: count})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].RelationshipCount != files[j].RelationshipCount {
			return files[i].RelationshipCount > files[j].RelationshipCount
		}
		return files[i].Path < files[j].Path
	})
	if len(files) > n {
		files = files[:n]
	}
	return files
}
