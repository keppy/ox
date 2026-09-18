package testguard

import (
	"path/filepath"
	"runtime"
	"strings"
)

// FakeHome returns posixHome (e.g. "/Users/someone") as a path that
// filepath.IsAbs accepts on the running platform. Allow-list and path-policy
// tests need a fixed, fake home directory that is absolute but never touches
// the real filesystem; "/Users/someone" is not absolute on Windows (no
// volume), so those tests fail there for a reason unrelated to the logic
// under test.
//
// On Windows the result is `C:\Users\someone`; elsewhere posixHome is
// returned unchanged. Pair with FakePath to build children of that home.
func FakeHome(posixHome string) string {
	if runtime.GOOS != "windows" {
		return posixHome
	}
	return `C:` + filepath.FromSlash(posixHome)
}

// FakePath converts a POSIX-style absolute path under the same fake root as
// FakeHome to the platform form. Relative paths and the empty string are
// returned unchanged so "reject: relative path" cases keep their meaning.
func FakePath(posix string) string {
	if runtime.GOOS != "windows" || !strings.HasPrefix(posix, "/") {
		return posix
	}
	return `C:` + filepath.FromSlash(posix)
}
