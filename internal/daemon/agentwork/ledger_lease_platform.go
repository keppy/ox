package agentwork

import (
	"os"

	"github.com/sageox/ox/internal/fileutil"
)

// platformAcquireLedgerLease attempts a non-blocking exclusive lock on the
// lease file via fileutil.TryLockFile (flock(2) on Unix, LockFileEx on
// Windows). Contention returns (nil, false, nil).
//
// The lease file is never deleted — unlinking it would let a second process
// create and lock a different inode while the first still holds the old one.
func platformAcquireLedgerLease(path string) (*os.File, bool, error) {
	return fileutil.TryLockFile(path)
}

func platformReleaseLedgerLease(f *os.File) error {
	return fileutil.UnlockFile(f)
}
