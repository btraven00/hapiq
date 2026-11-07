package cellxgene

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

const (
	collID  = "0d281971-11ea-4f85-9c0f-a3a4a7b4f888"
	collVer = "11111111-1111-1111-1111-111111111111"
	dsID    = "b9735320-2e2f-4f44-81ff-b6cac7381dc5"
	dsVer   = "7ce226c8-98ba-47b5-af16-3f88fbbb695f"
)

// fakeAPI serves one collection with one human and one mouse dataset; only the
// collection and dataset-versions endpoints know them, like the real API.
func fakeAPI(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		human := dataset{DatasetID: dsID, DatasetVersionID: dsVer, CollectionID: collID, Title: "gd T",
			Organism: []label{{"Homo sapiens"}},
			Assets:   []asset{{Filetype: "H5AD", URL: srv.URL + "/files/" + dsVer + ".h5ad", Filesize: 5}}}
		mouse := dataset{DatasetID: "m", DatasetVersionID: "mv", Title: "mouse",
			Organism: []label{{"Mus musculus"}},
			Assets:   []asset{{Filetype: "H5AD", URL: srv.URL + "/files/mv.h5ad", Filesize: 3}}}
		var body any
		switch r.URL.Path {
		case "/collections/" + collID:
			body = collection{CollectionID: collID, CollectionVersionID: collVer, Name: "SCLC", Datasets: []dataset{human, mouse}}
		case "/collections": // the public listing
			body = []collection{{CollectionID: collID, Description: "PBMCs from cancer patients"}}
		case "/datasets":
			body = []dataset{human, mouse}
		case "/datasets/" + dsID + "/versions":
			body = []dataset{human}
		case "/files/" + dsVer + ".h5ad":
			_, _ = w.Write([]byte("hello"))
			return
		case "/files/mv.h5ad":
			_, _ = w.Write([]byte("bad!")) // 4 bytes, API says 3
			return
		default:
			http.Error(w, "nope", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCellxgene(t *testing.T) {
	ctx := context.Background()
	d := New(WithBaseURL(fakeAPI(t).URL))

	// Discover URL of a dataset → latest version, pinned.
	r, err := d.resolve(ctx, "https://cellxgene.cziscience.com/e/"+dsID+".cxg/")
	if err != nil || r.Kind != "dataset" || r.PinnedID != dsVer {
		t.Fatalf("dataset resolve: %+v, %v", r, err)
	}
	if v, _ := d.Validate(ctx, "not-a-uuid"); v.Valid {
		t.Fatal("garbage ID validated")
	}
	if v, _ := d.Validate(ctx, "22222222-2222-2222-2222-222222222222"); v.Valid {
		t.Fatal("unknown UUID validated")
	}

	// Collection filtered to human: one file, witness pinned to the collection version.
	out := t.TempDir()
	meta, err := d.GetMetadata(ctx, collID)
	if err != nil || meta.FileCount != 2 || meta.TotalSize != 8 {
		t.Fatalf("metadata: %+v, %v", meta, err)
	}
	res, _ := d.Download(ctx, &downloaders.DownloadRequest{ID: collID, OutputDir: out, Metadata: meta,
		Options: &downloaders.DownloadOptions{Organism: "sapiens"}})
	if !res.Success || len(res.Files) != 1 {
		t.Fatalf("download: %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(out, dsVer+".h5ad")); string(b) != "hello" {
		t.Fatalf("content %q", b)
	}
	var w downloaders.WitnessFile
	raw, _ := os.ReadFile(filepath.Join(out, "hapiq.json"))
	if err := json.Unmarshal(raw, &w); err != nil || w.OriginalID != collVer || w.Source != "cellxgene" {
		t.Fatalf("witness: %+v, %v", w, err)
	}

	// Search is case-insensitive; "gd" matches only the human dataset.
	hits, err := d.Search(ctx, "GD", downloaders.SearchOptions{})
	if err != nil || len(hits) != 1 || hits[0].Accession != dsID || hits[0].FileSize != 5 {
		t.Fatalf("search: %+v, %v", hits, err)
	}
	// Words match separately, here one in the title and one in the collection description.
	if hits, _ := d.Search(ctx, "gd cancer", downloaders.SearchOptions{}); len(hits) != 1 || hits[0].Accession != dsID {
		t.Fatalf("multi-word search: %+v", hits)
	}
	if hits, _ := d.Search(ctx, "", downloaders.SearchOptions{Organism: "musculus"}); len(hits) != 1 || hits[0].Accession != "m" {
		t.Fatalf("organism search: %+v", hits)
	}

	// Size mismatch fails and leaves no partial file.
	res, _ = d.Download(ctx, &downloaders.DownloadRequest{ID: collID, OutputDir: out,
		Options: &downloaders.DownloadOptions{Subset: []string{"m"}}})
	if res.Success {
		t.Fatal("size mismatch accepted")
	}
	if _, err := os.Stat(filepath.Join(out, "mv.h5ad")); err == nil {
		t.Fatal("bad file left behind")
	}
}

// stub serves routes: an int is a status code, a string a raw body, anything
// else JSON. Unknown paths are 404.
func stub(t *testing.T, routes map[string]any) *Downloader {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch v := routes[r.URL.Path].(type) {
		case nil:
			http.NotFound(w, r)
		case int:
			w.WriteHeader(v)
		case string:
			_, _ = w.Write([]byte(v))
		default:
			_ = json.NewEncoder(w).Encode(v)
		}
	}))
	t.Cleanup(srv.Close)
	return New(WithBaseURL(srv.URL), WithTimeout(time.Second), WithVerbose(true))
}

func TestResolveFallbacks(t *testing.T) {
	ctx := context.Background()
	doi := "10.1/x"
	d := stub(t, map[string]any{
		"/collection_versions/" + collVer: collection{CollectionID: collID, DOI: &doi},
		"/dataset_versions/" + dsVer:      dataset{DatasetVersionID: dsVer, Title: "v"},
		"/datasets/" + dsID + "/versions": []dataset{},
	})
	// Collection version without a version ID in the body: pin the UUID asked for.
	r, err := d.resolve(ctx, collVer)
	if err != nil || r.Kind != "collection" || r.PinnedID != collVer || r.DOI != doi {
		t.Fatalf("collection version: %+v, %v", r, err)
	}
	if r, err = d.resolve(ctx, dsVer); err != nil || r.Kind != "dataset" || r.PinnedID != dsVer {
		t.Fatalf("dataset version: %+v, %v", r, err)
	}
	// A dataset with no versions falls through to "not found".
	if _, err = d.resolve(ctx, dsID); err == nil {
		t.Fatal("dataset with no versions resolved")
	}
	if v, _ := d.Validate(ctx, dsVer); !v.Valid {
		t.Fatalf("validate: %+v", v)
	}
	if _, err := d.GetMetadata(ctx, "nope"); err == nil {
		t.Fatal("metadata for garbage ID")
	}
}

func TestAPIErrors(t *testing.T) {
	ctx := context.Background()
	for name, routes := range map[string]map[string]any{
		"429 on collections":     {"/collections/" + collID: http.StatusTooManyRequests},
		"500 on versions":        {"/collection_versions/" + collID: http.StatusInternalServerError},
		"bad JSON on dataset":    {"/datasets/" + collID + "/versions": "{"},
		"500 on dataset version": {"/dataset_versions/" + collID: http.StatusBadGateway},
	} {
		if _, err := stub(t, routes).resolve(ctx, collID); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := New(WithBaseURL("http://\x7f")).resolve(ctx, collID); err == nil {
		t.Error("bad base URL: no error")
	}
	if _, err := New(WithBaseURL("http://127.0.0.1:1")).resolve(ctx, collID); err == nil {
		t.Error("unreachable API: no error")
	}
	if _, err := stub(t, map[string]any{"/datasets": 500, "/collections": []collection{}}).Search(ctx, "", downloaders.SearchOptions{}); err == nil {
		t.Error("search: /datasets error swallowed")
	}
	if _, err := stub(t, map[string]any{"/datasets": []dataset{}, "/collections": 500}).Search(ctx, "", downloaders.SearchOptions{}); err == nil {
		t.Error("search: /collections error swallowed")
	}
}

func TestSearchFilters(t *testing.T) {
	ds := []dataset{
		{DatasetID: "a", Assay: []label{{"10x 3' v3"}, {"Smart-seq2"}}, PublishedAt: "2024-01-02T00:00:00"},
		{DatasetID: "b", Assay: []label{{"Smart-seq2"}}},
		{DatasetID: "c", Assay: []label{{"10x 3' v2"}}},
	}
	d := stub(t, map[string]any{"/datasets": ds, "/collections": []collection{}})
	hits, err := d.Search(context.Background(), "", downloaders.SearchOptions{EntryType: "10X", Limit: 1})
	if err != nil || len(hits) != 1 || hits[0].Accession != "a" || hits[0].EntryType != "10x 3' v3 +1" || hits[0].Date != "2024-01-02" {
		t.Fatalf("search: %+v, %v", hits, err)
	}
}

func TestDownloadPaths(t *testing.T) {
	ctx := context.Background()
	d := New(WithBaseURL(fakeAPI(t).URL), WithVerbose(true))
	req := func(out string, o *downloaders.DownloadOptions) *downloaders.DownloadRequest {
		return &downloaders.DownloadRequest{ID: collID, OutputDir: out, Options: o, Metadata: &downloaders.Metadata{}}
	}

	if res, _ := d.Download(ctx, &downloaders.DownloadRequest{ID: "nope"}); res.Success || len(res.Errors) != 1 {
		t.Fatalf("bad ID: %+v", res)
	}
	// Dry run with a file limit.
	res, _ := d.Download(ctx, req("", &downloaders.DownloadOptions{DryRun: true, LimitFiles: 1}))
	if !res.Success || len(res.Files) != 1 || res.Files[0].Size != 5 {
		t.Fatalf("dry run: %+v", res)
	}

	// Output dir under a regular file cannot be created.
	f := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(f, nil, 0o600)
	if res, _ := d.Download(ctx, req(filepath.Join(f, "sub"), nil)); res.Success {
		t.Fatal("unwritable output dir accepted")
	}

	// Existing file is skipped; hapiq.json as a directory makes the witness a warning.
	out := t.TempDir()
	_ = os.WriteFile(filepath.Join(out, dsVer+".h5ad"), []byte("old"), 0o600)
	_ = os.Mkdir(filepath.Join(out, "hapiq.json"), 0o700)
	res, _ = d.Download(ctx, req(out, &downloaders.DownloadOptions{SkipExisting: true, Subset: []string{dsVer}}))
	if res.FilesSkipped != 1 || len(res.Files) != 0 {
		t.Fatalf("skip existing: %+v", res)
	}
	res, _ = d.Download(ctx, req(out, &downloaders.DownloadOptions{Subset: []string{dsID}}))
	if !res.Success || len(res.Warnings) != 1 || res.WitnessFile != "" {
		t.Fatalf("witness failure: %+v", res)
	}
}
