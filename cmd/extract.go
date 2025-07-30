package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/btraven00/hapiq/internal/extractor"
	"github.com/spf13/cobra"

	_ "github.com/btraven00/hapiq/pkg/validators/domains/bio" // Import for side effects (validator registration)
)

var (
	validateLinks   bool
	includeContext  bool
	contextLength   int
	filterDomains   []string
	minConfidence   float64
	outputFormat    string
	batchMode       bool
	maxLinksPerPage int
	numWorkers      int
	showProgress    bool
	keep404s        bool
)

// extractCmd represents the extract command
var extractCmd = &cobra.Command{
	Use:   "extract [file...]",
	Short: "Extract links from PDF documents",
	Long: `Extract links from PDF documents and identify potential dataset references.

This command analyzes PDF files to find URLs, DOIs, and other identifiers that
may point to datasets or supplementary materials. It can validate links for
accessibility and categorize them by type and confidence.

Examples:
  hapiq extract paper.pdf
  hapiq extract --validate-links --output csv paper.pdf
  hapiq extract --batch --min-confidence 0.8 *.pdf
  hapiq extract --filter-domains figshare.com,zenodo.org paper.pdf
  hapiq extract --validate-links --filter-404s paper.pdf`,
	Args: cobra.MinimumNArgs(1),
	RunE: runExtractLinks,
}

func init() {
	rootCmd.AddCommand(extractCmd)

	extractCmd.Flags().BoolVar(&validateLinks, "validate-links", false, "validate extracted links for accessibility")
	extractCmd.Flags().BoolVar(&includeContext, "include-context", true, "include surrounding text context for links")
	extractCmd.Flags().IntVar(&contextLength, "context-length", 100, "length of context to extract around links")
	extractCmd.Flags().StringSliceVar(&filterDomains, "filter-domains", nil, "comma-separated list of domains to filter (e.g., figshare.com,zenodo.org)")
	extractCmd.Flags().Float64Var(&minConfidence, "min-confidence", 0.85, "minimum confidence threshold for including links")
	extractCmd.Flags().StringVar(&outputFormat, "format", "human", "output format (human, json, csv)")
	extractCmd.Flags().BoolVar(&batchMode, "batch", false, "process multiple files and output summary")
	extractCmd.Flags().IntVar(&maxLinksPerPage, "max-links-per-page", 50, "maximum number of links to extract per page")
	extractCmd.Flags().IntVar(&numWorkers, "workers", runtime.NumCPU(), "number of parallel workers for processing")
	extractCmd.Flags().BoolVar(&showProgress, "progress", true, "show progress during batch processing")
	extractCmd.Flags().BoolVar(&keep404s, "keep-404s", false, "keep links that return 404 or are inaccessible (by default they are filtered out)")
}

func runExtractLinks(cmd *cobra.Command, args []string) error {
	// Create extraction options
	options := extractor.ExtractionOptions{
		ValidateLinks:           validateLinks,
		IncludeContext:          includeContext,
		ContextLength:           contextLength,
		FilterDomains:           filterDomains,
		MinConfidence:           minConfidence,
		MaxLinksPerPage:         maxLinksPerPage,
		UseAccessionRecognition: true,
		UseConvertTokenization:  true,
		ExtractPositions:        false,
		Keep404s:                keep404s,
	}

	pdfExtractor := extractor.NewPDFExtractor(options)

	if batchMode {
		return processBatchFilesParallel(args, options)
	}

	for _, filename := range args {
		if err := processSingleFile(pdfExtractor, filename); err != nil {
			return fmt.Errorf("failed to process %s: %w", filename, err)
		}
	}

	return nil
}

func processSingleFile(pdfExtractor *extractor.PDFExtractor, filename string) error {
	if !quiet {
		fmt.Fprintf(os.Stderr, "Processing %s...\n", filename)
	}

	result, err := pdfExtractor.ExtractFromFile(filename)
	if err != nil {
		return err
	}

	return outputResult(result)
}

