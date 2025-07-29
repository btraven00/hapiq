// Package geo provides metadata extraction functionality for different GEO dataset types
// using the official NCBI E-utilities REST API for robust and structured data access.
package geo

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

// EUtilsResponse represents the response structure from NCBI E-utilities
type EUtilsResponse struct {
	XMLName xml.Name `xml:"eSummaryResult"`
	DocSum  []DocSum `xml:"DocSum"`
}

// DocSum represents a document summary from E-utilities
type DocSum struct {
	ID    string `xml:"Id"`
	Items []Item `xml:"Item"`
}

// Item represents individual fields in the document summary
type Item struct {
	Name    string `xml:"Name,attr"`
	Type    string `xml:"Type,attr"`
	Content string `xml:",chardata"`
	Items   []Item `xml:"Item,omitempty"`
}

// ESearchResponse represents the response from ESearch utility
type ESearchResponse struct {
	XMLName   xml.Name `xml:"eSearchResult"`
	Count     string   `xml:"Count"`
	RetMax    string   `xml:"RetMax"`
	RetStart  string   `xml:"RetStart"`
	IdList    IdList   `xml:"IdList"`
	QueryKey  string   `xml:"QueryKey,omitempty"`
	WebEnv    string   `xml:"WebEnv,omitempty"`
	ErrorList struct {
		PhraseNotFound []string `xml:"PhraseNotFound"`
		FieldNotFound  []string `xml:"FieldNotFound"`
	} `xml:"ErrorList,omitempty"`
}

// IdList contains the list of UIDs returned by ESearch
type IdList struct {
	IDs []string `xml:"Id"`
}

// getSeriesMetadata retrieves metadata for a GEO Series (GSE) using E-utilities
func (d *GEODownloader) getSeriesMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	// First, search for the GSE to get the UID
	uid, err := d.searchGEORecord(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to find GEO record: %w", err)
	}

	// Get summary information using ESummary
	summary, err := d.getSummary(ctx, "gds", uid)
	if err != nil {
		return nil, fmt.Errorf("failed to get summary: %w", err)
	}

	metadata := &downloaders.Metadata{
		Source: d.GetSourceType(),
		ID:     id,
		Custom: make(map[string]any),
	}

	// Parse summary fields
	for _, item := range summary.Items {
		switch item.Name {
		case "title":
			metadata.Title = item.Content
		case "summary":
			metadata.Description = item.Content
		case "GPL":
			metadata.Custom["platform"] = item.Content
		case "taxon":
			metadata.Custom["organism"] = item.Content
		case "entryType":
			metadata.Custom["entry_type"] = item.Content
		case "gdsType":
			metadata.Custom["dataset_type"] = item.Content
		case "ptechType":
			metadata.Custom["platform_technology"] = item.Content
		case "valType":
			metadata.Custom["value_type"] = item.Content
		case "SSInfo":
			// Parse sample and subset information
			if sampleInfo := d.parseSampleInfo(item); sampleInfo != nil {
				metadata.Custom["sample_info"] = sampleInfo
				if count, ok := sampleInfo["sample_count"].(int); ok {
					metadata.FileCount = count * 2                       // Estimate: raw + processed per sample
					metadata.TotalSize = int64(count) * 50 * 1024 * 1024 // 50MB per sample estimate
				}
			}
		case "PDAT":
			if parsed, err := d.parseEUtilsDate(item.Content); err == nil {
				metadata.Created = parsed
			}
		case "suppFile":
			// Parse supplementary file information
			metadata.Custom["supplementary_files"] = item.Content
		case "Accession":
			// Verify this matches our expected ID
			if item.Content != id {
				metadata.Custom["canonical_accession"] = item.Content
			}
		}
	}

	// For debugging, let's try a different approach to get sample information
	// First check if we have any file count information already
	if metadata.FileCount == 0 {
		// Set some default estimates for now
		metadata.FileCount = 10                // Conservative estimate
		metadata.TotalSize = 500 * 1024 * 1024 // 500MB estimate

		// Create a basic collection
		collection := downloaders.Collection{
			Type:          "geo_series",
			ID:            id,
			Title:         metadata.Title,
			FileCount:     metadata.FileCount,
			EstimatedSize: metadata.TotalSize,
			UserConfirmed: false,
			Samples:       []string{}, // Will be populated during download
		}
		metadata.Collections = []downloaders.Collection{collection}
	}

	// Try to get publication information
	if pubmedIDs, err := d.getLinkedPubMed(ctx, uid); err == nil && len(pubmedIDs) > 0 {
		metadata.Custom["pubmed_ids"] = pubmedIDs
		// Could fetch publication details here if needed
	}

	return metadata, nil
}

