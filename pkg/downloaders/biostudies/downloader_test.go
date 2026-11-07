package biostudies

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

// TestDownloadSkipExistingRecordsWitness: a file already on disk is listed
// as skipped in the result, still listed in hapiq.json, and not counted as
// downloaded there (FileWitness.Skipped is not serialized).
func TestDownloadSkipExistingRecordsWitness(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"accno":"S-BSST1","section":{"files":[{"path":"data/a.txt","size":5}]}}`))
	}))
	defer srv.Close()
	target, _ := url.Parse(srv.URL)

	d := NewBioStudiesDownloader(WithVerbose(true))
	d.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
		return http.DefaultTransport.RoundTrip(r)
	})}

	out := t.TempDir()
	existing := filepath.Join(out, "data", "a.txt")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := d.Download(context.Background(), &downloaders.DownloadRequest{
		ID: "S-BSST1", OutputDir: out,
		Options:  &downloaders.DownloadOptions{SkipExisting: true},
		Metadata: &downloaders.Metadata{ID: "S-BSST1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.FilesSkipped != 1 || len(res.Files) != 1 || !res.Files[0].Skipped || res.Files[0].Path != existing {
		t.Fatalf("want one skipped file %s, got skipped=%d files=%+v", existing, res.FilesSkipped, res.Files)
	}

	raw, err := os.ReadFile(filepath.Join(out, "hapiq.json"))
	if err != nil {
		t.Fatal(err)
	}
	var w downloaders.WitnessFile
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	if len(w.Files) != 1 || filepath.Join(out, w.Files[0].Path) != existing || w.DownloadStats.FilesDownloaded != 0 {
		t.Fatalf("witness: files=%+v stats=%+v", w.Files, w.DownloadStats)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
