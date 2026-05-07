package sra

import (
	"context"
	"crypto/md5" // #nosec G501 -- matches ENA-provided checksum format
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/btraven00/hapiq/pkg/cache"
)

// TestSRADownloadWithMD5HonorsCache verifies that SRADownloader.downloadWithMD5
// — which implements its own inline cache flow rather than delegating to
// common.Fetch — honors the blob cache across runs.
func TestSRADownloadWithMD5HonorsCache(t *testing.T) {
	const blobPath = "/vol1/fastq/SRR000001/SRR000001.fastq.gz"
	content := []byte("SRA blob content for cache contract test")
	sum := md5.Sum(content) // #nosec G401 -- test fixture md5
	expectedMD5 := hex.EncodeToString(sum[:])

	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == blobPath {
			atomic.AddInt64(&hits, 1)
		}
		_, _ = w.Write(content)
	}))
	t.Cleanup(srv.Close)

	c, err := cache.Open(cache.Config{Dir: t.TempDir(), LinkStrategy: cache.StrategyHardlink})
	if err != nil {
		t.Fatalf("cache.Open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	d := NewSRADownloader()
	ctx := cache.WithCache(context.Background(), c)
	url := srv.URL + blobPath

	dest1 := filepath.Join(t.TempDir(), "out.fastq.gz")
	fi1, err := d.downloadWithMD5(ctx, url, dest1, expectedMD5)
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

	dest2 := filepath.Join(t.TempDir(), "out.fastq.gz")
	fi2, err := d.downloadWithMD5(ctx, url, dest2, expectedMD5)
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

// TestSRADownloadWithMD5NoCache verifies the function still works without a
// cache attached to ctx.
func TestSRADownloadWithMD5NoCache(t *testing.T) {
	content := []byte("SRA blob content")
	sum := md5.Sum(content) // #nosec G401 -- test fixture md5
	expectedMD5 := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	t.Cleanup(srv.Close)

	d := NewSRADownloader()
	dest := filepath.Join(t.TempDir(), "out.fastq.gz")
	fi, err := d.downloadWithMD5(context.Background(), srv.URL+"/blob", dest, expectedMD5)
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
