// Package sra downloads raw sequencing reads (FASTQ) from the ENA/SRA
// via EBI's public HTTPS mirror. No special tools (sra-tools, fasterq-dump)
// are required.
//
// Supported input accessions:
//   - PRJNA*, PRJNB*, PRJEB*  – BioProject (all runs in the project)
//   - SRR*, ERR*, DRR*        – individual SRA runs
//   - SRX*, ERX*, DRX*        – SRA experiments (→ all runs)
//   - SRS*, ERS*, DRS*        – SRA samples (→ all runs)
//
// For GEO series (GSE*), use the GEO downloader with --include-sra.
package sra

import (
	"context"
	"crypto/md5" // #nosec G501 -- MD5 used for checksum verification only, as provided by ENA
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
)

// SRADownloader implements Downloader for SRA/ENA datasets.
type SRADownloader struct {
	client  *http.Client
	timeout time.Duration
	verbose bool
}

// Option configures the SRADownloader.
type Option func(*SRADownloader)

// WithTimeout sets the HTTP timeout.
func WithTimeout(t time.Duration) Option {
	return func(d *SRADownloader) {
		d.timeout = t
		d.client.Timeout = t
	}
}

// WithVerbose enables verbose output.
func WithVerbose(v bool) Option {
	return func(d *SRADownloader) { d.verbose = v }
}

// NewSRADownloader creates a new SRADownloader.
func NewSRADownloader(opts ...Option) *SRADownloader {
	d := &SRADownloader{
		client:  &http.Client{Timeout: 60 * time.Second},
		timeout: 60 * time.Second,
	}
	for _, o := range opts {
		o(d)
	}
	return d
}

// GetSourceType returns the source type identifier.
func (d *SRADownloader) GetSourceType() string { return "sra" }

var sraPattern = regexp.MustCompile(`^(PRJ[NEDB][A-Z]?\d+|[ESDB]RR\d+|[ESDB]RX\d+|[ESDB]RS\d+)$`)

// Validate checks the accession format.
func (d *SRADownloader) Validate(_ context.Context, id string) (*downloaders.ValidationResult, error) {
	clean := strings.ToUpper(strings.TrimSpace(id))
	result := &downloaders.ValidationResult{
		ID:         clean,
		SourceType: d.GetSourceType(),
		Valid:       sraPattern.MatchString(clean),
	}
	if !result.Valid {
		result.Errors = []string{fmt.Sprintf("unrecognized SRA/BioProject accession format: %q", id)}
	}
	return result, nil
}

// GetMetadata fetches run-level metadata from the ENA filereport API.
func (d *SRADownloader) GetMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	runs, err := d.fetchRunInfo(ctx, strings.ToUpper(strings.TrimSpace(id)))
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, fmt.Errorf("no runs found for %s in ENA", id)
	}

	var totalBytes int64
	for _, r := range runs {
		for _, f := range r.Files {
			totalBytes += f.Bytes
		}
	}

	meta := &downloaders.Metadata{
		Source:    d.GetSourceType(),
		ID:        id,
		FileCount: len(runs),
		TotalSize: totalBytes,
		Custom:    map[string]any{"runs": runs},
	}
	if len(runs) > 0 {
		meta.Title = fmt.Sprintf("%d SRA run(s) for %s", len(runs), id)
	}
	return meta, nil
}

