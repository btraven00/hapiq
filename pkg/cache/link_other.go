//go:build !linux

package cache

import "errors"

func tryReflink(src, dst string) error {
	return errors.New("reflink not supported on this platform")
}
