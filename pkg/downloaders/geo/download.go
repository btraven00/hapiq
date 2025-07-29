// Package geo provides download functionality for different GEO dataset types
// using NCBI E-utilities for metadata discovery and FTP for file downloads.
package geo

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
)

// Rate limiting for E-utilities (3 requests per second without API key, 10 with API key)
var lastEUtilsRequest time.Time
var eUtilsRateLimit = 400 * time.Millisecond           // More conservative: 2.5 requests/second
var eUtilsRateLimitWithAPIKey = 120 * time.Millisecond // 8 requests/second (conservative 10/sec limit)

// ELinkResponse represents the response from ELink utility for finding related records
type ELinkResponse struct {
	XMLName  xml.Name  `xml:"eLinkResult"`
	LinkSets []LinkSet `xml:"LinkSet"`
}

// LinkSet contains linked records
type LinkSet struct {
	DbFrom   string   `xml:"DbFrom"`
	IdList   []string `xml:"IdList>Id"`
	LinkInfo []LinkDB `xml:"LinkSetDb"`
}

// LinkDB contains links to specific databases
type LinkDB struct {
	DbTo     string   `xml:"DbTo"`
	LinkName string   `xml:"LinkName"`
	Links    []string `xml:"Link>Id"`
}

// downloadSeries downloads a complete GEO Series (GSE) with all samples
func (d *GEODownloader) downloadSeries(ctx context.Context, id, targetDir string, options *downloaders.DownloadOptions, result *downloaders.DownloadResult) error {
	if d.verbose {
		fmt.Printf("📦 Downloading GEO Series: %s\n", id)
	}

	// Create series subdirectories
	metadataDir := filepath.Join(targetDir, "metadata")
	supplementaryDir := filepath.Join(targetDir, "supplementary")

	for _, dir := range []string{metadataDir, supplementaryDir} {
		if err := common.EnsureDirectory(dir); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Download series metadata files using known FTP patterns
	if err := d.downloadSeriesMetadata(ctx, id, metadataDir, result); err != nil {
		if d.verbose {
			fmt.Printf("⚠️  Series metadata download had issues: %v\n", err)
		}
		// Don't fail completely for metadata issues
		result.Warnings = append(result.Warnings, fmt.Sprintf("series metadata download issues: %v", err))
	}

	// Download series-level supplementary files (priority for modern datasets)
	if err := d.downloadSeriesSupplementary(ctx, id, supplementaryDir, result); err != nil {
		if d.verbose {
			fmt.Printf("⚠️  Series supplementary download had issues: %v\n", err)
		}
		result.Warnings = append(result.Warnings, fmt.Sprintf("series supplementary files: %v", err))
	}

	// Try to download individual sample files if they exist
	// Many modern datasets (like GSE166895) only have series-level files
	samples, err := d.getSeriesSamplesViaEUtils(ctx, id)
	if err != nil {
		if d.verbose {
			fmt.Printf("⚠️  Could not get sample list: %v\n", err)
		}
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to get samples list: %v", err))
		// Continue without individual samples - this is not a critical error
	} else {
		if d.verbose {
			fmt.Printf("🧬 Found %d samples in series, checking for individual files\n", len(samples))
		}

		// Only create samples directory if we have samples to download
		samplesDir := filepath.Join(targetDir, "samples")
		sampleFilesFound := false

		for i, sampleID := range samples {
			if d.verbose {
				fmt.Printf("📁 [%d/%d] Checking sample: %s\n", i+1, len(samples), sampleID)
			}

			// Check if sample has individual files before creating directory
			if !d.sampleHasFiles(ctx, sampleID) {
				if d.verbose {
					fmt.Printf("   No individual files found for %s\n", sampleID)
				}
				continue
			}

			// Create samples directory only when we find the first sample with files
			if !sampleFilesFound {
				if err := common.EnsureDirectory(samplesDir); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("failed to create samples directory: %v", err))
					break
				}
				sampleFilesFound = true
			}

			sampleDir := filepath.Join(samplesDir, sampleID)
			if err := common.EnsureDirectory(sampleDir); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("failed to create sample directory %s: %v", sampleID, err))
				continue
			}

			if err := d.downloadSample(ctx, sampleID, sampleDir, options, result); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("failed to download sample %s: %v", sampleID, err))
				continue
			}
		}

		if !sampleFilesFound && len(samples) > 0 {
			if d.verbose {
				fmt.Printf("ℹ️  No individual sample files found - this is normal for datasets with series-level data only\n")
			}
		}
	}

	return nil
}

