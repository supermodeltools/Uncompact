package detect

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/supermodeltools/uncompact/internal/fsutil"
)

// RepoInfo holds all detected information about the repository.
type RepoInfo struct {
	ProjectName string
	Language    string
	Version     string
	Module      string // e.g. Go module path
	BuildCmd    string
	LintCmd     string
	TestCmd     string
	CodeStyle   string
}

// LanguageSummary returns a human-readable language + version string.
func (r *RepoInfo) LanguageSummary() string {
	var parts []string
	if r.Language != "" {
		lang := r.Language
		if r.Version != "" {
			lang += " " + r.Version
		}
		parts = append(parts, lang)
	}
	if r.Module != "" {
		parts = append(parts, "module "+r.Module)
	}
	if len(parts) == 0 {
		return "Unknown"
	}
	return strings.Join(parts, ", ")
}

// Analyze examines dir and returns detected repository information.
func Analyze(dir string) *RepoInfo {
	info := &RepoInfo{
		ProjectName: filepath.Base(dir),
	}

	switch {
	case fsutil.FileExists(filepath.Join(dir, "go.mod")):
		analyzeGo(dir, info)
	case fsutil.FileExists(filepath.Join(dir, "package.json")):
		analyzeNode(dir, info)
	case fsutil.FileExists(filepath.Join(dir, "Cargo.toml")):
		analyzeRust(dir, info)
	case fsutil.FileExists(filepath.Join(dir, "pyproject.toml")),
		fsutil.FileExists(filepath.Join(dir, "requirements.txt")),
		fsutil.FileExists(filepath.Join(dir, "setup.py")),
		fsutil.FileExists(filepath.Join(dir, "setup.cfg")):
		analyzePython(dir, info)
	default:
		info.Language = "Unknown"
	}

	return info
}

// makefileHasTarget returns true if a Makefile in dir has a target named target.
func makefileHasTarget(dir, target string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "Makefile"))
	if err != nil {
		return false
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, target+":") || strings.HasPrefix(line, target+" ") {
			return true
		}
	}
	return false
}

// hasGoTestFiles returns true if any *_test.go files exist under dir.
func hasGoTestFiles(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if found {
			return filepath.SkipAll
		}
		if d.IsDir() {
			name := d.Name()
			if name == "vendor" || name == ".git" || name == "testdata" ||
				name == "node_modules" || name == "dist" || name == "build" ||
				name == "target" || name == "__pycache__" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), "_test.go") {
			found = true
		}
		return nil
	})
	return found
}

func analyzeGo(dir string, info *RepoInfo) {
	info.Language = "Go"

	// Parse go.mod for module name and Go version.
	if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "module ") {
				mod := strings.TrimSpace(strings.TrimPrefix(line, "module "))
				info.Module = mod
				parts := strings.Split(mod, "/")
				info.ProjectName = parts[len(parts)-1]
			} else if strings.HasPrefix(line, "go ") {
				info.Version = strings.TrimSpace(strings.TrimPrefix(line, "go "))
			}
		}
	}

	// Build command.
	if makefileHasTarget(dir, "build") {
		info.BuildCmd = "make build"
	} else {
		info.BuildCmd = "go build ./..."
	}

	// Lint command.
	hasGolangCI := fsutil.FileExists(filepath.Join(dir, ".golangci.yml")) ||
		fsutil.FileExists(filepath.Join(dir, ".golangci.yaml")) ||
		fsutil.FileExists(filepath.Join(dir, ".golangci.json")) ||
		fsutil.FileExists(filepath.Join(dir, ".golangci.toml"))
	if hasGolangCI {
		info.LintCmd = "golangci-lint run"
		info.CodeStyle = "Uses golangci-lint for style enforcement. Run `golangci-lint run` before committing."
	} else if makefileHasTarget(dir, "lint") {
		info.LintCmd = "make lint"
		info.CodeStyle = "Follow standard Go conventions. Use `gofmt` for formatting."
	} else {
		info.LintCmd = "go vet ./..."
		info.CodeStyle = "Follow standard Go conventions. Use `gofmt` for formatting."
	}

	// Test command.
	if hasGoTestFiles(dir) {
		if makefileHasTarget(dir, "test") {
			info.TestCmd = "make test"
		} else {
			info.TestCmd = "go test ./..."
		}
	}
}

