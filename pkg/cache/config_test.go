package cache_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/btraven00/hapiq/pkg/cache"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
)

// resetViper clears all viper state so each test starts from a clean slate.
// Without this the global viper instance leaks key/value pairs across tests.
func resetViper(t *testing.T) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
}

// TestConfigFromViper_TOMLFile loads a TOML file via viper.SetConfigFile and
// verifies cache.ConfigFromViper produces the expected Config. This is the
// canonical wiring used by the --config flag and the /etc/hapiq/config.toml
// fallback in cmd/root.go.
func TestConfigFromViper_TOMLFile(t *testing.T) {
	resetViper(t)

	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "blobs")
	cfgPath := filepath.Join(dir, "hapiqrc.toml")

	body := "" +
		"[cache]\n" +
		"mode = \"on\"\n" +
		"dir = \"" + cacheDir + "\"\n" +
		"link_strategy = \"hardlink\"\n" +
		"max_size = \"1GB\"\n" +
		"min_free_disk = \"100MB\"\n" +
		"quota_policy = \"lru\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cache.RegisterDefaults()
	viper.SetConfigFile(cfgPath)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}

	cfg := cache.ConfigFromViper()
	if cfg.Mode != "on" {
		t.Errorf("Mode: got %q, want \"on\"", cfg.Mode)
	}
	if cfg.Dir != cacheDir {
		t.Errorf("Dir: got %q, want %q", cfg.Dir, cacheDir)
	}
	if cfg.LinkStrategy != cache.StrategyHardlink {
		t.Errorf("LinkStrategy: got %q, want %q", cfg.LinkStrategy, cache.StrategyHardlink)
	}
	if cfg.MaxSize != 1_000_000_000 {
		t.Errorf("MaxSize: got %d, want 1GB (SI, 10^9)", cfg.MaxSize)
	}
	if cfg.MinFreeDisk != 100_000_000 {
		t.Errorf("MinFreeDisk: got %d, want 100MB (SI, 10^8)", cfg.MinFreeDisk)
	}
}

// TestConfigFromViper_TildeExpansion verifies that a `cache.dir` value
// starting with `~/` is expanded against $HOME. This is a common config-file
// idiom and silently broken if the expansion logic regresses.
func TestConfigFromViper_TildeExpansion(t *testing.T) {
	resetViper(t)

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	cache.RegisterDefaults()
	viper.Set("cache.mode", "on")
	viper.Set("cache.dir", "~/my-hapiq-cache")

	cfg := cache.ConfigFromViper()
	want := filepath.Join(fakeHome, "my-hapiq-cache")
	if cfg.Dir != want {
		t.Errorf("Dir: got %q, want %q", cfg.Dir, want)
	}
}

// TestConfigFromViper_Defaults verifies the empty-config path: when no file
// has been read and only RegisterDefaults has been called, ConfigFromViper
// returns sensible values (cache disabled, default dir, lru policy, 5GB min
// free disk).
func TestConfigFromViper_Defaults(t *testing.T) {
	resetViper(t)
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	cache.RegisterDefaults()
	cfg := cache.ConfigFromViper()

	if cfg.Mode != "off" {
		t.Errorf("default Mode: got %q, want \"off\"", cfg.Mode)
	}
	if want := filepath.Join(fakeHome, ".cache", "hapiq"); cfg.Dir != want {
		t.Errorf("default Dir: got %q, want %q", cfg.Dir, want)
	}
	if cfg.QuotaPolicy != "lru" {
		t.Errorf("default QuotaPolicy: got %q, want \"lru\"", cfg.QuotaPolicy)
	}
	// RegisterDefaults sets min_free_disk to "5GB" (SI). Note: the
	// hard-coded fallback inside ConfigFromViper is 5 * 1<<30 (binary GiB)
	// — these only diverge when RegisterDefaults has not run, which
	// shouldn't happen at runtime. Tracked as a cleanup item in
	// scratch/roadmap.md.
	if cfg.MinFreeDisk != 5_000_000_000 {
		t.Errorf("default MinFreeDisk: got %d, want 5GB (SI)", cfg.MinFreeDisk)
	}
}

// TestConfigFromViper_EndToEndDownload exercises the full wire from a
// config-file on disk through to a blob landing in the cache directory the
// file specified. This is the smoke test that catches viper-key-name
// regressions, struct-tag drift, and any silent config-loading failures.
func TestConfigFromViper_EndToEndDownload(t *testing.T) {
	resetViper(t)

	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "configured-cache")
	// Use a .toml extension so viper.SetConfigFile can infer the type. The
	// production /etc/hapiq/config.toml path goes through the same code path.
	// The ~/.hapiqrc path is exercised separately (it relies on
	// SetConfigType being called explicitly, see TestConfigFromViper_HapiqrcExtensionless).
	cfgPath := filepath.Join(dir, "hapiqrc.toml")

	body := "" +
		"[cache]\n" +
		"mode = \"on\"\n" +
		"dir = \"" + cacheDir + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cache.RegisterDefaults()
	viper.SetConfigFile(cfgPath)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}
	if got := viper.GetString("cache.mode"); got != "on" {
		t.Fatalf("viper failed to load cache.mode from TOML: got %q", got)
	}

	cfg := cache.ConfigFromViper()
	c, err := cache.Open(cfg)
	if err != nil {
		t.Fatalf("cache.Open(%s): %v", cacheDir, err)
	}
	t.Cleanup(func() { _ = c.Close() })

	// Verify the cache root was created at the configured location, not the
	// default. This is the regression we care about: silent fallback to
	// ~/.cache/hapiq when the config-file value is dropped on the floor.
	if _, err := os.Stat(cacheDir); err != nil {
		t.Fatalf("configured cache dir not created: %v", err)
	}

	const blobPath = "/blob.bin"
	content := []byte("config-driven cache smoke test payload")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	t.Cleanup(srv.Close)

	ctx := cache.WithCache(context.Background(), c)
	dest := filepath.Join(t.TempDir(), "out.bin")
	if _, err := common.Fetch(ctx, srv.URL+blobPath, dest, common.FetchOptions{}); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// The blob must land under the configured cache dir, not the default.
	blobsRoot := filepath.Join(cacheDir, "blobs", "sha256")
	if !blobLandedUnder(t, blobsRoot) {
		t.Fatalf("no blob found under %s — cache.dir from config was not honoured", blobsRoot)
	}
}