// downloadSample downloads a single GEO Sample (GSM)
func (d *GEODownloader) downloadSample(ctx context.Context, id, targetDir string, options *downloaders.DownloadOptions, result *downloaders.DownloadResult) error {
	if d.verbose {
		fmt.Printf("🧬 Downloading GEO Sample: %s\n", id)
	}

	// Get sample metadata to determine available files
	metadata, err := d.getSampleMetadata(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get sample metadata: %w", err)
	}

	// Generate file URLs based on GEO FTP patterns and metadata
	fileURLs := d.generateSampleFileURLs(id, metadata)
	if len(fileURLs) == 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("no downloadable files found for sample %s", id))
		return nil
	}

	// Download each file
	for filename, fileURL := range fileURLs {
		targetPath := filepath.Join(targetDir, filename)

		// Skip if file exists and skip_existing is enabled
		if options != nil && options.SkipExisting {
			if _, err := os.Stat(targetPath); err == nil {
				if d.verbose {
					fmt.Printf("⏭️  Skipping existing file: %s\n", filename)
				}
				continue
			}
		}

		// Apply filters if specified
		if options != nil && !d.shouldDownloadFile(filename, options) {
			if d.verbose {
				fmt.Printf("🚫 Filtering out file: %s\n", filename)
			}
			continue
		}

		if d.verbose {
			fmt.Printf("⬇️  Downloading: %s\n", filename)
		}

		fileInfo, err := d.downloadFileWithRetry(ctx, fileURL, targetPath)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to download %s: %v", filename, err))
			continue
		}

		// Make path relative to target directory
		relPath, err := filepath.Rel(targetDir, fileInfo.Path)
		if err != nil {
			relPath = fileInfo.Path
		}
		fileInfo.Path = relPath

		result.Files = append(result.Files, *fileInfo)
	}

	return nil
}

// downloadPlatform downloads a GEO Platform (GPL) annotation file
func (d *GEODownloader) downloadPlatform(ctx context.Context, id, targetDir string, options *downloaders.DownloadOptions, result *downloaders.DownloadResult) error {
	if d.verbose {
		fmt.Printf("🔬 Downloading GEO Platform: %s\n", id)
	}

	// Platform annotation file URL using FTP pattern
	annotationURL := fmt.Sprintf("%s/platforms/%s/%s/%s.annot.gz", d.ftpBaseURL, d.getGPLSubdir(id), id, id)

	filename := fmt.Sprintf("%s_annotation.txt.gz", id)
	targetPath := filepath.Join(targetDir, filename)

	if d.verbose {
		fmt.Printf("⬇️  Downloading platform annotation: %s\n", filename)
	}

	fileInfo, err := d.downloadFileWithRetry(ctx, annotationURL, targetPath)
	if err != nil {
		// Try alternative soft format
		softURL := fmt.Sprintf("%s/platforms/%s/%s/%s.soft.gz", d.ftpBaseURL, d.getGPLSubdir(id), id, id)
		softFilename := fmt.Sprintf("%s_platform.soft.gz", id)
		softTargetPath := filepath.Join(targetDir, softFilename)

		if d.verbose {
			fmt.Printf("🔄 Trying alternative SOFT format for platform data\n")
		}

		fileInfo, err = d.downloadFileWithRetry(ctx, softURL, softTargetPath)
		if err != nil {
			return fmt.Errorf("failed to download platform data: %w", err)
		}
	}

	// Make path relative to target directory
	relPath, err := filepath.Rel(targetDir, fileInfo.Path)
	if err != nil {
		relPath = fileInfo.Path
	}
	fileInfo.Path = relPath

	result.Files = append(result.Files, *fileInfo)
	return nil
}

// downloadDataset downloads a GEO Dataset (GDS) processed data
func (d *GEODownloader) downloadDataset(ctx context.Context, id, targetDir string, options *downloaders.DownloadOptions, result *downloaders.DownloadResult) error {
	if d.verbose {
		fmt.Printf("📊 Downloading GEO Dataset: %s\n", id)
	}

	// Dataset SOFT format URL
	softURL := fmt.Sprintf("%s/datasets/%s/%s/%s.soft.gz", d.ftpBaseURL, d.getGDSSubdir(id), id, id)

	filename := fmt.Sprintf("%s_dataset.soft.gz", id)
	targetPath := filepath.Join(targetDir, filename)

	if d.verbose {
		fmt.Printf("⬇️  Downloading dataset: %s\n", filename)
	}

	fileInfo, err := d.downloadFileWithRetry(ctx, softURL, targetPath)
	if err != nil {
		return fmt.Errorf("failed to download dataset: %w", err)
	}

	// Make path relative to target directory
	relPath, err := filepath.Rel(targetDir, fileInfo.Path)
	if err != nil {
		relPath = fileInfo.Path
	}
	fileInfo.Path = relPath

	result.Files = append(result.Files, *fileInfo)
	return nil
}

