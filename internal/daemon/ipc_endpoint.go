package daemon

import "net"

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
