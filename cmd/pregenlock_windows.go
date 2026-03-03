//go:build windows

package cmd

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// acquirePregenLock creates a Windows named mutex and attempts a non-blocking
// acquisition (WaitForSingleObject with timeout=0).
//
// Returns:
//   - unlock: a function that releases the mutex and closes the handle (call via defer)
//   - acquired: true if the mutex was obtained
//   - err: non-nil only for unexpected OS errors
//
// If acquired is false and err is nil, another pregen instance already holds
// the mutex — the caller should exit silently.
func acquirePregenLock(path string) (unlock func(), acquired bool, err error) {
	// Derive a project-specific mutex name from the lock file's base name so
	// that concurrent pregen runs for different projects do not block each other.
	// Mutex names may not contain backslashes outside the "Local\" prefix segment.
	safeName := strings.ReplaceAll(filepath.Base(path), "\\", "_")
	name, err := windows.UTF16PtrFromString(`Local\UncompactPregen-` + safeName)
	if err != nil {
		return nil, false, err
	}

	h, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		return nil, false, err
	}

	// Non-blocking wait: timeout=0 means return immediately if not available.
	event, err := windows.WaitForSingleObject(h, 0)
	if err != nil {
		windows.CloseHandle(h)
		return nil, false, err
	}

	if event == uint32(windows.WAIT_TIMEOUT) {
		// Another pregen instance already holds the mutex.
		windows.CloseHandle(h)
		return nil, false, nil
	}

	// WAIT_OBJECT_0 or WAIT_ABANDONED — we own the mutex.
	unlock = func() {
		windows.ReleaseMutex(h)
		windows.CloseHandle(h)
	}
	return unlock, true, nil
}
