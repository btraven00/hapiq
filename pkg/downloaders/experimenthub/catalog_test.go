package experimenthub

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

// writeCatalog builds a minimal experimenthub.sqlite3 in dir. As upstream,
// rdatapath ids differ from EH numbers: EH3796's file is rdatapath 3832, and
// rdatapath 3796 belongs to EH3760. EH2517 has two files.
func writeCatalog(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, cacheFilename)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		`CREATE TABLE resources (id INTEGER PRIMARY KEY, ah_id TEXT, title TEXT, description TEXT,
			species TEXT, dataprovider TEXT, maintainer TEXT, rdatadateadded TEXT)`,
		`CREATE TABLE rdatapaths (id INTEGER PRIMARY KEY, resource_id INTEGER, rdatapath TEXT,
			rdataclass TEXT, dispatchclass TEXT)`,
		`INSERT INTO resources VALUES (1, 'EH3760', 'scnmt met cgi', '', 'Mus musculus', '', '', ''),
			(2, 'EH3796', 'cord blood counts', '', 'Homo sapiens', '', '', ''),
			(3, 'EH2517', 'two files', '', 'Homo sapiens', '', '', '')`,
		`INSERT INTO rdatapaths VALUES
			(3796, 1, 'SingleCellMultiModal/mouse_gastrulation/v2.0.0/scnmt_met_cgi.rda', 'matrix', 'Rda'),
			(3832, 2, 'SingleCellMultiModal/cord_blood/v1.0.0/scRNAseq_Counts.rda', 'matrix', 'Rda'),
			(2600, 3, 'pkg/a.h5', 'HDF5', 'H5File'),
			(2601, 3, 'pkg/b.rds', 'HDF5', 'Rds')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// Regression: EH3796 was fetched as /fetch/3796, which is EH3760's file.
func TestDryRunResolvesEHIDToRDataPaths(t *testing.T) {
	dir := t.TempDir()
	writeCatalog(t, dir)
	t.Setenv(envMetadataDir, dir)

	d := NewExperimentHubDownloader()
	for id, want := range map[string][]string{
		"EH3796": {fetchURLPrefix + "3832"},
		"EH2517": {fetchURLPrefix + "2600", fetchURLPrefix + "2601"},
	} {
		res, err := d.Download(context.Background(), &downloaders.DownloadRequest{
			ID: id, OutputDir: t.TempDir(), Options: &downloaders.DownloadOptions{DryRun: true},
		})
		if err != nil || !res.Success {
			t.Fatalf("%s: err=%v errors=%v", id, err, res.Errors)
		}
		var got []string
		for _, f := range res.Files {
			got = append(got, f.SourceURL)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: fetch URLs = %v, want %v", id, got, want)
		}
	}

	res, _ := d.Download(context.Background(), &downloaders.DownloadRequest{
		ID: "EH1", OutputDir: t.TempDir(), Options: &downloaders.DownloadOptions{DryRun: true},
	})
	if res.Success {
		t.Error("unknown EH id should fail, not fetch /fetch/1")
	}
}

// Search reports the EH id (ah_id), once per resource.
func TestSearchReportsAHID(t *testing.T) {
	path := writeCatalog(t, t.TempDir())
	got, err := searchCatalog(path, "", downloaders.SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range got {
		ids = append(ids, r.Accession)
	}
	if want := []string{"EH2517", "EH3796", "EH3760"}; !slices.Equal(ids, want) {
		t.Errorf("accessions = %v, want %v", ids, want)
	}
}
