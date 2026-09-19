//go:build windows

package gitserver

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// Match the daemon hook runner's native Windows descendant termination.
func configureReadProcess(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, "taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
			killErr := cmd.Process.Kill()
			// Windows releases the process handle when Wait reaps the
			// process, so Kill on an already-finished command reports
			// EINVAL ("invalid argument") instead of os.ErrProcessDone.
			// Normalize it, mirroring the Unix path's ESRCH handling:
			// without this, a cancel racing a normal exit is reported to
			// exec's context watcher as "exec: canceling Cmd: invalid
			// argument" — a spurious failure the Unix build never sees.
			if errors.Is(killErr, syscall.EINVAL) {
				return os.ErrProcessDone
			}
			return killErr
		}
		return nil
	}
}
