package fileutil

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFileURL(t *testing.T) {
	posix := []struct {
		name string
		path string
		want string
	}{
		{
			name: "posix absolute path",
			path: filepath.FromSlash("/tmp/repo.git"),
			want: "file:///tmp/repo.git",
		},
		{
			name: "posix space is escaped",
			path: filepath.FromSlash("/tmp/my repo.git"),
			want: "file:///tmp/my%20repo.git",
		},
		{
			name: "posix hash is escaped",
			path: filepath.FromSlash("/tmp/re#po.git"),
			want: "file:///tmp/re%23po.git",
		},
	}
	windows := []struct {
		name string
		path string
		want string
	}{
		{
			name: "windows drive letter path",
			path: filepath.FromSlash("C:/Users/RUNNER~1/AppData/Local/Temp/x/ledger.bare"),
			want: "file://C:/Users/RUNNER~1/AppData/Local/Temp/x/ledger.bare",
		},
		{
			name: "windows path with spaces",
			path: filepath.FromSlash("C:/Users/runneradmin/AppData/Local/Temp/my repo/ledger.bare"),
			want: "file://C:/Users/runneradmin/AppData/Local/Temp/my repo/ledger.bare",
		},
	}

	// Both tables must hold on the running platform: the cases describe the
	// same contract ("a URL git and url.Parse accept"), and the platform only
	// decides which spelling satisfies it. Running the "other" platform's
	// cases through FromSlash keeps the tables honest about separators.
	tests := posix
	if runtime.GOOS == "windows" {
		tests = windows
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FileURL(tt.path)
			if got != tt.want {
				t.Fatalf("FileURL(%q) = %q, want %q", tt.path, got, tt.want)
			}
			assertParsable(t, got)
		})
	}
}

// TestFileURL_BackslashPathIsParsable pins the CI failure this helper exists
// for: a Windows path with backslash separators. Concatenation yields
// `file://C:\Users\...`, which url.Parse rejects; FileURL must produce a URL
// that survives url.Parse and keeps the path recoverable via Host+Path (the
// Windows form carries the drive letter in the host).
func TestFileURL_BackslashPathIsParsable(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("backslash paths only occur on Windows")
	}
	raw := `C:\Users\RUNNER~1\AppData\Local\Temp\TestBlueGreenGC\002\ledger.bare`

	if _, err := url.Parse("file://" + raw); err == nil {
		t.Fatal("url.Parse accepted \"file://\" + a backslash path; the helper's premise no longer holds")
	}

	got := FileURL(raw)
	if err := assertParsable(t, got); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(got)
	recovered := filepath.FromSlash(u.Host + u.Path)
	if recovered != raw {
		t.Fatalf("round trip = %q, want %q", recovered, raw)
	}
}

// assertParsable requires the URL to parse with a file scheme, and (on POSIX,
// where the form is the RFC one) an empty host. Git for Windows needs the
// opposite: a two-slash URL whose host is the drive letter.
func assertParsable(t *testing.T, rawURL string) error {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if u.Scheme != "file" {
		return errScheme(rawURL, u.Scheme)
	}
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(u.Host, ":") {
			return errHost(rawURL, u.Host)
		}
		return nil
	}
	if u.Host != "" {
		return errHost(rawURL, u.Host)
	}
	return nil
}

type urlFormError struct{ url, got, wantWhat string }

func (e urlFormError) Error() string {
	return e.url + ": got " + e.got + ", want " + e.wantWhat
}

func errScheme(u, scheme string) error { return urlFormError{u, scheme, "the file scheme"} }
func errHost(u, host string) error {
	return urlFormError{u, "host " + host, "the platform's drive-letter or empty host"}
}
