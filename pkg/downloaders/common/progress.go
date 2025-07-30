// Package common provides progress tracking utilities for download operations
// with support for concurrent downloads and real-time progress reporting.
package common

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

// ProgressTracker manages progress tracking for downloads.
type ProgressTracker struct {
	startTime       time.Time
	lastUpdateTime  time.Time
	files           map[string]*FileProgress
	callback        downloaders.ProgressCallback
	totalBytes      int64
	downloadedBytes int64
	totalFiles      int
	downloadedFiles int
	failedFiles     int
	skippedFiles    int
	mu              sync.RWMutex
	verbose         bool
}

// FileProgress tracks progress for individual files.
type FileProgress struct {
	StartTime      time.Time
	LastUpdateTime time.Time
	Error          error
	Filename       string
	Size           int64
	Downloaded     int64
	Speed          float64
	Status         FileStatus
}

// FileStatus represents the status of a file download.
type FileStatus int

const (
	StatusPending FileStatus = iota
	StatusDownloading
	StatusCompleted
	StatusFailed
	StatusSkipped
)

func (s FileStatus) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusDownloading:
		return "downloading"
	case StatusCompleted:
		return "completed"
	case StatusFailed:
		return "failed"
	case StatusSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

// NewProgressTracker creates a new progress tracker.
func NewProgressTracker(totalFiles int, totalBytes int64, callback downloaders.ProgressCallback, verbose bool) *ProgressTracker {
	return &ProgressTracker{
		totalBytes:     totalBytes,
		totalFiles:     totalFiles,
		startTime:      time.Now(),
		lastUpdateTime: time.Now(),
		files:          make(map[string]*FileProgress),
		callback:       callback,
		verbose:        verbose,
	}
}

// StartFile registers a new file for progress tracking.
func (pt *ProgressTracker) StartFile(filename string, size int64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	pt.files[filename] = &FileProgress{
		Filename:       filename,
		Size:           size,
		Downloaded:     0,
		StartTime:      time.Now(),
		LastUpdateTime: time.Now(),
		Status:         StatusDownloading,
	}

	if pt.verbose {
		fmt.Printf("📁 Starting download: %s (%s)\n", filename, FormatBytes(size))
	}
}

// UpdateFile updates progress for a specific file.
func (pt *ProgressTracker) UpdateFile(filename string, downloaded int64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	fileProgress, exists := pt.files[filename]
	if !exists {
		return
	}

	now := time.Now()
	timeDiff := now.Sub(fileProgress.LastUpdateTime).Seconds()

	// Update file progress
	bytesDiff := downloaded - fileProgress.Downloaded
	fileProgress.Downloaded = downloaded
	fileProgress.LastUpdateTime = now

	// Calculate speed (bytes per second)
	if timeDiff > 0 {
		fileProgress.Speed = float64(bytesDiff) / timeDiff
	}

	// Update total progress
	pt.downloadedBytes += bytesDiff
	pt.lastUpdateTime = now

	// Call progress callback if provided
	if pt.callback != nil {
		pt.callback(pt.downloadedBytes, pt.totalBytes, filename)
	}

	// Print progress in verbose mode
	if pt.verbose && timeDiff > 1.0 { // Update every second in verbose mode
		pt.printProgress()
	}
}

// CompleteFile marks a file as completed.
func (pt *ProgressTracker) CompleteFile(filename string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if fileProgress, exists := pt.files[filename]; exists {
		fileProgress.Status = StatusCompleted
		pt.downloadedFiles++

		if pt.verbose {
			duration := time.Since(fileProgress.StartTime)
			avgSpeed := float64(fileProgress.Size) / duration.Seconds()
			fmt.Printf("✅ Completed: %s (%s in %v, avg: %s/s)\n",
				filename,
				FormatBytes(fileProgress.Size),
				duration.Round(time.Second),
				FormatBytes(int64(avgSpeed)))
		}
	}
}

// FailFile marks a file as failed.
func (pt *ProgressTracker) FailFile(filename string, err error) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if fileProgress, exists := pt.files[filename]; exists {
		fileProgress.Status = StatusFailed
		fileProgress.Error = err
		pt.failedFiles++

		if pt.verbose {
			fmt.Printf("❌ Failed: %s - %v\n", filename, err)
		}
	}
}

// SkipFile marks a file as skipped.
func (pt *ProgressTracker) SkipFile(filename, reason string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if fileProgress, exists := pt.files[filename]; exists {
		fileProgress.Status = StatusSkipped
		pt.skippedFiles++

		if pt.verbose {
			fmt.Printf("⏭️  Skipped: %s - %s\n", filename, reason)
		}
	}
}

