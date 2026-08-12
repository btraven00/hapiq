package url

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

// fixtureFiles mirrors a published dataset directory: several data files that
// must all arrive for the dataset to be usable.
var fixtureFiles = map[string]string{
	"be1.h5ad":                   "H5-ish payload",
	"be1.clusters_truth.tsv":     "cell\tlabel\n",
	"be1_properties.yaml":        "n_cells: 3\n",
	"be1.clusters_truth_num.txt": "3",
}

// dirServer serves a Caddy-style browsable directory: JSON when the client asks
// for it, an HTML index page otherwise.
func dirServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/data/")

		// "/data" and "/data/" both address the directory. Serving the index at
		// the slashless form (rather than redirecting to the slash form) is the
		// harder case: nothing downstream can tell it apart from a real file.
		if name == "" || name == "/data" { // the directory itself
			if !strings.Contains(r.Header.Get("Accept"), "application/json") {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write([]byte("<!DOCTYPE html><html><body>index page</body></html>"))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			rows := []string{`{"name":"sub","size":0,"is_dir":true}`}
			for n, body := range fixtureFiles {
				rows = append(rows, fmt.Sprintf(`{"name":%q,"size":%d,"is_dir":false}`, n, len(body)))
			}
			_, _ = w.Write([]byte("[" + strings.Join(rows, ",") + "]"))
			return
		}

		body, ok := fixtureFiles[name]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
}

func newRequest(dir, rawURL string, opts *downloaders.DownloadOptions) *downloaders.DownloadRequest {
	return &downloaders.DownloadRequest{
		ID:        rawURL,
		OutputDir: dir,
		Metadata:  &downloaders.Metadata{Source: "url", ID: rawURL},
		Options:   opts,
	}
}

func TestDownload_Directory(t *testing.T) {
	srv := dirServer(t)
	defer srv.Close()

	dir := t.TempDir()
	d := New(WithTimeout(5 * time.Second))
	result, err := d.Download(context.Background(), newRequest(dir, srv.URL+"/data/", nil))
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	if !result.Success {
		t.Fatalf("Download() Success=false, errors: %v", result.Errors)
	}

	// Every file arrives, with its own name and bytes -- the subdirectory does not.
	if len(result.Files) != len(fixtureFiles) {
		t.Fatalf("len(Files) = %d, want %d", len(result.Files), len(fixtureFiles))
	}
	var total int64
	for name, body := range fixtureFiles {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if string(got) != body {
			t.Errorf("%s = %q, want %q", name, got, body)
		}
		total += int64(len(body))
	}
	if result.BytesDownloaded != total {
		t.Errorf("BytesDownloaded = %d, want %d", result.BytesDownloaded, total)
	}
	for _, fi := range result.Files {
		if fi.Checksum == "" {
			t.Errorf("%s: checksum is empty", fi.OriginalName)
		}
	}
	if result.WitnessFile == "" {
		t.Error("WitnessFile is empty; expected hapiq.json covering the directory")
	}
}

// The bug this whole feature exists for: a directory URL used to be fetched as
// a single file, storing the HTML index page as if it were the dataset.
func TestDownload_DirectoryNeverStoresIndexPage(t *testing.T) {
	srv := dirServer(t)
	defer srv.Close()

	dir := t.TempDir()
	d := New(WithTimeout(5 * time.Second))
	result, err := d.Download(context.Background(), newRequest(dir, srv.URL+"/data/", nil))
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		body, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		if strings.Contains(string(body), "<!DOCTYPE html>") {
			t.Fatalf("%s contains the HTML index page", e.Name())
		}
	}
	_ = result
}

// Without the trailing slash there is no directory intent, so the fetch would
// store the index page. It must fail loudly instead, and leave nothing behind.
func TestDownload_ExtensionlessHTMLIsRejected(t *testing.T) {
	srv := dirServer(t)
	defer srv.Close()

	dir := t.TempDir()
	d := New(WithTimeout(5 * time.Second))
	result, err := d.Download(context.Background(), newRequest(dir, srv.URL+"/data", nil))
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	if result.Success {
		t.Fatal("Download() Success=true; expected the HTML index to be rejected")
	}
	if len(result.Errors) == 0 || !strings.Contains(strings.Join(result.Errors, " "), "trailing slash") {
		t.Errorf("errors = %v, want one naming the trailing slash", result.Errors)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("output dir has %d entries, want none", len(entries))
	}
}

// The rejection must be based on the bytes, not on the Content-Type header:
// a cache hit supplies no header, so a header-only check would let an index
// page cached once slip through on every later run.
func TestDownload_HTMLRejectedRegardlessOfHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream") // header lies
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body>index page</body></html>"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	d := New(WithTimeout(5 * time.Second))
	result, err := d.Download(context.Background(), newRequest(dir, srv.URL+"/data", nil))
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	if result.Success {
		t.Fatal("Download() Success=true; expected HTML bytes to be rejected on sniffing")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("output dir has %d entries, want none", len(entries))
	}
}

