package testguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// WriteShellExecutable writes a POSIX shell script that tests can execute as
// a fake binary named base (for example "ox-adapter-fake") and returns the
// path of the file to invoke.
//
// On Unix it is simply dir/base with mode 0755. Windows will not run a
// shebang file, so there the script is written to dir/base.sh and a
// dir/base.exe launcher is placed beside it that hands stdin, arguments,
// environment, and exit code through to bash. Production discovery finds
// the .exe via fileutil.FindExecutable / PATHEXT, exactly as it would find a
// real adapter, so the test exercises the same lookup path a Windows install
// does.
//
// The launcher is a real executable rather than a .cmd shim on purpose:
// cmd.exe re-parses its command line, so `^`, `&`, `%`, `|` and quotes in
// arguments are mangled or executed (`HEAD^{tree}` arrives as `HEADtree`,
// `x&y` runs `y`). A native launcher receives argv exactly as the caller
// passed it.
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

	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("shell-script fixtures need bash on PATH (install Git for Windows)")
	}
	sh := filepath.Join(dir, base+".sh")
	if err := os.WriteFile(sh, []byte(strings.ReplaceAll(script, "\r\n", "\n")), 0o755); err != nil {
		t.Fatalf("write %s: %v", sh, err)
	}
	exe := filepath.Join(dir, base+".exe")
	if err := copyFile(shellLauncher(t), exe); err != nil {
		t.Fatalf("install launcher %s: %v", exe, err)
	}
	return exe
}

// shellLauncherSrc is the launcher program. It runs the .sh that shares its
// own basename (foo.exe → foo.sh) under bash, so one compiled binary serves
// every fixture in a test process.
const shellLauncherSrc = `package main

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// MSYS bash re-parses its Windows command line with its own quoting and
// glob rules ("HEAD^{tree}" loses its braces, an empty argument merges into
// its neighbour), so argv is not handed to bash as arguments. Each one is
// base64-encoded and NUL-joined into OX_FIXTURE_ARGS; a fixed prologue
// decodes them and re-execs the fixture script with a faithful "$@".
func main() {
	self, err := os.Executable()
	if err != nil {
		os.Exit(127)
	}
	script := strings.TrimSuffix(self, filepath.Ext(self)) + ".sh"
	enc := make([]string, 0, len(os.Args)-1)
	for _, a := range os.Args[1:] {
		enc = append(enc, base64.StdEncoding.EncodeToString([]byte(a)))
	}
	// Every argument is terminated by ':' (so an empty argument is an empty
	// field, not a dropped one) and decoded in a read loop rather than by
	// word-splitting, which would collapse empty fields.
	prologue := "set -- ; while IFS= read -r -d : e; do set -- \"$@\" \"$(printf %s \"$e\" | base64 -d)\"; done <<EOF\n$OX_FIXTURE_ARGS\nEOF\nexec \"$OX_FIXTURE_SCRIPT\" \"$@\""
	cmd := exec.Command("bash", "-c", prologue)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(),
		"OX_FIXTURE_ARGS="+strings.Join(enc, ":")+terminator(len(enc)),
		"OX_FIXTURE_SCRIPT="+filepath.ToSlash(script))
	// Bind bash (and anything it spawns) to this process: when the test
	// kills the fixture, Windows tears the whole job down. Without this a
	// killed launcher leaves bash running and holding the caller's pipes,
	// so cmd.Wait blocks until WaitDelay — the opposite of what a
	// "subprocess was canceled promptly" test is checking.
	job := bindToJob()
	if err := cmd.Start(); err != nil {
		os.Exit(127)
	}
	if job != 0 {
		h, _ := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
		if h != 0 {
			_ = windows.AssignProcessToJobObject(job, h)
			_ = windows.CloseHandle(h)
		}
	}
	if err := cmd.Wait(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		os.Exit(127)
	}
}

// bindToJob creates a job object that kills every member when its last
// handle closes — i.e. when this launcher exits or is killed.
func bindToJob() windows.Handle {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	_, _ = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	return job
}

func terminator(n int) string {
	if n == 0 {
		return ""
	}
	return ":"
}
`

var (
	launcherOnce sync.Once
	launcherPath string
	launcherErr  error
)

// shellLauncher builds the launcher once per test binary and returns its path.
func shellLauncher(t testing.TB) string {
	t.Helper()
	launcherOnce.Do(func() {
		dir, err := os.MkdirTemp("", "ox-shell-launcher-")
		if err != nil {
			launcherErr = err
			return
		}
		src := filepath.Join(dir, "main.go")
		if err := os.WriteFile(src, []byte(shellLauncherSrc), 0o644); err != nil {
			launcherErr = err
			return
		}
		// a throwaway module so the build does not depend on the caller's cwd
		// golang.org/x/sys is already in the ox module graph; pin the
		// launcher to the same version so the build resolves from the local
		// module cache and never reaches the network.
		xsys, err := exec.Command("go", "list", "-m", "-f", "{{.Version}}", "golang.org/x/sys").Output()
		if err != nil {
			launcherErr = err
			return
		}
		gomod := "module launcher\n\ngo 1.22\n\nrequire golang.org/x/sys " + strings.TrimSpace(string(xsys)) + "\n"
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
			launcherErr = err
			return
		}
		out := filepath.Join(dir, "launcher.exe")
		cmd := exec.Command("go", "build", "-o", out, ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-mod=mod", "GOPROXY=off")
		if b, err := cmd.CombinedOutput(); err != nil {
			launcherErr = &launcherBuildError{out: string(b), err: err}
			return
		}
		launcherPath = out
	})
	if launcherErr != nil {
		t.Fatalf("build shell launcher: %v", launcherErr)
	}
	return launcherPath
}

type launcherBuildError struct {
	out string
	err error
}

func (e *launcherBuildError) Error() string { return e.err.Error() + ": " + e.out }

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
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
