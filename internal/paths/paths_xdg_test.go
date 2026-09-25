package paths

import (
	"github.com/sageox/ox/internal/testguard"

	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sageox/ox/internal/endpoint"
)

func TestSageoxDir(t *testing.T) {
	saved := saveEnv("OX_XDG_ENABLE", "OX_XDG_DISABLE")
	defer restoreEnv(saved)

	t.Run("default mode returns empty (XDG is default)", func(t *testing.T) {
		clearXDGEnv()
		dir := SageoxDir()
		// XDG is now the default, so SageoxDir returns empty
		if dir != "" {
			t.Errorf("SageoxDir() = %q in default (XDG) mode, want empty", dir)
		}
	})

	t.Run("legacy mode returns .sageox", func(t *testing.T) {
		setLegacyMode()
		dir := SageoxDir()
		if dir == "" {
			t.Error("SageoxDir() returned empty string in legacy mode")
		}
		if !strings.HasSuffix(dir, ".sageox") {
			t.Errorf("SageoxDir() = %q, want suffix .sageox", dir)
		}
	})

	t.Run("OX_XDG_ENABLE still works for compatibility", func(t *testing.T) {
		clearXDGEnv()
		os.Setenv("OX_XDG_ENABLE", "1")
		dir := SageoxDir()
		if dir != "" {
			t.Errorf("SageoxDir() with OX_XDG_ENABLE=1 = %q, want empty", dir)
		}
	})
}

func TestConfigDir(t *testing.T) {
	saved := saveEnv("OX_XDG_ENABLE", "OX_XDG_DISABLE", "XDG_CONFIG_HOME")
	defer restoreEnv(saved)

	t.Run("default mode uses XDG", func(t *testing.T) {
		clearXDGEnv()
		dir := filepath.ToSlash(ConfigDir())
		// XDG is now the default
		if !strings.Contains(dir, ".config") || !strings.HasSuffix(dir, "sageox") {
			t.Errorf("ConfigDir() = %q, want ~/.config/sageox", dir)
		}
	})

	t.Run("default mode respects XDG_CONFIG_HOME", func(t *testing.T) {
		clearXDGEnv()
		os.Setenv("XDG_CONFIG_HOME", testguard.FakePath("/custom/config"))
		dir := filepath.ToSlash(ConfigDir())
		want := filepath.ToSlash(testguard.FakePath("/custom/config/sageox"))
		if dir != want {
			t.Errorf("ConfigDir() = %q, want %q", dir, want)
		}
	})

	t.Run("legacy mode uses .sageox", func(t *testing.T) {
		setLegacyMode()
		dir := filepath.ToSlash(ConfigDir())
		if !strings.Contains(dir, ".sageox") || !strings.HasSuffix(dir, "config") {
			t.Errorf("ConfigDir() = %q in legacy mode, want ~/.sageox/config", dir)
		}
	})

	t.Run("legacy mode ignores XDG_CONFIG_HOME", func(t *testing.T) {
		setLegacyMode()
		os.Setenv("XDG_CONFIG_HOME", testguard.FakePath("/custom/config"))
		dir := filepath.ToSlash(ConfigDir())
		// in legacy mode, XDG_CONFIG_HOME should be ignored
		if strings.Contains(dir, "/custom/") {
			t.Errorf("ConfigDir() = %q, should ignore XDG_CONFIG_HOME in legacy mode", dir)
		}
		if !strings.Contains(dir, ".sageox/config") {
			t.Errorf("ConfigDir() = %q, want ~/.sageox/config", dir)
		}
	})
}

