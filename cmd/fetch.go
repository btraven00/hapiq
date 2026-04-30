package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/btraven00/hapiq/pkg/cache"
	"github.com/btraven00/hapiq/pkg/downloaders"
)

var (
	fetchOutputDir string
	fetchDryRun    bool
	fetchHash      string
)

var fetchCmd = &cobra.Command{
	Use:   "fetch <url>",
	Short: "Download a single file directly from an HTTP(S) URL",
	Long: `Fetch downloads a single file from a direct HTTP or HTTPS URL.

The file is named after the URL path basename, or the Content-Disposition
header if the server provides one.  A hapiq.json witness file is written
alongside the download for provenance tracking.

In a YAML manifest use:
  accession: url:https://example.com/file.csv

Examples:
  hapiq fetch https://example.com/data.csv --out ./data
  hapiq fetch https://example.com/data.h5ad --out ./data --dry-run
  hapiq fetch https://example.com/data.h5ad --out ./data --hash sha256:abc123...`,
	Args: cobra.ExactArgs(1),
	RunE: runFetch,
}

func runFetch(_ *cobra.Command, args []string) error {
	rawURL := args[0]

	if fetchOutputDir == "" {
		return fmt.Errorf("output directory must be specified with --out flag")
	}
	if err := os.MkdirAll(fetchOutputDir, defaultDirPermissions); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	if err := initializeDownloaders(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(downloadTimeout)*time.Second)
	defer cancel()

	if viper.GetString("cache.mode") == "on" {
		cfg := cache.ConfigFromViper()
		if c, err := cache.Open(cfg); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: cache unavailable: %v\n", err)
		} else {
			defer c.Close()
			ctx = cache.WithCache(ctx, c)
			if !quiet {
				_, _ = fmt.Fprintf(os.Stderr, "Cache enabled: %s\n", cfg.Dir)
			}
		}
	}

	vr, err := downloaders.Validate(ctx, "url", rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if !vr.Valid {
		return fmt.Errorf("invalid URL %q: %s", rawURL, vr.Errors)
	}

	meta, err := downloaders.GetMetadata(ctx, "url", rawURL)
	if err != nil {
		return fmt.Errorf("metadata: %w", err)
	}

	if !quiet {
		_, _ = fmt.Fprintf(os.Stderr, "Fetching: %s\n", rawURL)
		_, _ = fmt.Fprintf(os.Stderr, "Output directory: %s\n", fetchOutputDir)
	}

	req := &downloaders.DownloadRequest{
		ID:        rawURL,
		OutputDir: fetchOutputDir,
		Options: &downloaders.DownloadOptions{
			DryRun:       fetchDryRun,
			SkipExisting: skipExisting,
		},
		Metadata: meta,
	}

	result, err := downloaders.Download(ctx, "url", req)
	if err != nil {
		return err
	}

	if fetchHash != "" && !fetchDryRun {
		if err := verifyExpectedHash(result, fetchOutputDir, fetchHash); err != nil {
			return err
		}
	}

	displayResults(result)

	if !result.Success {
		return fmt.Errorf("fetch failed")
	}
	if fetchDryRun {
		_, _ = fmt.Fprintf(os.Stderr, "\n✅ Dry-run complete. Use without --dry-run to download.\n")
		return nil
	}
	_, _ = fmt.Fprintf(os.Stderr, "\n🎉 Fetch completed successfully!\n")
	return nil
}

func init() {
	rootCmd.AddCommand(fetchCmd)

	fetchCmd.Flags().StringVar(&fetchOutputDir, "out", "", "output directory for the downloaded file (required)")
	_ = fetchCmd.MarkFlagRequired("out")

	fetchCmd.Flags().BoolVar(&fetchDryRun, "dry-run", false,
		"show what would be downloaded without writing anything to disk")
	fetchCmd.Flags().StringVar(&fetchHash, "hash", "",
		"expected file hash in <algo>:<hex> form (e.g. sha256:abc123...)")
	fetchCmd.Flags().BoolVar(&skipExisting, "skip-existing", false,
		"skip download if the file already exists")
	fetchCmd.Flags().IntVarP(&downloadTimeout, "timeout", "t", defaultDownloadTimeoutSec,
		"timeout in seconds")
}
