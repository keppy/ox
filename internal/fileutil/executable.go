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

// StripExecExt returns name without a trailing Windows executable extension
// (".exe", ".cmd", ...). On Unix it returns name unchanged: a Unix binary
// called "tool.exe" is legitimately named that. Use it wherever a binary's
// file name is turned back into a logical name (ox-adapter-<name>).
func StripExecExt(name string) string {
	if runtime.GOOS != "windows" {
		return name
	}
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range windowsExecExts() {
		if ext == e {
			return strings.TrimSuffix(name, name[len(name)-len(ext):])
		}
	}
	return name
}

// FindExecutable looks for an executable called base inside dir and returns
// its full path. On Unix that is dir/base with an execute bit. On Windows the
// same logical name may be spelled base.exe, base.cmd, ... — every PATHEXT
// extension is tried in order, mirroring what CreateProcess and cmd.exe do
// for a bare command name — so callers never have to know which one a
// particular install produced.
func FindExecutable(dir, base string) (string, bool) {
	try := func(p string) (string, bool) {
		fi, err := os.Stat(p)
		if err != nil || !IsExecutable(fi, p) {
			return "", false
		}
		return p, true
	}
	if p, ok := try(filepath.Join(dir, base)); ok {
		return p, true
	}
	if runtime.GOOS != "windows" {
		return "", false
	}
	for _, ext := range windowsExecExts() {
		if p, ok := try(filepath.Join(dir, base+ext)); ok {
			return p, true
		}
	}
	return "", false
}
