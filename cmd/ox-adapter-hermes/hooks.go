// hooks.go installs ox lifecycle hooks into a Hermes profile's config.yaml.
//
// Hermes "shell hooks" are declared under a top-level `hooks:` mapping:
//
//	hooks:
//	  on_session_start:
//	    - command: "ox agent hook on_session_start"
//	      timeout: 30
//	  pre_llm_call:
//	    - command: "ox agent hook pre_llm_call"
//	hooks_auto_accept: true
//
// Each entry is run with shell=False (shlex-style split — no shell, so no
// `if command -v ox` wrapper; a missing binary is a logged warning, never a
// crash) and receives a Claude-Code-shaped JSON payload on stdin:
//
//	{"hook_event_name": "...", "tool_name": ..., "tool_input": ..., "session_id": "...",
//	 "cwd": "...", "profile": "default", "extra": {...}}
//
// stdout is parsed as JSON. For pre_llm_call a {"context": "..."} object is
// injected into the model's next turn — that is where ox's prime output and
// whispers go. Other events ignore stdout.
//
// Hermes has no per-project hook file. Scope "project" therefore installs the
// same user-level hooks; the per-repo half of the integration is the AGENTS.md
// prime marker, which Hermes reads natively.
//
// Reference: https://hermes-agent.nousresearch.com/docs/user-guide/features/hooks
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sageox/ox/internal/fileutil"
	"github.com/sageox/ox/pkg/adapterprotocol"
)

const (
	scopeUser = "user"

	// oxHookMarker identifies a hook entry this adapter installed. Uninstall
	// removes only entries containing it, because the hooks: block is shared
	// with the user's own hooks.
	oxHookMarker = "ox agent hook"

	hooksKey      = "hooks"
	autoAcceptKey = "hooks_auto_accept"
)

// hookEvents are the Hermes events ox acts on, and how they map onto ox's
// canonical lifecycle phases (see cmd/ox/agent_hook.go localEventPhases):
//
//	on_session_start    → start      prime + start recording
//	pre_llm_call        → prompt     whispers / recall preamble via {"context": ...}
//	post_tool_call      → after_tool heartbeat, session tail
//	on_session_end      → stop       fires at every turn finalization
//	on_session_finalize → end        CLI/TUI/gateway teardown
//
// pre_llm_call is Hermes's UserPromptSubmit equivalent and the ONLY event
// whose stdout reaches the model, so it is where prime output must land.
// on_session_end fires per turn, not per session — ox's stop phase is already
// idempotent for that (Codex has the same shape).
var hookEvents = []string{
	"on_session_start",
	"pre_llm_call",
	"post_tool_call",
	"on_session_end",
	"on_session_finalize",
}

// hookTimeouts overrides Hermes's 60s default. on_session_start runs a full
// `ox agent prime`; everything else is a fast local call and must never
// stall a turn if ox wedges.
var hookTimeouts = map[string]int{
	"on_session_start": 30,
	"pre_llm_call":     15,
}

const defaultHookTimeout = 10

// hookCommand is the argv Hermes will split and exec. No shell, so no
// AGENT_ENV=hermes prefix is possible here; ox learns the agent from the
// `--agent hermes` flag on the hook subcommand instead (see agent_hook.go).
// The binary is a bare "ox" so PATH resolution — including .exe on Windows
// — is Hermes's problem, not a path we bake into the user's config.
func hookCommand(event string) string {
	return fmt.Sprintf("ox agent hook %s --agent hermes", event)
}

// --- install / check / uninstall ---

func handleInstallHooks(p adapterprotocol.HookParams) (*adapterprotocol.InstallHooksResponse, error) {
	path := configPath()
	if path == "" {
		return nil, fmt.Errorf("cannot resolve Hermes home directory")
	}

	root, err := loadConfig(path)
	if err != nil {
		return nil, err
	}

	hooks := ensureMapping(root, hooksKey)
	for _, event := range hookEvents {
		seq := ensureSequence(hooks, event)
		if sequenceHasOxHook(seq) {
			continue
		}
		entry := &yaml.Node{Kind: yaml.MappingNode}
		entry.Content = append(entry.Content,
			scalar("command"), scalar(hookCommand(event)),
			scalar("timeout"), intScalar(timeoutFor(event)),
		)
		seq.Content = append(seq.Content, entry)
	}

	if err := saveConfig(path, root); err != nil {
		return nil, err
	}

	return &adapterprotocol.InstallHooksResponse{
		Installed:    true,
		FilesWritten: []string{path},
		Hooks:        hookEvents,
	}, nil
}

func handleCheckHooks(_ adapterprotocol.HookParams) (*adapterprotocol.CheckHooksResponse, error) {
	path := configPath()
	files := []string{path}
	if path == "" {
		return &adapterprotocol.CheckHooksResponse{Installed: false, Scope: scopeUser, HookFiles: files}, nil
	}
	root, err := loadConfig(path)
	if err != nil {
		return &adapterprotocol.CheckHooksResponse{Installed: false, Scope: scopeUser, HookFiles: files}, nil
	}
	hooks := findMapping(root, hooksKey)
	if hooks == nil {
		return &adapterprotocol.CheckHooksResponse{Installed: false, Scope: scopeUser, HookFiles: files}, nil
	}
	for _, event := range hookEvents {
		if !sequenceHasOxHook(findSequence(hooks, event)) {
			return &adapterprotocol.CheckHooksResponse{Installed: false, Scope: scopeUser, HookFiles: files}, nil
		}
	}
	return &adapterprotocol.CheckHooksResponse{Installed: true, Scope: scopeUser, HookFiles: files}, nil
}

