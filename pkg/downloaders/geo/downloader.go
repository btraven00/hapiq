// Package geo provides a downloader implementation for NCBI Gene Expression Omnibus (GEO)
// datasets, supporting series (GSE), samples (GSM), platforms (GPL), and datasets (GDS).
package geo

import (
	"context"
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

// GEODownloader implements the Downloader interface for GEO datasets.
type GEODownloader struct {
	client     *http.Client
	baseURL    string
	ftpBaseURL string
	apiKey     string
	timeout    time.Duration
	verbose    bool
}

// NewGEODownloader creates a new GEO downloader.
func NewGEODownloader(options ...Option) *GEODownloader {
	d := &GEODownloader{
		client:     &http.Client{Timeout: 30 * time.Second},
		baseURL:    "https://www.ncbi.nlm.nih.gov/geo",
		ftpBaseURL: "https://ftp.ncbi.nlm.nih.gov/geo",
		timeout:    30 * time.Second,
		verbose:    false,
		apiKey:     "", // Can be set via environment variable or option
	}

	for _, option := range options {
		option(d)
	}

	return d
}

// Option defines configuration options for GEODownloader.
type Option func(*GEODownloader)

// WithTimeout sets the HTTP timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(d *GEODownloader) {
		d.timeout = timeout
		d.client.Timeout = timeout
	}
}

// WithVerbose enables verbose logging.
func WithVerbose(verbose bool) Option {
	return func(d *GEODownloader) {
		d.verbose = verbose
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(d *GEODownloader) {
		d.client = client
	}
}

// WithAPIKey sets the NCBI API key for increased rate limits (10 req/sec instead of 3).
func WithAPIKey(apiKey string) Option {
	return func(d *GEODownloader) {
		d.apiKey = apiKey
	}
}

// GetSourceType returns the source type identifier.
func (d *GEODownloader) GetSourceType() string {
	return "geo"
}

// Validate checks if the ID is a valid GEO accession.
func (d *GEODownloader) Validate(ctx context.Context, id string) (*downloaders.ValidationResult, error) {
	result := &downloaders.ValidationResult{
		ID:         id,
		SourceType: d.GetSourceType(),
		Valid:      false,
		Errors:     []string{},
		Warnings:   []string{},
	}

	// Clean the ID
	cleanID := d.cleanGEOID(id)
	if cleanID != id {
		result.Warnings = append(result.Warnings, fmt.Sprintf("normalized ID from '%s' to '%s'", id, cleanID))
		result.ID = cleanID
	}

	// Validate format using regex
	geoPattern := regexp.MustCompile(`^(GSE|GSM|GPL|GDS)\d+$`)
	if !geoPattern.MatchString(cleanID) {
		result.Errors = append(result.Errors, "invalid GEO accession format (expected GSE, GSM, GPL, or GDS followed by digits)")
		return result, nil
	}

	// Extract type and number
	geoType := cleanID[:3]
	numberStr := cleanID[3:]

	number, err := strconv.Atoi(numberStr)
	if err != nil {
		result.Errors = append(result.Errors, "invalid accession number")
		return result, nil
	}

	// Basic range validation
	if number <= 0 {
		result.Errors = append(result.Errors, "accession number must be positive")
		return result, nil
	}

	// Warn about very old or potentially invalid accessions
	if number < 100 {
		result.Warnings = append(result.Warnings, "very low accession number, dataset may not exist")
	}

	// Check for unsupported types (we primarily support GSE for now)
	if geoType != "GSE" {
		result.Warnings = append(result.Warnings, fmt.Sprintf("type '%s' has limited support, GSE (series) is recommended", geoType))
	}

	result.Valid = true

	return result, nil
}

// GetMetadata retrieves metadata for a GEO dataset.
func (d *GEODownloader) GetMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	cleanID := d.cleanGEOID(id)

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

	geoType := cleanID[:3]

	switch geoType {
	case "GSE":
		return d.getSeriesMetadata(ctx, cleanID)
	case "GSM":
		return d.getSampleMetadata(ctx, cleanID)
	case "GPL":
		return d.getPlatformMetadata(ctx, cleanID)
	case "GDS":
		return d.getDatasetMetadata(ctx, cleanID)
	default:
		return nil, &downloaders.DownloaderError{
			Type:    "unsupported_type",
			Message: fmt.Sprintf("unsupported GEO type: %s", geoType),
			Source:  d.GetSourceType(),
			ID:      cleanID,
		}
	}
}

