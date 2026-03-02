package zip

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// makeFile creates a file at path with the given content, creating parent dirs as needed.
func makeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// zipNames returns the set of entry names in a zip archive.
func zipNames(t *testing.T, data []byte) map[string]bool {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	names := make(map[string]bool)
	for _, f := range r.File {
		names[f.Name] = true
	}
	return names
}

// --- skipDirs ---

func TestSkipDirs(t *testing.T) {
	tests := []struct{ name, dir string }{
		{"node_modules", "node_modules"},
		{"vendor", "vendor"},
		{"build", "build"},
		{"target", "target"},
		{"dist", "dist"},
		{"__pycache__", "__pycache__"},
		{"coverage", "coverage"},
		{"elm-stuff", "elm-stuff"},
		{"_build", "_build"},
		{"Pods", "Pods"},
		{"out", "out"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			makeFile(t, filepath.Join(root, tc.dir, "file.txt"), []byte("skip me"))
			makeFile(t, filepath.Join(root, "keep.txt"), []byte("keep me"))

			data, _, err := RepoZip(context.Background(), root)
			if err != nil {
				t.Fatalf("RepoZip: %v", err)
			}

			names := zipNames(t, data)
			if names[tc.dir+"/file.txt"] {
				t.Errorf("file inside %s/ was not excluded", tc.dir)
			}
			if !names["keep.txt"] {
				t.Error("keep.txt was unexpectedly absent")
			}
		})
	}
}

// --- hidden directories ---

func TestSkipHiddenDirs(t *testing.T) {
	root := t.TempDir()
	makeFile(t, filepath.Join(root, ".hidden", "secret.txt"), []byte("secret"))
	makeFile(t, filepath.Join(root, "visible.txt"), []byte("visible"))

	data, _, err := RepoZip(context.Background(), root)
	if err != nil {
		t.Fatalf("RepoZip: %v", err)
	}

	names := zipNames(t, data)
	if names[".hidden/secret.txt"] {
		t.Error("file inside hidden directory was not excluded")
	}
	if !names["visible.txt"] {
		t.Error("visible.txt was unexpectedly absent")
	}
}

// --- skipExts ---

func TestSkipExts(t *testing.T) {
	tests := []struct{ name, ext string }{
		{"exe", ".exe"},
		{"dll", ".dll"},
		{"so", ".so"},
		{"png", ".png"},
		{"jpg", ".jpg"},
		{"mp4", ".mp4"},
		{"mp3", ".mp3"},
		{"pdf", ".pdf"},
		{"zip", ".zip"},
		{"tar", ".tar"},
		{"gz", ".gz"},
		{"wasm", ".wasm"},
		{"pyc", ".pyc"},
		{"class", ".class"},
		{"db", ".db"},
		{"sqlite", ".sqlite"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			makeFile(t, filepath.Join(root, "binary"+tc.ext), []byte("binary"))
			makeFile(t, filepath.Join(root, "keep.txt"), []byte("text"))

			data, _, err := RepoZip(context.Background(), root)
			if err != nil {
				t.Fatalf("RepoZip: %v", err)
			}

			names := zipNames(t, data)
			if names["binary"+tc.ext] {
				t.Errorf("file with extension %s was not excluded", tc.ext)
			}
			if !names["keep.txt"] {
				t.Error("keep.txt was unexpectedly absent")
			}
		})
	}
}

// --- hidden files ---

func TestSkipHiddenFiles(t *testing.T) {
	root := t.TempDir()
	makeFile(t, filepath.Join(root, ".env"), []byte("SECRET=value"))
	makeFile(t, filepath.Join(root, ".gitignore"), []byte("*.pyc"))
	makeFile(t, filepath.Join(root, "visible.txt"), []byte("visible"))

	data, _, err := RepoZip(context.Background(), root)
	if err != nil {
		t.Fatalf("RepoZip: %v", err)
	}

	names := zipNames(t, data)
	if names[".env"] {
		t.Error(".env was not excluded")
	}
	if names[".gitignore"] {
		t.Error(".gitignore was not excluded")
	}
	if !names["visible.txt"] {
		t.Error("visible.txt was unexpectedly absent")
	}
}

// --- oversized files ---

