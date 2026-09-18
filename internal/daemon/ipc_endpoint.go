package daemon

import (
	"net"
	"sync"
	"time"
)

// EndpointPath maps a daemon socket path to the address the server binds and
// the client dials on this platform: a named pipe derived from the path on
// Windows, the path itself on POSIX systems.
//
// Test doubles (internal/daemon/testutil, pkg/faultdaemon) must bind exactly
// what the real client dials. Exposing the mapping keeps that in one place:
// reimplementing it is how the mocks came to bind unix sockets on Windows and
// fail every round trip.
func EndpointPath(socketPath string) string { return endpointPath(socketPath) }

// ListenEndpoint binds socketPath's platform endpoint the way the server does.
func ListenEndpoint(socketPath string) (net.Listener, error) { return listen(socketPath) }

// DialEndpoint connects to socketPath's platform endpoint the way the client does.
func DialEndpoint(socketPath string) (net.Conn, error) { return dial(socketPath) }

// StopEndpoint closes an endpoint bound by ListenEndpoint and waits for wg,
// without ever blocking forever.
//
// go-winio's pipe listener can deadlock in Close while an Accept is pending:
// Close waits on the listener routine, and the listener routine is waiting to
// complete the accept. A unix socket's Close unblocks Accept immediately, which
// is why this only bites on Windows — and only for a caller that deliberately
// leaves an accept in flight, such as the "refuse after accept" fault. Test
// doubles stop through here so a stuck close cannot hang a test run: the
// endpoint is closed either way, and a goroutine that never returns exits with
// the test binary.
func StopEndpoint(l net.Listener, wg *sync.WaitGroup) {
	if l != nil {
		go func() { _ = l.Close() }()
	}
	done := make(chan struct{})
	go func() {
		if wg != nil {
			wg.Wait()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}
