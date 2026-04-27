package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/manifest"
)

var (
	manifestParent         string
	manifestContinueOnFail bool
)

var manifestCmd = &cobra.Command{
	Use:   "manifest",
	Short: "Declare and run batch downloads from a YAML manifest",
	Long: `A manifest is a YAML file listing datasets to download. Each entry pairs
a folder identifier with a canonical accession ("source:id") and optional
expected files with hashes for verification.

Example manifest:

  - identifier: pbmc3k
    accession: geo:GSE123456
    files:
      - name: GSE123456_matrix.mtx.gz
        hash: md5:abc123...
  - identifier: tabula-sapiens
    accession: hca:cc95ff89-2e68-4a08-a234-480eca21ce79
    hash: sha256:def456...   # shorthand: expect exactly one file
`,
}

var manifestGenCmd = &cobra.Command{
	Use:   "gen <dir>",
	Short: "Generate a manifest entry from a downloaded dataset's witness file",
	Long: `Reads <dir>/hapiq.json and prints a TOML [[entry]] snippet to stdout
that, when added to a manifest, will reproduce the download and verify its
files via the recorded checksums.`,
	Args: cobra.ExactArgs(1),
	RunE: runManifestGen,
}

var manifestGetCmd = &cobra.Command{
	Use:   "get <manifest.yaml>",
	Short: "Download every entry in a manifest, verifying file hashes",
	Args:  cobra.ExactArgs(1),
	RunE:  runManifestGet,
}

func runManifestGen(_ *cobra.Command, args []string) error {
	dir := args[0]
	witness := filepath.Join(dir, "hapiq.json")
	entry, err := manifest.FromWitness(witness)
	if err != nil {
		return err
	}
	out, err := manifest.RenderEntry(entry)
	if err != nil {
		return fmt.Errorf("render entry: %w", err)
	}
	fmt.Print(out)
	return nil
}

func runManifestGet(_ *cobra.Command, args []string) error {
	entries, err := manifest.Load(args[0])
	if err != nil {
		return err
	}

	if manifestParent == "" {
		return fmt.Errorf("--parent-dir is required")
	}
	if err := os.MkdirAll(manifestParent, defaultDirPermissions); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	if err := initializeDownloaders(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(downloadTimeout)*time.Second)
	defer cancel()

	var failed int
	for i := range entries {
		e := &entries[i]
		if err := runEntry(ctx, manifestParent, e); err != nil {
			failed++
			_, _ = fmt.Fprintf(os.Stderr, "❌ %s: %v\n", e.Identifier, err)
			if !manifestContinueOnFail {
				return fmt.Errorf("entry %q failed: %w", e.Identifier, err)
			}
			continue
		}
		_, _ = fmt.Fprintf(os.Stderr, "✅ %s\n", e.Identifier)
	}
	if failed > 0 {
		return fmt.Errorf("%d/%d entries failed", failed, len(entries))
	}
	return nil
}

func runEntry(ctx context.Context, parent string, e *manifest.Entry) error {
	source, id, err := manifest.SplitAccession(e.Accession)
	if err != nil {
		return err
	}

	target := filepath.Join(parent, e.Identifier)
	if err := os.MkdirAll(target, defaultDirPermissions); err != nil {
		return fmt.Errorf("create target: %w", err)
	}

	opts, err := buildEntryOptions(e.Options)
	if err != nil {
		return err
	}

	vr, err := downloaders.Validate(ctx, source, id)
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	if !vr.Valid {
		return fmt.Errorf("invalid accession")
	}

	meta, err := downloaders.GetMetadata(ctx, source, vr.ID)
	if err != nil {
		return fmt.Errorf("metadata: %w", err)
	}

	req := &downloaders.DownloadRequest{
		ID:        vr.ID,
		OutputDir: target,
		Options:   opts,
		Metadata:  meta,
	}
	res, err := downloaders.Download(ctx, source, req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if !res.Success {
		return fmt.Errorf("download reported failure")
	}

	return verifyEntry(target, e, res.Files)
}

func buildEntryOptions(o *manifest.Options) (*downloaders.DownloadOptions, error) {
	out := &downloaders.DownloadOptions{
		IncludeRaw:    true,
		MaxConcurrent: defaultConcurrentDL,
		// Manifest runs are non-interactive by definition.
		NonInteractive: true,
		SkipExisting:   true,
	}
	if o == nil {
		return out, nil
	}
	out.IncludeExts = o.IncludeExts
	out.ExcludeExts = o.ExcludeExts
	out.FilenameGlob = o.FilenameGlob
	out.Subset = o.Subset
	out.Organism = o.Organism
	out.ExcludeSupplementary = o.ExcludeSupplementary
	out.IncludeRaw = !o.ExcludeRaw
	out.IncludeSRA = o.IncludeSRA
	out.LimitFiles = o.LimitFiles
	if o.MaxFileSize != "" {
		n, err := parseSize(o.MaxFileSize)
		if err != nil {
			return nil, err
		}
		out.MaxFileSize = n
	}
	return out, nil
}

func verifyEntry(target string, e *manifest.Entry, files []downloaders.FileInfo) error {
	if e.Hash != "" && len(e.Files) > 0 {
		return fmt.Errorf("entry has both 'hash' shorthand and explicit 'file' table; pick one")
	}

	if e.Hash != "" {
		if len(files) != 1 {
			return fmt.Errorf("hash shorthand expects exactly 1 downloaded file, got %d (narrow with options)", len(files))
		}
		return manifest.VerifyFile(files[0].Path, e.Hash)
	}

	for _, want := range e.Files {
		path := filepath.Join(target, want.Name)
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("expected file missing: %s", want.Name)
		}
		if err := manifest.VerifyFile(path, want.Hash); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	rootCmd.AddCommand(manifestCmd)
	manifestCmd.AddCommand(manifestGenCmd)
	manifestCmd.AddCommand(manifestGetCmd)

	manifestGetCmd.Flags().StringVar(&manifestParent, "parent-dir", "",
		"parent directory; each entry creates <parent-dir>/<identifier> (required)")
	_ = manifestGetCmd.MarkFlagRequired("parent-dir")
	manifestGetCmd.Flags().BoolVar(&manifestContinueOnFail, "continue-on-error", false,
		"keep going after an entry fails instead of stopping")
	manifestGetCmd.Flags().IntVarP(&downloadTimeout, "timeout", "t", defaultDownloadTimeoutSec,
		"timeout in seconds for the whole run")
}
