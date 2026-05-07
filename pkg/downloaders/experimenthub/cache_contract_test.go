package experimenthub

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

// TestExperimentHubDownloadFileHonorsCache verifies that the inline cache
// flow added to ExperimentHubDownloader.downloadFile honors the blob cache
// across runs: the first call hits the network and populates the cache; the
// second call with the same URL is materialized from the cache without a
// network round-trip.
func TestExperimentHubDownloadFileHonorsCache(t *testing.T) {
	const blobPath = "/fetch/12345"
	content := []byte("ExperimentHub blob content for cache contract test")

	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == blobPath {
			atomic.AddInt64(&hits, 1)
		}
		w.Header().Set("Content-Disposition", `attachment; filename="resource.rds"`)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(content)
	}))
	t.Cleanup(srv.Close)

	c, err := cache.Open(cache.Config{Dir: t.TempDir(), LinkStrategy: cache.StrategyHardlink})
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	d := NewExperimentHubDownloader()
	ctx := cache.WithCache(context.Background(), c)
	url := srv.URL + blobPath

	outDir1 := t.TempDir()
	target1 := filepath.Join(outDir1, "fallback.bin")
	fi1, name1, err := d.downloadFile(ctx, url, target1, "fallback.bin")
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	// Run 1 follows Content-Disposition rename to "resource.rds".
	if name1 != "resource.rds" {
		t.Fatalf("run 1: expected name resource.rds, got %q", name1)
	}
	if got, _ := os.ReadFile(fi1.Path); string(got) != string(content) {
		t.Fatal("run 1: content mismatch")
	}
	if h := atomic.LoadInt64(&hits); h != 1 {
		t.Fatalf("run 1: expected 1 server hit, got %d", h)
	}

	outDir2 := t.TempDir()
	target2 := filepath.Join(outDir2, "fallback.bin")
	fi2, _, err := d.downloadFile(ctx, url, target2, "fallback.bin")
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if !fi2.CacheHit {
		t.Fatal("run 2: expected CacheHit=true")
	}
	// On a cache hit the caller's targetPath is authoritative (Content-Disposition
	// is not consulted).
	if fi2.Path != target2 {
		t.Fatalf("run 2: expected path %s, got %s", target2, fi2.Path)
	}
	if got, _ := os.ReadFile(fi2.Path); string(got) != string(content) {
		t.Fatal("run 2: content mismatch")
	}
	if h := atomic.LoadInt64(&hits); h != 1 {
		t.Fatalf("run 2: cache hit must not contact server; total hits=%d", h)
	}
}

// TestExperimentHubDownloadFileNoCache verifies the function works without a
// cache attached to ctx.
func TestExperimentHubDownloadFileNoCache(t *testing.T) {
	content := []byte("ExperimentHub blob content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	t.Cleanup(srv.Close)

	d := NewExperimentHubDownloader()
	target := filepath.Join(t.TempDir(), "out.bin")
	fi, _, err := d.downloadFile(context.Background(), srv.URL+"/fetch/1", target, "out.bin")
	if err != nil {
		t.Fatalf("no-cache: %v", err)
	}
	if fi.CacheHit {
		t.Fatal("no-cache: must not report CacheHit")
	}
	if got, _ := os.ReadFile(fi.Path); string(got) != string(content) {
		t.Fatal("no-cache: content mismatch")
	}
}
