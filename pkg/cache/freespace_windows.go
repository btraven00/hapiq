//go:build windows

package cache

func diskFreeBytes(dir string) (int64, error) {
	return 0, nil
}
