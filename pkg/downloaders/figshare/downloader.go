// Package figshare provides a downloader implementation for Figshare repository
// datasets, supporting articles, collections, and projects with comprehensive
// metadata extraction and file download capabilities.
package figshare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
)

// FigshareDownloader implements the Downloader interface for Figshare datasets.
type FigshareDownloader struct {
	client  *http.Client
	baseURL string
	apiURL  string
	timeout time.Duration
	verbose bool
}

// NewFigshareDownloader creates a new Figshare downloader.
func NewFigshareDownloader(options ...Option) *FigshareDownloader {
	d := &FigshareDownloader{
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: "https://figshare.com",
		apiURL:  "https://api.figshare.com/v2",
		timeout: 30 * time.Second,
		verbose: false,
	}

	for _, option := range options {
		option(d)
	}

	return d
}

// Option defines configuration options for FigshareDownloader.
type Option func(*FigshareDownloader)

// WithTimeout sets the HTTP timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(d *FigshareDownloader) {
		d.timeout = timeout
		d.client.Timeout = timeout
	}
}

// WithVerbose enables verbose logging.
func WithVerbose(verbose bool) Option {
	return func(d *FigshareDownloader) {
		d.verbose = verbose
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(d *FigshareDownloader) {
		d.client = client
	}
}

// GetSourceType returns the source type identifier.
func (d *FigshareDownloader) GetSourceType() string {
	return "figshare"
}

// Validate checks if the ID is a valid Figshare identifier.
func (d *FigshareDownloader) Validate(ctx context.Context, id string) (*downloaders.ValidationResult, error) {
	result := &downloaders.ValidationResult{
		ID:         id,
		SourceType: d.GetSourceType(),
		Valid:      false,
		Errors:     []string{},
		Warnings:   []string{},
	}

	// Clean the ID
	cleanID := d.cleanFigshareID(id)
	if cleanID != id {
		result.Warnings = append(result.Warnings, fmt.Sprintf("normalized ID from '%s' to '%s'", id, cleanID))
		result.ID = cleanID
	}

	// Validate format - Figshare uses numeric IDs
	if matched, _ := regexp.MatchString(`^\d+$`, cleanID); !matched {
		result.Errors = append(result.Errors, "invalid Figshare ID format (expected numeric ID)")
		return result, nil
	}

	// Convert to number for range validation
	idNum, err := strconv.ParseInt(cleanID, 10, 64)
	if err != nil {
		result.Errors = append(result.Errors, "invalid numeric ID")
		return result, nil
	}

	// Basic range validation
	if idNum <= 0 {
		result.Errors = append(result.Errors, "ID must be positive")
		return result, nil
	}

	if idNum > 99999999 { // Reasonable upper bound
		result.Warnings = append(result.Warnings, "very high ID number, dataset may not exist")
	}

	result.Valid = true

	return result, nil
}

// GetMetadata retrieves metadata for a Figshare dataset.
func (d *FigshareDownloader) GetMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	cleanID := d.cleanFigshareID(id)

	// Validate ID first
	validation, err := d.Validate(ctx, cleanID)
	if err != nil {
		return nil, err
	}

	if !validation.Valid {
		return nil, &downloaders.DownloaderError{
			Type:    "invalid_id",
			Message: strings.Join(validation.Errors, "; "),
			Source:  d.GetSourceType(),
			ID:      cleanID,
		}
	}

	// Try to get article metadata first

	metadata, err := d.getArticleMetadata(ctx, cleanID)
	if err == nil {
		return metadata, nil
	}

	// If article fails, try collection

	metadata, err = d.getCollectionMetadata(ctx, cleanID)
	if err == nil {
		return metadata, nil
	}

	// If both fail, try project

	metadata, err = d.getProjectMetadata(ctx, cleanID)
	if err == nil {
		return metadata, nil
	}

	return nil, &downloaders.DownloaderError{
		Type:    "not_found",
		Message: "dataset not found or inaccessible",
		Source:  d.GetSourceType(),
		ID:      cleanID,
	}
}

