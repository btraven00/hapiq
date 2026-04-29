// Package zenodo provides downloading functionality for Zenodo datasets
// with comprehensive API integration and metadata tracking.
package zenodo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
)

// ZenodoArtifactType represents the type of Zenodo artifact
type ZenodoArtifactType int

const (
	ArtifactTypeRecord ZenodoArtifactType = iota
	ArtifactTypeDeposit
	ArtifactTypeCommunity
	ArtifactTypeConcept
	ArtifactTypeUnknown
)

func (t ZenodoArtifactType) String() string {
	switch t {
	case ArtifactTypeRecord:
		return "record"
	case ArtifactTypeDeposit:
		return "deposit"
	case ArtifactTypeCommunity:
		return "community"
	case ArtifactTypeConcept:
		return "concept"
	default:
		return "unknown"
	}
}

// ZenodoIdentifier represents a parsed Zenodo identifier
type ZenodoIdentifier struct {
	ID           string
	Type         ZenodoArtifactType
	IsVersioned  bool
	OriginalText string
}

// ZenodoDownloader handles downloading from Zenodo repository.
type ZenodoDownloader struct {
	client  *http.Client
	baseURL string
	apiURL  string
	timeout time.Duration
	verbose bool
}

// NewZenodoDownloader creates a new Zenodo downloader.
func NewZenodoDownloader(options ...Option) *ZenodoDownloader {
	d := &ZenodoDownloader{
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: "https://zenodo.org",
		apiURL:  "https://zenodo.org/api/records",
		timeout: 30 * time.Second,
		verbose: false,
	}

	for _, option := range options {
		option(d)
	}

	return d
}

// Option defines configuration options for ZenodoDownloader.
type Option func(*ZenodoDownloader)

// WithTimeout sets the HTTP timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(d *ZenodoDownloader) {
		d.timeout = timeout
		d.client.Timeout = timeout
	}
}

// WithVerbose enables verbose logging.
func WithVerbose(verbose bool) Option {
	return func(d *ZenodoDownloader) {
		d.verbose = verbose
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(d *ZenodoDownloader) {
		d.client = client
	}
}

// GetSourceType returns the source type identifier.
func (d *ZenodoDownloader) GetSourceType() string {
	return "zenodo"
}

// Validate checks if the ID is valid for Zenodo and provides detailed artifact type information.
func (d *ZenodoDownloader) Validate(ctx context.Context, id string) (*downloaders.ValidationResult, error) {
	result := &downloaders.ValidationResult{
		ID:         id,
		SourceType: d.GetSourceType(),
		Valid:      false,
	}

	// Parse and validate the identifier
	identifier, err := d.parseZenodoIdentifier(id)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	// Update with parsed ID
	result.ID = identifier.ID

	// Add warnings for non-downloadable artifact types
	switch identifier.Type {
	case ArtifactTypeDeposit:
		result.Warnings = append(result.Warnings, "This is a deposit (unpublished draft) - not available for download")
		result.Errors = append(result.Errors, "deposits cannot be downloaded")
		return result, nil
	case ArtifactTypeCommunity:
		result.Warnings = append(result.Warnings, "This is a community (organizational space) - download individual records instead")
		result.Errors = append(result.Errors, "communities cannot be downloaded directly")
		return result, nil
	case ArtifactTypeConcept:
		result.Warnings = append(result.Warnings, "This is a concept DOI (version group) - will resolve to latest version")
	}

	// Try to fetch metadata to verify the artifact exists and is accessible
	_, err = d.getArtifactMetadata(ctx, identifier)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			result.Errors = append(result.Errors, fmt.Sprintf("%s not found", identifier.Type.String()))
		} else if strings.Contains(err.Error(), "403") {
			result.Errors = append(result.Errors, fmt.Sprintf("access denied - %s may be private or restricted", identifier.Type.String()))
		} else {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to validate %s: %v", identifier.Type.String(), err))
		}
		return result, nil
	}

	result.Valid = true
	return result, nil
}

