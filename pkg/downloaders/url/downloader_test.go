package url

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

func TestGetSourceType(t *testing.T) {
	d := New()
	if got := d.GetSourceType(); got != "url" {
		t.Errorf("GetSourceType() = %q, want %q", got, "url")
	}
}

func TestValidate(t *testing.T) {
	d := New()
	ctx := context.Background()

	tests := []struct {
		id        string
		wantValid bool
	}{
		{"https://example.com/file.csv", true},
		{"http://example.com/file.csv", true},
		{"https://example.com", true},
		{"ftp://example.com/file.csv", false},
		{"not-a-url", false},
		{"", false},
		{"https://", false},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			result, err := d.Validate(ctx, tt.id)
			if err != nil {
				t.Fatalf("Validate() unexpected error: %v", err)
			}
			if result.Valid != tt.wantValid {
				t.Errorf("Validate(%q).Valid = %v, want %v", tt.id, result.Valid, tt.wantValid)
			}
			if !result.Valid && len(result.Errors) == 0 {
				t.Errorf("Validate(%q) invalid but no errors reported", tt.id)
			}
		})
	}
}

func TestGetMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", "12345")
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	d := New(WithTimeout(5 * time.Second))
	ctx := context.Background()
	rawURL := srv.URL + "/data.csv"

	meta, err := d.GetMetadata(ctx, rawURL)
	if err != nil {
		t.Fatalf("GetMetadata() error: %v", err)
	}
	if meta.Source != "url" {
		t.Errorf("Source = %q, want %q", meta.Source, "url")
	}
	if meta.ID != rawURL {
		t.Errorf("ID = %q, want %q", meta.ID, rawURL)
	}
	if meta.TotalSize != 12345 {
		t.Errorf("TotalSize = %d, want %d", meta.TotalSize, 12345)
	}
	if meta.FileCount != 1 {
		t.Errorf("FileCount = %d, want 1", meta.FileCount)
	}
}

func TestGetMetadata_Unreachable(t *testing.T) {
	// Unreachable server: GetMetadata should return minimal metadata, not error.
	d := New(WithTimeout(100 * time.Millisecond))
	ctx := context.Background()
	meta, err := d.GetMetadata(ctx, "http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("GetMetadata() should not error on HEAD failure, got: %v", err)
	}
	if meta.Source != "url" {
		t.Errorf("Source = %q, want %q", meta.Source, "url")
	}
}

func TestDownload_Basic(t *testing.T) {
	const body = "hello hapiq"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("Content-Length", "11")
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	d := New(WithTimeout(5*time.Second), WithVerbose(true))
	ctx := context.Background()
	rawURL := srv.URL + "/file.txt"

	meta := &downloaders.Metadata{Source: "url", ID: rawURL}
	req := &downloaders.DownloadRequest{ID: rawURL, OutputDir: dir, Metadata: meta}

	result, err := d.Download(ctx, req)
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	if !result.Success {
		t.Fatalf("Download() Success=false, errors: %v", result.Errors)
	}
	if len(result.Files) != 1 {
		t.Fatalf("Download() len(Files) = %d, want 1", len(result.Files))
	}
	fi := result.Files[0]
	if fi.Size != int64(len(body)) {
		t.Errorf("File.Size = %d, want %d", fi.Size, len(body))
	}
	if fi.Checksum == "" {
		t.Error("File.Checksum is empty")
	}
	if fi.ChecksumType != "sha256" {
		t.Errorf("File.ChecksumType = %q, want sha256", fi.ChecksumType)
	}
	if result.WitnessFile == "" {
		t.Error("WitnessFile is empty; expected hapiq.json to be written")
	}
	got, _ := os.ReadFile(fi.Path)
	if string(got) != body {
		t.Errorf("file content = %q, want %q", string(got), body)
	}
}

