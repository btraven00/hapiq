// Package ensembl provides download functionality for Ensembl Genomes datasets.
package ensembl

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
)

// SpeciesInfo represents information about a species from the Ensembl species list.
type SpeciesInfo struct {
	Name        string
	CoreDB      string
	TaxonID     string
	Assembly    string
	Source      string
	Collection  string
	SpeciesName string // Used for URL construction
}

// downloadEnsemblData performs the actual download of Ensembl data.
func (d *EnsemblDownloader) downloadEnsemblData(ctx context.Context, req *EnsemblRequest, outputDir string, options *downloaders.DownloadOptions, result *downloaders.DownloadResult) error {
	// Create output directory structure
	targetDir := filepath.Join(outputDir, fmt.Sprintf("ensembl_%s_release-%s_%s", req.Database, req.Version, req.Content))
	if err := common.EnsureDirectory(targetDir); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	// Download and parse species list
	speciesListURL := fmt.Sprintf("%s/release-%s/%s/species_Ensembl%s.txt",
		d.ftpBaseURL, req.Version, req.Database, strings.Title(string(req.Database)))

	speciesList, err := d.downloadSpeciesList(ctx, speciesListURL, targetDir)
	if err != nil {
		return fmt.Errorf("failed to download species list: %w", err)
	}

	// Parse species information
	species, err := d.parseSpeciesList(speciesList)
	if err != nil {
		return fmt.Errorf("failed to parse species list: %w", err)
	}

	// Filter species if specific species requested
	if req.Species != "" {
		filteredSpecies := []SpeciesInfo{}
		for _, s := range species {
			if strings.Contains(strings.ToLower(s.SpeciesName), strings.ToLower(req.Species)) ||
				strings.Contains(strings.ToLower(s.Name), strings.ToLower(req.Species)) {
				filteredSpecies = append(filteredSpecies, s)
			}
		}
		if len(filteredSpecies) == 0 {
			return fmt.Errorf("no species found matching: %s", req.Species)
		}
		species = filteredSpecies
	}

	// Generate download URLs
	urls, err := d.generateDownloadURLs(req, species)
	if err != nil {
		return fmt.Errorf("failed to generate download URLs: %w", err)
	}

	// Set up progress tracking with FTP-aware concurrency limits
	maxConcurrent := 1 // Very conservative default for FTP
	if options != nil && options.MaxConcurrent > 0 {
		maxConcurrent = options.MaxConcurrent
	}

	// For FTP URLs, further limit concurrency to avoid server limits
	if len(urls) > 0 && strings.HasPrefix(urls[0], "ftp://") {
		if maxConcurrent > 2 {
			maxConcurrent = 2 // Max 2 concurrent FTP connections
		}
	}

	// Estimate total size for progress tracking
	var totalSize int64
	for _, s := range species {
		totalSize += d.estimateFileSize(req, s)
	}

	tracker := common.NewProgressTracker(len(urls), totalSize, nil, d.verbose)

	// Download files with progress tracking
	semaphore := make(chan struct{}, maxConcurrent)
	errChan := make(chan error, len(urls))
	fileChan := make(chan *downloaders.FileInfo, len(urls))

	// Add rate limiting for FTP downloads
	var rateLimiter <-chan time.Time
	if len(urls) > 0 && strings.HasPrefix(urls[0], "ftp://") {
		// Rate limit FTP downloads to 1 per second to be server-friendly
		rateLimiter = time.Tick(1 * time.Second)
	}

	for i, url := range urls {
		go func(idx int, downloadURL string) {
			semaphore <- struct{}{}        // Acquire semaphore
			defer func() { <-semaphore }() // Release semaphore

			// Rate limit FTP downloads
			if rateLimiter != nil {
				<-rateLimiter
			}

			fileName := filepath.Base(downloadURL)
			targetPath := filepath.Join(targetDir, fileName)

			// Skip if file exists and skip_existing is enabled
			if options != nil && options.SkipExisting {
				if _, err := os.Stat(targetPath); err == nil {
					fileChan <- &downloaders.FileInfo{
						Path:         targetPath,
						OriginalName: fileName,
						Size:         0, // We don't know the size without downloading
						DownloadTime: time.Now(),
						SourceURL:    downloadURL,
					}
					errChan <- nil
					return
				}
			}

			fileInfo, err := d.downloadFileWithProgress(ctx, downloadURL, targetPath, fileName, -1, tracker)
			if err != nil {
				errChan <- fmt.Errorf("failed to download %s: %w", fileName, err)
				return
			}

			fileChan <- fileInfo
			errChan <- nil
		}(i, url)
	}

	// Collect results
	var downloadErrors []string
	for i := 0; i < len(urls); i++ {
		select {
		case err := <-errChan:
			if err != nil {
				downloadErrors = append(downloadErrors, err.Error())
			}
		case file := <-fileChan:
			if file != nil {
				result.Files = append(result.Files, *file)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Add any download errors to result
	if len(downloadErrors) > 0 {
		result.Warnings = append(result.Warnings, downloadErrors...)
	}

	// Create witness file
	witness := &downloaders.WitnessFile{
		HapiqVersion: "dev",
		DownloadTime: time.Now(),
		Source:       d.GetSourceType(),
		OriginalID:   fmt.Sprintf("%s:%s:%s", req.Database, req.Version, req.Content),
		Metadata:     result.Metadata,
		Files:        make([]downloaders.FileWitness, len(result.Files)),
		DownloadStats: &downloaders.DownloadStats{
			Duration:        result.Duration,
			BytesTotal:      result.BytesTotal,
			BytesDownloaded: result.BytesDownloaded,
			FilesTotal:      len(urls),
			FilesDownloaded: len(result.Files),
			FilesSkipped:    0,
			FilesFailed:     len(downloadErrors),
			AverageSpeed:    0, // Will be calculated later
			MaxConcurrent:   maxConcurrent,
			ResumedDownload: false,
		},
		Options: options,
	}

	// Convert FileInfo to FileWitness
	for i, file := range result.Files {
		witness.Files[i] = downloaders.FileWitness(file)
	}

	// Write witness file
	if err := common.WriteWitnessFile(targetDir, witness); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("failed to write witness file: %v", err))
	} else {
		result.WitnessFile = filepath.Join(targetDir, "hapiq.json")
	}

	return nil
}

// downloadSpeciesList downloads the species list file.
func (d *EnsemblDownloader) downloadSpeciesList(ctx context.Context, url, targetDir string) (string, error) {
	resp, err := d.protoClient.Get(ctx, url)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("received nil response")
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Status %d when downloading species list", resp.StatusCode)
	}

	if resp.Body == nil {
		return "", fmt.Errorf("response body is nil")
	}

	// Save species list to file
	speciesListPath := filepath.Join(targetDir, filepath.Base(url))
	file, err := os.Create(filepath.Clean(speciesListPath)) // #nosec G304 -- internal species list path
	if err != nil {
		return "", err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return "", err
	}

	return speciesListPath, nil
}