func outputResult(result *extractor.ExtractionResult) error {
	switch strings.ToLower(outputFormat) {
	case "json":
		return outputJSON(result)
	case "csv":
		return outputCSV(result)
	case "human":
		return outputHuman(result)
	default:
		return fmt.Errorf("unsupported output format: %s", outputFormat)
	}
}

func outputHuman(result *extractor.ExtractionResult) error {
	fmt.Printf("📄 File: %s\n", result.Filename)
	fmt.Printf("📊 Pages: %d | Text: %d chars | Processing time: %v\n",
		result.Pages, result.TotalText, result.ProcessTime)
	// Count links above confidence threshold
	highConfidenceLinks := 0
	for _, link := range result.Links {
		if link.Confidence >= minConfidence {
			highConfidenceLinks++
		}
	}

	fmt.Printf("🔗 Found %d links total (%d above %.0f%% confidence)\n",
		result.Summary.TotalLinks, highConfidenceLinks, minConfidence*100)

	if len(result.Errors) > 0 {
		fmt.Printf("\n❌ Errors:\n")
		for _, err := range result.Errors {
			fmt.Printf("   • %s\n", err)
		}
	}

	if len(result.Warnings) > 0 && !quiet {
		fmt.Printf("\n⚠️  Warnings:\n")
		for _, warning := range result.Warnings {
			fmt.Printf("   • %s\n", warning)
		}
	}

	// Filter links by confidence threshold and group by type
	linksByType := make(map[extractor.LinkType][]extractor.ExtractedLink)
	for _, link := range result.Links {
		if link.Confidence >= minConfidence {
			linksByType[link.Type] = append(linksByType[link.Type], link)
		}
	}

	// Display links by type
	typeOrder := []extractor.LinkType{
		extractor.LinkTypeDOI,
		extractor.LinkTypeGeoID,
		extractor.LinkTypeFigshare,
		extractor.LinkTypeZenodo,
		extractor.LinkTypeURL,
		extractor.LinkTypeGeneric,
	}

	typeEmoji := map[extractor.LinkType]string{
		extractor.LinkTypeDOI:      "📚",
		extractor.LinkTypeGeoID:    "🧬",
		extractor.LinkTypeFigshare: "📊",
		extractor.LinkTypeZenodo:   "🗄️",
		extractor.LinkTypeURL:      "🌐",
		extractor.LinkTypeGeneric:  "🔗",
	}

	for _, linkType := range typeOrder {
		links := linksByType[linkType]
		if len(links) == 0 {
			continue
		}

		emoji := typeEmoji[linkType]
		if emoji == "" {
			emoji = "🔗"
		}

		fmt.Printf("\n%s %s Links (%d):\n", emoji, strings.ToUpper(string(linkType)), len(links))

		for i, link := range links {
			// URLs from extractor are already cleaned, no need for additional cleanup
			cleanedURL := link.URL
			if cleanedURL == "" {
				continue
			}

			if i >= 10 && quiet {
				fmt.Printf("   ... and %d more (use without --quiet to show all)\n", len(links)-10)
				break
			}

			confidence := fmt.Sprintf("%.1f%%", link.Confidence*100)
			pageInfo := fmt.Sprintf("p.%d", link.Page)

			// Add validation status if available
			status := ""
			if link.Validation != nil {
				if link.Validation.IsAccessible {
					status = " ✅"
				} else {
					status = " ❌"
				}
			}

			fmt.Printf("   • %s [%s, %s]%s\n", cleanedURL, confidence, pageInfo, status)
		}
	}

	// Summary statistics
	fmt.Printf("\n📈 Summary:\n")
	for linkType, count := range result.Summary.LinksByType {
		if count > 0 {
			fmt.Printf("   %s: %d\n", linkType, count)
		}
	}

	if result.Summary.ValidatedLinks > 0 {
		accessiblePercent := float64(result.Summary.AccessibleLinks) / float64(result.Summary.ValidatedLinks) * 100
		fmt.Printf("   Validated: %d/%d (%.1f%% accessible)\n",
			result.Summary.AccessibleLinks, result.Summary.ValidatedLinks, accessiblePercent)
	}

	return nil
}

