package project

import (
	"bytes"
	"context"
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
func Detect(ctx context.Context, dir string) (*Info, error) {
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
	if out, err := runGit(ctx, root, "remote", "get-url", "origin"); err == nil {
		info.GitURL = strings.TrimSpace(out)
	}

	// Try to get current branch
	if out, err := runGit(ctx, root, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		info.Branch = strings.TrimSpace(out)
	}

	// Build a stable hash from the root path (and remote URL if available).
	// h[:8] = first 8 bytes of SHA-256, formatted as %x = 16 hex characters.
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
// The command is cancelled if ctx is done before it completes.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%w: %s", err, msg)
		}
		return "", err
	}
	return string(out), nil
}
