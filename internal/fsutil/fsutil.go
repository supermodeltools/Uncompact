package fsutil

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FileExists reports whether a file or directory exists at path.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// BuildGitFileSet runs git ls-files to get the set of files that git
// considers non-ignored (tracked files plus untracked files not excluded by
// .gitignore). Paths are slash-separated and relative to root.
// Returns nil if git is not available or root is not a git repository,
// in which case the caller falls back to the hardcoded allow/skip lists.
func BuildGitFileSet(ctx context.Context, root string) map[string]bool {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "--cached", "--others", "--exclude-standard")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
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
