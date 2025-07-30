// Package common provides progress-aware I/O utilities for tracking download progress
// in real-time with support for speed calculation and ETA estimation.
package common

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// ProgressReader wraps an io.Reader to track read progress.
type ProgressReader struct {
	lastUpdate   time.Time
	reader       io.Reader
	tracker      *ProgressTracker
	filename     string
	total        int64
	current      int64
	updateFreq   time.Duration
	mu           sync.RWMutex
	showProgress bool
}

// NewProgressReader creates a new progress-tracking reader.
func NewProgressReader(reader io.Reader, total int64, filename string, tracker *ProgressTracker, showProgress bool) *ProgressReader {
	return &ProgressReader{
		reader:       reader,
		total:        total,
		filename:     filename,
		tracker:      tracker,
		lastUpdate:   time.Now(),
		updateFreq:   500 * time.Millisecond, // Update every 500ms
		showProgress: showProgress,
	}
}

// Read implements io.Reader interface with progress tracking.
func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)

	pr.mu.Lock()
	pr.current += int64(n)
	current := pr.current
	total := pr.total
	pr.mu.Unlock()

	// Update progress tracker
	if pr.tracker != nil {
		pr.tracker.UpdateFile(pr.filename, current)
	}

	// Show live progress if enabled
	if pr.showProgress && time.Since(pr.lastUpdate) >= pr.updateFreq {
		pr.showLiveProgress(current, total)
		pr.lastUpdate = time.Now()
	}

	return n, err
}

// showLiveProgress displays a real-time progress bar.
func (pr *ProgressReader) showLiveProgress(current, total int64) {
	if total <= 0 {
		return
	}

	percentage := float64(current) / float64(total) * 100
	if percentage > 100 {
		percentage = 100
	}

	// Create progress bar
	barWidth := 30
	filled := int(percentage / 100 * float64(barWidth))

	if filled > barWidth {
		filled = barWidth
	}

	bar := ""
	for i := 0; i < filled; i++ {
		bar += "█"
	}

	for i := filled; i < barWidth; i++ {
		bar += "░"
	}

	// Calculate speed and ETA
	elapsed := time.Since(pr.lastUpdate)

	var speed float64

	var eta string

	if elapsed.Seconds() > 0 && current > 0 {
		// Calculate speed from tracker if available
		if pr.tracker != nil {
			pr.tracker.mu.RLock()
			if fileProgress, exists := pr.tracker.files[pr.filename]; exists {
				speed = fileProgress.Speed
			}
			pr.tracker.mu.RUnlock()
		}

		if speed > 0 {
			remaining := total - current
			etaSeconds := float64(remaining) / speed
			eta = formatDuration(time.Duration(etaSeconds) * time.Second)
		} else {
			eta = "calculating..."
		}
	} else {
		eta = "calculating..."
	}

	// Format the progress line
	currentStr := FormatBytes(current)
	totalStr := FormatBytes(total)
	speedStr := FormatBytes(int64(speed)) + "/s"

	// Truncate filename if too long
	displayName := pr.filename
	if len(displayName) > 25 {
		displayName = displayName[:22] + "..."
	}

	// Print progress line (overwrites previous line)
	fmt.Printf("\r⬇️  %-25s [%s] %6.1f%% %s/%s %s ETA: %s\033[K",
		displayName,
		bar,
		percentage,
		currentStr,
		totalStr,
		speedStr,
		eta,
	)

	// Add newline when complete
	if percentage >= 100 {
		fmt.Println()
	}
}

// Close completes the progress tracking.
func (pr *ProgressReader) Close() error {
	if pr.showProgress {
		fmt.Println() // Ensure we end on a new line
	}

	if pr.tracker != nil {
		pr.tracker.CompleteFile(pr.filename)
	}

	return nil
}

// GetProgress returns current progress information.
func (pr *ProgressReader) GetProgress() (current, total int64, percentage float64) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	current = pr.current
	total = pr.total

	if total > 0 {
		percentage = float64(current) / float64(total) * 100
	}

	return
}

// formatDuration formats a duration for display.
func formatDuration(d time.Duration) string {
	if d < 0 {
		return "unknown"
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh%dm%ds", hours, minutes, seconds)
	} else if minutes > 0 {
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	} else {
		return fmt.Sprintf("%ds", seconds)
	}
}

// MultiFileProgressDisplay manages progress display for multiple concurrent downloads.
type MultiFileProgressDisplay struct {
	lastUpdate time.Time
	files      map[string]*ProgressReader
	stopChan   chan struct{}
	updateFreq time.Duration
	mu         sync.RWMutex
	isActive   bool
}

// NewMultiFileProgressDisplay creates a new multi-file progress display.
func NewMultiFileProgressDisplay() *MultiFileProgressDisplay {
	return &MultiFileProgressDisplay{
		files:      make(map[string]*ProgressReader),
		updateFreq: time.Second,
		stopChan:   make(chan struct{}),
	}
}

// AddFile adds a file to the progress display.
func (mpd *MultiFileProgressDisplay) AddFile(filename string, reader *ProgressReader) {
	mpd.mu.Lock()
	defer mpd.mu.Unlock()
	mpd.files[filename] = reader
}

// RemoveFile removes a file from the progress display.
func (mpd *MultiFileProgressDisplay) RemoveFile(filename string) {
	mpd.mu.Lock()
	defer mpd.mu.Unlock()
	delete(mpd.files, filename)
}

// Start begins the progress display loop.
func (mpd *MultiFileProgressDisplay) Start() {
	mpd.mu.Lock()
	if mpd.isActive {
		mpd.mu.Unlock()
		return
	}

	mpd.isActive = true
	mpd.mu.Unlock()

	go mpd.displayLoop()
}

// Stop ends the progress display loop.
func (mpd *MultiFileProgressDisplay) Stop() {
	mpd.mu.Lock()
	defer mpd.mu.Unlock()

	if mpd.isActive {
		mpd.isActive = false
		close(mpd.stopChan)
	}
}

// displayLoop runs the progress display update loop.
func (mpd *MultiFileProgressDisplay) displayLoop() {
	ticker := time.NewTicker(mpd.updateFreq)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mpd.updateDisplay()
		case <-mpd.stopChan:
			return
		}
	}
}

// updateDisplay updates the multi-file progress display.
func (mpd *MultiFileProgressDisplay) updateDisplay() {
	mpd.mu.RLock()

	activeFiles := make(map[string]*ProgressReader)
	for k, v := range mpd.files {
		activeFiles[k] = v
	}
	mpd.mu.RUnlock()

	if len(activeFiles) == 0 {
		return
	}

	// Clear previous lines
	fmt.Printf("\033[%dA", len(activeFiles)) // Move cursor up
	fmt.Print("\033[J")                      // Clear from cursor to end

	// Display progress for each file
	for _, reader := range activeFiles {
		current, total, _ := reader.GetProgress()
		reader.showLiveProgress(current, total)
	}
}
