package cache_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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

func TestOrphans(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()

	// Indexed blob: never an orphan.
	tmp, sha := writeTmp(t, c, []byte("indexed"))
	if err := c.Put(ctx, "https://example.org/indexed", tmp, sha); err != nil {
		t.Fatalf("Put: %v", err)
	}

	old := time.Now().Add(-48 * time.Hour)
	mkBlob := func(name string, mtime time.Time) string {
		p := filepath.Join(c.Dir(), "blobs", "sha256", name[:2], name)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		return p
	}
	stale := mkBlob("aa"+strings.Repeat("0", 62), old)
	linked := mkBlob("bb"+strings.Repeat("0", 62), old)
	mkBlob("cc"+strings.Repeat("0", 62), time.Now()) // within grace: may be mid-Put
	if err := os.Link(linked, filepath.Join(t.TempDir(), "output")); err != nil {
		t.Skipf("hardlinks unsupported: %v", err)
	}

	orphans, err := c.Orphans(ctx)
	if err != nil {
		t.Fatalf("Orphans: %v", err)
	}
	got := map[string]cache.Orphan{}
	for _, o := range orphans {
		got[o.Path] = o
	}
	if len(got) != 2 || got[stale].Path == "" || got[linked].Path == "" {
		t.Fatalf("Orphans = %v, want only %s and %s", orphans, stale, linked)
	}

	if ok, err := c.RemoveOrphan(ctx, got[stale]); err != nil || !ok {
		t.Errorf("RemoveOrphan(stale) = %v, %v; want true", ok, err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale orphan still on disk")
	}
	if ok, _ := c.RemoveOrphan(ctx, got[linked]); ok && runtime.GOOS != "windows" {
		t.Errorf("RemoveOrphan removed a hardlinked blob")
	}
	if _, _, hit, _ := c.Get(ctx, "https://example.org/indexed"); !hit {
		t.Errorf("indexed blob lost")
	}
}
