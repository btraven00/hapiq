package tenx

import (
	"context"
	"crypto/md5" // #nosec G501 -- test fixture md5
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

func md5hex(s string) string { h := md5.Sum([]byte(s)); return hex.EncodeToString(h[:]) } // #nosec G401

// writePage writes a dataset page JSON shaped like 10x's, listing files on
// srv. web_summary.html appears in both outputs and summaries, as on 10x.
// corrupt gives metrics_summary.csv a wrong md5.
func writePage(t *testing.T, srv *httptest.Server, corrupt bool) string {
	t.Helper()
	metricsMD5 := md5hex("metric,value\n")
	if corrupt {
		metricsMD5 = md5hex("something else")
	}
	base := srv.URL + "/samples/cell-vdj/8.0.0/ds_sample/ds_sample_"
	page := fmt.Sprintf(`{"pageProps":{"dataset":{"title":"Test dataset","slug":"test-ds",
		"publishedAt":"2024-04-15T17:54:51.394Z","pipeline":"8.0.0","software":{"name":"Cell Ranger"},
		"species":["Human, Mouse"],"cellNucleiCount":6076},
	  "filesetMap":{"Sample Outputs":{
		"outputs":[
		  {"bytes":13,"md5sum":"%s","title":"Summary CSV","url":"%smetrics_summary.csv"},
		  {"bytes":10,"md5sum":"%s","title":"Summary HTML","url":"%sweb_summary.html"},
		  {"bytes":4,"md5sum":"%s","title":"Feature / cell matrix HDF5","url":"%sfiltered_feature_bc_matrix.h5"}],
		"inputs":[],
		"summaries":[{"bytes":10,"md5sum":"%s","title":"Summary","url":"%sweb_summary.html"}]}}},
	  "__N_SSG":true}`,
		metricsMD5, base, md5hex("<p>web</p>"), base, md5hex("hdf5"), base, md5hex("<p>web</p>"), base)
	p := filepath.Join(t.TempDir(), "test-ds.json")
	if err := os.WriteFile(p, []byte(page), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func fakeCDN(t *testing.T) *httptest.Server {
	t.Helper()
	body := map[string]string{
		"metrics_summary.csv":           "metric,value\n",
		"web_summary.html":              "<p>web</p>",
		"filtered_feature_bc_matrix.h5": "hdf5",
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/samples/cell-vdj/8.0.0/ds_sample/ds_sample_")
		b, ok := body[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(b))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestDownloader(srv *httptest.Server) *TenxDownloader {
	d := New()
	d.client = srv.Client()
	return d
}

func TestDownloadFromPageJSON(t *testing.T) {
	srv := fakeCDN(t)
	d := newTestDownloader(srv)
	ctx := context.Background()
	page := writePage(t, srv, false)

	meta, err := d.GetMetadata(ctx, page)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ID != "test-ds" || meta.FileCount != 3 || meta.TotalSize != 27 {
		t.Errorf("metadata: id=%s files=%d size=%d, want test-ds 3 27 (web_summary counted once)",
			meta.ID, meta.FileCount, meta.TotalSize)
	}

	out := t.TempDir()
	res, err := d.Download(ctx, &downloaders.DownloadRequest{ID: page, OutputDir: out, Metadata: meta})
	if err != nil || !res.Success || len(res.Files) != 3 {
		t.Fatalf("err=%v success=%v errors=%v files=%d", err, res.Success, res.Errors, len(res.Files))
	}
	for _, f := range res.Files {
		if !strings.HasPrefix(f.SourceChecksum, "md5:") {
			t.Errorf("%s: SourceChecksum = %q, want the page's md5", f.Path, f.SourceChecksum)
		}
	}

	// The page JSON is kept with the data and hapiq.json points at it.
	w, err := os.ReadFile(filepath.Join(out, "hapiq.json"))
	if err != nil {
		t.Fatal(err)
	}
	keep, _ := filepath.Abs(filepath.Join(out, "test-ds.10x.json"))
	if !strings.Contains(string(w), fmt.Sprintf("%q", keep)) {
		t.Errorf("hapiq.json original_id should be %s:\n%s", keep, w)
	}

	// Refetching from the kept copy works (same file as src and dst).
	_ = os.Remove(filepath.Join(out, "ds_sample_metrics_summary.csv"))
	res, err = d.Download(ctx, &downloaders.DownloadRequest{
		ID: keep, OutputDir: out, Options: &downloaders.DownloadOptions{SkipExisting: true},
	})
	if err != nil || !res.Success || len(res.Files) != 1 || res.FilesSkipped != 2 {
		t.Fatalf("refetch: err=%v errors=%v files=%d skipped=%d", err, res.Errors, len(res.Files), res.FilesSkipped)
	}
}

func TestDownloadRejectsMD5Mismatch(t *testing.T) {
	srv := fakeCDN(t)
	out := t.TempDir()
	res, err := newTestDownloader(srv).Download(context.Background(), &downloaders.DownloadRequest{
		ID: writePage(t, srv, true), OutputDir: out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "metrics_summary.csv") {
		t.Fatalf("Success=%v Errors=%v, want one md5 error for metrics_summary.csv", res.Success, res.Errors)
	}
	if _, err := os.Stat(filepath.Join(out, "ds_sample_metrics_summary.csv")); !os.IsNotExist(err) {
		t.Error("mismatched file left on disk")
	}
}

func TestValidateRejectsListingJSON(t *testing.T) {
	// The datasets search listing has results but no pageProps.dataset.
	p := filepath.Join(t.TempDir(), "list.json")
	_ = os.WriteFile(p, []byte(`{"meta":{"count":4},"results":[{"slug":"x"}]}`), 0o600)
	vr, err := New().Validate(context.Background(), p)
	if err != nil || vr.Valid {
		t.Fatalf("listing JSON should be invalid: valid=%v err=%v", vr.Valid, err)
	}
}
