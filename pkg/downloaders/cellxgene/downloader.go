// Package cellxgene downloads datasets from CZ CELLxGENE Discover via its
// public curation API (https://api.cellxgene.cziscience.com/curation/ui/).
//
// An ID is a collection, dataset, collection-version or dataset-version UUID,
// or any Discover URL containing one (…/collections/<uuid>, …/e/<uuid>.cxg).
// A collection downloads every dataset's assets (H5AD, ATAC fragments); a
// dataset downloads its latest version. The witness pins the version ID, so
// refetch gets the same bytes.
package cellxgene

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/btraven00/hapiq/internal/version"
	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
)

const defaultBaseURL = "https://api.cellxgene.cziscience.com/curation/v1"

var uuidRe = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

type label struct {
	Label string `json:"label"`
}

type asset struct {
	Filetype string `json:"filetype"`
	URL      string `json:"url"`
	Filesize int64  `json:"filesize"`
}

type dataset struct {
	DatasetID        string  `json:"dataset_id"`
	DatasetVersionID string  `json:"dataset_version_id"`
	CollectionID     string  `json:"collection_id"`
	Title            string  `json:"title"`
	Organism         []label `json:"organism"`
	Assay            []label `json:"assay"`
	Tissue           []label `json:"tissue"`
	Disease          []label `json:"disease"`
	CellType         []label `json:"cell_type"`
	CollectionName   string  `json:"collection_name"`
	PublishedAt      string  `json:"published_at"`
	Assets           []asset `json:"assets"`
	CellCount        int64   `json:"cell_count"`
}

type collection struct {
	CollectionID        string    `json:"collection_id"`
	CollectionVersionID string    `json:"collection_version_id"`
	Name                string    `json:"name"`
	Description         string    `json:"description"`
	DOI                 *string   `json:"doi"`
	PublishedAt         string    `json:"published_at"`
	Datasets            []dataset `json:"datasets"`
}

// resolved is what an ID points at: a collection or a single dataset.
type resolved struct {
	Title, Description, DOI, Kind string
	PinnedID, URL                 string
	Datasets                      []dataset
}

// Downloader downloads datasets from CELLxGENE Discover.
type Downloader struct {
	client  *http.Client
	baseURL string
	verbose bool
}

// Option configures a Downloader.
type Option func(*Downloader)

// WithTimeout sets the HTTP idle timeout.
func WithTimeout(t time.Duration) Option {
	return func(d *Downloader) { common.SetIdleTimeout(d.client, t) }
}

// WithVerbose enables verbose output.
func WithVerbose(v bool) Option { return func(d *Downloader) { d.verbose = v } }

// WithBaseURL overrides the curation API base URL. Intended for tests.
func WithBaseURL(u string) Option { return func(d *Downloader) { d.baseURL = u } }

// New creates a CELLxGENE downloader.
func New(opts ...Option) *Downloader {
	d := &Downloader{client: &http.Client{Timeout: 60 * time.Second}, baseURL: defaultBaseURL}
	for _, o := range opts {
		o(d)
	}
	return d
}

// GetSourceType returns the source identifier.
func (d *Downloader) GetSourceType() string { return "cellxgene" }

// Validate checks that the ID holds a UUID that resolves on Discover.
func (d *Downloader) Validate(ctx context.Context, id string) (*downloaders.ValidationResult, error) {
	res := &downloaders.ValidationResult{ID: id, SourceType: d.GetSourceType()}
	if _, err := d.resolve(ctx, id); err != nil {
		res.Errors = []string{err.Error()}
		return res, nil
	}
	res.Valid = true
	return res, nil
}

// GetMetadata returns dataset information without downloading.
func (d *Downloader) GetMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	r, err := d.resolve(ctx, id)
	if err != nil {
		return nil, err
	}
	meta := &downloaders.Metadata{
		Source:      d.GetSourceType(),
		ID:          r.PinnedID,
		Title:       r.Title,
		Description: r.Description,
		DOI:         r.DOI,
		Custom:      map[string]any{"kind": r.Kind, "url": r.URL},
	}
	var titles []string
	for _, ds := range r.Datasets {
		titles = append(titles, ds.DatasetVersionID+": "+ds.Title)
		for _, a := range ds.Assets {
			meta.TotalSize += a.Filesize
			meta.FileCount++
		}
	}
	meta.Custom["datasets"] = titles
	return meta, nil
}

