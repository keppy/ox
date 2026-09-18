//go:build windows

package proc

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Detach starts the child in its own process group and without a console
// so tool-runner cleanup (which signals ox's own console group) does not
// take the daemon down with it. This is the closest Windows analog of
// Setsid: the child no longer shares our console or our Ctrl-C group.
func Detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
}

// stillActive is the exit code GetExitCodeProcess reports for a process that has
// not exited (STILL_ACTIVE in the Win32 headers, 259). x/sys/windows exposes the
// same value as STATUS_PENDING but not under the STILL_ACTIVE name, so spell it
// out rather than borrowing a constant that means something else.
const stillActive = 259

// snapshotEntry looks up pid in a Toolhelp32 process snapshot. Toolhelp is the
// documented, non-privileged way to read another process's name and parent on
// Windows; /proc and ps(1) have no equivalent here.
func snapshotEntry(pid int) (windows.ProcessEntry32, bool) {
	var zero windows.ProcessEntry32
	if pid <= 0 {
		return zero, false
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return zero, false
	}
	defer func() { _ = windows.CloseHandle(snap) }()

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if int(entry.ProcessID) == pid {
			return entry, true
		}
	}
	return zero, false
}

// parentPID returns the parent PID of pid via a Toolhelp32 snapshot.
func parentPID(pid int) (int, error) {
	entry, ok := snapshotEntry(pid)
	if !ok {
		return 0, os.ErrProcessDone
	}
	return int(entry.ParentProcessID), nil
}

// processName returns the executable base name for pid, lower-cased and with
// the .exe suffix removed so it compares equal to the Unix spelling that
// knownAgentBinaries and matchesAgent expect ("claude", not "claude.exe").
func processName(pid int) string {
	entry, ok := snapshotEntry(pid)
	if !ok {
		return ""
	}
	name := windows.UTF16ToString(entry.ExeFile[:])
	return strings.TrimSuffix(strings.ToLower(name), ".exe")
}

// isAliveProc reports whether a process is still running.
//
// This used to return `proc != nil`, which is ALWAYS true: os.FindProcess never
// fails on Windows, so IsAlive reported every PID — including long-dead ones —
// as alive. Callers that use liveness to decide whether to restart something
// (carts' dolt server) would therefore trust a dead record forever.
//
// Signal(0) is not an option either: Go's Windows os.Process.Signal supports only
// os.Kill and returns syscall.EWINDOWS for anything else. Open the process and
// ask for its exit code instead.
func isAliveProc(proc *os.Process) bool {
	if proc == nil {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(proc.Pid))
	if err != nil {
		// Most often ERROR_INVALID_PARAMETER: the PID names nothing. A permission
		// failure also lands here; reporting "not alive" is the safe answer, since
		// a process we cannot query is one we cannot manage either.
		return false
	}
	defer func() { _ = windows.CloseHandle(h) }()

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// terminateProc kills the process. os.Interrupt is not deliverable on Windows —
// os.Process.Signal returns syscall.EWINDOWS for it — so attempting a graceful
// signal here would fail while reporting nothing useful to the caller.
func terminateProc(proc *os.Process) error {
	return proc.Kill()
}
