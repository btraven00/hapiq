//go:build linux

package cache

import (
	"os"

	"golang.org/x/sys/unix"
)

func tryReflink(src, dst string) error {
	srcF, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcF.Close()

	dstF, err := os.Create(dst)
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
