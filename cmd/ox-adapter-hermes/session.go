// session.go handles Hermes session reading from state.db.
//
// Hermes stores sessions in $HERMES_HOME/state.db with two tables that matter:
//
//	sessions(id TEXT PK, source TEXT, cwd TEXT, git_repo_root TEXT, model TEXT,
//	         started_at REAL, ended_at REAL, message_count, ...)
//	messages(id INTEGER PK AUTOINCREMENT, session_id, role TEXT, content TEXT,
//	         tool_call_id TEXT, tool_calls TEXT (JSON), tool_name TEXT,
//	         timestamp REAL, active INTEGER, compacted INTEGER, ...)
//
// Rows are OpenAI chat-completion shaped: an assistant row may carry a
// tool_calls JSON array; a tool row carries the result text in content with
// tool_call_id pointing at the request. Reasoning lives in separate columns
// (reasoning, reasoning_content) that this adapter never reads: reasoning
// content is never recorded for any agent.
//
// Compaction: Hermes rewrites a session in place, archiving old rows with
// active=0 and inserting a summary. Only active=1 rows are read, so the
// Ledger receives the live conversation; archived generations are skipped.
// The incremental offset is MAX(messages.id) — a real AUTOINCREMENT
// watermark, the same design as the Goose adapter.
package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite; Hermes stores sessions in SQLite

	"github.com/sageox/ox/pkg/adapterprotocol"
	"github.com/sageox/ox/pkg/adapterruntime"
)

// sessionFilePrefix marks the virtual session handle. Hermes sessions are rows,
// not files, so there is no real path to hand back to the protocol.
const sessionFilePrefix = "hermes:"

// hermesToolCall is one element of messages.tool_calls (OpenAI function-call
// shape). Only the fields ox consumes are decoded.
type hermesToolCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

// --- database helpers ---

func openDB() (*sql.DB, error) {
	return openDBAt(hermesDBPath())
}

func openDBAt(dbPath string) (*sql.DB, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("hermes home directory not found")
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("hermes state.db not found at %s", dbPath)
	}
	// Read-only; Hermes runs state.db in WAL mode so a live session is never
	// blocked by our reads. filepath.ToSlash: the sqlite URI form wants
	// forward slashes even on Windows.
	dsn := "file:" + filepath.ToSlash(dbPath) + "?mode=ro&_pragma=busy_timeout(3000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open hermes state.db: %w", err)
	}
	return db, nil
}

// --- session discovery ---

func handleFindSession(p adapterprotocol.FindSessionParams) (*adapterprotocol.FindSessionResult, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	sessionID, err := resolveSessionID(db, p.AgentSessionID, p.RepoRoot, p.Since)
	if err != nil {
		return nil, err
	}
	offset, err := maxMessageID(db, sessionID)
	if err != nil {
		return nil, err
	}
	return &adapterprotocol.FindSessionResult{
		SessionFile: sessionFilePrefix + sessionID,
		Offset:      offset,
	}, nil
}

