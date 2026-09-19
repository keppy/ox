package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// StatStrict is os.Stat with one portability fix: when a component of the
// path's parent chain is a regular file, Unix returns ENOTDIR but Windows
// returns ERROR_PATH_NOT_FOUND, which Go maps to os.ErrNotExist. Callers that
// treat "not exist" as "safe to proceed" (nothing to protect, nothing to
// overwrite) would therefore be fooled on Windows by a file blocking the
// directory. StatStrict walks up on ErrNotExist and returns a *PathError
// wrapping ErrNotDirectory when it finds such a file, so both platforms
// report the same class of failure.
func StatStrict(path string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if err == nil {
		return info, nil
	}
	// Unix reports a file in the parent chain directly as ENOTDIR (not
	// os.ErrNotExist), so map it to the same sentinel the walk below returns
	// on Windows; otherwise the ErrNotDirectory contract is Windows-only.
	// The ErrNotExist exclusion matters on Windows, where os.Stat maps a
	// missing parent (ERROR_PATH_NOT_FOUND) to an ENOTDIR that still Is
	// ErrNotExist — that case is genuinely absent and must keep walking.
	if errors.Is(err, syscall.ENOTDIR) && !errors.Is(err, os.ErrNotExist) {
		return nil, &os.PathError{Op: "stat", Path: path, Err: ErrNotDirectory}
	}
	if !errors.Is(err, os.ErrNotExist) {
		return info, err
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		fi, perr := os.Lstat(dir)
		if perr == nil {
			if !fi.IsDir() {
				return nil, &os.PathError{Op: "stat", Path: path, Err: ErrNotDirectory}
			}
			return nil, err // parent chain is sound: genuinely absent
		}
		if !errors.Is(perr, os.ErrNotExist) {
			return nil, err
		}
		if parent := filepath.Dir(dir); parent == dir {
			return nil, err
		}
	}
}

// LstatStrict is StatStrict without following the final symlink.
func LstatStrict(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		return info, err
	}
	_, serr := StatStrict(path)
	return nil, serr
}

// ErrNotDirectory reports that a path component that must be a directory is a
// file. It is the portable spelling of ENOTDIR.
var ErrNotDirectory = errors.New("not a directory")
