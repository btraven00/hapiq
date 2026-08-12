// Package url implements a hapiq downloader that fetches directly from an
// arbitrary HTTP(S) URL. The "ID" is the URL itself.
// In manifests use the dedicated url: field:
//
//   - identifier: my-file
//     url: https://example.com/file.csv
//
// A URL whose path ends in "/" is treated as a directory: its files are
// enumerated from the server's JSON index (Caddy file_server browse, nginx
// autoindex_format json) and fetched into the output directory, subject to the
// usual --include-ext / --filename-pattern / --limit-files filters.
//
//   - identifier: my-dataset
//     url: https://example.com/datasets/be1-fixture/
package url

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/btraven00/hapiq/internal/version"
	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
	"github.com/btraven00/hapiq/pkg/downloaders/sharepoint"
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
	headURL, nameHint, err := d.resolveURL(ctx, id)
	if err != nil {
		// Resolution failed (e.g. a sign-in-only share link); return what we can.
		return &downloaders.Metadata{
			Source: d.GetSourceType(), ID: id, Title: filenameFromURL(id), FileCount: 1,
		}, nil
	}
	title := nameHint
	if title == "" {
		title = filenameFromURL(headURL)
	}

	// A directory is described by its listing, not by a HEAD. Errors are
	// returned rather than swallowed here: without a listing the only thing
	// left to do is fetch the index page as if it were data.
	if isDirectoryURL(headURL) {
		entries, err := d.listDirectory(ctx, headURL)
		if err != nil {
			return nil, err
		}
		meta := &downloaders.Metadata{
			Source: d.GetSourceType(),
			ID:     id,
			Title:  title,
		}
		for _, e := range entries {
			if e.isDir() {
				continue
			}
			meta.FileCount++
			meta.TotalSize += e.Size
		}
		return meta, nil
	}

	meta := &downloaders.Metadata{
		Source:    d.GetSourceType(),
		ID:        id,
		Title:     title,
		FileCount: 1,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, headURL, http.NoBody)
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

// Download fetches req.ID into req.OutputDir: one file, or -- when the URL
// addresses a directory -- every file its index lists.
func (d *URLDownloader) Download(ctx context.Context, req *downloaders.DownloadRequest) (*downloaders.DownloadResult, error) {
	start := time.Now()
	result := &downloaders.DownloadResult{Files: []downloaders.FileInfo{}}

	rawURL, nameHint, err := d.resolveURL(ctx, req.ID)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	if isDirectoryURL(rawURL) {
		return d.downloadDirectory(ctx, rawURL, req, start)
	}

	// Resolve the filename once. A resolver-supplied hint (e.g. SharePoint) wins;
	// otherwise prefer Content-Disposition from a HEAD request, falling back to
	// the URL path basename.
	filename := nameHint
	if filename == "" {
		filename = resolveFilename(ctx, rawURL, d.client)
	}

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

	fi, warnings, err := d.downloadOne(ctx, rawURL, filename, req.OutputDir, opts)
	result.Warnings = append(result.Warnings, warnings...)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	if fi == nil { // an existing file was left in place
		result.Success = true
		return result, nil
	}

	result.Files = append(result.Files, *fi)
	result.BytesDownloaded = fi.Size
	result.BytesTotal = fi.Size
	result.Duration = time.Since(start)
	result.Success = true

	d.writeWitness(req, result, rawURL, start)

	return result, nil
}

