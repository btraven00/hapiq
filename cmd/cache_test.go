package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/btraven00/hapiq/pkg/cache"
)

// captureStdout runs fn and returns what it printed to stdout.
func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	runErr := fn()
	os.Stdout = orig
	w.Close()
	out, _ := io.ReadAll(r)
	if runErr != nil {
		t.Fatalf("command failed: %v\n%s", runErr, out)
	}
	return string(out)
}

func TestCacheVerifyOrphans(t *testing.T) {
	dir := t.TempDir()
	c, err := cache.Open(cache.Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()

	sha := "ab" + strings.Repeat("0", 62)
	orphan := filepath.Join(dir, "blobs", "sha256", sha[:2], sha)
	if err := os.MkdirAll(filepath.Dir(orphan), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphan, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(orphan, old, old); err != nil {
		t.Fatal(err)
	}

	cacheDirFlag = dir
	t.Cleanup(func() { cacheDirFlag, cacheVerifyRemoveOrphans = "", false })

	run := func() error { return cacheVerifyCmd.RunE(cacheVerifyCmd, nil) }

	out := captureStdout(t, run)
	if !strings.Contains(out, "ORPHAN: "+orphan) || !strings.Contains(out, "Found 1 orphan blob files") {
		t.Errorf("report mode output missing orphan:\n%s", out)
	}
	if _, err := os.Stat(orphan); err != nil {
		t.Fatalf("report mode must not delete: %v", err)
	}

	cacheVerifyRemoveOrphans = true
	out = captureStdout(t, run)
	if !strings.Contains(out, "Removed 1 of 1 orphan blob files") {
		t.Errorf("remove mode output:\n%s", out)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("orphan still on disk after --remove-orphans")
	}
}
