//go:build linux

package cache

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func tryReflink(src, dst string) error {
	srcF, err := os.Open(filepath.Clean(src)) // #nosec G304 -- internal cache blob path
	if err != nil {
		return err
	}
	defer srcF.Close()

	dstF, err := os.Create(filepath.Clean(dst)) // #nosec G304 -- caller-controlled destination
	if err != nil {
		return err
	}
	defer dstF.Close()

	if err := unix.IoctlFileClone(int(dstF.Fd()), int(srcF.Fd())); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}
