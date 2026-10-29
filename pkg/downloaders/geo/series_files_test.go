package geo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
)

const testGSE = "GSE147405"

// fakeGEOFTP serves a GSE's suppl/ listing, matrix and SOFT files, and counts
// GETs per path.
func fakeGEOFTP(t *testing.T) (*httptest.Server, func(string) int) {
	t.Helper()

	base := "/series/GSE147nnn/" + testGSE
	listing := `<pre>Name   Last modified   Size  <hr><a href="/geo/series/GSE147nnn/GSE147405/">Parent Directory</a>   -
<a href="GSE147405_A549_EGF_TimeCourse_UMI_matrix.csv.gz">GSE147405_A549_EGF_TimeCourse_UMI_matrix.csv.gz</a>   2020-03-23 12:48   35M
<a href="GSE147405_A549_EGF_TimeCourse_metadata.csv.gz">GSE147405_A549_EGF_TimeCourse_metadata.csv.gz</a>   2020-03-23 12:48  650K
<a href="GSE147405_A549_EGF_KinaseScreen_UMI_matrix.csv.gz">GSE147405_A549_EGF_KinaseScreen_UMI_matrix.csv.gz</a>   2020-03-23 12:47   14M
<a href="GSE147405_MCF7_TGFB1_TimeCourse_UMI_matrix.csv.gz">GSE147405_MCF7_TGFB1_TimeCourse_UMI_matrix.csv.gz</a>   2020-03-23 12:48  5.0M
</pre>`

	var mu sync.Mutex
	hits := map[string]int{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()

		switch {
		case r.URL.Path == base+"/suppl/":
			_, _ = w.Write([]byte(listing))
		case strings.HasPrefix(r.URL.Path, base+"/"):
			_, _ = w.Write([]byte("payload")) // matrix (no GSM ids → no samples), SOFT, suppl files
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	return srv, func(p string) int {
		mu.Lock()
		defer mu.Unlock()

		return hits[base+p]
	}
}

func newTestDownloader(ftpBaseURL string) *GEODownloader {
	d := NewGEODownloader()
	d.ftpBaseURL = ftpBaseURL
	d.apiKey = "test" // only shortens the rate-limit sleep; no E-utils calls are made

	return d
}

func paths(files []downloaders.FileInfo) []string {
	var out []string
	for _, f := range files {
		out = append(out, f.Path)
	}

	slices.Sort(out)

	return out
}

// Regression: series supplementary downloads ignored the filters and
// SkipExisting, so the dry run and the real download disagreed.
func TestSeriesDownloadHonorsFiltersAndSkipExisting(t *testing.T) {
	srv, hits := fakeGEOFTP(t)
	d := newTestDownloader(srv.URL)
	ctx := context.Background()
	opts := &downloaders.DownloadOptions{FilenameGlob: "*A549*TimeCourse*", SkipExisting: true}

	dry := &downloaders.DownloadResult{}
	if err := d.enumerateSeries(ctx, testGSE, opts, dry); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"metadata/GSE147405_family.soft.gz",
		"metadata/GSE147405_series_matrix.txt.gz",
		"supplementary/GSE147405_A549_EGF_TimeCourse_UMI_matrix.csv.gz",
		"supplementary/GSE147405_A549_EGF_TimeCourse_metadata.csv.gz",
	}
	if got := paths(dry.Files); !slices.Equal(got, want) {
		t.Fatalf("dry-run paths = %v, want %v", got, want)
	}

	// Dry-run entries carry a prospective size and no fake timestamp.
	for _, f := range dry.Files {
		if strings.HasSuffix(f.Path, "UMI_matrix.csv.gz") && f.Size != 35<<20 {
			t.Errorf("dry-run size of %s = %d, want %d", f.Path, f.Size, 35<<20)
		}
	}

	if b, _ := json.Marshal(dry.Files[0]); strings.Contains(string(b), "download_time") {
		t.Errorf("dry-run FileInfo should omit zero download_time: %s", b)
	}

	// Contract: the real download fetches exactly the dry-run file set.
	dir := t.TempDir()

	run1 := &downloaders.DownloadResult{}
	if err := d.downloadSeries(ctx, testGSE, dir, opts, run1); err != nil {
		t.Fatal(err)
	}

	if got := paths(run1.Files); !slices.Equal(got, want) {
		t.Fatalf("download paths = %v, want dry-run paths %v", got, want)
	}

	if n := hits("/suppl/GSE147405_A549_EGF_KinaseScreen_UMI_matrix.csv.gz"); n != 0 {
		t.Errorf("filtered-out file was requested %d times", n)
	}

	// Second run into the same directory: everything is skipped, nothing
	// re-fetched, and the skipped files are still listed for hapiq.json.
	run2 := &downloaders.DownloadResult{}
	if err := d.downloadSeries(ctx, testGSE, dir, opts, run2); err != nil {
		t.Fatal(err)
	}

	if got := paths(run2.Files); !slices.Equal(got, want) || run2.FilesSkipped != len(want) {
		t.Errorf("run 2: files=%v skipped=%d, want %v all skipped", got, run2.FilesSkipped, want)
	}
	for _, f := range run2.Files {
		if !f.Skipped {
			t.Errorf("run 2: %s not marked Skipped", f.Path)
		}
	}

	for _, p := range []string{
		"/suppl/GSE147405_A549_EGF_TimeCourse_UMI_matrix.csv.gz",
		"/suppl/GSE147405_A549_EGF_TimeCourse_metadata.csv.gz",
		"/soft/GSE147405_family.soft.gz",
	} {
		if n := hits(p); n != 1 {
			t.Errorf("%s fetched %d times across two runs, want 1", p, n)
		}
	}
}

// Regression: a filter that matches nothing must not be silent.
func TestSeriesPlanWarnsWhenFiltersExcludeEverything(t *testing.T) {
	srv, _ := fakeGEOFTP(t)
	d := newTestDownloader(srv.URL)

	result := &downloaders.DownloadResult{}
	files := d.planSeriesFiles(context.Background(), testGSE, &downloaders.DownloadOptions{IncludeExts: []string{".h5ad"}}, result)

	if len(files) != 2 { // the two series metadata files bypass filters
		t.Fatalf("got %v, want only the 2 metadata files", paths(files))
	}

	if !slices.ContainsFunc(result.Warnings, func(w string) bool { return strings.Contains(w, "excluded by filters") }) {
		t.Errorf("expected an all-filtered warning, got %v", result.Warnings)
	}
}

// Regression: sizes come from the listing, not a hardcoded guess.
func TestParseDirectoryListingSizes(t *testing.T) {
	html := `<a href="/geo/series/GSE147nnn/GSE147405/">Parent Directory</a>    -
<a href="a.csv.gz">a.csv.gz</a>          2020-03-23 12:47   14M
<a href="b.csv.gz">b.csv.gz</a>          2020-03-23 12:47  242K
<a href="c.csv.gz">c.csv.gz</a>          2020-03-23 12:47  5.0M
<a href="d.txt">d.txt</a>                2020-03-23 12:47  512
<a href="e.txt">e.txt</a>
<a href="f.txt">f.txt</a>                2020-03-23 12:47  -
<a href="https://www.hhs.gov/vulnerability-disclosure-policy/index.html">HHS Vulnerability Disclosure</a>
<a href="">broken</a>`

	want := []listingEntry{
		{"a.csv.gz", 14 << 20},
		{"b.csv.gz", 242 << 10},
		{"c.csv.gz", 5 << 20},
		{"d.txt", 512},
		{"e.txt", -1},
		{"f.txt", -1},
	}
	if got := parseDirectoryListing(html); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// The full Download path: a second run into a populated directory skips every
// file, still counts as a success, and records the skips in the witness stats.
func TestDownloadRerunSkipsEverything(t *testing.T) {
	srv, hits := fakeGEOFTP(t)
	d := newTestDownloader(srv.URL)
	out := t.TempDir()
	req := &downloaders.DownloadRequest{
		ID:        testGSE,
		OutputDir: out,
		Metadata:  &downloaders.Metadata{Source: "geo", ID: testGSE, Custom: map[string]any{}},
		Options:   &downloaders.DownloadOptions{FilenameGlob: "*A549*TimeCourse*", SkipExisting: true, NonInteractive: true},
	}

	for run := 1; run <= 2; run++ {
		result, err := d.Download(context.Background(), req)
		if err != nil || !result.Success {
			t.Fatalf("run %d: success=%v err=%v errors=%v", run, result.Success, err, result.Errors)
		}
	}

	if n := hits("/suppl/GSE147405_A549_EGF_TimeCourse_UMI_matrix.csv.gz"); n != 1 {
		t.Errorf("file fetched %d times across two runs, want 1", n)
	}

	witness, err := common.LoadWitnessFile(filepath.Join(out, testGSE))
	if err != nil {
		t.Fatal(err)
	}

	got := *witness.DownloadStats
	if got.FilesTotal != 4 || got.FilesSkipped != 4 || got.FilesDownloaded != 0 || got.BytesDownloaded != 0 {
		t.Errorf("run 2 witness stats = %+v, want 4 total, 4 skipped, 0 downloaded", got)
	}

	if len(witness.Files) != 4 {
		t.Errorf("witness should still list the 4 files from run 1, got %d", len(witness.Files))
	}
}

// A listed supplementary file that fails (missing or truncated) is an error (the run fails); the
// guessed SOFT file only warns. Nothing failed is listed or left on disk.
func TestSeriesFetchFailures(t *testing.T) {
	base := "/series/GSE147nnn/" + testGSE
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case base + "/suppl/":
			_, _ = w.Write([]byte(`<a href="gone.csv.gz">gone.csv.gz</a>   2020-03-23 12:48   35M
<a href="short.csv.gz">short.csv.gz</a>   2020-03-23 12:48   35M`))
		case base + "/suppl/short.csv.gz": // truncated mid-transfer
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte("abc"))
		case base + "/matrix/" + testGSE + "_series_matrix.txt.gz":
			_, _ = w.Write([]byte("payload")) // served, so no E-utils fallback
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	d := newTestDownloader(srv.URL)
	dir := t.TempDir()
	result := &downloaders.DownloadResult{}

	if err := d.downloadSeries(context.Background(), testGSE, dir, nil, result); err != nil {
		t.Fatal(err)
	}

	if got := paths(result.Files); len(got) != 1 || got[0] != "metadata/"+testGSE+"_series_matrix.txt.gz" {
		t.Errorf("only the matrix should be listed, got %v", got)
	}

	for _, want := range []string{"supplementary/gone.csv.gz", "supplementary/short.csv.gz"} {
		if !slices.ContainsFunc(result.Errors, func(e string) bool { return strings.Contains(e, want) }) {
			t.Errorf("missing error for %s in %v", want, result.Errors)
		}

		if _, err := os.Stat(filepath.Join(dir, want)); !os.IsNotExist(err) {
			t.Errorf("failed %s left on disk (stat err=%v)", want, err)
		}
	}

	soft := "metadata/" + testGSE + "_family.soft.gz"
	if !slices.ContainsFunc(result.Warnings, func(w string) bool { return strings.Contains(w, soft) }) {
		t.Errorf("missing warning for %s in %v", soft, result.Warnings)
	}
}

// An unlistable suppl/ leaves just the metadata files, with a warning.
func TestSeriesPlanWarnsWhenUnlistable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)

	result := &downloaders.DownloadResult{}

	files := newTestDownloader(srv.URL).planSeriesFiles(context.Background(), testGSE, nil, result)
	if len(files) != 2 {
		t.Errorf("unlistable suppl/ should leave just the 2 metadata files, got %v", paths(files))
	}

	if !slices.ContainsFunc(result.Warnings, func(w string) bool { return strings.Contains(w, "could not list supplementary files") }) {
		t.Errorf("missing listing warning in %v", result.Warnings)
	}
}

// FlatOutput writes into OutputDir itself; by default a series nests under
// <OutputDir>/<id>. manifest get relies on the flat form.
func TestDownloadFlatOutput(t *testing.T) {
	srv, _ := fakeGEOFTP(t)
	d := newTestDownloader(srv.URL)

	for _, flat := range []bool{false, true} {
		out := t.TempDir()
		res, err := d.Download(context.Background(), &downloaders.DownloadRequest{
			ID:        testGSE,
			OutputDir: out,
			Metadata:  &downloaders.Metadata{ID: testGSE},
			Options: &downloaders.DownloadOptions{
				FilenameGlob: "*metadata*", NonInteractive: true, SkipExisting: true, FlatOutput: flat, // as manifest get
			},
		})
		if err != nil || !res.Success {
			t.Fatalf("flat=%v: err=%v errors=%v", flat, err, res.Errors)
		}

		want := filepath.Join(out, testGSE, "hapiq.json")
		if flat {
			want = filepath.Join(out, "hapiq.json")
		}

		if res.WitnessFile != want {
			t.Errorf("flat=%v: witness at %s, want %s", flat, res.WitnessFile, want)
		}
	}
}
