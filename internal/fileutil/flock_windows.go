//go:build windows

package fileutil

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// tryFlock attempts a non-blocking exclusive lock via LockFileEx. Windows
// has no flock(2); LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY on a
// one-byte region gives the same semantics: contention returns
// ERROR_LOCK_VIOLATION instead of blocking, and the lock is released when
// the handle is unlocked or closed (including on process death).
//
// The one-byte region is an ownership token, not a data range — the lock
// file is a zero-length sidecar that nothing reads or writes.
func tryFlock(f *os.File) error {
	var overlapped windows.Overlapped
	err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1, 0,
		&overlapped,
	)
	if err != nil {
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return ErrLocked
		}
		return err
	}
	return nil
}

// unlockFlock releases a lock previously acquired via tryFlock.
func unlockFlock(f *os.File) error {
	var overlapped windows.Overlapped
	err := windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &overlapped)
	if err != nil && !errors.Is(err, windows.ERROR_NOT_LOCKED) {
		return err
	}
	return nil
}
