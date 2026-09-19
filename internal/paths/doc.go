// Package paths provides centralized path resolution for all SageOx directories.
//
// # Architecture Overview
//
// This package implements an XDG-inspired directory structure within ~/.sageox/
// for discoverability, while supporting full XDG compliance via OX_XDG_ENABLE=1
// for users who prefer standard XDG locations.
//
// # Directory Structure
//
// Default layout (~/.sageox/):
//
//	~/.sageox/
//	├── config/    # User configuration (XDG_CONFIG_HOME equivalent)
//	├── data/      # Persistent data like team contexts (XDG_DATA_HOME equivalent)
//	├── cache/     # Disposable cached data (XDG_CACHE_HOME equivalent)
//	└── state/     # Runtime state like daemon sockets (XDG_STATE_HOME equivalent)
//
// # Why ~/.sageox/ Instead of XDG by Default
//
//  1. Single discoverable location - users can easily find all SageOx data
//  2. Easier to inspect, backup, and troubleshoot
//  3. Follows precedent of ~/.docker, ~/.cargo, ~/.npm
//  4. XDG purists can opt-in via OX_XDG_ENABLE=1
//
// # XDG Compatibility Mode
//
// Set OX_XDG_ENABLE=1 to use standard XDG locations:
//
//	config/ → $XDG_CONFIG_HOME/sageox/  (default: ~/.config/sageox/)
//	data/   → $XDG_DATA_HOME/sageox/    (default: ~/.local/share/sageox/)
//	cache/  → $XDG_CACHE_HOME/sageox/   (default: ~/.cache/sageox/)
//	state/  → $XDG_STATE_HOME/sageox/   (default: ~/.local/state/sageox/)
//
// Note: When OX_XDG_ENABLE=1, daemon state uses $XDG_RUNTIME_DIR/sageox/ if available,
// falling back to /tmp/sageox/ for ephemeral runtime files (sockets, locks).
//
// # Usage
//
// All path access should go through this package. Never hardcode paths elsewhere:
//
//	// Good
//	configPath := paths.UserConfigFile()
//	teamsDir := paths.TeamsDataDir()
//
//	// Bad - don't do this
//	configPath := filepath.Join(homedir.Dir(), ".sageox", "config", "config.yaml")
//
// # Thread Safety
//
// All functions in this package are safe for concurrent use. Path resolution is
// deterministic based on environment variables read at call time.
package paths
