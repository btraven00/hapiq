// Package zenodo provides download functionality for Zenodo datasets
// with progress tracking, file management, and error handling.
package zenodo

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
)

// DownloadManager handles concurrent downloads with progress tracking.
type DownloadManager struct {
	downloader    *ZenodoDownloader
	maxConcurrent int
	progress      *common.ProgressTracker
	verbose       bool
}

// NewDownloadManager creates a new download manager.
func NewDownloadManager(downloader *ZenodoDownloader, maxConcurrent int) *DownloadManager {
	return &DownloadManager{
		downloader:    downloader,
		maxConcurrent: maxConcurrent,
		verbose:       downloader.verbose,
	}
}

// DownloadJob represents a single file download job.
type DownloadJob struct {
	File      ZenodoFile
	OutputDir string
	Options   *downloaders.DownloadOptions
}

// DownloadResult represents the result of a download job.
type DownloadJobResult struct {
	FileInfo *downloaders.FileInfo
	Error    error
	Job      *DownloadJob
}

// DownloadFiles downloads multiple files concurrently with progress tracking.
func (dm *DownloadManager) DownloadFiles(ctx context.Context, jobs []DownloadJob, progressCallback func(int, int, string)) ([]downloaders.FileInfo, []error) {
	if len(jobs) == 0 {
		return nil, nil
	}

	// Initialize progress tracking
	var totalBytes int64
	for _, job := range jobs {
		totalBytes += job.File.Size
	}
	dm.progress = common.NewProgressTracker(len(jobs), totalBytes, nil, dm.verbose)

	// Create channels for job distribution and result collection
	jobChan := make(chan DownloadJob, len(jobs))
	resultChan := make(chan DownloadJobResult, len(jobs))

	// Start worker goroutines
	var wg sync.WaitGroup
	workerCount := dm.maxConcurrent
	if workerCount > len(jobs) {
		workerCount = len(jobs)
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go dm.downloadWorker(ctx, &wg, jobChan, resultChan)
	}

	// Send jobs to workers
	go func() {
		for _, job := range jobs {
			select {
			case jobChan <- job:
			case <-ctx.Done():
				break
			}
		}
		close(jobChan)
	}()

	// Collect results
	var files []downloaders.FileInfo
	var errors []error
	completed := 0

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	for result := range resultChan {
		completed++

		if result.Error != nil {
			errors = append(errors, result.Error)
		} else if result.FileInfo != nil {
			files = append(files, *result.FileInfo)
		}

		// Update progress
		if progressCallback != nil {
			filename := ""
			if result.Job != nil {
				filename = result.Job.File.Key
			}
			progressCallback(completed, len(jobs), filename)
		}
	}

	return files, errors
}

// downloadWorker processes download jobs from the job channel.
func (dm *DownloadManager) downloadWorker(ctx context.Context, wg *sync.WaitGroup, jobChan <-chan DownloadJob, resultChan chan<- DownloadJobResult) {
	defer wg.Done()

	for {
		select {
		case job, ok := <-jobChan:
			if !ok {
				return
			}

			fileInfo, err := dm.downloader.downloadFile(ctx, job.File, job.OutputDir, job.Options)
			resultChan <- DownloadJobResult{
				FileInfo: fileInfo,
				Error:    err,
				Job:      &job,
			}

		case <-ctx.Done():
			return
		}
	}
}

// DownloadWithResume attempts to resume a partial download.
func (d *ZenodoDownloader) DownloadWithResume(ctx context.Context, file ZenodoFile, outputPath string, options *downloaders.DownloadOptions) (*downloaders.FileInfo, error) {
	if file.Links.Self == "" {
		return nil, fmt.Errorf("no download URL available for file %s", file.Key)
	}

	// Check if partial file exists
	var startByte int64 = 0
	if options != nil && options.Resume {
		if info, err := os.Stat(outputPath); err == nil {
			startByte = info.Size()
			if d.verbose {
				_, _ = fmt.Fprintf(os.Stderr, "Resuming download of %s from byte %d\n", file.Key, startByte)
			}
		}
	}

	// Create HTTP request with range header for resume
	req, err := http.NewRequestWithContext(ctx, "GET", file.Links.Self, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "hapiq/1.0")
	if startByte > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startByte))
	}

	// Execute request
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if startByte > 0 {
		if resp.StatusCode != 206 { // Partial Content
			if resp.StatusCode == 200 {
				// Server doesn't support range requests, start over
				startByte = 0
				if d.verbose {
					_, _ = fmt.Fprintf(os.Stderr, "Server doesn't support range requests, restarting download of %s\n", file.Key)
				}
			} else {
				return nil, fmt.Errorf("resume failed with status %d", resp.StatusCode)
			}
		}
	} else if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Create or open output file
	var outFile *os.File
	if startByte > 0 && resp.StatusCode == 206 {
		outFile, err = os.OpenFile(outputPath, os.O_WRONLY|os.O_APPEND, 0644)
	} else {
		// Create directory if needed
		if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory: %w", err)
		}
		outFile, err = os.Create(outputPath)
		startByte = 0
	}

	if err != nil {
		return nil, fmt.Errorf("failed to open output file: %w", err)
	}
	defer outFile.Close()

	// Copy data with progress tracking
	bytesWritten, err := io.Copy(outFile, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	totalSize := startByte + bytesWritten
	contentType := resp.Header.Get("Content-Type")

	return &downloaders.FileInfo{
		Path:         outputPath,
		OriginalName: file.Key,
		SourceURL:    file.Links.Self,
		ContentType:  contentType,
		Size:         totalSize,
		Checksum:     file.Checksum,
		ChecksumType: "md5", // Zenodo typically uses MD5
		DownloadTime: time.Now(),
	}, nil
}

