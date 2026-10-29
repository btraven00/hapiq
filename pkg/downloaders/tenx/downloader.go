// Package tenx downloads 10x Genomics example datasets.
//
// The 10x datasets site (www.10xgenomics.com/datasets) is behind a bot
// checkpoint, so hapiq does not fetch it. A dataset page embeds its file list
// (URL, size and md5 of every file) in __NEXT_DATA__, and the Internet
// Archive keeps copies of those pages, so for an ID like "10k-hgmm-5p-gemx"
// the list comes from the latest Wayback Machine snapshot. "<slug>@<timestamp>"
// pins a snapshot; a page saved from a browser (HTML or its JSON) works too.
// The files themselves are on open hosts (cf.10xgenomics.com,
// s3-us-west-2.amazonaws.com/10x.files) and are checked against the md5s.
package tenx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/btraven00/hapiq/internal/version"
	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
	"github.com/btraven00/hapiq/pkg/manifest"
)

// TenxDownloader downloads the files listed on a 10x dataset page.
type TenxDownloader struct {
	client      *http.Client
	pages       map[string]*loaded // by ID: Validate, GetMetadata and Download share one fetch
	archiveAPI  string
	waybackBase string
	timeout     time.Duration
	mu          sync.Mutex
	verbose     bool
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
	d := &TenxDownloader{
		client:      &http.Client{Timeout: 60 * time.Second},
		pages:       map[string]*loaded{},
		archiveAPI:  "https://archive.org/wayback/available",
		waybackBase: "https://web.archive.org/web/",
		timeout:     60 * time.Second,
	}
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

// loaded is a parsed dataset page and where it came from.
type loaded struct {
	pg       *page
	raw      []byte // the page data, kept next to the downloaded files
	snapshot string // Wayback snapshot URL, "" for a saved page
	pin      string // "<slug>@<timestamp>" for a snapshot, "" for a saved page
	files    []tenxFile
}

var slugPattern = regexp.MustCompile(`^([a-z0-9][a-z0-9-]*)(?:@(\d{14}))?$`)

func (d *TenxDownloader) load(ctx context.Context, id string) (*loaded, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if l, ok := d.pages[id]; ok {
		return l, nil
	}
	l, err := d.fetchPage(ctx, id)
	if err != nil {
		return nil, err
	}
	d.pages[id] = l
	return l, nil
}

// fetchPage reads a saved page if id is a file, else the archived page of
// the slug id (at its pinned timestamp, or the latest snapshot).
func (d *TenxDownloader) fetchPage(ctx context.Context, id string) (*loaded, error) {
	if _, err := os.Stat(id); err == nil {
		data, err := os.ReadFile(filepath.Clean(id)) // #nosec G304 -- user-supplied saved page
		if err != nil {
			return nil, err
		}
		l, err := parsePage(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		return l, nil
	}

	m := slugPattern.FindStringSubmatch(id)
	if m == nil {
		return nil, fmt.Errorf("%q is neither a dataset slug (e.g. 10k-hgmm-5p-gemx) nor a saved page file", id)
	}
	slug, ts := m[1], m[2]
	if ts == "" {
		var err error
		if ts, err = d.latestSnapshot(ctx, slug); err != nil {
			return nil, err
		}
	}
	data, err := d.get(ctx, d.waybackBase+ts+"id_/"+datasetURL(slug))
	if err != nil {
		return nil, fmt.Errorf("archived copy of %s: %w", datasetURL(slug), err)
	}
	l, err := parsePage(data)
	if err != nil {
		return nil, fmt.Errorf("archived copy of %s (%s): %w", datasetURL(slug), ts, err)
	}
	l.snapshot = d.waybackBase + ts + "/" + datasetURL(slug)
	l.pin = slug + "@" + ts
	return l, nil
}

// latestSnapshot asks the Wayback Machine for the newest copy of a dataset page.
func (d *TenxDownloader) latestSnapshot(ctx context.Context, slug string) (string, error) {
	data, err := d.get(ctx, d.archiveAPI+"?url="+url.QueryEscape(strings.TrimPrefix(datasetURL(slug), "https://")))
	if err != nil {
		return "", fmt.Errorf("Wayback Machine: %w", err)
	}
	var r struct {
		ArchivedSnapshots struct {
			Closest struct {
				Timestamp string `json:"timestamp"`
				Status    string `json:"status"`
				Available bool   `json:"available"`
			} `json:"closest"`
		} `json:"archived_snapshots"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return "", fmt.Errorf("Wayback Machine: %w", err)
	}
	c := r.ArchivedSnapshots.Closest
	if !c.Available || c.Status != "200" || c.Timestamp == "" {
		return "", fmt.Errorf("no archived copy of %s; open it in a browser, save the page (Ctrl+S) and pass the file instead", datasetURL(slug))
	}
	return c.Timestamp, nil
}

func (d *TenxDownloader) get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "hapiq/"+version.String()+" (+https://github.com/btraven00/hapiq)")
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

// parsePage reads a dataset page: its HTML (the __NEXT_DATA__ script), the
// __NEXT_DATA__ JSON, or the _next/data JSON. Files are deduplicated by URL
// (summaries repeat outputs) and kept in a stable order.
func parsePage(data []byte) (*loaded, error) {
	if t := bytes.TrimSpace(data); len(t) > 0 && t[0] == '<' {
		js, err := nextData(t)
		if err != nil {
			return nil, err
		}
		data = js
	}
	var doc struct {
		Props *page `json:"props"` // __NEXT_DATA__
		page        // _next/data JSON: pageProps at the top level
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse 10x page data: %w", err)
	}
	pg := &doc.page
	if doc.Props != nil {
		pg = doc.Props
	}
	if pg.PageProps.Dataset.Slug == "" {
		return nil, fmt.Errorf("not a 10x dataset page (no pageProps.dataset.slug)")
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
				return nil, fmt.Errorf("file %q: not an https URL: %q", f.Title, f.URL)
			}
			f.name = path.Base(u.Path)
			if !filepath.IsLocal(f.name) {
				return nil, fmt.Errorf("file %q: unsafe file name in %q", f.Title, f.URL)
			}
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("the page lists no files (pageProps.filesetMap is empty)")
	}
	return &loaded{pg: pg, raw: data, files: files}, nil
}

// nextData extracts the JSON of the __NEXT_DATA__ script from a page's HTML.
func nextData(html []byte) ([]byte, error) {
	i := bytes.Index(html, []byte(`id="__NEXT_DATA__"`))
	if i < 0 {
		return nil, fmt.Errorf("no __NEXT_DATA__ in the page HTML")
	}
	start := bytes.IndexByte(html[i:], '>')
	end := bytes.Index(html[i:], []byte("</script>"))
	if start < 0 || end < start {
		return nil, fmt.Errorf("malformed __NEXT_DATA__ script")
	}
	return html[i+start+1 : i+end], nil
}

// Validate checks that id resolves to a dataset page with files.
func (d *TenxDownloader) Validate(ctx context.Context, id string) (*downloaders.ValidationResult, error) {
	result := &downloaders.ValidationResult{ID: id, SourceType: d.GetSourceType()}
	if _, err := d.load(ctx, id); err != nil {
		result.Errors = []string{err.Error()}
		return result, nil
	}
	result.Valid = true
	return result, nil
}

// GetMetadata returns dataset metadata from the page.
func (d *TenxDownloader) GetMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	l, err := d.load(ctx, id)
	if err != nil {
		return nil, err
	}
	ds, files := l.pg.PageProps.Dataset, l.files
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
	if l.snapshot != "" {
		meta.Custom["file_list_from"] = l.snapshot
	}
	for _, f := range files {
		meta.TotalSize += f.Bytes
	}
	return meta, nil
}

// Download fetches the (filtered) files into OutputDir and checks each
// against its md5 from the page. The page data is kept next to the files.
func (d *TenxDownloader) Download(ctx context.Context, req *downloaders.DownloadRequest) (*downloaders.DownloadResult, error) {
	start := time.Now()
	result := &downloaders.DownloadResult{Files: []downloaders.FileInfo{}}

	l, err := d.load(ctx, req.ID)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	slug, files := l.pg.PageProps.Dataset.Slug, l.files

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

	// Keep the page data with the files: it is their only list and checksums.
	pageCopy := filepath.Join(req.OutputDir, slug+".10x.json")
	if err := os.WriteFile(pageCopy, l.raw, 0o600); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("keep page JSON: %v", err))
		return result, nil
	}

	for _, f := range selected {
		targetPath := filepath.Join(req.OutputDir, f.name)

		if opts != nil && opts.SkipExisting {
			if fi, ok := common.SkippedFile(targetPath, f.name, f.URL); ok {
				if d.verbose {
					fmt.Fprintf(os.Stderr, "⏭️  Skipping existing: %s\n", f.name)
				}
				fi.Path = f.name
				result.Files = append(result.Files, fi)
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
		// The ID that reproduces this file list: the pinned snapshot, or for a
		// saved page the kept copy (absolute, so refetch works from any cwd).
		originalID, resolved := l.pin, l.snapshot
		if originalID == "" {
			if originalID, err = filepath.Abs(pageCopy); err != nil {
				originalID = pageCopy
			}
			resolved = datasetURL(slug)
		}
		witness := &downloaders.WitnessFile{
			HapiqVersion: version.String(),
			DownloadTime: start,
			Source:       d.GetSourceType(),
			OriginalID:   originalID,
			ResolvedURL:  resolved,
			Metadata:     req.Metadata,
			Options:      req.Options,
			Files:        make([]downloaders.FileWitness, len(result.Files)),
			DownloadStats: &downloaders.DownloadStats{
				Duration:        result.Duration,
				BytesTotal:      result.BytesTotal,
				BytesDownloaded: result.BytesDownloaded,
				FilesTotal:      len(selected),
				FilesDownloaded: len(result.Files) - result.FilesSkipped,
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
