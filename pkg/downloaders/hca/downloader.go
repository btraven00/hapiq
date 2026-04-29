package hca

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
)

// idPattern matches a 36-character HCA project UUID.
var idPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// HCADownloader fetches matrix and processed-output files from the
// Human Cell Atlas Data Portal (Azul service).
type HCADownloader struct {
	client  *http.Client
	timeout time.Duration
	verbose bool
}

// Option configures an HCADownloader.
type Option func(*HCADownloader)

// WithTimeout sets the HTTP timeout.
func WithTimeout(t time.Duration) Option {
	return func(d *HCADownloader) {
		d.timeout = t
		d.client = newHTTPClient(t)
	}
}

// WithVerbose toggles progress logging to stderr.
func WithVerbose(v bool) Option { return func(d *HCADownloader) { d.verbose = v } }

// NewHCADownloader creates a new downloader.
func NewHCADownloader(opts ...Option) *HCADownloader {
	d := &HCADownloader{timeout: 60 * time.Second}
	d.client = newHTTPClient(d.timeout)
	for _, o := range opts {
		o(d)
	}
	return d
}

// GetSourceType returns the source identifier.
func (d *HCADownloader) GetSourceType() string { return "hca" }

// Validate checks the UUID format and that the project resolves.
func (d *HCADownloader) Validate(ctx context.Context, id string) (*downloaders.ValidationResult, error) {
	result := &downloaders.ValidationResult{ID: id, SourceType: d.GetSourceType()}
	if !idPattern.MatchString(id) {
		result.Errors = []string{fmt.Sprintf("invalid HCA project UUID %q: expected 36-char UUID", id)}
		return result, nil
	}
	if _, err := fetchProject(ctx, d.client, id); err != nil {
		result.Errors = []string{err.Error()}
		return result, nil
	}
	result.Valid = true
	return result, nil
}

// GetMetadata returns project-level metadata without downloading files.
func (d *HCADownloader) GetMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	p, err := fetchProject(ctx, d.client, id)
	if err != nil {
		return nil, err
	}

	files := allFiles(p)
	var totalSize int64
	for _, f := range files {
		totalSize += f.Size
	}

	custom := map[string]any{
		"shortname": p.ProjectShortname,
	}
	if len(p.Accessions) > 0 {
		custom["accessions"] = p.Accessions
	}
	if p.EstimatedCells != nil {
		custom["estimated_cells"] = *p.EstimatedCells
	}

	return &downloaders.Metadata{
		Source:      d.GetSourceType(),
		ID:          p.ProjectID,
		Title:       p.ProjectTitle,
		Description: p.Description,
		FileCount:   len(files),
		TotalSize:   totalSize,
		Custom:      custom,
	}, nil
}

// Download fetches all (filtered) matrix files for the project.
func (d *HCADownloader) Download(ctx context.Context, req *downloaders.DownloadRequest) (*downloaders.DownloadResult, error) {
	start := time.Now()
	result := &downloaders.DownloadResult{Files: []downloaders.FileInfo{}}

	p, err := fetchProject(ctx, d.client, req.ID)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	files := allFiles(p)
	if len(files) == 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("HCA project %s has no matrix files", req.ID))
	}

	opts := req.Options

	if opts != nil && opts.DryRun {
		for _, f := range files {
			if !downloaders.ShouldDownload(f.Name, f.Size, opts) {
				continue
			}
			result.Files = append(result.Files, downloaders.FileInfo{
				OriginalName: f.Name,
				Size:         f.Size,
				SourceURL:    f.AzulURL,
				Checksum:     f.SHA256,
				ChecksumType: "sha256",
			})
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
		if !downloaders.ShouldDownload(f.Name, f.Size, opts) {
			continue
		}
		if opts != nil && opts.LimitFiles > 0 && downloaded >= opts.LimitFiles {
			break
		}
		if f.AzulURL == "" {
			result.Warnings = append(result.Warnings, fmt.Sprintf("file %q has no azul_url; skipping", f.Name))
			continue
		}

		targetPath := filepath.Join(req.OutputDir, common.SanitizeFilename(f.Name))

		if opts != nil && opts.SkipExisting {
			if _, err := os.Stat(targetPath); err == nil {
				if d.verbose {
					fmt.Fprintf(os.Stderr, "⏭️  Skipping existing: %s\n", f.Name)
				}
				continue
			}
		}

		if d.verbose {
			fmt.Fprintf(os.Stderr, "⬇️  %s (%s)\n", f.Name, common.FormatBytes(f.Size))
		}

		fi, err := d.downloadFile(ctx, f.AzulURL, targetPath)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed %s: %v", f.Name, err))
			continue
		}
		fi.OriginalName = f.Name
		// Prefer the server-declared sha256 over a local recompute when present.
		if f.SHA256 != "" {
			fi.Checksum = f.SHA256
			fi.ChecksumType = "sha256"
		}
		result.Files = append(result.Files, *fi)
		result.BytesDownloaded += fi.Size
		downloaded++
	}

	result.Duration = time.Since(start)
	result.BytesTotal = result.BytesDownloaded
	result.Success = len(result.Errors) == 0

	if req.Metadata != nil && len(result.Files) > 0 {
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

func (d *HCADownloader) downloadFile(ctx context.Context, rawURL, targetPath string) (*downloaders.FileInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}

	f, err := os.Create(filepath.Clean(targetPath)) // #nosec G304 -- caller-controlled target path
	if err != nil {
		return nil, err
	}
	defer f.Close()

	written, err := io.Copy(f, resp.Body)
	if err != nil {
		_ = os.Remove(targetPath)
		return nil, fmt.Errorf("write %s: %w", filepath.Base(targetPath), err)
	}

	return &downloaders.FileInfo{
		Path:         targetPath,
		OriginalName: filepath.Base(targetPath),
		Size:         written,
		SourceURL:    rawURL,
		DownloadTime: time.Now(),
		ContentType:  resp.Header.Get("Content-Type"),
	}, nil
}
