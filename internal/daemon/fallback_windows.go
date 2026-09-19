//go:build windows

package daemon

import (
	"fmt"
	"os"
	"syscall"

	"github.com/sageox/ox/internal/proc"
)

// sigTERM is the "graceful" termination request. Windows has no SIGTERM;
// signalProcess maps any non-zero signal to TerminateProcess, so the value
// is only a label for the escalation log lines in KillStaleDaemon.
const sigTERM = syscall.Signal(0xF)

// sigKILL mirrors sigTERM: both reach TerminateProcess on Windows.
const sigKILL = syscall.Signal(0x9)

// isOxDaemonProcess reports whether pid is an ox process, as the PID-reuse
// guard KillStaleDaemon consults before terminating it.
//
// Windows exposes a process's executable name cheaply (Toolhelp32) but its
// full command line only via the target's PEB, which needs PROCESS_VM_READ.
// So this checks argv[0] — the same first half of the Unix matchesOxDaemon
// test — and cannot verify the "daemon" subcommand. That is still a real
// guard: the previous implementation returned true unconditionally, which
// would TerminateProcess whatever unrelated program had inherited the PID.
// A false positive now requires the reused PID to belong to another ox
// invocation, and a stray `ox status` killed by mistake is recoverable in a
// way that a killed editor is not.
func isOxDaemonProcess(pid int) bool {
	return oxDaemonExecutables[proc.Name(pid)] || oxDaemonExecutables[proc.Name(pid)+".exe"]
}

// oxDaemonExecutables mirrors fallback_unix.go: the argv[0] basenames a real
// ox daemon can have. "ox.test" is deliberately absent so a test binary is
// never mistaken for a daemon.
var oxDaemonExecutables = map[string]bool{"ox": true, "ox.exe": true}

// signalProcess sends a signal to pid. Signal 0 is a liveness probe; any
// other value terminates the process.
//
// os.FindProcess never fails on Windows, even for a PID that names nothing,
// and os.Process.Signal(0) returns EWINDOWS, so neither can answer "is it
// alive?". proc.IsAlive opens the process and reads its exit code instead.
func signalProcess(pid int, sig syscall.Signal) error {
	if sig == 0 {
		if !proc.IsAlive(pid) {
			return os.ErrProcessDone
		}
		return nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := p.Kill(); err != nil {
		return fmt.Errorf("kill process %d: %w", pid, err)
	}
	return nil
}