func handleUninstallHooks(_ adapterprotocol.HookParams) (*adapterprotocol.UninstallHooksResponse, error) {
	path := configPath()
	if path == "" {
		return &adapterprotocol.UninstallHooksResponse{Uninstalled: true}, nil
	}
	root, err := loadConfig(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &adapterprotocol.UninstallHooksResponse{Uninstalled: true}, nil
		}
		return nil, err
	}
	hooks := findMapping(root, hooksKey)
	if hooks == nil {
		return &adapterprotocol.UninstallHooksResponse{Uninstalled: true}, nil
	}

	changed := false
	for i := 0; i+1 < len(hooks.Content); {
		key, seq := hooks.Content[i], hooks.Content[i+1]
		if seq.Kind == yaml.SequenceNode {
			kept := seq.Content[:0]
			for _, e := range seq.Content {
				if entryIsOxHook(e) {
					changed = true
					continue
				}
				kept = append(kept, e)
			}
			seq.Content = kept
		}
		if seq.Kind == yaml.SequenceNode && len(seq.Content) == 0 {
			// drop the now-empty event key so we leave no trace
			hooks.Content = append(hooks.Content[:i], hooks.Content[i+2:]...)
			changed = true
			_ = key
			continue
		}
		i += 2
	}
	if len(hooks.Content) == 0 {
		removeKey(root, hooksKey)
		changed = true
	}

	if !changed {
		return &adapterprotocol.UninstallHooksResponse{Uninstalled: true}, nil
	}
	if err := saveConfig(path, root); err != nil {
		return nil, err
	}
	return &adapterprotocol.UninstallHooksResponse{Uninstalled: true, FilesModified: []string{path}}, nil
}

// hooksAutoAccepted reports whether the profile pre-approves shell hooks.
// Without it, the first fire of each (event, command) pair prompts in an
// interactive session and is skipped in a non-interactive one.
func hooksAutoAccepted() bool {
	path := configPath()
	if path == "" {
		return false
	}
	root, err := loadConfig(path)
	if err != nil {
		return false
	}
	v := findValue(root, autoAcceptKey)
	return v != nil && strings.EqualFold(v.Value, "true")
}

// --- YAML node helpers ---
//
// The config is edited at the node level and re-encoded so every key the user
// (or Hermes itself) put there survives byte-for-byte in meaning; comments and
// ordering are preserved by yaml.v3's node round-trip. This is the same kind
// of structured write `hermes config set` performs — never a text splice.

func loadConfig(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path derived from HERMES_HOME
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// A fresh profile has no config.yaml yet; start from an empty
			// document so install works before the first `hermes setup`.
			return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: top level is not a mapping", path)
	}
	return &doc, nil
}

func saveConfig(path string, root *yaml.Node) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return err
	}
	// Hermes reloads config.yaml live (gateway, desktop); a torn write would
	// take the running instance down with a parse error.
	return fileutil.AtomicWriteBytes(path, buf.Bytes(), 0o600)
}

func top(root *yaml.Node) *yaml.Node { return root.Content[0] }

func scalar(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

func intScalar(n int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(n)}
}

func findValue(root *yaml.Node, key string) *yaml.Node {
	m := top(root)
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func findMapping(root *yaml.Node, key string) *yaml.Node {
	v := findValue(root, key)
	if v == nil || v.Kind != yaml.MappingNode {
		return nil
	}
	return v
}

func ensureMapping(root *yaml.Node, key string) *yaml.Node {
	if m := findMapping(root, key); m != nil {
		return m
	}
	// replace a null/scalar placeholder (`hooks:` with nothing under it) too
	m := top(root)
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = &yaml.Node{Kind: yaml.MappingNode}
			return m.Content[i+1]
		}
	}
	v := &yaml.Node{Kind: yaml.MappingNode}
	m.Content = append(m.Content, scalar(key), v)
	return v
}

func removeKey(root *yaml.Node, key string) {
	m := top(root)
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

func findSequence(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key && m.Content[i+1].Kind == yaml.SequenceNode {
			return m.Content[i+1]
		}
	}
	return nil
}

func ensureSequence(m *yaml.Node, key string) *yaml.Node {
	if s := findSequence(m, key); s != nil {
		return s
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = &yaml.Node{Kind: yaml.SequenceNode}
			return m.Content[i+1]
		}
	}
	s := &yaml.Node{Kind: yaml.SequenceNode}
	m.Content = append(m.Content, scalar(key), s)
	return s
}

func sequenceHasOxHook(seq *yaml.Node) bool {
	if seq == nil {
		return false
	}
	for _, e := range seq.Content {
		if entryIsOxHook(e) {
			return true
		}
	}
	return false
}

func entryIsOxHook(e *yaml.Node) bool {
	if e.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(e.Content); i += 2 {
		if e.Content[i].Value == "command" && strings.Contains(e.Content[i+1].Value, oxHookMarker) {
			return true
		}
	}
	return false
}

func timeoutFor(event string) int {
	if t, ok := hookTimeouts[event]; ok {
		return t
	}
	return defaultHookTimeout
}
