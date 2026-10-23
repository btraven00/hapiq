package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

// Delete the data, keep hapiq.json, refetch: the file comes back verified.
// If upstream changed the bytes, the refetch fails on the recorded checksum.
func TestRefetch(t *testing.T) {
	var body atomic.Value
	body.Store("original bytes\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body.Load().(string)))
	}))
	t.Cleanup(srv.Close)

	if err := initializeDownloaders(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")

	res, err := downloaders.Download(ctx, "url", &downloaders.DownloadRequest{
		ID: srv.URL + "/a.txt", OutputDir: dir, Metadata: &downloaders.Metadata{},
	})
	if err != nil || !res.Success {
		t.Fatalf("initial download: err=%v errors=%v", err, res.Errors)
	}

	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := refetchDir(ctx, dir); err != nil {
		t.Fatalf("refetch: %v", err)
	}
	if got, _ := os.ReadFile(file); string(got) != "original bytes\n" {
		t.Fatalf("refetched content = %q", got)
	}

	body.Store("changed upstream\n")
	_ = os.Remove(file)
	if err := refetchDir(ctx, dir); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("changed upstream: err = %v, want a hash mismatch", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("mismatched file should be removed")
	}

	// hapiq.json still holds the original hash: restoring upstream passes.
	body.Store("original bytes\n")
	if err := refetchDir(ctx, dir); err != nil {
		t.Fatalf("refetch after upstream restored: %v", err)
	}
}

func TestRefetchOutputDir(t *testing.T) {
	if got, err := refetchOutputDir("geo", "GSE1", "data/GSE1"); err != nil || got != "data" {
		t.Errorf("geo: %q, %v", got, err)
	}
	if _, err := refetchOutputDir("geo", "GSE1", "data/renamed"); err == nil {
		t.Error("geo in a renamed dir should fail before downloading")
	}
	if got, err := refetchOutputDir("zenodo", "123", "data/x"); err != nil || got != "data/x" {
		t.Errorf("zenodo: %q, %v", got, err)
	}
}