// GetStats returns current download statistics.
func (pt *ProgressTracker) GetStats() *downloaders.DownloadStats {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	duration := time.Since(pt.startTime)

	var averageSpeed float64

	if duration.Seconds() > 0 {
		averageSpeed = float64(pt.downloadedBytes) / duration.Seconds()
	}

	return &downloaders.DownloadStats{
		Duration:        duration,
		BytesTotal:      pt.totalBytes,
		BytesDownloaded: pt.downloadedBytes,
		FilesTotal:      pt.totalFiles,
		FilesDownloaded: pt.downloadedFiles,
		FilesSkipped:    pt.skippedFiles,
		FilesFailed:     pt.failedFiles,
		AverageSpeed:    averageSpeed,
		MaxConcurrent:   1, // This would be set by the caller
		ResumedDownload: false,
	}
}

// PrintSummary prints a final summary of the download operation.
func (pt *ProgressTracker) PrintSummary() {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	stats := pt.GetStats()

	fmt.Printf("\n📊 Download Summary:\n")
	fmt.Printf("   Duration: %v\n", stats.Duration.Round(time.Second))
	fmt.Printf("   Files: %d downloaded, %d failed, %d skipped (total: %d)\n",
		stats.FilesDownloaded, stats.FilesFailed, stats.FilesSkipped, stats.FilesTotal)
	fmt.Printf("   Data: %s / %s (%.1f%%)\n",
		FormatBytes(stats.BytesDownloaded),
		FormatBytes(stats.BytesTotal),
		float64(stats.BytesDownloaded)/float64(stats.BytesTotal)*100)

	if stats.AverageSpeed > 0 {
		fmt.Printf("   Average speed: %s/s\n", FormatBytes(int64(stats.AverageSpeed)))
	}

	// Show failed files if any
	if stats.FilesFailed > 0 {
		fmt.Printf("\n❌ Failed files:\n")

		for filename, fileProgress := range pt.files {
			if fileProgress.Status == StatusFailed {
				fmt.Printf("   %s: %v\n", filename, fileProgress.Error)
			}
		}
	}
}

// printProgress prints current progress (internal method).
func (pt *ProgressTracker) printProgress() {
	var percentage float64
	if pt.totalBytes > 0 {
		percentage = float64(pt.downloadedBytes) / float64(pt.totalBytes) * 100
	}

	duration := time.Since(pt.startTime)

	var speed float64

	if duration.Seconds() > 0 {
		speed = float64(pt.downloadedBytes) / duration.Seconds()
	}

	var eta string

	if speed > 0 && pt.totalBytes > pt.downloadedBytes {
		remainingBytes := pt.totalBytes - pt.downloadedBytes
		eta = fmt.Sprintf(" (ETA: %s)", EstimateDownloadTime(remainingBytes, speed))
	}

	fmt.Printf("📈 Progress: %.1f%% (%s/%s) at %s/s%s\n",
		percentage,
		FormatBytes(pt.downloadedBytes),
		FormatBytes(pt.totalBytes),
		FormatBytes(int64(speed)),
		eta)
}

// ProgressBar creates a simple text progress bar.
func ProgressBar(current, total int64, width int) string {
	if total == 0 {
		return strings.Repeat("?", width)
	}

	percentage := float64(current) / float64(total)
	filled := int(percentage * float64(width))

	if filled > width {
		filled = width
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)

	return fmt.Sprintf("[%s] %.1f%%", bar, percentage*100)
}

// FileProgressBar creates a progress bar for individual files.
func (fp *FileProgress) ProgressBar(width int) string {
	return ProgressBar(fp.Downloaded, fp.Size, width)
}

// GetCurrentSpeed returns the current download speed for a file.
func (fp *FileProgress) GetCurrentSpeed() float64 {
	return fp.Speed
}

// GetETA estimates time remaining for file download.
func (fp *FileProgress) GetETA() time.Duration {
	if fp.Speed <= 0 || fp.Downloaded >= fp.Size {
		return 0
	}

	remainingBytes := fp.Size - fp.Downloaded
	remainingSeconds := float64(remainingBytes) / fp.Speed

	return time.Duration(remainingSeconds) * time.Second
}

// GetPercentage returns download percentage for a file.
func (fp *FileProgress) GetPercentage() float64 {
	if fp.Size == 0 {
		return 0
	}

	return float64(fp.Downloaded) / float64(fp.Size) * 100
}