func analyzeNode(dir string, info *RepoInfo) {
	info.Language = "Node.js"

	// Detect package manager from lockfiles.
	var pkgMgr string
	switch {
	case fsutil.FileExists(filepath.Join(dir, "bun.lockb")):
		pkgMgr = "bun"
	case fsutil.FileExists(filepath.Join(dir, "pnpm-lock.yaml")):
		pkgMgr = "pnpm"
	case fsutil.FileExists(filepath.Join(dir, "yarn.lock")):
		pkgMgr = "yarn"
	default:
		pkgMgr = "npm"
	}

	// Parse package.json.
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var pkg struct {
			Name    string            `json:"name"`
			Scripts map[string]string `json:"scripts"`
			Engines struct {
				Node string `json:"node"`
			} `json:"engines"`
		}
		if json.Unmarshal(data, &pkg) == nil {
			if pkg.Name != "" {
				info.ProjectName = pkg.Name
			}
			if pkg.Engines.Node != "" {
				info.Version = pkg.Engines.Node
			}
			if _, ok := pkg.Scripts["build"]; ok {
				info.BuildCmd = pkgMgr + " run build"
			}
			if _, ok := pkg.Scripts["lint"]; ok {
				info.LintCmd = pkgMgr + " run lint"
			}
			if _, ok := pkg.Scripts["test"]; ok {
				info.TestCmd = pkgMgr + " test"
			}
		}
	}

	// Check .nvmrc for Node version.
	if info.Version == "" {
		if data, err := os.ReadFile(filepath.Join(dir, ".nvmrc")); err == nil {
			info.Version = strings.TrimSpace(string(data))
		}
	}

	// Detect linter from config files if not set from package.json scripts.
	if info.LintCmd == "" {
		hasESLint := fsutil.FileExists(filepath.Join(dir, ".eslintrc.js")) ||
			fsutil.FileExists(filepath.Join(dir, ".eslintrc.json")) ||
			fsutil.FileExists(filepath.Join(dir, ".eslintrc.yml")) ||
			fsutil.FileExists(filepath.Join(dir, ".eslintrc.yaml")) ||
			fsutil.FileExists(filepath.Join(dir, "eslint.config.js")) ||
			fsutil.FileExists(filepath.Join(dir, "eslint.config.mjs"))
		hasBiome := fsutil.FileExists(filepath.Join(dir, "biome.json")) ||
			fsutil.FileExists(filepath.Join(dir, "biome.jsonc"))
		if hasESLint {
			info.LintCmd = "npx eslint ."
		} else if hasBiome {
			info.LintCmd = "npx biome check ."
		}
	}

	// Detect formatter.
	hasPrettier := fsutil.FileExists(filepath.Join(dir, ".prettierrc")) ||
		fsutil.FileExists(filepath.Join(dir, ".prettierrc.json")) ||
		fsutil.FileExists(filepath.Join(dir, ".prettierrc.js")) ||
		fsutil.FileExists(filepath.Join(dir, "prettier.config.js")) ||
		fsutil.FileExists(filepath.Join(dir, "prettier.config.mjs"))
	if hasPrettier {
		info.CodeStyle = "Uses Prettier for formatting. Run `npx prettier --write .` before committing."
	}
}

func analyzeRust(dir string, info *RepoInfo) {
	info.Language = "Rust"

	// Parse Cargo.toml for package name and edition.
	if data, err := os.ReadFile(filepath.Join(dir, "Cargo.toml")); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		inPackage := false
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "[package]" {
				inPackage = true
				continue
			}
			if strings.HasPrefix(line, "[") {
				inPackage = false
			}
			if !inPackage {
				continue
			}
			if strings.HasPrefix(line, "name") && strings.Contains(line, "=") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					info.ProjectName = strings.Trim(strings.TrimSpace(parts[1]), `"`)
				}
			}
			if strings.HasPrefix(line, "edition") && strings.Contains(line, "=") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					info.Version = "edition " + strings.Trim(strings.TrimSpace(parts[1]), `"`)
				}
			}
		}
	}

	if makefileHasTarget(dir, "build") {
		info.BuildCmd = "make build"
	} else {
		info.BuildCmd = "cargo build"
	}
	info.LintCmd = "cargo clippy"
	info.TestCmd = "cargo test"
	info.CodeStyle = "Follow Rust conventions. Use `cargo fmt` for formatting and `cargo clippy` for linting."
}

