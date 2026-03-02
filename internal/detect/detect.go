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
	case fsutil.FileExists(filepath.Join(dir, "Gemfile")):
		analyzeRuby(dir, info)
	case fsutil.FileExists(filepath.Join(dir, "pom.xml")),
		fsutil.FileExists(filepath.Join(dir, "build.gradle")),
		fsutil.FileExists(filepath.Join(dir, "build.gradle.kts")):
		analyzeJava(dir, info)
	case fsutil.FileExists(filepath.Join(dir, "composer.json")):
		analyzePHP(dir, info)
	case fsutil.FileExists(filepath.Join(dir, "global.json")),
		fsutil.FileExists(filepath.Join(dir, "Directory.Build.props")),
		hasDotNetFile(dir):
		analyzeDotNet(dir, info)
	case fsutil.FileExists(filepath.Join(dir, "Package.swift")):
		analyzeSwift(dir, info)
	case fsutil.FileExists(filepath.Join(dir, "mix.exs")):
		analyzeElixir(dir, info)
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
	case fsutil.FileExists(filepath.Join(dir, "bun.lockb")),
		fsutil.FileExists(filepath.Join(dir, "bun.lock")):
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
				switch pkgMgr {
				case "npm", "yarn", "pnpm":
					info.TestCmd = pkgMgr + " test"
				default: // bun
					info.TestCmd = pkgMgr + " run test"
				}
			}
		}
	}

	// Check .nvmrc for Node version.
	if info.Version == "" {
		if data, err := os.ReadFile(filepath.Join(dir, ".nvmrc")); err == nil {
			info.Version = strings.TrimSpace(string(data))
		}
	}

	// execPrefix returns the correct executor prefix for the detected package manager.
	execPrefix := func(tool string) string {
		switch pkgMgr {
		case "bun":
			return "bunx " + tool
		case "pnpm":
			return "pnpm exec " + tool
		case "yarn":
			return "yarn " + tool
		default:
			return "npx " + tool
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
			info.LintCmd = execPrefix("eslint .")
		} else if hasBiome {
			info.LintCmd = execPrefix("biome check .")
		}
	}

	// Detect formatter.
	hasPrettier := fsutil.FileExists(filepath.Join(dir, ".prettierrc")) ||
		fsutil.FileExists(filepath.Join(dir, ".prettierrc.json")) ||
		fsutil.FileExists(filepath.Join(dir, ".prettierrc.js")) ||
		fsutil.FileExists(filepath.Join(dir, "prettier.config.js")) ||
		fsutil.FileExists(filepath.Join(dir, "prettier.config.mjs"))
	if hasPrettier {
		info.CodeStyle = "Uses Prettier for formatting. Run `" + execPrefix("prettier --write .") + "` before committing."
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
		inProject := false
		inPoetry := false
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "[") {
				inProject = line == "[project]"
				inPoetry = line == "[tool.poetry]"
				continue
			}
			if (inProject || inPoetry) && strings.HasPrefix(line, "name") && strings.Contains(line, "=") && info.ProjectName == filepath.Base(dir) {
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

// hasDotNetFile reports whether any .csproj, .fsproj, .vbproj, or .sln file
// exists directly in dir.
func hasDotNetFile(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".csproj") ||
			strings.HasSuffix(name, ".fsproj") ||
			strings.HasSuffix(name, ".vbproj") ||
			strings.HasSuffix(name, ".sln") {
			return true
		}
	}
	return false
}

