package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
	"github.com/btraven00/hapiq/pkg/downloaders/figshare"
	"github.com/btraven00/hapiq/pkg/downloaders/geo"
)

var (
	outputDir            string
	excludeRaw           bool
	excludeSupplementary bool
	maxConcurrent        int
	resumeDownload       bool
	skipExisting         bool
	nonInteractive       bool
	downloadTimeout      int
	customFilters        map[string]string
)

// downloadCmd represents the download command.
var downloadCmd = &cobra.Command{
	Use:   "download <source> <id>",
	Short: "Download datasets from scientific data repositories",
	Long: `Download datasets from various scientific data repositories with comprehensive
metadata tracking and provenance information.

Supported sources:
  geo       - NCBI Gene Expression Omnibus (GSE, GSM, GPL, GDS)
  figshare  - Figshare articles, collections, and projects

Examples:
  hapiq download geo GSE123456 --out ./datasets
  hapiq download figshare 12345678 --out ./datasets
  hapiq download geo GSE123456 --out ./data --parallel 4
  hapiq download figshare 12345678 --out ./data --exclude-raw --exclude-supplementary --quiet`,
	Args: cobra.ExactArgs(2),
	RunE: runDownload,
}

func runDownload(cmd *cobra.Command, args []string) error {
	sourceType := args[0]
	id := args[1]

	// Validate output directory
	if outputDir == "" {
		return fmt.Errorf("output directory must be specified with --out flag")
	}

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Initialize downloaders registry
	if err := initializeDownloaders(); err != nil {
		return fmt.Errorf("failed to initialize downloaders: %w", err)
	}

	// Create download context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(downloadTimeout)*time.Second)
	defer cancel()

	if !quiet {
		fmt.Printf("Downloading from %s: %s\n", sourceType, id)
		fmt.Printf("Output directory: %s\n", outputDir)
		fmt.Printf("Include raw data: %t\n", !excludeRaw)
		fmt.Printf("Exclude supplementary: %t\n", excludeSupplementary)
		fmt.Printf("Max concurrent: %d\n", maxConcurrent)
		fmt.Printf("Non-interactive: %t\n", nonInteractive)
		fmt.Printf("Timeout: %ds\n", downloadTimeout)
	}

	// Validate source type and ID
	validationResult, err := downloaders.Validate(ctx, sourceType, id)
	if err != nil {
		return fmt.Errorf("failed to validate %s ID '%s': %w", sourceType, id, err)
	}

	if !validationResult.Valid {
		fmt.Printf("❌ Validation failed for %s ID '%s':\n", sourceType, id)

		for _, errMsg := range validationResult.Errors {
			fmt.Printf("   Error: %s\n", errMsg)
		}

		return fmt.Errorf("invalid %s ID: %s", sourceType, id)
	}

	// Show validation warnings
	if len(validationResult.Warnings) > 0 {
		fmt.Printf("⚠️  Validation warnings for %s ID '%s':\n", sourceType, id)

		for _, warning := range validationResult.Warnings {
			fmt.Printf("   Warning: %s\n", warning)
		}
	}

	if !quiet {
		fmt.Printf("✅ ID validation successful\n")
	}

	// Get metadata first
	if !quiet {
		fmt.Printf("🔍 Retrieving metadata...\n")
	}

	metadata, err := downloaders.GetMetadata(ctx, sourceType, validationResult.ID)
	if err != nil {
		return fmt.Errorf("failed to get metadata for %s ID '%s': %w", sourceType, validationResult.ID, err)
	}

	// Display metadata summary
	fmt.Printf("\n📊 Dataset Information:\n")
	fmt.Printf("   Source: %s\n", metadata.Source)
	fmt.Printf("   ID: %s\n", metadata.ID)
	fmt.Printf("   Title: %s\n", metadata.Title)

	if metadata.Description != "" {
		fmt.Printf("   Description: %s\n", truncateString(metadata.Description, 100))
	}

	if len(metadata.Authors) > 0 {
		fmt.Printf("   Authors: %s\n", metadata.Authors[0])

		if len(metadata.Authors) > 1 {
			fmt.Printf("            (+%d more)\n", len(metadata.Authors)-1)
		}
	}

	fmt.Printf("   Files: %d\n", metadata.FileCount)
	fmt.Printf("   Total size: %s\n", common.FormatBytes(metadata.TotalSize))

	if metadata.DOI != "" {
		fmt.Printf("   DOI: %s\n", metadata.DOI)
	}

	if metadata.License != "" {
		fmt.Printf("   License: %s\n", metadata.License)
	}

	// Show collections if present
	if len(metadata.Collections) > 0 {
		fmt.Printf("\n📦 Collections:\n")

		for _, collection := range metadata.Collections {
			fmt.Printf("   %s: %s (%d files, %s)\n",
				collection.Type,
				collection.Title,
				collection.FileCount,
				common.FormatBytes(collection.EstimatedSize))
		}
	}

	// Create download request
	downloadOptions := &downloaders.DownloadOptions{
		IncludeRaw:           !excludeRaw,
		ExcludeSupplementary: excludeSupplementary,
		MaxConcurrent:        maxConcurrent,
		Resume:               resumeDownload,
		SkipExisting:         skipExisting,
		NonInteractive:       nonInteractive,
		CustomFilters:        customFilters,
	}

	downloadRequest := &downloaders.DownloadRequest{
		ID:        validationResult.ID,
		OutputDir: outputDir,
		Options:   downloadOptions,
		Metadata:  metadata,
	}

	// Perform the download
	if !quiet {
		fmt.Printf("\n⬇️  Starting download...\n")
	}

	result, err := downloaders.Download(ctx, sourceType, downloadRequest)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Display results
	fmt.Printf("\n📋 Download Results:\n")

	if result.Success {
		fmt.Printf("   Status: ✅ Success\n")
	} else {
		fmt.Printf("   Status: ❌ Failed\n")
	}

	fmt.Printf("   Duration: %v\n", result.Duration.Round(time.Second))
	fmt.Printf("   Files downloaded: %d\n", len(result.Files))
	fmt.Printf("   Data downloaded: %s\n", common.FormatBytes(result.BytesDownloaded))

	if result.BytesTotal > 0 {
		percentage := float64(result.BytesDownloaded) / float64(result.BytesTotal) * 100
		fmt.Printf("   Completion: %.1f%%\n", percentage)
	}

	// Show download speed
	if result.Duration.Seconds() > 0 {
		speed := float64(result.BytesDownloaded) / result.Duration.Seconds()
		fmt.Printf("   Average speed: %s/s\n", common.FormatBytes(int64(speed)))
	}

	// Show witness file location
	if result.WitnessFile != "" {
		fmt.Printf("   Witness file: %s\n", result.WitnessFile)
	}

	// Show warnings if any
	if len(result.Warnings) > 0 {
		fmt.Printf("\n⚠️  Warnings:\n")

		for _, warning := range result.Warnings {
			fmt.Printf("   %s\n", warning)
		}
	}

	// Show errors if any
	if len(result.Errors) > 0 {
		fmt.Printf("\n❌ Errors:\n")

		for _, error := range result.Errors {
			fmt.Printf("   %s\n", error)
		}
	}

	// Output detailed file list in verbose mode
	if !quiet && len(result.Files) > 0 {
		fmt.Printf("\n📁 Downloaded Files:\n")

		for _, file := range result.Files {
			fmt.Printf("   %s (%s)\n", file.Path, common.FormatBytes(file.Size))

			if file.Checksum != "" {
				fmt.Printf("      %s: %s\n", file.ChecksumType, file.Checksum)
			}
		}
	}

	// Output in JSON format if requested
	if output == "json" {
		encoder := &jsonEncoder{}
		encoder.SetIndent("", "  ")

		if err := encoder.Encode(result); err != nil {
			return fmt.Errorf("failed to encode JSON output: %w", err)
		}
	}

	if !result.Success {
		return fmt.Errorf("download completed with errors")
	}

	fmt.Printf("\n🎉 Download completed successfully!\n")

	return nil
}