// Download performs the actual download operation.
func (d *GEODownloader) Download(ctx context.Context, req *downloaders.DownloadRequest) (*downloaders.DownloadResult, error) {
	startTime := time.Now()

	result := &downloaders.DownloadResult{
		Success:  false,
		Files:    []downloaders.FileInfo{},
		Duration: 0,
		Errors:   []string{},
		Warnings: []string{},
	}

	// Clean and validate ID
	cleanID := d.cleanGEOID(req.ID)

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

	// Handle collection confirmation for series
	geoType := cleanID[:3]
	if geoType == "GSE" && len(metadata.Collections) > 0 {
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

	// Perform the actual download based on type
	var downloadErr error

	switch geoType {
	case "GSE":
		downloadErr = d.downloadSeries(ctx, cleanID, targetDir, req.Options, result)
	case "GSM":
		downloadErr = d.downloadSample(ctx, cleanID, targetDir, req.Options, result)
	case "GPL":
		downloadErr = d.downloadPlatform(ctx, cleanID, targetDir, req.Options, result)
	case "GDS":
		downloadErr = d.downloadDataset(ctx, cleanID, targetDir, req.Options, result)
	default:
		downloadErr = fmt.Errorf("unsupported GEO type: %s", geoType)
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
		ResolvedURL:  d.buildGEOURL(cleanID),
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

// cleanGEOID normalizes GEO identifiers.
func (d *GEODownloader) cleanGEOID(id string) string {
	// Remove whitespace
	cleaned := strings.TrimSpace(id)

	// Convert to uppercase
	cleaned = strings.ToUpper(cleaned)

	// Remove common prefixes/suffixes
	prefixes := []string{"GEO:", "NCBI:", "HTTP://", "HTTPS://"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(strings.ToUpper(cleaned), prefix) {
			cleaned = cleaned[len(prefix):]
		}
	}

	// Extract accession from URL if present
	if strings.Contains(cleaned, "ACC=") {
		parts := strings.Split(cleaned, "ACC=")
		if len(parts) > 1 {
			cleaned = strings.Split(parts[1], "&")[0]
		}
	}

	// Remove any remaining non-alphanumeric characters except the accession format
	accessionPattern := regexp.MustCompile(`(GSE|GSM|GPL|GDS)\d+`)

	matches := accessionPattern.FindStringSubmatch(cleaned)
	if len(matches) > 0 {
		return matches[0]
	}

	// Return empty string if no valid GEO pattern found
	return ""
}

// buildGEOURL constructs the GEO URL for an accession.
func (d *GEODownloader) buildGEOURL(id string) string {
	return fmt.Sprintf("%s/query/acc.cgi?acc=%s", d.baseURL, id)
}

// confirmCollection asks user for confirmation when downloading large collections.
func (d *GEODownloader) confirmCollection(ctx context.Context, collection *downloaders.Collection) (bool, error) {
	fmt.Printf("🔍 Detected %s collection:\n", collection.Type)
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
func (d *GEODownloader) downloadFile(ctx context.Context, url, targetPath string) (*downloaders.FileInfo, error) {
	return d.downloadFileWithProgress(ctx, url, targetPath, filepath.Base(targetPath), -1, nil)
}

// downloadFileWithProgress downloads a file with optional progress tracking.
func (d *GEODownloader) downloadFileWithProgress(ctx context.Context, url, targetPath, filename string, size int64, tracker *common.ProgressTracker) (*downloaders.FileInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
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

	// Get content length if size wasn't provided
	if size <= 0 && resp.ContentLength > 0 {
		size = resp.ContentLength
	}

	// Create target file
	file, err := os.Create(targetPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	downloadTime := time.Now()

	var copiedSize int64

	// Use progress reader if tracker is available and size is known
	if tracker != nil && size > 0 {
		progressReader := common.NewProgressReader(resp.Body, size, filename, tracker, d.verbose)
		defer progressReader.Close()
		copiedSize, err = io.Copy(file, progressReader)
	} else {
		// Fallback to simple copy
		copiedSize, err = io.Copy(file, resp.Body)
	}

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