func analyzeRuby(dir string, info *RepoInfo) {
	info.Language = "Ruby"

	// Check .ruby-version for interpreter version.
	if data, err := os.ReadFile(filepath.Join(dir, ".ruby-version")); err == nil {
		info.Version = strings.TrimSpace(string(data))
	}

	// Build command.
	if makefileHasTarget(dir, "build") {
		info.BuildCmd = "make build"
	}

	// Lint command.
	hasRuboCopConfig := fsutil.FileExists(filepath.Join(dir, ".rubocop.yml")) ||
		fsutil.FileExists(filepath.Join(dir, ".rubocop.yaml"))
	if hasRuboCopConfig {
		info.LintCmd = "bundle exec rubocop"
		info.CodeStyle = "Uses RuboCop for style enforcement. Run `bundle exec rubocop` before committing."
	} else if makefileHasTarget(dir, "lint") {
		info.LintCmd = "make lint"
	}

	// Test command.
	if fsutil.FileExists(filepath.Join(dir, "spec")) {
		if makefileHasTarget(dir, "test") {
			info.TestCmd = "make test"
		} else {
			info.TestCmd = "bundle exec rspec"
		}
	} else if fsutil.FileExists(filepath.Join(dir, "test")) {
		if makefileHasTarget(dir, "test") {
			info.TestCmd = "make test"
		} else {
			info.TestCmd = "bundle exec rake test"
		}
	} else if makefileHasTarget(dir, "test") {
		info.TestCmd = "make test"
	}
}

func analyzeJava(dir string, info *RepoInfo) {
	// Use Kotlin for Gradle Kotlin DSL projects, Java otherwise.
	if fsutil.FileExists(filepath.Join(dir, "build.gradle.kts")) {
		info.Language = "Kotlin"
	} else {
		info.Language = "Java"
	}

	// Check .java-version for JDK version.
	if data, err := os.ReadFile(filepath.Join(dir, ".java-version")); err == nil {
		info.Version = strings.TrimSpace(string(data))
	}

	useGradle := fsutil.FileExists(filepath.Join(dir, "build.gradle")) ||
		fsutil.FileExists(filepath.Join(dir, "build.gradle.kts"))
	useMaven := fsutil.FileExists(filepath.Join(dir, "pom.xml"))

	if useGradle {
		gradleCmd := "gradle"
		if fsutil.FileExists(filepath.Join(dir, "gradlew")) {
			gradleCmd = "./gradlew"
		}
		if makefileHasTarget(dir, "build") {
			info.BuildCmd = "make build"
		} else {
			info.BuildCmd = gradleCmd + " build"
		}
		if makefileHasTarget(dir, "test") {
			info.TestCmd = "make test"
		} else {
			info.TestCmd = gradleCmd + " test"
		}
	} else if useMaven {
		mvnCmd := "mvn"
		if fsutil.FileExists(filepath.Join(dir, "mvnw")) {
			mvnCmd = "./mvnw"
		}
		if makefileHasTarget(dir, "build") {
			info.BuildCmd = "make build"
		} else {
			info.BuildCmd = mvnCmd + " package"
		}
		if makefileHasTarget(dir, "test") {
			info.TestCmd = "make test"
		} else {
			info.TestCmd = mvnCmd + " test"
		}
	}
}

func analyzePHP(dir string, info *RepoInfo) {
	info.Language = "PHP"

	// Parse composer.json for project metadata and scripts.
	if data, err := os.ReadFile(filepath.Join(dir, "composer.json")); err == nil {
		var composer struct {
			Name    string                     `json:"name"`
			Require map[string]string          `json:"require"`
			Scripts map[string]json.RawMessage `json:"scripts"`
		}
		if json.Unmarshal(data, &composer) == nil {
			if composer.Name != "" {
				// composer names are vendor/package — use only the package part.
				if _, pkg, ok := strings.Cut(composer.Name, "/"); ok {
					info.ProjectName = pkg
				} else {
					info.ProjectName = composer.Name
				}
			}
			if phpVer, ok := composer.Require["php"]; ok {
				v := strings.TrimLeft(phpVer, ">=^~<!")
				if i := strings.IndexAny(v, ", "); i != -1 {
					v = v[:i]
				}
				info.Version = strings.TrimSpace(v)
			}
			if _, ok := composer.Scripts["test"]; ok {
				info.TestCmd = "composer test"
			}
			if _, ok := composer.Scripts["lint"]; ok {
				info.LintCmd = "composer lint"
			}
		}
	}

	// Build command.
	if makefileHasTarget(dir, "build") {
		info.BuildCmd = "make build"
	} else {
		info.BuildCmd = "composer install"
	}

	// Fallback test detection.
	if info.TestCmd == "" {
		if fsutil.FileExists(filepath.Join(dir, "vendor/bin/phpunit")) {
			info.TestCmd = "./vendor/bin/phpunit"
		} else if fsutil.FileExists(filepath.Join(dir, "phpunit.xml")) ||
			fsutil.FileExists(filepath.Join(dir, "phpunit.xml.dist")) {
			info.TestCmd = "phpunit"
		}
	}

	// Fallback lint detection.
	if info.LintCmd == "" {
		if fsutil.FileExists(filepath.Join(dir, "vendor/bin/phpstan")) {
			info.LintCmd = "./vendor/bin/phpstan analyse"
		} else if fsutil.FileExists(filepath.Join(dir, "vendor/bin/phpcs")) {
			info.LintCmd = "./vendor/bin/phpcs"
		}
	}

	info.CodeStyle = "Follow PSR-12 coding standards."
}

