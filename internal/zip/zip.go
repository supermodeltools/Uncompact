package zip

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// skipDirs are directories to exclude from the repo zip.
// Note: any directory whose name starts with "." is also skipped unconditionally
// (see the filepath.WalkDir callback below), so dot-prefixed directories such as
// .venv, .gradle, .turbo, .parcel-cache, .dart_tool, .nyc_output, .svelte-kit,
// and .output are already excluded without needing explicit entries here.
var skipDirs = map[string]bool{
	// VCS
	".git": true,
	// JavaScript / Node
	"node_modules": true,
	"dist":         true,
	// Go
	"vendor": true,
	// IDE
	".idea":   true,
	".vscode": true,
	// Python
	"__pycache__":  true,
	".pytest_cache": true,
	"venv":         true,
	"env":          true,
	// Java / Android
	"build":  true,
	"target": true,
	// Elm
	"elm-stuff": true,
	// Elixir / OCaml
	"_build": true,
	// iOS
	"Pods": true,
	// Test coverage
	".nyc_output": true,
	"coverage":    true,
	// Next.js / Nuxt / SvelteKit
	".next":       true,
	".nuxt":       true,
	".svelte-kit": true,
	".output":     true,
	"out":         true,
}

// skipExts are file extensions to exclude (binaries, media, etc.)
var skipExts = map[string]bool{
	".exe": true, ".dll": true, ".so": true, ".dylib": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".bmp": true, ".ico": true, ".svg": true, ".webp": true,
	".mp4": true, ".mp3": true, ".wav": true, ".avi": true,
	".pdf": true, ".zip": true, ".tar": true, ".gz": true,
	".wasm": true, ".pyc": true, ".class": true,
	".db": true, ".sqlite": true, ".sqlite3": true,
}

// maxFileSize is the maximum size (bytes) for a single file to be included.
const maxFileSize = 512 * 1024 // 512KB per file

// maxTotalSize is the maximum total uncompressed size to zip.
const maxTotalSize = 10 * 1024 * 1024 // 10MB total

// buildGitFileSet runs git ls-files to get the set of files that git
// considers non-ignored (tracked files plus untracked files not excluded by
// .gitignore). Paths are slash-separated and relative to root.
// Returns nil if git is not available or root is not a git repository,
// in which case the caller falls back to the hardcoded allow/skip lists.
func buildGitFileSet(ctx context.Context, root string) map[string]bool {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "--cached", "--others", "--exclude-standard")
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			fmt.Fprintf(os.Stderr, "git ls-files: %s\n", msg)
		}
		return nil
	}

	files := make(map[string]bool)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files[filepath.ToSlash(line)] = true
		}
	}
	return files
}

// SkipReport describes files that were excluded from the ZIP archive.
type SkipReport struct {
	// OversizedFiles are relative paths of files skipped because they exceed maxFileSize (512KB).
	OversizedFiles []string
	// BudgetSkipped is the count of files skipped because the total size budget (10MB) was reached.
	BudgetSkipped int
	// OpenErrors is the count of files skipped because they could not be opened (e.g. permission denied).
	OpenErrors int
}

// Truncated returns true if any files were excluded from the archive.
func (s SkipReport) Truncated() bool {
	return len(s.OversizedFiles) > 0 || s.BudgetSkipped > 0 || s.OpenErrors > 0
}

// errOpenFailed is returned by addFileToZip when the source file cannot be
// opened. The caller should skip the file and increment SkipReport.OpenErrors
// rather than aborting the archive.
var errOpenFailed = errors.New("could not open file")

// addFileToZip opens path, creates a zip entry named rel inside w, and copies
// the file contents. It returns the number of bytes written. Using a helper
// function ensures that defer f.Close() is scoped to each individual file
// rather than accumulating until the outer RepoZip function returns.
//
// errOpenFailed is returned when the file cannot be opened; the caller may
// choose to skip the file silently. All other errors (w.Create, io.Copy) are
// fatal: they indicate the zip.Writer may be in a corrupt state and the
// archive walk should be aborted.
func addFileToZip(w *zip.Writer, path, rel string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, errOpenFailed
	}
	defer f.Close()

	zw, err := w.Create(rel)
	if err != nil {
		return 0, err
	}

	n, err := io.Copy(zw, f)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// RepoZip creates an in-memory ZIP archive of the project root.
// The second return value describes any files that were excluded from the archive.
func RepoZip(ctx context.Context, root string) ([]byte, SkipReport, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	var totalSize int64
	var report SkipReport
	var budgetExceeded bool

	// Build the set of git-tracked/unignored files so we can respect .gitignore.
	// gitFiles is nil when git is unavailable; in that case we fall back to the
	// hardcoded skip lists below.
	gitFiles := buildGitFileSet(ctx, root)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return nil // skip unreadable files
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		// Skip hidden entries and known large/irrelevant directories.
		base := filepath.Base(path)
		if d.IsDir() {
			if skipDirs[base] || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip hidden files (e.g. .env, .secrets).
		if strings.HasPrefix(base, ".") {
			return nil
		}

		// Skip symlinks to avoid including files outside the repo root.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}

		// Respect .gitignore: if we have a git file list, skip any file not in it.
		if gitFiles != nil && !gitFiles[rel] {
			return nil
		}

		// Skip by extension
		ext := strings.ToLower(filepath.Ext(path))
		if skipExts[ext] {
			return nil
		}

		// Get file info for size checks. d.Info reuses directory-read metadata
		// when available, avoiding an extra Lstat syscall on most platforms.
		info, err := d.Info()
		if err != nil {
			return nil // file removed between walk and stat; skip silently
		}

		// Skip large files, recording the path so callers can report them.
		if info.Size() > maxFileSize {
			report.OversizedFiles = append(report.OversizedFiles, rel)
			return nil
		}

		// Check total size budget; once exceeded, count remaining files but don't add them.
		if budgetExceeded || totalSize+info.Size() > maxTotalSize {
			budgetExceeded = true
			report.BudgetSkipped++
			return nil
		}

		n, err := addFileToZip(w, path, rel)
		if errors.Is(err, errOpenFailed) {
			report.OpenErrors++
			return nil
		}
		if err != nil {
			return err
		}
		totalSize += n
		return nil
	})

	if err != nil {
		return nil, SkipReport{}, err
	}

	if err := w.Close(); err != nil {
		return nil, SkipReport{}, err
	}

	return buf.Bytes(), report, nil
}
