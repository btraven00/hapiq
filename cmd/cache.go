package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/btraven00/hapiq/pkg/cache"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
)

var (
	cacheDirFlag string
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the local blob cache",
}

var cacheInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Print cache statistics",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, cfg, err := openCacheForCmd()
		if err != nil {
			return err
		}
		defer c.Close()

		ctx := context.Background()
		total, err := c.TotalSize(ctx)
		if err != nil {
			return err
		}
		count, err := c.BlobCount(ctx)
		if err != nil {
			return err
		}

		fmt.Printf("Cache dir:   %s\n", cfg.Dir)
		fmt.Printf("Blobs:       %d\n", count)
		fmt.Printf("Total size:  %s\n", common.FormatBytes(total))
		if cfg.MaxSize > 0 {
			pct := float64(total) / float64(cfg.MaxSize) * 100
			fmt.Printf("Quota:       %s (%.1f%% full)\n", common.FormatBytes(cfg.MaxSize), pct)
		} else {
			fmt.Printf("Quota:       none\n")
		}
		return nil
	},
}

var (
	cacheListURL  string
	cacheListJSON bool
)

var cacheListCmd = &cobra.Command{
	Use:   "list",
	Short: "List cached blobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := openCacheForCmd()
		if err != nil {
			return err
		}
		defer c.Close()

		blobs, err := c.ListBlobs(context.Background(), cacheListURL)
		if err != nil {
			return err
		}

		if cacheListJSON {
			return json.NewEncoder(os.Stdout).Encode(blobs)
		}

		fmt.Printf("%-64s  %10s  %-20s  %s\n", "SHA256", "SIZE", "LAST USED", "URL(s)")
		for _, b := range blobs {
			url := ""
			if len(b.URLs) > 0 {
				url = b.URLs[0]
			}
			fmt.Printf("%-64s  %10s  %-20s  %s\n",
				b.SHA256,
				common.FormatBytes(b.Size),
				b.LastUsed.Format("2006-01-02 15:04:05"),
				url,
			)
			for _, u := range b.URLs[1:] {
				fmt.Printf("%s  %s\n", strings.Repeat(" ", 64+10+20+4), u)
			}
		}
		return nil
	},
}

var (
	cacheVerifyAll bool
)

var cacheVerifyCmd = &cobra.Command{
	Use:   "verify [sha256]",
	Short: "Re-hash blobs and evict corrupt entries",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := openCacheForCmd()
		if err != nil {
			return err
		}
		defer c.Close()

		ctx := context.Background()

		if len(args) == 1 {
			ok, err := c.VerifyBlob(ctx, args[0])
			if err != nil {
				return err
			}
			if ok {
				fmt.Printf("OK: %s\n", args[0])
			} else {
				fmt.Printf("CORRUPT (evicted): %s\n", args[0])
			}
			return nil
		}

		blobs, err := c.ListBlobs(ctx, "")
		if err != nil {
			return err
		}

		corrupt := 0
		for _, b := range blobs {
			ok, err := c.VerifyBlob(ctx, b.SHA256)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error verifying %s: %v\n", b.SHA256[:16], err)
				continue
			}
			if !ok {
				fmt.Printf("CORRUPT (evicted): %s\n", b.SHA256)
				corrupt++
			}
		}
		fmt.Printf("Verified %d blobs; %d corrupt.\n", len(blobs), corrupt)
		return nil
	},
}

var (
	cacheGCDryRun bool
	cacheGCKeep   string
)

var cacheGCCmd = &cobra.Command{
	Use:   "gc",
	Short: "Evict least-recently-used blobs until under quota",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := openCacheForCmd()
		if err != nil {
			return err
		}
		defer c.Close()

		var keep time.Duration
		if cacheGCKeep != "" {
			keep, err = time.ParseDuration(cacheGCKeep)
			if err != nil {
				return fmt.Errorf("invalid --keep value: %w", err)
			}
		}

		result, err := c.GC(context.Background(), cacheGCDryRun, keep)
		if err != nil {
			return err
		}

		if cacheGCDryRun {
			fmt.Printf("Dry-run: would evict %d blobs, freeing %s\n",
				result.Evicted, common.FormatBytes(result.Freed))
		} else {
			fmt.Printf("Evicted %d blobs, freed %s\n",
				result.Evicted, common.FormatBytes(result.Freed))
		}
		return nil
	},
}

var cacheEvictCmd = &cobra.Command{
	Use:   "evict <sha256>",
	Short: "Remove a specific blob and its URL mappings",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := openCacheForCmd()
		if err != nil {
			return err
		}
		defer c.Close()

		if err := c.Evict(context.Background(), args[0]); err != nil {
			return err
		}
		fmt.Printf("Evicted: %s\n", args[0])
		return nil
	},
}

var cachePruneURLsCmd = &cobra.Command{
	Use:   "prune-urls",
	Short: "Remove URL index entries whose blobs are missing",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _, err := openCacheForCmd()
		if err != nil {
			return err
		}
		defer c.Close()

		n, err := c.PruneURLs(context.Background())
		if err != nil {
			return err
		}
		fmt.Printf("Pruned %d stale URL entries.\n", n)
		return nil
	},
}

func init() {
	cacheCmd.PersistentFlags().StringVar(&cacheDirFlag, "cache-dir", "", "override cache directory")

	cacheListCmd.Flags().StringVar(&cacheListURL, "url", "", "filter by URL glob pattern")
	cacheListCmd.Flags().BoolVar(&cacheListJSON, "json", false, "output as JSON")

	cacheVerifyCmd.Flags().BoolVar(&cacheVerifyAll, "all", false, "verify all blobs (default when no sha256 given)")

	cacheGCCmd.Flags().BoolVar(&cacheGCDryRun, "dry-run", false, "show what would be evicted without removing")
	cacheGCCmd.Flags().StringVar(&cacheGCKeep, "keep", "", "spare blobs accessed within this duration (e.g. 7d, 24h)")

	cacheCmd.AddCommand(cacheInfoCmd, cacheListCmd, cacheVerifyCmd, cacheGCCmd, cacheEvictCmd, cachePruneURLsCmd)
	rootCmd.AddCommand(cacheCmd)
}

// openCacheForCmd opens the cache using the resolved config, with --cache-dir override.
func openCacheForCmd() (*cache.Cache, cache.Config, error) {
	cfg := cache.ConfigFromViper()
	if cacheDirFlag != "" {
		cfg.Dir = cacheDirFlag
	}
	if cfg.Dir == "" {
		cfg.Dir = cache.DefaultDir()
	}

	// For management commands the cache mode check is bypassed — operators need
	// to inspect the store regardless of whether downloads have caching enabled.
	c, err := cache.Open(cfg)
	if err != nil {
		// If the directory doesn't exist yet, give a clear message.
		if _, statErr := os.Stat(cfg.Dir); os.IsNotExist(statErr) {
			return nil, cfg, fmt.Errorf("cache directory does not exist: %s (set cache.mode=on or use --cache-dir)", cfg.Dir)
		}
		return nil, cfg, fmt.Errorf("open cache: %w", err)
	}

	_ = viper.Get("cache.mode") // ensure viper is initialised
	return c, cfg, nil
}
