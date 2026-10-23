// Package huggingface downloads Hugging Face Hub datasets.
//
// IDs are "<owner>/<name>" or "<owner>/<name>@<revision>" (branch, tag or
// commit). The revision is resolved to a commit sha before anything is
// listed, and that sha is what the download URLs and hapiq.json record, so
// a refetch gets exactly the same files. Set HF_TOKEN for private or gated
// datasets; their files bypass the blob cache and LAN peers.
package huggingface

import (
	"context"
	"crypto/sha1" // #nosec G505 -- git blob ids are sha1; used only to verify small files
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/btraven00/hapiq/internal/version"
	"github.com/btraven00/hapiq/pkg/cache"
	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
)

const defaultBaseURL = "https://huggingface.co"

var idPattern = regexp.MustCompile(`^[\w.-]+/[\w.-]+(@[\w./-]+)?$`)

// linkNext extracts the next-page URL from a Link header.
var linkNext = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// HFDownloader downloads datasets from the Hugging Face Hub.
type HFDownloader struct {
	client  *http.Client
	baseURL string
	token   string
	timeout time.Duration
	verbose bool
}

// Option configures an HFDownloader.
type Option func(*HFDownloader)

// WithTimeout sets the HTTP stall limit.
func WithTimeout(t time.Duration) Option {
	return func(d *HFDownloader) {
		d.timeout = t
		common.SetIdleTimeout(d.client, t)
	}
}

// WithVerbose toggles progress logging to stderr.
func WithVerbose(v bool) Option { return func(d *HFDownloader) { d.verbose = v } }

// WithToken sets the Hub access token (for private or gated datasets).
func WithToken(token string) Option { return func(d *HFDownloader) { d.token = token } }

// New creates a Hugging Face downloader.
func New(opts ...Option) *HFDownloader {
	d := &HFDownloader{
		client:  &http.Client{Timeout: 60 * time.Second},
		baseURL: defaultBaseURL,
		timeout: 60 * time.Second,
	}
	for _, o := range opts {
		o(d)
	}
	return d
}

// GetSourceType returns the source identifier.
func (d *HFDownloader) GetSourceType() string { return "hf" }

// repoInfo is the subset of /api/datasets/<repo>/revision/<rev> hapiq uses.
type repoInfo struct {
	LastModified time.Time `json:"lastModified"`
	ID           string    `json:"id"`
	SHA          string    `json:"sha"`
	Description  string    `json:"description"`
	Author       string    `json:"author"`
	Tags         []string  `json:"tags"`
	Gated        any       `json:"gated"` // false or "auto"/"manual"
	Private      bool      `json:"private"`
}

// restricted reports whether the dataset needs a token (gated or private).
func (r *repoInfo) restricted() bool {
	return r.Private || (r.Gated != nil && r.Gated != false)
}

// hfFile is one entry of the recursive tree listing.
type hfFile struct {
	LFS  *struct{ OID string } `json:"lfs"`
	Type string                `json:"type"`
	OID  string                `json:"oid"` // git blob sha1
	Path string                `json:"path"`
	Size int64                 `json:"size"`
}

func splitID(id string) (repo, rev string) {
	repo, rev, ok := strings.Cut(strings.TrimSpace(id), "@")
	if !ok || rev == "" {
		rev = "main"
	}
	return repo, rev
}

// Validate checks the ID format and that the revision resolves.
func (d *HFDownloader) Validate(ctx context.Context, id string) (*downloaders.ValidationResult, error) {
	result := &downloaders.ValidationResult{ID: strings.TrimSpace(id), SourceType: d.GetSourceType()}
	if !idPattern.MatchString(result.ID) {
		result.Errors = []string{fmt.Sprintf("invalid Hugging Face dataset id %q: expected <owner>/<name>[@revision]", id)}
		return result, nil
	}
	if _, err := d.resolve(ctx, result.ID); err != nil {
		result.Errors = []string{err.Error()}
		return result, nil
	}
	result.Valid = true
	return result, nil
}

