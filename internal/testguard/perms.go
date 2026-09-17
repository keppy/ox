package testguard

import (
	"os"
	"runtime"
	"testing"
)

// RequirePOSIXPerms skips the test when the platform cannot enforce the
// permission-denied condition the test is about to create with os.Chmod.
//
// Go's os.Chmod on Windows maps only the read-only attribute: removing the
// read or execute bit from a directory (0o300, 0o500, 0o000) changes nothing,
// so os.ReadDir / os.CreateTemp keep succeeding and the test asserts on a
// failure that never happened. Root on Unix bypasses mode bits the same way.
// Call this at the top of any test whose failure injection is a chmod.
func RequirePOSIXPerms(t testing.TB) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod cannot deny directory access on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
}
