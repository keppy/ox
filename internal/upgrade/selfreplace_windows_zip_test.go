package upgrade

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// makeZip builds an in-memory .zip from name->content.
func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// windowsReleaseServer serves a zip asset under the windows naming scheme.
func windowsReleaseServer(t *testing.T, archive []byte) *httptest.Server {
	t.Helper()
	asset := AssetName(testVer, "windows", testArch)
	checksums := fmt.Sprintf("%s  %s\n", sha256hex(archive), asset)
	mux := http.NewServeMux()
	mux.HandleFunc("/v"+testVer+"/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(checksums))
	})
	mux.HandleFunc("/v"+testVer+"/"+asset, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	return httptest.NewServer(mux)
}

func TestAssetName_Windows(t *testing.T) {
	if got := AssetName("0.14.0", "windows", "amd64"); got != "ox_0.14.0_windows_amd64.zip" {
		t.Errorf("AssetName = %q", got)
	}
}

// Windows releases are zips whose binaries carry .exe; the installed set is
// matched by canonical name so ox.exe and ox-adapter-*.exe are upgraded and
// the .exe suffix is preserved on disk.
func TestReplaceRunningBinary_WindowsZip(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ox.exe"), "OLD-ox")
	writeFile(t, filepath.Join(dir, "ox-adapter-hermes.exe"), "OLD-adapter")
	// a .bak stranded by a previous upgrade (the OS could not delete the
	// mapped image at the time) must be swept on the next run.
	writeFile(t, filepath.Join(dir, ".ox-upgrade-123.bak"), "stale")

	archive := makeZip(t, map[string]string{
		"ox.exe":                  "NEW-ox",
		"ox-adapter-hermes.exe":   "NEW-adapter",
		"ox-adapter-gemini.exe":   "NEW-gemini", // not installed => skipped
		"README.md":               "docs",
		"nested/ox-adapter-x.exe": "ignored", // basename lookup, not installed
	})
	srv := windowsReleaseServer(t, archive)
	defer srv.Close()

	cfg := baseConfig(dir, srv.URL)
	cfg.OS = "windows"
	if err := ReplaceRunningBinary(context.Background(), cfg); err != nil {
		t.Fatalf("ReplaceRunningBinary: %v", err)
	}

	if got := readFile(t, filepath.Join(dir, "ox.exe")); got != "NEW-ox" {
		t.Errorf("ox.exe not replaced: got %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "ox-adapter-hermes.exe")); got != "NEW-adapter" {
		t.Errorf("adapter not replaced: got %q", got)
	}
	for _, unexpected := range []string{"ox", "ox-adapter-gemini.exe", "README.md", ".ox-upgrade-123.bak"} {
		if _, err := os.Stat(filepath.Join(dir, unexpected)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s should not exist after upgrade", unexpected)
		}
	}
}

// A tar.gz that happens to carry .exe names is still recognised, so a single
// canonicalisation path serves both archive kinds.
func TestReadArchiveBinaries_CanonicalisesExe(t *testing.T) {
	tarball := makeTarball(t, map[string]string{"ox.exe": "x", "ox-adapter-a.exe": "y", "LICENSE": "z"})
	got, err := readArchiveBinaries(context.Background(), tarball, "linux")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || string(got["ox"]) != "x" || string(got["ox-adapter-a"]) != "y" {
		t.Errorf("unexpected contents: %v", got)
	}
}
