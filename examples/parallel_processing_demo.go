//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/btraven00/hapiq/internal/extractor"
)

// ValidationResult for demo purposes
type ValidationResult struct {
	IsAccessible bool
	StatusCode   int
	IsDataset    bool
}

func main() {
	fmt.Println("🚀 Parallel Processing Demo")
	fmt.Println("============================")
	fmt.Println("Demonstrating worker pool, progress tracking, and URL cleaning")
	fmt.Println()

	// Demo 1: Worker Pool Processing
	fmt.Println("📋 Demo 1: Worker Pool with Progress Tracking")
	fmt.Println("----------------------------------------------")

	// Create some mock extraction tasks (will fail but shows the workflow)
	tasks := []extractor.ExtractionTask{
		{ID: "paper1", Filename: "research-paper-1.pdf", Options: extractor.DefaultExtractionOptions()},
		{ID: "paper2", Filename: "research-paper-2.pdf", Options: extractor.DefaultExtractionOptions()},
		{ID: "paper3", Filename: "research-paper-3.pdf", Options: extractor.DefaultExtractionOptions()},
		{ID: "paper4", Filename: "research-paper-4.pdf", Options: extractor.DefaultExtractionOptions()},
		{ID: "paper5", Filename: "research-paper-5.pdf", Options: extractor.DefaultExtractionOptions()},
		{ID: "paper6", Filename: "research-paper-6.pdf", Options: extractor.DefaultExtractionOptions()},
	}

	numWorkers := 3
	fmt.Printf("Creating worker pool with %d workers for %d tasks...\n", numWorkers, len(tasks))

	// Create and start worker pool
	pool := extractor.NewWorkerPool(numWorkers)
	pool.Start()

	// Set up progress tracking
	progressTracker := extractor.NewProgressTracker()

	// Start progress reporting
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			progressTracker.PrintProgress()
		}
	}()

	// Start progress collection
	go func() {
		for update := range pool.Progress() {
			progressTracker.Update(update)
			if update.Status == extractor.TaskStatusProcessing {
				fmt.Printf("\n🔄 Worker started processing %s\n", update.Filename)
			} else if update.Status == extractor.TaskStatusCompleted {
				fmt.Printf("\n✅ Completed %s in %v\n", update.Filename, update.ElapsedTime)
			} else if update.Status == extractor.TaskStatusFailed {
				fmt.Printf("\n❌ Failed %s: %s\n", update.Filename, update.Message)
			}
		}
	}()

	// Submit all tasks
	for _, task := range tasks {
		pool.SubmitTask(task)
	}

	// Collect results
	var results []extractor.ExtractionTaskResult
	for i := 0; i < len(tasks); i++ {
		result := <-pool.Results()
		results = append(results, result)
	}

	// Shutdown pool
	pool.Wait()

	// Final progress
	progressTracker.PrintProgress()
	fmt.Println()

	// Show summary
	fmt.Printf("\n📊 Processing Summary:\n")
	fmt.Printf("  Total tasks: %d\n", len(tasks))

	failed := 0
	for _, result := range results {
		if result.Error != nil {
			failed++
		}
	}
	fmt.Printf("  Completed: %d\n", len(results)-failed)
	fmt.Printf("  Failed: %d\n", failed)

	// Demo 2: URL Cleaning and Validation
	fmt.Println("\n🧹 Demo 2: URL Cleaning (fixing PDF extraction artifacts)")
	fmt.Println("----------------------------------------------------------")

	extractor := extractor.NewPDFExtractor(extractor.DefaultExtractionOptions())

	malformedURLs := []string{
		"https://doi.org/10.1038/s41467-021-23778-6CorrespondenceandrequestsformaterialsshouldbeaddressedtoC.J.",
		"https://zenodo.org/record/123456Peerreviewinformation",
		"https://figshare.com/articles/dataset/my-data/123456Naturecommunications",
		"https://github.com/user/datasetPublishersnote",
		"https://example.com/data.csvReprintsandpermission",
		"https://ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE123456Acknowledgments",
	}

	fmt.Println("Cleaning malformed URLs from PDF text concatenation:")
	for i, malformed := range malformedURLs {
		// This uses internal method for demo - in real usage it happens automatically
		fmt.Printf("\n%d. Original (malformed):\n   %s\n", i+1, malformed)

		// Simulate the cleaning process
		cleaned := cleanURLDemo(malformed)
		fmt.Printf("   Cleaned:\n   %s\n", cleaned)

		// Show validation result
		if isValidURLDemo(cleaned) {
			fmt.Printf("   Status: ✅ Valid URL\n")
		} else {
			fmt.Printf("   Status: ❌ Still invalid\n")
		}
	}

	// Demo 3: Confidence Adjustment
	fmt.Println("\n🎯 Demo 3: Confidence Adjustment Based on HTTP Status")
	fmt.Println("----------------------------------------------------")

	testCases := []struct {
		url        string
		confidence float64
		statusCode int
		accessible bool
		isDataset  bool
	}{
		{"https://zenodo.org/record/123456", 0.9, 200, true, true},
		{"https://broken-link.example.com", 0.8, 404, false, false},
		{"https://forbidden.example.com", 0.8, 403, false, false},
		{"https://server-error.example.com", 0.9, 500, false, false},
		{"https://figshare.com/dataset/456", 0.85, 200, true, true},
	}

	for _, tc := range testCases {
		validation := &ValidationResult{
			IsAccessible: tc.accessible,
			StatusCode:   tc.statusCode,
			IsDataset:    tc.isDataset,
		}

		adjustedConfidence := adjustConfidenceDemo(tc.confidence, validation)

		fmt.Printf("URL: %s\n", tc.url)
		fmt.Printf("  Original confidence: %.1f%%\n", tc.confidence*100)
		fmt.Printf("  HTTP %d (%s) -> Adjusted: %.1f%%\n",
			tc.statusCode,
			map[bool]string{true: "accessible", false: "not accessible"}[tc.accessible],
			adjustedConfidence*100)

		if tc.isDataset && tc.accessible {
			fmt.Printf("  📊 Detected as dataset - confidence boosted\n")
		}
		if tc.statusCode == 404 {
			fmt.Printf("  ⚠️  404 Not Found - confidence severely reduced\n")
		}
		fmt.Println()
	}

	// Demo 4: Performance Comparison
	fmt.Println("⚡ Demo 4: Performance Comparison")
	fmt.Println("--------------------------------")

	fmt.Printf("System info:\n")
	fmt.Printf("  CPU cores: %d\n", runtime.NumCPU())
	fmt.Printf("  GOMAXPROCS: %d\n", runtime.GOMAXPROCS(0))

	// Simulate processing times
	sequentialTime := time.Duration(len(tasks)) * 2 * time.Second // 2s per task
	parallelTime := time.Duration(len(tasks)) * 2 * time.Second / time.Duration(numWorkers)

	fmt.Printf("\nEstimated processing times for %d PDF files:\n", len(tasks))
	fmt.Printf("  Sequential: %v\n", sequentialTime)
	fmt.Printf("  Parallel (%d workers): %v\n", numWorkers, parallelTime)
	fmt.Printf("  Speedup: %.1fx\n", float64(sequentialTime)/float64(parallelTime))

	fmt.Println("\n✅ Parallel Processing Demo Complete!")
	fmt.Println("\nKey features demonstrated:")
	fmt.Println("  • Worker pool with configurable concurrency")
	fmt.Println("  • Real-time progress tracking and reporting")
	fmt.Println("  • URL cleaning to fix PDF text extraction artifacts")
	fmt.Println("  • Confidence adjustment based on HTTP validation")
	fmt.Println("  • Graceful error handling for failed tasks")
	fmt.Println("  • Automatic load balancing across workers")
	fmt.Println("  • Resource-efficient parallel processing")

	fmt.Println("\nUsage in CLI:")
	fmt.Println("  # Process multiple PDFs with 8 workers")
	fmt.Println("  ./hapiq extract --batch --workers 8 --validate-links *.pdf")
	fmt.Println("  ")
	fmt.Println("  # Show progress during processing")
	fmt.Println("  ./hapiq extract --batch --progress --verbose papers/*.pdf")
}