// Download performs the actual download operation.
func (d *FigshareDownloader) Download(ctx context.Context, req *downloaders.DownloadRequest) (*downloaders.DownloadResult, error) {
	startTime := time.Now()

	result := &downloaders.DownloadResult{
		Success:  false,
		Files:    []downloaders.FileInfo{},
		Duration: 0,
		Errors:   []string{},
		Warnings: []string{},
	}

	// Clean and validate ID
	cleanID := d.cleanFigshareID(req.ID)

	validation, err := d.Validate(ctx, cleanID)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	if !validation.Valid {
		result.Errors = append(result.Errors, strings.Join(validation.Errors, "; "))
		return result, nil
	}

	// Get metadata if not provided
	metadata := req.Metadata
	if metadata == nil {
		metadata, err = d.GetMetadata(ctx, cleanID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to get metadata: %v", err))
			return result, nil
		}
	}

	result.Metadata = metadata

	// Check directory and handle conflicts
	dirChecker := common.NewDirectoryChecker(req.OutputDir)

	dirStatus, err := dirChecker.CheckAndPrepare(cleanID)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("directory check failed: %v", err))
		return result, nil
	}

	nonInteractive := req.Options != nil && req.Options.NonInteractive

	action, err := common.HandleDirectoryConflicts(dirStatus, nonInteractive)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("conflict resolution failed: %v", err))
		return result, nil
	}

	switch action {
	case downloaders.ActionAbort:
		result.Errors = append(result.Errors, "download aborted by user")
		return result, nil
	case downloaders.ActionSkip:
		result.Warnings = append(result.Warnings, "download skipped due to existing directory")
		result.Success = true

		return result, nil
	case downloaders.ActionOverwrite:
		if err := os.RemoveAll(dirStatus.TargetPath); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to remove existing directory: %v", err))
			return result, nil
		}
	}

	// Create target directory
	targetDir := dirStatus.TargetPath
	if err := common.EnsureDirectory(targetDir); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to create directory: %v", err))
		return result, nil
	}

	// Handle collection confirmation
	if len(metadata.Collections) > 0 {
		collection := metadata.Collections[0]
		if !nonInteractive && !collection.UserConfirmed {
			confirmed, err := d.confirmCollection(ctx, &collection)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("collection confirmation failed: %v", err))
				return result, nil
			}

			if !confirmed {
				result.Warnings = append(result.Warnings, "download canceled by user")
				return result, nil
			}

			collection.UserConfirmed = true
			metadata.Collections[0] = collection
		}
	}

	// Determine dataset type and download accordingly
	datasetType := d.determineDatasetType(metadata)

	var downloadErr error

	switch datasetType {
	case "article":
		downloadErr = d.downloadArticle(ctx, cleanID, targetDir, req.Options, result)
	case "collection":
		downloadErr = d.downloadCollection(ctx, cleanID, targetDir, req.Options, result)
	case "project":
		downloadErr = d.downloadProject(ctx, cleanID, targetDir, req.Options, result)
	default:
		downloadErr = fmt.Errorf("unknown dataset type: %s", datasetType)
	}

	if downloadErr != nil {
		result.Errors = append(result.Errors, downloadErr.Error())
		return result, nil
	}

	// Calculate final statistics
	result.Duration = time.Since(startTime)
	for _, file := range result.Files {
		result.BytesDownloaded += file.Size
		result.BytesTotal += file.Size
	}

	// Determine success based on actual download results
	// Success means: no critical errors AND at least some files downloaded OR only warnings
	hasCriticalErrors := len(result.Errors) > 0
	hasDownloadedFiles := len(result.Files) > 0

	if hasCriticalErrors {
		result.Success = false
	} else if hasDownloadedFiles {
		result.Success = true
	} else if len(result.Warnings) > 0 {
		// If we have warnings but no errors and no files, it might be an empty dataset
		// This is considered a partial success
		result.Success = true
	} else {
		// No files, no warnings, no errors - this shouldn't happen but treat as failure
		result.Success = false
	}

	// Create witness file
	witness := &downloaders.WitnessFile{
		HapiqVersion: "dev", // This should come from version info
		DownloadTime: startTime,
		Source:       d.GetSourceType(),
		OriginalID:   req.ID,
		ResolvedURL:  d.buildFigshareURL(cleanID),
		Metadata:     metadata,
		Files:        make([]downloaders.FileWitness, len(result.Files)),
		Collections:  metadata.Collections,
		DownloadStats: &downloaders.DownloadStats{
			Duration:        result.Duration,
			BytesTotal:      result.BytesTotal,
			BytesDownloaded: result.BytesDownloaded,
			FilesTotal:      len(result.Files),
			FilesDownloaded: len(result.Files),
			FilesSkipped:    0,
			FilesFailed:     0,
			AverageSpeed:    float64(result.BytesDownloaded) / result.Duration.Seconds(),
			MaxConcurrent:   1,
			ResumedDownload: false,
		},
		Options: req.Options,
	}

	// Convert FileInfo to FileWitness
	for i, file := range result.Files {
		witness.Files[i] = downloaders.FileWitness(file)
	}

	// Write witness file
	if err := common.WriteWitnessFile(targetDir, witness); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to write witness file: %v", err))
	} else {
		result.WitnessFile = filepath.Join(targetDir, "hapiq.json")
	}

	return result, nil
}

