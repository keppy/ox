package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sageox/ox/internal/testguard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fixtureTranscript = "testdata/session-v3.jsonl"

func clearOMPPathEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"OMP_PROFILE", "PI_PROFILE", "PI_CONFIG_DIR", "PI_CODING_AGENT_DIR",
		"PI_CODING_AGENT_SESSION_DIR", "XDG_DATA_HOME",
	} {
		t.Setenv(key, "")
	}
}

func TestParseOMPLineUserMessage(t *testing.T) {
	line := []byte(`{"type":"message","timestamp":"2026-08-17T16:55:45.408Z","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}`)
	entries := parseOMPLine(line)
	require.Len(t, entries, 1)
	assert.Equal(t, "user", entries[0].Role)
	assert.Equal(t, "hello", entries[0].Content)
}

func TestParseOMPLineDropsThinkingAndKeepsTools(t *testing.T) {
	line := []byte(`{"type":"message","timestamp":"2026-08-17T16:55:45.408Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"private"},{"type":"text","text":"visible"},{"type":"toolCall","id":"call-1","name":"read","arguments":{"path":"AGENTS.md"}}]}}`)
	entries := parseOMPLine(line)
	require.Len(t, entries, 2)
	assert.Equal(t, "assistant", entries[0].Role)
	assert.Equal(t, "visible", entries[0].Content)
	assert.Equal(t, "tool", entries[1].Role)
	assert.Equal(t, "read", entries[1].ToolName)
	assert.NotContains(t, entries[0].Content, "private")
}

func TestParseOMPLineToolResult(t *testing.T) {
	line := []byte(`{"type":"message","timestamp":"2026-08-17T16:55:45.408Z","message":{"role":"toolResult","toolCallId":"call-1","toolName":"bash","isError":true,"content":[{"type":"text","text":"exit 1"}]}}`)
	entries := parseOMPLine(line)
	require.Len(t, entries, 1)
	assert.Equal(t, "tool", entries[0].Role)
	assert.True(t, entries[0].IsError)
	assert.Equal(t, "call-1", entries[0].CallID)
}

func TestReadOMPFileRealTranscript(t *testing.T) {
	entries, err := readOMPFile(fixtureTranscript)
	require.NoError(t, err)
	require.Len(t, entries, 4)
	assert.Equal(t, []string{"user", "tool", "tool", "assistant"}, []string{
		entries[0].Role, entries[1].Role, entries[2].Role, entries[3].Role,
	})
}

func TestExtractOMPMetadataUsesCurrentModelField(t *testing.T) {
	meta := extractOMPMetadata(fixtureTranscript)
	require.NotNil(t, meta)
	assert.Equal(t, "omp-session-v3", meta.AgentVersion)
	assert.Equal(t, "openai-codex/gpt-5.6-sol", meta.Model)
}

func TestReadOMPFromOffsetPreservesPartialLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	first := `{"type":"message","timestamp":"2026-08-17T16:55:45Z","message":{"role":"user","content":[{"type":"text","text":"first"}]}}` + "\n"
	second := `{"type":"message","timestamp":"2026-08-17T16:55:46Z","message":{"role":"assistant","content":[{"type":"text","text":"second"}]}}`
	require.NoError(t, os.WriteFile(path, []byte(first+second[:len(second)/2]), 0o644))

	entries, offset, err := readOMPFromOffset(path, 0)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, int64(len(first)), offset)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString(second[len(second)/2:] + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	entries, _, err = readOMPFromOffset(path, offset)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "second", entries[0].Content)
}

func TestOMPSessionRootsDefaultAndCustomLocations(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	clearOMPPathEnv(t)

	roots, err := ompSessionRoots()
	require.NoError(t, err)
	require.NotEmpty(t, roots)
	assert.Equal(t, filepath.Join(home, ".omp", "agent", "sessions"), roots[0].path)

	t.Setenv("PI_CODING_AGENT_DIR", "~/custom-agent")
	roots, err = ompSessionRoots()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "custom-agent", "sessions"), roots[0].path)

	t.Setenv("PI_CODING_AGENT_SESSION_DIR", "~/direct-sessions")
	roots, err = ompSessionRoots()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "direct-sessions"), roots[0].path)
	assert.True(t, roots[0].direct)
}

