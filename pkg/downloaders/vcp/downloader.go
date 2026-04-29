package vcp

import (
	"context"
	"fmt"
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

// VCPDownloader downloads datasets from the CZI Virtual Cell Platform.
type VCPDownloader struct {
	c       *client
	timeout time.Duration
	verbose bool
}

// Option configures a VCPDownloader.
type Option func(*VCPDownloader)

// WithTimeout sets the HTTP timeout.
func WithTimeout(t time.Duration) Option {
	return func(d *VCPDownloader) {
		d.timeout = t
		d.c = newClient(d.c.token, t)
	}
}

// WithVerbose enables verbose output.
func WithVerbose(v bool) Option { return func(d *VCPDownloader) { d.verbose = v } }

// WithToken sets the VCP authentication token (JWT).
// If empty, public endpoints are used (no authentication).
func WithToken(token string) Option {
	return func(d *VCPDownloader) { d.c.token = token }
}

// NewVCPDownloader creates a new VCPDownloader.
func NewVCPDownloader(opts ...Option) *VCPDownloader {
	d := &VCPDownloader{
		timeout: 60 * time.Second,
	}
	d.c = newClient("", d.timeout)
	for _, o := range opts {
		o(d)
	}
	return d
}

// GetSourceType returns the source identifier.
func (d *VCPDownloader) GetSourceType() string { return "vcp" }

// Validate checks that the id is a 24-char hex VCP dataset ID.
func (d *VCPDownloader) Validate(_ context.Context, id string) (*downloaders.ValidationResult, error) {
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
func (d *VCPDownloader) GetMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	rec, err := d.c.getDataset(ctx, id, false)
	if err != nil {
		return nil, fmt.Errorf("CZI metadata for %s: %w", id, err)
	}
	return recordToMetadata(id, rec), nil
}

// Download fetches dataset files from VCP/S3.
func (d *VCPDownloader) Download(ctx context.Context, req *downloaders.DownloadRequest) (*downloaders.DownloadResult, error) {
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

func (d *VCPDownloader) downloadFile(ctx context.Context, rawURL, targetPath string) (*downloaders.FileInfo, error) {
	// Pre-flight HEAD to surface auth errors before touching the cache.
	head, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	if hresp, herr := d.c.http.Do(head); herr == nil {
		_ = hresp.Body.Close()
		if hresp.StatusCode == http.StatusForbidden || hresp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("access denied for %s — set VCP_TOKEN for private datasets", filepath.Base(rawURL))
		}
	}

	result, err := common.Fetch(ctx, rawURL, targetPath, common.FetchOptions{Client: d.c.http})
	if err != nil {
		return nil, err
	}
	return &downloaders.FileInfo{
		Path:         targetPath,
		OriginalName: filepath.Base(targetPath),
		Size:         result.N,
		Checksum:     result.SHA256,
		ChecksumType: "sha256",
		SourceURL:    rawURL,
		DownloadTime: time.Now(),
		ContentType:  result.ContentType,
		CacheHit:     result.Hit,
	}, nil
}

// recordToMetadata converts a DatasetRecord to the common Metadata type.
func recordToMetadata(id string, rec *DatasetRecord) *downloaders.Metadata {
	m := &downloaders.Metadata{
		Source:    "vcp",
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
