package scanpy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

// TestDownloadSkipExistingRecordsWitness: a file already on disk is listed
// as skipped in the result, still listed in hapiq.json, and not counted as
// downloaded there (FileWitness.Skipped is not serialized).
// The scanpy catalog is static, so no HTTP is involved.
func TestDownloadSkipExistingRecordsWitness(t *testing.T) {
	out := t.TempDir()
	existing := filepath.Join(out, "pbmc3k.h5ad")
	if err := os.WriteFile(existing, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := New(WithVerbose(true)).Download(context.Background(), &downloaders.DownloadRequest{
		ID: "pbmc3k_processed", OutputDir: out,
		Options:  &downloaders.DownloadOptions{SkipExisting: true},
		Metadata: &downloaders.Metadata{ID: "pbmc3k_processed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.FilesSkipped != 1 || len(res.Files) != 1 || !res.Files[0].Skipped || res.Files[0].Path != existing {
		t.Fatalf("want one skipped file %s, got skipped=%d files=%+v", existing, res.FilesSkipped, res.Files)
	}

	raw, err := os.ReadFile(filepath.Join(out, "hapiq.json"))
	if err != nil {
		t.Fatal(err)
	}
	var w downloaders.WitnessFile
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	if len(w.Files) != 1 || filepath.Join(out, w.Files[0].Path) != existing || w.DownloadStats.FilesDownloaded != 0 {
		t.Fatalf("witness: files=%+v stats=%+v", w.Files, w.DownloadStats)
	}
}
