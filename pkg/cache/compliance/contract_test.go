package compliance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/btraven00/hapiq/pkg/cache"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
)

// openTestCache creates a temporary blob cache rooted at t.TempDir().
func openTestCache(t *testing.T) *cache.Cache {
	t.Helper()
	c, err := cache.Open(cache.Config{
		Dir:          t.TempDir(),
		LinkStrategy: cache.StrategyHardlink,
	})
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// hitCounter wraps an http.Handler and tracks request counts per URL path so
// tests can assert "no network round-trip on cache hit".
type hitCounter struct {
	handler http.Handler
	hits    map[string]*int64
}

func newHitCounter(h http.Handler, paths ...string) *hitCounter {
	hc := &hitCounter{handler: h, hits: make(map[string]*int64, len(paths))}
	for _, p := range paths {
		var n int64
		hc.hits[p] = &n
	}
	return hc
}

func (h *hitCounter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if counter, ok := h.hits[r.URL.Path]; ok {
		atomic.AddInt64(counter, 1)
	}
	h.handler.ServeHTTP(w, r)
}

func (h *hitCounter) count(path string) int64 {
	if c, ok := h.hits[path]; ok {
		return atomic.LoadInt64(c)
	}
	return 0
}

// blobHandler returns a handler that serves `content` at any path.
func blobHandler(content []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(content)
	})
}

// TestCommonFetchCacheContract verifies that pkg/downloaders/common.Fetch —
// the sanctioned cache-aware primitive used by 8 of the 10 main downloaders —
// honors the cache across run/rerun and falls back gracefully when no cache
// is attached to the context.
//
// This test is the behavioral counterpart to the static lint: the lint
// guarantees every blob-streaming function references either common.Fetch or
// cache.FromContext, and this test verifies common.Fetch's contract. Since
// most downloaders delegate here, exercising this primitive transitively
// covers them.
func TestCommonFetchCacheContract(t *testing.T) {
	const blobPath = "/dataset.bin"
	content := []byte("the quick brown fox jumps over the lazy dog")

	hc := newHitCounter(blobHandler(content), blobPath)
	srv := httptest.NewServer(hc)
	t.Cleanup(srv.Close)

	url := srv.URL + blobPath

	t.Run("with cache: miss then hit", func(t *testing.T) {
		c := openTestCache(t)
		ctx := cache.WithCache(context.Background(), c)

		dest1 := filepath.Join(t.TempDir(), "out.bin")
		res1, err := common.Fetch(ctx, url, dest1, common.FetchOptions{})
		if err != nil {
			t.Fatalf("run 1: %v", err)
		}
		if res1.Hit {
			t.Fatal("run 1: expected cache miss")
		}
		if got := mustRead(t, dest1); string(got) != string(content) {
			t.Fatalf("run 1: file content mismatch")
		}

		hitsBefore := hc.count(blobPath)
		if hitsBefore != 1 {
			t.Fatalf("run 1: expected 1 server hit, got %d", hitsBefore)
		}

		// Run 2: same URL, fresh destination, same cache → must be a hit and
		// must not touch the network.
		dest2 := filepath.Join(t.TempDir(), "out.bin")
		res2, err := common.Fetch(ctx, url, dest2, common.FetchOptions{})
		if err != nil {
			t.Fatalf("run 2: %v", err)
		}
		if !res2.Hit {
			t.Fatal("run 2: expected cache hit")
		}
		if got := mustRead(t, dest2); string(got) != string(content) {
			t.Fatalf("run 2: file content mismatch")
		}
		if hitsAfter := hc.count(blobPath); hitsAfter != hitsBefore {
			t.Fatalf("run 2: cache hit must not contact server; before=%d after=%d", hitsBefore, hitsAfter)
		}

		// Blob landed in the CAS.
		if n, err := c.BlobCount(ctx); err != nil || n != 1 {
			t.Fatalf("expected 1 blob in cache, got n=%d err=%v", n, err)
		}
	})

	t.Run("no cache: still works", func(t *testing.T) {
		// Fresh server so request count is independent of the previous run.
		hc2 := newHitCounter(blobHandler(content), blobPath)
		srv2 := httptest.NewServer(hc2)
		t.Cleanup(srv2.Close)

		dest := filepath.Join(t.TempDir(), "out.bin")
		res, err := common.Fetch(context.Background(), srv2.URL+blobPath, dest, common.FetchOptions{})
		if err != nil {
			t.Fatalf("no-cache fetch: %v", err)
		}
		if res.Hit {
			t.Fatal("no-cache fetch: Hit must be false")
		}
		if got := mustRead(t, dest); string(got) != string(content) {
			t.Fatal("no-cache fetch: content mismatch")
		}
	})
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304 -- test path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}