// blobLandedUnder reports whether at least one regular file exists under
// dir (recursively). Used as a coarse "the cache wrote something" check.
func blobLandedUnder(t *testing.T, dir string) bool {
	t.Helper()
	found := false
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.Mode().IsRegular() {
			found = true
		}
		return nil
	})
	return found
}

// TestConfigFromViper_HapiqrcExtensionless documents the codepath used by
// `--config ~/.hapiqrc` (extensionless TOML file). This must use
// SetConfigType("toml") + SetConfigFile so viper knows the format. The
// equivalent codepath in cmd/root.go (the `if cfgFile != ""` branch) does
// NOT call SetConfigType, which silently breaks for extensionless files —
// see scratch/roadmap.md.
func TestConfigFromViper_HapiqrcExtensionless(t *testing.T) {
	resetViper(t)

	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "blobs")
	cfgPath := filepath.Join(dir, ".hapiqrc")

	body := "[cache]\nmode = \"on\"\ndir = \"" + cacheDir + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cache.RegisterDefaults()
	viper.SetConfigType("toml") // <-- the line missing in cmd/root.go's --config branch
	viper.SetConfigFile(cfgPath)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}

	cfg := cache.ConfigFromViper()
	if cfg.Mode != "on" || cfg.Dir != cacheDir {
		t.Fatalf("config not loaded: %+v", cfg)
	}
}

// TestConfigFromViper_LinkStrategyDefault verifies that an unset link_strategy
// falls back to StrategyAuto rather than the empty string. This is a common
// foot-gun: the empty default would otherwise propagate into Materialize and
// produce confusing errors.
func TestConfigFromViper_LinkStrategyDefault(t *testing.T) {
	resetViper(t)
	t.Setenv("HOME", t.TempDir())
	cache.RegisterDefaults()

	cfg := cache.ConfigFromViper()
	if cfg.LinkStrategy != cache.StrategyAuto {
		t.Errorf("default LinkStrategy: got %q, want %q", cfg.LinkStrategy, cache.StrategyAuto)
	}
	// Sanity: any explicit value should round-trip.
	for _, s := range []cache.Strategy{cache.StrategyHardlink, cache.StrategyCopy, cache.StrategyAuto} {
		viper.Set("cache.link_strategy", string(s))
		got := cache.ConfigFromViper().LinkStrategy
		if got != s {
			t.Errorf("link_strategy=%q round-trip: got %q", s, got)
		}
	}
}

// TestConfigFromViper_KeyNamesAreStable is a regression guard against
// renaming TOML keys without updating viper-side. If someone changes
// `cache.dir` to e.g. `cache.directory` in TOML but forgets to update
// ConfigFromViper, this test will catch it because the parsed key names must
// match what ConfigFromViper expects.
func TestConfigFromViper_KeyNamesAreStable(t *testing.T) {
	resetViper(t)

	const body = `
[cache]
mode = "on"
dir = "/tmp/x"
link_strategy = "copy"
max_size = "2GB"
min_free_disk = "1GB"
quota_policy = "lru"
`
	cfgPath := filepath.Join(t.TempDir(), "k.toml")
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cache.RegisterDefaults()
	viper.SetConfigFile(cfgPath)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}

	// Every key must have made it from TOML into viper under the same name
	// ConfigFromViper reads from. Failure here means a key got dropped or
	// renamed somewhere along the wire.
	for _, key := range []string{
		"cache.mode",
		"cache.dir",
		"cache.link_strategy",
		"cache.max_size",
		"cache.min_free_disk",
		"cache.quota_policy",
	} {
		if v := viper.GetString(key); strings.TrimSpace(v) == "" {
			t.Errorf("key %q: empty after loading TOML — key name drift?", key)
		}
	}

	cfg := cache.ConfigFromViper()
	if cfg.Mode != "on" || cfg.Dir != "/tmp/x" || cfg.LinkStrategy != cache.StrategyCopy {
		t.Errorf("ConfigFromViper did not pick up TOML values: %+v", cfg)
	}
	if cfg.MaxSize != 2_000_000_000 || cfg.MinFreeDisk != 1_000_000_000 {
		t.Errorf("size parsing wrong: MaxSize=%d MinFreeDisk=%d", cfg.MaxSize, cfg.MinFreeDisk)
	}
}
