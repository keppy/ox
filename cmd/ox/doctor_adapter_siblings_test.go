package main

import (
	"github.com/sageox/ox/internal/testguard"

	"fmt"

	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAdapterSiblingsResult_AllPresent covers the healthy install: every
// adapter goreleaser ships sits next to the ox binary.
func TestAdapterSiblingsResult_AllPresent(t *testing.T) {
	dir := t.TempDir()
	for _, name := range expectedAdapterSiblings {
		touchFile(t, filepath.Join(dir, testguard.ExeName("ox-adapter-"+name)))
	}

	result := adapterSiblingsResult([]string{dir})

	if !result.passed || result.warning {
		t.Fatalf("expected a clean pass, got %+v", result)
	}
	if result.message != fmt.Sprintf("%d/%d present", len(expectedAdapterSiblings), len(expectedAdapterSiblings)) {
		t.Errorf("message = %q, want %q", result.message, fmt.Sprintf("%d/%d present", len(expectedAdapterSiblings), len(expectedAdapterSiblings)))
	}
}

// TestAdapterSiblingsResult_MissingSome is the reported failure: a
// symlinked or partial install leaves some adapters undiscoverable, and
// the check must name exactly which ones plus where it looked.
func TestAdapterSiblingsResult_MissingSome(t *testing.T) {
	dir := t.TempDir()
	touchFile(t, filepath.Join(dir, testguard.ExeName("ox-adapter-codex")))
	touchFile(t, filepath.Join(dir, testguard.ExeName("ox-adapter-gemini")))
	// every other expected adapter is absent

	result := adapterSiblingsResult([]string{dir})

	if !result.warning {
		t.Fatalf("expected a warning, got %+v", result)
	}
	if result.message != fmt.Sprintf("2/%d present", len(expectedAdapterSiblings)) {
		t.Errorf("message = %q, want %q", result.message, fmt.Sprintf("2/%d present", len(expectedAdapterSiblings)))
	}
	for _, want := range []string{"aider", "amp", "claude-code", "droid", "goose", "hermes", "omp", "opencode", "pi"} {
		if !strings.Contains(result.detail, want) {
			t.Errorf("detail missing %q: %s", want, result.detail)
		}
	}
	if strings.Contains(result.detail, "codex") || strings.Contains(result.detail, "gemini") {
		t.Errorf("detail should only list MISSING adapters, not present ones: %s", result.detail)
	}
	if !strings.Contains(result.detail, dir) {
		t.Errorf("detail should name the scanned path %q: %s", dir, result.detail)
	}
}

// TestAdapterSiblingsResult_UnionsAcrossMultipleDirs covers the
// symlink-aware fix: adapters split across the unresolved and resolved
// bundled dirs (both returned by adapters.BundledAdapterDirs) must still
// count as present.
func TestAdapterSiblingsResult_UnionsAcrossMultipleDirs(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	for i, name := range expectedAdapterSiblings {
		dir := dirA
		if i%2 == 1 {
			dir = dirB
		}
		touchFile(t, filepath.Join(dir, testguard.ExeName("ox-adapter-"+name)))
	}

	result := adapterSiblingsResult([]string{dirA, dirB})

	if !result.passed || result.warning {
		t.Fatalf("expected a clean pass when siblings are split across scanned dirs, got %+v", result)
	}
}

// TestAdapterSiblingsResult_IgnoresUnrelatedFiles ensures a directory full
// of other binaries (the ox binary itself, ox-adapter-test, random files)
// never counts toward the expected set.
func TestAdapterSiblingsResult_IgnoresUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	touchFile(t, filepath.Join(dir, "ox"))
	touchFile(t, filepath.Join(dir, testguard.ExeName("ox-adapter-test"))) // dev-only, not in expectedAdapterSiblings
	touchFile(t, filepath.Join(dir, "some-other-tool"))

	result := adapterSiblingsResult([]string{dir})

	if result.message != fmt.Sprintf("0/%d present", len(expectedAdapterSiblings)) {
		t.Errorf("message = %q, want %q", result.message, fmt.Sprintf("0/%d present", len(expectedAdapterSiblings)))
	}
}

// TestAdapterSiblingsResult_NonExecutableNotCounted is the red-first proof
// that a present-but-not-executable ox-adapter-* must not count as
// "present": internal/session/adapters/discovery.go's own resolver requires
// the executable bit (fi.Mode()&0111 != 0) before it will run a binary as
// an adapter, so a check that counted a non-executable file anyway would
// report fmt.Sprintf("%d/%d present", len(expectedAdapterSiblings), len(expectedAdapterSiblings)) while session hooks silently no-op on that
// adapter -- lying in the same direction as the bug this check exists to
// catch.
func TestAdapterSiblingsResult_NonExecutableNotCounted(t *testing.T) {
	dir := t.TempDir()
	// codex is executable and must count; gemini is present but not
	// executable and must NOT count.
	touchFile(t, filepath.Join(dir, testguard.ExeName("ox-adapter-codex")))
	nonExecPath := filepath.Join(dir, "ox-adapter-gemini")
	if err := os.WriteFile(nonExecPath, []byte("#!/bin/sh\n"), 0644); err != nil {
		t.Fatalf("write non-executable adapter: %v", err)
	}

	result := adapterSiblingsResult([]string{dir})

	if result.message != fmt.Sprintf("1/%d present", len(expectedAdapterSiblings)) {
		t.Errorf("message = %q, want %q (gemini is present but not executable, must not count)", result.message, fmt.Sprintf("1/%d present", len(expectedAdapterSiblings)))
	}
	if !strings.Contains(result.detail, "gemini") {
		t.Errorf("detail should list gemini as missing since it isn't executable: %s", result.detail)
	}
}

// TestAdapterSiblingsResult_NoDirs covers the case where
// adapters.BundledAdapterDirs itself could not determine anything (e.g.
// os.Executable failed) -- must skip, never warn on nothing.
func TestAdapterSiblingsResult_NoDirs(t *testing.T) {
	result := adapterSiblingsResult(nil)

	if !result.skipped {
		t.Fatalf("expected skipped, got %+v", result)
	}
}

// TestAdapterSiblingsResult_UnreadableDirDoesNotPanic covers a scanned dir
// that doesn't exist (e.g. a stale OX_ADAPTER_PATH entry) -- must degrade
// gracefully, not error out.
func TestAdapterSiblingsResult_UnreadableDirDoesNotPanic(t *testing.T) {
	result := adapterSiblingsResult([]string{"/no/such/directory/at/all"})

	if !result.warning {
		t.Fatalf("expected a warning (nothing found), got %+v", result)
	}
	if result.message != fmt.Sprintf("0/%d present", len(expectedAdapterSiblings)) {
		t.Errorf("message = %q, want %q", result.message, fmt.Sprintf("0/%d present", len(expectedAdapterSiblings)))
	}
}

func touchFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("touchFile(%q): %v", path, err)
	}
}