func TestXDGPartialConfiguration(t *testing.T) {
	saved := saveEnv("OX_XDG_ENABLE", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR")
	defer restoreEnv(saved)

	t.Run("XDG mode with only some vars set", func(t *testing.T) {
		clearXDGEnv()
		os.Setenv("OX_XDG_ENABLE", "1")
		// only set XDG_CONFIG_HOME, leave others unset
		os.Setenv("XDG_CONFIG_HOME", testguard.FakePath("/custom/config"))

		// config should use custom path
		configDir := filepath.ToSlash(ConfigDir())
		wantConfig := filepath.ToSlash(testguard.FakePath("/custom/config/sageox"))
		if configDir != wantConfig {
			t.Errorf("ConfigDir() = %q, want %q", configDir, wantConfig)
		}

		// data should use default XDG path
		dataDir := filepath.ToSlash(DataDir())
		if !strings.Contains(dataDir, ".local/share/sageox") {
			t.Errorf("DataDir() = %q, want to contain .local/share/sageox", dataDir)
		}

		// cache should use default XDG path
		cacheDir := filepath.ToSlash(CacheDir())
		if !strings.Contains(cacheDir, ".cache/sageox") {
			t.Errorf("CacheDir() = %q, want to contain .cache/sageox", cacheDir)
		}
	})

	t.Run("XDG mode with mixed custom paths", func(t *testing.T) {
		clearXDGEnv()
		os.Setenv("OX_XDG_ENABLE", "1")
		os.Setenv("XDG_CONFIG_HOME", testguard.FakePath("/a/config"))
		os.Setenv("XDG_DATA_HOME", testguard.FakePath("/b/data"))
		// leave cache and runtime unset

		if got, want := ConfigDir(), testguard.FakePath("/a/config/sageox"); got != want {
			t.Errorf("ConfigDir() = %q, want %q", got, want)
		}
		if got, want := DataDir(), testguard.FakePath("/b/data/sageox"); got != want {
			t.Errorf("DataDir() = %q, want %q", got, want)
		}
		// cache uses default
		if got := filepath.ToSlash(CacheDir()); !strings.Contains(got, ".cache/sageox") {
			t.Errorf("CacheDir() = %q, want to contain .cache/sageox", got)
		}
	})

	t.Run("XDG runtime dir for daemon state", func(t *testing.T) {
		clearXDGEnv()
		os.Setenv("OX_XDG_ENABLE", "1")
		os.Setenv("XDG_RUNTIME_DIR", testguard.FakePath("/run/user/1000"))

		stateDir := filepath.ToSlash(StateDir())
		want := filepath.ToSlash(testguard.FakePath("/run/user/1000/sageox"))
		if stateDir != want {
			t.Errorf("StateDir() = %q, want %q", stateDir, want)
		}
	})

	t.Run("XDG runtime dir fallback to tmp", func(t *testing.T) {
		clearXDGEnv()
		os.Setenv("OX_XDG_ENABLE", "1")
		// XDG_RUNTIME_DIR not set

		stateDir := filepath.ToSlash(StateDir())
		// should fall back to temp dir
		if !strings.Contains(stateDir, "sageox") {
			t.Errorf("StateDir() = %q, want to contain sageox", stateDir)
		}
	})
}

func TestConsistencyBetweenModes(t *testing.T) {
	saved := saveEnv("OX_XDG_ENABLE", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR")
	defer restoreEnv(saved)

	t.Run("all paths use consistent base in default mode", func(t *testing.T) {
		clearXDGEnv()

		baseDir := SageoxDir()
		configDir := filepath.ToSlash(ConfigDir())
		dataDir := filepath.ToSlash(DataDir())
		cacheDir := filepath.ToSlash(CacheDir())
		stateDir := filepath.ToSlash(StateDir())

		// all should be under ~/.sageox/
		for name, dir := range map[string]string{
			"config": configDir,
			"data":   dataDir,
			"cache":  cacheDir,
			"state":  stateDir,
		} {
			if !strings.HasPrefix(dir, baseDir) {
				t.Errorf("%sDir() = %q, want prefix %q", name, dir, baseDir)
			}
		}
	})

	t.Run("specific files resolve under correct dirs", func(t *testing.T) {
		clearXDGEnv()

		// config files
		if !strings.HasPrefix(UserConfigFile(), ConfigDir()) {
			t.Errorf("UserConfigFile() not under ConfigDir()")
		}
		if !strings.HasPrefix(AuthFile(), ConfigDir()) {
			t.Errorf("AuthFile() not under ConfigDir()")
		}

		// cache files
		if !strings.HasPrefix(GuidanceCacheDir(), CacheDir()) {
			t.Errorf("GuidanceCacheDir() not under CacheDir()")
		}

		// data files (production endpoint - should be under DataDir/<endpoint>/teams/)
		teamsDir := TeamsDataDir(endpoint.Production)
		if !strings.HasPrefix(teamsDir, DataDir()) {
			t.Errorf("TeamsDataDir() not under DataDir()")
		}
		// verify endpoint slug is included in path
		if !strings.Contains(teamsDir, "sageox.ai") {
			t.Errorf("TeamsDataDir() = %q, should contain endpoint slug sageox.ai", teamsDir)
		}

		// ledger files (production endpoint - should be under DataDir/<endpoint>/ledgers/)
		ledgersDir := LedgersDataDir("repo123", endpoint.Production)
		if !strings.HasPrefix(ledgersDir, DataDir()) {
			t.Errorf("LedgersDataDir() not under DataDir()")
		}
		if !strings.Contains(ledgersDir, "sageox.ai") {
			t.Errorf("LedgersDataDir() = %q, should contain endpoint slug sageox.ai", ledgersDir)
		}

		// state files
		if !strings.HasPrefix(DaemonStateDir(), StateDir()) {
			t.Errorf("DaemonStateDir() not under StateDir()")
		}
	})
}

func TestDataDir(t *testing.T) {
	saved := saveEnv("OX_XDG_ENABLE", "OX_XDG_DISABLE", "XDG_DATA_HOME")
	defer restoreEnv(saved)

	t.Run("default mode uses XDG", func(t *testing.T) {
		clearXDGEnv()
		dir := filepath.ToSlash(DataDir())
		// XDG is now the default
		if !strings.Contains(dir, ".local/share") || !strings.HasSuffix(dir, "sageox") {
			t.Errorf("DataDir() = %q, want ~/.local/share/sageox", dir)
		}
	})

	t.Run("default mode respects XDG_DATA_HOME", func(t *testing.T) {
		clearXDGEnv()
		os.Setenv("XDG_DATA_HOME", testguard.FakePath("/custom/data"))
		dir := filepath.ToSlash(DataDir())
		want := filepath.ToSlash(testguard.FakePath("/custom/data/sageox"))
		if dir != want {
			t.Errorf("DataDir() = %q, want %q", dir, want)
		}
	})

	t.Run("legacy mode uses .sageox", func(t *testing.T) {
		setLegacyMode()
		dir := filepath.ToSlash(DataDir())
		if !strings.Contains(dir, ".sageox") || !strings.HasSuffix(dir, "data") {
			t.Errorf("DataDir() = %q in legacy mode, want ~/.sageox/data", dir)
		}
	})
}

func TestCacheDir(t *testing.T) {
	saved := saveEnv("OX_XDG_ENABLE", "OX_XDG_DISABLE", "XDG_CACHE_HOME")
	defer restoreEnv(saved)

	t.Run("default mode uses XDG", func(t *testing.T) {
		clearXDGEnv()
		dir := filepath.ToSlash(CacheDir())
		// XDG is now the default
		if !strings.Contains(dir, ".cache") || !strings.HasSuffix(dir, "sageox") {
			t.Errorf("CacheDir() = %q, want ~/.cache/sageox", dir)
		}
	})

	t.Run("default mode respects XDG_CACHE_HOME", func(t *testing.T) {
		clearXDGEnv()
		os.Setenv("XDG_CACHE_HOME", testguard.FakePath("/custom/cache"))
		dir := filepath.ToSlash(CacheDir())
		want := filepath.ToSlash(testguard.FakePath("/custom/cache/sageox"))
		if dir != want {
			t.Errorf("CacheDir() = %q, want %q", dir, want)
		}
	})

	t.Run("legacy mode uses .sageox", func(t *testing.T) {
		setLegacyMode()
		dir := filepath.ToSlash(CacheDir())
		if !strings.Contains(dir, ".sageox") || !strings.HasSuffix(dir, "cache") {
			t.Errorf("CacheDir() = %q in legacy mode, want ~/.sageox/cache", dir)
		}
	})
}

func TestStateDir(t *testing.T) {
	saved := saveEnv("OX_XDG_ENABLE", "OX_XDG_DISABLE", "XDG_RUNTIME_DIR")
	defer restoreEnv(saved)

	t.Run("default mode uses XDG_RUNTIME_DIR", func(t *testing.T) {
		clearXDGEnv()
		os.Setenv("XDG_RUNTIME_DIR", testguard.FakePath("/run/user/1000"))
		dir := filepath.ToSlash(StateDir())
		want := filepath.ToSlash(testguard.FakePath("/run/user/1000/sageox"))
		if dir != want {
			t.Errorf("StateDir() = %q, want %q", dir, want)
		}
	})

	t.Run("default mode falls back to temp dir", func(t *testing.T) {
		clearXDGEnv()
		// no XDG_RUNTIME_DIR set, falls back to os.TempDir()
		dir := filepath.ToSlash(StateDir())
		// should contain "sageox" suffix
		if !strings.HasSuffix(dir, "sageox") {
			t.Errorf("StateDir() = %q, want suffix 'sageox'", dir)
		}
	})

	t.Run("legacy mode uses .sageox", func(t *testing.T) {
		setLegacyMode()
		dir := filepath.ToSlash(StateDir())
		if !strings.Contains(dir, ".sageox") || !strings.HasSuffix(dir, "state") {
			t.Errorf("StateDir() = %q in legacy mode, want ~/.sageox/state", dir)
		}
	})
}

func TestXDGStateHome(t *testing.T) {
	saved := saveEnv("OX_XDG_ENABLE", "OX_XDG_DISABLE", "XDG_STATE_HOME")
	defer restoreEnv(saved)

	t.Run("respects XDG_STATE_HOME", func(t *testing.T) {
		clearXDGEnv()
		os.Setenv("XDG_STATE_HOME", testguard.FakePath("/custom/state"))
		// xdgStateHome is not exported, but we can test it indirectly via StateDir
		// when OX_XDG_DISABLE is NOT set (XDG mode), StateDir uses xdgRuntimeDir
		// so we need legacy mode where StateDir uses SageoxDir()/state
		// Actually xdgStateHome is used nowhere in production currently...
		// Let's test it directly since we're in the same package
		result := xdgStateHome()
		want := testguard.FakePath("/custom/state")
		if result != want {
			t.Errorf("xdgStateHome() = %q, want %q", result, want)
		}
	})

	t.Run("defaults to ~/.local/state", func(t *testing.T) {
		clearXDGEnv()
		os.Unsetenv("XDG_STATE_HOME")
		result := xdgStateHome()
		if !strings.HasSuffix(result, filepath.Join(".local", "state")) {
			t.Errorf("xdgStateHome() = %q, want suffix .local/state", result)
		}
	})
}

// setLegacyMode enables legacy mode (non-XDG paths)
func setLegacyMode() {
	clearXDGEnv()
	os.Setenv("OX_XDG_DISABLE", "1")
}