func TestDownload_DryRun(t *testing.T) {
	getCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			getCount++
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	d := New()
	ctx := context.Background()
	req := &downloaders.DownloadRequest{
		ID:        srv.URL + "/file.txt",
		OutputDir: dir,
		Options:   &downloaders.DownloadOptions{DryRun: true},
	}

	result, err := d.Download(ctx, req)
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	if !result.Success {
		t.Fatalf("DryRun returned Success=false")
	}
	if len(result.Files) != 1 {
		t.Fatalf("DryRun len(Files) = %d, want 1", len(result.Files))
	}
	if result.Files[0].Path != "" {
		t.Error("DryRun should not produce a real file path")
	}
	if getCount != 0 {
		t.Errorf("DryRun issued %d GET(s), want 0", getCount)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("DryRun wrote %d file(s) to disk, want 0", len(entries))
	}
}

func TestDownload_SkipExisting(t *testing.T) {
	getCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			getCount++
			_, _ = w.Write([]byte("data"))
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	// Pre-create the target file so SkipExisting kicks in.
	_ = os.WriteFile(filepath.Join(dir, "data.bin"), []byte("existing"), 0o644)

	d := New(WithTimeout(5 * time.Second))
	ctx := context.Background()
	req := &downloaders.DownloadRequest{
		ID:        srv.URL + "/data.bin",
		OutputDir: dir,
		Options:   &downloaders.DownloadOptions{SkipExisting: true},
	}

	result, err := d.Download(ctx, req)
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	if !result.Success {
		t.Fatalf("Download() Success=false")
	}
	if getCount != 0 {
		t.Errorf("SkipExisting issued %d GET(s), want 0", getCount)
	}
}

func TestDownload_ContentDisposition(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="real-name.csv"`)
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte("csv,data"))
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	d := New(WithTimeout(5 * time.Second))
	ctx := context.Background()
	req := &downloaders.DownloadRequest{ID: srv.URL + "/ugly?token=abc", OutputDir: dir}

	result, err := d.Download(ctx, req)
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	if !result.Success {
		t.Fatalf("Download() Success=false, errors: %v", result.Errors)
	}
	if result.Files[0].OriginalName != "real-name.csv" {
		t.Errorf("OriginalName = %q, want %q", result.Files[0].OriginalName, "real-name.csv")
	}
}

func TestDownload_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	d := New(WithTimeout(5 * time.Second))
	ctx := context.Background()
	req := &downloaders.DownloadRequest{ID: srv.URL + "/file.bin", OutputDir: dir}

	result, err := d.Download(ctx, req)
	if err != nil {
		t.Fatalf("Download() returned error: %v (want failure inside result)", err)
	}
	if result.Success {
		t.Error("Download() Success=true on server 500, want false")
	}
	if len(result.Errors) == 0 {
		t.Error("Download() no errors reported on server 500")
	}
}

func TestFilenameFromURL(t *testing.T) {
	tests := []struct {
		rawURL string
		want   string
	}{
		{"https://example.com/data.csv", "data.csv"},
		{"https://example.com/path/to/file.h5ad", "file.h5ad"},
		{"https://example.com/", "example.com"},
		{"https://example.com", "example.com"},
		{"not-a-url", "not-a-url"}, // url.Parse succeeds; base of the path is used
	}
	for _, tt := range tests {
		t.Run(tt.rawURL, func(t *testing.T) {
			got := filenameFromURL(tt.rawURL)
			if got != tt.want {
				t.Errorf("filenameFromURL(%q) = %q, want %q", tt.rawURL, got, tt.want)
			}
		})
	}
}

func TestOptions(t *testing.T) {
	d := New(WithVerbose(true), WithTimeout(30*time.Second))
	if !d.verbose {
		t.Error("WithVerbose(true) not applied")
	}
	if d.client.Timeout != 30*time.Second {
		t.Errorf("WithTimeout: client.Timeout = %v, want 30s", d.client.Timeout)
	}
}
