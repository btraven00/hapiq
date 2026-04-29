package cache

import (
	"fmt"
	"strconv"
	"strings"
)

var sizeUnits = []struct {
	suffix string
	bytes  int64
}{
	{"TiB", 1 << 40},
	{"GiB", 1 << 30},
	{"MiB", 1 << 20},
	{"KiB", 1 << 10},
	{"TB", 1_000_000_000_000},
	{"GB", 1_000_000_000},
	{"MB", 1_000_000},
	{"KB", 1_000},
	{"B", 1},
}

// ParseSize parses a human-readable size string into bytes.
// Accepts B/KB/MB/GB/TB (SI) and KiB/MiB/GiB/TiB (binary), or a raw integer.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}
	upper := strings.ToUpper(s)
	for _, u := range sizeUnits {
		if strings.HasSuffix(upper, strings.ToUpper(u.suffix)) {
			numStr := strings.TrimSpace(upper[:len(upper)-len(u.suffix)])
			f, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid size %q: %w", s, err)
			}
			return int64(f * float64(u.bytes)), nil
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return n, nil
}

// ParseSizeDefault parses s, returning defaultVal on empty string or error.
func ParseSizeDefault(s string, defaultVal int64) int64 {
	if s == "" {
		return defaultVal
	}
	n, err := ParseSize(s)
	if err != nil {
		return defaultVal
	}
	return n
}

func formatSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
