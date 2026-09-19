//go:build windows

package daemon

// endpointPath is the named pipe derived from the socket path on Windows.
func endpointPath(socketPath string) string { return `\\.\pipe\` + pipeName(socketPath) }
