package fsutil

import "os"

// FileExists reports whether a file or directory exists at path.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}
