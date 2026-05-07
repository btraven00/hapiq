package geo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/btraven00/hapiq/pkg/cache"
)

// TestGEODownloadFileHonorsCache verifies that the inline cache flow inside
// GEODownloader.downloadFileWithProgress (which does not delegate to
// common.Fetch) honors the blob cache: the first call hits the network and
// populates the cache; the second call with the same URL is served from the
// cache without a network round-trip.
//
// This is the behavioral counterpart to the static cache-compliance lint for
// the GEO downloader's inline-cache pattern.
func TestGEODownloadFileHonorsCache(t *testing.T) {
	const blobPath = "/series/file.txt.gz"
	content := []byte("GEO blob content for cache contract test")

	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == blobPath {
			atomic.AddInt64(&hits, 1)
		}
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(content)
	}))
	t.Cleanup(srv.Close)

	c, err := cache.Open(cache.Config{Dir: t.TempDir(), LinkStrategy: cache.StrategyHardlink})
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	d := NewGEODownloader()
	ctx := cache.WithCache(context.Background(), c)
	url := srv.URL + blobPath

	dest1 := filepath.Join(t.TempDir(), "out.bin")
	fi1, err := d.downloadFileWithProgress(ctx, url, dest1, "out.bin", -1, nil)
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if fi1.CacheHit {
		t.Fatal("run 1: expected cache miss")
	}
	if got, _ := os.ReadFile(dest1); string(got) != string(content) {
		t.Fatal("run 1: content mismatch")
	}
	if h := atomic.LoadInt64(&hits); h != 1 {
		t.Fatalf("run 1: expected 1 server hit, got %d", h)
	}

	dest2 := filepath.Join(t.TempDir(), "out.bin")
	fi2, err := d.downloadFileWithProgress(ctx, url, dest2, "out.bin", -1, nil)
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if !fi2.CacheHit {
		t.Fatal("run 2: expected cache hit")
	}
	if got, _ := os.ReadFile(dest2); string(got) != string(content) {
		t.Fatal("run 2: content mismatch")
	}
	if h := atomic.LoadInt64(&hits); h != 1 {
		t.Fatalf("run 2: cache hit must not contact server; total hits=%d", h)
	}
}

// TestGEODownloadFileNoCache verifies the inline path still works when no
// cache is attached to ctx (cache is opt-in).
func TestGEODownloadFileNoCache(t *testing.T) {
	const blobPath = "/series/file.txt.gz"
	content := []byte("GEO blob content")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	t.Cleanup(srv.Close)

	d := NewGEODownloader()
	dest := filepath.Join(t.TempDir(), "out.bin")
	fi, err := d.downloadFileWithProgress(context.Background(), srv.URL+blobPath, dest, "out.bin", -1, nil)
	if err != nil {
		t.Fatalf("no-cache: %v", err)
	}
	if fi.CacheHit {
		t.Fatal("no-cache: must not report CacheHit")
	}
	if got, _ := os.ReadFile(dest); string(got) != string(content) {
		t.Fatal("no-cache: content mismatch")
	}
}
