package geo

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

const defaultSearchLimit = 10

// Search queries NCBI GEO using the eutils esearch + esummary APIs.
// It returns one SearchResult per matching dataset, ordered by relevance.
//
// The query is treated as free text. Additional field operators are appended
// when SearchOptions.Organism or SearchOptions.EntryType are set, so callers
// do not need to know NCBI query syntax.
//
// Example:
//
//	results, err := d.Search(ctx, "ATAC-seq human liver", downloaders.SearchOptions{
//	    Organism:  "Homo sapiens",
//	    EntryType: "GSE",
//	    Limit:     20,
//	})
func (d *GEODownloader) Search(ctx context.Context, query string, opts downloaders.SearchOptions) ([]downloaders.SearchResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}

	// Build query string with optional field operators.
	terms := []string{query}
	if opts.Organism != "" {
		terms = append(terms, fmt.Sprintf(`"%s"[Organism]`, opts.Organism))
	}
	if opts.EntryType != "" {
		terms = append(terms, fmt.Sprintf(`"%s"[Entry Type]`, strings.ToUpper(opts.EntryType)))
	}
	fullQuery := strings.Join(terms, " AND ")

	// --- Step 1: esearch to get UIDs ---
	uids, total, err := d.esearch(ctx, fullQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("GEO search failed: %w", err)
	}

	if len(uids) == 0 {
		return nil, nil
	}

	_ = total // available for display if needed later

	// --- Step 2: batch esummary to get metadata ---
	summaries, err := d.getBatchSummaries(ctx, uids)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch summaries: %w", err)
	}

	results := make([]downloaders.SearchResult, 0, len(summaries))
	for _, summary := range summaries {
		results = append(results, parseSummaryToSearchResult(summary))
	}

	return results, nil
}

// esearch calls the NCBI ESearch endpoint and returns (uids, totalCount, error).
func (d *GEODownloader) esearch(ctx context.Context, query string, retmax int) ([]string, int, error) {
	params := url.Values{}
	params.Set("db", "gds")
	params.Set("term", query)
	params.Set("retmax", strconv.Itoa(retmax))
	params.Set("usehistory", "n")
	params.Set("tool", "hapiq")
	params.Set("email", "hapiq@example.com")

	searchURL := "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esearch.fcgi?" + params.Encode()

	d.rateLimitEUtils()

	content, err := d.makeEUtilsRequest(ctx, searchURL)
	if err != nil {
		return nil, 0, err
	}

	var resp ESearchResponse
	if err := xml.Unmarshal(content, &resp); err != nil {
		return nil, 0, fmt.Errorf("failed to parse esearch response: %w", err)
	}

	total, _ := strconv.Atoi(resp.Count)

	return resp.IdList.IDs, total, nil
}

// getBatchSummaries fetches esummary for up to 500 UIDs in a single request.
func (d *GEODownloader) getBatchSummaries(ctx context.Context, uids []string) ([]DocSum, error) {
	params := url.Values{}
	params.Set("db", "gds")
	params.Set("id", strings.Join(uids, ","))
	params.Set("tool", "hapiq")
	params.Set("email", "hapiq@example.com")

	summaryURL := "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esummary.fcgi?" + params.Encode()

	d.rateLimitEUtils()

	content, err := d.makeEUtilsRequest(ctx, summaryURL)
	if err != nil {
		return nil, err
	}

	var resp EUtilsResponse
	if err := xml.Unmarshal(content, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse esummary response: %w", err)
	}

	return resp.DocSum, nil
}

// parseSummaryToSearchResult converts a DocSum into a SearchResult.
func parseSummaryToSearchResult(summary DocSum) downloaders.SearchResult {
	result := downloaders.SearchResult{}

	for _, item := range summary.Items {
		switch item.Name {
		case "title":
			result.Title = item.Content
		case "Accession":
			result.Accession = item.Content
		case "taxon":
			result.Organism = item.Content
		case "entryType":
			result.EntryType = item.Content
		case "gdsType":
			result.DatasetType = item.Content
		case "PDAT":
			// Normalize YYYY/MM/DD → YYYY-MM
			parts := strings.SplitN(item.Content, "/", 3)
			if len(parts) >= 2 {
				result.Date = parts[0] + "-" + parts[1]
			} else {
				result.Date = item.Content
			}
		case "SSInfo":
			// Nested sample count from series info
			for _, sub := range item.Items {
				if sub.Name == "samples" {
					if n, err := strconv.Atoi(sub.Content); err == nil {
						result.SampleCount = n
					}
				}
			}
		case "n_samples":
			if n, err := strconv.Atoi(item.Content); err == nil && result.SampleCount == 0 {
				result.SampleCount = n
			}
		}
	}

	return result
}
