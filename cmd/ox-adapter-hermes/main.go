// ox-adapter-hermes is the external adapter binary for Hermes Agent sessions.
//
// Hermes Agent (https://github.com/NousResearch/hermes-agent) stores every
// session — CLI, TUI, desktop app, and messaging gateway — in one SQLite
// database, $HERMES_HOME/state.db, with a `sessions` table (id, source, cwd,
// git_repo_root, ...) and a `messages` table (OpenAI-style role/content rows
// plus tool_calls JSON). Session reading queries that database directly
// using modernc.org/sqlite (pure Go, no CGo), exactly like the Goose adapter.
//
// Hooks are Hermes "shell hooks": a `hooks:` block in the profile's
// config.yaml that runs a subprocess per lifecycle event with a JSON payload
// on stdin (Claude Code compatible shape). Hermes has no per-project hook
// file, so hooks are always installed at user scope; project scope
// additionally relies on the AGENTS.md prime marker that `ox init` writes,
// which Hermes loads natively. See hooks.go.
//
// HERMES_HOME resolution follows Hermes itself: $HERMES_HOME if set, else
// %LOCALAPPDATA%\hermes on Windows and ~/.hermes elsewhere. Profiles live at
// <root>/profiles/<name>/ with the same layout.
package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/sageox/ox/internal/homedir"
	"github.com/sageox/ox/pkg/adapterprotocol"
	"github.com/sageox/ox/pkg/adapterruntime"
)

const (
	adapterName    = "hermes"
	adapterDisplay = "Hermes Agent"
	adapterVersion = "0.1.0"
)

func main() {
	adapterruntime.Run(adapterruntime.Config{
		Info:           handleInfo,
		Detect:         handleDetect,
		FindSession:    handleFindSession,
		Read:           handleRead,
		ReadMetadata:   handleReadMetadata,
		ReadFromOffset: handleReadFromOffset,
		InstallHooks:   handleInstallHooks,
		CheckHooks:     handleCheckHooks,
		UninstallHooks: handleUninstallHooks,
		Diagnose:       handleDiagnose,
		ImportSession:  handleImportSession,
		CapturePrior:   handleCapturePrior,
		Serve:          handleServe,
	})
}

func handleInfo() (*adapterprotocol.InfoResponse, error) {
	return &adapterprotocol.InfoResponse{
		ProtocolVersion: adapterprotocol.ProtocolVersion,
		Name:            adapterName,
		DisplayName:     adapterDisplay,
		Version:         adapterVersion,
		Type:            adapterprotocol.TypeSession,
		Capabilities: []string{
			adapterprotocol.CapSessionReader,
			adapterprotocol.CapHookInstaller,
			adapterprotocol.CapIncrementalReader,
			adapterprotocol.CapSessionImporter,
			adapterprotocol.CapCapturePrior,
			adapterprotocol.CapServeMode,
		},
		// No CapFileWatcher: the session handle is virtual ("hermes:<id>"), so
		// there is no path for fsnotify to watch. Recording is hook-driven.
		HookEnvValues: []string{"hermes"},
		// HERMES_HOME selects the profile (default vs. profiles/<name>);
		// without it the adapter would read and write the default profile
		// from inside a hook fired by another one. ox strips every
		// non-allowlisted variable from adapter subprocesses, so it must be
		// declared here.
		RequiredEnv: []string{"HERMES_HOME"},
		ServeMode:   true,
	}, nil
}

func handleDetect() (*adapterprotocol.DetectResponse, error) {
	if os.Getenv("AGENT_ENV") == "hermes" {
		return &adapterprotocol.DetectResponse{Detected: true, Reason: "AGENT_ENV=hermes"}, nil
	}
	// Hermes exports HERMES_HOME into every hook subprocess and into the
	// terminal tool's shell, so its presence is a strong runtime signal.
	if os.Getenv("HERMES_HOME") != "" {
		return &adapterprotocol.DetectResponse{Detected: true, Reason: "HERMES_HOME set"}, nil
	}
	if _, err := os.Stat(hermesDBPath()); err == nil {
		return &adapterprotocol.DetectResponse{Detected: true, Reason: "found state.db"}, nil
	}
	if dir := hermesHome(); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return &adapterprotocol.DetectResponse{Detected: true, Reason: "found " + dir}, nil
		}
	}
	if _, err := exec.LookPath("hermes"); err == nil {
		return &adapterprotocol.DetectResponse{Detected: true, Reason: "hermes binary found in PATH"}, nil
	}
	return &adapterprotocol.DetectResponse{Detected: false, Reason: "hermes home not found and hermes not in PATH"}, nil
}

