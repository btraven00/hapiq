//go:build !windows

package cache

import (
	"math"
	"syscall"
)

func diskFreeBytes(dir string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	bavail := st.Bavail
	bsize := st.Bsize
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
