package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/sageox/ox/pkg/adapterprotocol"
	"github.com/sageox/ox/pkg/adaptertest"
)

// fixtureSchema is the subset of Hermes's state.db DDL this adapter reads,
// captured from a real %LOCALAPPDATA%\hermes\state.db (Hermes Agent v0.21.3,
// 2026-09-17). Columns the adapter never touches are omitted.
const fixtureSchema = `
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    model TEXT,
    started_at REAL NOT NULL,
    ended_at REAL,
    message_count INTEGER DEFAULT 0,
    cwd TEXT,
    git_repo_root TEXT
);
CREATE TABLE messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(id),
    role TEXT NOT NULL,
    content TEXT,
    tool_call_id TEXT,
    tool_calls TEXT,
    tool_name TEXT,
    timestamp REAL NOT NULL,
    active INTEGER NOT NULL DEFAULT 1,
    compacted INTEGER NOT NULL DEFAULT 0
);`

// Row shapes copied from real rows in that database; conversation text
// synthesized. Row 5 is a compaction archive (active=0) and must be skipped.
// Row 8 is a failed tool result ({"error": ...}) and must carry is_error.
const fixtureSeed = `
INSERT INTO sessions VALUES ('sess_fx', 'desktop', 'claude-fable-5-1', 1789673709.7, NULL, 8, 'C:\Users\dev\git\proj\sub', 'C:\Users\dev\git\proj');
INSERT INTO sessions VALUES ('sess_other', 'cli', 'gpt-x', 1789673000.0, NULL, 0, '/home/dev/other', '/home/dev/other');
INSERT INTO messages (session_id, role, content, tool_call_id, tool_calls, tool_name, timestamp, active) VALUES
 ('sess_fx', 'system', 'You are Hermes.', NULL, NULL, NULL, 1789673710.0, 1),
 ('sess_fx', 'user', 'improve the docs', NULL, NULL, NULL, 1789673711.1, 1),
 ('sess_fx', 'assistant', '', NULL, '[{"id":"toolu_1","type":"function","function":{"name":"read_file","arguments":"{\"path\": \"README.md\"}"}},{"id":"toolu_2","type":"function","function":{"name":"search_files","arguments":"{\"pattern\": \"kappa\"}"}}]', NULL, 1789673725.5, 1),
 ('sess_fx', 'tool', '{"content": "1|# proj"}', 'toolu_1', NULL, 'read_file', 1789673727.0, 1),
 ('sess_fx', 'assistant', 'archived generation text', NULL, NULL, NULL, 1789673727.5, 0),
 ('sess_fx', 'tool', '{"total_count": 3}', 'toolu_2', NULL, 'search_files', 1789673727.1, 1),
 ('sess_fx', 'assistant', 'Let me check the code.', NULL, '[{"id":"toolu_3","type":"function","function":{"name":"terminal","arguments":"{\"command\": \"go test\"}"}}]', NULL, 1789673734.4, 1),
 ('sess_fx', 'tool', '{"error": "exit status 1"}', 'toolu_3', NULL, 'terminal', 1789673735.0, 1),
 ('sess_fx', 'assistant', 'Done.', NULL, NULL, NULL, 1789673740.0, 1);
`

func newFixtureDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fixtureSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fixtureSeed); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	ro, err := openDBAt(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ro.Close() })
	return ro, dir
}

func TestConformance_RealSchemaFixture(t *testing.T) {
	db, _ := newFixtureDB(t)
	adaptertest.Run(t, adaptertest.Suite{
		Adapter:    "hermes",
		Provenance: "Hermes Agent v0.21.3 state.db DDL (sqlite3 .schema, captured 2026-09-17) + tool_calls/tool row shapes copied from real rows, conversation text synthesized",
		ReadAll: func() ([]adapterprotocol.RawEntry, error) {
			e, _, err := readMessages(db, "sess_fx", 0)
			return e, err
		},
		ReadFrom: func(afterID int64) ([]adapterprotocol.RawEntry, int64, error) {
			return readMessages(db, "sess_fx", afterID)
		},
		EndOffset: func() (int64, error) { return maxMessageID(db, "sess_fx") },
		ResumePoints: func() ([]int64, error) {
			return []int64{2, 4, 6}, nil
		},
		Want: adaptertest.Want{
			MinEntries:     9,
			UserTurns:      1,
			AssistantTurns: 2,
			ToolCalls:      3,
			ToolResults:    3,
			PairedResults:  3,
			ErroredResults: 1,
		},
	})
}

