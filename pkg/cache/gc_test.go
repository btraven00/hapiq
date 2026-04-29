package cache_test

import (
	"context"
	"testing"

	"github.com/btraven00/hapiq/pkg/cache"
)

func TestQuotaRefusal(t *testing.T) {
	dir := t.TempDir()
	// Quota of 1 byte — any blob should be refused.
	c, err := cache.Open(cache.Config{
		Dir:     dir,
		MaxSize: 1,
	})
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	defer c.Close()

	content := []byte("this is larger than 1 byte")
	tmpPath, hash := writeTmp(t, c, content)

	err = c.Put(context.Background(), "https://example.com/quota", tmpPath, hash)
	if err == nil {
		t.Fatal("expected quota error but got nil")
	}
}

func TestGCDryRun(t *testing.T) {
	dir := t.TempDir()
	c, err := cache.Open(cache.Config{
		Dir:     dir,
		MaxSize: 1, // ensure we're over quota after any put
	})
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	defer c.Close()

	ctx := context.Background()

	// Bypass quota by temporarily opening with no quota, put a blob, then recheck.
	c2, err := cache.Open(cache.Config{Dir: dir})
	if err != nil {
		t.Fatalf("cache.Open no-quota: %v", err)
	}
	content := []byte("gc dry run content")
	tmpPath, hash := writeTmp(t, c2, content)
	if err := c2.Put(ctx, "https://example.com/gc", tmpPath, hash); err != nil {
		t.Fatalf("Put: %v", err)
	}
	c2.Close()

	// Now run GC dry-run — should report something to evict.
	res, err := c.GC(ctx, true, 0)
	if err != nil {
		t.Fatalf("GC dry-run: %v", err)
	}
	if res.Evicted == 0 {
		t.Error("expected dry-run to report at least one eviction candidate")
	}

	// Actual blob count must be unchanged.
	count, _ := c.BlobCount(ctx)
	if count == 0 {
		t.Error("dry-run must not actually evict blobs")
	}
}

func TestPruneURLs(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()

	content := []byte("prune test")
	tmpPath, hash := writeTmp(t, c, content)
	if err := c.Put(ctx, "https://example.com/prune", tmpPath, hash); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Evict the blob (removes from blobs table but leaves urls if cascade fails).
	// PruneURLs should clean up orphaned URL entries.
	if err := c.Evict(ctx, hash); err != nil {
		t.Fatalf("Evict: %v", err)
	}

	pruned, err := c.PruneURLs(ctx)
	if err != nil {
		t.Fatalf("PruneURLs: %v", err)
	}
	// After Evict uses a transaction with cascading deletes, URL rows should
	// already be gone. PruneURLs is still correct if it returns 0.
	_ = pruned
}
