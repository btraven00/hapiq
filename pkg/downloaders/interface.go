// Package downloaders provides a pluggable interface for downloading datasets
// from various scientific data repositories with comprehensive metadata tracking
// and provenance information.
package downloaders

import (
	"context"
	"time"
)

// Downloader defines the interface for downloading datasets from specific sources.
type Downloader interface {
	// Validate checks if the ID is valid for this source type
	Validate(ctx context.Context, id string) (*ValidationResult, error)

	// GetMetadata retrieves dataset information without downloading
	GetMetadata(ctx context.Context, id string) (*Metadata, error)

	// Download performs the actual download with progress tracking
	Download(ctx context.Context, req *DownloadRequest) (*DownloadResult, error)

	// GetSourceType returns the source type identifier (e.g., "geo", "figshare")
	GetSourceType() string
}

// DownloadRequest encapsulates all parameters for a download operation.
type DownloadRequest struct {
	Options   *DownloadOptions `json:"options,omitempty"`
	Metadata  *Metadata        `json:"metadata,omitempty"`
	ID        string           `json:"id"`
	OutputDir string           `json:"output_dir"`
}

// DownloadOptions provides configuration for download behavior.
type DownloadOptions struct {
	CustomFilters        map[string]string `json:"custom_filters,omitempty"`
	MaxConcurrent        int               `json:"max_concurrent"`
	IncludeRaw           bool              `json:"include_raw"`
	ExcludeSupplementary bool              `json:"exclude_supplementary"`
	Resume               bool              `json:"resume"`
	SkipExisting         bool              `json:"skip_existing"`
	NonInteractive       bool              `json:"non_interactive"`
}

