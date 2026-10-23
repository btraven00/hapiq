// Package tenx downloads 10x Genomics example datasets.
//
// The 10x datasets site (www.10xgenomics.com/datasets) is behind a bot
// checkpoint, so hapiq does not fetch it. Instead the ID is a dataset page's
// JSON saved from a browser (the _next/data/.../datasets/<slug>.json request).
// It lists every file with its URL, size and md5; the files themselves are on
// open hosts (cf.10xgenomics.com, s3-us-west-2.amazonaws.com/10x.files).
package tenx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/btraven00/hapiq/internal/version"
	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
	"github.com/btraven00/hapiq/pkg/manifest"
)

// TenxDownloader downloads the files listed in a saved 10x dataset page JSON.
type TenxDownloader struct {
	client  *http.Client
	timeout time.Duration
	verbose bool
}

// Option configures a TenxDownloader.
type Option func(*TenxDownloader)

// WithTimeout sets the HTTP stall limit.
func WithTimeout(t time.Duration) Option {
	return func(d *TenxDownloader) {
		d.timeout = t
		common.SetIdleTimeout(d.client, t)
	}
}

// WithVerbose toggles progress logging to stderr.
func WithVerbose(v bool) Option { return func(d *TenxDownloader) { d.verbose = v } }

// New creates a 10x downloader.
func New(opts ...Option) *TenxDownloader {
	d := &TenxDownloader{client: &http.Client{Timeout: 60 * time.Second}, timeout: 60 * time.Second}
	for _, o := range opts {
		o(d)
	}
	return d
}

// GetSourceType returns the source identifier.
func (d *TenxDownloader) GetSourceType() string { return "10x" }

// page is the subset of a saved dataset page JSON hapiq uses.
type page struct {
	PageProps struct {
		FilesetMap map[string]struct {
			Outputs   []tenxFile `json:"outputs"`
			Inputs    []tenxFile `json:"inputs"`
			Summaries []tenxFile `json:"summaries"`
		} `json:"filesetMap"`
		Dataset struct {
			PublishedAt        time.Time             `json:"publishedAt"`
			Title              string                `json:"title"`
			Slug               string                `json:"slug"`
			Pipeline           string                `json:"pipeline"`
			Body               string                `json:"body"`
			Software           struct{ Name string } `json:"software"`
			Species            []string              `json:"species"`
			Chemistries        []string              `json:"chemistries"`
			AnatomicalEntities []string              `json:"anatomicalEntities"`
			CellNucleiCount    int                   `json:"cellNucleiCount"`
		} `json:"dataset"`
	} `json:"pageProps"`
}

type tenxFile struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	MD5   string `json:"md5sum"`
	Bytes int64  `json:"bytes"`
	name  string // basename of URL, the local file name
}

