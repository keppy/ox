package daemon

import (
	"os"

	"github.com/sageox/ox/internal/fileutil"
)

// platformAcquireGlobalLease opens (or creates) the lease file and attempts
// a non-blocking exclusive lock via fileutil.TryLockFile, which is flock(2)
// on Unix and LockFileEx on Windows. Returns the open *os.File on success so
// Release can unlock + close the same handle; otherwise (nil, false, nil)
// for "another holder" or (nil, false, err) for a filesystem failure.
//
// Unlike kb_lock.go, the lease file is held open for the daemon's lifetime
// and only released on shutdown; there is no per-tick acquire path here.
// The file is never deleted (deleting it would create a TOCTOU race with
// another daemon's acquire).
func platformAcquireGlobalLease(path string) (*os.File, bool, error) {
	return fileutil.TryLockFile(path)
}

// platformReleaseGlobalLease releases the lock and closes the handle.
func platformReleaseGlobalLease(f *os.File) error {
	return fileutil.UnlockFile(f)
}
