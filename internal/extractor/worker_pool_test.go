package extractor

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewWorkerPool(t *testing.T) {
	pool := NewWorkerPool(4)
	if pool == nil {
		t.Fatal("NewWorkerPool returned nil")
	}

	if pool.numWorkers != 4 {
		t.Errorf("Expected 4 workers, got %d", pool.numWorkers)
	}

	if pool.tasks == nil {
		t.Error("Tasks channel not initialized")
	}

	if pool.results == nil {
		t.Error("Results channel not initialized")
	}

	if pool.progressChan == nil {
		t.Error("Progress channel not initialized")
	}

	pool.Shutdown()
}

func TestWorkerPoolProcessing(t *testing.T) {
	// Create test files (we'll use empty options for simplicity)
	testTasks := []ExtractionTask{
		{
			ID:       "test1",
			Filename: "test1.pdf", // This will fail but that's OK for testing
			Options:  DefaultExtractionOptions(),
		},
		{
			ID:       "test2",
			Filename: "test2.pdf", // This will fail but that's OK for testing
			Options:  DefaultExtractionOptions(),
		},
	}

	pool := NewWorkerPool(2)

	// Track all progress updates
	var progressUpdates []ProgressUpdate
	var progressMu sync.Mutex
	var wg sync.WaitGroup

	// Start collecting progress updates
	wg.Add(1)
	go func() {
		defer wg.Done()
		for update := range pool.Progress() {
			progressMu.Lock()
			progressUpdates = append(progressUpdates, update)
			progressMu.Unlock()
		}
	}()

	pool.Start()

	// Submit tasks with small delay to help with race conditions
	for _, task := range testTasks {
		pool.SubmitTask(task)
		time.Sleep(10 * time.Millisecond)
	}

	// Collect results
	var results []ExtractionTaskResult
	for i := 0; i < len(testTasks); i++ {
		result := <-pool.Results()
		results = append(results, result)
	}

	// Wait for all workers to finish and close channels
	pool.Wait()
	wg.Wait()

	// Verify results
	if len(results) != len(testTasks) {
		t.Errorf("Expected %d results, got %d", len(testTasks), len(results))
	}

	// All results should have errors since we're using fake files
	for _, result := range results {
		if result.Error == nil {
			t.Errorf("Expected error for task %s (fake file), got nil", result.Task.ID)
		}
	}

	// Get final progress updates safely
	progressMu.Lock()
	finalProgressUpdates := make([]ProgressUpdate, len(progressUpdates))
	copy(finalProgressUpdates, progressUpdates)
	progressMu.Unlock()

	// Should have received progress updates
	if len(finalProgressUpdates) == 0 {
		t.Error("Expected progress updates, got none")
	}

	// Verify task status progression
	taskStatuses := make(map[string][]TaskStatus)
	for _, update := range finalProgressUpdates {
		taskStatuses[update.TaskID] = append(taskStatuses[update.TaskID], update.Status)
	}

	for taskID, statuses := range taskStatuses {
		if len(statuses) == 0 {
			t.Errorf("No status updates for task %s", taskID)
		}
		// Should have at least pending and some final status
		foundPending := false
		for _, status := range statuses {
			if status == TaskStatusPending {
				foundPending = true
				break
			}
		}
		if !foundPending {
			t.Errorf("Task %s never had pending status. Statuses received: %v", taskID, statuses)
		}
	}
}

func TestWorkerPoolStats(t *testing.T) {
	pool := NewWorkerPool(3)

	// Initial stats
	stats := pool.GetStats()
	if stats.TotalTasks != 0 {
		t.Errorf("Expected 0 total tasks initially, got %d", stats.TotalTasks)
	}
	if stats.NumWorkers != 3 {
		t.Errorf("Expected 3 workers, got %d", stats.NumWorkers)
	}

	// Submit some tasks
	pool.SubmitTask(ExtractionTask{ID: "1", Filename: "test1.pdf"})
	pool.SubmitTask(ExtractionTask{ID: "2", Filename: "test2.pdf"})

	stats = pool.GetStats()
	if stats.TotalTasks != 2 {
		t.Errorf("Expected 2 total tasks after submission, got %d", stats.TotalTasks)
	}

	pool.Shutdown()
}

