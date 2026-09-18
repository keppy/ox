package gitutil

import "runtime"

// Git for Windows refuses any path longer than MAX_PATH (260 characters)
// unless core.longpaths is enabled — it fails with "Filename too long"
// rather than truncating, so a deep checkout simply cannot be written.
//
// ox's managed trees are deep by construction:
//
//	<home>\.local\share\sageox\<endpoint>\ledgers\<repo-id>\data\murmurs\2026-09-17\16\<id>.json
//
// and .sageox/cache/... sits below that. A long user name, a long endpoint
// host, or a project checked out several directories deep is enough to cross
// the limit, and the failure lands mid-clone — leaving a half-written
// checkout that the next pass has to repair. Every other Windows git tool
// (GitHub Desktop, VS, VS Code) sets this on the repositories it manages;
// ox manages repositories too, so it should set it on its own.
//
// The key is set on Windows only: on Unix it is inert, and writing it into a
// shared checkout would be noise.

// LongPathsArgs returns the `-c` flags that let a git invocation handle paths
// beyond MAX_PATH. Prepend to a clone's argument list — config cannot be read
// before the repository exists, so the flag has to ride on the command line
// for the clone that creates it.
//
// Returns nil off Windows, so callers can append unconditionally.
func LongPathsArgs() []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	return []string{"-c", "core.longpaths=true"}
}
