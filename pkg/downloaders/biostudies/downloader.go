package biostudies

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

// idPattern matches BioStudies accessions: S-<COLLECTION><digits> (e.g. S-BSST1502, S-EPMC123)
// or E-<TYPE>-<digits> (e.g. E-MTAB-8077).
var idPattern = regexp.MustCompile(`^(S-[A-Z]{2,}\d+|E-[A-Z]+-\d+)$`)

// BioStudiesDownloader downloads files attached to BioStudies studies.
type BioStudiesDownloader struct {
	client  *http.Client
	timeout time.Duration
	verbose bool
}

// Option configures a BioStudiesDownloader.
type Option func(*BioStudiesDownloader)

// WithTimeout sets the HTTP timeout.
func WithTimeout(t time.Duration) Option {
	return func(d *BioStudiesDownloader) {
		d.timeout = t
		d.client = newHTTPClient(t)
	}
}

// WithVerbose toggles progress logging to stderr.
func WithVerbose(v bool) Option { return func(d *BioStudiesDownloader) { d.verbose = v } }

// NewBioStudiesDownloader creates a new downloader.
func NewBioStudiesDownloader(opts ...Option) *BioStudiesDownloader {
	d := &BioStudiesDownloader{timeout: 60 * time.Second}
	d.client = newHTTPClient(d.timeout)
	for _, o := range opts {
		o(d)
	}
	return d
}

// GetSourceType returns the source identifier.
func (d *BioStudiesDownloader) GetSourceType() string { return "biostudies" }

// Validate checks the accession format and that the study exists.
func (d *BioStudiesDownloader) Validate(ctx context.Context, id string) (*downloaders.ValidationResult, error) {
	result := &downloaders.ValidationResult{
		ID:         id,
		SourceType: d.GetSourceType(),
	}
	if !idPattern.MatchString(id) {
		result.Errors = []string{fmt.Sprintf(
			"invalid BioStudies accession %q: expected S-<COLLECTION><digits> or E-<TYPE>-<digits>", id,
		)}
		return result, nil
	}
	if _, err := fetchStudy(ctx, d.client, id); err != nil {
		result.Errors = []string{err.Error()}
		return result, nil
	}
	result.Valid = true
	return result, nil
}

// GetMetadata returns study-level metadata without downloading files.
func (d *BioStudiesDownloader) GetMetadata(ctx context.Context, id string) (*downloaders.Metadata, error) {
	study, err := fetchStudy(ctx, d.client, id)
	if err != nil {
		return nil, err
	}

	files := walkFiles(&study.Section)
	var totalSize int64
	for _, f := range files {
		if f.Size > 0 {
			totalSize += f.Size
		}
	}

	meta := &downloaders.Metadata{
		Source:      d.GetSourceType(),
		ID:          study.Accno,
		Title:       attrValue(study.Attributes, "Title"),
		Description: attrValue(study.Section.Attributes, "Description"),
		FileCount:   len(files),
		TotalSize:   totalSize,
		Custom: map[string]any{
			"section_type": study.Section.Type,
		},
	}
	if rd := attrValue(study.Attributes, "ReleaseDate"); rd != "" {
		if t, err := time.Parse("2006-01-02", rd); err == nil {
			meta.Created = t
		}
		meta.Custom["release_date"] = rd
	}
	if hcaUUID := attrValue(study.Attributes, "HCA Project UUID"); hcaUUID != "" {
		meta.Custom["hca_project_uuid"] = hcaUUID
	}
	if links := summarizeLinks(study.Section.Links); len(links) > 0 {
		meta.Custom["link_summary"] = links
	}
	return meta, nil
}

// summarizeLinks groups outgoing links by Type and counts/samples each group.
func summarizeLinks(links []Link) map[string]map[string]any {
	if len(links) == 0 {
		return nil
	}
	groups := map[string][]string{}
	for _, l := range links {
		t := l.LinkType()
		if t == "" {
			t = "Other"
		}
		groups[t] = append(groups[t], l.URL)
	}
	out := make(map[string]map[string]any, len(groups))
	for t, urls := range groups {
		sample := urls
		if len(sample) > 5 {
			sample = sample[:5]
		}
		out[t] = map[string]any{
			"count":   len(urls),
			"example": sample,
		}
	}
	return out
}

