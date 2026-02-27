//go:build windows

package cmd

// acquirePregenLock is a no-op on Windows: exclusive locking via syscall.Flock
// is not available, so concurrent pregen instances are not guarded there.
func acquirePregenLock(_ string) (unlock func(), acquired bool, err error) {
	return func() {}, true, nil
}
