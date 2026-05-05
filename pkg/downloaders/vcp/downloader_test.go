package vcp

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

const testID = "aabbccddeeff001122334455"

// newFakeVCPServer starts a test server that serves:
//   - GET /v1/data/public/dataset/{id} → DatasetRecord JSON with one file
//   - HEAD /blobs/{filename}           → 200 with Content-Length
//   - GET  /blobs/{filename}           → random blob bytes
//
// blobGETs is incremented on each blob GET so callers can assert download counts.
func newFakeVCPServer(t *testing.T, filename string, blob []byte, blobGETs *atomic.Int32) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/public/dataset/"):
			// endpoint("dataset") with no token builds: baseURL + "/public/dataset"
			rec := DatasetRecord{
				InternalID: testID,
				Label:      "test dataset",
				Locations: []Location{
					{URL: srv.URL + "/blobs/" + filename, ContentSize: int64(len(blob))},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(rec)

		case strings.HasPrefix(r.URL.Path, "/blobs/"):
			switch r.Method {
			case http.MethodHead:
				w.Header().Set("Content-Length", fmt.Sprintf("%d", len(blob)))
				w.WriteHeader(http.StatusOK)
			case http.MethodGet:
				blobGETs.Add(1)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(blob)
			}

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func randomBlob(t *testing.T, size int) []byte {
	t.Helper()
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return b
}

func TestDownload_Basic(t *testing.T) {
	blob := randomBlob(t, 4096)
	var gets atomic.Int32
	srv := newFakeVCPServer(t, "data.h5ad", blob, &gets)

	dir := t.TempDir()
	d := NewVCPDownloader(WithBaseURL(srv.URL), WithTimeout(5*time.Second))
	req := &downloaders.DownloadRequest{
		ID:        testID,
		OutputDir: dir,
		Metadata:  &downloaders.Metadata{Source: "vcp", ID: testID},
	}

	result, err := d.Download(context.Background(), req)
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	if !result.Success {
		t.Fatalf("Download() Success=false, errors: %v", result.Errors)
	}
	if gets.Load() != 1 {
		t.Errorf("expected 1 blob GET, got %d", gets.Load())
	}

	dest := filepath.Join(dir, "data.h5ad")
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(blob) {
		t.Error("file content does not match served blob")
	}
}

func TestDownload_SkipExistingByDefault(t *testing.T) {
	blob := randomBlob(t, 4096)
	var gets atomic.Int32
	srv := newFakeVCPServer(t, "data.h5ad", blob, &gets)

	dir := t.TempDir()
	existing := []byte("already here")
	if err := os.WriteFile(filepath.Join(dir, "data.h5ad"), existing, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	d := NewVCPDownloader(WithBaseURL(srv.URL), WithTimeout(5*time.Second))
	req := &downloaders.DownloadRequest{
		ID:        testID,
		OutputDir: dir,
	}

	result, err := d.Download(context.Background(), req)
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	if !result.Success {
		t.Fatalf("Download() Success=false, errors: %v", result.Errors)
	}
	if gets.Load() != 0 {
		t.Errorf("expected 0 blob GETs (skip existing), got %d", gets.Load())
	}

	// Original file must be untouched.
	got, _ := os.ReadFile(filepath.Join(dir, "data.h5ad"))
	if string(got) != string(existing) {
		t.Error("existing file was overwritten without --force")
	}
}

func TestDownload_ForceOverwritesExisting(t *testing.T) {
	blob := randomBlob(t, 4096)
	var gets atomic.Int32
	srv := newFakeVCPServer(t, "data.h5ad", blob, &gets)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.h5ad"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	d := NewVCPDownloader(WithBaseURL(srv.URL), WithTimeout(5*time.Second))
	req := &downloaders.DownloadRequest{
		ID:        testID,
		OutputDir: dir,
		Options:   &downloaders.DownloadOptions{Force: true},
	}

	result, err := d.Download(context.Background(), req)
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	if !result.Success {
		t.Fatalf("Download() Success=false, errors: %v", result.Errors)
	}
	if gets.Load() != 1 {
		t.Errorf("expected 1 blob GET with --force, got %d", gets.Load())
	}

	got, _ := os.ReadFile(filepath.Join(dir, "data.h5ad"))
	if string(got) != string(blob) {
		t.Error("file was not overwritten by --force")
	}
}