func TestOMPSessionRootsProfilesAndXDG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	clearOMPPathEnv(t)
	t.Setenv("OMP_PROFILE", "work")

	roots, err := ompSessionRoots()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".omp", "profiles", "work", "agent", "sessions"), roots[0].path)

	xdg := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(xdg, "omp"), 0o755))
	t.Setenv("XDG_DATA_HOME", xdg)
	roots, err = ompSessionRoots()
	require.NoError(t, err)
	if runtime.GOOS == "windows" {
		// OMP's own resolver redirects under $XDG_DATA_HOME/omp only on
		// linux/darwin (the process.platform gate in @oh-my-pi/pi-utils/dirs).
		// Honoring the variable on Windows would advertise a session root OMP
		// never writes to, so the override must stay ignored there.
		assert.False(t, hasOMPRoot(roots, filepath.Join(xdg, "omp", "sessions")),
			"XDG_DATA_HOME must be ignored on Windows; OMP gates the XDG redirect to linux/darwin")
		return
	}
	assert.True(t, hasOMPRoot(roots, filepath.Join(xdg, "omp", "sessions")))
}

func TestOMPSessionDirNamesMatchDocumentedHomeEncoding(t *testing.T) {
	// The documented encoding is written with POSIX literals, but
	// /Users/tester is not an absolute path on Windows (no volume), so
	// homedir.Dir() ignores it there by design and the cwd would classify as
	// outside home. Build the same fixture in platform form — on Windows that
	// is C:\Users	ester, the shape an OMP user's home actually has.
	home := testguard.FakeHome("/Users/tester")
	cwd := testguard.FakePath("/Users/tester/projects/ox")
	t.Setenv("HOME", home)

	names := ompSessionDirNames(cwd)
	require.NotEmpty(t, names)
	// Current encoding: the path under home with separators replaced by '-'.
	assert.Equal(t, "-projects-ox", names[0])
	// Legacy absolute encoding: the full path with separators replaced by '-',
	// wrapped in '--'. On Windows the drive letter is part of the name and its
	// ':' is encoded like a separator (OMP's encoder maps '/', '\' and ':'
	// alike), so the Windows form carries the extra 'C--'.
	legacy := "--Users-tester-projects-ox--"
	if runtime.GOOS == "windows" {
		legacy = "--C--Users-tester-projects-ox--"
	}
	assert.Contains(t, names, legacy)
}

func TestFindOMPSessionUsesTimestampPrefixedSessionID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	clearOMPPathEnv(t)
	repo := filepath.Join(home, "projects", "repo")
	require.NoError(t, os.MkdirAll(repo, 0o755))

	root := filepath.Join(home, ".omp", "agent", "sessions")
	dir := filepath.Join(root, ompSessionDirNames(repo)[0])
	require.NoError(t, os.MkdirAll(dir, 0o755))
	id := "01a010a6-0d3d-7000-983a-8d87e5a3d151"
	path := filepath.Join(dir, "2026-08-17T16-55-12-957Z_"+id+".jsonl")
	writeOMPSession(t, path, repo, "agent-marker")

	found, err := findOMPSession(repo, "", "", id)
	require.NoError(t, err)
	assert.Equal(t, path, found)

	found, err = findOMPSession(repo, "agent-marker", "", "")
	require.NoError(t, err)
	assert.Equal(t, path, found)
}

func TestFindOMPSessionRejectsDifferentProjectInDirectStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	clearOMPPathEnv(t)
	direct := filepath.Join(home, "direct")
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", direct)
	require.NoError(t, os.MkdirAll(direct, 0o755))
	writeOMPSession(t, filepath.Join(direct, "2026_wrong.jsonl"), filepath.Join(home, "other"), "marker")

	_, err := findOMPSession(filepath.Join(home, "wanted"), "marker", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no omp sessions found")
}

func hasOMPRoot(roots []ompSessionRoot, want string) bool {
	for _, root := range roots {
		if root.path == want {
			return true
		}
	}
	return false
}

func writeOMPSession(t *testing.T, path, cwd, marker string) {
	t.Helper()
	body := strings.Join([]string{
		`{"type":"title","v":1,"title":"Fixture"}`,
		`{"type":"session","version":3,"id":"fixture","timestamp":"2026-08-17T16:55:12Z","cwd":` + quoteJSON(cwd) + `}`,
		`{"type":"message","timestamp":"2026-08-17T16:55:45Z","message":{"role":"user","content":[{"type":"text","text":` + quoteJSON(marker) + `}]}}`,
	}, "\n") + "\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func quoteJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