func outputJSON(result *extractor.ExtractionResult) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func outputCSV(result *extractor.ExtractionResult) error {
	writer := csv.NewWriter(os.Stdout)
	defer writer.Flush()

	// Write header
	header := []string{
		"filename", "url", "type", "confidence", "page", "section",
		"context", "validated", "accessible", "status_code", "content_type",
	}
	if err := writer.Write(header); err != nil {
		return err
	}

	// Write data rows
	for _, link := range result.Links {
		row := []string{
			result.Filename,
			link.URL,
			string(link.Type),
			fmt.Sprintf("%.3f", link.Confidence),
			strconv.Itoa(link.Page),
			link.Section,
			link.Context,
		}

		// Add validation information
		if link.Validation != nil {
			row = append(row,
				"true",
				fmt.Sprintf("%t", link.Validation.IsAccessible),
				strconv.Itoa(link.Validation.StatusCode),
				link.Validation.ContentType,
			)
		} else {
			row = append(row, "false", "", "", "")
		}

		if err := writer.Write(row); err != nil {
			return err
		}
	}

	return nil
}

func processBatchFilesParallel(filenames []string, options extractor.ExtractionOptions) error {
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}

	fmt.Printf("🚀 Processing %d files with %d workers...\n", len(filenames), numWorkers)

	// Create worker pool
	pool := extractor.NewWorkerPool(numWorkers)
	pool.Start()

	// Start progress tracking
	var progressTracker *extractor.ProgressTracker
	var progressMu sync.Mutex
	if showProgress {
		progressTracker = extractor.NewProgressTracker()

		// Start progress reporting goroutine
		go func() {
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()

			for range ticker.C {
				progressMu.Lock()
				if progressTracker != nil {
					progressTracker.PrintProgress()
				}
				progressMu.Unlock()
			}
		}()
	}

	// Submit all tasks
	for i, filename := range filenames {
		task := extractor.ExtractionTask{
			ID:       fmt.Sprintf("task-%d", i),
			Filename: filename,
			Options:  options,
		}
		pool.SubmitTask(task)
	}

	// Collect results
	var allResults []*extractor.ExtractionResult
	var totalLinks, totalAccessible, totalValidated int
	var totalProcessTime time.Duration
	var failedFiles []string

	// Process progress updates and results
	go func() {
		for update := range pool.Progress() {
			if showProgress && progressTracker != nil {
				progressMu.Lock()
				progressTracker.Update(update)
				progressMu.Unlock()
			}

			if !quiet && update.Status == extractor.TaskStatusFailed {
				fmt.Fprintf(os.Stderr, "\n❌ Failed to process %s: %s\n", update.Filename, update.Message)
			}
		}
	}()

	// Collect all results
	for i := 0; i < len(filenames); i++ {
		select {
		case result := <-pool.Results():
			if result.Error != nil {
				failedFiles = append(failedFiles, result.Task.Filename)
				if quiet {
					fmt.Fprintf(os.Stderr, "❌ Failed: %s\n", filepath.Base(result.Task.Filename))
				}
				continue
			}

			allResults = append(allResults, result.Result)
			totalLinks += result.Result.Summary.TotalLinks
			totalAccessible += result.Result.Summary.AccessibleLinks
			totalValidated += result.Result.Summary.ValidatedLinks
			totalProcessTime += result.Result.ProcessTime
		}
	}

	// Shutdown worker pool
	pool.Wait()

	// Stop progress tracking
	if showProgress {
		progressMu.Lock()
		if progressTracker != nil {
			progressTracker.PrintProgress()
			fmt.Println() // New line after final progress
		}
		progressTracker = nil
		progressMu.Unlock()
	}

	// Show completion summary
	fmt.Printf("✅ Completed processing %d files", len(filenames))
	if len(failedFiles) > 0 {
		fmt.Printf(" (%d failed)", len(failedFiles))
	}
	fmt.Printf(" in %v\n", totalProcessTime)

	if len(failedFiles) > 0 && !quiet {
		fmt.Printf("Failed files: %v\n", failedFiles)
	}

	// Output batch summary
	return outputBatchResults(allResults, totalLinks, totalAccessible, totalValidated, totalProcessTime)
}

