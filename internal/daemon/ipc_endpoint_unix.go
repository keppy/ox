//go:build !windows

package daemon

// endpointPath is the socket path itself on POSIX systems.
func endpointPath(socketPath string) string { return socketPath }
