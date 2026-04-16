package scperturb

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

// idPattern accepts AuthorYear or AuthorYear_anything.
var idPattern = regexp.MustCompile(`^[A-Z][a-zA-Z]+[0-9]{4}(_\S+)?$`)

// ScPerturbDownloader downloads datasets from the scPerturb collection.
type ScPerturbDownloader struct {
	client  *http.Client
	timeout time.Duration
	verbose bool
}

// Option configures a ScPerturbDownloader.
type Option func(*ScPerturbDownloader)

// WithTimeout sets the HTTP timeout.
func WithTimeout(t time.Duration) Option {
	return func(d *ScPerturbDownloader) {
		d.timeout = t
		d.client = newHTTPClient(t)
	}
}

// WithVerbose enables verbose output.
func WithVerbose(v bool) Option { return func(d *ScPerturbDownloader) { d.verbose = v } }

// NewScPerturbDownloader creates a new downloader.
func NewScPerturbDownloader(opts ...Option) *ScPerturbDownloader {
	d := &ScPerturbDownloader{timeout: 60 * time.Second}
	d.client = newHTTPClient(d.timeout)
	for _, o := range opts {
		o(d)
	}
	return d
}

// GetSourceType returns the source identifier.
func (d *ScPerturbDownloader) GetSourceType() string { return "scperturb" }

// Validate checks the ID format and resolves it against the catalog.
func (d *ScPerturbDownloader) Validate(ctx context.Context, id string) (*downloaders.ValidationResult, error) {
	result := &downloaders.ValidationResult{
		ID:         id,
		SourceType: d.GetSourceType(),
	}

	if !idPattern.MatchString(id) {
		result.Errors = []string{fmt.Sprintf(
			"invalid scPerturb ID %q: expected AuthorYear or AuthorYear_SubsetID (e.g. NormanWeissman2019)", id,
		)}
		return result, nil
	}

	catalog, err := loadCatalog(ctx, d.client)
	if err != nil {
		result.Errors = []string{fmt.Sprintf("could not load scPerturb catalog: %v", err)}
		return result, nil
	}

	matches := resolve(catalog, id)
	if len(matches) == 0 {
		result.Errors = []string{fmt.Sprintf("no scPerturb datasets found for %q", id)}
		return result, nil
	}

	result.Valid = true
	if len(matches) > 1 {
		result.Warnings = []string{fmt.Sprintf("matched %d datasets for publication %q", len(matches), id)}
	}
	return result, nil
}

// GetMetadata returns dataset information without downloading.
func (d *ScPerturbDownloader) GetMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	catalog, err := loadCatalog(ctx, d.client)
	if err != nil {
		return nil, err
	}

	matches := resolve(catalog, id)
	if len(matches) == 0 {
		return nil, fmt.Errorf("no scPerturb datasets found for %q", id)
	}

	first := matches[0]
	meta := &downloaders.Metadata{
		Source:    d.GetSourceType(),
		ID:        id,
		Title:     first.Title,
		FileCount: len(matches),
		Authors:   []string{first.FirstAuthor},
		Custom: map[string]any{
			"organism":    first.Organism,
			"modality":    first.Modality,
			"method":      first.Method,
			"perturbation": first.Perturbation,
			"zenodo_id":   first.ZenodoID,
			"datasets":    datasetsToNames(matches),
		},
	}
	if first.DOI != "" {
		meta.DOI = first.DOI
	}

	// Estimate total size from Zenodo metadata (we don't do a HEAD per file here).
	meta.TotalSize = int64(len(matches)) * 500 * 1024 * 1024 // rough 500 MB/file estimate

	return meta, nil
}

