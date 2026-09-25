//go:build unix

package fileutil

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryFlock attempts a non-blocking exclusive lock. Returns nil on
// success, ErrLocked if another process holds the lock, or any other
// error for a real I/O failure. Callers retry; the polling cadence lives
// in WithFileLock.
func tryFlock(f *os.File) error {
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		// Both EWOULDBLOCK and EAGAIN signal "another holder" on POSIX
		// systems; they are the same value on Linux but distinct on some
		// BSDs. Treat both as the expected contention case.
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return ErrLocked
		}
		return err
	}
	return nil
}

// unlockFlock releases an advisory lock previously acquired via
// tryFlock. Errors are logged by the caller (via defer) since by the
// time we're unlocking the success path has already happened.
func unlockFlock(f *os.File) error {
	err := unix.Flock(int(f.Fd()), unix.LOCK_UN)
	if err != nil && !errors.Is(err, unix.EBADF) {
		// EBADF can happen if the file was closed before unlock — not
		// a real failure for our caller, just defensive.
		return err
	}
	return nil
}