// downloadSeriesMetadata downloads series-level metadata files using FTP patterns
func (d *GEODownloader) downloadSeriesMetadata(ctx context.Context, id, targetDir string, result *downloaders.DownloadResult) error {
	// Try matrix directory first (more reliable structure)
	matrixFile := fmt.Sprintf("%s_series_matrix.txt.gz", id)
	matrixURL := fmt.Sprintf("%s/series/%s/%s/matrix/%s", d.ftpBaseURL, d.getGSESubdir(id), id, matrixFile)

	// Try soft directory
	softFile := fmt.Sprintf("%s_family.soft.gz", id)
	softURL := fmt.Sprintf("%s/series/%s/%s/soft/%s", d.ftpBaseURL, d.getGSESubdir(id), id, softFile)

	metadataFiles := map[string]string{
		matrixFile: matrixURL,
		softFile:   softURL,
	}

	var downloadedAny bool
	for filename, url := range metadataFiles {
		targetPath := filepath.Join(targetDir, filename)

		if d.verbose {
			fmt.Printf("📄 Downloading metadata: %s\n", filename)
		}

		fileInfo, err := d.downloadFileWithRetry(ctx, url, targetPath)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to download %s: %v", filename, err))
			continue
		}

		// Make path relative to metadata directory
		relPath := filepath.Join("metadata", filename)
		fileInfo.Path = relPath

		result.Files = append(result.Files, *fileInfo)
		downloadedAny = true
	}

	if !downloadedAny {
		return fmt.Errorf("no metadata files could be downloaded")
	}

	return nil
}

// downloadSeriesSupplementary downloads supplementary files for a series
// downloadSeriesSupplementary downloads supplementary files from series suppl directory
func (d *GEODownloader) downloadSeriesSupplementary(ctx context.Context, id, targetDir string, result *downloaders.DownloadResult) error {
	// Supplementary files base URL
	suppBaseURL := fmt.Sprintf("%s/series/%s/%s/suppl/", d.ftpBaseURL, d.getGSESubdir(id), id)

	if d.verbose {
		fmt.Printf("📎 Checking supplementary files directory...\n")
	}

	// Get actual file list from FTP directory
	files, err := d.getDirectoryListing(ctx, suppBaseURL)
	if err != nil {
		if d.verbose {
			fmt.Printf("⚠️  Could not access supplementary directory: %v\n", err)
		}
		return fmt.Errorf("failed to access supplementary directory: %w", err)
	}

	if len(files) == 0 {
		if d.verbose {
			fmt.Printf("ℹ️  No supplementary files found in directory\n")
		}
		return nil
	}

	if d.verbose {
		fmt.Printf("📁 Found %d supplementary files\n", len(files))
	}

	downloadedAny := false
	for _, filename := range files {
		fileURL := suppBaseURL + filename
		targetPath := filepath.Join(targetDir, filename)

		if d.verbose {
			fmt.Printf("📎 Downloading: %s\n", filename)
		}

		fileInfo, err := d.downloadFileWithRetry(ctx, fileURL, targetPath)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to download %s: %v", filename, err))
			continue
		}

		// Make path relative to supplementary directory
		relPath := filepath.Join("supplementary", filename)
		fileInfo.Path = relPath

		result.Files = append(result.Files, *fileInfo)
		downloadedAny = true
	}

	if !downloadedAny && len(files) > 0 {
		return fmt.Errorf("failed to download any supplementary files")
	}

	return nil
}

// getSeriesSamplesViaEUtils retrieves sample IDs for a series using series matrix file
func (d *GEODownloader) getSeriesSamplesViaEUtils(ctx context.Context, seriesID string) ([]string, error) {
	// Try to get samples from the series matrix file, which reliably contains sample IDs
	samples, err := d.extractSamplesFromMatrixFile(ctx, seriesID)
	if err != nil {
		// Fallback: try to search for samples using E-utilities
		samples, err = d.searchSamplesForSeries(ctx, seriesID)
		if err != nil {
			return nil, fmt.Errorf("failed to get samples for series: %w", err)
		}
	}

	return samples, nil
}