func analyzeDotNet(dir string, info *RepoInfo) {
	info.Language = "C#"

	// Parse global.json for SDK version.
	if data, err := os.ReadFile(filepath.Join(dir, "global.json")); err == nil {
		var globalJSON struct {
			SDK struct {
				Version string `json:"version"`
			} `json:"sdk"`
		}
		if json.Unmarshal(data, &globalJSON) == nil && globalJSON.SDK.Version != "" {
			info.Version = globalJSON.SDK.Version
		}
	}

	// Build command.
	if makefileHasTarget(dir, "build") {
		info.BuildCmd = "make build"
	} else {
		info.BuildCmd = "dotnet build"
	}

	// Lint command.
	info.LintCmd = "dotnet format --verify-no-changes"

	// Test command.
	if makefileHasTarget(dir, "test") {
		info.TestCmd = "make test"
	} else {
		info.TestCmd = "dotnet test"
	}

	info.CodeStyle = "Follow .NET coding conventions. Run `dotnet format` for formatting."
}

func analyzeSwift(dir string, info *RepoInfo) {
	info.Language = "Swift"

	// Parse Package.swift for swift-tools-version comment.
	if data, err := os.ReadFile(filepath.Join(dir, "Package.swift")); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			// e.g. // swift-tools-version:5.9 or // swift-tools-version: 5.9
			if strings.HasPrefix(line, "// swift-tools-version:") {
				ver := strings.TrimSpace(strings.TrimPrefix(line, "// swift-tools-version:"))
				if ver != "" {
					info.Version = ver
				}
				break
			}
		}
	}

	info.BuildCmd = "swift build"
	info.TestCmd = "swift test"

	// Lint: swiftlint if config file is present.
	if fsutil.FileExists(filepath.Join(dir, ".swiftlint.yml")) ||
		fsutil.FileExists(filepath.Join(dir, ".swiftlint.yaml")) {
		info.LintCmd = "swiftlint"
	}
}

func analyzeElixir(dir string, info *RepoInfo) {
	info.Language = "Elixir"

	// Parse mix.exs for elixir version and credo dependency.
	if data, err := os.ReadFile(filepath.Join(dir, "mix.exs")); err == nil {
		content := string(data)
		scanner := bufio.NewScanner(strings.NewReader(content))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			// Look for: elixir: "~> 1.15"
			if strings.Contains(line, "elixir:") && strings.Contains(line, "\"") {
				parts := strings.SplitN(line, "\"", 3)
				if len(parts) >= 3 {
					ver := strings.TrimLeft(parts[1], "~>^<!=")
					ver = strings.TrimSpace(ver)
					if ver != "" {
						info.Version = ver
					}
				}
				break
			}
		}
		// Lint: mix credo if credo appears in deps.
		if strings.Contains(content, "credo") {
			info.LintCmd = "mix credo"
		}
	}

	info.BuildCmd = "mix compile"
	info.TestCmd = "mix test"
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