// Download fetches the FASTQ files with MD5 verification.
func (d *SRADownloader) Download(ctx context.Context, req *downloaders.DownloadRequest) (*downloaders.DownloadResult, error) {
	start := time.Now()
	result := &downloaders.DownloadResult{Files: []downloaders.FileInfo{}}

	id := strings.ToUpper(strings.TrimSpace(req.ID))
	runs, err := d.fetchRunInfo(ctx, id)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}
	if len(runs) == 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("no runs found for %s in ENA", id))
		result.Success = true
		return result, nil
	}

	opts := req.Options

	// Dry-run: enumerate without downloading.
	if opts != nil && opts.DryRun {
		return d.dryRun(runs, opts, result), nil
	}

	if err := common.EnsureDirectory(req.OutputDir); err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	// Collect all files across runs, apply filters, respect --limit-files.
	type pendingFile struct {
		run  RunInfo
		file ENAFile
	}
	var pending []pendingFile
	for _, run := range runs {
		for _, f := range run.Files {
			if opts == nil || downloaders.ShouldDownload(f.Name, f.Bytes, opts) {
				pending = append(pending, pendingFile{run, f})
			}
		}
	}
	if opts != nil && opts.LimitFiles > 0 && len(pending) > opts.LimitFiles {
		if d.verbose {
			fmt.Printf("ℹ️  --limit-files %d: downloading %d of %d files\n",
				opts.LimitFiles, opts.LimitFiles, len(pending))
		}
		pending = pending[:opts.LimitFiles]
	}

	// Parallel download with configurable concurrency.
	concurrency := 2 // conservative default for large FASTQ files
	if opts != nil && opts.MaxConcurrent > 0 {
		concurrency = opts.MaxConcurrent
	}

	type dlResult struct {
		fi  *downloaders.FileInfo
		err error
		msg string
	}

	sem := make(chan struct{}, concurrency)
	dlResults := make(chan dlResult, len(pending))
	var wg sync.WaitGroup

	for _, p := range pending {
		p := p
		runDir := filepath.Join(req.OutputDir, p.run.RunAccession)
		if err := common.EnsureDirectory(runDir); err != nil {
			dlResults <- dlResult{err: err, msg: fmt.Sprintf("mkdir %s", runDir)}
			continue
		}

		targetPath := filepath.Join(runDir, p.file.Name)

		if opts != nil && opts.SkipExisting {
			if _, err := os.Stat(targetPath); err == nil {
				if d.verbose {
					fmt.Printf("⏭️  Skipping existing: %s\n", p.file.Name)
				}
				continue
			}
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if d.verbose {
				fmt.Printf("⬇️  %s (%s)\n", p.file.Name, common.FormatBytes(p.file.Bytes))
			}

			fi, err := d.downloadWithMD5(ctx, p.file.HTTPSURL(), targetPath, p.file.MD5)
			if err != nil {
				dlResults <- dlResult{err: err, msg: p.file.Name}
			} else {
				dlResults <- dlResult{fi: fi}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(dlResults)
	}()

	for r := range dlResults {
		if r.err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("failed to download %s: %v", r.msg, r.err))
		} else if r.fi != nil {
			result.Files = append(result.Files, *r.fi)
			result.BytesDownloaded += r.fi.Size
		}
	}

	result.Duration = time.Since(start)
	result.BytesTotal = result.BytesDownloaded
	result.Success = len(result.Errors) == 0

	// Write witness file.
	if req.Metadata != nil {
		witness := &downloaders.WitnessFile{
			HapiqVersion:  "dev",
			DownloadTime:  start,
			Source:        d.GetSourceType(),
			OriginalID:    req.ID,
			Metadata:      req.Metadata,
			Files:         make([]downloaders.FileWitness, len(result.Files)),
			DownloadStats: &downloaders.DownloadStats{Duration: result.Duration, BytesDownloaded: result.BytesDownloaded},
			Options:       req.Options,
		}
		for i, f := range result.Files {
			witness.Files[i] = downloaders.FileWitness(f)
		}
		if err := common.WriteWitnessFile(req.OutputDir, witness); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("witness file: %v", err))
		} else {
			result.WitnessFile = filepath.Join(req.OutputDir, "hapiq.json")
		}
	}

	return result, nil
}

// dryRun lists what would be downloaded without writing anything.
func (d *SRADownloader) dryRun(runs []RunInfo, opts *downloaders.DownloadOptions, result *downloaders.DownloadResult) *downloaders.DownloadResult {
	count := 0
	for _, run := range runs {
		for _, f := range run.Files {
			if opts == nil || downloaders.ShouldDownload(f.Name, f.Bytes, opts) {
				if opts != nil && opts.LimitFiles > 0 && count >= opts.LimitFiles {
					break
				}
				result.Files = append(result.Files, downloaders.FileInfo{
					OriginalName: f.Name,
					SourceURL:    f.HTTPSURL(),
					Size:         f.Bytes,
					Checksum:     f.MD5,
					ChecksumType: "md5",
				})
				count++
			}
		}
	}
	result.Success = true
	return result
}

// downloadWithMD5 downloads a file and verifies the ENA-provided MD5.
// TODO(cache): integrate local cache — requires computing sha256 in parallel with md5
// during streaming, then calling cache.Put with the sha256. common.Fetch cannot be
// used directly because ENA supplies MD5 checksums that must be verified inline.
func (d *SRADownloader) downloadWithMD5(ctx context.Context, url, targetPath, expectedMD5 string) (*downloaders.FileInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	f, err := os.Create(filepath.Clean(targetPath)) // #nosec G304 -- targetPath is a constructed download path
	if err != nil {
		return nil, err
	}
	defer f.Close()

	h := md5.New() // #nosec G401 -- ENA-provided MD5 checksum verification
	written, err := io.Copy(io.MultiWriter(f, h), resp.Body)
	if err != nil {
		return nil, fmt.Errorf("write %s: %w", targetPath, err)
	}

	gotMD5 := hex.EncodeToString(h.Sum(nil))
	if expectedMD5 != "" && gotMD5 != expectedMD5 {
		_ = os.Remove(targetPath) // don't keep corrupt file
		return nil, fmt.Errorf("MD5 mismatch for %s: got %s, want %s", filepath.Base(targetPath), gotMD5, expectedMD5)
	}

	return &downloaders.FileInfo{
		Path:         targetPath,
		OriginalName: filepath.Base(targetPath),
		Size:         written,
		Checksum:     gotMD5,
		ChecksumType: "md5",
		SourceURL:    url,
		DownloadTime: time.Now(),
	}, nil
}