// load parses a saved page JSON and returns it with its files, deduplicated
// by URL (summaries repeat outputs) and in a stable order.
func load(p string) (*page, []tenxFile, error) {
	data, err := os.ReadFile(filepath.Clean(p)) // #nosec G304 -- user-supplied page JSON
	if err != nil {
		return nil, nil, fmt.Errorf("read 10x page JSON: %w", err)
	}
	var pg page
	if err := json.Unmarshal(data, &pg); err != nil {
		return nil, nil, fmt.Errorf("parse 10x page JSON: %w", err)
	}
	if pg.PageProps.Dataset.Slug == "" {
		return nil, nil, fmt.Errorf("%s is not a 10x dataset page JSON (no pageProps.dataset.slug)", p)
	}

	groups := make([]string, 0, len(pg.PageProps.FilesetMap))
	for g := range pg.PageProps.FilesetMap {
		groups = append(groups, g)
	}
	sort.Strings(groups)

	seen := map[string]bool{}
	var files []tenxFile
	for _, g := range groups {
		fs := pg.PageProps.FilesetMap[g]
		for _, f := range append(append(fs.Outputs, fs.Inputs...), fs.Summaries...) {
			if seen[f.URL] {
				continue
			}
			seen[f.URL] = true
			u, err := url.Parse(f.URL)
			if err != nil || u.Scheme != "https" {
				return nil, nil, fmt.Errorf("file %q: not an https URL: %q", f.Title, f.URL)
			}
			f.name = path.Base(u.Path)
			if !filepath.IsLocal(f.name) {
				return nil, nil, fmt.Errorf("file %q: unsafe file name in %q", f.Title, f.URL)
			}
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("%s lists no files (pageProps.filesetMap is empty)", p)
	}
	return &pg, files, nil
}

// Validate checks that id is a readable dataset page JSON with files.
func (d *TenxDownloader) Validate(_ context.Context, id string) (*downloaders.ValidationResult, error) {
	result := &downloaders.ValidationResult{ID: id, SourceType: d.GetSourceType()}
	if _, _, err := load(id); err != nil {
		result.Errors = []string{err.Error() +
			" (save it from the dataset page in a browser: DevTools > Network > datasets/<slug>.json > Response)"}
		return result, nil
	}
	result.Valid = true
	return result, nil
}

// GetMetadata returns dataset metadata from the page JSON.
func (d *TenxDownloader) GetMetadata(_ context.Context, id string) (*downloaders.Metadata, error) {
	pg, files, err := load(id)
	if err != nil {
		return nil, err
	}
	ds := pg.PageProps.Dataset
	meta := &downloaders.Metadata{
		Source:      d.GetSourceType(),
		ID:          ds.Slug,
		Title:       ds.Title,
		Description: strings.TrimSpace(ds.Body),
		Created:     ds.PublishedAt,
		FileCount:   len(files),
		Custom: map[string]any{
			"page_url":    datasetURL(ds.Slug),
			"software":    strings.TrimSpace(ds.Software.Name + " " + ds.Pipeline),
			"species":     ds.Species,
			"chemistries": ds.Chemistries,
			"tissue":      ds.AnatomicalEntities,
			"cells":       ds.CellNucleiCount,
		},
	}
	for _, f := range files {
		meta.TotalSize += f.Bytes
	}
	return meta, nil
}

// Download fetches the (filtered) files into OutputDir and checks each
// against its md5 from the page JSON. A copy of the JSON is kept next to
// the data, and hapiq.json points at it, so `hapiq refetch` works.
func (d *TenxDownloader) Download(ctx context.Context, req *downloaders.DownloadRequest) (*downloaders.DownloadResult, error) {
	start := time.Now()
	result := &downloaders.DownloadResult{Files: []downloaders.FileInfo{}}

	pg, files, err := load(req.ID)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	slug := pg.PageProps.Dataset.Slug

	opts := req.Options
	var selected []tenxFile
	for _, f := range files {
		if opts != nil && opts.LimitFiles > 0 && len(selected) >= opts.LimitFiles {
			break
		}
		if downloaders.ShouldDownload(f.name, f.Bytes, opts) {
			selected = append(selected, f)
		}
	}

	if opts != nil && opts.DryRun {
		for _, f := range selected {
			result.Files = append(result.Files, downloaders.FileInfo{
				Path:           f.name,
				OriginalName:   f.name,
				Size:           f.Bytes,
				SourceURL:      f.URL,
				SourceChecksum: md5Spec(f),
			})
		}
		result.Success = true
		return result, nil
	}

	if err := common.EnsureDirectory(req.OutputDir); err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	// Keep the page JSON with the data: it is the only file list there is.
	pageCopy := filepath.Join(req.OutputDir, slug+".10x.json")
	if err := copyIfDifferent(req.ID, pageCopy); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("keep page JSON: %v", err))
		return result, nil
	}

	for _, f := range selected {
		targetPath := filepath.Join(req.OutputDir, f.name)

		if opts != nil && opts.SkipExisting {
			if _, err := os.Stat(targetPath); err == nil {
				if d.verbose {
					fmt.Fprintf(os.Stderr, "⏭️  Skipping existing: %s\n", f.name)
				}
				result.FilesSkipped++
				continue
			}
		}
		if d.verbose {
			fmt.Fprintf(os.Stderr, "⬇️  %s (%s)\n", f.name, common.FormatBytes(f.Bytes))
		}

		fi, err := d.downloadFile(ctx, f, targetPath)
		if err != nil {
			_ = os.Remove(targetPath)
			result.Errors = append(result.Errors, fmt.Sprintf("failed to download %s: %v", f.name, err))
			continue
		}
		result.Files = append(result.Files, *fi)
		result.BytesDownloaded += fi.Size
	}

	result.Duration = time.Since(start)
	result.BytesTotal = result.BytesDownloaded
	result.Success = len(result.Errors) == 0

	if req.Metadata != nil && len(result.Files) > 0 {
		abs, err := filepath.Abs(pageCopy)
		if err != nil {
			abs = pageCopy
		}
		witness := &downloaders.WitnessFile{
			HapiqVersion: version.String(),
			DownloadTime: start,
			Source:       d.GetSourceType(),
			// ponytail: an absolute path, so refetch finds the JSON from any
			// working directory; a moved folder needs `hapiq download 10x` again.
			OriginalID:  abs,
			ResolvedURL: datasetURL(slug),
			Metadata:    req.Metadata,
			Options:     req.Options,
			Files:       make([]downloaders.FileWitness, len(result.Files)),
			DownloadStats: &downloaders.DownloadStats{
				Duration:        result.Duration,
				BytesTotal:      result.BytesTotal,
				BytesDownloaded: result.BytesDownloaded,
				FilesTotal:      len(selected),
				FilesDownloaded: len(result.Files),
				FilesSkipped:    result.FilesSkipped,
				FilesFailed:     len(result.Errors),
				AverageSpeed:    downloaders.Speed(result.BytesDownloaded, result.Duration),
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

// downloadFile fetches f and checks its size and the md5 from the page JSON.
// The md5 matters: large files are multipart uploads whose ETag is no md5.
func (d *TenxDownloader) downloadFile(ctx context.Context, f tenxFile, targetPath string) (*downloaders.FileInfo, error) {
	res, err := common.Fetch(ctx, f.URL, targetPath, common.FetchOptions{Client: d.client})
	if err != nil {
		return nil, err
	}
	if f.Bytes > 0 && res.N != f.Bytes {
		return nil, fmt.Errorf("size %d, 10x lists %d", res.N, f.Bytes)
	}
	if spec := md5Spec(f); spec != "" {
		if err := manifest.VerifyFile(targetPath, spec); err != nil {
			return nil, err
		}
	}
	return &downloaders.FileInfo{
		Path:           f.name,
		OriginalName:   f.name,
		Size:           res.N,
		Checksum:       res.SHA256,
		ChecksumType:   "sha256",
		SourceChecksum: md5Spec(f),
		SourceURL:      f.URL,
		ContentType:    res.ContentType,
		DownloadTime:   time.Now(),
		CacheHit:       res.Hit,
	}, nil
}

func md5Spec(f tenxFile) string {
	if f.MD5 == "" {
		return ""
	}
	return "md5:" + f.MD5
}

func datasetURL(slug string) string { return "https://www.10xgenomics.com/datasets/" + slug }

// copyIfDifferent copies src to dst unless they are the same file (a refetch
// reads the copy it made last time).
func copyIfDifferent(src, dst string) error {
	if a, err := os.Stat(src); err == nil {
		if b, err := os.Stat(dst); err == nil && os.SameFile(a, b) {
			return nil
		}
	}
	data, err := os.ReadFile(filepath.Clean(src)) // #nosec G304 -- user-supplied page JSON
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}
