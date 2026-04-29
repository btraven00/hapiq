package experimenthub

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Cache layout follows the same convention as the hapiq blob cache: a single
// per-user (or per-host) directory under ~/.cache/hapiq, overridable via
// config file (key `experimenthub.metadata_dir` or, as a fallback,
// `cache.dir`) or environment variables (HAPIQ_EXPERIMENTHUB_METADATA_DIR,
// HAPIQ_CACHE_DIR). This lets users point hapiq at a shared site cache or a
// host-local fast disk without code changes.
const (
	defaultCacheSubdir = "hapiq"
	cacheFilename      = "experimenthub.sqlite3"

	viperKeyMetadataDir = "experimenthub.metadata_dir"
	viperKeyMaxAge      = "experimenthub.metadata_max_age"
	viperKeyCacheDir    = "cache.dir" // shared with blob cache when set

	envMetadataDir = "HAPIQ_EXPERIMENTHUB_METADATA_DIR"
	envCacheDir    = "HAPIQ_CACHE_DIR"

	defaultMaxAge = 7 * 24 * time.Hour
)

// RegisterDefaults sets viper defaults; safe to call multiple times.
func RegisterDefaults() {
	viper.SetDefault(viperKeyMetadataDir, "")
	viper.SetDefault(viperKeyMaxAge, defaultMaxAge.String())
}

// resolveCacheDir picks the directory holding the metadata sqlite, in order:
//  1. viper `experimenthub.metadata_dir`
//  2. env HAPIQ_EXPERIMENTHUB_METADATA_DIR
//  3. viper `cache.dir`
//  4. env HAPIQ_CACHE_DIR
//  5. ~/.cache/hapiq
func resolveCacheDir() string {
	candidates := []string{
		viper.GetString(viperKeyMetadataDir),
		os.Getenv(envMetadataDir),
		viper.GetString(viperKeyCacheDir),
		os.Getenv(envCacheDir),
	}
	for _, c := range candidates {
		if c = strings.TrimSpace(c); c != "" {
			return expandHome(c)
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", defaultCacheSubdir)
}

// resolveMaxAge returns the catalog refresh interval.
func resolveMaxAge() time.Duration {
	if s := strings.TrimSpace(viper.GetString(viperKeyMaxAge)); s != "" {
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			return d
		}
	}
	return defaultMaxAge
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") || p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
