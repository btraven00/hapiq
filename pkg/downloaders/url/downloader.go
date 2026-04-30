// Package url implements a hapiq downloader that fetches a single file
// directly from an arbitrary HTTP(S) URL. The "ID" is the URL itself.
// In manifests use the dedicated url: field:
//
//	- identifier: my-file
//	  url: https://example.com/file.csv
package url

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
)

// URLDownloader downloads a single file from a user-supplied HTTP(S) URL.
type URLDownloader struct {
	client  *http.Client
	verbose bool
}

// Option configures a URLDownloader.
type Option func(*URLDownloader)

// WithVerbose toggles progress logging to stderr.
func WithVerbose(v bool) Option {
	return func(d *URLDownloader) { d.verbose = v }
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(t time.Duration) Option {
	return func(d *URLDownloader) { d.client.Timeout = t }
}

// New creates a URLDownloader.
func New(opts ...Option) *URLDownloader {
	d := &URLDownloader{client: &http.Client{Timeout: 60 * time.Second}}
	for _, o := range opts {
		o(d)
	}
	return d
}

// GetSourceType returns the source identifier.
func (d *URLDownloader) GetSourceType() string { return "url" }

// Validate checks that id is a valid http or https URL.
func (d *URLDownloader) Validate(_ context.Context, id string) (*downloaders.ValidationResult, error) {
	result := &downloaders.ValidationResult{ID: id, SourceType: d.GetSourceType()}
	u, err := url.ParseRequestURI(id)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		result.Errors = []string{fmt.Sprintf("not a valid http/https URL: %q", id)}
		return result, nil
	}
	result.Valid = true
	return result, nil
}

// GetMetadata issues a HEAD request to retrieve size and content-type.
func (d *URLDownloader) GetMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	meta := &downloaders.Metadata{
		Source:    d.GetSourceType(),
		ID:        id,
		Title:     filenameFromURL(id),
		FileCount: 1,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, id, http.NoBody)
	if err != nil {
		return meta, nil
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return meta, nil
	}
	_ = resp.Body.Close()
	if resp.ContentLength > 0 {
		meta.TotalSize = resp.ContentLength
	}
	return meta, nil
}

// Download fetches the URL and writes the file into req.OutputDir.
func (d *URLDownloader) Download(ctx context.Context, req *downloaders.DownloadRequest) (*downloaders.DownloadResult, error) {
	start := time.Now()
	result := &downloaders.DownloadResult{Files: []downloaders.FileInfo{}}

	rawURL := req.ID

	// Resolve the filename once: prefer Content-Disposition from a HEAD request,
	// fall back to the URL path basename.
	filename := resolveFilename(ctx, rawURL, d.client)

	opts := req.Options
	if opts != nil && opts.DryRun {
		result.Files = append(result.Files, downloaders.FileInfo{
			OriginalName: filename,
			SourceURL:    rawURL,
		})
		result.Success = true
		return result, nil
	}

	if err := common.EnsureDirectory(req.OutputDir); err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	targetPath := filepath.Join(req.OutputDir, common.SanitizeFilename(filename))

	if opts != nil && opts.SkipExisting {
		if _, err := os.Stat(targetPath); err == nil {
			result.Success = true
			return result, nil
		}
	}

	if d.verbose {
		_, _ = fmt.Fprintf(os.Stderr, "⬇️  %s\n", rawURL)
	}

	fr, err := common.Fetch(ctx, rawURL, targetPath, common.FetchOptions{Client: d.client})
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	fi := downloaders.FileInfo{
		Path:         targetPath,
		OriginalName: filename,
		SourceURL:    rawURL,
		Size:         fr.N,
		Checksum:     fr.SHA256,
		ChecksumType: "sha256",
		CacheHit:     fr.Hit,
		DownloadTime: time.Now(),
		ContentType:  fr.ContentType,
	}

	result.Files = append(result.Files, fi)
	result.BytesDownloaded = fr.N
	result.BytesTotal = fr.N
	result.Duration = time.Since(start)
	result.Success = true

	if req.Metadata != nil {
		req.Metadata.TotalSize = fr.N
		req.Metadata.FileCount = 1

		witness := &downloaders.WitnessFile{
			HapiqVersion: "dev",
			DownloadTime: start,
			Source:       d.GetSourceType(),
			OriginalID:   req.ID,
			ResolvedURL:  rawURL,
			Metadata:     req.Metadata,
			Files:        []downloaders.FileWitness{downloaders.FileWitness(fi)},
			DownloadStats: &downloaders.DownloadStats{
				Duration:        result.Duration,
				BytesTotal:      fr.N,
				BytesDownloaded: fr.N,
				FilesTotal:      1,
				FilesDownloaded: 1,
				AverageSpeed:    downloaders.Speed(fr.N, result.Duration),
			},
		}
		if err := common.WriteWitnessFile(req.OutputDir, witness); err != nil {
			result.Warnings = append(result.Warnings, "witness file: "+err.Error())
		} else {
			result.WitnessFile = filepath.Join(req.OutputDir, "hapiq.json")
		}
	}

	return result, nil
}

// resolveFilename determines the output filename: Content-Disposition wins over
// the URL path basename.
func resolveFilename(ctx context.Context, rawURL string, client *http.Client) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, http.NoBody)
	if err == nil {
		if resp, err := client.Do(req); err == nil {
			_ = resp.Body.Close()
			if cd := resp.Header.Get("Content-Disposition"); cd != "" {
				if _, params, err := mime.ParseMediaType(cd); err == nil {
					if v := strings.TrimSpace(params["filename"]); v != "" {
						return v
					}
				}
			}
		}
	}
	return filenameFromURL(rawURL)
}

// filenameFromURL derives a filename from a URL's path component.
func filenameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "download"
	}
	base := filepath.Base(u.Path)
	if base == "" || base == "." || base == "/" {
		base = u.Host
	}
	if base == "" {
		base = "download"
	}
	return base
}
