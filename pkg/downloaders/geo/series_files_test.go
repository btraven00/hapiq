package geo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/btraven00/hapiq/pkg/downloaders"
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

	// Second run into the same directory: everything is skipped, nothing re-fetched.
	run2 := &downloaders.DownloadResult{}
	if err := d.downloadSeries(ctx, testGSE, dir, opts, run2); err != nil {
		t.Fatal(err)
	}

	if len(run2.Files) != 0 || run2.FilesSkipped != len(want) {
		t.Errorf("run 2: files=%d skipped=%d, want 0 and %d", len(run2.Files), run2.FilesSkipped, len(want))
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
<a href="e.txt">e.txt</a>`

	want := []listingEntry{
		{"a.csv.gz", 14 << 20},
		{"b.csv.gz", 242 << 10},
		{"c.csv.gz", 5 << 20},
		{"d.txt", 512},
		{"e.txt", -1},
	}
	if got := parseDirectoryListing(html); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
