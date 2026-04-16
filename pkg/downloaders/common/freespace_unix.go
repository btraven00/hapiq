//go:build !windows

package common

import "syscall"

// getFreeSpace returns available disk space in bytes.
func (dc *DirectoryChecker) getFreeSpace(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}
