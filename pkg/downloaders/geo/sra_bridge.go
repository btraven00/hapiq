package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
	"github.com/btraven00/hapiq/pkg/downloaders/sra"
)

// SRAManifest is the JSON structure written for --raw --dry-run.
type SRAManifest struct {
	Series     string        `json:"series"`
	TotalFiles int           `json:"total_files"`
	TotalBytes int64         `json:"total_bytes"`
	Runs       []SRARunEntry `json:"runs"`
}

// SRARunEntry holds per-run info in the manifest.
type SRARunEntry struct {
	Run        string         `json:"run"`
	Experiment string         `json:"experiment,omitempty"`
	Sample     string         `json:"sample,omitempty"`
	Layout     string         `json:"layout,omitempty"`
	Files      []SRAFileEntry `json:"files"`
}

// SRAFileEntry holds per-file info in the manifest.
type SRAFileEntry struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	MD5   string `json:"md5,omitempty"`
	URL   string `json:"url"`
}

// sraRunDetail bundles SRA run metadata with its ENA file list.
type sraRunDetail struct {
	run  SRARun
	info sra.RunInfo // ENA-sourced file metadata
}

// downloadSRA fetches raw FASTQ files for a GEO series via ENA/SRA.
// When opts.DryRun is true it writes a JSON manifest to stdout instead of
// downloading anything.
func (d *GEODownloader) downloadSRA(
	ctx context.Context,
	gseID, targetDir, gdsUID string,
	opts *downloaders.DownloadOptions,
	result *downloaders.DownloadResult,
) error {
	if d.verbose {
		fmt.Fprintf(os.Stderr,"🧬 Resolving SRA runs for %s...\n", gseID)
	}

	sraRuns, err := d.ResolveGSEToSRARuns(ctx, gdsUID, gseID)
	if err != nil {
		return fmt.Errorf("failed to resolve SRA runs: %w", err)
	}
	if len(sraRuns) == 0 {
		return fmt.Errorf("no SRA runs found for %s — the dataset may not have raw data in SRA, or the accession may not be linked", gseID)
	}
	if d.verbose {
		fmt.Fprintf(os.Stderr,"   Found %d SRA runs — fetching ENA file info\n", len(sraRuns))
	}

	sraDownloader := sra.NewSRADownloader(
		sra.WithVerbose(d.verbose),
		sra.WithTimeout(d.timeout),
	)

	details, totalBytes := d.collectSRADetails(ctx, sraRuns, sraDownloader, result)

	if opts != nil && opts.DryRun {
		return writeSRAManifest(gseID, details, totalBytes)
	}

	// Real download.
	downloaded := 0
	for _, det := range details {
		if opts != nil && opts.LimitFiles > 0 && downloaded >= opts.LimitFiles {
			break
		}
		req := &downloaders.DownloadRequest{
			ID:        det.run.RunAccession,
			OutputDir: targetDir,
			Options:   opts,
			Metadata:  &downloaders.Metadata{Source: "sra", ID: det.run.RunAccession},
		}
		sub, err := sraDownloader.Download(ctx, req)
		if err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("SRA download error for %s: %v", det.run.RunAccession, err))
			continue
		}
		result.Files = append(result.Files, sub.Files...)
		result.BytesDownloaded += sub.BytesDownloaded
		result.Warnings = append(result.Warnings, sub.Warnings...)
		downloaded += len(sub.Files)
	}
	return nil
}

// collectSRADetails fetches ENA file metadata for each SRA run.
func (d *GEODownloader) collectSRADetails(
	ctx context.Context,
	sraRuns []SRARun,
	sraDownloader *sra.SRADownloader,
	result *downloaders.DownloadResult,
) ([]sraRunDetail, int64) {
	var details []sraRunDetail
	var totalBytes int64

	for _, r := range sraRuns {
		if r.RunAccession == "" {
			continue
		}
		meta, err := sraDownloader.GetMetadata(ctx, r.RunAccession)
		if err != nil || meta == nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("ENA lookup failed for %s: %v", r.RunAccession, err))
			continue
		}
		rawRuns, ok := meta.Custom["runs"].([]sra.RunInfo)
		if !ok || len(rawRuns) == 0 {
			continue
		}
		details = append(details, sraRunDetail{run: r, info: rawRuns[0]})
		for _, f := range rawRuns[0].Files {
			totalBytes += f.Bytes
		}
	}
	return details, totalBytes
}

// confirmSRADownload fetches total size from ENA and asks the user to confirm
// before committing to a potentially large download. Skipped when DryRun or
// NonInteractive is set.
func (d *GEODownloader) confirmSRADownload(
	ctx context.Context,
	gseID, gdsUID string,
	opts *downloaders.DownloadOptions,
	result *downloaders.DownloadResult,
) error {
	if opts != nil && (opts.DryRun || opts.NonInteractive) {
		return nil
	}

	sraRuns, err := d.ResolveGSEToSRARuns(ctx, gdsUID, gseID)
	if err != nil || len(sraRuns) == 0 {
		return nil // can't get size, proceed anyway
	}

	sraDownloader := sra.NewSRADownloader(sra.WithVerbose(false), sra.WithTimeout(d.timeout))
	details, _ := d.collectSRADetails(ctx, sraRuns, sraDownloader, result)

	// Count files and bytes, honouring --limit-files so the confirmation
	// prompt reflects what will actually be downloaded.
	limit := 0
	if opts != nil {
		limit = opts.LimitFiles
	}
	countedFiles := 0
	var countedBytes int64
	for _, det := range details {
		for _, f := range det.info.Files {
			if limit > 0 && countedFiles >= limit {
				goto doneCount
			}
			countedFiles++
			countedBytes += f.Bytes
		}
	}
doneCount:

	limited := limit > 0 && countedFiles == limit

	fmt.Fprintf(os.Stderr, "\n📦 Raw data summary for %s:\n", gseID)
	fmt.Fprintf(os.Stderr, "   Runs (total): %d\n", len(details))
	if limited {
		fmt.Fprintf(os.Stderr, "   Files (first %d): %d\n", limit, countedFiles)
		fmt.Fprintf(os.Stderr, "   Estimated size:   %s\n", formatBytes(countedBytes))
	} else {
		fmt.Fprintf(os.Stderr, "   Files: %d\n", countedFiles)
		fmt.Fprintf(os.Stderr, "   Total: %s\n", formatBytes(countedBytes))
	}
	fmt.Fprintln(os.Stderr)

	question := fmt.Sprintf("Download %s of raw FASTQ data?", formatBytes(countedBytes))
	ok, err := common.AskUserConfirmation(question)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("download cancelled by user")
	}
	return nil
}

// formatBytes formats a byte count as a human-readable string.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// writeSRAManifest prints the SRA file manifest as JSON to stdout.
func writeSRAManifest(gseID string, details []sraRunDetail, totalBytes int64) error {
	manifest := SRAManifest{
		Series:     gseID,
		TotalBytes: totalBytes,
	}

	totalFiles := 0
	for _, det := range details {
		entry := SRARunEntry{
			Run:        det.run.RunAccession,
			Experiment: det.run.Experiment,
			Sample:     det.run.Sample,
			Layout:     det.info.Layout,
		}
		for _, f := range det.info.Files {
			entry.Files = append(entry.Files, SRAFileEntry{
				Name:  f.Name,
				Bytes: f.Bytes,
				MD5:   f.MD5,
				URL:   f.HTTPSURL(),
			})
			totalFiles++
		}
		manifest.Runs = append(manifest.Runs, entry)
	}
	manifest.TotalFiles = totalFiles

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(manifest)
}
