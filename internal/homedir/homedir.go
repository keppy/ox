// Package homedir resolves the current user's home directory.
//
// It exists because os.UserHomeDir is platform-inconsistent in a way that
// bites ox: on Unix it honors $HOME, on Windows it reads only %USERPROFILE%
// and ignores $HOME entirely. Every ox test that isolates a fake home does so
// with t.Setenv("HOME", dir) — a convention that is correct on Unix and
// silently a no-op on Windows, so those tests would read (and write!) the
// developer's real home.
//
// The rule here: $HOME wins when it is set to an absolute path for THIS
// platform; otherwise the account database's home, then os.UserHomeDir.
// On Windows a Git-Bash
// style HOME=/c/Users/me is not filepath.IsAbs (no volume), so it does not
// hijack resolution — only a Windows-shaped HOME=C:\Users\me does, which is
// what tests set and what users who deliberately export HOME expect. This
// matches the behavior of the widely used mitchellh/go-homedir package.
//
// No other package in ox should call os.UserHomeDir directly.
package homedir

import (
	"os"
	"os/user"
	"path/filepath"
)

// Dir returns the current user's home directory. Signature matches
// os.UserHomeDir so it is a drop-in replacement.
func Dir() (string, error) {
	if home := os.Getenv("HOME"); home != "" && filepath.IsAbs(home) {
		return home, nil
	}
	// os.UserHomeDir is not a fallback here: on Unix it only re-reads $HOME,
	// so a HOME this function just rejected (relative, or unset when it is the
	// only source) comes straight back, or errors. The account database is the
	// platform's own answer, independent of the environment.
	if u, err := user.Current(); err == nil && u.HomeDir != "" && filepath.IsAbs(u.HomeDir) {
		return u.HomeDir, nil
	}
	return os.UserHomeDir()
}

// MustDir is Dir with the error dropped; returns "" when no home can be
// resolved. For callers that already treated an error as "no home".
func MustDir() string {
	d, err := Dir()
	if err != nil {
		return ""
	}
	return d
}
