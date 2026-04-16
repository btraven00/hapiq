package vcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

const defaultSearchLimit = 10

// Search implements the Searcher interface for VCP datasets.
//
// The query is treated as free text. Additional Lucene field filters are
// appended when SearchOptions fields are set:
//
//	--organism "Homo sapiens"  →  AND organism:"Homo sapiens"
//	--assay "Perturb-Seq"      →  AND assay:"Perturb-Seq"
func (d *VCPDownloader) Search(ctx context.Context, query string, opts downloaders.SearchOptions) ([]downloaders.SearchResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}

	fullQuery := buildQuery(query, opts)

	resp, err := d.c.search(ctx, fullQuery, limit, "")
	if err != nil {
		return nil, fmt.Errorf("VCP search: %w", err)
	}

	results := make([]downloaders.SearchResult, 0, len(resp.Data))
	for _, item := range resp.Data {
		results = append(results, itemToSearchResult(item))
	}
	return results, nil
}

// buildQuery constructs the Lucene query string from the user's term + filters.
func buildQuery(term string, opts downloaders.SearchOptions) string {
	parts := []string{term}

	if opts.Organism != "" {
		parts = append(parts, fmt.Sprintf(`organism:"%s"`, opts.Organism))
	}
	// EntryType is re-used for assay in CZI context.
	if opts.EntryType != "" {
		parts = append(parts, fmt.Sprintf(`assay:"%s"`, opts.EntryType))
	}

	return strings.Join(parts, " AND ")
}

// itemToSearchResult converts a DataItem to the common SearchResult type.
func itemToSearchResult(item DataItem) downloaders.SearchResult {
	var totalSize int64
	for _, loc := range item.Locations {
		totalSize += loc.ContentSize
	}

	return downloaders.SearchResult{
		Accession:   item.InternalID,
		Title:       item.Name,
		Organism:    strings.Join(item.Organism, ", "),
		EntryType:   strings.Join(item.Assay, ", "), // assay is more useful than domain for display
		DatasetType: item.Domain,
		FileSize:    totalSize,
	}
}