// GetMetadata retrieves dataset metadata from Zenodo with enhanced artifact type support.
func (d *ZenodoDownloader) GetMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	identifier, err := d.parseZenodoIdentifier(id)
	if err != nil {
		return nil, fmt.Errorf("invalid Zenodo identifier: %w", err)
	}

	// Reject non-downloadable types early
	switch identifier.Type {
	case ArtifactTypeDeposit:
		return nil, fmt.Errorf("deposits (unpublished drafts) cannot be downloaded")
	case ArtifactTypeCommunity:
		return nil, fmt.Errorf("communities are organizational spaces - specify individual records for download")
	}

	record, err := d.getArtifactMetadata(ctx, identifier)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata: %w", err)
	}

	metadata := d.convertToMetadata(record, identifier.ID)

	// Add artifact type information to custom metadata
	if metadata.Custom == nil {
		metadata.Custom = make(map[string]any)
	}
	metadata.Custom["artifact_type"] = identifier.Type.String()
	metadata.Custom["is_versioned"] = identifier.IsVersioned
	metadata.Custom["original_identifier"] = identifier.OriginalText

	return metadata, nil
}

// Download performs the actual download with progress tracking.
func (d *ZenodoDownloader) Download(ctx context.Context, req *downloaders.DownloadRequest) (*downloaders.DownloadResult, error) {
	startTime := time.Now()

	result := &downloaders.DownloadResult{
		Metadata: req.Metadata,
		Files:    []downloaders.FileInfo{},
		Success:  false,
		Duration: 0,
	}

	// Get the record metadata if not provided
	var record *ZenodoRecord
	var err error
	if req.Metadata == nil {
		record, err = d.getRecordMetadata(ctx, req.ID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to get metadata: %v", err))
			result.Duration = time.Since(startTime)
			return result, nil
		}
		result.Metadata = d.convertToMetadata(record, req.ID)
	} else {
		// Still get the record for file information
		record, err = d.getRecordMetadata(ctx, req.ID)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to get record details: %v", err))
			result.Duration = time.Since(startTime)
			return result, nil
		}
	}

	// Create output directory
	if err := os.MkdirAll(req.OutputDir, 0o750); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("failed to create output directory: %v", err))
		result.Duration = time.Since(startTime)
		return result, nil
	}

	// Filter files based on options
	filesToDownload := d.filterFiles(record.Files, req.Options)
	if len(filesToDownload) == 0 {
		result.Warnings = append(result.Warnings, "no files match the download criteria")
		result.Success = true
		result.Duration = time.Since(startTime)
		return result, nil
	}

	// Calculate total bytes
	var totalBytes int64
	for _, file := range filesToDownload {
		totalBytes += file.Size
		result.BytesTotal += file.Size
	}

	// Download files
	for _, file := range filesToDownload {
		if d.verbose {
			_, _ = fmt.Fprintf(os.Stderr, "Downloading: %s (%s)\n", file.Key, common.FormatBytes(file.Size))
		}

		fileInfo, err := d.downloadFile(ctx, file, req.OutputDir, req.Options)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to download %s: %v", file.Key, err))
			continue
		}

		result.Files = append(result.Files, *fileInfo)
		result.BytesDownloaded += fileInfo.Size
	}

	// Create witness file
	witness := d.createWitnessFile(result, req)
	if err := common.WriteWitnessFile(req.OutputDir, witness); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to create witness file: %v", err))
	} else {
		result.WitnessFile = filepath.Join(req.OutputDir, "hapiq.json")
	}

	result.Success = len(result.Errors) == 0
	result.Duration = time.Since(startTime)

	return result, nil
}