// getSampleMetadata retrieves metadata for a GEO Sample (GSM) using E-utilities
func (d *GEODownloader) getSampleMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	// Search for the GSM to get the UID
	uid, err := d.searchGEORecord(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to find GEO record: %w", err)
	}

	// Get summary information
	summary, err := d.getSummary(ctx, "gds", uid)
	if err != nil {
		return nil, fmt.Errorf("failed to get summary: %w", err)
	}

	metadata := &downloaders.Metadata{
		Source: d.GetSourceType(),
		ID:     id,
		Custom: make(map[string]any),
	}

	// Parse summary fields
	for _, item := range summary.Items {
		switch item.Name {
		case "title":
			metadata.Title = item.Content
		case "summary":
			metadata.Description = item.Content
		case "GPL":
			metadata.Custom["platform"] = item.Content
		case "taxon":
			metadata.Custom["organism"] = item.Content
		case "sampleType":
			metadata.Custom["sample_type"] = item.Content
		case "sourceNameCh1":
			metadata.Custom["source_name"] = item.Content
		case "moleculeCh1":
			metadata.Custom["molecule"] = item.Content
		case "extractProtocolCh1":
			metadata.Custom["extraction_protocol"] = item.Content
		case "PDAT":
			if parsed, err := d.parseEUtilsDate(item.Content); err == nil {
				metadata.Created = parsed
			}
		case "suppFile":
			if item.Content != "" {
				// Count supplementary files
				files := strings.Split(item.Content, ";")
				metadata.FileCount = len(files)
				metadata.TotalSize = int64(len(files)) * 25 * 1024 * 1024 // 25MB per file estimate
				metadata.Custom["supplementary_files"] = files
			}
		}
	}

	// Default file estimates for samples without supplementary file info
	if metadata.FileCount == 0 {
		metadata.FileCount = 2                // Typical: raw + processed
		metadata.TotalSize = 50 * 1024 * 1024 // 50MB estimate
	}

	return metadata, nil
}

// getPlatformMetadata retrieves metadata for a GEO Platform (GPL) using E-utilities
func (d *GEODownloader) getPlatformMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	// Search for the GPL to get the UID
	uid, err := d.searchGEORecord(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to find GEO record: %w", err)
	}

	// Get summary information
	summary, err := d.getSummary(ctx, "gds", uid)
	if err != nil {
		return nil, fmt.Errorf("failed to get summary: %w", err)
	}

	metadata := &downloaders.Metadata{
		Source: d.GetSourceType(),
		ID:     id,
		Custom: make(map[string]any),
	}

	// Parse summary fields
	for _, item := range summary.Items {
		switch item.Name {
		case "title":
			metadata.Title = item.Content
		case "summary":
			metadata.Description = item.Content
		case "taxon":
			metadata.Custom["organism"] = item.Content
		case "technology":
			metadata.Custom["technology"] = item.Content
		case "distribution":
			metadata.Custom["distribution"] = item.Content
		case "manufacturer":
			metadata.Custom["manufacturer"] = item.Content
		case "manufactureProtocol":
			metadata.Custom["manufacture_protocol"] = item.Content
		case "PDAT":
			if parsed, err := d.parseEUtilsDate(item.Content); err == nil {
				metadata.Created = parsed
			}
		case "n_samples":
			if count, err := strconv.Atoi(item.Content); err == nil {
				metadata.Custom["associated_samples"] = count
			}
		}
	}

	// Platform files are typically annotation files
	metadata.FileCount = 1
	metadata.TotalSize = 5 * 1024 * 1024 // 5MB estimate for annotation file

	return metadata, nil
}