func handleImportSession(p adapterprotocol.ImportSessionParams) (*adapterprotocol.ImportSessionResult, error) {
	if p.SessionID == "" {
		return nil, fmt.Errorf("--session-id is required")
	}

	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	var id string
	err = db.QueryRow("SELECT id FROM sessions WHERE id = ? LIMIT 1", p.SessionID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("session %q not found", p.SessionID)
	}
	if err != nil {
		return nil, fmt.Errorf("querying session: %w", err)
	}

	entries, _, err := readMessages(db, p.SessionID, 0)
	if err != nil {
		return nil, fmt.Errorf("reading session: %w", err)
	}

	meta, metaErr := readMetadata(db, p.SessionID)
	if metaErr != nil {
		slog.Warn("reading hermes session metadata", "session_id", p.SessionID, "err", metaErr)
	}

	return &adapterprotocol.ImportSessionResult{Metadata: meta, Entries: entries}, nil
}

func handleDiagnose(p adapterprotocol.DiagnoseParams) (*adapterprotocol.DiagnoseResult, error) {
	var issues []adapterprotocol.DiagnoseIssue

	if _, err := exec.LookPath("hermes"); err != nil {
		issues = append(issues, adapterprotocol.DiagnoseIssue{
			Slug:     "not-installed",
			Severity: "warning",
			Title:    "Hermes CLI not detected",
			Detail:   "hermes binary not found in PATH. The desktop app ships its own launcher, so this can be a false alarm; hook installation needs the CLI though.",
		})
	}

	if _, err := os.Stat(hermesDBPath()); err != nil {
		issues = append(issues, adapterprotocol.DiagnoseIssue{
			Slug:     "no-database",
			Severity: "info",
			Title:    "Hermes session database not found",
			Detail:   fmt.Sprintf("%s not found — session reading unavailable until Hermes is used.", hermesDBPath()),
		})
	}

	// Hooks are user-scoped in Hermes regardless of the scope ox asked for.
	check, err := handleCheckHooks(adapterprotocol.HookParams{RepoRoot: p.RepoRoot, Scope: scopeUser})
	if err == nil && !check.Installed {
		issues = append(issues, adapterprotocol.DiagnoseIssue{
			Slug:     "hooks-missing",
			Severity: "warning",
			Title:    "Hermes hooks not installed",
			Detail:   fmt.Sprintf("no ox shell hooks in %s.", configPath()),
			Fix:      "ox-adapter-hermes install-hooks --scope user",
			FixSafe:  true,
		})
	} else if err == nil && !hooksAutoAccepted() {
		issues = append(issues, adapterprotocol.DiagnoseIssue{
			Slug:     "hooks-consent",
			Severity: "info",
			Title:    "Hermes hooks await first-use consent",
			Detail:   "Hermes prompts once per hook the first time it fires in an interactive session. Non-interactive surfaces (gateway, cron) skip un-approved hooks; set hooks_auto_accept: true to pre-approve.",
			Fix:      "hermes config set hooks_auto_accept true",
			FixSafe:  false,
		})
	}

	return &adapterprotocol.DiagnoseResult{OK: len(issues) == 0, Issues: issues}, nil
}

// hermesHome mirrors Hermes's get_hermes_home(): $HERMES_HOME wins; otherwise
// the platform default. Named profiles set HERMES_HOME themselves, so a hook
// fired by profile "coder" resolves to that profile's directory automatically.
func hermesHome() string {
	if h := os.Getenv("HERMES_HOME"); h != "" {
		return h
	}
	if runtime.GOOS == "windows" {
		if lad := os.Getenv("LOCALAPPDATA"); lad != "" {
			return filepath.Join(lad, "hermes")
		}
	}
	home, err := homedir.Dir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".hermes")
}

func hermesDBPath() string {
	root := hermesHome()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "state.db")
}

func configPath() string {
	root := hermesHome()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "config.yaml")
}
