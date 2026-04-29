// Package experimenthub implements a hapiq downloader for Bioconductor's
// ExperimentHub. Resources are addressed by their EH<digits> identifier and
// fetched via https://experimenthub.bioconductor.org/fetch/<digits>.
//
// Resource metadata (title, description, species, rdatapath/dispatchclass)
// comes from the upstream metadata sqlite (experimenthub.sqlite3), which is
// cached locally for a week to avoid refetching ~MB of catalog on every run.
// The cache directory follows the same conventions as the hapiq blob cache —
// see config.go.
package experimenthub

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
)

var idPattern = regexp.MustCompile(`^EH(\d+)$`)

// ExperimentHubDownloader downloads ExperimentHub resources by EH id.
type ExperimentHubDownloader struct {
	client  *http.Client
	timeout time.Duration
	verbose bool
}

// Option configures an ExperimentHubDownloader.
type Option func(*ExperimentHubDownloader)

// WithTimeout sets the HTTP client timeout.
func WithTimeout(t time.Duration) Option {
	return func(d *ExperimentHubDownloader) {
		d.timeout = t
		d.client = newHTTPClient(t)
	}
}

// WithVerbose toggles progress logging to stderr.
func WithVerbose(v bool) Option {
	return func(d *ExperimentHubDownloader) { d.verbose = v }
}

// NewExperimentHubDownloader creates a downloader.
func NewExperimentHubDownloader(opts ...Option) *ExperimentHubDownloader {
	d := &ExperimentHubDownloader{timeout: 60 * time.Second}
	d.client = newHTTPClient(d.timeout)
	for _, o := range opts {
		o(d)
	}
	return d
}

// GetSourceType returns the source identifier.
func (d *ExperimentHubDownloader) GetSourceType() string { return "experimenthub" }

// Validate checks the EH<digits> format and that the resource is in the catalog.
func (d *ExperimentHubDownloader) Validate(ctx context.Context, id string) (*downloaders.ValidationResult, error) {
	result := &downloaders.ValidationResult{ID: id, SourceType: d.GetSourceType()}
	num, err := parseID(id)
	if err != nil {
		result.Errors = []string{err.Error()}
		return result, nil
	}

	dbPath, err := ensureMetadata(ctx, d.client, d.verbose)
	if err != nil {
		// Don't hard-fail validation on a metadata fetch error: the fetch URL
		// is constructable from the ID alone. Surface it as a warning.
		result.Warnings = []string{fmt.Sprintf("metadata catalog unavailable: %v", err)}
		result.Valid = true
		return result, nil
	}

	ri, err := lookupResource(dbPath, num)
	if err != nil {
		result.Warnings = []string{fmt.Sprintf("metadata lookup failed: %v", err)}
		result.Valid = true
		return result, nil
	}
	if ri == nil {
		result.Errors = []string{fmt.Sprintf("EH%d not found in ExperimentHub catalog", num)}
		return result, nil
	}
	result.Valid = true
	return result, nil
}

// GetMetadata returns catalog metadata for the resource, or a minimal record
// when the catalog is unreachable.
func (d *ExperimentHubDownloader) GetMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	num, err := parseID(id)
	if err != nil {
		return nil, err
	}

	meta := &downloaders.Metadata{
		Source: d.GetSourceType(),
		ID:     fmt.Sprintf("EH%d", num),
	}

	dbPath, err := ensureMetadata(ctx, d.client, d.verbose)
	if err != nil {
		return meta, nil
	}
	ri, err := lookupResource(dbPath, num)
	if err != nil || ri == nil {
		return meta, nil
	}

	meta.Title = ri.Title
	meta.Description = ri.Description
	meta.FileCount = 1
	if ri.Maintainer != "" {
		meta.Authors = []string{ri.Maintainer}
	}
	custom := map[string]any{}
	if ri.Species != "" {
		custom["species"] = ri.Species
	}
	if ri.DataProvider != "" {
		custom["data_provider"] = ri.DataProvider
	}
	if ri.RDataClass != "" {
		custom["rdata_class"] = ri.RDataClass
	}
	if ri.RDataPath != "" {
		custom["rdata_path"] = ri.RDataPath
	}
	if ri.DispatchClass != "" {
		custom["dispatch_class"] = ri.DispatchClass
	}
	if len(custom) > 0 {
		meta.Custom = custom
	}
	return meta, nil
}

