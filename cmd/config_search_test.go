package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

// resetForTest clears global state (viper + cmd-level vars) so each test
// can drive initConfig from a clean slate.
func resetForTest(t *testing.T) {
	t.Helper()
	viper.Reset()
	prev := cfgFile
	cfgFile = ""
	t.Cleanup(func() {
		viper.Reset()
		cfgFile = prev
	})
}

// TestInitConfig_DiscoversHapiqrc verifies that initConfig finds and loads
// $HOME/.hapiqrc when it exists. This is the documented primary config
// path and the one most users will hit.
func TestInitConfig_DiscoversHapiqrc(t *testing.T) {
	resetForTest(t)

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	cacheDir := filepath.Join(fakeHome, "my-cache")
	body := "[cache]\nmode = \"on\"\ndir = \"" + cacheDir + "\"\n"
	if err := os.WriteFile(filepath.Join(fakeHome, ".hapiqrc"), []byte(body), 0o600); err != nil {
		t.Fatalf("write .hapiqrc: %v", err)
	}

	initConfig()

	if got := viper.GetString("cache.mode"); got != "on" {
		t.Errorf("cache.mode: got %q, want \"on\" — initConfig did not load ~/.hapiqrc", got)
	}
	if got := viper.GetString("cache.dir"); got != cacheDir {
		t.Errorf("cache.dir: got %q, want %q", got, cacheDir)
	}
}

// TestInitConfig_NoConfigUsesDefaults verifies that when no config file
// exists in any of the search paths, initConfig leaves viper with the
// registered defaults rather than failing.
//
// initConfig also falls through to /etc/hapiq/config.toml as the final
// search path. If the test host has a real /etc/hapiq/config.toml the test
// is skipped — the fallback is doing its job, but we cannot make
// assertions about defaults under those conditions.
func TestInitConfig_NoConfigUsesDefaults(t *testing.T) {
	if _, err := os.Stat("/etc/hapiq/config.toml"); err == nil {
		t.Skip("/etc/hapiq/config.toml exists on host; defaults test cannot run in isolation")
	}
	resetForTest(t)

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	initConfig()

	if got := viper.GetString("cache.mode"); got != "off" {
		t.Errorf("cache.mode default: got %q, want \"off\"", got)
	}
}

// TestInitConfig_ConfigFlagWithTomlExtension verifies the --config <path.toml>
// codepath. This is the path used to point at a system-wide
// /etc/hapiq/config.toml or any explicit override; it works because viper
// infers the type from the .toml extension.
func TestInitConfig_ConfigFlagWithTomlExtension(t *testing.T) {
	resetForTest(t)

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	cacheDir := filepath.Join(dir, "explicit-cache")
	body := "[cache]\nmode = \"on\"\ndir = \"" + cacheDir + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfgFile = cfgPath
	initConfig()

	if got := viper.GetString("cache.mode"); got != "on" {
		t.Errorf("cache.mode: got %q, want \"on\"", got)
	}
	if got := viper.GetString("cache.dir"); got != cacheDir {
		t.Errorf("cache.dir: got %q, want %q", got, cacheDir)
	}
}

// TestInitConfig_ConfigFlagWithExtensionlessHapiqrc is a REGRESSION TEST for
// a real production bug: when the user passes `--config /path/to/.hapiqrc`
// (extensionless TOML, the documented hapiq config name), viper cannot
// detect the type and silently drops the file. The user's cache
// configuration is ignored.
//
// The cmd/root.go --config branch must call viper.SetConfigType("toml")
// before SetConfigFile to fix this. Tracked in scratch/roadmap.md.
//
// Locked in as a regression guard after the cmd/root.go fix that sniffs
// the type from the file extension and defaults to TOML for unknown
// extensions (covers the documented ~/.hapiqrc).
func TestInitConfig_ConfigFlagWithExtensionlessHapiqrc(t *testing.T) {
	resetForTest(t)

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".hapiqrc")
	cacheDir := filepath.Join(dir, "explicit-cache")
	body := "[cache]\nmode = \"on\"\ndir = \"" + cacheDir + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfgFile = cfgPath
	initConfig()

	if got := viper.GetString("cache.mode"); got != "on" {
		t.Errorf("cache.mode: got %q, want \"on\" — extensionless --config silently dropped, see scratch/roadmap.md", got)
	}
}
