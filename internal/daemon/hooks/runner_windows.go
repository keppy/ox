//go:build windows

package hooks

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// newHookCmd builds the exec.Cmd used to run a hook's command.
//
// A hook command line is a POSIX shell command line: the `ox hooks add` help
// examples use pipes, `$(cat)` and quoting, and the Unix runner executes them
// with `sh -c`. cmd.exe is not a substitute — it re-parses the command line
// (`%VAR%` expansion, `|` and `&` as operators, no `$VAR` expansion at all),
// so a POSIX hook run through it silently does something else or nothing.
//
// Windows therefore runs hooks through the POSIX shell Git for Windows ships
// (git-bash is part of the documented ox install path) and only falls back to
// cmd.exe when no POSIX shell is installed, saying so — a hook that quietly
// stops matching its own command line is the failure mode this avoids.
func newHookCmd(command string, logger *slog.Logger) *exec.Cmd {
	if shell := posixShell(); shell != "" {
		return exec.Command(shell, "-c", command)
	}
	if logger != nil {
		logger.Warn("hook shell: no POSIX shell found, running hook command through cmd.exe; "+
			"POSIX syntax ($VAR, $(...), pipes) will not work — install Git for Windows for bash",
			"command", command)
	}
	return exec.Command("cmd.exe", "/c", command)
}

// posixShell returns the POSIX shell to run hook commands through, or "" when
// the machine has none (then newHookCmd falls back to cmd.exe).
//
// A Git for Windows install is checked first — it is the shell the ox install
// path documents and the only one guaranteed to be present on a supported
// Windows box — then bash/sh on PATH. WSL's C:\Windows\System32\bash.exe is
// rejected: it starts a Linux VM that cannot resolve the Windows paths hooks
// are handed, so it would break every hook rather than run them.
func posixShell() string {
	for _, dir := range gitInstallDirs() {
		for _, rel := range []string{
			filepath.Join("bin", "bash.exe"),
			filepath.Join("usr", "bin", "bash.exe"),
			filepath.Join("usr", "bin", "sh.exe"),
		} {
			p := filepath.Join(dir, rel)
			if isExecutableFile(p) {
				return p
			}
		}
	}
	for _, name := range []string{"bash", "sh"} {
		if p, err := exec.LookPath(name); err == nil && !isWSLStub(p) {
			return p
		}
	}
	return ""
}

// gitInstallDirs lists the directories a Git for Windows install may live in,
// in preference order (per-machine before per-user).
func gitInstallDirs() []string {
	var dirs []string
	for _, env := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
		if root := os.Getenv(env); root != "" {
			dirs = append(dirs, filepath.Join(root, "Git"))
		}
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		dirs = append(dirs, filepath.Join(local, "Programs", "Git"))
	}
	return dirs
}

// isExecutableFile reports whether p is an existing regular file. Windows has
// no executable bit; what makes a file runnable is its extension, which the
// callers here pin explicitly (.exe).
func isExecutableFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// isWSLStub reports whether p is WSL's bash launcher (the one Windows ships in
// System32), which runs a Linux distribution rather than a Windows shell.
func isWSLStub(p string) bool {
	system32 := filepath.Join(os.Getenv("SystemRoot"), "System32")
	return strings.EqualFold(filepath.Dir(p), system32)
}

// setProcessGroup starts the hook in a new process group ID (equal to its
// own PID) so terminateProcessGroup can target the group with a console
// control event, and so a Ctrl+Break delivered to ox's own console doesn't
// also land on the hook.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// terminateProcessGroup asks the hook's process group to exit by delivering
// CTRL_BREAK_EVENT — the Windows analog of SIGTERM for a process group.
//
// Caveat for reviewers: CTRL_BREAK_EVENT only reaches console-aware
// processes that installed a handler for it; cmd.exe and most simple child
// processes ignore it and keep running. It is NOT a reliable full-tree
// terminator on its own (unlike SIGTERM to a Unix process group, which the
// kernel delivers unconditionally). killProcessGroup below is the backstop
// that actually reaps a stuck hook tree on Windows.
func terminateProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(cmd.Process.Pid))
}

// killProcessGroup force-kills the entire process tree rooted at the hook's
// PID via taskkill /T /F. Windows has no SIGKILL-to-process-group equivalent
// in the stdlib syscall/exec surface (a full descendant-tree kill without a
// job object needs either CGo or golang.org/x/sys/windows job-object APIs,
// neither of which this package otherwise needs), so shelling out to the
// built-in taskkill utility is the pragmatic, dependency-free choice here.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
}
