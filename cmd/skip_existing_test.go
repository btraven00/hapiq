package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

// freshRegistry lets a test run a command that registers every downloader
// more than once in one process.
func freshRegistry(t *testing.T) {
	t.Helper()
	old := downloaders.DefaultRegistry
	t.Cleanup(func() { downloaders.DefaultRegistry = old })
	downloaders.DefaultRegistry = downloaders.NewRegistry()
}

// A rerun with --skip-existing reports the file as already present rather
// than downloaded; manifest get (always skip-existing) records it too.
func TestRunDownloadSkipExisting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("payload\n"))
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	oldOut, oldSkip, oldYes := outputDir, skipExisting, nonInteractive
	t.Cleanup(func() { outputDir, skipExisting, nonInteractive = oldOut, oldSkip, oldYes })
	outputDir, nonInteractive = dir, true

	args := []string{"url", srv.URL + "/a.txt"}
	freshRegistry(t)
	if err := runDownload(nil, args); err != nil {
		t.Fatalf("first run: %v", err)
	}
	skipExisting = true
	freshRegistry(t)
	if err := runDownload(nil, args); err != nil {
		t.Fatalf("skip run: %v", err)
	}

	m := filepath.Join(t.TempDir(), "m.yaml")
	if err := os.WriteFile(m, []byte("- identifier: a\n  url: "+srv.URL+"/a.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldParent := manifestParent
	t.Cleanup(func() { manifestParent = oldParent })
	manifestParent = t.TempDir()
	freshRegistry(t)
	if err := runManifestGet(nil, []string{m}); err != nil {
		t.Fatalf("manifest get: %v", err)
	}
}

// Cache hits count against downloaded files only, not skipped ones.
func TestDisplayResultsCacheHitsAndSkipped(t *testing.T) {
	r := &downloaders.DownloadResult{Success: true, BytesTotal: 2, Files: []downloaders.FileInfo{
		{Path: "a", CacheHit: true}, {Path: "b", Skipped: true},
	}}
	if got := downloadedFiles(r); got != 1 {
		t.Fatalf("downloadedFiles = %d, want 1", got)
	}
	displayResults(r)
}