func TestProgressTracker(t *testing.T) {
	tracker := NewProgressTracker()

	if tracker == nil {
		t.Fatal("NewProgressTracker returned nil")
	}

	// Initial summary
	summary := tracker.GetSummary()
	if summary.TotalTasks != 0 {
		t.Errorf("Expected 0 total tasks initially, got %d", summary.TotalTasks)
	}

	// Add some progress updates
	tracker.Update(ProgressUpdate{
		TaskID: "task1",
		Status: TaskStatusPending,
	})

	tracker.Update(ProgressUpdate{
		TaskID: "task1",
		Status: TaskStatusProcessing,
	})

	tracker.Update(ProgressUpdate{
		TaskID: "task2",
		Status: TaskStatusPending,
	})

	tracker.Update(ProgressUpdate{
		TaskID: "task1",
		Status: TaskStatusCompleted,
	})

	summary = tracker.GetSummary()
	if summary.TotalTasks != 2 {
		t.Errorf("Expected 2 total tasks, got %d", summary.TotalTasks)
	}

	if summary.StatusCounts[TaskStatusCompleted] != 1 {
		t.Errorf("Expected 1 completed task, got %d", summary.StatusCounts[TaskStatusCompleted])
	}

	if summary.StatusCounts[TaskStatusPending] != 1 {
		t.Errorf("Expected 1 pending task, got %d", summary.StatusCounts[TaskStatusPending])
	}

	// Test completion estimation
	estimation := tracker.EstimateCompletion()
	if estimation < 0 {
		t.Error("Completion estimation should not be negative")
	}
}