// parseZenodoIdentifier extracts and validates Zenodo identifiers, detecting artifact type.
func (d *ZenodoDownloader) parseZenodoIdentifier(input string) (*ZenodoIdentifier, error) {
	if input == "" {
		return nil, fmt.Errorf("empty input")
	}

	input = strings.TrimSpace(input)

	identifier := &ZenodoIdentifier{
		OriginalText: input,
		Type:         ArtifactTypeUnknown,
		IsVersioned:  false,
	}

	// Pattern 1: Direct numeric ID (assume record unless context suggests otherwise)
	if matched, _ := regexp.MatchString(`^\d+$`, input); matched {
		identifier.ID = input
		identifier.Type = ArtifactTypeRecord
		return identifier, nil
	}

	// Pattern 2: Zenodo record URL
	recordURLPattern := regexp.MustCompile(`https?://zenodo\.org/record/(\d+)`)
	if matches := recordURLPattern.FindStringSubmatch(input); len(matches) > 1 {
		identifier.ID = matches[1]
		identifier.Type = ArtifactTypeRecord
		return identifier, nil
	}

	// Pattern 3: Zenodo deposit URL
	depositURLPattern := regexp.MustCompile(`https?://zenodo\.org/deposit/(\d+)`)
	if matches := depositURLPattern.FindStringSubmatch(input); len(matches) > 1 {
		identifier.ID = matches[1]
		identifier.Type = ArtifactTypeDeposit
		return identifier, nil
	}

	// Pattern 4: Zenodo community URL
	communityURLPattern := regexp.MustCompile(`https?://zenodo\.org/communities/([^/?]+)`)
	if matches := communityURLPattern.FindStringSubmatch(input); len(matches) > 1 {
		identifier.ID = matches[1]
		identifier.Type = ArtifactTypeCommunity
		return identifier, nil
	}

	// Pattern 5: Versioned DOI (10.5281/zenodo.123456.v1) - check this first
	versionedDOIPattern := regexp.MustCompile(`10\.5281/zenodo\.(\d+)\.v\d+`)
	if matches := versionedDOIPattern.FindStringSubmatch(input); len(matches) > 1 {
		identifier.ID = matches[1]
		identifier.Type = ArtifactTypeConcept
		identifier.IsVersioned = true
		return identifier, nil
	}

	// Pattern 6: Zenodo DOI (10.5281/zenodo.123456)
	zenodoDOIPattern := regexp.MustCompile(`10\.5281/zenodo\.(\d+)`)
	if matches := zenodoDOIPattern.FindStringSubmatch(input); len(matches) > 1 {
		identifier.ID = matches[1]
		identifier.Type = ArtifactTypeRecord
		return identifier, nil
	}

	// Pattern 7: DOI URL (https://doi.org/10.5281/zenodo.123456)
	doiURLPattern := regexp.MustCompile(`https?://doi\.org/10\.5281/zenodo\.(\d+)`)
	if matches := doiURLPattern.FindStringSubmatch(input); len(matches) > 1 {
		identifier.ID = matches[1]
		identifier.Type = ArtifactTypeRecord
		return identifier, nil
	}

	return nil, fmt.Errorf("unrecognized Zenodo identifier format: %s", input)
}

// cleanZenodoID maintains backward compatibility - extracts ID for records only
func (d *ZenodoDownloader) cleanZenodoID(input string) (string, error) {
	identifier, err := d.parseZenodoIdentifier(input)
	if err != nil {
		return "", err
	}

	// Only support records for backward compatibility
	if identifier.Type != ArtifactTypeRecord && identifier.Type != ArtifactTypeConcept {
		return "", fmt.Errorf("unsupported artifact type '%s' for identifier: %s", identifier.Type.String(), input)
	}

	return identifier.ID, nil
}

// getArtifactMetadata fetches metadata for any Zenodo artifact type.
func (d *ZenodoDownloader) getArtifactMetadata(ctx context.Context, identifier *ZenodoIdentifier) (*ZenodoRecord, error) {
	var url string

	switch identifier.Type {
	case ArtifactTypeRecord:
		url = fmt.Sprintf("%s/%s", d.apiURL, identifier.ID)
	case ArtifactTypeConcept:
		// For concepts, get the latest version
		url = fmt.Sprintf("%s/%s", d.apiURL, identifier.ID)
	case ArtifactTypeDeposit:
		return nil, fmt.Errorf("deposits are not yet published and cannot be downloaded")
	case ArtifactTypeCommunity:
		return nil, fmt.Errorf("communities are organizational spaces and cannot be downloaded directly - please specify a record within the community")
	default:
		return nil, fmt.Errorf("unsupported artifact type: %s", identifier.Type.String())
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "hapiq/1.0")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, d.formatNotFoundError(identifier)
	}
	if resp.StatusCode == 403 {
		return nil, fmt.Errorf("access denied (403) - %s may be private or restricted", identifier.Type.String())
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API returned status %d for %s %s", resp.StatusCode, identifier.Type.String(), identifier.ID)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var record ZenodoRecord
	if err := json.Unmarshal(body, &record); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	return &record, nil
}

