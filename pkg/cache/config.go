package cache

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config holds resolved cache configuration.
type Config struct {
	Mode         string
	Dir          string
	LinkStrategy Strategy
	MaxSize      int64
	MinFreeDisk  int64
	QuotaPolicy  string
}

// DefaultDir returns the default cache directory (~/.cache/hapiq).
func DefaultDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "hapiq")
}

// RegisterDefaults sets Viper defaults for all cache keys.
// Call this once during app init so unset keys have sensible values.
func RegisterDefaults() {
	viper.SetDefault("cache.mode", "off")
	viper.SetDefault("cache.dir", "")
	viper.SetDefault("cache.link_strategy", string(StrategyAuto))
	viper.SetDefault("cache.max_size", "")
	viper.SetDefault("cache.min_free_disk", "5GB")
	viper.SetDefault("cache.quota_policy", "lru")
}

// ConfigFromViper builds a Config from the current Viper state.
func ConfigFromViper() Config {
	dir := viper.GetString("cache.dir")
	if dir == "" {
		dir = DefaultDir()
	}
	if strings.HasPrefix(dir, "~/") {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, dir[2:])
	}

	strategy := Strategy(viper.GetString("cache.link_strategy"))
	if strategy == "" {
		strategy = StrategyAuto
	}

	policy := viper.GetString("cache.quota_policy")
	if policy == "" {
		policy = "lru"
	}

	return Config{
		Mode:         viper.GetString("cache.mode"),
		Dir:          dir,
		LinkStrategy: strategy,
		MaxSize:      ParseSizeDefault(viper.GetString("cache.max_size"), 0),
		MinFreeDisk:  ParseSizeDefault(viper.GetString("cache.min_free_disk"), 5*1024*1024*1024),
		QuotaPolicy:  policy,
	}
}