// cleanFigshareID normalizes Figshare identifiers.
func (d *FigshareDownloader) cleanFigshareID(id string) string {
	// Remove whitespace
	cleaned := strings.TrimSpace(id)

	// Check if this is a sharing URL and resolve it
	if d.isSharingURL(cleaned) {
		if resolvedID, err := d.resolveSharingURL(cleaned); err == nil {
			return resolvedID
		}
		// If resolution fails, continue with normal processing
	}

	// Remove common prefixes
	prefixes := []string{
		"figshare:",
		"http://",
		"https://",
		"www.",
		"figshare.com/",
		"figshare.com/articles/",
		"figshare.com/articles/dataset/",
		"figshare.com/collections/",
		"figshare.com/projects/",
	}

	for _, prefix := range prefixes {
		if strings.HasPrefix(strings.ToLower(cleaned), prefix) {
			cleaned = cleaned[len(prefix):]
		}
	}

	// Extract ID from URL path
	if strings.Contains(cleaned, "/") {
		parts := strings.Split(cleaned, "/")
		// Look for numeric ID in URL parts
		for _, part := range parts {
			if matched, _ := regexp.MatchString(`^\d+$`, part); matched {
				cleaned = part
				break
			}
		}
	}

	// Remove any non-numeric characters
	re := regexp.MustCompile(`\d+`)

	matches := re.FindStringSubmatch(cleaned)
	if len(matches) > 0 {
		cleaned = matches[0]
	}

	return cleaned
}

// isSharingURL checks if the URL is a figshare sharing URL.
func (d *FigshareDownloader) isSharingURL(url string) bool {
	return strings.Contains(strings.ToLower(url), "figshare.com/s/")
}