// GetMetadata returns dataset metadata, with size and file count from the tree.
func (d *HFDownloader) GetMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	info, err := d.resolve(ctx, id)
	if err != nil {
		return nil, err
	}
	files, err := d.listFiles(ctx, info.ID, info.SHA)
	if err != nil {
		return nil, err
	}

	meta := &downloaders.Metadata{
		Source:       d.GetSourceType(),
		ID:           info.ID + "@" + info.SHA,
		Title:        info.ID,
		Description:  strings.TrimSpace(info.Description),
		Version:      info.SHA,
		LastModified: info.LastModified,
		Tags:         info.Tags,
		FileCount:    len(files),
		Custom:       map[string]any{"revision": info.SHA},
	}
	if info.Author != "" {
		meta.Authors = []string{info.Author}
	}
	for _, t := range info.Tags {
		if lic, ok := strings.CutPrefix(t, "license:"); ok {
			meta.License = lic
		}
	}
	for _, f := range files {
		meta.TotalSize += f.Size
	}
	return meta, nil
}

// Download fetches the (filtered) files of the dataset at its pinned commit.
func (d *HFDownloader) Download(ctx context.Context, req *downloaders.DownloadRequest) (*downloaders.DownloadResult, error) {
	start := time.Now()
	result := &downloaders.DownloadResult{Files: []downloaders.FileInfo{}}

	info, err := d.resolve(ctx, req.ID)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	files, err := d.listFiles(ctx, info.ID, info.SHA)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	opts := req.Options
	var selected []hfFile
	for _, f := range files {
		if opts != nil && opts.LimitFiles > 0 && len(selected) >= opts.LimitFiles {
			break
		}
		if downloaders.ShouldDownload(path.Base(f.Path), f.Size, opts) {
			selected = append(selected, f)
		}
	}

	if opts != nil && opts.DryRun {
		for _, f := range selected {
			result.Files = append(result.Files, downloaders.FileInfo{
				Path:           f.Path,
				OriginalName:   path.Base(f.Path),
				Size:           f.Size,
				SourceURL:      d.fileURL(info.ID, info.SHA, f.Path),
				SourceChecksum: sourceChecksum(f),
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
		targetPath := filepath.Join(req.OutputDir, filepath.FromSlash(f.Path))

		if opts != nil && opts.SkipExisting {
			if _, err := os.Stat(targetPath); err == nil {
				if d.verbose {
					fmt.Fprintf(os.Stderr, "⏭️  Skipping existing: %s\n", f.Path)
				}
				result.FilesSkipped++
				continue
			}
		}
		if d.verbose {
			fmt.Fprintf(os.Stderr, "⬇️  %s (%s)\n", f.Path, common.FormatBytes(f.Size))
		}

		fi, err := d.downloadFile(ctx, info, f, targetPath)
		if err != nil {
			_ = os.Remove(targetPath)
			result.Errors = append(result.Errors, fmt.Sprintf("failed to download %s: %v", f.Path, err))
			continue
		}
		result.Files = append(result.Files, *fi)
		result.BytesDownloaded += fi.Size
	}

	result.Duration = time.Since(start)
	result.BytesTotal = result.BytesDownloaded
	result.Success = len(result.Errors) == 0

	if req.Metadata != nil && len(result.Files) > 0 {
		witness := &downloaders.WitnessFile{
			HapiqVersion: version.String(),
			DownloadTime: start,
			Source:       d.GetSourceType(),
			// The pinned form, so a refetch gets this exact commit.
			OriginalID:  info.ID + "@" + info.SHA,
			ResolvedURL: d.baseURL + "/datasets/" + info.ID + "/tree/" + info.SHA,
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

// downloadFile fetches one file and checks it against the Hub's own hash:
// the LFS sha256 for large files, the git blob sha1 for the rest.
func (d *HFDownloader) downloadFile(ctx context.Context, info *repoInfo, f hfFile, targetPath string) (*downloaders.FileInfo, error) {
	if err := common.EnsureDirectory(filepath.Dir(targetPath)); err != nil {
		return nil, err
	}
	rawURL := d.fileURL(info.ID, info.SHA, f.Path)
	if info.restricted() {
		// Gated/private files stay out of the blob cache: `hapiq cache serve`
		// would hand them to LAN peers that have no token or never accepted
		// the terms. Peers are skipped too, so nothing is looked up by URL.
		ctx = cache.WithPeers(cache.WithCache(ctx, nil), nil)
	}
	res, err := common.Fetch(ctx, rawURL, targetPath, common.FetchOptions{Client: d.client, ExtraHeaders: d.authHeader()})
	if err != nil {
		// Gated datasets list their files publicly; the 401 comes only here.
		if s := err.Error(); strings.Contains(s, "HTTP 401") || strings.Contains(s, "HTTP 403") {
			return nil, fmt.Errorf("%w (gated or private: set HF_TOKEN and accept the dataset's terms on the Hub)", err)
		}
		return nil, err
	}

	if f.LFS != nil {
		if !strings.EqualFold(res.SHA256, f.LFS.OID) {
			return nil, fmt.Errorf("sha256 %s, the Hub lists %s", res.SHA256, f.LFS.OID)
		}
	} else if f.OID != "" {
		got, err := gitBlobSHA1(targetPath)
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(got, f.OID) {
			return nil, fmt.Errorf("git blob sha1 %s, the Hub lists %s", got, f.OID)
		}
	}

	return &downloaders.FileInfo{
		Path:           f.Path,
		OriginalName:   path.Base(f.Path),
		Size:           res.N,
		Checksum:       res.SHA256,
		ChecksumType:   "sha256",
		SourceChecksum: sourceChecksum(f),
		SourceURL:      rawURL,
		ContentType:    res.ContentType,
		DownloadTime:   time.Now(),
		CacheHit:       res.Hit,
	}, nil
}

// resolve pins id's revision to a commit sha.
func (d *HFDownloader) resolve(ctx context.Context, id string) (*repoInfo, error) {
	repo, rev := splitID(id)
	var info repoInfo
	u := d.baseURL + "/api/datasets/" + repo + "/revision/" + url.PathEscape(rev)
	if _, err := d.getJSON(ctx, u, &info); err != nil {
		return nil, fmt.Errorf("%s@%s: %w", repo, rev, err)
	}
	if info.SHA == "" {
		return nil, fmt.Errorf("%s@%s: the Hub returned no commit sha", repo, rev)
	}
	return &info, nil
}

// listFiles returns every file in the dataset at sha, following pagination.
// .gitattributes is repo plumbing, not data, and is left out.
func (d *HFDownloader) listFiles(ctx context.Context, repo, sha string) ([]hfFile, error) {
	var files []hfFile
	next := d.baseURL + "/api/datasets/" + repo + "/tree/" + sha + "?recursive=true"
	for next != "" {
		var page []hfFile
		h, err := d.getJSON(ctx, next, &page)
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", repo, err)
		}
		for _, f := range page {
			if f.Type != "file" || f.Path == ".gitattributes" {
				continue
			}
			// Paths become local paths under --out; refuse anything that escapes it.
			if !filepath.IsLocal(filepath.FromSlash(f.Path)) {
				return nil, fmt.Errorf("list %s: unsafe path %q", repo, f.Path)
			}
			files = append(files, f)
		}
		next = ""
		if m := linkNext.FindStringSubmatch(h.Get("Link")); m != nil {
			next = m[1]
		}
	}
	return files, nil
}

func (d *HFDownloader) getJSON(ctx context.Context, rawURL string, v any) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	for k, val := range d.authHeader() {
		req.Header.Set(k, val)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		// The Hub answers 401 for a missing repo too, so both are possible.
		hint := "or private/gated: set HF_TOKEN"
		if d.token != "" {
			hint = "or HF_TOKEN has no access: for a gated dataset, accept its terms on the Hub"
		}
		return nil, fmt.Errorf("HTTP %d: dataset not found (%s)", resp.StatusCode, hint)
	case resp.StatusCode != http.StatusOK:
		var e struct{ Error string }
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, e.Error)
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp.Header, json.NewDecoder(resp.Body).Decode(v)
}

func (d *HFDownloader) authHeader() map[string]string {
	if d.token == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + d.token}
}

func (d *HFDownloader) fileURL(repo, sha, p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return d.baseURL + "/datasets/" + repo + "/resolve/" + sha + "/" + strings.Join(segs, "/")
}

// sourceChecksum is the Hub's content hash in hapiq's algo:hex form. Only
// LFS files have one; a git blob sha1 is not a plain sha1 of the content.
func sourceChecksum(f hfFile) string {
	if f.LFS != nil && f.LFS.OID != "" {
		return "sha256:" + f.LFS.OID
	}
	return ""
}

// gitBlobSHA1 computes the git object id of a file: sha1("blob <size>\0" + content).
func gitBlobSHA1(p string) (string, error) {
	f, err := os.Open(filepath.Clean(p)) // #nosec G304 -- file this downloader just wrote
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	h := sha1.New() // #nosec G401 -- git object id, not security
	_, _ = fmt.Fprintf(h, "blob %d\x00", st.Size())
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