// getRecordMetadata fetches record metadata from Zenodo API (backward compatibility).
func (d *ZenodoDownloader) getRecordMetadata(ctx context.Context, id string) (*ZenodoRecord, error) {
	identifier := &ZenodoIdentifier{
		ID:           id,
		Type:         ArtifactTypeRecord,
		OriginalText: id,
	}
	return d.getArtifactMetadata(ctx, identifier)
}

// formatNotFoundError provides context-aware error messages for different artifact types.
func (d *ZenodoDownloader) formatNotFoundError(identifier *ZenodoIdentifier) error {
	switch identifier.Type {
	case ArtifactTypeRecord:
		return fmt.Errorf("record not found (404) - record %s may not exist or may be private", identifier.ID)
	case ArtifactTypeConcept:
		return fmt.Errorf("concept not found (404) - concept %s may not exist or may be private", identifier.ID)
	case ArtifactTypeDeposit:
		return fmt.Errorf("deposit not found (404) - deposit %s may not exist or you may lack access", identifier.ID)
	case ArtifactTypeCommunity:
		return fmt.Errorf("community not found (404) - community '%s' may not exist", identifier.ID)
	default:
		return fmt.Errorf("artifact not found (404)")
	}
}

// convertToMetadata converts a ZenodoRecord to downloaders.Metadata.
func (d *ZenodoDownloader) convertToMetadata(record *ZenodoRecord, id string) *downloaders.Metadata {
	metadata := &downloaders.Metadata{
		Source:      d.GetSourceType(),
		ID:          id,
		Title:       record.Metadata.Title,
		Description: record.Metadata.Description,
		DOI:         record.DOI,
		License:     record.Metadata.License.ID,
		Version:     record.Metadata.Version,
		FileCount:   len(record.Files),
		Custom:      make(map[string]any),
	}

	// Parse dates
	if created, err := time.Parse("2006-01-02T15:04:05.000000+00:00", record.Created); err == nil {
		metadata.Created = created
	} else if created, err := time.Parse("2006-01-02T15:04:05Z", record.Created); err == nil {
		metadata.Created = created
	}

	if modified, err := time.Parse("2006-01-02T15:04:05.000000+00:00", record.Modified); err == nil {
		metadata.LastModified = modified
	} else if modified, err := time.Parse("2006-01-02T15:04:05Z", record.Modified); err == nil {
		metadata.LastModified = modified
	}

	// Extract creators as authors
	var authors []string
	for _, creator := range record.Metadata.Creators {
		authors = append(authors, creator.Name)
	}
	metadata.Authors = authors

	// Extract keywords and subjects
	if len(record.Metadata.Keywords) > 0 {
		metadata.Keywords = record.Metadata.Keywords
	}

	var tags []string
	for _, subject := range record.Metadata.Subjects {
		tags = append(tags, subject.Term)
	}
	metadata.Tags = tags

	// Calculate total size
	var totalSize int64
	for _, file := range record.Files {
		totalSize += file.Size
	}
	metadata.TotalSize = totalSize

	// Add custom fields
	metadata.Custom["conceptdoi"] = record.ConceptDOI
	metadata.Custom["conceptrecid"] = record.ConceptRecID
	metadata.Custom["state"] = record.State
	metadata.Custom["submitted"] = record.Submitted
	metadata.Custom["published"] = record.Metadata.PublicationDate

	if record.Metadata.License.URL != "" {
		metadata.Custom["license_url"] = record.Metadata.License.URL
	}

	// Add resource type
	if record.Metadata.ResourceType.Type != "" {
		metadata.Custom["resource_type"] = record.Metadata.ResourceType.Type
		metadata.Custom["resource_subtype"] = record.Metadata.ResourceType.Subtype
	}

	// Add communities
	if len(record.Metadata.Communities) > 0 {
		var communities []string
		for _, community := range record.Metadata.Communities {
			communities = append(communities, community.ID)
		}
		metadata.Custom["communities"] = communities
	}

	// Add related identifiers
	if len(record.Metadata.RelatedIdentifiers) > 0 {
		metadata.Custom["related_identifiers"] = record.Metadata.RelatedIdentifiers
	}

	// Add file information
	var fileInfos []map[string]interface{}
	for _, file := range record.Files {
		fileInfo := map[string]interface{}{
			"id":       file.ID,
			"key":      file.Key,
			"size":     file.Size,
			"checksum": file.Checksum,
			"type":     file.Type,
		}
		if file.Links.Self != "" {
			fileInfo["download_url"] = file.Links.Self
		}
		fileInfos = append(fileInfos, fileInfo)
	}
	metadata.Custom["files"] = fileInfos

	return metadata
}

