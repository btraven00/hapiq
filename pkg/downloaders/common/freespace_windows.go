//go:build windows

package common

import (
	"golang.org/x/sys/windows"
)

// getFreeSpace returns available disk space in bytes.
func (dc *DirectoryChecker) getFreeSpace(path string) (int64, error) {
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	err := windows.GetDiskFreeSpaceEx(
		windows.StringToUTF16Ptr(path),
		&freeBytesAvailable,
		&totalBytes,
		&totalFreeBytes,
	)
	if err != nil {
		return 0, err
	}
	return int64(freeBytesAvailable), nil
}
