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

// FigshareDownloader implements the Downloader interface for Figshare datasets
type FigshareDownloader struct {
	client  *http.Client
	baseURL string
	apiURL  string
	timeout time.Duration
	verbose bool
}

// NewFigshareDownloader creates a new Figshare downloader
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

// Option defines configuration options for FigshareDownloader
type Option func(*FigshareDownloader)

// WithTimeout sets the HTTP timeout
func WithTimeout(timeout time.Duration) Option {
	return func(d *FigshareDownloader) {
		d.timeout = timeout
		d.client.Timeout = timeout
	}
}

// WithVerbose enables verbose logging
func WithVerbose(verbose bool) Option {
	return func(d *FigshareDownloader) {
		d.verbose = verbose
	}
}

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(client *http.Client) Option {
	return func(d *FigshareDownloader) {
		d.client = client
	}
}

// GetSourceType returns the source type identifier
func (d *FigshareDownloader) GetSourceType() string {
	return "figshare"
}

// Validate checks if the ID is a valid Figshare identifier
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

// GetMetadata retrieves metadata for a Figshare dataset
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

// Download performs the actual download operation
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
				result.Warnings = append(result.Warnings, "download cancelled by user")
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
		witness.Files[i] = downloaders.FileWitness{
			Path:         file.Path,
			OriginalName: file.OriginalName,
			Size:         file.Size,
			Checksum:     file.Checksum,
			ChecksumType: file.ChecksumType,
			DownloadTime: file.DownloadTime,
			SourceURL:    file.SourceURL,
			ContentType:  file.ContentType,
		}
	}

	// Write witness file
	if err := common.WriteWitnessFile(targetDir, witness); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to write witness file: %v", err))
	} else {
		result.WitnessFile = filepath.Join(targetDir, "hapiq.json")
	}

	result.Success = true
	return result, nil
}

// cleanFigshareID normalizes Figshare identifiers
func (d *FigshareDownloader) cleanFigshareID(id string) string {
	// Remove whitespace
	cleaned := strings.TrimSpace(id)

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

// buildFigshareURL constructs the Figshare URL for an ID
func (d *FigshareDownloader) buildFigshareURL(id string) string {
	return fmt.Sprintf("%s/articles/%s", d.baseURL, id)
}

// determineDatasetType determines if the dataset is an article, collection, or project
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

// confirmCollection asks user for confirmation when downloading large collections
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

// downloadFile downloads a single file with progress tracking
func (d *FigshareDownloader) downloadFile(ctx context.Context, url, targetPath string) (*downloaders.FileInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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

// apiRequest makes a request to the Figshare API
func (d *FigshareDownloader) apiRequest(ctx context.Context, endpoint string, result interface{}) error {
	url := fmt.Sprintf("%s/%s", d.apiURL, endpoint)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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
