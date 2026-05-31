package common

import (
	"context"
	"errors"
	"io"
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