// ValidationResult contains the outcome of ID validation.
type ValidationResult struct {
	ID         string   `json:"id"`
	SourceType string   `json:"source_type"`
	Errors     []string `json:"errors,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
	Valid      bool     `json:"valid"`
}

// Metadata contains comprehensive information about a dataset.
type Metadata struct {
	LastModified time.Time      `json:"last_modified"`
	Created      time.Time      `json:"created,omitempty"`
	Custom       map[string]any `json:"custom,omitempty"`
	DOI          string         `json:"doi,omitempty"`
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	Description  string         `json:"description"`
	Source       string         `json:"source"`
	Version      string         `json:"version,omitempty"`
	License      string         `json:"license,omitempty"`
	Authors      []string       `json:"authors,omitempty"`
	Tags         []string       `json:"tags,omitempty"`
	Keywords     []string       `json:"keywords,omitempty"`
	Collections  []Collection   `json:"collections,omitempty"`
	TotalSize    int64          `json:"total_size"`
	FileCount    int            `json:"file_count"`
}

// Collection represents a hierarchical dataset collection.
type Collection struct {
	Type          string   `json:"type"`
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Samples       []string `json:"samples,omitempty"`
	FileCount     int      `json:"file_count"`
	EstimatedSize int64    `json:"estimated_size"`
	UserConfirmed bool     `json:"user_confirmed"`
}

// DownloadResult contains the outcome and metadata of a download operation.
type DownloadResult struct {
	Metadata        *Metadata     `json:"metadata"`
	Checksum        string        `json:"checksum,omitempty"`
	ChecksumType    string        `json:"checksum_type,omitempty"`
	WitnessFile     string        `json:"witness_file"`
	Files           []FileInfo    `json:"files"`
	Collections     []Collection  `json:"collections,omitempty"`
	Errors          []string      `json:"errors,omitempty"`
	Warnings        []string      `json:"warnings,omitempty"`
	Duration        time.Duration `json:"duration"`
	BytesTotal      int64         `json:"bytes_total"`
	BytesDownloaded int64         `json:"bytes_downloaded"`
	Success         bool          `json:"success"`
}

// FileInfo contains metadata about an individual downloaded file.
type FileInfo struct {
	DownloadTime time.Time `json:"download_time"`
	Path         string    `json:"path"`
	OriginalName string    `json:"original_name"`
	Checksum     string    `json:"checksum,omitempty"`
	ChecksumType string    `json:"checksum_type,omitempty"`
	SourceURL    string    `json:"source_url"`
	ContentType  string    `json:"content_type,omitempty"`
	Size         int64     `json:"size"`
}

// WitnessFile represents the hapiq.json metadata file for provenance tracking.
type WitnessFile struct {
	DownloadTime  time.Time        `json:"download_time"`
	Metadata      *Metadata        `json:"metadata"`
	DownloadStats *DownloadStats   `json:"download_stats"`
	Verification  *Verification    `json:"verification,omitempty"`
	Options       *DownloadOptions `json:"options,omitempty"`
	HapiqVersion  string           `json:"hapiq_version"`
	Source        string           `json:"source"`
	OriginalID    string           `json:"original_id"`
	ResolvedURL   string           `json:"resolved_url,omitempty"`
	Files         []FileWitness    `json:"files"`
	Collections   []Collection     `json:"collections,omitempty"`
}

// FileWitness contains detailed provenance information for each file.
type FileWitness struct {
	DownloadTime time.Time `json:"download_time"`
	Path         string    `json:"path"`
	OriginalName string    `json:"original_name"`
	Checksum     string    `json:"checksum,omitempty"`
	ChecksumType string    `json:"checksum_type,omitempty"`
	SourceURL    string    `json:"source_url"`
	ContentType  string    `json:"content_type,omitempty"`
	Size         int64     `json:"size"`
}

// DownloadStats contains performance and operational statistics.
type DownloadStats struct {
	Duration        time.Duration `json:"duration"`
	BytesTotal      int64         `json:"bytes_total"`
	BytesDownloaded int64         `json:"bytes_downloaded"`
	FilesTotal      int           `json:"files_total"`
	FilesDownloaded int           `json:"files_downloaded"`
	FilesSkipped    int           `json:"files_skipped"`
	FilesFailed     int           `json:"files_failed"`
	AverageSpeed    float64       `json:"average_speed_bps"` // Bytes per second
	MaxConcurrent   int           `json:"max_concurrent"`
	ResumedDownload bool          `json:"resumed_download"`
}

// Verification contains integrity verification information.
type Verification struct {
	VerifyTime time.Time `json:"verify_time"`
	Method     string    `json:"method"`
	Expected   string    `json:"expected,omitempty"`
	Actual     string    `json:"actual,omitempty"`
	Errors     []string  `json:"errors,omitempty"`
	Verified   bool      `json:"verified"`
}

// DirectoryStatus represents the state of the target download directory.
type DirectoryStatus struct {
	TargetPath string   `json:"target_path"`
	Conflicts  []string `json:"conflicts,omitempty"`
	FreeSpace  int64    `json:"free_space,omitempty"`
	Exists     bool     `json:"exists"`
	HasWitness bool     `json:"has_witness"`
}

// Action represents user choices for conflict resolution.
type Action int

const (
	ActionProceed Action = iota
	ActionSkip
	ActionMerge
	ActionOverwrite
	ActionAbort
)

func (a Action) String() string {
	switch a {
	case ActionProceed:
		return "proceed"
	case ActionSkip:
		return "skip"
	case ActionMerge:
		return "merge"
	case ActionOverwrite:
		return "overwrite"
	case ActionAbort:
		return "abort"
	default:
		return "unknown"
	}
}

// ProgressCallback is called during download operations to report progress.
type ProgressCallback func(bytesDownloaded, bytesTotal int64, filename string)

// HierarchyTree represents the structure of a hierarchical dataset.
type HierarchyTree struct {
	Name     string           `json:"name"`
	Type     string           `json:"type"`
	Children []*HierarchyTree `json:"children,omitempty"`
	Size     int64            `json:"size,omitempty"`
}

// Error types for specific error conditions.
type DownloaderError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Source  string `json:"source,omitempty"`
	ID      string `json:"id,omitempty"`
}

func (e *DownloaderError) Error() string {
	if e.Source != "" && e.ID != "" {
		return e.Type + ": " + e.Message + " (source: " + e.Source + ", id: " + e.ID + ")"
	}

	return e.Type + ": " + e.Message
}

// Common error types.
var (
	ErrInvalidID         = &DownloaderError{Type: "invalid_id", Message: "invalid identifier format"}
	ErrNotFound          = &DownloaderError{Type: "not_found", Message: "dataset not found"}
	ErrAccessDenied      = &DownloaderError{Type: "access_denied", Message: "access denied"}
	ErrNetworkError      = &DownloaderError{Type: "network_error", Message: "network error"}
	ErrInsufficientSpace = &DownloaderError{Type: "insufficient_space", Message: "insufficient disk space"}
	ErrUnsupportedType   = &DownloaderError{Type: "unsupported_type", Message: "unsupported dataset type"}
)
