package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/btraven00/hapiq/pkg/remote"
)

// A locked dataset staged under $HAPIQ_STAGED/<digest>/ is linked, with no
// remote (the entry lists none); a wrong sha256 falls through to the remotes.
func TestFetchLockedStaged(t *testing.T) {
	root, dir := t.TempDir(), t.TempDir()
	body := []byte("counts")
	sum := sha256.Sum256(body)
	e := &remote.Entry{Accession: "geo:GSE1", Digest: "d1",
		Files: []remote.File{{Path: "supplementary/m.mtx.gz", Size: 6, SHA256: hex.EncodeToString(sum[:])}}}
	src := filepath.Join(root, "d1", "supplementary", "m.mtx.gz")
	if err := os.MkdirAll(filepath.Dir(src), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, body, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HAPIQ_STAGED", root)
	loc, err := fetchLocked(context.Background(), e, dir, nil)
	if err != nil || loc.Type != "staged" {
		t.Fatalf("fetchLocked = %v, %v", loc, err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "supplementary", "m.mtx.gz")); err != nil || string(got) != "counts" {
		t.Fatalf("linked file = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hapiq.json")); err != nil {
		t.Fatal(err)
	}
	e.Files[0].SHA256 = "00"
	if _, err := fetchLocked(context.Background(), e, t.TempDir(), nil); err == nil {
		t.Fatal("a mismatched staged copy was used")
	}
}