// Demo helper functions (simplified versions of internal methods)

func cleanURLDemo(url string) string {
	// Simplified URL cleaning logic for demo
	stopPatterns := []string{
		"Correspondence", "Peerreview", "Naturecommunications", "Publishersnote",
		"Springernature", "Reprintsandpermission", "Acknowledgments",
	}

	for _, pattern := range stopPatterns {
		if idx := strings.Index(url, pattern); idx != -1 {
			url = url[:idx]
			break
		}
	}

	// Remove trailing punctuation
	return strings.TrimRight(url, ".,:;!?)]}")
}

func isValidURLDemo(url string) bool {
	// Simplified validation for demo
	if len(url) > 500 {
		return false
	}

	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return false
	}

	// Check for suspicious concatenated text
	suspicious := []string{"correspondence", "peerreview", "naturecommunications"}
	urlLower := strings.ToLower(url)
	for _, pattern := range suspicious {
		if strings.Contains(urlLower, pattern) {
			return false
		}
	}

	return true
}

func adjustConfidenceDemo(original float64, validation *ValidationResult) float64 {
	// Simplified confidence adjustment for demo
	if validation == nil {
		return original
	}

	if validation.IsAccessible {
		if validation.IsDataset {
			return min(original*1.1, 1.0)
		}
		return original
	}

	// Adjust based on status code
	switch validation.StatusCode {
	case 404:
		return min(original*0.1, 0.15)
	case 403:
		return min(original*0.6, 0.7)
	default:
		return min(original*0.5, 0.6)
	}
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
