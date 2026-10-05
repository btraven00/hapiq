package version

import (
	"fmt"
	"runtime/debug"
)

// Version is the release version, stamped at build time with
// -ldflags "-X github.com/btraven00/hapiq/internal/version.Version=0.1.0".
// Empty for plain `go build`.
var Version string

// String returns Version when stamped, otherwise a string derived from VCS
// build info. Examples:
//
//	0.1.0
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
