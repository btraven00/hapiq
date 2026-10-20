package common

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A body that keeps trickling in for longer than the timeout must complete;
// one that stalls for longer than the timeout must fail with a clear error.
func TestSetIdleTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gap := 50 * time.Millisecond
		if r.URL.Path == "/stall" {
			gap = 2 * time.Second
		}
		for i := 0; i < 6; i++ {
			_, _ = w.Write([]byte("x"))
			w.(http.Flusher).Flush()
			select {
			case <-time.After(gap):
			case <-r.Context().Done():
				return
			}
		}
	}))
	defer srv.Close()

	c := &http.Client{Timeout: 200 * time.Millisecond}
	SetIdleTimeout(c, 200*time.Millisecond)

	get := func(path string) (string, error) {
		resp, err := c.Get(srv.URL + path)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		return string(b), err
	}

	// 6 × 50 ms = 300 ms total, longer than the 200 ms timeout.
	if body, err := get("/trickle"); err != nil || body != "xxxxxx" {
		t.Fatalf("trickle: body=%q err=%v, want full body", body, err)
	}
	if _, err := get("/stall"); err == nil || !strings.Contains(err.Error(), "no data received") {
		t.Fatalf("stall: err=%v, want idle-timeout error", err)
	}
}