// extractSamplesFromMatrixFile downloads series matrix file and extracts sample IDs
func (d *GEODownloader) extractSamplesFromMatrixFile(ctx context.Context, seriesID string) ([]string, error) {
	// Series matrix URL pattern
	matrixURL := fmt.Sprintf("%s/series/%s/%s/matrix/%s_series_matrix.txt.gz", d.ftpBaseURL, d.getGSESubdir(seriesID), seriesID, seriesID)

	// Rate limit requests
	d.rateLimitEUtils()

	req, err := http.NewRequestWithContext(ctx, "GET", matrixURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch series matrix file: HTTP %d", resp.StatusCode)
	}

	// Read first few KB to find sample IDs (they're at the beginning)
	buffer := make([]byte, 8192) // 8KB should be enough for sample headers
	n, err := resp.Body.Read(buffer)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read matrix file: %w", err)
	}

	content := string(buffer[:n])
	return d.extractSampleIDsFromMatrixContent(content), nil
}

// extractSampleIDsFromMatrixContent extracts GSM IDs from series matrix content
func (d *GEODownloader) extractSampleIDsFromMatrixContent(content string) []string {
	// Series matrix files have sample IDs in headers like:
	// !Sample_geo_accession	"GSM123456"	"GSM123457"	...
	gsmPattern := regexp.MustCompile(`GSM\d+`)
	matches := gsmPattern.FindAllString(content, -1)

	// Remove duplicates
	seen := make(map[string]bool)
	var samples []string
	for _, sample := range matches {
		if !seen[sample] {
			samples = append(samples, sample)
			seen[sample] = true
		}
	}

	return samples
}

// searchSamplesForSeries searches for samples that belong to a series
func (d *GEODownloader) searchSamplesForSeries(ctx context.Context, seriesID string) ([]string, error) {
	// Rate limit E-utilities requests
	d.rateLimitEUtils()

	// Search for samples that reference this series
	searchTerm := fmt.Sprintf("%s[Series Accession]", seriesID)

	params := url.Values{}
	params.Set("db", "gds")
	params.Set("term", searchTerm)
	params.Set("retmax", "100") // Limit to first 100 samples
	params.Set("tool", "hapiq")
	params.Set("email", "hapiq@example.com")

	searchURL := fmt.Sprintf("https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esearch.fcgi?%s", params.Encode())

	// Apply rate limiting before making request
	d.rateLimitEUtils()

	content, err := d.makeEUtilsRequest(ctx, searchURL)
	if err != nil {
		return nil, err
	}

	var response ESearchResponse
	if err := xml.Unmarshal(content, &response); err != nil {
		return nil, fmt.Errorf("failed to parse search response: %w", err)
	}

	// For each UID found, get its summary to extract the GSM ID
	var samples []string
	for _, uid := range response.IdList.IDs {
		summary, err := d.getSummary(ctx, "gds", uid)
		if err != nil {
			continue // Skip failed summaries
		}

		// Look for GSM accession in the summary
		for _, item := range summary.Items {
			if item.Name == "Accession" && strings.HasPrefix(item.Content, "GSM") {
				samples = append(samples, item.Content)
				break
			}
		}
	}

	return samples, nil
}

// extractSampleIDsFromSOFT extracts GSM IDs from SOFT format content (fallback method)
func (d *GEODownloader) extractSampleIDsFromSOFT(content string) []string {
	// Pattern to match GSM sample references in SOFT format
	gsmPattern := regexp.MustCompile(`\^SAMPLE\s*=\s*(GSM\d+)`)
	matches := gsmPattern.FindAllStringSubmatch(content, -1)

	seen := make(map[string]bool)
	var samples []string

	for _, match := range matches {
		if len(match) > 1 {
			sampleID := match[1]
			if !seen[sampleID] {
				samples = append(samples, sampleID)
				seen[sampleID] = true
			}
		}
	}

	return samples
}

