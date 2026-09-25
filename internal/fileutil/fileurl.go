package fileutil

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// FileURL renders a local filesystem path as a `file://` URL that both git and
// url.Parse accept on the running platform.
//
// Concatenating `"file://" + path` is only correct on Windows when the path
// happens to use forward slashes (e.g. TMP=C:/tmp). Windows paths normally use
// backslashes, so the result is `file://C:\Users\...`, which url.Parse rejects
// with `invalid port ":\Users\..." after host` (it reads `C:` as the host and
// the backslash as the start of a bad port). That is why tests built this way
// pass on a dev box configured with forward-slash TMP and fail on a Windows CI
// runner, whose TEMP is `C:\Users\RUNNER~1\AppData\Local\Temp`.
//
// The two platform forms differ, and the difference is load-bearing — Git for
// Windows resolves a file URL by stripping the `file://` prefix and using the
// remainder as a filesystem path (verified against git 2.47.1.windows.2):
//
//   - Windows: `file://C:/Users/...` — two slashes, the drive letter is the URL
//     host. Stripping the prefix yields `C:/Users/...`, a real path.
//   - POSIX: `file:///tmp/repo.git` — three slashes, empty host. Stripping the
//     prefix yields `/tmp/repo.git`.
//
// The RFC 8089 form `file:///C:/Users/...` is rejected by Git for Windows with
// `fatal: '/C:/Users/...' does not appear to be a git repository`, so it must
// not be used there even though it is the "more correct" URL.
//
// Callers that need the path back (rather than the URL) must not trim a fixed
// prefix: on Windows the drive letter lives in the URL host, so the canonical
// inverse is `url.Parse(u).Host + url.Parse(u).Path`.
func FileURL(path string) string {
	slash := filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		return "file://" + slash
	}
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	// url.URL escapes the path (spaces, `#`, `?`) and emits the three-slash
	// form for an empty host.
	return (&url.URL{Scheme: "file", Path: slash}).String()
}
