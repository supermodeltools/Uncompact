package project

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Info holds project metadata.
type Info struct {
	Name    string
	RootDir string
	Hash    string // stable hash for cache keying
	GitURL  string
	Branch  string
}

// Detect resolves the current project from the working directory.
func Detect(dir string) (*Info, error) {
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}

	root := findGitRoot(dir)
	if root == "" {
		root = dir
	}

	info := &Info{
		Name:    filepath.Base(root),
		RootDir: root,
	}

	// Try to get git remote URL
	if out, err := runGit(root, "remote", "get-url", "origin"); err == nil {
		info.GitURL = strings.TrimSpace(out)
	}

	// Try to get current branch
	if out, err := runGit(root, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		info.Branch = strings.TrimSpace(out)
	}

	// Build a stable hash from the root path (and remote URL if available)
	hashInput := root
	if info.GitURL != "" {
		hashInput = info.GitURL
	}
	h := sha256.Sum256([]byte(hashInput))
	info.Hash = fmt.Sprintf("%x", h[:8])

	return info, nil
}

// findGitRoot walks up the directory tree to find the .git directory.
func findGitRoot(start string) string {
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// runGit runs a git command in the given directory and returns stdout.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
