//go:build !windows

package common

import (
	"math"
	"syscall"
)

// getFreeSpace returns available disk space in bytes.
func (dc *DirectoryChecker) getFreeSpace(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	bavail := stat.Bavail
	bsize := stat.Bsize
	if bsize < 0 {
		return 0, nil
	}
	bsizeU := uint64(bsize)
	if bsizeU != 0 && bavail > math.MaxInt64/bsizeU {
		return math.MaxInt64, nil
	}
	free := bavail * bsizeU
	if free > math.MaxInt64 {
		return math.MaxInt64, nil
	}
	return int64(free), nil
}
