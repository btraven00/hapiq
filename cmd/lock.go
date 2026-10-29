package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/btraven00/hapiq/internal/version"
	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/common"
	"github.com/btraven00/hapiq/pkg/remote"
	_ "github.com/btraven00/hapiq/pkg/remote/thoa" // registers the thoa remote
)

var (
	lockPath   string
	lockRemote string
	lockUpdate bool
)

var lockCmd = &cobra.Command{
	Use:   "lock",
	Short: "Check that every dataset in a hapiq.lock still exists on its remote",
	Long: `hapiq.lock pins datasets to the copies pushed to a remote (thoa's data
lake), so jobs fetch them from there instead of the upstream repository.
It is used only when named: --lock <path> or HAPIQ_LOCK=<path>.

  hapiq download geo GSE123456 --out data/pbmc --include-ext .h5ad
  hapiq lock add data/pbmc          # push to thoa, write ./hapiq.lock
  hapiq lock                        # check every entry still exists

In a job, with HAPIQ_LOCK set, the same 'hapiq download' fetches from thoa
and checks each file's sha256 against the lock. See docs/mirrors.md.`,
	Args: cobra.NoArgs,
	RunE: runLockCheck,
}

var lockAddCmd = &cobra.Command{
	Use:   "add <dir>...",
	Short: "Push downloaded datasets to a remote and pin them in hapiq.lock",
	Long: `Reads <dir>/hapiq.json, pushes the files it lists to the remote and records
accession + filters -> digest + files + remote dataset in the lockfile.

A dataset already locked with the same digest is not pushed again while its
remote copy exists. One whose files changed (a different digest) is refused
unless --update is given.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runLockAdd,
}

func runLockAdd(_ *cobra.Command, args []string) error {
	path := lockFilePath("hapiq.lock")
	lock, err := remote.LoadOrNewLock(path)
	if err != nil {
		return err
	}
	r, err := remote.Open(lockRemote)
	if err != nil {
		return err
	}
	ctx := context.Background()
	for _, dir := range args {
		e, err := remote.EntryFromDir(filepath.Clean(dir))
		if err != nil {
			return err
		}
		if old := lock.Find(e.Accession, e.Filters); old != nil {
			if old.Digest != e.Digest && !lockUpdate {
				return fmt.Errorf("%s: files differ from the locked version (digest %s, locked %s); pass --update to re-pin",
					e.Accession, short(e.Digest), short(old.Digest))
			}
			if old.Digest == e.Digest && liveCopy(ctx, r, old) {
				_, _ = fmt.Fprintf(os.Stderr, "✅ %s already locked (%s)\n", e.Accession, short(e.Digest))
				continue
			}
		}
		_, _ = fmt.Fprintf(os.Stderr, "⬆️  pushing %s (%d files) to %s\n", e.Accession, len(e.Files), lockRemote)
		loc, err := r.Push(ctx, e.Files)
		if err != nil {
			return fmt.Errorf("%s: push: %w", e.Accession, err)
		}
		if err := checkPaths(ctx, r, loc, e.Files); err != nil {
			return fmt.Errorf("%s: after push: %w", e.Accession, err)
		}
		e.Remotes = []remote.Location{loc}
		lock.Put(*e)
		// Save after every dataset, so a later failure keeps what was pushed.
		if err := lock.Save(path); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(os.Stderr, "🔒 %s  %s  %s\n", e.Accession, short(e.Digest), loc)
	}
	return lock.Save(path)
}

func runLockCheck(_ *cobra.Command, _ []string) error {
	path := lockFilePath("")
	if path == "" {
		return fmt.Errorf("no lockfile: pass --lock <path> or set HAPIQ_LOCK")
	}
	lock, err := remote.LoadLock(path)
	if err != nil {
		return err
	}
	ctx := context.Background()
	var bad int
	for i := range lock.Entries {
		e := &lock.Entries[i]
		for _, loc := range e.Remotes {
			r, err := remote.Open(loc.Type)
			if err == nil {
				err = checkPaths(ctx, r, loc, e.Files)
			}
			if err != nil {
				bad++
				_, _ = fmt.Fprintf(os.Stderr, "❌ %s  %s: %v\n", e.Accession, loc, err)
				continue
			}
			_, _ = fmt.Fprintf(os.Stderr, "✅ %s  %s\n", e.Accession, loc)
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d remote copies missing or incomplete", bad)
	}
	return nil
}

// lockFilePath resolves --lock, then HAPIQ_LOCK, then def. Never searched for.
func lockFilePath(def string) string {
	if lockPath != "" {
		return lockPath
	}
	if p := os.Getenv("HAPIQ_LOCK"); p != "" {
		return p
	}
	return def
}

func liveCopy(ctx context.Context, r remote.Remote, e *remote.Entry) bool {
	for _, loc := range e.Remotes {
		if loc.Type == lockRemote && checkPaths(ctx, r, loc, e.Files) == nil {
			return true
		}
	}
	return false
}

// checkPaths compares the remote's path list with the file set. Content is
// checked by sha256 on fetch; thoa reports no hashes through its file listing.
func checkPaths(ctx context.Context, r remote.Remote, loc remote.Location, files []remote.File) error {
	got, err := r.Stat(ctx, loc)
	if err != nil {
		return err
	}
	for _, f := range files {
		if !slices.Contains(got, f.Path) {
			return fmt.Errorf("%s missing on the remote", f.Path)
		}
	}
	if len(got) != len(files) {
		return fmt.Errorf("remote holds %d files, lock lists %d", len(got), len(files))
	}
	return nil
}

// fetchLocked downloads a locked dataset from the first of its remotes whose
// files all match the lock, and writes hapiq.json as a download would.
func fetchLocked(ctx context.Context, e *remote.Entry, dir string, opts *downloaders.DownloadOptions) (remote.Location, error) {
	var lastErr error
	for _, loc := range e.Remotes {
		r, err := remote.Open(loc.Type)
		if err == nil {
			err = r.Fetch(ctx, loc, dir)
		}
		if err == nil {
			err = remote.Verify(dir, e.Files)
		}
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", loc, err)
			_, _ = fmt.Fprintf(os.Stderr, "⚠️  %v\n", lastErr)
			continue
		}
		return loc, writeLockedWitness(e, dir, loc, opts)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no remote copies recorded")
	}
	return remote.Location{}, lastErr
}

func writeLockedWitness(e *remote.Entry, dir string, loc remote.Location, opts *downloaders.DownloadOptions) error {
	source, id, _ := strings.Cut(e.Accession, ":")
	now := time.Now()
	w := &downloaders.WitnessFile{
		DownloadTime: now,
		HapiqVersion: version.String(),
		Source:       source,
		OriginalID:   id,
		Options:      opts,
		FetchedFrom:  loc.String(),
		Digest:       e.Digest,
	}
	for _, f := range e.Files {
		w.Files = append(w.Files, downloaders.FileWitness{
			DownloadTime: now,
			Path:         f.Path,
			OriginalName: filepath.Base(f.Path),
			Checksum:     f.SHA256,
			ChecksumType: "sha256",
			SourceURL:    loc.String() + "/" + f.Path,
			Size:         f.Size,
		})
	}
	return common.WriteWitnessFile(dir, w)
}

func short(digest string) string {
	if len(digest) > 12 {
		return digest[:12]
	}
	return digest
}

func init() {
	rootCmd.AddCommand(lockCmd)
	lockCmd.AddCommand(lockAddCmd)
	lockCmd.PersistentFlags().StringVar(&lockPath, "lock", "",
		"lockfile to use (default: $HAPIQ_LOCK; lock add writes ./hapiq.lock)")
	lockAddCmd.Flags().StringVar(&lockRemote, "remote", "thoa", "remote type to push to")
	lockAddCmd.Flags().BoolVar(&lockUpdate, "update", false, "re-pin a dataset whose files changed")
	downloadCmd.Flags().StringVar(&lockPath, "lock", "",
		"fetch locked datasets from their remote copy (default: $HAPIQ_LOCK)")
}