// A directory index is remote input. An entry naming a path outside the
// directory must abort the download, not get quietly flattened into a file
// name: a listing that tries it is not one to take the rest of on trust.
func TestDownload_RejectsTraversalInIndex(t *testing.T) {
	for _, name := range []string{
		"../../../../tmp/pwned",
		"..",
		"sub/nested.h5ad",
		`..\..\windows`,
		"/etc/passwd",
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(fmt.Appendf(nil,
					`[{"name":%q,"size":4,"is_dir":false},{"name":"ok.h5ad","size":2,"is_dir":false}]`, name))
			}))
			defer srv.Close()

			dir := t.TempDir()
			d := New(WithTimeout(5 * time.Second))
			result, err := d.Download(context.Background(), newRequest(dir, srv.URL+"/data/", nil))
			if err != nil {
				t.Fatalf("Download() error: %v", err)
			}
			if result.Success {
				t.Fatalf("Download() Success=true; expected entry %q to be refused", name)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Errorf("output dir has %d entries, want none", len(entries))
			}
		})
	}
}

func TestIsPlainName(t *testing.T) {
	tests := map[string]bool{
		"be1.h5ad":       true,
		".hidden":        true,
		"with space.tsv": true,
		"":               false,
		".":              false,
		"..":             false,
		"../escape":      false,
		"sub/file":       false,
		`sub\file`:       false,
		"/absolute":      false,
		"nul\x00byte":    false,
	}
	for name, want := range tests {
		if got := isPlainName(name); got != want {
			t.Errorf("isPlainName(%q) = %v, want %v", name, got, want)
		}
	}
}

// A server with no JSON index must say so rather than hand back its HTML page.
func TestDownload_DirectoryWithoutJSONIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>no json here</body></html>"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	d := New(WithTimeout(5 * time.Second))
	result, err := d.Download(context.Background(), newRequest(dir, srv.URL+"/data/", nil))
	if err != nil {
		t.Fatalf("Download() error: %v", err)
	}
	if result.Success {
		t.Fatal("Download() Success=true; expected a non-JSON index to fail")
	}
	if !strings.Contains(strings.Join(result.Errors, " "), "JSON index") {
		t.Errorf("errors = %v, want one naming the missing JSON index", result.Errors)
	}
}

func TestDownload_DirectoryFilters(t *testing.T) {
	srv := dirServer(t)
	defer srv.Close()

	t.Run("include-ext", func(t *testing.T) {
		dir := t.TempDir()
		d := New(WithTimeout(5 * time.Second))
		req := newRequest(dir, srv.URL+"/data/", &downloaders.DownloadOptions{IncludeExts: []string{".h5ad"}})
		result, err := d.Download(context.Background(), req)
		if err != nil {
			t.Fatalf("Download() error: %v", err)
		}
		if len(result.Files) != 1 || result.Files[0].OriginalName != "be1.h5ad" {
			t.Fatalf("Files = %+v, want only be1.h5ad", result.Files)
		}
	})

	t.Run("limit-files", func(t *testing.T) {
		dir := t.TempDir()
		d := New(WithTimeout(5 * time.Second))
		req := newRequest(dir, srv.URL+"/data/", &downloaders.DownloadOptions{LimitFiles: 2})
		result, err := d.Download(context.Background(), req)
		if err != nil {
			t.Fatalf("Download() error: %v", err)
		}
		if len(result.Files) != 2 {
			t.Fatalf("len(Files) = %d, want 2", len(result.Files))
		}
	})

	t.Run("dry-run writes nothing", func(t *testing.T) {
		dir := t.TempDir()
		d := New(WithTimeout(5 * time.Second))
		req := newRequest(dir, srv.URL+"/data/", &downloaders.DownloadOptions{DryRun: true})
		result, err := d.Download(context.Background(), req)
		if err != nil {
			t.Fatalf("Download() error: %v", err)
		}
		if len(result.Files) != len(fixtureFiles) {
			t.Fatalf("len(Files) = %d, want %d enumerated", len(result.Files), len(fixtureFiles))
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("dry run wrote %d entries", len(entries))
		}
	})
}

// GetMetadata must count the directory's files, not report the old hardcoded 1.
func TestGetMetadata_Directory(t *testing.T) {
	srv := dirServer(t)
	defer srv.Close()

	d := New(WithTimeout(5 * time.Second))
	meta, err := d.GetMetadata(context.Background(), srv.URL+"/data/")
	if err != nil {
		t.Fatalf("GetMetadata() error: %v", err)
	}
	if meta.FileCount != len(fixtureFiles) {
		t.Errorf("FileCount = %d, want %d", meta.FileCount, len(fixtureFiles))
	}
	var total int64
	for _, body := range fixtureFiles {
		total += int64(len(body))
	}
	if meta.TotalSize != total {
		t.Errorf("TotalSize = %d, want %d", meta.TotalSize, total)
	}
}

func TestIsDirectoryURL(t *testing.T) {
	tests := map[string]bool{
		"https://example.com/data/":          true,
		"https://example.com/":               true,
		"https://example.com/data/file.h5ad": false,
		"https://example.com/data":           false,
	}
	for rawURL, want := range tests {
		if got := isDirectoryURL(rawURL); got != want {
			t.Errorf("isDirectoryURL(%q) = %v, want %v", rawURL, got, want)
		}
	}
}

func TestChildURL(t *testing.T) {
	tests := []struct{ dir, name, want string }{
		{"https://example.com/data/", "be1.h5ad", "https://example.com/data/be1.h5ad"},
		{"https://example.com/data/", "a b.tsv", "https://example.com/data/a%20b.tsv"},
	}
	for _, tt := range tests {
		if got := childURL(tt.dir, tt.name); got != tt.want {
			t.Errorf("childURL(%q, %q) = %q, want %q", tt.dir, tt.name, got, tt.want)
		}
	}
}
