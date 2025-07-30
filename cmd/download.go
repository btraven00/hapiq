// Package cmd provides command-line interface commands for the hapiq tool.
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

const (
	outputFormatJSON = "json"
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

func runDownload(_ *cobra.Command, args []string) error {
	sourceType := args[0]
	id := args[1]

	if err := validateAndPrepareDownload(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(downloadTimeout)*time.Second)
	defer cancel()

	printDownloadInfo(sourceType, id)

	validationResult, err := validateSourceAndID(ctx, sourceType, id)
	if err != nil {
		return err
	}

	metadata, err := getAndDisplayMetadata(ctx, sourceType, validationResult.ID)
	if err != nil {
		return err
	}

	downloadRequest := createDownloadRequest(validationResult, metadata)

	result, err := performDownload(ctx, sourceType, downloadRequest)
	if err != nil {
		return err
	}

	displayResults(result)

	if output == outputFormatJSON {
		if err := outputJSON(result); err != nil {
			return err
		}
	}

	if !result.Success {
		return fmt.Errorf("download completed with errors")
	}

	_, _ = fmt.Fprintf(os.Stderr, "\n🎉 Download completed successfully!\n")

	return nil
}

func validateAndPrepareDownload() error {
	if outputDir == "" {
		return fmt.Errorf("output directory must be specified with --out flag")
	}

	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	return initializeDownloaders()
}

func printDownloadInfo(sourceType, id string) {
	if quiet {
		return
	}

	_, _ = fmt.Fprintf(os.Stderr, "Downloading from %s: %s\n", sourceType, id)
	_, _ = fmt.Fprintf(os.Stderr, "Output directory: %s\n", outputDir)
	_, _ = fmt.Fprintf(os.Stderr, "Include raw data: %t\n", !excludeRaw)
	_, _ = fmt.Fprintf(os.Stderr, "Exclude supplementary: %t\n", excludeSupplementary)
	_, _ = fmt.Fprintf(os.Stderr, "Max concurrent: %d\n", maxConcurrent)
	_, _ = fmt.Fprintf(os.Stderr, "Non-interactive: %t\n", nonInteractive)
	_, _ = fmt.Fprintf(os.Stderr, "Timeout: %ds\n", downloadTimeout)
}

func validateSourceAndID(ctx context.Context, sourceType, id string) (*downloaders.ValidationResult, error) {
	validationResult, err := downloaders.Validate(ctx, sourceType, id)
	if err != nil {
		return nil, fmt.Errorf("failed to validate %s ID '%s': %w", sourceType, id, err)
	}

	if !validationResult.Valid {
		_, _ = fmt.Fprintf(os.Stderr, "❌ Validation failed for %s ID '%s':\n", sourceType, id)
		for _, errMsg := range validationResult.Errors {
			_, _ = fmt.Fprintf(os.Stderr, "   Error: %s\n", errMsg)
		}

		return nil, fmt.Errorf("invalid %s ID: %s", sourceType, id)
	}

	if len(validationResult.Warnings) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "⚠️  Validation warnings for %s ID '%s':\n", sourceType, id)
		for _, warning := range validationResult.Warnings {
			_, _ = fmt.Fprintf(os.Stderr, "   Warning: %s\n", warning)
		}
	}

	if !quiet {
		_, _ = fmt.Fprintf(os.Stderr, "✅ ID validation successful\n")
	}

	return validationResult, nil
}

func getAndDisplayMetadata(ctx context.Context, sourceType, id string) (*downloaders.Metadata, error) {
	if !quiet {
		_, _ = fmt.Fprintf(os.Stderr, "🔍 Retrieving metadata...\n")
	}

	metadata, err := downloaders.GetMetadata(ctx, sourceType, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata for %s ID '%s': %w", sourceType, id, err)
	}

	displayMetadataSummary(metadata)

	return metadata, nil
}

func displayMetadataSummary(metadata *downloaders.Metadata) {
	_, _ = fmt.Fprintf(os.Stderr, "\n📊 Dataset Information:\n")
	_, _ = fmt.Fprintf(os.Stderr, "   Source: %s\n", metadata.Source)
	_, _ = fmt.Fprintf(os.Stderr, "   ID: %s\n", metadata.ID)
	_, _ = fmt.Fprintf(os.Stderr, "   Title: %s\n", metadata.Title)

	if metadata.Description != "" {
		_, _ = fmt.Fprintf(os.Stderr, "   Description: %s\n", truncateString(metadata.Description, 100))
	}

	if len(metadata.Authors) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "   Authors: %s\n", metadata.Authors[0])
		if len(metadata.Authors) > 1 {
			_, _ = fmt.Fprintf(os.Stderr, "            (+%d more)\n", len(metadata.Authors)-1)
		}
	}

	_, _ = fmt.Fprintf(os.Stderr, "   Files: %d\n", metadata.FileCount)
	_, _ = fmt.Fprintf(os.Stderr, "   Total size: %s\n", common.FormatBytes(metadata.TotalSize))

	if metadata.DOI != "" {
		_, _ = fmt.Fprintf(os.Stderr, "   DOI: %s\n", metadata.DOI)
	}

	if metadata.License != "" {
		_, _ = fmt.Fprintf(os.Stderr, "   License: %s\n", metadata.License)
	}

	if len(metadata.Collections) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "\n📦 Collections:\n")
		for _, collection := range metadata.Collections {
			_, _ = fmt.Fprintf(os.Stderr, "   %s: %s (%d files, %s)\n",
				collection.Type, collection.Title, collection.FileCount,
				common.FormatBytes(collection.EstimatedSize))
		}
	}
}

