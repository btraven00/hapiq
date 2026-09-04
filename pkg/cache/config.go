package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	Server       ServerConfig
}

// ServerConfig holds the peer-sharing configuration: both the serving side
// (`hapiq cache serve`) and the client side (which peers to consult before
// hitting the origin URL).
type ServerConfig struct {
	// Enabled starts the server as part of long-running commands. `hapiq cache
	// serve` ignores it and always serves.
	Enabled bool
	// Listen is the server bind address.
	Listen string
	// Token, when non-empty, is a shared bearer token required by every
	// endpoint except /v1/healthz, and sent to peers on outbound requests.
	Token string
	// Advertise is the base URL other nodes should use to reach this one.
	// Empty means derive it from Listen and the outbound interface address.
	Advertise string
	// Peers are base URLs consulted, in order, before the origin URL.
	Peers []string
	// Introducers are peers asked for their peer list at startup. An
	// introducer is an ordinary peer that also answers /v1/peers.
	Introducers []string
	// Discover selects an automatic discovery mechanism: "" (none) or "mdns".
	Discover string
	// Timeout is the base peer deadline (dial and per-request). Zero means
	// DefaultPeerTimeout.
	Timeout time.Duration
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
	viper.SetDefault("cache.server.enabled", false)
	viper.SetDefault("cache.server.listen", "0.0.0.0:7777")
	viper.SetDefault("cache.server.token", "")
	viper.SetDefault("cache.server.advertise", "")
	viper.SetDefault("cache.server.peers", []string{})
	viper.SetDefault("cache.server.introducers", []string{})
	viper.SetDefault("cache.server.discover", "")
	viper.SetDefault("cache.server.timeout", DefaultPeerTimeout.String())
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

	listen := viper.GetString("cache.server.listen")
	if listen == "" {
		listen = "0.0.0.0:7777"
	}

	return Config{
		Mode:         viper.GetString("cache.mode"),
		Dir:          dir,
		LinkStrategy: strategy,
		MaxSize:      ParseSizeDefault(viper.GetString("cache.max_size"), 0),
		MinFreeDisk:  ParseSizeDefault(viper.GetString("cache.min_free_disk"), 5_000_000_000), // 5GB SI, matches RegisterDefaults
		QuotaPolicy:  policy,
		Server: ServerConfig{
			Enabled:     viper.GetBool("cache.server.enabled"),
			Listen:      listen,
			Token:       viper.GetString("cache.server.token"),
			Advertise:   strings.TrimSuffix(viper.GetString("cache.server.advertise"), "/"),
			Peers:       normalizeBaseURLs(viper.GetStringSlice("cache.server.peers")),
			Introducers: normalizeBaseURLs(viper.GetStringSlice("cache.server.introducers")),
			Discover:    strings.ToLower(viper.GetString("cache.server.discover")),
			Timeout:     peerTimeoutFromViper(),
		},
	}
}

// peerTimeoutFromViper reads cache.server.timeout, accepting either a duration
// string ("5s") or a bare number of seconds. An unparseable value falls back to
// the default rather than failing a download over a config typo.
func peerTimeoutFromViper() time.Duration {
	raw := strings.TrimSpace(viper.GetString("cache.server.timeout"))
	if raw == "" {
		return DefaultPeerTimeout
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d
	}
	if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	fmt.Fprintf(os.Stderr, "cache: ignoring unparseable cache.server.timeout %q, using %s\n", raw, DefaultPeerTimeout)
	return DefaultPeerTimeout
}

// normalizeBaseURLs trims trailing slashes and drops empty entries so peer base
// URLs concatenate cleanly with endpoint paths.
func normalizeBaseURLs(in []string) []string {
	var out []string
	for _, u := range in {
		if u = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(u), "/")); u != "" {
			out = append(out, u)
		}
	}
	return out
}
