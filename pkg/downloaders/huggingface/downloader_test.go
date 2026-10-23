package huggingface

import (
	"context"
	"crypto/sha1" // #nosec G505 -- test fixture git blob id
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

const (
	testRepo = "lab/cells"
	testSHA  = "0123456789abcdef0123456789abcdef01234567"
)

func sha256hex(b string) string { s := sha256.Sum256([]byte(b)); return hex.EncodeToString(s[:]) }

func blobSHA1(b string) string {
	h := sha1.New() // #nosec G401 -- test fixture
	_, _ = fmt.Fprintf(h, "blob %d\x00%s", len(b), b)
	return hex.EncodeToString(h.Sum(nil))
}

// fakeHub serves a dataset whose tree spans two pages: an LFS file, a small
// git file, and (when corrupt) an LFS file whose bytes do not match its oid.
func fakeHub(t *testing.T, corrupt bool) *httptest.Server {
	t.Helper()
	contents := map[string]string{
		"data/train.h5ad": "anndata bytes",
		"README.md":       "# cells\n",
		"data/bad.h5ad":   "served bytes",
	}
	page1 := []map[string]any{
		{"type": "file", "path": ".gitattributes", "oid": "x", "size": 1},
		{"type": "directory", "path": "data"},
		{"type": "file", "path": "data/train.h5ad", "oid": "x", "size": 13,
			"lfs": map[string]any{"oid": sha256hex(contents["data/train.h5ad"]), "size": 13}},
	}
	page2 := []map[string]any{
		{"type": "file", "path": "README.md", "oid": blobSHA1(contents["README.md"]), "size": 8},
	}
	if corrupt {
		page2 = append(page2, map[string]any{"type": "file", "path": "data/bad.h5ad", "oid": "x", "size": 12,
			"lfs": map[string]any{"oid": sha256hex("published bytes"), "size": 12}})
	}

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api := "/api/datasets/" + testRepo
		switch p := r.URL.Path; {
		case p == api+"/revision/main" || p == api+"/revision/"+testSHA:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": testRepo, "sha": testSHA, "tags": []string{"license:cc-by-4.0"}})
		case p == api+"/tree/"+testSHA && r.URL.Query().Get("cursor") == "":
			w.Header().Set("Link", fmt.Sprintf(`<%s%s?recursive=true&cursor=2>; rel="next"`, srv.URL, p))
			_ = json.NewEncoder(w).Encode(page1)
		case p == api+"/tree/"+testSHA:
			_ = json.NewEncoder(w).Encode(page2)
		case strings.HasPrefix(p, "/datasets/"+testRepo+"/resolve/"+testSHA+"/"):
			_, _ = w.Write([]byte(contents[strings.TrimPrefix(p, "/datasets/"+testRepo+"/resolve/"+testSHA+"/")]))
		default:
			http.Error(w, `{"error":"Repository not found"}`, http.StatusUnauthorized)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestDownloader(srv *httptest.Server) *HFDownloader {
	d := New()
	d.baseURL = srv.URL
	return d
}

func TestDownloadPinsAndVerifies(t *testing.T) {
	d := newTestDownloader(fakeHub(t, false))
	ctx := context.Background()

	meta, err := d.GetMetadata(ctx, testRepo)
	if err != nil {
		t.Fatal(err)
	}
	if meta.FileCount != 2 || meta.TotalSize != 21 || meta.License != "cc-by-4.0" || meta.Version != testSHA {
		t.Errorf("metadata = %+v", meta)
	}

	out := t.TempDir()
	res, err := d.Download(ctx, &downloaders.DownloadRequest{ID: testRepo, OutputDir: out, Metadata: meta})
	if err != nil || !res.Success {
		t.Fatalf("err=%v errors=%v", err, res.Errors)
	}
	if len(res.Files) != 2 {
		t.Fatalf("files = %+v, want train.h5ad and README.md", res.Files)
	}
	if got, _ := os.ReadFile(filepath.Join(out, "data", "train.h5ad")); string(got) != "anndata bytes" {
		t.Errorf("data/train.h5ad = %q", got)
	}
	if _, err := os.Stat(filepath.Join(out, ".gitattributes")); !os.IsNotExist(err) {
		t.Error(".gitattributes should not be downloaded")
	}

	w, err := os.ReadFile(filepath.Join(out, "hapiq.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(w), `"original_id": "`+testRepo+"@"+testSHA+`"`) {
		t.Errorf("hapiq.json should pin the commit in original_id:\n%s", w)
	}
	if !strings.Contains(string(w), `"source_checksum": "sha256:`+sha256hex("anndata bytes")) {
		t.Errorf("hapiq.json should carry the LFS sha256")
	}
}

func TestDownloadRejectsMismatch(t *testing.T) {
	d := newTestDownloader(fakeHub(t, true))
	out := t.TempDir()
	res, err := d.Download(context.Background(), &downloaders.DownloadRequest{
		ID: testRepo, OutputDir: out, Options: &downloaders.DownloadOptions{IncludeExts: []string{".h5ad"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "data/bad.h5ad") {
		t.Fatalf("Success=%v Errors=%v, want one error for data/bad.h5ad", res.Success, res.Errors)
	}
	if _, err := os.Stat(filepath.Join(out, "data", "bad.h5ad")); !os.IsNotExist(err) {
		t.Error("mismatched file left on disk")
	}
	if len(res.Files) != 1 || res.Files[0].Path != "data/train.h5ad" {
		t.Errorf("files = %+v, want only data/train.h5ad (filter drops README.md)", res.Files)
	}
}

func TestValidate(t *testing.T) {
	d := newTestDownloader(fakeHub(t, false))
	for id, valid := range map[string]bool{
		testRepo:             true,
		testRepo + "@main":   true,
		"lab/missing":        false, // 401 from the Hub
		"not-a-repo":         false,
		"lab/cells@bad rev!": false,
	} {
		vr, err := d.Validate(context.Background(), id)
		if err != nil || vr.Valid != valid {
			t.Errorf("%q: valid=%v err=%v errors=%v, want valid=%v", id, vr.Valid, err, vr.Errors, valid)
		}
	}
}