// resolveSessionID finds the Hermes session to record. A session ID from the
// hook payload always wins; otherwise fall back to the most recently started
// session whose git_repo_root or cwd is the repo (or a directory inside it).
func resolveSessionID(db *sql.DB, agentSessionID, repoRoot, since string) (string, error) {
	if agentSessionID != "" {
		var id string
		err := db.QueryRow("SELECT id FROM sessions WHERE id = ? LIMIT 1", agentSessionID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("session %s not found", agentSessionID)
		}
		if err != nil {
			return "", fmt.Errorf("query session: %w", err)
		}
		return id, nil
	}

	where := []string{"1=1"}
	args := []any{}

	if repoRoot != "" {
		// Hermes stores whatever spelling the OS gave it; on Windows that is
		// backslashes. Match the native form and the slash form so a repo
		// root passed in either spelling finds the session. The LIKE prefix
		// is escaped: `_` and `%` are wildcards and ordinary path characters.
		native := filepath.FromSlash(repoRoot)
		slashed := filepath.ToSlash(repoRoot)
		clause := `(git_repo_root = ? OR git_repo_root = ? OR cwd = ? OR cwd = ? OR cwd LIKE ? ESCAPE '\' OR cwd LIKE ? ESCAPE '\')`
		where = append(where, clause)
		args = append(args, native, slashed, native, slashed,
			escapeLike(native)+`\`+"%", escapeLike(slashed)+"/%")
	}

	if since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err != nil {
			return "", fmt.Errorf("invalid since %q: %w", since, err)
		}
		where = append(where, "started_at >= ?")
		args = append(args, float64(t.UnixNano())/1e9)
	}

	query := "SELECT id FROM sessions WHERE " + strings.Join(where, " AND ") +
		" ORDER BY started_at DESC LIMIT 1"

	var id string
	err := db.QueryRow(query, args...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		if repoRoot != "" {
			return "", fmt.Errorf("no hermes sessions found for %s", repoRoot)
		}
		return "", fmt.Errorf("no hermes sessions found")
	}
	if err != nil {
		return "", fmt.Errorf("query sessions: %w", err)
	}
	return id, nil
}

// escapeLike escapes the SQL LIKE wildcards so a literal path can be used as a
// prefix pattern. Backslash is the ESCAPE character, so it is escaped first.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// maxMessageID returns the highest message rowid for the session, which is the
// incremental-read watermark. A session with no messages yet yields 0.
func maxMessageID(db *sql.DB, sessionID string) (int64, error) {
	var maxID sql.NullInt64
	if err := db.QueryRow("SELECT MAX(id) FROM messages WHERE session_id = ?", sessionID).Scan(&maxID); err != nil {
		return 0, fmt.Errorf("max message id for session %s: %w", sessionID, err)
	}
	if !maxID.Valid {
		return 0, nil
	}
	return maxID.Int64, nil
}

// --- reads ---

func handleRead(p adapterprotocol.ReadParams) (*adapterprotocol.ReadResult, error) {
	sessionID := extractSessionID(p.SessionFile)
	if sessionID == "" {
		return nil, fmt.Errorf("invalid session file: %s (expected %s<session-id>)", p.SessionFile, sessionFilePrefix)
	}
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	entries, _, skipped, err := readMessagesWithStats(db, sessionID, 0)
	if err != nil {
		return nil, err
	}
	meta, _ := readMetadata(db, sessionID)
	return &adapterprotocol.ReadResult{Entries: entries, Metadata: meta, Skipped: skipped}, nil
}

func handleReadMetadata(p adapterprotocol.ReadParams) (*adapterprotocol.ReadMetadataResult, error) {
	sessionID := extractSessionID(p.SessionFile)
	if sessionID == "" {
		return &adapterprotocol.ReadMetadataResult{}, nil
	}
	db, err := openDB()
	if err != nil {
		return &adapterprotocol.ReadMetadataResult{}, nil
	}
	defer func() { _ = db.Close() }()

	meta, _ := readMetadata(db, sessionID)
	if meta == nil {
		return &adapterprotocol.ReadMetadataResult{}, nil
	}
	return &adapterprotocol.ReadMetadataResult{Model: meta.Model}, nil
}

// handleReadFromOffset reads messages whose rowid is strictly greater than the
// caller's watermark.
func handleReadFromOffset(p adapterprotocol.ReadFromOffsetParams) (*adapterprotocol.ReadFromOffsetResult, error) {
	sessionID := extractSessionID(p.SessionFile)
	if sessionID == "" {
		return nil, fmt.Errorf("invalid session file: %s", p.SessionFile)
	}
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	// The watermark is the highest rowid actually read, NOT a fresh MAX(id):
	// Hermes writes concurrently, and a row inserted between two queries
	// would otherwise be skipped past and lost from the Ledger forever.
	entries, newOffset, err := readMessages(db, sessionID, p.Offset)
	if err != nil {
		return nil, err
	}
	return &adapterprotocol.ReadFromOffsetResult{Entries: entries, NewOffset: newOffset}, nil
}

func handleCapturePrior(p adapterprotocol.CapturePriorParams) (*adapterprotocol.CapturePriorResult, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	sessionID, err := resolveSessionID(db, p.SessionID, p.RepoRoot, "")
	if err != nil {
		return nil, err
	}
	entries, _, err := readMessages(db, sessionID, 0)
	if err != nil {
		return nil, err
	}
	meta, _ := readMetadata(db, sessionID)
	return &adapterprotocol.CapturePriorResult{
		Entries:   entries,
		Metadata:  meta,
		AgentType: adapterName,
		SessionID: sessionID,
	}, nil
}

// --- parsing ---

func readMessages(db *sql.DB, sessionID string, afterID int64) ([]adapterprotocol.RawEntry, int64, error) {
	entries, lastID, _, err := readMessagesWithStats(db, sessionID, afterID)
	return entries, lastID, err
}

// readMessagesWithStats reads active rows with rowid > afterID, oldest first,
// and returns the highest rowid it actually read plus a count of rows that
// were understood and deliberately not emitted.
func readMessagesWithStats(db *sql.DB, sessionID string, afterID int64) ([]adapterprotocol.RawEntry, int64, int, error) {
	rows, err := db.Query(
		`SELECT id, role, COALESCE(content, ''), COALESCE(tool_call_id, ''), COALESCE(tool_calls, ''),
		        COALESCE(tool_name, ''), timestamp, active
		   FROM messages WHERE session_id = ? AND id > ? ORDER BY id ASC`,
		sessionID, afterID,
	)
	if err != nil {
		return nil, afterID, 0, fmt.Errorf("query messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []adapterprotocol.RawEntry
	lastID := afterID
	skipped := 0

	for rows.Next() {
		var rowID int64
		var role, content, toolCallID, toolCalls, toolName string
		var tsFloat float64
		var active int

		if err := rows.Scan(&rowID, &role, &content, &toolCallID, &toolCalls, &toolName, &tsFloat, &active); err != nil {
			return nil, afterID, 0, fmt.Errorf("scan message row for session %s: %w", sessionID, err)
		}
		lastID = rowID

		if active == 0 {
			// compaction archive — the live generation carries the same
			// protected turns, so emitting these would duplicate them
			skipped++
			continue
		}

		ts := unixFloatToTime(tsFloat)
		parsed, dropped := parseRow(role, content, toolCallID, toolCalls, toolName, ts)
		entries = append(entries, parsed...)
		skipped += dropped
	}
	if err := rows.Err(); err != nil {
		return nil, afterID, 0, err
	}
	return entries, lastID, skipped, nil
}

// parseRow converts one messages row into ox entries. An assistant row can
// yield text plus several tool calls.
func parseRow(role, content, toolCallID, toolCalls, toolName string, ts time.Time) ([]adapterprotocol.RawEntry, int) {
	var entries []adapterprotocol.RawEntry
	skipped := 0

	switch role {
	case "user":
		if content == "" {
			return nil, 1
		}
		entries = append(entries, adapterruntime.UserEntry(ts, content))

	case "assistant":
		if content != "" {
			entries = append(entries, adapterruntime.AssistantEntry(ts, content))
		}
		if toolCalls != "" {
			var calls []hermesToolCall
			if err := json.Unmarshal([]byte(toolCalls), &calls); err != nil {
				skipped++
			} else {
				for _, c := range calls {
					if c.Function.Name == "" {
						skipped++
						continue
					}
					entries = append(entries, adapterruntime.ToolUseWithID(ts, c.Function.Name, toolArgs(c.Function.Arguments), c.ID))
				}
			}
		}
		if content == "" && toolCalls == "" {
			skipped++
		}

	case "tool":
		if toolCallID == "" {
			// uncorrelatable to its request; nothing useful to record
			return nil, 1
		}
		entries = append(entries, adapterruntime.ToolResultWithID(ts, content, toolResultIsError(content), toolCallID))

	case "system":
		// The system prompt is Hermes's own, not the coworker's conversation,
		// and can embed the user's SOUL.md / memory. Never recorded.
		skipped++

	default:
		skipped++
	}
	_ = toolName
	return entries, skipped
}

// toolArgs returns the arguments as a JSON string. Hermes stores them as an
// OpenAI-style JSON string ("{\"path\": ...}") but a raw object is tolerated.
func toolArgs(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

// toolResultIsError inspects a tool result payload for the error shapes
// Hermes's built-in tools emit: a JSON object with a non-empty "error" key,
// or a "success": false flag.
func toolResultIsError(content string) bool {
	var probe struct {
		Error   any   `json:"error"`
		Success *bool `json:"success"`
	}
	if json.Unmarshal([]byte(content), &probe) != nil {
		return false
	}
	if probe.Success != nil && !*probe.Success {
		return true
	}
	switch e := probe.Error.(type) {
	case nil:
		return false
	case string:
		return e != ""
	default:
		return true
	}
}

// readMetadata extracts the model from the session row.
func readMetadata(db *sql.DB, sessionID string) (*adapterprotocol.SessionMetadata, error) {
	var model sql.NullString
	err := db.QueryRow("SELECT model FROM sessions WHERE id = ? LIMIT 1", sessionID).Scan(&model)
	if err != nil {
		return nil, err
	}
	if !model.Valid || model.String == "" {
		return nil, nil
	}
	return &adapterprotocol.SessionMetadata{Model: model.String}, nil
}

// unixFloatToTime converts Hermes's REAL seconds-since-epoch to a UTC time.
func unixFloatToTime(f float64) time.Time {
	sec, frac := math.Modf(f)
	return time.Unix(int64(sec), int64(frac*1e9)).UTC()
}

// extractSessionID parses the session ID out of the virtual handle
// "hermes:<session-id>". Returns empty for anything else.
func extractSessionID(sessionFile string) string {
	if !strings.HasPrefix(sessionFile, sessionFilePrefix) {
		return ""
	}
	return strings.TrimPrefix(sessionFile, sessionFilePrefix)
}
