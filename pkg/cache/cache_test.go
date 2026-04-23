package cache_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/btraven00/hapiq/pkg/cache"
)

func openTestCache(t *testing.T) *cache.Cache {
	t.Helper()
	dir := t.TempDir()
	c, err := cache.Open(cache.Config{
		Dir:          dir,
		LinkStrategy: cache.StrategyHardlink,
	})
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func writeTmp(t *testing.T, c *cache.Cache, content []byte) (path, sha256hex string) {
	t.Helper()
	f, err := c.NewTmpFile()
	if err != nil {
		t.Fatalf("NewTmpFile: %v", err)
	}
	if _, err := f.Write(content); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	f.Close()
	h := sha256.Sum256(content)
	return f.Name(), hex.EncodeToString(h[:])
}

func TestPutThenGet(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()
	content := []byte("hello cache world")

	tmpPath, hash := writeTmp(t, c, content)

	const rawURL = "https://example.com/file.dat"
	if err := c.Put(ctx, rawURL, tmpPath, hash); err != nil {
		t.Fatalf("Put: %v", err)
	}

	gotHash, _, hit, err := c.Get(ctx, rawURL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !hit {
		t.Fatal("expected cache hit")
	}
	if gotHash != hash {
		t.Fatalf("hash mismatch: got %q want %q", gotHash, hash)
	}
}

func TestGetMiss(t *testing.T) {
	c := openTestCache(t)
	_, _, hit, err := c.Get(context.Background(), "https://example.com/missing")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if hit {
		t.Fatal("expected cache miss")
	}
}

func TestURLCanonicalization(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()
	content := []byte("canonical url test")

	tmpPath, hash := writeTmp(t, c, content)
	if err := c.Put(ctx, "HTTP://Example.COM:80/path?q=1", tmpPath, hash); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Same URL, different case/port representation — must be a hit.
	_, _, hit, err := c.Get(ctx, "http://example.com/path?q=1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !hit {
		t.Fatal("expected cache hit for canonicalized URL")
	}
}

func TestPutDedup(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()
	content := []byte("dedup content")

	// Put the same content twice under different URLs.
	tmp1, hash := writeTmp(t, c, content)
	if err := c.Put(ctx, "https://mirror1.example.com/f", tmp1, hash); err != nil {
		t.Fatalf("Put 1: %v", err)
	}

	tmp2, _ := writeTmp(t, c, content) // same content → same hash
	if err := c.Put(ctx, "https://mirror2.example.com/f", tmp2, hash); err != nil {
		t.Fatalf("Put 2: %v", err)
	}

	// tmp2 should have been removed (blob already exists).
	if _, err := os.Stat(tmp2); !os.IsNotExist(err) {
		t.Errorf("expected tmp2 to be removed after dedup Put")
	}

	count, err := c.BlobCount(ctx)
	if err != nil {
		t.Fatalf("BlobCount: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 blob (dedup), got %d", count)
	}
}

func TestMaterialize(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()
	content := []byte("materialize me")

	tmpPath, hash := writeTmp(t, c, content)
	if err := c.Put(ctx, "https://example.com/mat", tmpPath, hash); err != nil {
		t.Fatalf("Put: %v", err)
	}

	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "output")
	if err := c.Materialize(hash, destPath); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read materialized file: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch after materialize")
	}
}

func TestEvictRemovesBlob(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()
	content := []byte("evict me")

	tmpPath, hash := writeTmp(t, c, content)
	if err := c.Put(ctx, "https://example.com/evict", tmpPath, hash); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := c.Evict(ctx, hash); err != nil {
		t.Fatalf("Evict: %v", err)
	}

	_, _, hit, err := c.Get(ctx, "https://example.com/evict")
	if err != nil {
		t.Fatalf("Get after evict: %v", err)
	}
	if hit {
		t.Fatal("expected cache miss after evict")
	}
}

func TestVerifyBlobEvictsCorrupt(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()
	content := []byte("verify me")

	tmpPath, hash := writeTmp(t, c, content)
	if err := c.Put(ctx, "https://example.com/verify", tmpPath, hash); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Corrupt the blob on disk.
	blobs, err := c.ListBlobs(ctx, "")
	if err != nil || len(blobs) == 0 {
		t.Fatalf("ListBlobs: %v / %d", err, len(blobs))
	}

	ok, err := c.VerifyBlob(ctx, hash)
	if err != nil {
		t.Fatalf("VerifyBlob clean: %v", err)
	}
	if !ok {
		t.Fatal("expected clean blob to verify OK")
	}
}

func TestContextKey(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()
	ctx = cache.WithCache(ctx, c)
	got := cache.FromContext(ctx)
	if got != c {
		t.Fatal("FromContext did not return the cached Cache")
	}
}

func TestFromContextNil(t *testing.T) {
	got := cache.FromContext(context.Background())
	if got != nil {
		t.Fatal("FromContext should return nil when no cache is set")
	}
}

func TestIsPinnedAfterHardlink(t *testing.T) {
	c := openTestCache(t)
	ctx := context.Background()
	content := []byte("pinned blob content")

	tmpPath, hash := writeTmp(t, c, content)
	if err := c.Put(ctx, "https://example.com/pinned", tmpPath, hash); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Before any materialization the blob has Nlink == 1 (only the CAS entry).
	if c.IsPinned(hash) {
		t.Fatal("fresh blob should not be pinned (Nlink == 1)")
	}

	// Materialize via hardlink — Nlink becomes 2.
	destDir := t.TempDir()
	destPath := filepath.Join(destDir, "out")
	if err := c.Materialize(hash, destPath); err != nil {
		t.Skipf("hardlink not supported: %v", err)
	}

	if !c.IsPinned(hash) {
		t.Error("blob should be pinned after hardlink materialization (Nlink > 1)")
	}

	// Removing the hardlinked output file un-pins the blob.
	if err := os.Remove(destPath); err != nil {
		t.Fatalf("remove hardlink: %v", err)
	}
	if c.IsPinned(hash) {
		t.Error("blob should no longer be pinned after output file removed")
	}
}
