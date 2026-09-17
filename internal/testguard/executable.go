package testguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// WriteShellExecutable writes a POSIX shell script that tests can execute as
// a fake binary named base (for example "ox-adapter-fake") and returns the
// path of the file to invoke.
//
// On Unix it is simply dir/base with mode 0755. Windows will not run a
// shebang file, so there the script is written to dir/base.sh and a tiny
// dir/base.cmd shim is generated that hands stdin, arguments, and exit code
// through to bash. Production discovery finds the .cmd via
// fileutil.FindExecutable / PATHEXT, exactly as it would find a real .exe,
// so the test exercises the same lookup path a Windows install does.
//
// Requires bash on PATH on Windows (Git for Windows provides it). The test is
// skipped, not failed, when it is missing.
func WriteShellExecutable(t testing.TB, dir, base, script string) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		p := filepath.Join(dir, base)
		if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
		return p
	}

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("shell-script fixtures need bash on PATH (install Git for Windows)")
	}
	sh := filepath.Join(dir, base+".sh")
	if err := os.WriteFile(sh, []byte(strings.ReplaceAll(script, "\r\n", "\n")), 0o755); err != nil {
		t.Fatalf("write %s: %v", sh, err)
	}
	// %* forwards every argument; stdin is inherited; exit code propagates.
	// The script path is passed in Windows form — MSYS bash accepts it.
	shim := "@echo off\r\n\"" + bash + "\" \"" + sh + "\" %*\r\nexit /b %ERRORLEVEL%\r\n"
	cmd := filepath.Join(dir, base+".cmd")
	if err := os.WriteFile(cmd, []byte(shim), 0o755); err != nil {
		t.Fatalf("write %s: %v", cmd, err)
	}
	return cmd
}

// ExeName returns base with ".exe" appended on Windows and unchanged
// elsewhere — the file name `go build -o` should be given so the result is
// runnable on the current platform.
func ExeName(base string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(base), ".exe") {
		return base + ".exe"
	}
	return base
}
