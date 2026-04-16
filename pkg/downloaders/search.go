package downloaders

import "context"

// SearchOptions configures a dataset search operation.
type SearchOptions struct {
	Organism  string // filter by organism (added as a query field operator)
	EntryType string // filter by entry type (e.g. "GSE", "GSM")
	Limit     int    // maximum number of results (0 → source default)
}

// SearchResult represents a single search hit from a data repository.
type SearchResult struct {
	Accession   string `json:"accession"`
	Title       string `json:"title"`
	Organism    string `json:"organism,omitempty"`
	EntryType   string `json:"entry_type,omitempty"`
	DatasetType string `json:"dataset_type,omitempty"`
	Date        string `json:"date,omitempty"`
	SampleCount int    `json:"sample_count,omitempty"`
	FileSize    int64  `json:"file_size,omitempty"` // total bytes of all files in the dataset
}

// Searcher is an optional capability a Downloader can implement to support
// free-text dataset discovery.
type Searcher interface {
	Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error)
}
