package project

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RootDir returns the git root of the working directory, or the working directory itself.
func RootDir() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	return os.Getwd()
}

// Hash returns a short stable hash identifying the project at dir.
// Uses the git remote URL if available, otherwise hashes the absolute path.
func Hash(dir string) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	// Try git remote origin URL for a stable cross-machine identifier
	out, err := exec.Command("git", "-C", absDir, "remote", "get-url", "origin").Output()
	if err == nil && len(out) > 0 {
		remote := strings.TrimSpace(string(out))
		sum := sha256.Sum256([]byte(remote))
		return fmt.Sprintf("%x", sum[:8]), nil
	}

	// Fall back to hashing the absolute path
	sum := sha256.Sum256([]byte(absDir))
	return fmt.Sprintf("%x", sum[:8]), nil
}

// Name returns a human-readable project name for the directory.
func Name(dir string) string {
	// Try git remote URL
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err == nil {
		remote := strings.TrimSpace(string(out))
		// Extract "owner/repo" from URL
		remote = strings.TrimSuffix(remote, ".git")
		parts := strings.Split(remote, "/")
		if len(parts) >= 2 {
			return parts[len(parts)-2] + "/" + parts[len(parts)-1]
		}
	}
	return filepath.Base(dir)
}