// parseSpeciesList parses the Ensembl species list file.
func (d *EnsemblDownloader) parseSpeciesList(filePath string) ([]SpeciesInfo, error) {
	file, err := os.Open(filepath.Clean(filePath)) // #nosec G304 -- internal species list path
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var species []SpeciesInfo
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()

		// Skip comments and empty lines
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}

		// Replace empty values with "NA" to handle parsing
		line = d.normalizeSpeciesLine(line)

		// Parse tab-separated values
		fields := strings.Split(line, "\t")
		if len(fields) < 14 {
			continue // Skip malformed lines
		}

		speciesInfo := SpeciesInfo{
			Name:        fields[0],
			SpeciesName: fields[1],
			TaxonID:     fields[3],
			Assembly:    fields[4],
			Source:      fields[5],
			CoreDB:      fields[13],
		}

		// Determine collection structure
		coreDBClean := regexp.MustCompile(`_core_\d+_\d+_\d+`).ReplaceAllString(speciesInfo.CoreDB, "")

		// Handle collection logic (simplified from bash script)
		if !strings.HasPrefix(coreDBClean, "bacteria") &&
			!strings.HasPrefix(coreDBClean, "fungi") &&
			!strings.HasPrefix(coreDBClean, "metazoa") &&
			!strings.HasPrefix(coreDBClean, "plants") &&
			!strings.HasPrefix(coreDBClean, "protists") {
			speciesInfo.Collection = coreDBClean
		} else {
			speciesInfo.Collection = fmt.Sprintf("%s/%s", coreDBClean, speciesInfo.SpeciesName)
		}

		species = append(species, speciesInfo)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return species, nil
}

// normalizeSpeciesLine handles empty fields in the species list.
func (d *EnsemblDownloader) normalizeSpeciesLine(line string) string {
	// Replace empty tabs with NA (similar to sed command in bash script)
	line = regexp.MustCompile(`^\t`).ReplaceAllString(line, "NA\t")
	line = regexp.MustCompile(`\t$`).ReplaceAllString(line, "\tNA")
	line = regexp.MustCompile(`\t\t`).ReplaceAllString(line, "\tNA\t")
	return line
}

