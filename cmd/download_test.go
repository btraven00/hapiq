package cmd

import (
	"crypto/md5" // #nosec G501 -- checksum verification only
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

func md5Hex(b []byte) string {
	h := md5.Sum(b) // #nosec G401 -- checksum verification only
	return hex.EncodeToString(h[:])
}

// TestVerifyExpectedHash_AlreadyJoinedRelativePath is the regression guard for
// the double-join bug: downloaders store Path already joined with the output
// dir, so verifyExpectedHash must use it verbatim rather than re-joining the
// output dir (which produced <out>/<out>/<file> and a spurious failure).
func TestVerifyExpectedHash_AlreadyJoinedRelativePath(t *testing.T) {
	t.Chdir(t.TempDir())

	outputDir := "out"
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("scfoundation norman h5ad bytes")
	joined := filepath.Join(outputDir, "data.bin") // out/data.bin, relative to cwd
	if err := os.WriteFile(joined, content, 0o644); err != nil {
		t.Fatal(err)
	}

	result := &downloaders.DownloadResult{
		Files: []downloaders.FileInfo{{Path: joined, OriginalName: "data.bin"}},
	}

	if err := verifyExpectedHash(result, outputDir, "md5:"+md5Hex(content)); err != nil {
		t.Fatalf("verifyExpectedHash() error = %v, want nil (must not re-join %q)", err, outputDir)
	}
}

// TestVerifyExpectedHash_BareBasenameFallback covers the fallback branch: when
// Path is a bare basename that does not resolve from the cwd, it is joined with
// the output dir.
func TestVerifyExpectedHash_BareBasenameFallback(t *testing.T) {
	t.Chdir(t.TempDir())

	outputDir := "out"
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("hello")
	if err := os.WriteFile(filepath.Join(outputDir, "data.bin"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	result := &downloaders.DownloadResult{
		Files: []downloaders.FileInfo{{Path: "data.bin", OriginalName: "data.bin"}},
	}

	if err := verifyExpectedHash(result, outputDir, "md5:"+md5Hex(content)); err != nil {
		t.Fatalf("verifyExpectedHash() error = %v, want nil", err)
	}
}

func TestVerifyExpectedHash_Mismatch(t *testing.T) {
	t.Chdir(t.TempDir())

	outputDir := "out"
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	joined := filepath.Join(outputDir, "data.bin")
	if err := os.WriteFile(joined, []byte("actual content"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := &downloaders.DownloadResult{
		Files: []downloaders.FileInfo{{Path: joined, OriginalName: "data.bin"}},
	}

	err := verifyExpectedHash(result, outputDir, "md5:"+md5Hex([]byte("different content")))
	if err == nil {
		t.Fatal("expected a hash mismatch error")
	}
	// A mismatch removes the offending file from the output dir.
	if _, statErr := os.Stat(joined); !os.IsNotExist(statErr) {
		t.Errorf("expected %q to be removed on mismatch, stat err = %v", joined, statErr)
	}
}