// Download fetches all (filtered) files attached to the study.
func (d *BioStudiesDownloader) Download(ctx context.Context, req *downloaders.DownloadRequest) (*downloaders.DownloadResult, error) {
	start := time.Now()
	result := &downloaders.DownloadResult{Files: []downloaders.FileInfo{}}

	study, err := fetchStudy(ctx, d.client, req.ID)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	files := walkFiles(&study.Section)
	if len(files) == 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("study %s has no attached files", study.Accno))
		for _, h := range emptyStudyHints(study) {
			result.Warnings = append(result.Warnings, "hint: "+h)
		}
	}

	opts := req.Options

	// Dry-run: enumerate without downloading.
	if opts != nil && opts.DryRun {
		for _, f := range files {
			if !downloaders.ShouldDownload(f.Path, f.Size, opts) {
				continue
			}
			result.Files = append(result.Files, downloaders.FileInfo{
				OriginalName: f.Path,
				Size:         f.Size,
				SourceURL:    f.DownloadURL(study.Accno),
			})
		}
		result.Success = true
		return result, nil
	}

	if err := common.EnsureDirectory(req.OutputDir); err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result, nil
	}

	downloaded := 0
	for _, f := range files {
		if !downloaders.ShouldDownload(f.Path, f.Size, opts) {
			continue
		}
		if opts != nil && opts.LimitFiles > 0 && downloaded >= opts.LimitFiles {
			break
		}

		// Preserve the file's path within the study under the output dir.
		relPath := filepath.FromSlash(f.Path)
		targetPath := filepath.Join(req.OutputDir, relPath)
		if err := common.EnsureDirectory(filepath.Dir(targetPath)); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("mkdir %s: %v", filepath.Dir(targetPath), err))
			continue
		}

		if opts != nil && opts.SkipExisting {
			if _, err := os.Stat(targetPath); err == nil {
				if d.verbose {
					fmt.Fprintf(os.Stderr, "⏭️  Skipping existing: %s\n", f.Path)
				}
				continue
			}
		}

		srcURL := f.DownloadURL(study.Accno)
		if d.verbose {
			fmt.Fprintf(os.Stderr, "⬇️  %s → %s\n", f.Path, srcURL)
		}

		fi, err := d.downloadFile(ctx, srcURL, targetPath)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed %s: %v", f.Path, err))
			continue
		}
		fi.OriginalName = f.Path
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

func (d *BioStudiesDownloader) downloadFile(ctx context.Context, rawURL, targetPath string) (*downloaders.FileInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
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

// emptyStudyHints suggests where the user might find data when a study
// itself carries no file attachments. Looks for HCA project UUIDs and
// outgoing BioSample / array-express links.
func emptyStudyHints(study *Study) []string {
	var hints []string

	if uuid := attrValue(study.Attributes, "HCA Project UUID"); uuid != "" {
		hints = append(hints, fmt.Sprintf(
			"this study links to HCA project %s — try `hapiq download hca %s` for processed/count matrices",
			uuid, uuid,
		))
	}

	counts := map[string]int{}
	for _, l := range study.Section.Links {
		t := l.LinkType()
		if t == "" {
			t = "Other"
		}
		counts[t]++
	}
	for t, n := range counts {
		switch strings.ToLower(t) {
		case "biosample":
			hints = append(hints, fmt.Sprintf(
				"%d BioSample link(s) found; raw reads can be fetched via `hapiq download sra <ENA project/run>` (note: BioSamples → FASTQs, not count matrices)",
				n,
			))
		case "arrayexpress", "ena", "sra":
			hints = append(hints, fmt.Sprintf("%d %s link(s) found; check the corresponding source", n, t))
		}
	}
	return hints
}