// generateDownloadURLs creates download URLs for all species.
func (d *EnsemblDownloader) generateDownloadURLs(req *EnsemblRequest, species []SpeciesInfo) ([]string, error) {
	var urls []string

	for _, s := range species {
		url, err := d.buildDownloadURL(req, s)
		if err != nil {
			continue // Skip species with URL generation errors
		}
		urls = append(urls, url)
	}

	return urls, nil
}

// buildDownloadURL constructs the download URL for a specific species.
func (d *EnsemblDownloader) buildDownloadURL(req *EnsemblRequest, species SpeciesInfo) (string, error) {
	// Capitalize first letter of species name
	speciesCapital := strings.Title(species.SpeciesName)

	// Clean up assembly name (based on bash script logic)
	assembly := d.cleanAssemblyName(species.Assembly, species.Source)

	// Handle special cases for fungi (simplified from bash script)
	if req.Database == DatabaseFungi {
		assembly = d.handleFungiAssembly(species, assembly)
	}

	var fileExtension string
	var contentPath string

	switch req.Content {
	case ContentPeptides:
		fileExtension = "pep.all.fa.gz"
		contentPath = "pep"
	case ContentCDS:
		fileExtension = "cds.all.fa.gz"
		contentPath = "cds"
	case ContentGFF3:
		fileExtension = fmt.Sprintf("%s.gff3.gz", req.Version)
		contentPath = ""
	case ContentDNA:
		fileExtension = "dna.toplevel.fa.gz"
		contentPath = "dna"
	default:
		return "", fmt.Errorf("unsupported content type: %s", req.Content)
	}

	var url string
	if req.Content == ContentGFF3 {
		// GFF3 files have a different URL structure
		url = fmt.Sprintf("%s/%s/release-%s/gff3/%s/%s.%s.%s",
			d.ftpBaseURL, req.Database, req.Version, species.Collection,
			speciesCapital, assembly, fileExtension)
	} else {
		url = fmt.Sprintf("%s/%s/release-%s/fasta/%s/%s/%s.%s.%s",
			d.ftpBaseURL, req.Database, req.Version, species.Collection,
			contentPath, speciesCapital, assembly, fileExtension)
	}

	return url, nil
}

// cleanAssemblyName cleans up assembly names based on source and content.
func (d *EnsemblDownloader) cleanAssemblyName(assembly, source string) string {
	// Handle special cases from bash script
	if strings.HasPrefix(source, "ENA") {
		return strings.ReplaceAll(assembly, " ", "")
	}

	if assembly == "P.sojae V3.0" {
		return "P_sojae_V3_0"
	}

	if assembly == "svevo" {
		return "Svevo.v1"
	}

	// Normal case: spaces to underscores, remove slashes and commas
	cleaned := strings.ReplaceAll(assembly, " ", "_")
	cleaned = strings.ReplaceAll(cleaned, "/", "_")
	cleaned = strings.ReplaceAll(cleaned, ",", "")

	return cleaned
}

// handleFungiAssembly handles special assembly naming for fungi database.
func (d *EnsemblDownloader) handleFungiAssembly(species SpeciesInfo, assembly string) string {
	// Simplified version of the complex fungi logic from bash script
	// This would need to be expanded for full compatibility

	if strings.HasPrefix(species.Name, "GCA_") || strings.Contains(assembly, "ASM") {
		return assembly
	}

	if strings.Contains(species.Source, "Broad") {
		return species.Assembly
	}

	return assembly
}