// resolveSharingURL resolves a figshare sharing URL to get the actual article ID.
func (d *FigshareDownloader) resolveSharingURL(sharingURL string) (string, error) {
	if d.verbose {
		fmt.Printf("🔗 Resolving sharing URL: %s\n", sharingURL)
	}

	// Ensure we have a complete URL
	url := sharingURL
	if !strings.HasPrefix(strings.ToLower(url), "http") {
		url = "https://" + url
	}

	// Make HTTP request to the sharing URL
	req, err := http.NewRequest("GET", url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers to mimic a browser request
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; hapiq-figshare-downloader)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch sharing URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d when fetching sharing URL", resp.StatusCode)
	}

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	bodyStr := string(body)

	// Try multiple methods to extract the article ID
	if d.verbose {
		fmt.Printf("🔍 Searching for article ID in page content (%d bytes)\n", len(bodyStr))
	}

	// Method 1: Look for figshare API endpoints that contain article IDs
	apiPattern := regexp.MustCompile(`/v2/articles/(\d+)`)
	if matches := apiPattern.FindStringSubmatch(bodyStr); len(matches) > 1 {
		if d.verbose {
			fmt.Printf("✅ Resolved article ID from API endpoint: %s\n", matches[1])
		}

		return matches[1], nil
	}

	// Method 1.5: Look for download URLs in page content - most reliable method
	downloadUrlPattern := regexp.MustCompile(`figshare\.com/ndownloader/articles/(\d+)`)
	if matches := downloadUrlPattern.FindStringSubmatch(bodyStr); len(matches) > 1 {
		if d.verbose {
			fmt.Printf("✅ Resolved article ID from download URL: %s\n", matches[1])
		}

		return matches[1], nil
	}

	// Method 2: Look for download link with "Download all" - this is the most reliable
	downloadAllPattern := regexp.MustCompile(`(?i)href="[^"]*ndownloader/articles/(\d+)[^"]*"[^>]*>.*download.*all`)
	if matches := downloadAllPattern.FindStringSubmatch(bodyStr); len(matches) > 1 {
		if d.verbose {
			fmt.Printf("✅ Resolved article ID from 'Download all' link: %s\n", matches[1])
		}

		return matches[1], nil
	}

	// Method 3: Look for any ndownloader link (fallback)
	downloadPattern := regexp.MustCompile(`/ndownloader/articles/(\d+)`)
	if matches := downloadPattern.FindStringSubmatch(bodyStr); len(matches) > 1 {
		if d.verbose {
			fmt.Printf("✅ Resolved article ID from download link: %s\n", matches[1])
		}

		return matches[1], nil
	}

	// Method 4: Look for DOI pattern in the page
	doiPattern := regexp.MustCompile(`10\.6084/m9\.figshare\.(\d+)`)
	if matches := doiPattern.FindStringSubmatch(bodyStr); len(matches) > 1 {
		if d.verbose {
			fmt.Printf("✅ Resolved article ID from DOI: %s\n", matches[1])
		}

		return matches[1], nil
	}

	// Method 5: Look for direct article URL references
	articlePattern := regexp.MustCompile(`/articles/[^/]+/(\d+)`)
	if matches := articlePattern.FindStringSubmatch(bodyStr); len(matches) > 1 {
		if d.verbose {
			fmt.Printf("✅ Resolved article ID from URL: %s\n", matches[1])
		}

		return matches[1], nil
	}

	// Method 6: Look for Apollo state with private link article reference
	apolloPrivateLinkPattern := regexp.MustCompile(`"PrivateLink:\d+":\s*\{[^}]*"article":\s*\{[^}]*"id":\s*(\d+)`)
	if matches := apolloPrivateLinkPattern.FindStringSubmatch(bodyStr); len(matches) > 1 {
		if d.verbose {
			fmt.Printf("✅ Resolved article ID from Apollo PrivateLink: %s\n", matches[1])
		}

		return matches[1], nil
	}

	// Method 7: Look for citation or metadata with DOI-like patterns
	citationPattern := regexp.MustCompile(`(?i)cite.*?figshare\.(\d+)`)
	if matches := citationPattern.FindStringSubmatch(bodyStr); len(matches) > 1 {
		if d.verbose {
			fmt.Printf("✅ Resolved article ID from citation: %s\n", matches[1])
		}

		return matches[1], nil
	}

	// Method 8: Look for Apollo state or other JSON structures containing article ID
	apolloPattern := regexp.MustCompile(`"article":\s*\{\s*"id":\s*(\d+)`)
	if matches := apolloPattern.FindStringSubmatch(bodyStr); len(matches) > 1 {
		if d.verbose {
			fmt.Printf("✅ Resolved article ID from Apollo state: %s\n", matches[1])
		}

		return matches[1], nil
	}

	// Method 9: Try to find the largest numeric ID as it's likely the article ID
	largestIDPattern := regexp.MustCompile(`\b(\d{6,8})\b`)

	allMatches := largestIDPattern.FindAllStringSubmatch(bodyStr, -1)
	if len(allMatches) > 0 {
		var largestID int64

		var bestMatch string

		for _, match := range allMatches {
			if id, err := strconv.ParseInt(match[1], 10, 64); err == nil {
				// Filter out IDs that are too small or too large to be article IDs
				if id >= 1000000 && id <= 99999999 && id > largestID {
					largestID = id
					bestMatch = match[1]
				}
			}
		}

		if bestMatch != "" {
			if d.verbose {
				fmt.Printf("✅ Resolved article ID from largest ID pattern: %s\n", bestMatch)
			}

			return bestMatch, nil
		}
	}

	// Debug: Show what IDs we found if verbose
	if d.verbose {
		allIDPattern := regexp.MustCompile(`\d{6,8}`)
		allMatches := allIDPattern.FindAllString(bodyStr, 10)
		fmt.Printf("🔍 Found potential IDs: %v\n", allMatches)
		fmt.Printf("❌ Could not resolve sharing URL to article ID\n")
	}

	return "", fmt.Errorf("could not extract article ID from sharing URL")
}

