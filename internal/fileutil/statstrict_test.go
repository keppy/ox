package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Failure prevented: a regular file sitting where a directory is expected
// reads as "not exist" on Windows, so a guard that only protects existing
// content would treat blocked content as absent and proceed destructively.
func TestStatStrict_FileAsParentIsNotNotExist(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "cache")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := StatStrict(filepath.Join(blocker, "sessions", "a", "raw.jsonl"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file-as-parent must not read as not-exist: %v", err)
	}
	if !errors.Is(err, ErrNotDirectory) {
		t.Fatalf("want ErrNotDirectory, got %v", err)
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("want *os.PathError, got %T", err)
	}

	// genuinely missing under a real directory stays ErrNotExist
	_, err = StatStrict(filepath.Join(dir, "nope", "raw.jsonl"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want ErrNotExist for a missing path, got %v", err)
	}
	// existing file behaves like os.Stat
	if _, err := StatStrict(blocker); err != nil {
		t.Fatal(err)
	}
}