// downloadFileWithProgress downloads a file with progress tracking.
// TODO(cache): integrate local cache — Ensembl uses a custom ProtocolClient that
// supports both HTTP and FTP. Cache integration requires wrapping protoClient.Get
// with a cache-check/put layer similar to common.Fetch, computing sha256 inline.
func (d *EnsemblDownloader) downloadFileWithProgress(ctx context.Context, url, targetPath, filename string, size int64, tracker *common.ProgressTracker) (*downloaders.FileInfo, error) {
	resp, err := d.protoClient.Get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to get file: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("received nil response")
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Status %d when downloading file", resp.StatusCode)
	}

	if resp.Body == nil {
		return nil, fmt.Errorf("response body is nil")
	}

	// Get content length if size wasn't provided
	if size <= 0 && resp.Size > 0 {
		size = resp.Size
	}

	// Create target file
	file, err := os.Create(filepath.Clean(targetPath)) // #nosec G304 -- caller-controlled target path
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	downloadTime := time.Now()

	var copiedSize int64

	// Use progress reader if tracker is available and size is known
	if tracker != nil && size > 0 {
		progressReader := common.NewProgressReader(resp.Body, size, filename, tracker, d.verbose)
		defer progressReader.Close()
		copiedSize, err = io.Copy(file, progressReader)
	} else {
		// Fallback to simple copy
		copiedSize, err = io.Copy(file, resp.Body)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to copy data: %w", err)
	}

	// Calculate checksum
	checksum, err := common.CalculateFileChecksum(targetPath)
	if err != nil {
		// Don't fail the download for checksum calculation errors
		checksum = ""
	}

	return &downloaders.FileInfo{
		Path:         targetPath,
		OriginalName: filename,
		Size:         copiedSize,
		Checksum:     checksum,
		ChecksumType: "sha256",
		DownloadTime: downloadTime,
		SourceURL:    url,
		ContentType:  getHeaderValue(resp.Header, "Content-Type"),
	}, nil
}

// estimateFileSize estimates the size of a file for a given species and content type.
func (d *EnsemblDownloader) estimateFileSize(req *EnsemblRequest, species SpeciesInfo) int64 {
	// Size estimates per species based on content type and database
	switch req.Content {
	case ContentPeptides:
		return d.getPeptideSizeEstimate(req.Database)
	case ContentCDS:
		return d.getCDSSizeEstimate(req.Database)
	case ContentGFF3:
		return d.getGFF3SizeEstimate(req.Database)
	case ContentDNA:
		return d.getDNASizeEstimate(req.Database)
	default:
		return 50 * 1024 * 1024 // 50MB default
	}
}

// Size estimation helpers
func (d *EnsemblDownloader) getPeptideSizeEstimate(database DatabaseType) int64 {
	estimates := map[DatabaseType]int64{
		DatabaseBacteria: 5 * 1024 * 1024,  // 5MB
		DatabaseFungi:    20 * 1024 * 1024, // 20MB
		DatabaseMetazoa:  50 * 1024 * 1024, // 50MB
		DatabasePlants:   30 * 1024 * 1024, // 30MB
		DatabaseProtists: 15 * 1024 * 1024, // 15MB
	}
	if size, exists := estimates[database]; exists {
		return size
	}
	return 20 * 1024 * 1024 // Default 20MB
}

func (d *EnsemblDownloader) getCDSSizeEstimate(database DatabaseType) int64 {
	estimates := map[DatabaseType]int64{
		DatabaseBacteria: 8 * 1024 * 1024,  // 8MB
		DatabaseFungi:    30 * 1024 * 1024, // 30MB
		DatabaseMetazoa:  80 * 1024 * 1024, // 80MB
		DatabasePlants:   50 * 1024 * 1024, // 50MB
		DatabaseProtists: 25 * 1024 * 1024, // 25MB
	}
	if size, exists := estimates[database]; exists {
		return size
	}
	return 30 * 1024 * 1024 // Default 30MB
}

func (d *EnsemblDownloader) getGFF3SizeEstimate(database DatabaseType) int64 {
	estimates := map[DatabaseType]int64{
		DatabaseBacteria: 10 * 1024 * 1024,  // 10MB
		DatabaseFungi:    40 * 1024 * 1024,  // 40MB
		DatabaseMetazoa:  100 * 1024 * 1024, // 100MB
		DatabasePlants:   60 * 1024 * 1024,  // 60MB
		DatabaseProtists: 30 * 1024 * 1024,  // 30MB
	}
	if size, exists := estimates[database]; exists {
		return size
	}
	return 50 * 1024 * 1024 // Default 50MB
}

func (d *EnsemblDownloader) getDNASizeEstimate(database DatabaseType) int64 {
	estimates := map[DatabaseType]int64{
		DatabaseBacteria: 50 * 1024 * 1024,   // 50MB
		DatabaseFungi:    200 * 1024 * 1024,  // 200MB
		DatabaseMetazoa:  1000 * 1024 * 1024, // 1GB
		DatabasePlants:   500 * 1024 * 1024,  // 500MB
		DatabaseProtists: 100 * 1024 * 1024,  // 100MB
	}
	if size, exists := estimates[database]; exists {
		return size
	}
	return 200 * 1024 * 1024 // Default 200MB
}

// getHeaderValue retrieves a header value from the protocol response headers.
func getHeaderValue(headers map[string][]string, key string) string {
	if values, exists := headers[key]; exists && len(values) > 0 {
		return values[0]
	}
	return ""
}
