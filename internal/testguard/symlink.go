package testguard

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// Symlink creates newname as a symbolic link to oldname, failing the test on
// any error EXCEPT the one Windows returns when the process lacks the
// SeCreateSymbolicLinkPrivilege (no Developer Mode, not elevated). In that
// case the test is skipped: the behavior under test is real and portable,
// the box just cannot host the fixture.
//
// Use this instead of os.Symlink in tests. The 40-odd existing sites that
// spell out `if err := os.Symlink(...); err != nil { t.Skipf(...) }` by hand
// are the same rule; this is the one place it lives.
func Symlink(t testing.TB, oldname, newname string) {
	t.Helper()
	err := os.Symlink(oldname, newname)
	if err == nil {
		return
	}
	if IsSymlinkPrivilegeError(err) {
		t.Skipf("symlinks unavailable on this host (enable Developer Mode or run elevated): %v", err)
	}
	t.Fatalf("symlink %s -> %s: %v", newname, oldname, err)
}

// IsSymlinkPrivilegeError reports whether err is Windows' ERROR_PRIVILEGE_NOT_HELD
// (1314) — "A required privilege is not held by the client" — which is what
// os.Symlink returns on a stock Windows install without Developer Mode.
func IsSymlinkPrivilegeError(err error) bool {
	if err == nil || runtime.GOOS != "windows" {
		return false
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == 1314 {
		return true
	}
	return strings.Contains(err.Error(), "required privilege is not held")
}

// SymlinksAvailable reports whether this process can create symlinks, by
// trying one in a temp dir. Use it to skip a whole test up front when the
// fixture would otherwise be built in several steps.
func SymlinksAvailable(t testing.TB) bool {
	t.Helper()
	dir := t.TempDir()
	target := dir + string(os.PathSeparator) + "t"
	link := dir + string(os.PathSeparator) + "l"
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatalf("probe write: %v", err)
	}
	err := os.Symlink(target, link)
	if err == nil {
		return true
	}
	if IsSymlinkPrivilegeError(err) {
		return false
	}
	t.Fatalf("probe symlink: %v", err)
	return false
}
