// Package ensembl provides a downloader implementation for Ensembl Genomes databases,
// supporting bacteria, fungi, metazoa, plants, and protists datasets.
package ensembl

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

// EnsemblDownloader implements the Downloader interface for Ensembl Genomes datasets.
type EnsemblDownloader struct {
	client      *http.Client
	protoClient ProtocolClient
	baseURL     string
	ftpBaseURL  string
	timeout     time.Duration
	verbose     bool
}

// NewEnsemblDownloader creates a new Ensembl downloader.
func NewEnsemblDownloader(options ...Option) *EnsemblDownloader {
	httpClient := &http.Client{Timeout: 30 * time.Second}

	d := &EnsemblDownloader{
		client:     httpClient,
		baseURL:    "https://www.ensembl.org",
		ftpBaseURL: "ftp://ftp.ensemblgenomes.org/pub",
		timeout:    30 * time.Second,
		verbose:    false,
	}

	for _, option := range options {
		option(d)
	}

	// Initialize protocol client after options are applied
	d.protoClient = NewMultiProtocolClient(d.client, d.timeout, d.verbose, 2)

	return d
}

// Option defines configuration options for EnsemblDownloader.
type Option func(*EnsemblDownloader)

// WithTimeout sets the HTTP timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(d *EnsemblDownloader) {
		d.timeout = timeout
		d.client.Timeout = timeout
	}
}

// WithVerbose enables verbose logging.
func WithVerbose(verbose bool) Option {
	return func(d *EnsemblDownloader) {
		d.verbose = verbose
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(d *EnsemblDownloader) {
		d.client = client
	}
}

// WithFTPConcurrency sets the maximum number of concurrent FTP connections.
func WithFTPConcurrency(maxConns int) Option {
	return func(d *EnsemblDownloader) {
		// This will be used when initializing the protocol client
		if maxConns < 1 {
			maxConns = 1
		}
		// Store in a custom field or recreate protocol client
		d.protoClient = NewMultiProtocolClient(d.client, d.timeout, d.verbose, maxConns)
	}
}

// WithRateLimit sets rate limiting for FTP downloads (requests per second).
func WithRateLimit(rps float64) Option {
	return func(d *EnsemblDownloader) {
		// This could be stored and used in download logic
		// For now, we'll keep the default 1 req/sec
	}
}

// GetSourceType returns the source type identifier.
func (d *EnsemblDownloader) GetSourceType() string {
	return "ensembl"
}

// GetProtocolClient returns the protocol client for direct access.
func (d *EnsemblDownloader) GetProtocolClient() ProtocolClient {
	return d.protoClient
}

// DatabaseType represents the supported Ensembl database types.
type DatabaseType string

const (
	DatabaseBacteria DatabaseType = "bacteria"
	DatabaseFungi    DatabaseType = "fungi"
	DatabaseMetazoa  DatabaseType = "metazoa"
	DatabasePlants   DatabaseType = "plants"
	DatabaseProtists DatabaseType = "protists"
)

// ContentType represents the supported content types.
type ContentType string

const (
	ContentPeptides ContentType = "pep"
	ContentCDS      ContentType = "cds"
	ContentGFF3     ContentType = "gff3"
	ContentDNA      ContentType = "dna"
)

// EnsemblRequest represents a parsed Ensembl download request.
type EnsemblRequest struct {
	Database DatabaseType
	Version  string
	Content  ContentType
	Species  string // Optional: specific species filter
}

// Validate checks if the ID is a valid Ensembl request format.
func (d *EnsemblDownloader) Validate(ctx context.Context, id string) (*downloaders.ValidationResult, error) {
	result := &downloaders.ValidationResult{
		ID:         id,
		SourceType: d.GetSourceType(),
		Valid:      false,
		Errors:     []string{},
		Warnings:   []string{},
	}

	// Clean the ID
	cleanID := d.cleanEnsemblID(id)
	if cleanID != id {
		result.Warnings = append(result.Warnings, fmt.Sprintf("normalized ID from '%s' to '%s'", id, cleanID))
		result.ID = cleanID
	}

	// Parse the request
	req, err := d.parseEnsemblID(cleanID)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	// Validate database type
	validDatabases := []DatabaseType{DatabaseBacteria, DatabaseFungi, DatabaseMetazoa, DatabasePlants, DatabaseProtists}
	validDB := false
	for _, db := range validDatabases {
		if req.Database == db {
			validDB = true
			break
		}
	}
	if !validDB {
		result.Errors = append(result.Errors, fmt.Sprintf("invalid database type: %s", req.Database))
		return result, nil
	}

	// Validate version format
	versionPattern := regexp.MustCompile(`^\d+$`)
	if !versionPattern.MatchString(req.Version) {
		result.Errors = append(result.Errors, "invalid version format (expected numeric version)")
		return result, nil
	}

	// Validate version range
	version, err := strconv.Atoi(req.Version)
	if err != nil {
		result.Errors = append(result.Errors, "invalid version number")
		return result, nil
	}

	if version < 1 || version > 100 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("version %d may not exist, typical range is 1-100", version))
	}

	// Validate content type
	validContent := []ContentType{ContentPeptides, ContentCDS, ContentGFF3, ContentDNA}
	validContentType := false
	for _, ct := range validContent {
		if req.Content == ct {
			validContentType = true
			break
		}
	}
	if !validContentType {
		result.Errors = append(result.Errors, fmt.Sprintf("invalid content type: %s", req.Content))
		return result, nil
	}

	// Warn about large downloads
	if req.Species == "" {
		result.Warnings = append(result.Warnings, "downloading all species - this will be a large dataset")
	}

	result.Valid = true
	return result, nil
}

