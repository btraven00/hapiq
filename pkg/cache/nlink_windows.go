//go:build windows

package cache

// blobNlink always returns 1 on Windows; hardlink count is not checked.
func blobNlink(path string) uint64 { return 1 }