// getDatasetMetadata retrieves metadata for a GEO Dataset (GDS) using E-utilities
func (d *GEODownloader) getDatasetMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	// Search for the GDS to get the UID
	uid, err := d.searchGEORecord(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to find GEO record: %w", err)
	}

	// Get summary information
	summary, err := d.getSummary(ctx, "gds", uid)
	if err != nil {
		return nil, fmt.Errorf("failed to get summary: %w", err)
	}

	metadata := &downloaders.Metadata{
		Source: d.GetSourceType(),
		ID:     id,
		Custom: make(map[string]any),
	}

	// Parse summary fields
	for _, item := range summary.Items {
		switch item.Name {
		case "title":
			metadata.Title = item.Content
		case "summary":
			metadata.Description = item.Content
		case "taxon":
			metadata.Custom["organism"] = item.Content
		case "gdsType":
			metadata.Custom["dataset_type"] = item.Content
		case "ptechType":
			metadata.Custom["platform_technology"] = item.Content
		case "valType":
			metadata.Custom["value_type"] = item.Content
		case "PDAT":
			if parsed, err := d.parseEUtilsDate(item.Content); err == nil {
				metadata.Created = parsed
			}
		case "n_samples":
			if count, err := strconv.Atoi(item.Content); err == nil {
				metadata.Custom["sample_count"] = count
				metadata.FileCount = 1                         // Usually one processed dataset file
				metadata.TotalSize = int64(count) * 100 * 1024 // 100KB per sample estimate
			}
		case "subsetInfo":
			// Parse subset information for experimental variables
			if subsets := d.parseSubsetInfo(item); len(subsets) > 0 {
				metadata.Custom["experimental_variables"] = subsets
			}
		}
	}

	return metadata, nil
}

// searchGEORecord searches for a GEO record and returns its UID
func (d *GEODownloader) searchGEORecord(ctx context.Context, accession string) (string, error) {
	// Construct search query
	query := fmt.Sprintf("%s[Accession]", accession)

	// Build ESearch URL
	params := url.Values{}
	params.Set("db", "gds")
	params.Set("term", query)
	params.Set("retmax", "1")
	params.Set("tool", "hapiq")
	params.Set("email", "hapiq@example.com") // Should be configurable

	searchURL := fmt.Sprintf("https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esearch.fcgi?%s", params.Encode())

	// Apply rate limiting before making request
	d.rateLimitEUtils()

	// Make request
	content, err := d.makeEUtilsRequest(ctx, searchURL)
	if err != nil {
		return "", err
	}

	// Parse response
	var response ESearchResponse
	if err := xml.Unmarshal(content, &response); err != nil {
		return "", fmt.Errorf("failed to parse search response: %w", err)
	}

	// Check if any results found
	if len(response.IdList.IDs) == 0 {
		return "", fmt.Errorf("no records found for accession %s", accession)
	}

	// Check for errors
	if len(response.ErrorList.PhraseNotFound) > 0 {
		return "", fmt.Errorf("phrase not found: %v", response.ErrorList.PhraseNotFound)
	}

	return response.IdList.IDs[0], nil
}

// getSummary retrieves document summary for a given UID
func (d *GEODownloader) getSummary(ctx context.Context, database, uid string) (*DocSum, error) {
	// Build ESummary URL
	params := url.Values{}
	params.Set("db", database)
	params.Set("id", uid)
	params.Set("tool", "hapiq")
	params.Set("email", "hapiq@example.com") // Should be configurable

	summaryURL := fmt.Sprintf("https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esummary.fcgi?%s", params.Encode())

	// Apply rate limiting before making request
	d.rateLimitEUtils()

	// Make request
	content, err := d.makeEUtilsRequest(ctx, summaryURL)
	if err != nil {
		return nil, err
	}

	// Parse response
	var response EUtilsResponse
	if err := xml.Unmarshal(content, &response); err != nil {
		return nil, fmt.Errorf("failed to parse summary response: %w", err)
	}

	if len(response.DocSum) == 0 {
		return nil, fmt.Errorf("no summary found for UID %s", uid)
	}

	return &response.DocSum[0], nil
}