// downloadDirectory fetches every file a directory index lists. Subdirectories
// are skipped, not walked: published datasets are flat, and recursion needs a
// depth bound and symlink-cycle handling to be safe.
//
// A failure on any one file aborts the whole directory. A partially fetched
// dataset that reports success is worse than one that fails: the caller cannot
// tell the difference, and downstream stages consume whatever landed.
func (d *URLDownloader) downloadDirectory(ctx context.Context, dirURL string, req *downloaders.DownloadRequest, start time.Time) (*downloaders.DownloadResult, error) {
	result := &downloaders.DownloadResult{Files: []downloaders.FileInfo{}}

	entries, err := d.listDirectory(ctx, dirURL)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	opts := req.Options

	selected := make([]indexEntry, 0, len(entries))
	for _, e := range entries {
		if e.isDir() || e.Name == "" {
			continue
		}
		if !downloaders.ShouldDownload(e.Name, e.Size, opts) {
			continue
		}
		selected = append(selected, e)
		if opts != nil && opts.LimitFiles > 0 && len(selected) >= opts.LimitFiles {
			break
		}
	}

	if len(selected) == 0 {
		result.Errors = append(result.Errors,
			fmt.Sprintf("no files to download in %s (%d entries listed)", dirURL, len(entries)))
		return result, nil
	}

	if opts != nil && opts.DryRun {
		for _, e := range selected {
			result.Files = append(result.Files, downloaders.FileInfo{
				OriginalName: e.Name,
				SourceURL:    childURL(dirURL, e.Name),
				Size:         e.Size,
			})
			result.BytesTotal += e.Size
		}
		result.Success = true
		return result, nil
	}

	if err := common.EnsureDirectory(req.OutputDir); err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	for _, e := range selected {
		fi, warnings, err := d.downloadOne(ctx, childURL(dirURL, e.Name), e.Name, req.OutputDir, opts)
		result.Warnings = append(result.Warnings, warnings...)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			return result, nil
		}
		if fi == nil { // an existing file was left in place
			continue
		}
		result.Files = append(result.Files, *fi)
		result.BytesDownloaded += fi.Size
	}

	result.BytesTotal = result.BytesDownloaded
	result.Duration = time.Since(start)
	result.Success = true

	d.writeWitness(req, result, dirURL, start)

	return result, nil
}

// downloadOne fetches rawURL into outputDir as filename. A nil FileInfo with a
// nil error means an existing file was deliberately left in place.
func (d *URLDownloader) downloadOne(ctx context.Context, rawURL, filename, outputDir string, opts *downloaders.DownloadOptions) (*downloaders.FileInfo, []string, error) {
	var warnings []string

	// This is the line that turns a remote-supplied name into a write, so it
	// does not delegate its own safety: SanitizeFilename maps separators to
	// "_" already, and safeJoin then proves the result is inside outputDir.
	targetPath, err := safeJoin(outputDir, common.SanitizeFilename(filename))
	if err != nil {
		return nil, warnings, err
	}
	targetName := filepath.Base(targetPath)

	// Every operation on the file we are about to write goes through a root
	// handle. Checking the name is not the same as confining the operation:
	// a symlink already sitting in outputDir would still redirect a plain
	// os.Rename or os.Remove, and os.Root refuses to leave the directory at
	// the OS level rather than on our say-so.
	root, err := os.OpenRoot(outputDir)
	if err != nil {
		return nil, warnings, err
	}
	defer func() { _ = root.Close() }()

	if _, statErr := root.Stat(targetName); statErr == nil {
		switch {
		case opts != nil && opts.SkipExisting:
			return nil, nil, nil
		case opts != nil && (opts.Force || opts.NonInteractive):
			// Force or --yes: overwrite silently.
		default:
			confirmed, err := common.AskUserConfirmation(
				fmt.Sprintf("file %q already exists. Overwrite?", filename),
			)
			if err != nil || !confirmed {
				return nil, nil, nil
			}
		}
	}

	if d.verbose {
		_, _ = fmt.Fprintf(os.Stderr, "⬇️  %s\n", rawURL)
	}

	fr, err := common.Fetch(ctx, rawURL, targetPath, common.FetchOptions{Client: d.client})
	if err != nil {
		return nil, warnings, err
	}

	// A directory requested without its trailing slash lands here: the server
	// answers with its browsable index, and storing that page as though it were
	// the dataset is the failure this guard exists to prevent. An extensionless
	// URL answering in HTML is that case; a genuine .html file still downloads.
	//
	// The bytes on disk are sniffed rather than the response header trusted: a
	// cache hit carries no Content-Type (and an index page cached before this
	// check existed would otherwise sail through on every later run).
	if filepath.Ext(filename) == "" && fileLooksLikeHTML(outputDir, targetName) {
		_ = root.Remove(targetName)
		return nil, warnings, fmt.Errorf(
			"%s served an HTML page, not a file — if it is a directory, ask for it "+
				"with a trailing slash (%s/)", rawURL, strings.TrimSuffix(rawURL, "/"))
	}

	// The GET response may reveal a better filename via Content-Disposition than
	// the pre-fetch HEAD did (common with storage backends that only set the
	// header on a redirect target). If so, rename the downloaded file to it.
	if fr.Filename != "" {
		if better := common.SanitizeFilename(fr.Filename); better != "" && better != filepath.Base(targetPath) {
			// Also remote input: the header names this file, so the rename
			// target gets the same containment proof as the original write.
			newPath, err := safeJoin(outputDir, better)
			if err != nil {
				return nil, warnings, err
			}
			if err := root.Rename(targetName, filepath.Base(newPath)); err == nil {
				targetPath = newPath
				targetName = filepath.Base(newPath)
				filename = fr.Filename
			} else {
				warnings = append(warnings, fmt.Sprintf("could not rename to %q: %v", better, err))
			}
		}
	}

	return &downloaders.FileInfo{
		Path:         targetPath,
		OriginalName: filename,
		SourceURL:    rawURL,
		Size:         fr.N,
		Checksum:     fr.SHA256,
		ChecksumType: "sha256",
		CacheHit:     fr.Hit,
		DownloadTime: time.Now(),
		ContentType:  fr.ContentType,
	}, warnings, nil
}

