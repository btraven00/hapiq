// Package scperturb downloads datasets from the scPerturb collection
// (Peidli et al., Nature Methods 2024), a standardised compendium of
// single-cell perturbation studies.
//
// Metadata is sourced from the project's GitHub repository; files are
// downloaded directly from Zenodo without requiring the Zenodo downloader.
//
// ID formats accepted:
//
//	NormanWeissman2019            – all datasets for that publication
//	NormanWeissman2019_filtered   – single dataset (exact Full index)
package scperturb

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	metadataURL = "https://raw.githubusercontent.com/sanderlab/scPerturb/master/website/datavzrd/scperturb_dataset_info_datavzrd_annotated.csv"
	zenodoBase  = "https://zenodo.org/record"
)

// Dataset holds per-file metadata from the scPerturb CSV.
type Dataset struct {
	FullIndex    string // e.g. NormanWeissman2019_filtered
	PubIndex     string // e.g. NormanWeissman2019
	Title        string
	DOI          string
	FirstAuthor  string
	Organism     string
	Modality     string // RNA, ATAC, Protein, …
	Method       string // Perturb-seq, CROP-seq, …
	Perturbation string // CRISPR-cas9, drugs, …
	Tissues      string
	CellType     string
	Disease      string
	ZenodoID     string
	FileExt      string // h5ad or zip
	Year         string
	NumCells     string
}

// DownloadURL returns the direct Zenodo file URL for this dataset.
func (d *Dataset) DownloadURL() string {
	return fmt.Sprintf("%s/%s/files/%s.%s", zenodoBase, d.ZenodoID, d.FullIndex, d.FileExt)
}

// catalog is a process-level cache of the scPerturb metadata.
var (
	catalogOnce sync.Once
	catalogData []Dataset
	catalogErr  error
)

// loadCatalog fetches and parses the scPerturb metadata CSV.
// Results are cached for the lifetime of the process.
func loadCatalog(ctx context.Context, c *http.Client) ([]Dataset, error) {
	catalogOnce.Do(func() {
		catalogData, catalogErr = fetchCatalog(ctx, c)
	})
	return catalogData, catalogErr
}

func fetchCatalog(ctx context.Context, c *http.Client) ([]Dataset, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", metadataURL, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch scPerturb metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scPerturb metadata HTTP %d", resp.StatusCode)
	}

	return parseCSV(resp.Body)
}

func parseCSV(r io.Reader) ([]Dataset, error) {
	cr := csv.NewReader(r)
	cr.LazyQuotes = true

	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("read CSV header: %w", err)
	}

	// Build column index.
	col := make(map[string]int, len(header))
	for i, h := range header {
		col[strings.TrimSpace(h)] = i
	}

	get := func(row []string, name string) string {
		if i, ok := col[name]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}

	var datasets []Dataset
	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read CSV row: %w", err)
		}

		datasets = append(datasets, Dataset{
			FullIndex:    get(row, "Full index"),
			PubIndex:     get(row, "Publication index"),
			Title:        get(row, "Title"),
			DOI:          get(row, "doi_url"),
			FirstAuthor:  get(row, "First Author"),
			Organism:     get(row, "Organisms"),
			Modality:     get(row, "Modality"),
			Method:       get(row, "Method"),
			Perturbation: get(row, "Perturbation"),
			Tissues:      get(row, "Tissues"),
			CellType:     get(row, "Cell Type"),
			Disease:      get(row, "Disease"),
			ZenodoID:     get(row, "Zenodo ID"),
			FileExt:      get(row, "File Extension"),
			Year:         get(row, "Year"),
			NumCells:     get(row, "Total Number of Cells"),
		})
	}

	return datasets, nil
}

// resolve returns all datasets matching an ID.
// An ID can be an exact Full index or a Publication index prefix.
func resolve(catalog []Dataset, id string) []Dataset {
	var matches []Dataset
	for _, d := range catalog {
		if d.FullIndex == id || d.PubIndex == id {
			matches = append(matches, d)
		}
	}
	return matches
}

// newHTTPClient creates a plain HTTP client with timeout.
func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}