// ValidateDownload verifies the integrity of a downloaded file.
func (d *ZenodoDownloader) ValidateDownload(filePath string, expectedChecksum string, checksumType string) error {
	if expectedChecksum == "" {
		return nil // No checksum to validate against
	}

	actualChecksum, err := common.CalculateFileChecksum(filePath)
	if err != nil {
		return fmt.Errorf("failed to calculate checksum: %w", err)
	}

	if !strings.EqualFold(actualChecksum, expectedChecksum) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}

	return nil
}

// PrepareDownloadDirectory ensures the download directory exists and handles conflicts.
func (d *ZenodoDownloader) PrepareDownloadDirectory(outputDir string, options *downloaders.DownloadOptions) (*downloaders.DirectoryStatus, error) {
	status := &downloaders.DirectoryStatus{
		TargetPath: outputDir,
		Exists:     false,
		HasWitness: false,
	}

	// Check if directory exists
	if info, err := os.Stat(outputDir); err == nil && info.IsDir() {
		status.Exists = true

		// Check for existing witness file
		witnessPath := filepath.Join(outputDir, "hapiq.json")
		if _, err := os.Stat(witnessPath); err == nil {
			status.HasWitness = true
		}

		// Check for conflicts (existing files)
		entries, err := os.ReadDir(outputDir)
		if err != nil {
			return nil, fmt.Errorf("failed to read directory: %w", err)
		}

		for _, entry := range entries {
			if !entry.IsDir() && entry.Name() != "hapiq.json" {
				status.Conflicts = append(status.Conflicts, entry.Name())
			}
		}
	}

	// Get free space information
	// Get free space information using DirectoryChecker
	dc := common.NewDirectoryChecker(filepath.Dir(outputDir))
	if tempStatus, err := dc.CheckAndPrepare(filepath.Base(outputDir)); err == nil {
		status.FreeSpace = tempStatus.FreeSpace
	}

	// Create directory if it doesn't exist
	if !status.Exists {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory: %w", err)
		}
		status.Exists = true
	}

	return status, nil
}

// HandleDownloadConflicts resolves conflicts in the download directory.
func (d *ZenodoDownloader) HandleDownloadConflicts(status *downloaders.DirectoryStatus, options *downloaders.DownloadOptions) (downloaders.Action, error) {
	if len(status.Conflicts) == 0 {
		return downloaders.ActionProceed, nil
	}

	if options != nil {
		if options.NonInteractive {
			if options.SkipExisting {
				return downloaders.ActionSkip, nil
			}
			return downloaders.ActionOverwrite, nil
		}
	}

	// Interactive conflict resolution would go here
	// For now, default to proceed with overwrite
	return downloaders.ActionOverwrite, nil
}

// EstimateDownloadTime provides an estimate of download time based on file sizes and connection speed.
func (d *ZenodoDownloader) EstimateDownloadTime(files []ZenodoFile, avgSpeedBytesPerSec float64) time.Duration {
	if avgSpeedBytesPerSec <= 0 {
		avgSpeedBytesPerSec = 1024 * 1024 // Assume 1 MB/s default
	}

	var totalBytes int64
	for _, file := range files {
		totalBytes += file.Size
	}

	estimatedSeconds := float64(totalBytes) / avgSpeedBytesPerSec
	return time.Duration(estimatedSeconds) * time.Second
}

// CleanupPartialDownloads removes incomplete download files.
func (d *ZenodoDownloader) CleanupPartialDownloads(outputDir string) error {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		if strings.HasSuffix(filename, ".partial") || strings.HasSuffix(filename, ".tmp") {
			filePath := filepath.Join(outputDir, filename)
			if err := os.Remove(filePath); err != nil {
				if d.verbose {
					_, _ = fmt.Fprintf(os.Stderr, "Warning: failed to remove partial file %s: %v\n", filename, err)
				}
			} else if d.verbose {
				_, _ = fmt.Fprintf(os.Stderr, "Removed partial file: %s\n", filename)
			}
		}
	}

	return nil
}

// GetDownloadURL resolves the direct download URL for a Zenodo file.
func (d *ZenodoDownloader) GetDownloadURL(ctx context.Context, recordID string, fileID string) (string, error) {
	// For most cases, the download URL is available directly in the file metadata
	// This method is useful for cases where we need to resolve URLs dynamically
	record, err := d.getRecordMetadata(ctx, recordID)
	if err != nil {
		return "", fmt.Errorf("failed to get record metadata: %w", err)
	}

	for _, file := range record.Files {
		if file.ID == fileID || file.Key == fileID {
			if file.Links.Self != "" {
				return file.Links.Self, nil
			}
		}
	}

	return "", fmt.Errorf("file not found or no download URL available")
}

// CreateDownloadManifest creates a manifest file listing all downloaded files.
func (d *ZenodoDownloader) CreateDownloadManifest(outputDir string, files []downloaders.FileInfo) error {
	manifestPath := filepath.Join(outputDir, "download_manifest.txt")

	file, err := os.Create(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to create manifest file: %w", err)
	}
	defer file.Close()

	_, err = fmt.Fprintf(file, "# Zenodo Download Manifest\n")
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(file, "# Generated on: %s\n", time.Now().Format(time.RFC3339))
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(file, "# Total files: %d\n\n", len(files))
	if err != nil {
		return err
	}

	for _, fileInfo := range files {
		_, err = fmt.Fprintf(file, "%s\t%d\t%s\t%s\n",
			fileInfo.OriginalName,
			fileInfo.Size,
			fileInfo.Checksum,
			fileInfo.SourceURL)
		if err != nil {
			return err
		}
	}

	return nil
}
