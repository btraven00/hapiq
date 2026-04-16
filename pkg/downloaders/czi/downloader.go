package czi

import (
	"context"
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

var idPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)

// CZIDownloader downloads datasets from the CZI Virtual Cell Platform.
type CZIDownloader struct {
	c       *client
	timeout time.Duration
	verbose bool
}

// Option configures a CZIDownloader.
type Option func(*CZIDownloader)

// WithTimeout sets the HTTP timeout.
func WithTimeout(t time.Duration) Option {
	return func(d *CZIDownloader) {
		d.timeout = t
		d.c = newClient(d.c.token, t)
	}
}

// WithVerbose enables verbose output.
func WithVerbose(v bool) Option { return func(d *CZIDownloader) { d.verbose = v } }

// WithToken sets the VCP authentication token (JWT).
// If empty, public endpoints are used (no authentication).
func WithToken(token string) Option {
	return func(d *CZIDownloader) { d.c.token = token }
}

// NewCZIDownloader creates a new CZIDownloader.
func NewCZIDownloader(opts ...Option) *CZIDownloader {
	d := &CZIDownloader{
		timeout: 60 * time.Second,
	}
	d.c = newClient("", d.timeout)
	for _, o := range opts {
		o(d)
	}
	return d
}

// GetSourceType returns the source identifier.
func (d *CZIDownloader) GetSourceType() string { return "czi" }

// Validate checks that the id is a 24-char hex VCP dataset ID.
func (d *CZIDownloader) Validate(_ context.Context, id string) (*downloaders.ValidationResult, error) {
	clean := strings.ToLower(strings.TrimSpace(id))
	result := &downloaders.ValidationResult{
		ID:         clean,
		SourceType: d.GetSourceType(),
		Valid:       idPattern.MatchString(clean),
	}
	if !result.Valid {
		result.Errors = []string{
			fmt.Sprintf("invalid VCP dataset ID %q: expected 24 hex characters", id),
		}
	}
	return result, nil
}

// GetMetadata retrieves dataset metadata from the VCP API.
func (d *CZIDownloader) GetMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	rec, err := d.c.getDataset(ctx, id, false)
	if err != nil {
		return nil, fmt.Errorf("CZI metadata for %s: %w", id, err)
	}
	return recordToMetadata(id, rec), nil
}

// Download fetches dataset files from VCP/S3.
func (d *CZIDownloader) Download(ctx context.Context, req *downloaders.DownloadRequest) (*downloaders.DownloadResult, error) {
	start := time.Now()
	result := &downloaders.DownloadResult{Files: []downloaders.FileInfo{}}

	rec, err := d.c.getDataset(ctx, req.ID, true)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	files := collectFiles(rec)
	if len(files) == 0 {
		result.Warnings = append(result.Warnings, "no downloadable files found for "+req.ID)
		result.Success = true
		return result, nil
	}

	opts := req.Options

	if opts != nil && opts.DryRun {
		for _, f := range files {
			if downloaders.ShouldDownload(f.name, f.size, opts) {
				result.Files = append(result.Files, downloaders.FileInfo{
					OriginalName: f.name,
					SourceURL:    f.httpsURL,
					Size:         f.size,
				})
			}
		}
		result.Success = true
		return result, nil
	}

	if err := common.EnsureDirectory(req.OutputDir); err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	downloaded := 0
	for _, f := range files {
		if opts != nil && opts.LimitFiles > 0 && downloaded >= opts.LimitFiles {
			break
		}
		if !downloaders.ShouldDownload(f.name, f.size, opts) {
			continue
		}
		if opts != nil && opts.SkipExisting {
			if _, err := os.Stat(filepath.Join(req.OutputDir, f.name)); err == nil {
				if d.verbose {
					fmt.Printf("⏭️  Skipping existing: %s\n", f.name)
				}
				continue
			}
		}
		if d.verbose {
			fmt.Printf("⬇️  %s\n", f.name)
		}
		fi, err := d.downloadFile(ctx, f.httpsURL, filepath.Join(req.OutputDir, f.name))
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed %s: %v", f.name, err))
			continue
		}
		result.Files = append(result.Files, *fi)
		result.BytesDownloaded += fi.Size
		downloaded++
	}

	result.Duration = time.Since(start)
	result.BytesTotal = result.BytesDownloaded
	result.Success = len(result.Errors) == 0

	if req.Metadata != nil {
		witness := &downloaders.WitnessFile{
			HapiqVersion: "dev",
			DownloadTime: start,
			Source:       d.GetSourceType(),
			OriginalID:   req.ID,
			Metadata:     req.Metadata,
			Files:        make([]downloaders.FileWitness, len(result.Files)),
			DownloadStats: &downloaders.DownloadStats{
				Duration:        result.Duration,
				BytesDownloaded: result.BytesDownloaded,
				FilesDownloaded: len(result.Files),
			},
		}
		for i, f := range result.Files {
			witness.Files[i] = downloaders.FileWitness(f)
		}
		if err := common.WriteWitnessFile(req.OutputDir, witness); err != nil {
			result.Warnings = append(result.Warnings, "witness file: "+err.Error())
		} else {
			result.WitnessFile = filepath.Join(req.OutputDir, "hapiq.json")
		}
	}

	return result, nil
}

