package version

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// Version is the release version, stamped at build time with
// -ldflags "-X github.com/btraven00/hapiq/internal/version.Version=0.1.0".
// Empty for plain `go build`; String() then falls back to build info.
var Version string

// String returns Version when stamped, otherwise a string derived from VCS
// build info. Examples:
//
//	0.1.0
//	0.1.1+dirty  (plain go build on a tag, Go 1.24+)
//	devel (abc1234, 2026-05-05T10:00:00Z)
//	devel (abc1234+dirty, 2026-05-05T10:00:00Z)
//	(unknown)
func String() string {
	if Version != "" {
		return Version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(unknown)"
	}

	// Go 1.24+ stamps the main module version from VCS tags on plain
	// `go build` (v0.1.1, v0.1.1+dirty, or a pseudo-version off-tag).
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return strings.TrimPrefix(v, "v")
	}

	var rev, ts string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 7 {
				rev = s.Value[:7]
			} else {
				rev = s.Value
			}
		case "vcs.time":
			ts = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}

	if rev == "" {
		return "devel"
	}
	if dirty {
		rev += "+dirty"
	}
	if ts != "" {
		return fmt.Sprintf("devel (%s, %s)", rev, ts)
	}
	return fmt.Sprintf("devel (%s)", rev)
}