// generateSampleFileURLs generates FTP URLs for sample files based on metadata
func (d *GEODownloader) generateSampleFileURLs(sampleID string, metadata *downloaders.Metadata) map[string]string {
	urls := make(map[string]string)

	// Base URL for sample supplementary files
	suppBaseURL := fmt.Sprintf("%s/samples/%s/%s/suppl/", d.ftpBaseURL, d.getGSMSubdir(sampleID), sampleID)

	// Always try to get the sample SOFT file (contains metadata)
	softFile := fmt.Sprintf("%s.soft.gz", sampleID)
	urls[softFile] = suppBaseURL + softFile

	// Common supplementary file patterns for samples
	commonPatterns := []string{
		fmt.Sprintf("%s.cel.gz", sampleID),
		fmt.Sprintf("%s.CEL.gz", sampleID),
		fmt.Sprintf("%s_processed.txt.gz", sampleID),
		fmt.Sprintf("%s.txt.gz", sampleID),
		fmt.Sprintf("%s.tar", sampleID),
		fmt.Sprintf("%s.tar.gz", sampleID),
	}

	for _, pattern := range commonPatterns {
		urls[pattern] = suppBaseURL + pattern
	}

	// Check metadata for additional file information from E-utilities
	if suppFiles, ok := metadata.Custom["supplementary_files"]; ok {
		if files, ok := suppFiles.([]string); ok {
			for _, file := range files {
				filename := filepath.Base(file)
				if filename != "" && filename != softFile {
					urls[filename] = suppBaseURL + filename
				}
			}
		} else if fileStr, ok := suppFiles.(string); ok && fileStr != "" {
			// Handle semicolon-separated file list from E-utilities
			files := strings.Split(fileStr, ";")
			for _, file := range files {
				filename := strings.TrimSpace(filepath.Base(file))
				if filename != "" && filename != softFile {
					urls[filename] = suppBaseURL + filename
				}
			}
		}
	}

	return urls
}

// downloadFileWithRetry downloads a file with retry logic for common network issues
func (d *GEODownloader) downloadFileWithRetry(ctx context.Context, url, targetPath string) (*downloaders.FileInfo, error) {
	maxRetries := 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		fileInfo, err := d.downloadFile(ctx, url, targetPath)
		if err == nil {
			return fileInfo, nil
		}

		lastErr = err

		// Check if it's a network error worth retrying
		if attempt < maxRetries && isRetryableError(err) {
			if d.verbose {
				fmt.Printf("🔄 Retry %d/%d for %s: %v\n", attempt, maxRetries, filepath.Base(url), err)
			}

			// Exponential backoff
			backoff := time.Duration(attempt) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
				continue
			}
		}

		break
	}

	return nil, lastErr
}

// isRetryableError determines if an error is worth retrying
func isRetryableError(err error) bool {
	errStr := err.Error()
	retryablePatterns := []string{
		"timeout",
		"connection reset",
		"temporary failure",
		"network is unreachable",
		"no such host",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(strings.ToLower(errStr), pattern) {
			return true
		}
	}

	return false
}

// shouldDownloadFile determines if a file should be downloaded based on options
func (d *GEODownloader) shouldDownloadFile(filename string, options *downloaders.DownloadOptions) bool {
	if options == nil {
		return true
	}

	filename = strings.ToLower(filename)

	// Check for raw data exclusion
	if !options.IncludeRaw {
		rawPatterns := []string{
			".fastq", ".fq", ".sra", ".bam", ".sam",
			"_raw", "raw_", ".cel",
		}
		for _, pattern := range rawPatterns {
			if strings.Contains(filename, pattern) {
				return false
			}
		}
	}

	// Check for supplementary exclusion
	if options.ExcludeSupplementary {
		suppPatterns := []string{
			"supplementary", "suppl", "readme", "filelist",
		}
		for _, pattern := range suppPatterns {
			if strings.Contains(filename, pattern) {
				return false
			}
		}
	}

	// Apply custom filters
	if options.CustomFilters != nil {
		for filterType, filterValue := range options.CustomFilters {
			switch filterType {
			case "extension":
				if !strings.HasSuffix(filename, filterValue) {
					return false
				}
			case "contains":
				if !strings.Contains(filename, filterValue) {
					return false
				}
			case "excludes":
				if strings.Contains(filename, filterValue) {
					return false
				}
			}
		}
	}

	return true
}

// getGSESubdir returns the FTP subdirectory for a GSE accession
func (d *GEODownloader) getGSESubdir(gseID string) string {
	// Extract number from GSE ID
	numberStr := strings.TrimPrefix(gseID, "GSE")
	if len(numberStr) < 3 {
		return "GSE000nnn"
	}

	// Create subdirectory based on number range
	// e.g., GSE123456 goes to GSE123nnn
	if len(numberStr) == 3 {
		return "GSE000nnn"
	}
	prefix := numberStr[:len(numberStr)-3]
	return "GSE" + prefix + "nnn"
}

