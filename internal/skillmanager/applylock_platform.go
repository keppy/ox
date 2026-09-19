package skillmanager

import "github.com/sageox/ox/internal/fileutil"

// platformAcquireApplyLock attempts a non-blocking exclusive lock on the
// skills apply lock file via fileutil.TryLockFile (flock(2) on Unix,
// LockFileEx on Windows). Contention returns acquired=false rather than
// waiting, because the caller — often a session start — must never block
// on a peer.
//
// Windows is a first-class target for the skill inventory (copy
// materialization is exactly what Windows gets), so an unlocked apply would
// let `ox init`, `ox doctor --fix`, the daemon tick, and `ox agent prime`
// interleave after planning and overwrite one another's managed files.
func platformAcquireApplyLock(path string) (unlock func(), acquired bool, err error) {
	f, acquired, err := fileutil.TryLockFile(path)
	if err != nil || !acquired {
		return nil, acquired, err
	}
	return func() { _ = fileutil.UnlockFile(f) }, true, nil
}