// Download fetches the h5ad files for the given ID.
func (d *ScPerturbDownloader) Download(ctx context.Context, req *downloaders.DownloadRequest) (*downloaders.DownloadResult, error) {
	start := time.Now()
	result := &downloaders.DownloadResult{Files: []downloaders.FileInfo{}}

	catalog, err := loadCatalog(ctx, d.client)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	matches := resolve(catalog, req.ID)
	if len(matches) == 0 {
		result.Errors = append(result.Errors, fmt.Sprintf("no datasets found for %q", req.ID))
		return result, nil
	}

	opts := req.Options

	// Dry-run: list files without downloading.
	if opts != nil && opts.DryRun {
		for _, ds := range matches {
			fname := ds.FullIndex + "." + ds.FileExt
			if downloaders.ShouldDownload(fname, -1, opts) {
				result.Files = append(result.Files, downloaders.FileInfo{
					OriginalName: fname,
					SourceURL:    ds.DownloadURL(),
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
	for _, ds := range matches {
		fname := ds.FullIndex + "." + ds.FileExt
		if !downloaders.ShouldDownload(fname, -1, opts) {
			continue
		}
		if opts != nil && opts.LimitFiles > 0 && downloaded >= opts.LimitFiles {
			break
		}

		targetPath := filepath.Join(req.OutputDir, fname)

		if opts != nil && opts.SkipExisting {
			if _, err := os.Stat(targetPath); err == nil {
				if d.verbose {
					fmt.Printf("⏭️  Skipping existing: %s\n", fname)
				}
				continue
			}
		}

		if d.verbose {
			fmt.Printf("⬇️  %s → %s\n", ds.FullIndex, ds.DownloadURL())
		}

		fi, err := d.downloadFile(ctx, ds.DownloadURL(), targetPath)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed %s: %v", fname, err))
			continue
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

func (d *ScPerturbDownloader) downloadFile(ctx context.Context, rawURL, targetPath string) (*downloaders.FileInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, http.NoBody)
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

	checksum, _ := common.CalculateFileChecksum(targetPath)
	return &downloaders.FileInfo{
		Path:         targetPath,
		OriginalName: filepath.Base(targetPath),
		Size:         written,
		Checksum:     checksum,
		ChecksumType: "sha256",
		SourceURL:    rawURL,
		DownloadTime: time.Now(),
		ContentType:  resp.Header.Get("Content-Type"),
	}, nil
}

func datasetsToNames(datasets []Dataset) []string {
	names := make([]string, len(datasets))
	for i, d := range datasets {
		names[i] = d.FullIndex
	}
	return names
}

// Search implements the Searcher interface.
func (d *ScPerturbDownloader) Search(ctx context.Context, query string, opts downloaders.SearchOptions) ([]downloaders.SearchResult, error) {
	catalog, err := loadCatalog(ctx, d.client)
	if err != nil {
		return nil, err
	}

	q := strings.ToLower(query)
	org := strings.ToLower(opts.Organism)
	assay := strings.ToLower(opts.EntryType) // re-used for method/modality filter

	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}

	// Deduplicate by publication index so we return one result per study.
	seen := make(map[string]bool)
	var results []downloaders.SearchResult

	for _, ds := range catalog {
		if len(results) >= limit {
			break
		}
		if seen[ds.PubIndex] {
			continue
		}

		// Text match across key fields.
		haystack := strings.ToLower(strings.Join([]string{
			ds.FullIndex, ds.PubIndex, ds.Title, ds.FirstAuthor,
			ds.Organism, ds.Method, ds.Modality, ds.Perturbation,
			ds.CellType, ds.Tissues, ds.Disease,
		}, " "))

		if q != "" && !strings.Contains(haystack, q) {
			continue
		}
		if org != "" && !strings.Contains(strings.ToLower(ds.Organism), org) {
			continue
		}
		if assay != "" && !strings.Contains(strings.ToLower(ds.Method+ds.Modality), assay) {
			continue
		}

		seen[ds.PubIndex] = true
		results = append(results, downloaders.SearchResult{
			Accession:   ds.PubIndex,
			Title:       ds.Title,
			Organism:    ds.Organism,
			EntryType:   ds.Method,
			DatasetType: ds.Modality,
			Date:        ds.Year,
		})
	}

	return results, nil
}
