package homedir

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDir_HonorsAbsoluteHOME(t *testing.T) {
	want := t.TempDir()
	t.Setenv("HOME", want)
	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if got != want {
		t.Fatalf("Dir() = %q, want HOME %q", got, want)
	}
}

func TestDir_IgnoresRelativeOrForeignHOME(t *testing.T) {
	// A Git-Bash / MSYS style path is not absolute on Windows and a bare
	// relative path is not absolute anywhere; both must fall through.
	cases := []string{"relative/home", "./x"}
	if runtime.GOOS == "windows" {
		cases = append(cases, "/c/Users/someone")
	}
	fallback, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no OS home available: %v", err)
	}
	for _, h := range cases {
		t.Setenv("HOME", h)
		got, err := Dir()
		if err != nil {
			t.Fatalf("HOME=%q: %v", h, err)
		}
		if got != fallback {
			t.Errorf("HOME=%q: Dir() = %q, want OS fallback %q", h, got, fallback)
		}
	}
}

func TestDir_EmptyHOMEFallsBack(t *testing.T) {
	t.Setenv("HOME", "")
	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("Dir() = %q, want an absolute OS home", got)
	}
}