func TestOversizedFile(t *testing.T) {
	root := t.TempDir()
	oversized := make([]byte, maxFileSize+1)
	makeFile(t, filepath.Join(root, "big.bin"), oversized)
	makeFile(t, filepath.Join(root, "small.txt"), []byte("small"))

	data, report, err := RepoZip(context.Background(), root)
	if err != nil {
		t.Fatalf("RepoZip: %v", err)
	}

	names := zipNames(t, data)
	if names["big.bin"] {
		t.Error("oversized file was included in zip")
	}
	if !names["small.txt"] {
		t.Error("small.txt was unexpectedly absent")
	}

	found := false
	for _, f := range report.OversizedFiles {
		if f == "big.bin" {
			found = true
		}
	}
	if !found {
		t.Errorf("big.bin not in OversizedFiles; got %v", report.OversizedFiles)
	}
}

func TestOversizedFile_ExactlyAtLimit_IsIncluded(t *testing.T) {
	root := t.TempDir()
	exact := make([]byte, maxFileSize)
	makeFile(t, filepath.Join(root, "exact.txt"), exact)

	data, report, err := RepoZip(context.Background(), root)
	if err != nil {
		t.Fatalf("RepoZip: %v", err)
	}

	names := zipNames(t, data)
	if !names["exact.txt"] {
		t.Error("file exactly at limit was unexpectedly excluded")
	}
	if len(report.OversizedFiles) != 0 {
		t.Errorf("expected no OversizedFiles, got %v", report.OversizedFiles)
	}
}

// --- budget enforcement ---

func TestBudgetSkipped(t *testing.T) {
	root := t.TempDir()
	// Each file is just under 512KB; 22 files exceed the 10MB budget.
	chunk := make([]byte, maxFileSize-1)
	for i := 0; i < 22; i++ {
		makeFile(t, filepath.Join(root, fmt.Sprintf("file%02d.txt", i)), chunk)
	}

	_, report, err := RepoZip(context.Background(), root)
	if err != nil {
		t.Fatalf("RepoZip: %v", err)
	}
	if report.BudgetSkipped == 0 {
		t.Error("expected BudgetSkipped > 0, got 0")
	}
}

// TestBudgetContinuesAfterSkip verifies that a file too large to fit in the
// remaining budget does not prevent subsequent smaller files from being included.
func TestBudgetContinuesAfterSkip(t *testing.T) {
	root := t.TempDir()

	// Fill budget to within ~200 KB of the limit using 20 files of (512KB-1).
	chunk := make([]byte, maxFileSize-1)
	for i := 0; i < 20; i++ {
		makeFile(t, filepath.Join(root, fmt.Sprintf("filler%02d.txt", i)), chunk)
	}
	// This file (~300 KB) would push us just over 10 MB; it should be skipped.
	tooBig := make([]byte, 300*1024)
	makeFile(t, filepath.Join(root, "zz_toobig.txt"), tooBig)
	// This tiny file should still fit in the remaining headroom and be included.
	makeFile(t, filepath.Join(root, "zz_tiny.txt"), []byte("tiny"))

	data, report, err := RepoZip(context.Background(), root)
	if err != nil {
		t.Fatalf("RepoZip: %v", err)
	}

	names := zipNames(t, data)
	if !names["zz_tiny.txt"] {
		t.Error("zz_tiny.txt should be included after a budget-busting file is skipped, but it was absent")
	}
	if names["zz_toobig.txt"] {
		t.Error("zz_toobig.txt should have been skipped due to budget, but it was included")
	}
	if report.BudgetSkipped == 0 {
		t.Error("expected BudgetSkipped > 0")
	}
}

// --- SkipReport.Truncated ---

