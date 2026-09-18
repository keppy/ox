//go:build windows

package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// listen creates a Windows named pipe listener.
// SECURITY: Pipe is created with SDDL that restricts access to current user only.
// This prevents other users on the same machine from connecting to the daemon.
func listen(path string) (net.Listener, error) {
	pipePath := `\\.\pipe\` + pipeName(path)

	// get current user's SID for SDDL
	sddl, err := currentUserSDDL()
	if err != nil {
		return nil, fmt.Errorf("get user SDDL: %w", err)
	}

	cfg := &winio.PipeConfig{
		SecurityDescriptor: sddl,
		MessageMode:        false,
		InputBufferSize:    4096,
		OutputBufferSize:   4096,
	}
	return winio.ListenPipe(pipePath, cfg)
}

// currentUserSDDL returns an SDDL string that grants full access only to the current user.
// Format: D:P(A;;GA;;;SID) = DACL with full access for the specified SID
func currentUserSDDL() (string, error) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("get token user: %w", err)
	}
	sidStr := user.User.Sid.String()
	// D:P = DACL present, protected (no inheritance)
	// A = Allow
	// GA = Generic All (full access)
	// SID = user's SID
	return fmt.Sprintf("D:P(A;;GA;;;%s)", sidStr), nil
}

// dial connects to a Windows named pipe with a timeout.
// Uses 5 second timeout to prevent indefinite hangs if daemon is stuck.
func dial(path string) (net.Conn, error) {
	pipePath := `\\.\pipe\` + pipeName(path)
	timeout := 5 * time.Second
	conn, err := winio.DialPipe(pipePath, &timeout)
	if err != nil {
		return nil, err
	}
	return &deadlineConn{Conn: conn}, nil
}

// deadlineConn maps go-winio's private timeout error onto
// os.ErrDeadlineExceeded, which is what net.Conn implementations in the
// standard library return and what every caller in this package checks
// with errors.Is. Without it a stalled daemon is classified as a protocol
// failure on Windows instead of a timeout.
type deadlineConn struct{ net.Conn }

func (c *deadlineConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	return n, mapTimeout(err)
}

func (c *deadlineConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	return n, mapTimeout(err)
}

func mapTimeout(err error) error {
	if err != nil && errors.Is(err, winio.ErrTimeout) {
		return fmt.Errorf("%w: %w", os.ErrDeadlineExceeded, err)
	}
	return err
}

// endpointExists reports whether the daemon's named pipe is currently
// bound. The pipe namespace has no stale entries — a name exists only while
// a server holds it — so this is a strict "listener present" check, unlike
// a Unix socket file left behind by a crash. os.Stat cannot see pipe
// paths; CreateFile with OPEN_EXISTING can, and ERROR_PIPE_BUSY (every
// instance currently connected) still means a server is there.
func endpointExists(path string) bool {
	name, err := windows.UTF16PtrFromString(`\\.\pipe\` + pipeName(path))
	if err != nil {
		return false
	}
	h, err := windows.CreateFile(name, 0, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err == nil {
		_ = windows.CloseHandle(h)
		return true
	}
	return errors.Is(err, windows.ERROR_PIPE_BUSY)
}

// cleanupSocket is a no-op on Windows (pipes are cleaned up automatically).
func cleanupSocket(path string) {
	// no-op: Windows named pipes are automatically cleaned up when closed
}
