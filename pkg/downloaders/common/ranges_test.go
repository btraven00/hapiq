package common

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// smallRanges shrinks the range tunables for one test: 1000-byte chunks,
// 4 workers, 3 tries, the given backoff.
func smallRanges(t *testing.T, backoff time.Duration) {
	t.Helper()
	c, w, r, b := rangeChunk, rangeWorkers, rangeRetries, rangeBackoff
	t.Cleanup(func() { rangeChunk, rangeWorkers, rangeRetries, rangeBackoff = c, w, r, b })
	rangeChunk, rangeWorkers, rangeRetries, rangeBackoff = 1000, 4, 3, backoff
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// rangeServer serves data with Range support. hook runs first with the
// Range header and how often that range was asked for; returning true means
// the hook has answered.
func rangeServer(t *testing.T, data []byte, hook func(w http.ResponseWriter, r *http.Request, rg string, n int) bool) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	seen := map[string]int{}
	modTime := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rg := r.Header.Get("Range")
		mu.Lock()
		seen[rg]++
		n := seen[rg]
		mu.Unlock()
		if hook != nil && hook(w, r, rg, n) {
			return
		}
		w.Header().Set("ETag", `"v1"`)
		http.ServeContent(w, r, "f.bin", modTime, bytes.NewReader(data))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fetchInto(t *testing.T, ctx context.Context, url string) ([]byte, FetchResult, error) {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "f.bin")
	res, err := Fetch(ctx, url, dest, FetchOptions{})
	got, _ := os.ReadFile(dest)
	return got, res, err
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// A ranged download reassembles the file exactly, even when one chunk is
// refused with 503 and another breaks off midway on its first try.
func TestFetch_ParallelRanges(t *testing.T) {
	smallRanges(t, time.Millisecond)
	data := randomBytes(10_500)
	srv := rangeServer(t, data, func(w http.ResponseWriter, _ *http.Request, rg string, n int) bool {
		switch {
		case rg == "bytes=3000-3999" && n == 1:
			w.WriteHeader(http.StatusServiceUnavailable)
			return true
		case rg == "bytes=5000-5999" && n == 1:
			// Promise the whole chunk, send half, then drop the connection.
			w.Header().Set("Content-Range", "bytes 5000-5999/10500")
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(data[5000:5500])
			panic(http.ErrAbortHandler)
		}
		return false
	})

	got, res, err := fetchInto(t, context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) || res.N != int64(len(data)) || res.SHA256 != sha(data) {
		t.Fatalf("reassembled file differs: N=%d sha=%s", res.N, res.SHA256)
	}
}

// A probe whose body ends early without an error is fetched again as an
// ordinary chunk.
func TestFetch_ShortProbeRefetched(t *testing.T) {
	smallRanges(t, time.Millisecond)
	data := randomBytes(2500)
	srv := rangeServer(t, data, func(w http.ResponseWriter, _ *http.Request, rg string, n int) bool {
		if rg != "bytes=0-999" || n != 1 {
			return false
		}
		// No Content-Length: the body is chunked and ends cleanly after 10 bytes.
		w.Header().Set("Content-Range", "bytes 0-999/2500")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[:10])
		return true
	})

	got, _, err := fetchInto(t, context.Background(), srv.URL)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("err=%v, file equal=%v", err, bytes.Equal(got, data))
	}
}

// An empty file cannot satisfy bytes=0-…; the plain GET that follows can.
func TestFetch_EmptyFileFallsBack(t *testing.T) {
	smallRanges(t, time.Millisecond)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		}
	}))
	defer srv.Close()

	got, res, err := fetchInto(t, context.Background(), srv.URL)
	if err != nil || len(got) != 0 || res.N != 0 || res.SHA256 != sha(nil) {
		t.Fatalf("err=%v len=%d N=%d", err, len(got), res.N)
	}
}

// The probe request itself failing is reported.
func TestFetch_ProbeFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := fetchInto(t, ctx, "http://127.0.0.1:1/f.bin"); err == nil {
		t.Fatal("want an error for a cancelled request")
	}
}

// A 206 for some other range than the one asked for is refused.
func TestFetch_ProbeWrongContentRange(t *testing.T) {
	smallRanges(t, time.Millisecond)
	srv := rangeServer(t, nil, func(w http.ResponseWriter, _ *http.Request, _ string, _ int) bool {
		w.Header().Set("Content-Range", "bytes 5-9/100")
		w.WriteHeader(http.StatusPartialContent)
		return true
	})
	if _, _, err := fetchInto(t, context.Background(), srv.URL); err == nil || !strings.Contains(err.Error(), "unexpected Content-Range") {
		t.Fatalf("err=%v", err)
	}
}

