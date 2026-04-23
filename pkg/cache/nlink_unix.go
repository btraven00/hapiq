//go:build !windows

package cache

import (
	"os"
	"syscall"
)

// blobNlink returns the hard-link count of path, or 1 on error.
func blobNlink(path string) uint64 {
	info, err := os.Stat(path)
	if err != nil {
		return 1
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 1
	}
	return uint64(st.Nlink)
}
