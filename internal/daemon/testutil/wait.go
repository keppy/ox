package testutil

import (
	"testing"
	"time"

	"github.com/sageox/ox/internal/daemon"

	"github.com/stretchr/testify/require"
)

// AwaitDaemonEndpoint polls until the daemon's endpoint accepts connections:
// a unix socket on POSIX platforms, a named pipe on Windows. Completes
// immediately when the endpoint is ready (typically <5ms), times out with a
// clear message if it never becomes available.
func AwaitDaemonEndpoint(t *testing.T, socketPath string) {
	t.Helper()
	require.Eventually(t, func() bool {
		conn, err := daemon.DialEndpoint(socketPath)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}, 5*time.Second, 5*time.Millisecond,
		"daemon endpoint never became ready: %s", socketPath)
}