// writeWitness records provenance for everything downloaded in this run.
func (d *URLDownloader) writeWitness(req *downloaders.DownloadRequest, result *downloaders.DownloadResult, resolvedURL string, start time.Time) {
	if req.Metadata == nil {
		return
	}

	req.Metadata.TotalSize = result.BytesDownloaded
	req.Metadata.FileCount = len(result.Files)

	files := make([]downloaders.FileWitness, 0, len(result.Files))
	for _, fi := range result.Files {
		files = append(files, downloaders.FileWitness(fi))
	}

	witness := &downloaders.WitnessFile{
		HapiqVersion: version.String(),
		DownloadTime: start,
		Source:       d.GetSourceType(),
		OriginalID:   req.ID,
		ResolvedURL:  resolvedURL,
		Metadata:     req.Metadata,
		Files:        files,
		DownloadStats: &downloaders.DownloadStats{
			Duration:        result.Duration,
			BytesTotal:      result.BytesDownloaded,
			BytesDownloaded: result.BytesDownloaded,
			FilesTotal:      len(files),
			FilesDownloaded: len(files),
			AverageSpeed:    downloaders.Speed(result.BytesDownloaded, result.Duration),
		},
	}
	if err := common.WriteWitnessFile(req.OutputDir, witness); err != nil {
		result.Warnings = append(result.Warnings, "witness file: "+err.Error())
	} else {
		result.WitnessFile = filepath.Join(req.OutputDir, "hapiq.json")
	}
}

// resolveFilename determines the output filename: Content-Disposition wins over
// the URL path basename.
func resolveFilename(ctx context.Context, rawURL string, client *http.Client) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, http.NoBody)
	if err == nil {
		if resp, err := client.Do(req); err == nil {
			_ = resp.Body.Close()
			if v := common.FilenameFromContentDisposition(resp.Header.Get("Content-Disposition")); v != "" {
				return v
			}
		}
	}
	return filenameFromURL(rawURL)
}

// normalizeURL rewrites known-problematic download URLs to an equivalent that
// serves bytes directly. Currently it maps figshare's web download host
// (https://figshare.com/ndownloader/files/<id>), which answers HTTP 202 while
// it generates the file server-side, to the API host
// (https://ndownloader.figshare.com/files/<id>), which streams the bytes
// without the 202 dance. Any URL it does not recognise is returned unchanged.
func normalizeURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	host := strings.TrimPrefix(u.Host, "www.")
	if host == "figshare.com" && strings.HasPrefix(u.Path, "/ndownloader/") {
		u.Host = "ndownloader.figshare.com"
		u.Path = strings.TrimPrefix(u.Path, "/ndownloader")
		return u.String()
	}

	return rawURL
}

// resolveURL turns a user-supplied ID into a byte-serving download URL. For
// SharePoint share links it follows the link to discover the real path and
// returns a filename hint; for everything else it applies normalizeURL and
// returns no hint.
func (d *URLDownloader) resolveURL(ctx context.Context, id string) (downloadURL, nameHint string, err error) {
	if sharepoint.IsShareURL(id) {
		dl, name, err := sharepoint.Resolve(ctx, d.client, id)
		if err != nil {
			return "", "", fmt.Errorf("resolve sharepoint link: %w", err)
		}
		return dl, name, nil
	}
	return normalizeURL(id), "", nil
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
