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
	"github.com/supermodeltools/uncompact/internal/fsutil"
)

// deepDirs are top-level directories that should be grouped at two levels deep
// (dir/subdir) instead of just dir, to preserve per-package structure.
var deepDirs = map[string]bool{
	// Go / generic
	"internal": true,
	"src":      true,
	"pkg":      true,
	"lib":      true,
	"app":      true,
	"cmd":      true,
	// frontend / full-stack
	"pages":       true, // Next.js, Ruby on Rails views
	"routes":      true, // Remix, Express, React Router
	"components":  true, // React, Vue, Angular
	"hooks":       true, // React custom hooks
	"store":       true, // Redux, Vuex, Zustand
	"features":    true, // feature-slice design
	"views":       true, // MVC views
	"containers":  true, // React container pattern
	"screens":     true, // React Native
	"api":         true, // API route handlers
	"controllers": true, // MVC controllers
	"services":    true, // service layer
	"middleware":  true, // Express/Koa/Django middleware
	"handlers":    true, // HTTP handlers
}

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
	"Pods":         true, // iOS / CocoaPods
	"elm-stuff":    true, // Elm build cache
	"_build":       true, // Elixir / OCaml build output
	"env":          true, // Python virtualenv (alt to venv)
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

	// Build the set of git-tracked/unignored files so we can include dotfiles
	// that are explicitly committed (e.g. .eslintrc.json, .prettierrc).
	// gitFiles is nil when git is unavailable; in that case we fall back to
	// skipping all dot-prefixed files for safety.
	gitFiles := fsutil.BuildGitFileSet(ctx, rootDir)

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

		// Skip symlinks to avoid including files outside the repo root.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return nil
		}

		// Skip hidden files (e.g. .env, .npmrc, .netrc) unless they are explicitly
		// git-tracked. When git is unavailable (gitFiles is nil) we fall back to
		// skipping all dot-prefixed files for safety. Tracked dotfiles such as
		// .eslintrc.json, .prettierrc, and .editorconfig are intentionally
		// included because they provide valuable project context.
		if strings.HasPrefix(d.Name(), ".") && (gitFiles == nil || !gitFiles[filepath.ToSlash(rel)]) {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != "" {
			extCounts[ext]++
		}
		total++

		// Group by directory. For well-known aggregator dirs (e.g. "internal",
		// "src"), use a two-level key (dir/subdir) to preserve per-package
		// structure. Root-level files use an empty string key.
		parts := strings.SplitN(rel, string(filepath.Separator), 3)
		dir := ""
		if len(parts) > 1 {
			dir = parts[0]
			if deepDirs[dir] && len(parts) > 2 {
				dir = parts[0] + string(filepath.Separator) + parts[1]
			}
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
		// Return the first non-empty, non-heading line (the description may
		// appear before any heading, so we don't require a heading first).
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") {
				continue
			}
			if strings.HasPrefix(line, "[![") || strings.HasPrefix(line, "![") {
				continue
			}
			if line != "" && len(line) < 250 {
				return line
			}
		}
	}
	return ""
}

// buildDomains groups files by directory key and creates one Domain per group.
// Groups are capped at maxDomains to avoid overwhelming the context bomb.
func buildDomains(dirFiles map[string][]string) []api.Domain {
	const maxKeyFiles = 8
	const maxDomains = 20

	var dirs []string
	for dir := range dirFiles {
		dirs = append(dirs, dir)
	}
	// Sort by file count descending so the most substantial directories are
	// kept when the list is truncated. Use alphabetical order as a tiebreaker.
	sort.Slice(dirs, func(i, j int) bool {
		ci, cj := len(dirFiles[dirs[i]]), len(dirFiles[dirs[j]])
		if ci != cj {
			return ci > cj // more files = higher priority
		}
		return dirs[i] < dirs[j] // alphabetical tiebreak
	})

	// Cap the number of directories to keep the domain list manageable.
	if len(dirs) > maxDomains {
		dirs = dirs[:maxDomains]
	}

	var domains []api.Domain
	for _, dir := range dirs {
		files := dirFiles[dir]
		sort.Slice(files, func(i, j int) bool {
			pi, pj := entryPointPriority(files[i]), entryPointPriority(files[j])
			if pi != pj {
				return pi > pj
			}
			li, lj := len(files[i]), len(files[j])
			if li != lj {
				return li < lj
			}
			return files[i] < files[j]
		})

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

// computeTopFiles picks the top key files across all domains.
// In local mode, domain co-occurrence always yields a count of 1 (each file belongs to
// exactly one domain), so RelationshipCount is left at 0 to suppress meaningless output.
// Files are ranked by a simple heuristic: well-known entry-point names first, then by
// path length (shorter = closer to root), then alphabetically.
func computeTopFiles(domains []api.Domain, n int) []api.CriticalFile {
	seen := make(map[string]struct{})
	var files []api.CriticalFile
	for _, d := range domains {
		for _, f := range d.KeyFiles {
			if _, ok := seen[f]; ok {
				continue
			}
			seen[f] = struct{}{}
			files = append(files, api.CriticalFile{Path: f, RelationshipCount: 0})
		}
	}
	sort.Slice(files, func(i, j int) bool {
		pi, pj := entryPointPriority(files[i].Path), entryPointPriority(files[j].Path)
		if pi != pj {
			return pi > pj
		}
		li, lj := len(files[i].Path), len(files[j].Path)
		if li != lj {
			return li < lj
		}
		return files[i].Path < files[j].Path
	})
	if len(files) > n {
		files = files[:n]
	}
	return files
}

// entryPointPriority returns a higher score for well-known entry-point filenames.
func entryPointPriority(path string) int {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	switch strings.ToLower(name) {
	case "main":
		return 4
	case "app", "application":
		return 3
	case "server", "index":
		return 2
	case "init", "__init__":
		return 1
	}
	return 0
}
