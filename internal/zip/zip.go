package zip

import (
	archivezip "archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// defaultIgnore contains directory names to skip when zipping.
var defaultIgnore = map[string]bool{
	".git":          true,
	"node_modules":  true,
	"vendor":        true,
	".venv":         true,
	"venv":          true,
	"__pycache__":   true,
	".tox":          true,
	"dist":          true,
	"build":         true,
	".next":         true,
	".nuxt":         true,
	"coverage":      true,
	".nyc_output":   true,
	"target":        true, // Rust/Java
	"bin":           true,
	"obj":           true,
	".gradle":       true,
	".mvn":          true,
}

// maxFileSizeBytes is the largest individual file to include.
const maxFileSizeBytes = 1_000_000 // 1 MB per file

// ZipDir creates an in-memory zip of dir, applying ignore rules.
func ZipDir(dir string) ([]byte, error) {
	var buf bytes.Buffer
	w := archivezip.NewWriter(&buf)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable paths
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}

		// Skip the root itself
		if rel == "." {
			return nil
		}

		// Skip ignored directories
		if info.IsDir() {
			if defaultIgnore[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip hidden files and large files
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}
		if info.Size() > maxFileSizeBytes {
			return nil
		}

		// Skip binary extensions
		if isBinary(info.Name()) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable files
		}

		f, err := w.Create(rel)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// binaryExtensions lists file extensions that are typically binary.
var binaryExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".ico": true,
	".svg": true, ".pdf": true, ".zip": true, ".tar": true, ".gz": true,
	".woff": true, ".woff2": true, ".ttf": true, ".eot": true,
	".exe": true, ".dll": true, ".so": true, ".dylib": true,
	".pyc": true, ".pyo": true, ".class": true,
	".mp4": true, ".mp3": true, ".wav": true, ".avi": true,
	".lock": true, // package lock files tend to be very large
}

func isBinary(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return binaryExtensions[ext]
}