// buildFigshareURL constructs the Figshare URL for an ID.
func (d *FigshareDownloader) buildFigshareURL(id string) string {
	return fmt.Sprintf("%s/articles/%s", d.baseURL, id)
}

// determineDatasetType determines if the dataset is an article, collection, or project.
func (d *FigshareDownloader) determineDatasetType(metadata *downloaders.Metadata) string {
	if len(metadata.Collections) > 0 {
		collectionType := metadata.Collections[0].Type
		if strings.Contains(collectionType, "collection") {
			return "collection"
		}

		if strings.Contains(collectionType, "project") {
			return "project"
		}
	}

	return "article"
}

// confirmCollection asks user for confirmation when downloading large collections.
func (d *FigshareDownloader) confirmCollection(ctx context.Context, collection *downloaders.Collection) (bool, error) {
	fmt.Printf("🔍 Detected %s:\n", collection.Type)
	fmt.Printf("   Title: %s\n", collection.Title)
	fmt.Printf("   Files: %d\n", collection.FileCount)
	fmt.Printf("   Estimated size: %s\n", common.FormatBytes(collection.EstimatedSize))

	if len(collection.Samples) > 0 {
		fmt.Printf("   Structure preview:\n")

		maxShow := 5
		for i, sample := range collection.Samples {
			if i >= maxShow {
				fmt.Printf("     ... and %d more\n", len(collection.Samples)-maxShow)
				break
			}

			fmt.Printf("     %s\n", sample)
		}
	}

	return common.AskUserConfirmation("Continue with download?")
}

// downloadFile downloads a single file with progress tracking.
func (d *FigshareDownloader) downloadFile(ctx context.Context, url, targetPath string) (*downloaders.FileInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Create target file
	file, err := os.Create(targetPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Copy with progress tracking
	downloadTime := time.Now()

	size, err := io.Copy(file, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to copy data: %w", err)
	}

	// Calculate checksum
	checksum, err := common.CalculateFileChecksum(targetPath)
	if err != nil {
		// Don't fail the download for checksum calculation errors
		checksum = ""
	}

	return &downloaders.FileInfo{
		Path:         targetPath,
		OriginalName: filepath.Base(targetPath),
		Size:         size,
		Checksum:     checksum,
		ChecksumType: "sha256",
		DownloadTime: downloadTime,
		SourceURL:    url,
		ContentType:  resp.Header.Get("Content-Type"),
	}, nil
}

// downloadFileWithProgressTracking downloads a file with real-time progress tracking.
func (d *FigshareDownloader) downloadFileWithProgressTracking(ctx context.Context, url, targetPath, filename string, size int64, tracker *common.ProgressTracker) (*downloaders.FileInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Create target file
	file, err := os.Create(targetPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Create progress reader
	progressReader := common.NewProgressReader(resp.Body, size, filename, tracker, d.verbose)
	defer progressReader.Close()

	// Copy with progress tracking
	downloadTime := time.Now()

	copiedSize, err := io.Copy(file, progressReader)
	if err != nil {
		return nil, fmt.Errorf("failed to copy data: %w", err)
	}

	// Calculate checksum
	checksum, err := common.CalculateFileChecksum(targetPath)
	if err != nil {
		// Don't fail the download for checksum calculation errors
		checksum = ""
	}

	return &downloaders.FileInfo{
		Path:         targetPath,
		OriginalName: filepath.Base(targetPath),
		Size:         copiedSize,
		Checksum:     checksum,
		ChecksumType: "sha256",
		DownloadTime: downloadTime,
		SourceURL:    url,
		ContentType:  resp.Header.Get("Content-Type"),
	}, nil
}

// apiRequest makes a request to the Figshare API.
func (d *FigshareDownloader) apiRequest(ctx context.Context, endpoint string, result interface{}) error {
	url := fmt.Sprintf("%s/%s", d.apiURL, endpoint)

	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API request failed: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return json.Unmarshal(body, result)
}
