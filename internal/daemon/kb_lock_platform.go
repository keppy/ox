package daemon

import "github.com/sageox/ox/internal/fileutil"

// platformAcquireKBLock opens (or creates) the lock file and attempts a
// non-blocking exclusive lock via fileutil.TryLockFile (flock(2) on Unix,
// LockFileEx on Windows). On contention (nil, false, nil) is returned. On
// success the unlock closure releases the lock and closes the handle; it is
// safe to call from a defer.
func platformAcquireKBLock(path string) (unlock func(), acquired bool, err error) {
	f, acquired, err := fileutil.TryLockFile(path)
	if err != nil || !acquired {
		return nil, acquired, err
	}
	return func() { _ = fileutil.UnlockFile(f) }, true, nil
}
