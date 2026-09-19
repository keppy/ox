package fileutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestIsExecutable(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, mode os.FileMode) (os.FileInfo, string) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), mode); err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		return fi, p
	}

	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
		for name, want := range map[string]bool{
			"tool.exe": true, "tool.CMD": true, "tool.bat": true,
			"tool": false, "tool.sh": false, "tool.txt": false,
		} {
			fi, p := write(name, 0o644)
			if got := IsExecutable(fi, p); got != want {
				t.Errorf("IsExecutable(%q) = %v, want %v", name, got, want)
			}
		}
		return
	}

	fi, p := write("exec", 0o755)
	if !IsExecutable(fi, p) {
		t.Error("0755 file should be executable")
	}
	fi, p = write("noexec", 0o644)
	if IsExecutable(fi, p) {
		t.Error("0644 file should not be executable")
	}
}

func TestIsExecutable_RejectsDirsAndNil(t *testing.T) {
	dir := t.TempDir()
	fi, _ := os.Stat(dir)
	if IsExecutable(fi, dir) {
		t.Error("a directory is not an executable")
	}
	if IsExecutable(nil, dir) {
		t.Error("nil FileInfo is not an executable")
	}
}

func TestStripExecExt(t *testing.T) {
	if runtime.GOOS != "windows" {
		if got := StripExecExt("ox-adapter-x.exe"); got != "ox-adapter-x.exe" {
			t.Errorf("unix must not strip: got %q", got)
		}
		return
	}
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	for in, want := range map[string]string{
		"ox-adapter-x.exe": "ox-adapter-x",
		"ox-adapter-x.CMD": "ox-adapter-x",
		"ox-adapter-x":     "ox-adapter-x",
		"ox-adapter-x.sh":  "ox-adapter-x.sh",
	} {
		if got := StripExecExt(in); got != want {
			t.Errorf("StripExecExt(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFindExecutable(t *testing.T) {
	dir := t.TempDir()
	if _, ok := FindExecutable(dir, "missing"); ok {
		t.Fatal("found a binary that does not exist")
	}
	name := "ox-adapter-probe"
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
		name += ".cmd"
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := FindExecutable(dir, "ox-adapter-probe")
	if !ok || got != p {
		t.Fatalf("FindExecutable = %q, %v; want %q, true", got, ok, p)
	}
}
