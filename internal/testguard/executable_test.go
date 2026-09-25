package testguard

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// Failure prevented: a fixture launcher that re-parses its command line
// (a .cmd shim) turns `HEAD^{tree}` into `HEADtree` and executes the `y`
// in `x&y`, so a git-wrapping fixture silently runs the wrong command and
// every ReadSync test on Windows fails with an unrelated git error.
func TestWriteShellExecutable_PassesArgumentsVerbatim(t *testing.T) {
	dir := t.TempDir()
	bin := WriteShellExecutable(t, dir, "argecho", "#!/bin/sh\nfor a in \"$@\"; do printf '[%s]' \"$a\"; done\nexit 3\n")

	out, err := exec.Command(bin, "HEAD^{tree}", "a b", "x&y", "%PATH%", "50%", `q"uote`, "").CombinedOutput()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "exit code must propagate: %s", out)
	require.Equal(t, 3, exitErr.ExitCode())
	require.Equal(t, `[HEAD^{tree}][a b][x&y][%PATH%][50%][q"uote][]`, string(out))
}