// vcpFile is an internal representation of a downloadable file.
type vcpFile struct {
	name     string
	httpsURL string
	size     int64
}

// collectFiles extracts all downloadable files from a DatasetRecord.
// S3 URIs are converted to HTTPS. Files with unresolvable URIs are skipped.
func collectFiles(rec *DatasetRecord) []vcpFile {
	var files []vcpFile

	// Prefer the Croissant distribution list (richer metadata).
	if rec.MD != nil && len(rec.MD.Distribution) > 0 {
		for _, cf := range rec.MD.Distribution {
			u := cf.ContentURL
			if strings.HasPrefix(u, "s3://") {
				u = s3ToHTTPS(u)
			}
			if u == "" {
				continue
			}
			name := filepath.Base(cf.ContentURL) // preserve original filename
			if name == "" || name == "." {
				name = cf.Name + ".bin"
			}
			files = append(files, vcpFile{name: name, httpsURL: u})
		}
		return files
	}

	// Fall back to the top-level locations array.
	for _, loc := range rec.Locations {
		u := loc.URL
		if strings.HasPrefix(u, "s3://") {
			u = s3ToHTTPS(u)
		}
		if u == "" {
			continue
		}
		name := filepath.Base(loc.URL)
		if name == "" || name == "." {
			name = rec.InternalID + ".bin"
		}
		files = append(files, vcpFile{name: name, httpsURL: u, size: loc.ContentSize})
	}
	return files
}

func (d *CZIDownloader) downloadFile(ctx context.Context, rawURL, targetPath string) (*downloaders.FileInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := d.c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("access denied for %s — set VCP_TOKEN for private datasets", filepath.Base(rawURL))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}

	// Capture the S3 ETag for integrity tracking. For single-part uploads it
	// is the MD5 hex digest; for multi-part uploads it is "<md5>-<parts>" and
	// is still useful as a fingerprint even though it's not a plain MD5.
	etag := strings.Trim(resp.Header.Get("ETag"), `"`)

	f, err := os.Create(targetPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	written, err := io.Copy(f, resp.Body)
	if err != nil {
		_ = os.Remove(targetPath)
		return nil, fmt.Errorf("write %s: %w", filepath.Base(targetPath), err)
	}

	// Compute SHA-256 locally for the witness file.
	sha256sum, _ := common.CalculateFileChecksum(targetPath)

	checksumType := "sha256"
	checksum := sha256sum
	// If we also have a clean single-part ETag (no dashes = no multi-part),
	// record the S3 ETag as the primary checksum so users can cross-check.
	if etag != "" && !strings.Contains(etag, "-") {
		checksumType = "md5(s3-etag)"
		checksum = etag
	}

	return &downloaders.FileInfo{
		Path:         targetPath,
		OriginalName: filepath.Base(targetPath),
		Size:         written,
		Checksum:     checksum,
		ChecksumType: checksumType,
		SourceURL:    rawURL,
		DownloadTime: time.Now(),
		ContentType:  resp.Header.Get("Content-Type"),
	}, nil
}

// recordToMetadata converts a DatasetRecord to the common Metadata type.
func recordToMetadata(id string, rec *DatasetRecord) *downloaders.Metadata {
	m := &downloaders.Metadata{
		Source:    "czi",
		ID:        id,
		FileCount: len(rec.Locations),
		Custom:    map[string]any{"domain": rec.Domain},
	}

	if rec.MD != nil {
		m.Title = rec.MD.Name
		m.Description = rec.MD.Description
		m.License = rec.MD.License
		m.Version = rec.MD.Version
		if len(rec.MD.Distribution) > 0 {
			m.FileCount = len(rec.MD.Distribution)
		}
	} else {
		m.Title = rec.Label
	}

	return m
}