// initializeDownloaders registers all available downloaders.
func initializeDownloaders() error {
	// Get NCBI API key from environment variable if available
	apiKey := os.Getenv("NCBI_API_KEY")

	// Register GEO downloader
	geoOptions := []geo.Option{
		geo.WithVerbose(!quiet),
		geo.WithTimeout(time.Duration(downloadTimeout) * time.Second),
	}

	if apiKey != "" {
		if !quiet {
			fmt.Printf("Using NCBI API key for increased rate limits\n")
		}

		geoOptions = append(geoOptions, geo.WithAPIKey(apiKey))
	} else if !quiet {
		fmt.Printf("No NCBI API key found. Set NCBI_API_KEY environment variable for higher rate limits\n")
	}

	geoDownloader := geo.NewGEODownloader(geoOptions...)
	if err := downloaders.Register(geoDownloader); err != nil {
		return fmt.Errorf("failed to register GEO downloader: %w", err)
	}

	// Register Figshare downloader
	figshareDownloader := figshare.NewFigshareDownloader(
		figshare.WithVerbose(!quiet),
		figshare.WithTimeout(time.Duration(downloadTimeout)*time.Second),
	)
	if err := downloaders.Register(figshareDownloader); err != nil {
		return fmt.Errorf("failed to register Figshare downloader: %w", err)
	}

	// Register aliases
	if err := downloaders.RegisterAlias("ncbi", "geo"); err != nil {
		return fmt.Errorf("failed to register NCBI alias: %w", err)
	}

	return nil
}

// truncateString truncates a string to the specified length.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	return s[:maxLen-3] + "..."
}

// jsonEncoder is a simple JSON encoder interface.
type jsonEncoder struct{}

func (e *jsonEncoder) SetIndent(prefix, indent string) {}
func (e *jsonEncoder) Encode(v interface{}) error {
	// This would use the actual JSON encoder from the common package
	// For now, just print a placeholder
	fmt.Println("JSON output would be here")
	return nil
}

func init() {
	rootCmd.AddCommand(downloadCmd)

	// Output directory flag (required)
	downloadCmd.Flags().StringVar(&outputDir, "out", "", "output directory for downloaded files (required)")
	downloadCmd.MarkFlagRequired("out")

	// Download behavior flags
	downloadCmd.Flags().BoolVar(&excludeRaw, "exclude-raw", false, "exclude raw data files (e.g., FASTQ, BAM)")
	downloadCmd.Flags().BoolVar(&excludeSupplementary, "exclude-supplementary", false, "exclude supplementary files")
	downloadCmd.Flags().IntVar(&maxConcurrent, "parallel", 8, "maximum number of concurrent downloads")
	downloadCmd.Flags().BoolVar(&resumeDownload, "resume", false, "resume interrupted downloads")
	downloadCmd.Flags().BoolVar(&skipExisting, "skip-existing", false, "skip files that already exist")
	downloadCmd.Flags().BoolVarP(&nonInteractive, "yes", "y", false, "non-interactive mode (auto-confirm prompts)")

	// Network and timeout flags
	downloadCmd.Flags().IntVarP(&downloadTimeout, "timeout", "t", 300, "timeout in seconds for download operations")

	// Custom filters flag
	downloadCmd.Flags().StringToStringVar(&customFilters, "filter", map[string]string{}, "custom filters (e.g., --filter extension=.txt)")
}