// filterFiles filters files based on download options.
func (d *ZenodoDownloader) filterFiles(files []ZenodoFile, options *downloaders.DownloadOptions) []ZenodoFile {
	if options == nil {
		return files
	}

	var filtered []ZenodoFile
	for _, file := range files {
		if d.shouldDownloadFile(file, options) {
			filtered = append(filtered, file)
		}
	}
	return filtered
}

// shouldDownloadFile determines if a file should be downloaded based on options.
func (d *ZenodoDownloader) shouldDownloadFile(file ZenodoFile, options *downloaders.DownloadOptions) bool {
	if options == nil {
		return true
	}
	return downloaders.ShouldDownload(file.Key, file.Size, options)
}

// downloadFile downloads a single file from Zenodo.
func (d *ZenodoDownloader) downloadFile(ctx context.Context, file ZenodoFile, outputDir string, options *downloaders.DownloadOptions) (*downloaders.FileInfo, error) {
	if file.Links.Self == "" {
		return nil, fmt.Errorf("no download URL available for file %s", file.Key)
	}

	outputPath := filepath.Join(outputDir, file.Key)

	if options != nil && options.SkipExisting {
		if _, err := os.Stat(outputPath); err == nil {
			if d.verbose {
				_, _ = fmt.Fprintf(os.Stderr, "Skipping existing file: %s\n", file.Key)
			}
			return &downloaders.FileInfo{
				Path:         outputPath,
				OriginalName: file.Key,
				SourceURL:    file.Links.Self,
				Size:         file.Size,
				DownloadTime: time.Now(),
			}, nil
		}
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	result, err := common.Fetch(ctx, file.Links.Self, outputPath, common.FetchOptions{
		Client:       d.client,
		ExtraHeaders: map[string]string{"User-Agent": "hapiq/1.0"},
	})
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", file.Key, err)
	}

	return &downloaders.FileInfo{
		Path:         outputPath,
		OriginalName: file.Key,
		SourceURL:    file.Links.Self,
		ContentType:  result.ContentType,
		Size:         result.N,
		Checksum:     result.SHA256,
		ChecksumType: "sha256",
		DownloadTime: time.Now(),
		CacheHit:     result.Hit,
	}, nil
}

// createWitnessFile creates a witness file for provenance tracking.
func (d *ZenodoDownloader) createWitnessFile(result *downloaders.DownloadResult, req *downloaders.DownloadRequest) *downloaders.WitnessFile {
	witness := &downloaders.WitnessFile{
		HapiqVersion: "1.0", // This should ideally come from a version constant
		Source:       d.GetSourceType(),
		OriginalID:   req.ID,
		ResolvedURL:  fmt.Sprintf("%s/record/%s", d.baseURL, req.ID),
		DownloadTime: time.Now(),
		Metadata:     result.Metadata,
		Options:      req.Options,
		Collections:  result.Collections,
		DownloadStats: &downloaders.DownloadStats{
			Duration:        result.Duration,
			BytesTotal:      result.BytesTotal,
			BytesDownloaded: result.BytesDownloaded,
			FilesTotal:      len(result.Files),
			FilesDownloaded: len(result.Files),
			FilesSkipped:    0, // TODO: Track this properly
			FilesFailed:     len(result.Errors),
			AverageSpeed:    downloaders.Speed(result.BytesDownloaded, result.Duration),
			ResumedDownload: false, // TODO: Implement resume functionality
		},
	}

	// Convert FileInfo to FileWitness
	for _, file := range result.Files {
		witness.Files = append(witness.Files, downloaders.FileWitness{
			Path:         file.Path,
			OriginalName: file.OriginalName,
			SourceURL:    file.SourceURL,
			ContentType:  file.ContentType,
			Size:         file.Size,
			Checksum:     file.Checksum,
			ChecksumType: file.ChecksumType,
			DownloadTime: file.DownloadTime,
		})
	}

	return witness
}