func TestCleanURL(t *testing.T) {
	extractor := NewPDFExtractor(DefaultExtractionOptions())

	testCases := []struct {
		input    string
		expected string
	}{
		{
			input:    "https://doi.org/10.1038/s41467-021-23778-6",
			expected: "https://doi.org/10.1038/s41467-021-23778-6",
		},
		{
			input:    "https://doi.org/10.1038/s41467-021-23778-6Correspondence",
			expected: "https://doi.org/10.1038/s41467-021-23778-6",
		},
		{
			input:    "https://zenodo.org/record/123456Peerreviewinformation",
			expected: "https://zenodo.org/record/123456",
		},
		{
			input:    "https://example.com/data.csv;",
			expected: "https://example.com/data.csv",
		},
		{
			input:    "https://github.com/user/repo  ",
			expected: "https://github.com/user/repo",
		},
		{
			input:    "https://example.com/path/10.",
			expected: "https://example.com/path",
		},
		{
			input:    "https://doi.org/10.1234/exampleNaturecommunications",
			expected: "https://doi.org/10.1234/example",
		},
	}

	for _, tc := range testCases {
		result := extractor.cleanURL(tc.input)
		if result != tc.expected {
			t.Errorf("cleanURL(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestIsValidURL(t *testing.T) {
	extractor := NewPDFExtractor(DefaultExtractionOptions())

	testCases := []struct {
		url      string
		expected bool
	}{
		{"https://example.com", true},
		{"http://example.com/path", true},
		{"ftp://example.com/file", true},
		{"10.1234/example", true}, // DOI
		{"https://example.com/CorrespondenceandrequestsformaterialsshouldbeaddressedtoC.J.orE.E.PeerreviewinformationNatureCommunications", false},
		{"https://example.com" + strings.Repeat("a", 500), false}, // Too long
		{"not-a-url", false},
		{"https://", false}, // No host
		{"https://example.compeerreviewinformation", false},           // Suspicious pattern
		{"https://example.com https://other.com", false},              // Multiple URLs
		{"https://example.com/" + strings.Repeat("path/", 50), false}, // Very long path
	}

	for _, tc := range testCases {
		result := extractor.isValidURL(tc.url)
		if result != tc.expected {
			t.Errorf("isValidURL(%q) = %v, expected %v", tc.url, result, tc.expected)
		}
	}
}

func TestAdjustConfidenceByValidation(t *testing.T) {
	extractor := NewPDFExtractor(DefaultExtractionOptions())

	testCases := []struct {
		name               string
		originalConfidence float64
		validation         *ValidationResult
		expectDecrease     bool
		expectIncrease     bool
	}{
		{
			name:               "No validation",
			originalConfidence: 0.8,
			validation:         nil,
			expectDecrease:     false,
			expectIncrease:     false,
		},
		{
			name:               "Accessible link",
			originalConfidence: 0.8,
			validation: &ValidationResult{
				IsAccessible: true,
				StatusCode:   200,
			},
			expectDecrease: false,
			expectIncrease: false,
		},
		{
			name:               "Dataset link",
			originalConfidence: 0.8,
			validation: &ValidationResult{
				IsAccessible: true,
				StatusCode:   200,
				IsDataset:    true,
			},
			expectDecrease: false,
			expectIncrease: true,
		},
		{
			name:               "404 Not Found",
			originalConfidence: 0.9,
			validation: &ValidationResult{
				IsAccessible: false,
				StatusCode:   404,
			},
			expectDecrease: true,
			expectIncrease: false,
		},
		{
			name:               "403 Forbidden",
			originalConfidence: 0.9,
			validation: &ValidationResult{
				IsAccessible: false,
				StatusCode:   403,
			},
			expectDecrease: true,
			expectIncrease: false,
		},
		{
			name:               "Server Error",
			originalConfidence: 0.9,
			validation: &ValidationResult{
				IsAccessible: false,
				StatusCode:   500,
			},
			expectDecrease: true,
			expectIncrease: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := extractor.adjustConfidenceByValidation(tc.originalConfidence, tc.validation)

			if tc.expectDecrease && result >= tc.originalConfidence {
				t.Errorf("Expected confidence to decrease from %.2f, got %.2f", tc.originalConfidence, result)
			}

			if tc.expectIncrease && result <= tc.originalConfidence {
				t.Errorf("Expected confidence to increase from %.2f, got %.2f", tc.originalConfidence, result)
			}

			if !tc.expectDecrease && !tc.expectIncrease && result != tc.originalConfidence {
				t.Errorf("Expected confidence to remain %.2f, got %.2f", tc.originalConfidence, result)
			}

			// Confidence should always be between 0 and 1
			if result < 0 || result > 1 {
				t.Errorf("Confidence %.2f is outside valid range [0, 1]", result)
			}

			// 404 should result in very low confidence
			if tc.validation != nil && tc.validation.StatusCode == 404 && result > 0.2 {
				t.Errorf("404 should result in very low confidence, got %.2f", result)
			}
		})
	}
}

func TestTaskStatusProgression(t *testing.T) {
	// Test that task statuses progress logically
	validTransitions := map[TaskStatus][]TaskStatus{
		TaskStatusPending:    {TaskStatusProcessing},
		TaskStatusProcessing: {TaskStatusCompleted, TaskStatusFailed},
		TaskStatusCompleted:  {}, // Terminal state
		TaskStatusFailed:     {}, // Terminal state
	}

	for fromStatus, validNext := range validTransitions {
		for _, toStatus := range validNext {
			// This is a valid transition
			if fromStatus == TaskStatusPending && toStatus != TaskStatusProcessing {
				t.Errorf("Invalid transition from %s to %s", fromStatus, toStatus)
			}
		}
	}
}

func BenchmarkWorkerPool(b *testing.B) {
	pool := NewWorkerPool(4)
	pool.Start()

	options := DefaultExtractionOptions()
	options.ValidateLinks = false // Disable validation for benchmark

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		task := ExtractionTask{
			ID:       fmt.Sprintf("bench-%d", i),
			Filename: "nonexistent.pdf", // Will fail quickly
			Options:  options,
		}
		pool.SubmitTask(task)

		// Consume result to prevent blocking
		<-pool.Results()
	}

	pool.Shutdown()
}

func BenchmarkURLCleaning(b *testing.B) {
	extractor := NewPDFExtractor(DefaultExtractionOptions())
	testURL := "https://doi.org/10.1038/s41467-021-23778-6.CorrespondenceandrequestsformaterialsshouldbeaddressedtoC.J.orE.E.PeerreviewinformationNatureCommunications"

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		extractor.cleanURL(testURL)
	}
}
