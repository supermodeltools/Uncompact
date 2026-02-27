package zip

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// skipDirs are directories to exclude from the repo zip.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".idea":        true,
	".vscode":      true,
	"__pycache__":  true,
	".pytest_cache": true,
	"dist":         true,
	"build":        true,
	"target":       true,
	".next":        true,
	".nuxt":        true,
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

// RepoZip creates an in-memory ZIP archive of the project root.
func RepoZip(root string) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	var totalSize int64

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
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
		if info.IsDir() {
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
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		// Skip by extension
		ext := strings.ToLower(filepath.Ext(path))
		if skipExts[ext] {
			return nil
		}

		// Skip large files
		if info.Size() > maxFileSize {
			return nil
		}

		// Check total size budget
		if totalSize+info.Size() > maxTotalSize {
			return io.EOF // signal we're done
		}

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		zw, err := w.Create(rel)
		if err != nil {
			return nil
		}

		n, err := io.Copy(zw, f)
		if err != nil {
			return nil
		}
		totalSize += n
		return nil
	})

	if err != nil && err != io.EOF {
		return nil, err
	}

	if err := w.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
