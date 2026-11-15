package common

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/btraven00/hapiq/pkg/cache"
)

// A verified peer download that the local cache refuses (quota full) is kept
// as the destination, not downloaded again from the origin.
func TestPeerFetchSurvivesFullCache(t *testing.T) {
	content := bytes.Repeat([]byte("dataset "), 1000)
	var originHits, blobHits atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		originHits.Add(1)
		_, _ = w.Write(content)
	}))
	t.Cleanup(origin.Close)

	open := func(maxSize int64) *cache.Cache {
		c, err := cache.Open(cache.Config{Dir: t.TempDir(), MaxSize: maxSize})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	}

	// Node A holds the file and serves it.
	cacheA := open(0)
	if _, err := Fetch(cache.WithCache(context.Background(), cacheA), origin.URL+"/f", filepath.Join(t.TempDir(), "f"), FetchOptions{}); err != nil {
		t.Fatal(err)
	}
	inner := cache.NewServer(cacheA, cache.ServerConfig{}).Handler()
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/blob/") {
			blobHits.Add(1)
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(peer.Close)

	// Node B's cache has no room for it.
	ctx := cache.WithCache(context.Background(), open(1))
	peers := cache.NewPeers(cache.ServerConfig{Peers: []string{peer.URL}})
	t.Cleanup(peers.Close)
	ctx = cache.WithPeers(ctx, peers)

	dest := filepath.Join(t.TempDir(), "out")
	res, err := Fetch(ctx, origin.URL+"/f", dest, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); !bytes.Equal(got, content) {
		t.Errorf("dest holds %d bytes, want the %d of the file", len(got), len(content))
	}
	if !res.Hit || originHits.Load() != 1 || blobHits.Load() != 1 {
		t.Errorf("hit=%v, origin fetched %d times (want 1, node A's), peer blob %d (want 1)", res.Hit, originHits.Load(), blobHits.Load())
	}
}
