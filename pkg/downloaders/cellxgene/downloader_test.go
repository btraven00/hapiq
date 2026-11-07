package cellxgene

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