func TestTruncated(t *testing.T) {
	tests := []struct {
		name   string
		report SkipReport
		want   bool
	}{
		{"empty", SkipReport{}, false},
		{"oversized files", SkipReport{OversizedFiles: []string{"a.txt"}}, true},
		{"budget skipped", SkipReport{BudgetSkipped: 1}, true},
		{"open errors", SkipReport{OpenErrors: 1}, true},
		{"all fields", SkipReport{OversizedFiles: []string{"a.txt"}, BudgetSkipped: 2, OpenErrors: 3}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.report.Truncated(); got != tc.want {
				t.Errorf("Truncated() = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- clean repo ---

func TestCleanRepo(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"main.go":        "package main",
		"README.md":      "# Project",
		"src/util.go":    "package src",
		"docs/index.txt": "documentation",
	}
	for rel, content := range files {
		makeFile(t, filepath.Join(root, filepath.FromSlash(rel)), []byte(content))
	}

	data, report, err := RepoZip(context.Background(), root)
	if err != nil {
		t.Fatalf("RepoZip: %v", err)
	}
	if report.Truncated() {
		t.Errorf("expected no skips for clean repo, got report: %+v", report)
	}

	names := zipNames(t, data)
	for rel := range files {
		if !names[rel] {
			t.Errorf("expected %q in zip; archive contains: %v", rel, names)
		}
	}
}

// --- context cancellation ---

func TestRepoZip_ContextCancellation(t *testing.T) {
	root := t.TempDir()
	// Create enough files to ensure the Walk callback is invoked multiple times.
	for i := 0; i < 10; i++ {
		makeFile(t, filepath.Join(root, fmt.Sprintf("file%02d.txt", i)), []byte("content"))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling RepoZip to exercise the fast-exit path

	_, _, err := RepoZip(ctx, root)
	if err == nil {
		t.Fatal("expected non-nil error for cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// --- symlink exclusion ---

func TestSkipSymlinks(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()

	// Create a file outside the repo root with sentinel content.
	externalFile := filepath.Join(external, "secret.txt")
	makeFile(t, externalFile, []byte("sentinel content outside root"))

	// Create a regular file inside the root to confirm non-symlink files are included.
	makeFile(t, filepath.Join(root, "normal.txt"), []byte("normal content"))

	// Create a symlink inside the root pointing to the external file.
	symlinkPath := filepath.Join(root, "link.txt")
	if err := os.Symlink(externalFile, symlinkPath); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	data, _, err := RepoZip(context.Background(), root)
	if err != nil {
		t.Fatalf("RepoZip: %v", err)
	}

	names := zipNames(t, data)
	if names["link.txt"] {
		t.Error("symlink was included in the zip archive; expected it to be skipped")
	}
	if !names["normal.txt"] {
		t.Error("normal.txt was unexpectedly absent from the zip archive")
	}
}

// --- git-tracked dotfiles ---

// TestGitTrackedDotfiles verifies that dotfiles explicitly committed to git
// (e.g. .eslintrc.json, .prettierrc) are included in the zip, while untracked
// dotfiles (e.g. .env) are still excluded.
func TestGitTrackedDotfiles(t *testing.T) {
	root := t.TempDir()

	// Initialize a git repo.
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")

	// Create config dotfiles, a secret dotfile, and a regular file.
	makeFile(t, filepath.Join(root, ".eslintrc.json"), []byte(`{"semi": false}`))
	makeFile(t, filepath.Join(root, ".prettierrc"), []byte(`{"tabWidth": 2}`))
	makeFile(t, filepath.Join(root, ".env"), []byte("SECRET=value"))
	makeFile(t, filepath.Join(root, "main.go"), []byte("package main"))

	// Stage and commit the config dotfiles and main.go; leave .env untracked.
	run("add", ".eslintrc.json", ".prettierrc", "main.go")
	run("commit", "-m", "initial")

	data, _, err := RepoZip(context.Background(), root)
	if err != nil {
		t.Fatalf("RepoZip: %v", err)
	}

	names := zipNames(t, data)

	if !names[".eslintrc.json"] {
		t.Error(".eslintrc.json (git-tracked dotfile) was unexpectedly excluded")
	}
	if !names[".prettierrc"] {
		t.Error(".prettierrc (git-tracked dotfile) was unexpectedly excluded")
	}
	if names[".env"] {
		t.Error(".env (untracked dotfile) was not excluded")
	}
	if !names["main.go"] {
		t.Error("main.go was unexpectedly absent")
	}
}

// --- git ls-files fallback ---

func TestGitFallback_NonGitDir(t *testing.T) {
	// t.TempDir() is outside any git repo, so buildGitFileSet returns nil
	// and the hardcoded skip lists must still protect sensitive files.
	root := t.TempDir()
	makeFile(t, filepath.Join(root, "code.go"), []byte("package main"))
	makeFile(t, filepath.Join(root, ".env"), []byte("SECRET=value"))
	makeFile(t, filepath.Join(root, "node_modules", "pkg", "index.js"), []byte("module"))

	data, _, err := RepoZip(context.Background(), root)
	if err != nil {
		t.Fatalf("RepoZip: %v", err)
	}

	names := zipNames(t, data)
	if !names["code.go"] {
		t.Error("code.go was unexpectedly absent in non-git fallback")
	}
	if names[".env"] {
		t.Error(".env was not excluded in non-git fallback")
	}
	if names["node_modules/pkg/index.js"] {
		t.Error("node_modules file was not excluded in non-git fallback")
	}
}
