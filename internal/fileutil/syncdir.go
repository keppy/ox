package fileutil

import (
	"errors"
	"os"
	"runtime"
	"syscall"
)

// SyncDir fsyncs a directory so a just-completed rename/create in it is
// durable across a hard crash.
//
// Directory handles cannot be flushed on Windows (FlushFileBuffers on a
// directory fails with ERROR_ACCESS_DENIED / EINVAL) and on some network
// filesystems, and NTFS journals metadata anyway. Those cases return nil:
// the caller's data file was already fsynced, and treating an unsupported
// directory flush as a failure would turn every successful write into an
// error on those platforms. Any other error (path missing, not a directory,
// I/O error) is returned.
func SyncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := f.Sync(); err != nil {
		if runtime.GOOS == "windows" || errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EACCES) {
			return nil
		}
		return err
	}
	return nil
}