// Download fetches the single resource file at /fetch/<id>.
func (d *ExperimentHubDownloader) Download(ctx context.Context, req *downloaders.DownloadRequest) (*downloaders.DownloadResult, error) {
	start := time.Now()
	result := &downloaders.DownloadResult{Files: []downloaders.FileInfo{}}

	num, err := parseID(req.ID)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	url := fetchURLPrefix + strconv.FormatInt(num, 10)

	// Best-effort metadata for naming + integrity.
	var ri *resourceInfo
	if dbPath, err := ensureMetadata(ctx, d.client, d.verbose); err == nil {
		ri, _ = lookupResource(dbPath, num)
	}
	filename := defaultFilename(num, ri)

	opts := req.Options
	if opts != nil && opts.DryRun {
		result.Files = append(result.Files, downloaders.FileInfo{
			OriginalName: filename,
			SourceURL:    url,
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
			if d.verbose {
				fmt.Fprintf(os.Stderr, "⏭️  Skipping existing: %s\n", filename)
			}
			result.Success = true
			return result, nil
		}
	}

	if d.verbose {
		fmt.Fprintf(os.Stderr, "⬇️  %s\n", url)
	}

	fi, gotName, err := d.downloadFile(ctx, url, targetPath, filename)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	fi.OriginalName = gotName
	_ = ri // catalog has no upstream md5/size to verify against

	result.Files = append(result.Files, *fi)
	result.BytesDownloaded += fi.Size
	result.BytesTotal = result.BytesDownloaded
	result.Duration = time.Since(start)
	result.Success = true

	// The catalog has no upstream size column, so back-fill totals from what
	// we just downloaded (single-file resource).
	if req.Metadata != nil {
		req.Metadata.TotalSize = result.BytesDownloaded
		req.Metadata.FileCount = 1
	}

	if req.Metadata != nil {
		witness := &downloaders.WitnessFile{
			HapiqVersion: "dev",
			DownloadTime: start,
			Source:       d.GetSourceType(),
			OriginalID:   req.ID,
			ResolvedURL:  url,
			Metadata:     req.Metadata,
			Files:        []downloaders.FileWitness{downloaders.FileWitness(*fi)},
			DownloadStats: &downloaders.DownloadStats{
				Duration:        result.Duration,
				BytesTotal:      result.BytesDownloaded,
				BytesDownloaded: result.BytesDownloaded,
				FilesTotal:      1,
				FilesDownloaded: 1,
				AverageSpeed:    averageSpeed(result.BytesDownloaded, result.Duration),
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

func (d *ExperimentHubDownloader) downloadFile(ctx context.Context, url, targetPath, fallbackName string) (*downloaders.FileInfo, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, "", err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	name := fallbackName
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if v, ok := params["filename"]; ok && v != "" {
				name = v
				targetPath = filepath.Join(filepath.Dir(targetPath), common.SanitizeFilename(name))
			}
		}
	}

	f, err := os.Create(filepath.Clean(targetPath)) // #nosec G304 -- caller-controlled target path
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	written, err := io.Copy(f, resp.Body)
	if err != nil {
		_ = os.Remove(targetPath)
		return nil, "", fmt.Errorf("write %s: %w", filepath.Base(targetPath), err)
	}

	return &downloaders.FileInfo{
		Path:         targetPath,
		Size:         written,
		SourceURL:    url,
		DownloadTime: time.Now(),
		ContentType:  resp.Header.Get("Content-Type"),
	}, name, nil
}

// Search implements downloaders.Searcher using the cached metadata sqlite.
// The query is matched (case-insensitive substring) against title, description,
// species, and dataprovider. opts.Organism narrows by species substring;
// opts.EntryType narrows by rdataclass (e.g. "SingleCellExperiment").
func (d *ExperimentHubDownloader) Search(ctx context.Context, query string, opts downloaders.SearchOptions) ([]downloaders.SearchResult, error) {
	dbPath, err := ensureMetadata(ctx, d.client, d.verbose)
	if err != nil {
		return nil, fmt.Errorf("metadata cache: %w", err)
	}
	return searchCatalog(dbPath, query, opts)
}

func averageSpeed(bytes int64, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(bytes) / d.Seconds()
}

func parseID(id string) (int64, error) {
	m := idPattern.FindStringSubmatch(strings.TrimSpace(id))
	if m == nil {
		return 0, fmt.Errorf("invalid ExperimentHub id %q: expected EH<digits>", id)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse EH id: %w", err)
	}
	return n, nil
}

func defaultFilename(num int64, ri *resourceInfo) string {
	if ri != nil && ri.RDataPath != "" {
		base := filepath.Base(ri.RDataPath)
		if base != "" && base != "." && base != "/" {
			return base
		}
	}
	return fmt.Sprintf("EH%d.rds", num)
}

// newHTTPClient builds a client tuned for ExperimentHub's redirect chain to
// third-party data providers (e.g. functionalgenomics.upf.edu), which can be
// slow to TLS-handshake. We extend handshake/dial budgets but leave the
// overall response read uncapped — large BAMs would otherwise trip a flat
// timeout. Callers should pass a context with their own deadline.
func newHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   60 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	}
	_ = timeout // honored by per-request context, not as a flat client cap
	return &http.Client{
		Transport: tr,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) > 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}