// getGSMSubdir returns the FTP subdirectory for a GSM accession
func (d *GEODownloader) getGSMSubdir(gsmID string) string {
	// Extract number from GSM ID
	numberStr := strings.TrimPrefix(gsmID, "GSM")
	if len(numberStr) < 3 {
		return "GSM000nnn"
	}

	// Create subdirectory based on number range
	if len(numberStr) == 3 {
		return "GSM000nnn"
	}
	prefix := numberStr[:len(numberStr)-3]
	return "GSM" + prefix + "nnn"
}

// getGPLSubdir returns the FTP subdirectory for a GPL accession
func (d *GEODownloader) getGPLSubdir(gplID string) string {
	// Extract number from GPL ID
	numberStr := strings.TrimPrefix(gplID, "GPL")
	if len(numberStr) < 2 {
		return "GPL0nnn"
	}

	// Create subdirectory based on number range
	if len(numberStr) >= 3 {
		prefix := numberStr[:len(numberStr)-3]
		return "GPL" + prefix + "nnn"
	} else {
		return "GPL0nnn"
	}
}

// getGDSSubdir returns the FTP subdirectory for a GDS accession
func (d *GEODownloader) getGDSSubdir(gdsID string) string {
	// Extract number from GDS ID
	numberStr := strings.TrimPrefix(gdsID, "GDS")
	if len(numberStr) < 3 {
		return "GDS000nnn"
	}

	// Create subdirectory based on number range
	prefix := numberStr[:len(numberStr)-3]
	return "GDS" + prefix + "nnn"
}

// rateLimitEUtils implements rate limiting for E-utilities requests
func (d *GEODownloader) rateLimitEUtils() {
	now := time.Now()
	elapsed := now.Sub(lastEUtilsRequest)

	// Use different rate limits based on API key availability
	rateLimit := eUtilsRateLimit
	if d.apiKey != "" {
		rateLimit = eUtilsRateLimitWithAPIKey
	}

	if elapsed < rateLimit {
		time.Sleep(rateLimit - elapsed)
	}

	lastEUtilsRequest = time.Now()
}

// fetchPageContent is deprecated - keeping for backward compatibility but should not be used
// sampleHasFiles checks if a sample has individual supplementary files
func (d *GEODownloader) sampleHasFiles(ctx context.Context, sampleID string) bool {
	// Rate limit to avoid overwhelming the server
	d.rateLimitEUtils()

	sampleURL := fmt.Sprintf("%s/samples/%s/%s/", d.ftpBaseURL, d.getGSMSubdir(sampleID), sampleID)

	req, err := http.NewRequestWithContext(ctx, "HEAD", sampleURL, nil)
	if err != nil {
		return false
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()

	// If we can access the sample directory, assume it has files
	return resp.StatusCode == http.StatusOK
}

func (d *GEODownloader) fetchPageContent(ctx context.Context, url string) ([]byte, error) {
	// Rate limit requests
	d.rateLimitEUtils()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return content, nil
}

// getDirectoryListing attempts to parse an FTP directory listing to get filenames
func (d *GEODownloader) getDirectoryListing(ctx context.Context, dirURL string) ([]string, error) {
	// Rate limit requests
	d.rateLimitEUtils()

	req, err := http.NewRequestWithContext(ctx, "GET", dirURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse HTML directory listing to extract filenames
	return d.parseDirectoryListing(string(content))
}

// parseDirectoryListing extracts filenames from HTML directory listing
func (d *GEODownloader) parseDirectoryListing(htmlContent string) ([]string, error) {
	var files []string

	// Look for href links that point to files (not directories)
	lines := strings.Split(htmlContent, "\n")
	for _, line := range lines {
		// Look for <a href="filename"> pattern
		if strings.Contains(line, `<a href="`) {
			start := strings.Index(line, `<a href="`) + 9
			if start >= 9 {
				end := strings.Index(line[start:], `"`)
				if end > 0 {
					filename := line[start : start+end]
					// Skip parent directory links and directories (ending with /)
					if filename != "../" && !strings.HasSuffix(filename, "/") && filename != "" {
						files = append(files, filename)
					}
				}
			}
		}
	}

	return files, nil
}