func TestReadMessages_SkipsSystemAndArchived(t *testing.T) {
	db, _ := newFixtureDB(t)
	entries, last, skipped, err := readMessagesWithStats(db, "sess_fx", 0)
	if err != nil {
		t.Fatal(err)
	}
	if last != 9 {
		t.Errorf("watermark = %d, want 9 (highest rowid read, including skipped rows)", last)
	}
	if skipped != 2 {
		t.Errorf("skipped = %d, want 2 (system prompt + compaction archive)", skipped)
	}
	for _, e := range entries {
		if strings.Contains(e.Content, "archived generation") || strings.Contains(e.Content, "You are Hermes") {
			t.Errorf("leaked a row that must never be recorded: %q", e.Content)
		}
	}
}

func TestResolveSessionID_MatchesRepoRootAndSubdir(t *testing.T) {
	db, _ := newFixtureDB(t)
	for _, root := range []string{`C:\Users\dev\git\proj`, `C:/Users/dev/git/proj`} {
		id, err := resolveSessionID(db, "", root, "")
		if err != nil {
			t.Fatalf("repoRoot %q: %v", root, err)
		}
		if id != "sess_fx" {
			t.Errorf("repoRoot %q: got %q, want sess_fx", root, id)
		}
	}
	if _, err := resolveSessionID(db, "", `C:\Users\dev\git\projX`, ""); err == nil {
		t.Error("LIKE prefix must not match a sibling repo sharing the prefix")
	}
	id, err := resolveSessionID(db, "sess_other", "", "")
	if err != nil || id != "sess_other" {
		t.Errorf("explicit session id: got %q, %v", id, err)
	}
}

func TestReadMetadata(t *testing.T) {
	db, _ := newFixtureDB(t)
	meta, err := readMetadata(db, "sess_fx")
	if err != nil || meta == nil || meta.Model != "claude-fable-5-1" {
		t.Fatalf("meta = %+v, err = %v", meta, err)
	}
}

func TestToolResultIsError(t *testing.T) {
	cases := map[string]bool{
		`{"error": "boom"}`:              true,
		`{"success": false, "x": 1}`:     true,
		`{"error": ""}`:                  false,
		`{"content": "ok"}`:              false,
		`plain text`:                     false,
		`{"error": {"code": 1}}`:         true,
		`{"success": true, "error": ""}`: false,
	}
	for in, want := range cases {
		if got := toolResultIsError(in); got != want {
			t.Errorf("toolResultIsError(%s) = %v, want %v", in, got, want)
		}
	}
}

func TestHandleInfo_CapabilitiesPinned(t *testing.T) {
	info, err := handleInfo()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		adapterprotocol.CapSessionReader, adapterprotocol.CapHookInstaller,
		adapterprotocol.CapIncrementalReader, adapterprotocol.CapSessionImporter,
		adapterprotocol.CapCapturePrior, adapterprotocol.CapServeMode,
	}
	got := append([]string(nil), info.Capabilities...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("capabilities = %v, want %v", got, want)
	}
	for _, c := range info.Capabilities {
		if c == adapterprotocol.CapFileWatcher {
			t.Error("hermes must not declare file_watcher: session handles are virtual")
		}
	}
	if info.Name != "hermes" || !info.ServeMode {
		t.Errorf("identity: %+v", info)
	}
}

// --- hooks ---

func withHermesHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HERMES_HOME", home)
	return home
}

func readYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	root, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := root.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestInstallHooks_FreshConfig(t *testing.T) {
	home := withHermesHome(t)
	resp, err := handleInstallHooks(adapterprotocol.HookParams{Scope: "user"})
	if err != nil || !resp.Installed {
		t.Fatalf("install: %+v %v", resp, err)
	}
	cfg := readYAML(t, filepath.Join(home, "config.yaml"))
	hooks, _ := cfg["hooks"].(map[string]any)
	if len(hooks) != len(hookEvents) {
		t.Fatalf("hooks = %v, want %d events", hooks, len(hookEvents))
	}
	first := hooks["pre_llm_call"].([]any)[0].(map[string]any)
	if first["command"] != "ox agent hook pre_llm_call --agent hermes" {
		t.Errorf("command = %v", first["command"])
	}
	if first["timeout"] != 15 {
		t.Errorf("timeout = %v (%T), want 15", first["timeout"], first["timeout"])
	}
	check, _ := handleCheckHooks(adapterprotocol.HookParams{Scope: "user"})
	if !check.Installed {
		t.Error("check-hooks should report installed")
	}
}

func TestInstallHooks_PreservesUserConfigAndHooks(t *testing.T) {
	home := withHermesHome(t)
	original := `# my config
model: anthropic/claude-x   # keep me
terminal:
  backend: local
hooks:
  pre_tool_call:
    - matcher: terminal
      command: ~/.hermes/agent-hooks/block-rm.sh
  pre_llm_call:
    - command: ~/.hermes/agent-hooks/git-status.sh
`
	path := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := handleInstallHooks(adapterprotocol.HookParams{Scope: "user"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	for _, want := range []string{"# my config", "# keep me", "backend: local", "block-rm.sh", "git-status.sh"} {
		if !strings.Contains(text, want) {
			t.Errorf("install dropped %q from config:\n%s", want, text)
		}
	}
	cfg := readYAML(t, path)
	pre := cfg["hooks"].(map[string]any)["pre_llm_call"].([]any)
	if len(pre) != 2 {
		t.Errorf("pre_llm_call should keep the user's hook and add ours, got %d entries", len(pre))
	}

	// idempotent
	if _, err := handleInstallHooks(adapterprotocol.HookParams{Scope: "user"}); err != nil {
		t.Fatal(err)
	}
	cfg = readYAML(t, path)
	if n := len(cfg["hooks"].(map[string]any)["pre_llm_call"].([]any)); n != 2 {
		t.Errorf("second install duplicated entries: %d", n)
	}

	// uninstall removes only ours
	resp, err := handleUninstallHooks(adapterprotocol.HookParams{Scope: "user"})
	if err != nil || !resp.Uninstalled {
		t.Fatalf("uninstall: %+v %v", resp, err)
	}
	data, _ = os.ReadFile(path)
	text = string(data)
	if strings.Contains(text, oxHookMarker) {
		t.Errorf("uninstall left ox hooks behind:\n%s", text)
	}
	for _, want := range []string{"block-rm.sh", "git-status.sh", "# keep me"} {
		if !strings.Contains(text, want) {
			t.Errorf("uninstall removed the user's own %q:\n%s", want, text)
		}
	}
	if _, ok := readYAML(t, path)["hooks"].(map[string]any)["on_session_start"]; ok {
		t.Error("uninstall should drop event keys that held only ox entries")
	}
}

func TestUninstallHooks_NothingInstalled(t *testing.T) {
	withHermesHome(t)
	resp, err := handleUninstallHooks(adapterprotocol.HookParams{Scope: "user"})
	if err != nil || !resp.Uninstalled {
		t.Fatalf("%+v %v", resp, err)
	}
}

func TestHermesHome_Resolution(t *testing.T) {
	t.Setenv("HERMES_HOME", "")
	t.Setenv("HERMES_HOME", `C:\x\profiles\coder`)
	if got := hermesHome(); got != `C:\x\profiles\coder` {
		t.Errorf("HERMES_HOME should win, got %q", got)
	}
}

func TestHookCommand_IsShellFree(t *testing.T) {
	// Hermes splits the command itself with shell=False: no `if`, no `&&`,
	// no env-var prefix can appear here or the hook silently never runs.
	for _, ev := range hookEvents {
		c := hookCommand(ev)
		for _, bad := range []string{"if ", "&&", "||", ";", "=", "$"} {
			if strings.Contains(c, bad) {
				t.Errorf("hookCommand(%s) = %q contains shell syntax %q", ev, c, bad)
			}
		}
		var payload map[string]any
		_ = json.Unmarshal([]byte(`{}`), &payload)
	}
}
