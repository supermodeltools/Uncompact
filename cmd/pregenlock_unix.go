//go:build !windows

package cmd

import (
	"os"
	"syscall"
)

// acquirePregenLock opens (or creates) the lock file at path and attempts to
// acquire an exclusive, non-blocking flock on it.
//
// Returns:
//   - unlock: a function that releases the lock and closes the file (call via defer)
//   - acquired: true if the lock was obtained
//   - err: non-nil only for unexpected OS errors (not for EWOULDBLOCK)
//
// If acquired is false and err is nil, another pregen instance already holds
// the lock — the caller should exit silently.
func acquirePregenLock(path string) (unlock func(), acquired bool, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, false, err
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			// Another pregen instance already holds the lock.
			return nil, false, nil
		}
		return nil, false, err
	}

	unlock = func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
	return unlock, true, nil
}
