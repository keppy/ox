//go:build darwin || linux

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sageox/ox/internal/daemon"
	"github.com/stretchr/testify/require"
)

// startFakeDaemon starts a minimal Unix-socket server speaking the daemon
// IPC wire format (one newline-delimited JSON Message in, one
// newline-delimited JSON Response out) and returns its socket path. respond
// computes the reply for each received message; the server handles exactly
// one message per connection, matching Client.sendMessage's connect-write-
// read-close pattern.
//
// This lives in its own unix-tagged file rather than beside its callers
// because it is the one fake in cmd/ox that cannot exist on Windows: the wire
// transport it imitates is an AF_UNIX socket, and net.Listen("unix", …) has no
// Windows equivalent (the daemon uses named pipes there, which is what
// doctor_team_daemon_test.go exercises instead). Keeping the tag here means the
// Windows build never sees it, and the callers that do use it — the test files
// already tagged darwin||linux — still compile.
func startFakeDaemon(t *testing.T, respond func(daemon.Message) daemon.Response) string {
	t.Helper()
	// os.TempDir(), not t.TempDir(): a long test name nests t.TempDir() deep
	// enough to exceed macOS's ~104-char AF_UNIX path limit ("bind: invalid
	// argument"). Same workaround as internal/daemon/friction_test.go.
	sock := filepath.Join(os.TempDir(), fmt.Sprintf("ox-fake-daemon-%d.sock", time.Now().UnixNano()))
	t.Cleanup(func() { os.Remove(sock) })
	listener, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				line, err := bufio.NewReader(c).ReadBytes('\n')
				if err != nil {
					return
				}
				var msg daemon.Message
				if err := json.Unmarshal(line, &msg); err != nil {
					return
				}
				data, err := json.Marshal(respond(msg))
				if err != nil {
					return
				}
				data = append(data, '\n')
				_, _ = c.Write(data)
			}(conn)
		}
	}()

	return sock
}
