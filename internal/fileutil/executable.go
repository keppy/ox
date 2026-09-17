package fileutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// IsExecutable reports whether a regular file at path would be runnable by
// os/exec. On Unix that means any execute bit is set. Windows has no execute
// bit — every file reports mode 0666 — so there the answer is decided by the
// extension the shell would honor (PATHEXT), defaulting to the classic set.
//
// Callers that gate binary discovery on `fi.Mode()&0111 != 0` silently drop
// every binary on Windows; route them through here instead.
func IsExecutable(fi os.FileInfo, path string) bool {
	if fi == nil || !fi.Mode().IsRegular() {
		return false
	}
	if runtime.GOOS != "windows" {
		return fi.Mode().Perm()&0o111 != 0
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return false
	}
	for _, e := range windowsExecExts() {
		if ext == e {
			return true
		}
	}
	return false
}

// windowsExecExts returns the lower-cased executable extensions from PATHEXT,
// falling back to the Windows default list when the variable is unset.
func windowsExecExts() []string {
	pathext := os.Getenv("PATHEXT")
	if pathext == "" {
		pathext = ".COM;.EXE;.BAT;.CMD"
	}
	parts := strings.Split(pathext, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
