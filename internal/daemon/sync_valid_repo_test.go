package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Failure prevented: a bubble whose .git was emptied by an interrupted
// clone sits inside a temp dir that is itself under a git repository (a
// developer whose $HOME is a checkout, CI runners). `git rev-parse
// --git-dir` then walks up and succeeds for the *enclosing* repo, so the
// corrupt bubble was reported healthy, never moved aside, and every pull
// after that failed the same way.
func TestIsValidGitRepo_NestedCorruptCheckoutIsNotValid(t *testing.T) {
	outer := t.TempDir()
	run := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s: %s", args, out)
	}
	run(outer, "init", "-q")

	inner := filepath.Join(outer, "bubbles", "kb_x")
	require.NoError(t, os.MkdirAll(inner, 0o755))
	run(inner, "init", "-q")
	require.True(t, isValidGitRepo(inner), "a real nested repo is valid")

	// corrupt: .git becomes an empty directory
	require.NoError(t, os.RemoveAll(filepath.Join(inner, ".git")))
	require.NoError(t, os.MkdirAll(filepath.Join(inner, ".git"), 0o755))
	require.False(t, isValidGitRepo(inner), "git resolving to the enclosing repo must not count")

	// missing entirely
	require.False(t, isValidGitRepo(filepath.Join(outer, "does-not-exist")))
}