// GetMetadata retrieves metadata for an Ensembl dataset.
func (d *EnsemblDownloader) GetMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	cleanID := d.cleanEnsemblID(id)

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

	req, err := d.parseEnsemblID(cleanID)
	if err != nil {
		return nil, err
	}

	return d.getEnsemblMetadata(ctx, req)
}

// Download performs the actual download operation.
func (d *EnsemblDownloader) Download(ctx context.Context, req *downloaders.DownloadRequest) (*downloaders.DownloadResult, error) {
	startTime := time.Now()

	result := &downloaders.DownloadResult{
		Success:  false,
		Files:    []downloaders.FileInfo{},
		Duration: 0,
		Errors:   []string{},
		Warnings: []string{},
	}

	// Clean and validate ID
	cleanID := d.cleanEnsemblID(req.ID)

	validation, err := d.Validate(ctx, cleanID)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	if !validation.Valid {
		result.Errors = append(result.Errors, strings.Join(validation.Errors, "; "))
		return result, nil
	}

	// Parse request
	ensemblReq, err := d.parseEnsemblID(cleanID)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
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

	// Perform the download
	downloadErr := d.downloadEnsemblData(ctx, ensemblReq, req.OutputDir, req.Options, result)
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

	// Determine success
	result.Success = len(result.Errors) == 0 && len(result.Files) > 0

	return result, nil
}

// cleanEnsemblID normalizes Ensembl identifiers.
func (d *EnsemblDownloader) cleanEnsemblID(id string) string {
	// Remove whitespace and convert to lowercase
	cleaned := strings.ToLower(strings.TrimSpace(id))

	// Remove common prefixes
	prefixes := []string{"ensembl:", "http://", "https://"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(cleaned, prefix) {
			cleaned = cleaned[len(prefix):]
		}
	}

	return cleaned
}

// parseEnsemblID parses an Ensembl ID into components.
// Expected format: database:version:content[:species]
// Examples: "bacteria:47:pep", "fungi:47:gff3", "plants:47:dna:triticum_turgidum"
func (d *EnsemblDownloader) parseEnsemblID(id string) (*EnsemblRequest, error) {
	parts := strings.Split(id, ":")
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid format, expected database:version:content[:species]")
	}

	req := &EnsemblRequest{
		Database: DatabaseType(parts[0]),
		Version:  parts[1],
		Content:  ContentType(parts[2]),
	}

	if len(parts) > 3 {
		req.Species = parts[3]
	}

	return req, nil
}

// getSpeciesCount retrieves the number of species from the species list.
func (d *EnsemblDownloader) getSpeciesCount(ctx context.Context, url string) (int, error) {
	resp, err := d.protoClient.Head(ctx, url)
	if err != nil {
		return 0, err
	}

	if resp.StatusCode != 200 {
		// If HEAD fails, try a quick GET to check existence
		resp, err = d.protoClient.Get(ctx, url)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return 0, fmt.Errorf("species list not found: Status %d", resp.StatusCode)
		}
	}

	// Estimate based on typical Ensembl database sizes
	// This is a rough estimate - actual implementation could parse the file

	// For now, return a conservative estimate
	// In a full implementation, we would parse the actual species list
	return 100, nil // Conservative default
}
