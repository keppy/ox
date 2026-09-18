package daemon

import (
	"os"
	"runtime"
)

// shortRuntimeDir returns a directory for XDG_RUNTIME_DIR in IPC tests.
// Unix socket paths are capped at ~104 bytes, so /tmp is used verbatim
// rather than t.TempDir(). On Windows the endpoint is a named pipe whose
// name is a hash of the path, so length is irrelevant — but /tmp does not
// exist, so the OS temp dir is used.
func shortRuntimeDir() string {
	if runtime.GOOS == "windows" {
		return os.TempDir()
	}
	return "/tmp"
}