func outputBatchResults(results []*extractor.ExtractionResult, totalLinks, totalAccessible, totalValidated int, totalTime time.Duration) error {
	if len(results) == 0 {
		fmt.Println("⚠️  No files were successfully processed")
		return nil
	}
	switch strings.ToLower(outputFormat) {
	case "json":
		return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"summary": map[string]interface{}{
				"files_processed":  len(results),
				"total_links":      totalLinks,
				"total_accessible": totalAccessible,
				"total_validated":  totalValidated,
				"processing_time":  totalTime,
			},
			"results": results,
		})

	case "csv":
		writer := csv.NewWriter(os.Stdout)
		defer writer.Flush()

		// Write header
		header := []string{
			"filename", "url", "type", "confidence", "page", "section",
			"context", "validated", "accessible", "status_code", "content_type",
		}
		if err := writer.Write(header); err != nil {
			return err
		}

		// Write all results
		for _, result := range results {
			for _, link := range result.Links {
				row := []string{
					filepath.Base(result.Filename),
					link.URL,
					string(link.Type),
					fmt.Sprintf("%.3f", link.Confidence),
					strconv.Itoa(link.Page),
					link.Section,
					link.Context,
				}

				if link.Validation != nil {
					row = append(row,
						"true",
						fmt.Sprintf("%t", link.Validation.IsAccessible),
						strconv.Itoa(link.Validation.StatusCode),
						link.Validation.ContentType,
					)
				} else {
					row = append(row, "false", "", "", "")
				}

				if err := writer.Write(row); err != nil {
					return err
				}
			}
		}

	default: // human format
		fmt.Printf("📊 Batch Processing Summary\n")
		fmt.Printf("═══════════════════════════\n")
		fmt.Printf("Files processed: %d\n", len(results))
		fmt.Printf("Total links found: %d\n", totalLinks)
		if totalValidated > 0 {
			accessiblePercent := float64(totalAccessible) / float64(totalValidated) * 100
			fmt.Printf("Links validated: %d (%.1f%% accessible)\n", totalValidated, accessiblePercent)
		}
		fmt.Printf("Total processing time: %v\n", totalTime)
		fmt.Printf("Average per file: %v\n", totalTime/time.Duration(len(results)))

		fmt.Printf("\n📄 Per-file Results:\n")
		for _, result := range results {
			filename := filepath.Base(result.Filename)
			fmt.Printf("• %s: %d links (%d unique) in %v\n",
				filename, result.Summary.TotalLinks, result.Summary.UniqueLinks, result.ProcessTime)

			if len(result.Errors) > 0 {
				fmt.Printf("  ❌ %d errors\n", len(result.Errors))
			}
		}

		// Show top domains if links were found
		if totalLinks > 0 {
			domainCounts := make(map[string]int)
			for _, result := range results {
				for _, link := range result.Links {
					// Extract domain from URL
					if strings.HasPrefix(link.URL, "http") {
						parts := strings.Split(link.URL, "/")
						if len(parts) >= 3 {
							domain := parts[2]
							domainCounts[domain]++
						}
					}
				}
			}

			fmt.Printf("\n🌐 Top Domains:\n")
			// Sort domains by count (simple approach)
			type domainCount struct {
				domain string
				count  int
			}
			var domains []domainCount
			for domain, count := range domainCounts {
				domains = append(domains, domainCount{domain, count})
			}

			// Simple bubble sort for demo (would use sort.Slice in production)
			for i := 0; i < len(domains)-1; i++ {
				for j := 0; j < len(domains)-i-1; j++ {
					if domains[j].count < domains[j+1].count {
						domains[j], domains[j+1] = domains[j+1], domains[j]
					}
				}
			}

			// Show top 10
			maxShow := 10
			if len(domains) < maxShow {
				maxShow = len(domains)
			}
			for i := 0; i < maxShow; i++ {
				fmt.Printf("• %s: %d links\n", domains[i].domain, domains[i].count)
			}
		}
	}

	return nil
}
