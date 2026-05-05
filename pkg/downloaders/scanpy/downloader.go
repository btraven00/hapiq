// Package scanpy implements a hapiq downloader for the curated datasets
// shipped with scanpy.datasets (the network-fetched ones; package-bundled
// datasets are out of scope).
//
// IDs are dataset names from the scanpy catalog. Parametrized datasets take
// a sub-id after a slash, e.g. `scanpy:visium_sge/V1_Human_Heart` or
// `scanpy:ebi_expression_atlas/E-GEOD-98816`.
package scanpy

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/btraven00/hapiq/internal/version"
	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
)

// Downloader fetches scanpy.datasets entries by name.
type Downloader struct {
	client  *http.Client
	verbose bool
}

// Option configures a Downloader.
type Option func(*Downloader)

// WithVerbose toggles progress logging.
func WithVerbose(v bool) Option { return func(d *Downloader) { d.verbose = v } }

// WithTimeout sets the HTTP client timeout.
func WithTimeout(t time.Duration) Option {
	return func(d *Downloader) { d.client.Timeout = t }
}

// New creates a Downloader.
func New(opts ...Option) *Downloader {
	d := &Downloader{client: &http.Client{Timeout: 60 * time.Second}}
	for _, o := range opts {
		o(d)
	}
	return d
}

// GetSourceType returns the source identifier.
func (d *Downloader) GetSourceType() string { return "scanpy" }

// Validate checks that the ID names a known dataset and resolves any parameter.
func (d *Downloader) Validate(_ context.Context, id string) (*downloaders.ValidationResult, error) {
	res := &downloaders.ValidationResult{ID: id, SourceType: d.GetSourceType()}
	e, files, err := resolveID(id)
	if err != nil {
		res.Errors = []string{err.Error()}
		return res, nil
	}
	res.Valid = true
	if len(e.Examples) > 0 && len(files) == 0 {
		res.Warnings = []string{fmt.Sprintf("dataset %q is parametrized; example sub-ids: %v", e.Name, e.Examples)}
	}
	return res, nil
}

// GetMetadata returns lightweight catalog metadata; sizes are not probed.
func (d *Downloader) GetMetadata(_ context.Context, id string) (*downloaders.Metadata, error) {
	e, files, err := resolveID(id)
	if err != nil {
		return nil, err
	}
	return &downloaders.Metadata{
		Source:      d.GetSourceType(),
		ID:          id,
		Title:       e.Label,
		Description: fmt.Sprintf("scanpy.datasets.%s", e.Name),
		FileCount:   len(files),
		Custom: map[string]any{
			"format": e.Format,
		},
	}, nil
}

// Download fetches all files for the resolved dataset into req.OutputDir.
func (d *Downloader) Download(ctx context.Context, req *downloaders.DownloadRequest) (*downloaders.DownloadResult, error) {
	start := time.Now()
	result := &downloaders.DownloadResult{Files: []downloaders.FileInfo{}}

	e, files, err := resolveID(req.ID)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	opts := req.Options

	if opts != nil && opts.DryRun {
		for _, f := range files {
			result.Files = append(result.Files, downloaders.FileInfo{
				OriginalName: filenameFor(f),
				SourceURL:    f.URL,
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
		if opts != nil && opts.LimitFiles > 0 && downloaded >= opts.LimitFiles {
			break
		}
		fname := filenameFor(f)
		if !downloaders.ShouldDownload(fname, -1, opts) {
			continue
		}

		targetPath := filepath.Join(req.OutputDir, common.SanitizeFilename(fname))

		if _, statErr := os.Stat(targetPath); statErr == nil {
			switch {
			case opts != nil && opts.SkipExisting:
				if d.verbose {
					_, _ = fmt.Fprintf(os.Stderr, "⏭️  Skipping existing: %s\n", fname)
				}
				continue
			case opts != nil && (opts.Force || opts.NonInteractive):
				// overwrite silently
			default:
				confirmed, cerr := common.AskUserConfirmation(
					fmt.Sprintf("file %q already exists. Overwrite?", fname),
				)
				if cerr != nil || !confirmed {
					continue
				}
			}
		}

		if d.verbose {
			_, _ = fmt.Fprintf(os.Stderr, "⬇️  %s\n", f.URL)
		}

		fr, err := common.Fetch(ctx, f.URL, targetPath, common.FetchOptions{Client: d.client})
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed %s: %v", fname, err))
			continue
		}

		fi := downloaders.FileInfo{
			Path:         targetPath,
			OriginalName: fname,
			SourceURL:    f.URL,
			Size:         fr.N,
			Checksum:     fr.SHA256,
			ChecksumType: "sha256",
			CacheHit:     fr.Hit,
			DownloadTime: time.Now(),
			ContentType:  fr.ContentType,
		}
		result.Files = append(result.Files, fi)
		result.BytesDownloaded += fr.N
		downloaded++
	}

	result.BytesTotal = result.BytesDownloaded
	result.Duration = time.Since(start)
	result.Success = len(result.Errors) == 0 && (len(result.Files) > 0 || (opts != nil && opts.SkipExisting))

	if req.Metadata != nil && len(result.Files) > 0 {
		req.Metadata.FileCount = len(result.Files)
		req.Metadata.TotalSize = result.BytesDownloaded
		witness := &downloaders.WitnessFile{
			HapiqVersion: version.String(),
			DownloadTime: start,
			Source:       d.GetSourceType(),
			OriginalID:   req.ID,
			Metadata:     req.Metadata,
			Files:        make([]downloaders.FileWitness, len(result.Files)),
			DownloadStats: &downloaders.DownloadStats{
				Duration:        result.Duration,
				BytesTotal:      result.BytesDownloaded,
				BytesDownloaded: result.BytesDownloaded,
				FilesTotal:      len(result.Files),
				FilesDownloaded: len(result.Files),
				AverageSpeed:    downloaders.Speed(result.BytesDownloaded, result.Duration),
			},
			Options: opts,
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

	_ = e
	return result, nil
}

// filenameFor returns the on-disk name for a fileSpec. Falls back to the URL
// path basename when no explicit Filename is set.
func filenameFor(f fileSpec) string {
	if f.Filename != "" {
		return f.Filename
	}
	if u, err := url.Parse(f.URL); err == nil {
		base := filepath.Base(u.Path)
		if base != "" && base != "." && base != "/" {
			return base
		}
	}
	return "download"
}