// Download fetches the (filtered) assets of every dataset the ID resolves to.
func (d *Downloader) Download(ctx context.Context, req *downloaders.DownloadRequest) (*downloaders.DownloadResult, error) {
	start := time.Now()
	result := &downloaders.DownloadResult{Files: []downloaders.FileInfo{}}
	opts := req.Options
	if opts == nil {
		opts = &downloaders.DownloadOptions{}
	}

	r, err := d.resolve(ctx, req.ID)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	type file struct {
		ds   dataset
		a    asset
		name string
	}
	var selected []file
	for _, ds := range r.Datasets {
		if len(opts.Subset) > 0 && !slices.Contains(opts.Subset, ds.DatasetID) && !slices.Contains(opts.Subset, ds.DatasetVersionID) {
			continue
		}
		if opts.Organism != "" && !hasOrganism(ds, opts.Organism) {
			continue
		}
		for _, a := range ds.Assets {
			name := common.SanitizeFilename(path.Base(a.URL))
			if downloaders.ShouldDownload(name, a.Filesize, opts) {
				selected = append(selected, file{ds, a, name})
			}
		}
	}
	if opts.LimitFiles > 0 && len(selected) > opts.LimitFiles {
		selected = selected[:opts.LimitFiles]
	}

	if opts.DryRun {
		for _, f := range selected {
			result.Files = append(result.Files, downloaders.FileInfo{
				OriginalName: f.name, Size: f.a.Filesize, SourceURL: f.a.URL,
			})
		}
		result.Success = true
		return result, nil
	}

	if err := common.EnsureDirectory(req.OutputDir); err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	for _, f := range selected {
		target := filepath.Join(req.OutputDir, f.name)
		if opts.SkipExisting {
			if fi, ok := common.SkippedFile(target, f.name, f.a.URL); ok {
				result.Files = append(result.Files, fi)
				result.FilesSkipped++
				continue
			}
		}
		if d.verbose {
			fmt.Fprintf(os.Stderr, "⬇️  %s (%s, %s)\n", f.name, f.ds.Title, common.FormatBytes(f.a.Filesize))
		}
		fr, err := common.Fetch(ctx, f.a.URL, target, common.FetchOptions{Client: d.client})
		if err == nil && f.a.Filesize > 0 && fr.N != f.a.Filesize {
			err = fmt.Errorf("size %d, CELLxGENE lists %d", fr.N, f.a.Filesize)
		}
		if err != nil {
			_ = os.Remove(target)
			result.Errors = append(result.Errors, fmt.Sprintf("failed to download %s: %v", f.name, err))
			continue
		}
		result.Files = append(result.Files, downloaders.FileInfo{
			Path: target, OriginalName: f.name, Size: fr.N,
			Checksum: fr.SHA256, ChecksumType: "sha256",
			SourceURL: f.a.URL, DownloadTime: time.Now(),
			ContentType: fr.ContentType, CacheHit: fr.Hit,
		})
		result.BytesDownloaded += fr.N
	}

	result.Duration = time.Since(start)
	result.BytesTotal = result.BytesDownloaded
	for _, f := range result.Files {
		if f.Skipped {
			result.BytesTotal += f.Size
		}
	}
	result.Success = len(result.Errors) == 0

	if req.Metadata != nil && len(result.Files) > 0 {
		witness := &downloaders.WitnessFile{
			HapiqVersion: version.String(),
			DownloadTime: start,
			Source:       d.GetSourceType(),
			OriginalID:   r.PinnedID, // version ID, so refetch gets these exact files
			ResolvedURL:  r.URL,
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

// Search matches every word of the query against each public dataset's title,
// collection name and description, tissue, disease and cell types; --type
// filters on assay.
// ponytail: downloads the whole /datasets and /collections listings (~13 MB,
// ~10 s, in parallel) per search; cache them on disk if that gets annoying.
func (d *Downloader) Search(ctx context.Context, query string, opts downloaders.SearchOptions) ([]downloaders.SearchResult, error) {
	var cols []collection
	colErr := make(chan error, 1)
	go func() {
		_, err := d.getJSON(ctx, "/collections?visibility=PUBLIC", &cols)
		colErr <- err
	}()
	var all []dataset
	if _, err := d.getJSON(ctx, "/datasets", &all); err != nil {
		return nil, err
	}
	if err := <-colErr; err != nil {
		return nil, err
	}
	desc := make(map[string]string, len(cols))
	for _, c := range cols {
		desc[c.CollectionID] = c.Description
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}
	words := strings.Fields(strings.ToLower(query))
	var out []downloaders.SearchResult
	for _, ds := range all {
		if len(out) >= limit {
			break
		}
		text := strings.ToLower(strings.Join([]string{ds.Title, ds.CollectionName, desc[ds.CollectionID],
			labels(ds.Tissue), labels(ds.Disease), labels(ds.CellType)}, " "))
		if !containsAll(text, words) ||
			(opts.Organism != "" && !hasOrganism(ds, opts.Organism)) ||
			(opts.EntryType != "" && !strings.Contains(strings.ToLower(labels(ds.Assay)), strings.ToLower(opts.EntryType))) {
			continue
		}
		var size int64
		for _, a := range ds.Assets {
			size += a.Filesize
		}
		out = append(out, downloaders.SearchResult{
			Accession:   ds.DatasetID,
			Title:       ds.Title,
			Organism:    labels(ds.Organism),
			EntryType:   firstOf(ds.Assay),
			Date:        strings.SplitN(ds.PublishedAt, "T", 2)[0],
			SampleCount: int(ds.CellCount),
			FileSize:    size,
		})
	}
	return out, nil
}

func containsAll(text string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(text, w) {
			return false
		}
	}
	return true
}

// firstOf keeps a table column narrow: "10x 3' v1 +8" for a 9-assay atlas.
func firstOf(ls []label) string {
	if len(ls) <= 1 {
		return labels(ls)
	}
	return fmt.Sprintf("%s +%d", ls[0].Label, len(ls)-1)
}

func labels(ls []label) string {
	s := make([]string, len(ls))
	for i, l := range ls {
		s[i] = l.Label
	}
	return strings.Join(s, ", ")
}

func hasOrganism(ds dataset, want string) bool {
	for _, o := range ds.Organism {
		if strings.Contains(strings.ToLower(o.Label), strings.ToLower(want)) {
			return true
		}
	}
	return false
}

// resolve tries the UUID as a collection, a collection version, a dataset
// (latest version) and a dataset version, in that order.
// ponytail: up to 4 lookups for a dataset-version ID; the API has no
// "what is this UUID" endpoint.
func (d *Downloader) resolve(ctx context.Context, id string) (*resolved, error) {
	uuid := uuidRe.FindString(strings.ToLower(id))
	if uuid == "" {
		return nil, fmt.Errorf("invalid CELLxGENE ID %q: expected a collection or dataset UUID, or a Discover URL", id)
	}

	for _, ep := range []string{"/collections/", "/collection_versions/"} {
		var c collection
		found, err := d.getJSON(ctx, ep+uuid, &c)
		if err != nil {
			return nil, err
		}
		if found {
			r := &resolved{
				Kind: "collection", Title: c.Name, Description: c.Description,
				PinnedID: c.CollectionVersionID, Datasets: c.Datasets,
				URL: "https://cellxgene.cziscience.com/collections/" + c.CollectionID,
			}
			if r.PinnedID == "" {
				r.PinnedID = uuid
			}
			if c.DOI != nil {
				r.DOI = *c.DOI
			}
			return r, nil
		}
	}

	var ds dataset
	var versions []dataset
	found, err := d.getJSON(ctx, "/datasets/"+uuid+"/versions", &versions)
	if err != nil {
		return nil, err
	}
	if found && len(versions) > 0 {
		ds = versions[0] // newest first
	} else if found, err = d.getJSON(ctx, "/dataset_versions/"+uuid, &ds); err != nil {
		return nil, err
	} else if !found {
		return nil, fmt.Errorf("CELLxGENE: no collection or dataset with ID %s", uuid)
	}
	return &resolved{
		Kind: "dataset", Title: ds.Title, PinnedID: ds.DatasetVersionID,
		Datasets: []dataset{ds},
		URL:      "https://cellxgene.cziscience.com/collections/" + ds.CollectionID,
	}, nil
}

// getJSON decodes a GET into v. A 4xx means "not this kind of ID" (the API
// answers 403 or 404 depending on endpoint), reported as found=false.
func (d *Downloader) getJSON(ctx context.Context, p string, v any) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL+p, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "hapiq/"+version.String())
	resp, err := d.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("CELLxGENE API: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return false, fmt.Errorf("CELLxGENE API %s: rate limited (HTTP 429)", p)
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return false, nil
	case resp.StatusCode != http.StatusOK:
		return false, fmt.Errorf("CELLxGENE API %s: HTTP %d", p, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return false, fmt.Errorf("CELLxGENE API %s: %w", p, err)
	}
	return true, nil
}