func createDownloadRequest(
	validationResult *downloaders.ValidationResult,
	metadata *downloaders.Metadata,
) *downloaders.DownloadRequest {
	downloadOptions := &downloaders.DownloadOptions{
		IncludeRaw:           !excludeRaw,
		ExcludeSupplementary: excludeSupplementary,
		MaxConcurrent:        maxConcurrent,
		Resume:               resumeDownload,
		SkipExisting:         skipExisting,
		NonInteractive:       nonInteractive,
		CustomFilters:        customFilters,
	}

	return &downloaders.DownloadRequest{
		ID:        validationResult.ID,
		OutputDir: outputDir,
		Options:   downloadOptions,
		Metadata:  metadata,
	}
}

func performDownload(
	ctx context.Context,
	sourceType string,
	request *downloaders.DownloadRequest,
) (*downloaders.DownloadResult, error) {
	if !quiet {
		_, _ = fmt.Fprintf(os.Stderr, "\n⬇️  Starting download...\n")
	}

	return downloaders.Download(ctx, sourceType, request)
}

func displayResults(result *downloaders.DownloadResult) {
	_, _ = fmt.Fprintf(os.Stderr, "\n📋 Download Results:\n")

	if result.Success {
		_, _ = fmt.Fprintf(os.Stderr, "   Status: ✅ Success\n")
	} else {
		_, _ = fmt.Fprintf(os.Stderr, "   Status: ❌ Failed\n")
	}

	_, _ = fmt.Fprintf(os.Stderr, "   Duration: %v\n", result.Duration.Round(time.Second))
	_, _ = fmt.Fprintf(os.Stderr, "   Files downloaded: %d\n", len(result.Files))
	_, _ = fmt.Fprintf(os.Stderr, "   Data downloaded: %s\n", common.FormatBytes(result.BytesDownloaded))

	if result.BytesTotal > 0 {
		percentage := float64(result.BytesDownloaded) / float64(result.BytesTotal) * 100
		_, _ = fmt.Fprintf(os.Stderr, "   Completion: %.1f%%\n", percentage)
	}

	if result.Duration.Seconds() > 0 {
		speed := float64(result.BytesDownloaded) / result.Duration.Seconds()
		_, _ = fmt.Fprintf(os.Stderr, "   Average speed: %s/s\n", common.FormatBytes(int64(speed)))
	}

	if result.WitnessFile != "" {
		_, _ = fmt.Fprintf(os.Stderr, "   Witness file: %s\n", result.WitnessFile)
	}

	displayWarningsAndErrors(result)
	displayFileList(result)
}

func displayWarningsAndErrors(result *downloaders.DownloadResult) {
	if len(result.Warnings) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "\n⚠️  Warnings:\n")
		for _, warning := range result.Warnings {
			_, _ = fmt.Fprintf(os.Stderr, "   %s\n", warning)
		}
	}

	if len(result.Errors) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "\n❌ Errors:\n")
		for _, error := range result.Errors {
			_, _ = fmt.Fprintf(os.Stderr, "   %s\n", error)
		}
	}
}

func displayFileList(result *downloaders.DownloadResult) {
	if quiet || len(result.Files) == 0 {
		return
	}

	_, _ = fmt.Fprintf(os.Stderr, "\n📁 Downloaded Files:\n")

	for i := range result.Files {
		file := &result.Files[i]
		_, _ = fmt.Fprintf(os.Stderr, "   %s (%s)\n", file.Path, common.FormatBytes(file.Size))

		if file.Checksum != "" {
			_, _ = fmt.Fprintf(os.Stderr, "      %s: %s\n", file.ChecksumType, file.Checksum)
		}
	}
}

func outputJSON(result *downloaders.DownloadResult) error {
	encoder := &jsonEncoder{}
	encoder.SetIndent("", "  ")

	return encoder.Encode(result)
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
			_, _ = fmt.Fprintf(os.Stderr, "Using NCBI API key for increased rate limits\n")
		}

		geoOptions = append(geoOptions, geo.WithAPIKey(apiKey))
	} else if !quiet {
		_, _ = fmt.Fprintf(os.Stderr, "No NCBI API key found. Set NCBI_API_KEY environment variable for higher rate limits\n")
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

func (e *jsonEncoder) SetIndent(_, _ string) {}
func (e *jsonEncoder) Encode(_ interface{}) error {
	// This would use the actual JSON encoder from the common package
	// For now, just print a placeholder
	_, _ = fmt.Fprintln(os.Stderr, "JSON output would be here")
	return nil
}

func init() {
	rootCmd.AddCommand(downloadCmd)

	// Output directory flag (required)
	downloadCmd.Flags().StringVar(&outputDir, "out", "", "output directory for downloaded files (required)")
	_ = downloadCmd.MarkFlagRequired("out")

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
	downloadCmd.Flags().StringToStringVar(&customFilters, "filter", map[string]string{},
		"custom filters (e.g., --filter extension=.txt)")
}