// A file that changes between chunks fails If-Range, so the server answers
// 200 instead of 206 and the download stops rather than mixing versions.
func TestFetch_FileChangedMidDownload(t *testing.T) {
	smallRanges(t, time.Millisecond)
	data := randomBytes(2500)
	srv := rangeServer(t, data, func(w http.ResponseWriter, r *http.Request, rg string, _ int) bool {
		if rg == "bytes=0-999" {
			return false // the probe sees version "v1"
		}
		if r.Header.Get("If-Range") != `"v1"` {
			t.Errorf("chunk %s sent If-Range %q, want \"v1\"", rg, r.Header.Get("If-Range"))
		}
		w.Header().Set("ETag", `"v2"`)
		http.ServeContent(w, r, "f.bin", time.Time{}, bytes.NewReader(data))
		return true
	})
	if _, _, err := fetchInto(t, context.Background(), srv.URL); err == nil || !strings.Contains(err.Error(), "did the file change") {
		t.Fatalf("err=%v", err)
	}
}

// A chunk that keeps failing gives up after rangeRetries tries.
func TestFetch_ChunkRetriesExhausted(t *testing.T) {
	smallRanges(t, time.Millisecond)
	var tries int
	srv := rangeServer(t, randomBytes(2500), func(w http.ResponseWriter, _ *http.Request, rg string, n int) bool {
		if rg != "bytes=1000-1999" {
			return false
		}
		tries = n
		w.WriteHeader(http.StatusServiceUnavailable)
		return true
	})
	_, _, err := fetchInto(t, context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "HTTP 503") || tries != 3 {
		t.Fatalf("err=%v tries=%d", err, tries)
	}
}

// Cancelling while a chunk waits to retry returns at once, not after the backoff.
func TestFetch_CancelDuringBackoff(t *testing.T) {
	smallRanges(t, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := rangeServer(t, randomBytes(1500), func(w http.ResponseWriter, _ *http.Request, rg string, _ int) bool {
		if rg != "bytes=1000-1499" {
			return false
		}
		cancel()
		w.WriteHeader(http.StatusServiceUnavailable)
		return true
	})
	if _, _, err := fetchInto(t, ctx, srv.URL); err == nil {
		t.Fatal("want an error after cancel")
	}
}

// A file that cannot be read back for hashing is an error, not a bad checksum.
func TestFetchToFile_HashReadFails(t *testing.T) {
	smallRanges(t, time.Millisecond)
	srv := rangeServer(t, randomBytes(500), nil)
	f, err := os.OpenFile(filepath.Join(t.TempDir(), "f.bin"), os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, _, _, _, err := fetchToFile(context.Background(), http.DefaultClient, srv.URL, nil, f); err == nil || !strings.Contains(err.Error(), "hash:") {
		t.Fatalf("err=%v", err)
	}
}

// directFetch (Fetch's path when the cache refuses a blob) goes through the
// range path too.
func TestDirectFetch(t *testing.T) {
	smallRanges(t, time.Millisecond)
	data := randomBytes(2500)
	srv := rangeServer(t, data, nil)
	dest := filepath.Join(t.TempDir(), "f.bin")
	res, err := directFetch(context.Background(), http.DefaultClient, srv.URL, dest, nil)
	got, _ := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, data) || res.SHA256 != sha(data) {
		t.Fatalf("err=%v sha=%s", err, res.SHA256)
	}
}

// WithConnections caps how many chunk requests are in flight at once.
func TestFetch_WithConnectionsCaps(t *testing.T) {
	smallRanges(t, time.Millisecond) // 4 workers unless ctx says otherwise
	data := randomBytes(8000)
	var mu sync.Mutex
	inFlight, peak := 0, 0
	srv := rangeServer(t, data, func(_ http.ResponseWriter, _ *http.Request, _ string, _ int) bool {
		mu.Lock()
		inFlight++
		peak = max(peak, inFlight)
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		return false
	})

	got, _, err := fetchInto(t, WithConnections(context.Background(), 1), srv.URL)
	if err != nil || !bytes.Equal(got, data) || peak != 1 {
		t.Fatalf("err=%v equal=%v peak=%d, want peak 1", err, bytes.Equal(got, data), peak)
	}
}