// getRelatedSamples retrieves related sample IDs for a series
func (d *GEODownloader) getRelatedSamples(ctx context.Context, uid string) ([]string, error) {
	// Use ELink to find related GSM records
	params := url.Values{}
	params.Set("dbfrom", "gds")
	params.Set("db", "gds")
	params.Set("id", uid)
	params.Set("linkname", "gds_gds")
	params.Set("tool", "hapiq")
	params.Set("email", "hapiq@example.com")

	linkURL := fmt.Sprintf("https://eutils.ncbi.nlm.nih.gov/entrez/eutils/elink.fcgi?%s", params.Encode())

	// Apply rate limiting before making request
	d.rateLimitEUtils()

	// Make request
	_, err := d.makeEUtilsRequest(ctx, linkURL)
	if err != nil {
		return nil, err
	}

	// For now, return empty slice - linking GSE to GSM is complex
	// In practice, you'd parse the ELink XML response and then fetch
	// the individual sample records
	return []string{}, nil
}

// getLinkedPubMed retrieves linked PubMed IDs
func (d *GEODownloader) getLinkedPubMed(ctx context.Context, uid string) ([]string, error) {
	// Use ELink to find related PubMed records
	params := url.Values{}
	params.Set("dbfrom", "gds")
	params.Set("db", "pubmed")
	params.Set("id", uid)
	params.Set("tool", "hapiq")
	params.Set("email", "hapiq@example.com")

	linkURL := fmt.Sprintf("https://eutils.ncbi.nlm.nih.gov/entrez/eutils/elink.fcgi?%s", params.Encode())

	// Apply rate limiting before making request
	d.rateLimitEUtils()

	// Make request
	_, err := d.makeEUtilsRequest(ctx, linkURL)
	if err != nil {
		return nil, err
	}

	// For now, return empty slice - would need to parse ELink XML
	return []string{}, nil
}

// makeEUtilsRequest makes an HTTP request to E-utilities and returns the response
func (d *GEODownloader) makeEUtilsRequest(ctx context.Context, url string) ([]byte, error) {
	// Add API key to URL if available
	if d.apiKey != "" {
		separator := "&"
		if !strings.Contains(url, "?") {
			separator = "?"
		}
		url = url + separator + "api_key=" + d.apiKey
	}

	// Note: Rate limiting is applied by the caller, not here to avoid double rate limiting

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		return nil, fmt.Errorf("E-utilities rate limit exceeded (HTTP 429). Consider setting NCBI_API_KEY environment variable for higher limits")
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("E-utilities request failed with status %d", resp.StatusCode)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return content, nil
}

// parseSampleInfo parses sample and subset information from SSInfo field
func (d *GEODownloader) parseSampleInfo(item Item) map[string]interface{} {
	info := make(map[string]interface{})

	// Parse nested items in SSInfo
	for _, subItem := range item.Items {
		switch subItem.Name {
		case "samples":
			if count, err := strconv.Atoi(subItem.Content); err == nil {
				info["sample_count"] = count
			}
		case "subsets":
			if count, err := strconv.Atoi(subItem.Content); err == nil {
				info["subset_count"] = count
			}
		}
	}

	return info
}

// parseSubsetInfo parses experimental subset information
func (d *GEODownloader) parseSubsetInfo(item Item) []string {
	var subsets []string

	for _, subItem := range item.Items {
		if subItem.Name == "subset" {
			subsets = append(subsets, subItem.Content)
		}
	}

	return subsets
}

// parseEUtilsDate parses E-utilities date formats
func (d *GEODownloader) parseEUtilsDate(dateStr string) (time.Time, error) {
	if dateStr == "" {
		return time.Time{}, fmt.Errorf("empty date string")
	}

	// E-utilities typically return dates in YYYY/MM/DD format
	formats := []string{
		"2006/01/02",
		"2006-01-02",
		"2006/1/2",
		"Jan 02, 2006",
		"Jan 2, 2006",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse date: %s", dateStr)
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
