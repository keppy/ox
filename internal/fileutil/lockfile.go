package fileutil

import (
	"errors"
	"fmt"
	"os"
)

// ErrLocked is returned by TryLockFile when another process already holds
// the exclusive lock. It is the cross-platform spelling of EWOULDBLOCK /
// ERROR_LOCK_VIOLATION so callers never have to import syscall or
// golang.org/x/sys/{unix,windows} to distinguish "busy" from "broken".
var ErrLocked = errors.New("file is locked by another process")

// TryLockFile opens (or creates, mode 0600) the file at path and attempts a
// non-blocking exclusive lock on it. It is the single cross-platform lock
// primitive for every "one holder per host" lease in ox — the daemon's
// global sync lease, the KB sync lock, the ledger anti-entropy lease, the
// skills apply lock — so that each of those has exactly one Unix and one
// Windows implementation instead of one per call site.
//
// Returns:
//   - (f, true, nil)    the lock is held; release it with UnlockFile(f)
//   - (nil, false, nil) another process holds it (contention, not an error)
//   - (nil, false, err) the file could not be opened or a real I/O error
//
// The lock is tied to the open file handle: it is released by UnlockFile,
// by Close, or by the OS when the process dies. Never delete the lock file
// while a lock may be held — that would let a second holder acquire a
// fresh inode/handle while the first still believes it is exclusive.
func TryLockFile(path string) (*os.File, bool, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("open lock file %s: %w", path, err)
	}
	if err := tryFlock(f); err != nil {
		_ = f.Close()
		if errors.Is(err, ErrLocked) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("lock %s: %w", path, err)
	}
	return f, true, nil
}

// TryLock attempts a non-blocking exclusive lock on an already-open file.
// Returns nil when the lock is held, ErrLocked when another process holds
// it, or the underlying error for a real failure. The lock is released by
// Unlock, by closing f, or when the process exits. Prefer TryLockFile when
// you do not already own the handle.
func TryLock(f *os.File) error { return tryFlock(f) }

// Unlock releases a lock acquired by TryLock without closing the file.
func Unlock(f *os.File) error { return unlockFlock(f) }

// UnlockFile releases a lock acquired by TryLockFile and closes the file.
// Safe to call with nil. Unlock errors are swallowed — by the time we are
// releasing, the protected work has already completed and Close alone is
// enough to drop the lock on every platform we ship to.
func UnlockFile(f *os.File) error {
	if f == nil {
		return nil
	}
	_ = unlockFlock(f)
	return f.Close()
}