func analyzePython(dir string, info *RepoInfo) {
	info.Language = "Python"

	// Check .python-version for interpreter version.
	if data, err := os.ReadFile(filepath.Join(dir, ".python-version")); err == nil {
		info.Version = strings.TrimSpace(string(data))
	}

	// Parse pyproject.toml.
	if data, err := os.ReadFile(filepath.Join(dir, "pyproject.toml")); err == nil {
		content := string(data)
		scanner := bufio.NewScanner(strings.NewReader(content))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "name") && strings.Contains(line, "=") && info.ProjectName == filepath.Base(dir) {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					name := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
					if name != "" {
						info.ProjectName = name
					}
				}
			}
			if strings.HasPrefix(line, "requires-python") && strings.Contains(line, "=") && info.Version == "" {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					v := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
					v = strings.TrimLeft(v, "><=!~^")
					if i := strings.IndexAny(v, ", "); i != -1 {
						v = v[:i]
					}
					info.Version = strings.TrimSpace(v)
				}
			}
		}
		if strings.Contains(content, "[tool.ruff]") || strings.Contains(content, `ruff`) {
			info.LintCmd = "ruff check ."
		}
		if strings.Contains(content, "pytest") {
			info.TestCmd = "pytest"
		}
		if strings.Contains(content, "uv") {
			info.CodeStyle = "Uses uv for dependency management. Run commands via `uv run`."
		}
	}

	// Fallback lint detection.
	if info.LintCmd == "" {
		if fsutil.FileExists(filepath.Join(dir, "ruff.toml")) || fsutil.FileExists(filepath.Join(dir, ".ruff.toml")) {
			info.LintCmd = "ruff check ."
		} else if fsutil.FileExists(filepath.Join(dir, ".flake8")) {
			info.LintCmd = "flake8"
		}
	}

	// Parse setup.cfg for project name and pytest configuration.
	if data, err := os.ReadFile(filepath.Join(dir, "setup.cfg")); err == nil {
		content := string(data)
		scanner := bufio.NewScanner(strings.NewReader(content))
		inMetadata := false
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "[metadata]" {
				inMetadata = true
				continue
			}
			if strings.HasPrefix(line, "[") {
				inMetadata = false
			}
			if inMetadata && strings.HasPrefix(line, "name") && strings.Contains(line, "=") && info.ProjectName == filepath.Base(dir) {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					name := strings.TrimSpace(parts[1])
					if name != "" {
						info.ProjectName = name
					}
				}
			}
		}
		if info.TestCmd == "" && (strings.Contains(content, "[tool:pytest]") || strings.Contains(content, "[pytest]")) {
			info.TestCmd = "pytest"
		}
	}

	// Test fallback.
	if info.TestCmd == "" {
		if fsutil.FileExists(filepath.Join(dir, "pytest.ini")) {
			info.TestCmd = "pytest"
		}
	}

	// Build.
	if makefileHasTarget(dir, "build") {
		info.BuildCmd = "make build"
	}
}

// GenerateCLAUDEMD produces the content for a CLAUDE.md file based on detected info.
func (r *RepoInfo) GenerateCLAUDEMD() string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "# %s — Claude Instructions\n\n", r.ProjectName)

	sb.WriteString("## Development\n\n")
	fmt.Fprintf(&sb, "- Language: %s\n", r.LanguageSummary())
	if r.BuildCmd != "" {
		fmt.Fprintf(&sb, "- Build: `%s`\n", r.BuildCmd)
	}
	if r.LintCmd != "" {
		fmt.Fprintf(&sb, "- Lint: `%s`\n", r.LintCmd)
	}
	if r.TestCmd != "" {
		fmt.Fprintf(&sb, "- Test: `%s`\n", r.TestCmd)
	} else {
		sb.WriteString("- Test: No test suite configured\n")
	}

	sb.WriteString("\n## Commits\n\n")
	sb.WriteString("Write clear, concise commit messages using the imperative mood.\n")
	sb.WriteString("Keep the summary line under 72 characters.\n")
	sb.WriteString("Use a blank line between the summary and any extended description.\n")

	sb.WriteString("\n## Branch naming\n\n")
	sb.WriteString("Use descriptive branch names that reflect the work being done:\n")
	sb.WriteString("- Features: `feat/short-description`\n")
	sb.WriteString("- Bug fixes: `fix/short-description`\n")
	sb.WriteString("- Chores: `chore/short-description`\n")

	if r.CodeStyle != "" {
		sb.WriteString("\n## Code Style\n\n")
		sb.WriteString(r.CodeStyle + "\n")
	}

	return sb.String()
}
