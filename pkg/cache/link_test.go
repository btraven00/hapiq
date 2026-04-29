//go:build integration

package cache_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/btraven00/hapiq/pkg/cache"
)

func TestMaterializeHardlink(t *testing.T) {
	c, hash := putSyntheticBlob(t, cache.StrategyHardlink)
	dest := filepath.Join(t.TempDir(), "out")

	if err := c.Materialize(hash, dest); err != nil {
		t.Skipf("hardlink not supported: %v", err)
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat materialized file: %v", err)
	}
	// A hardlink increases the link count to ≥ 2.
	// os.FileInfo on Linux exposes Sys() which has Nlink.
	sys, ok := info.Sys().(*syscallStat)
	if !ok {
		t.Skip("cannot read Nlink on this platform")
	}
	if sys.Nlink < 2 {
		t.Errorf("expected link count ≥ 2 for hardlink, got %d", sys.Nlink)
	}
}

func TestMaterializeSymlinkFallback(t *testing.T) {
	c, hash := putSyntheticBlob(t, cache.StrategySymlink)
	dest := filepath.Join(t.TempDir(), "symout")

	if err := c.Materialize(hash, dest); err != nil {
		t.Fatalf("Materialize with symlink: %v", err)
	}

	if _, err := os.Lstat(dest); err != nil {
		t.Fatalf("lstat symlink: %v", err)
	}
	target, err := os.Readlink(dest)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if !filepath.IsAbs(target) {
		t.Errorf("symlink target should be absolute, got %q", target)
	}
}

func TestMaterializeCopy(t *testing.T) {
	c, hash := putSyntheticBlob(t, cache.StrategyCopy)
	dest := filepath.Join(t.TempDir(), "copy")

	if err := c.Materialize(hash, dest); err != nil {
		t.Fatalf("Materialize with copy: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}
	if string(got) != "integration test blob" {
		t.Errorf("copy content mismatch: %q", string(got))
	}
}

func putSyntheticBlob(t *testing.T, strategy cache.Strategy) (*cache.Cache, string) {
	t.Helper()
	dir := t.TempDir()
	c, err := cache.Open(cache.Config{Dir: dir, LinkStrategy: strategy})
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	content := []byte("integration test blob")
	tmp, hash := writeTmp(t, c, content)
	if err := c.Put(context.Background(), "https://example.com/integ", tmp, hash); err != nil {
		t.Fatalf("Put: %v", err)
	}
	return c, hash
}

// syscallStat is the concrete type returned by FileInfo.Sys() on Linux.
// Defined inline to avoid importing syscall in tests without the build tag.
type syscallStat struct {
	Nlink uint64
}
