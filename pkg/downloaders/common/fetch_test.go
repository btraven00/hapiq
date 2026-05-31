package common

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// withFastBackoff shrinks the 202 polling delays for the duration of a test so
// the suite does not actually sleep seconds between polls.
func withFastBackoff(t *testing.T) {
	t.Helper()
	initial, max, retries := acceptedInitialBackoff, acceptedMaxBackoff, acceptedMaxRetries
	acceptedInitialBackoff = time.Millisecond
	acceptedMaxBackoff = 2 * time.Millisecond
	acceptedMaxRetries = 5
	t.Cleanup(func() {
		acceptedInitialBackoff, acceptedMaxBackoff, acceptedMaxRetries = initial, max, retries
	})
}

func TestGetWaitingForReady_PollsUntilReady(t *testing.T) {
	withFastBackoff(t)

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// First two requests are 202 (not ready), then serve the body.
		if atomic.AddInt32(&calls, 1) <= 2 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		_, _ = io.WriteString(w, "payload")
	}))
	defer srv.Close()

	resp, err := getWaitingForReady(context.Background(), srv.Client(), srv.URL, nil)
	if err != nil {
		t.Fatalf("getWaitingForReady() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "payload" {
		t.Errorf("body = %q, want %q", body, "payload")
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("server calls = %d, want 3 (two 202s then 200)", got)
	}
}

func TestGetWaitingForReady_BudgetExhausted(t *testing.T) {
	withFastBackoff(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted) // always "not ready"
	}))
	defer srv.Close()

	resp, err := getWaitingForReady(context.Background(), srv.Client(), srv.URL, nil)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected an error when the server never stops returning 202")
	}
}

func TestGetWaitingForReady_ContextCancelled(t *testing.T) {
	withFastBackoff(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the first backoff completes

	resp, err := getWaitingForReady(ctx, srv.Client(), srv.URL, nil)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

// TestFetch_202IntegrationWritesFile drives the full public Fetch pipeline
// (no cache) against a server that answers 202 twice before serving the bytes,
// the way figshare's web download host behaves. It asserts Fetch transparently
// polls and ends up with the correct file content, hash, and Content-Disposition
// filename on disk.
func TestFetch_202IntegrationWritesFile(t *testing.T) {
	withFastBackoff(t)

	const body = "scfoundation norman h5ad payload"
	want := sha256.Sum256([]byte(body))
	wantHex := hex.EncodeToString(want[:])

	var gets int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&gets, 1) <= 2 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="real.h5ad"`)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.bin")
	fr, err := Fetch(context.Background(), srv.URL, dest, FetchOptions{Client: srv.Client()})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if fr.SHA256 != wantHex {
		t.Errorf("SHA256 = %q, want %q", fr.SHA256, wantHex)
	}
	if fr.N != int64(len(body)) {
		t.Errorf("N = %d, want %d", fr.N, len(body))
	}
	if fr.Filename != "real.h5ad" {
		t.Errorf("Filename = %q, want %q", fr.Filename, "real.h5ad")
	}
	if fr.Hit {
		t.Error("Hit = true, want false (cache miss)")
	}
	got, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("reading written file: %v", readErr)
	}
	if string(got) != body {
		t.Errorf("file content = %q, want %q", got, body)
	}
	if n := atomic.LoadInt32(&gets); n != 3 {
		t.Errorf("server GETs = %d, want 3 (two 202s then 200)", n)
	}
}

func TestGetWaitingForReady_NonAcceptedReturnedImmediately(t *testing.T) {
	withFastBackoff(t)

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	resp, err := getWaitingForReady(context.Background(), srv.Client(), srv.URL, nil)
	if err != nil {
		t.Fatalf("getWaitingForReady() error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server calls = %d, want 1 (no polling on non-202)", got)
	}
}
