package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
	"github.com/btraven00/hapiq/pkg/manifest"
)

var refetchCmd = &cobra.Command{
	Use:   "refetch <dir>...",
	Short: "Re-download a dataset into its folder from the hapiq.json there",
	Long: `Reads <dir>/hapiq.json and downloads the same source, ID and filters
back into <dir>, so the data can be deleted and the hapiq.json kept.

Files still on disk are kept. Every file listed in hapiq.json is then checked
against its recorded checksum, so a file that changed upstream fails the run.
hapiq.json is updated in place (merged, as for any download into the folder).

  rm -r data/GSE264667/supplementary
  hapiq refetch data/GSE264667`,
	Args: cobra.MinimumNArgs(1),
	RunE: runRefetch,
}

func runRefetch(_ *cobra.Command, args []string) error {
	if err := initializeDownloaders(); err != nil {
		return err
	}

	ctx, closeCache := attachCache(context.Background(), quiet)
	defer closeCache()

	var failed int
	for _, dir := range args {
		if err := refetchDir(ctx, filepath.Clean(dir)); err != nil {
			failed++
			_, _ = fmt.Fprintf(os.Stderr, "❌ %s: %v\n", dir, err)
			continue
		}
		_, _ = fmt.Fprintf(os.Stderr, "✅ %s\n", dir)
	}
	if failed > 0 {
		return fmt.Errorf("%d/%d refetches failed", failed, len(args))
	}
	return nil
}

func refetchDir(ctx context.Context, dir string) (err error) {
	witnessPath := filepath.Join(dir, "hapiq.json")
	recorded, err := os.ReadFile(witnessPath) // #nosec G304 -- user-specified dataset dir
	if err != nil {
		return err
	}
	// The download merges its own hashes into hapiq.json. On failure put the
	// recorded one back, so a changed upstream is not written into provenance.
	defer func() {
		if err != nil {
			_ = os.WriteFile(witnessPath, recorded, 0o600) // #nosec G703 -- restores the hapiq.json read above, in the user-specified dataset dir
		}
	}()

	w, err := common.LoadWitnessFile(dir)
	if err != nil {
		return err
	}
	if w.Source == "" || w.OriginalID == "" {
		return fmt.Errorf("hapiq.json has no source/original_id")
	}

	// Read the recorded hashes now: the download rewrites hapiq.json.
	want, err := manifest.FromWitness(witnessPath)
	if err != nil {
		return err
	}

	outDir, err := refetchOutputDir(w.Source, w.OriginalID, dir)
	if err != nil {
		return err
	}

	opts := &downloaders.DownloadOptions{}
	if w.Options != nil {
		*opts = *w.Options
	}
	opts.DryRun, opts.Force, opts.Resume = false, false, false
	opts.SkipExisting, opts.NonInteractive = true, true

	vr, err := downloaders.Validate(ctx, w.Source, w.OriginalID)
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	if !vr.Valid {
		return fmt.Errorf("invalid %s id %q: %s", w.Source, w.OriginalID, strings.Join(vr.Errors, "; "))
	}

	meta, err := downloaders.GetMetadata(ctx, w.Source, vr.ID)
	if err != nil {
		return fmt.Errorf("metadata: %w", err)
	}

	res, err := downloaders.Download(ctx, w.Source, &downloaders.DownloadRequest{
		ID: vr.ID, OutputDir: outDir, Options: opts, Metadata: meta,
	})
	if err != nil {
		return err
	}
	if !res.Success {
		return fmt.Errorf("download failed: %s", strings.Join(res.Errors, "; "))
	}
	if res.WitnessFile != "" && filepath.Clean(filepath.Dir(res.WitnessFile)) != dir {
		return fmt.Errorf("download landed in %s, not %s", filepath.Dir(res.WitnessFile), dir)
	}

	for _, f := range want.Files {
		path, err := locateInDir(dir, f.Name)
		if err != nil {
			return err
		}
		if err := manifest.VerifyFile(path, f.Hash); err != nil {
			_ = os.Remove(path) // not the recorded data
			return fmt.Errorf("%w (removed; hapiq.json left as recorded)", err)
		}
	}
	return nil
}

// refetchOutputDir is the --out that makes a download land in dir again.
func refetchOutputDir(source, id, dir string) (string, error) {
	switch source {
	case "geo", "figshare": // these download into <out>/<id>
		if filepath.Base(dir) != common.SanitizeFilename(id) {
			return "", fmt.Errorf("%s downloads into <out>/%s, so %s cannot be refetched under its current name",
				source, common.SanitizeFilename(id), dir)
		}
		return filepath.Dir(dir), nil
	case "ensembl":
		// ponytail: hapiq.json records database:version:content but not the
		// species, so a refetch could pull a whole division. Add it there first.
		return "", fmt.Errorf("refetch does not support ensembl yet")
	}
	return dir, nil
}

// locateInDir finds a hapiq.json file path under dir. Downloaders record paths
// relative to dir ("supplementary/x"), to the working directory of the original
// run ("data/x"), or absolute; the longest suffix of the path found under dir wins.
func locateInDir(dir, name string) (string, error) {
	p := filepath.ToSlash(name)
	for {
		candidate := filepath.Join(dir, filepath.FromSlash(p))
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		i := strings.Index(p, "/")
		if i < 0 {
			return "", fmt.Errorf("%s listed in hapiq.json but missing after refetch", name)
		}
		p = p[i+1:]
	}
}

func init() {
	rootCmd.AddCommand(refetchCmd)
	refetchCmd.Flags().IntVarP(&downloadTimeout, "timeout", "t", defaultDownloadTimeoutSec,
		"stall limit in seconds: fail a request when no data arrives for this long")
}
