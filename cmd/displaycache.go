package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
)

// displayCachePath returns the UID-based path for the display cache temp file.
// On Windows, os.Getuid() returns -1, so we fall back to the username.
func displayCachePath() string {
	uid := os.Getuid()
	var id string
	if uid == -1 {
		// Windows: fall back to username.
		if u, err := user.Current(); err == nil {
			id = u.Username
		} else {
			id = "windows"
		}
	} else {
		id = fmt.Sprintf("%d", uid)
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("uncompact-display-%s.txt", id))
}

// writeDisplayCache atomically writes content to the display cache file so that
// the UserPromptSubmit hook (show-cache) can read and display it on the next prompt.
func writeDisplayCache(content string) error {
	cachePath := displayCachePath()
	tmp, err := os.CreateTemp(filepath.Dir(cachePath), "uncompact-display-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		if removeErr := os.Remove(tmp.Name()); removeErr != nil {
			fmt.Fprintf(os.Stderr, "[debug] failed to remove temp file %s: %v\n", tmp.Name(), removeErr)
		}
		return err
	}
	tmp.Close()
	if err := os.Rename(tmp.Name(), cachePath); err != nil {
		if removeErr := os.Remove(tmp.Name()); removeErr != nil {
			fmt.Fprintf(os.Stderr, "[debug] failed to remove temp file %s: %v\n", tmp.Name(), removeErr)
		}
		return err
	}
	return nil
}
