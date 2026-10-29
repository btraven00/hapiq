package common

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

func TestHandleDirectoryConflicts(t *testing.T) {
	existing := &downloaders.DirectoryStatus{Exists: true, Conflicts: []string{"a.txt"}}
	withWitness := &downloaders.DirectoryStatus{Exists: true, HasWitness: true}

	tests := []struct {
		name   string
		status *downloaders.DirectoryStatus
		opts   *downloaders.DownloadOptions
		want   downloaders.Action
	}{
		{"missing dir", &downloaders.DirectoryStatus{}, nil, downloaders.ActionProceed},
		{"force merges", existing, &downloaders.DownloadOptions{Force: true, NonInteractive: true}, downloaders.ActionMerge},
		{"skip-existing merges", existing, &downloaders.DownloadOptions{SkipExisting: true, NonInteractive: true}, downloaders.ActionMerge},
		{"force without -y", existing, &downloaders.DownloadOptions{Force: true}, downloaders.ActionMerge},
		{"-y with witness", withWitness, &downloaders.DownloadOptions{NonInteractive: true}, downloaders.ActionMerge},
		{"-y without witness", existing, &downloaders.DownloadOptions{NonInteractive: true}, downloaders.ActionSkip},
		// go test stdin is not a TTY: must not prompt and die on EOF.
		{"no tty, no flags", existing, nil, downloaders.ActionSkip},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := HandleDirectoryConflicts(tt.status, tt.opts)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// Regression: witness stats describe the latest run, not a
// running total, while the file list still accumulates.
func TestMergeWitnessFilesStatsArePerRun(t *testing.T) {
	prev := &downloaders.WitnessFile{
		OriginalID:    "GSE1",
		Files:         []downloaders.FileWitness{{Path: "a"}},
		DownloadStats: &downloaders.DownloadStats{BytesTotal: 100, BytesDownloaded: 100, FilesTotal: 1, FilesDownloaded: 1},
	}
	nextStats := &downloaders.DownloadStats{BytesTotal: 0, BytesDownloaded: 0, FilesTotal: 1, FilesSkipped: 1}
	next := &downloaders.WitnessFile{OriginalID: "GSE1", DownloadStats: nextStats}

	merged := mergeWitnessFiles(prev, next)

	if *merged.DownloadStats != *nextStats {
		t.Errorf("stats = %+v, want latest run's %+v", *merged.DownloadStats, *nextStats)
	}

	if len(merged.Files) != 1 || merged.Files[0].Path != "a" {
		t.Errorf("file list should still accumulate, got %+v", merged.Files)
	}
}

func TestFormatEstimate(t *testing.T) {
	if got := FormatEstimate(0); got != "unknown" {
		t.Errorf("FormatEstimate(0) = %q, want unknown", got)
	}

	if got := FormatEstimate(2048); got != FormatBytes(2048) {
		t.Errorf("FormatEstimate(2048) = %q, want %q", got, FormatBytes(2048))
	}
}

// Regression: experimenthub without a cache and sra (md5) wrote hapiq.json
// entries without a sha256. WriteWitnessFile now fills one in, for paths
// relative to the folder and for full paths.
func TestWriteWitnessFileFillsSHA256(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const helloSHA = "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03"

	w := &downloaders.WitnessFile{OriginalID: "x", Files: []downloaders.FileWitness{
		{Path: "a.txt"}, // relative to dir
		{Path: filepath.Join(dir, "a.txt"), Checksum: "b1946ac92492d2347c6235b4d2611184", ChecksumType: "md5"},
		{Path: "missing.txt"}, // unreadable: left empty
	}}
	if err := WriteWitnessFile(dir, w); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWitnessFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{helloSHA, helloSHA, ""} {
		f := got.Files[i]
		if f.Checksum != want || (want != "" && f.ChecksumType != "sha256") {
			t.Errorf("%s: got %s:%s, want sha256:%s", f.Path, f.ChecksumType, f.Checksum, want)
		}
	}
}

// Regression: manifest get --parent-dir data recorded data/good/LICENSE (relative
// to the working directory) and sra, url and experimenthub recorded absolute
// paths. hapiq.json now records every path relative to its own folder.
func TestWriteWitnessFileRelativePaths(t *testing.T) {
	t.Chdir(t.TempDir())
	dir := filepath.Join("data", "good")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b", "c", filepath.Join("sub", "d")} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}

	// An earlier run recorded an absolute path; it must merge with "a", not duplicate it.
	prev := &downloaders.WitnessFile{OriginalID: "x", Files: []downloaders.FileWitness{{Path: filepath.Join(absDir, "a")}}}
	if err := WriteWitnessFile(dir, prev); err != nil {
		t.Fatal(err)
	}
	w := &downloaders.WitnessFile{OriginalID: "x", Files: []downloaders.FileWitness{
		{Path: filepath.Join(absDir, "a")},        // absolute
		{Path: filepath.Join(dir, "b")},           // relative to the working directory
		{Path: "c"},                               // relative to the folder
		{Path: filepath.Join(absDir, "sub", "d")}, // absolute, nested
		{Path: "/elsewhere/e"},                    // outside the folder: left as given
	}}
	if err := WriteWitnessFile(dir, w); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWitnessFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", "b", "c", filepath.Join("sub", "d"), "/elsewhere/e"}
	if len(got.Files) != len(want) {
		t.Fatalf("got %d files %v, want %v", len(got.Files), got.Files, want)
	}
	for i, f := range got.Files {
		if f.Path != want[i] {
			t.Errorf("file %d: path %q, want %q", i, f.Path, want[i])
		}
	}
}

// Regression: a run that skipped every file as already present (e.g. an
// interrupted download resumed with --skip-existing) must still list them.
func TestWriteWitnessFileAllSkipped(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const helloSHA = "5891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03"

	fi, ok := SkippedFile(filepath.Join(dir, "a.txt"), "a.txt", "https://x/a.txt")
	if !ok || !fi.Skipped || fi.Size != 6 {
		t.Fatalf("SkippedFile = %+v, %v", fi, ok)
	}
	if _, ok := SkippedFile(filepath.Join(dir, "missing"), "", ""); ok {
		t.Error("SkippedFile reported a missing file")
	}

	w := &downloaders.WitnessFile{OriginalID: "x", Files: []downloaders.FileWitness{downloaders.FileWitness(fi)}}
	if err := WriteWitnessFile(dir, w); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWitnessFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "a.txt" || got.Files[0].Checksum != helloSHA {
		t.Errorf("files = %+v, want a.txt with sha256 %s", got.Files, helloSHA)
	}
}

// A skipped file keeps the checksum an earlier run recorded instead of being
// re-hashed: the recorded value is fake, so a re-hash would replace it.
func TestWriteWitnessFileSkippedKeepsPrevChecksum(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const fake = "0000000000000000000000000000000000000000000000000000000000000000"
	prev := &downloaders.WitnessFile{OriginalID: "x", Files: []downloaders.FileWitness{
		{Path: "a.txt", Size: 6, Checksum: fake, ChecksumType: "sha256", SourceChecksum: "md5:abc"},
	}}
	if err := WriteWitnessFile(dir, prev); err != nil {
		t.Fatal(err)
	}

	fi, _ := SkippedFile(filepath.Join(dir, "a.txt"), "a.txt", "")
	next := &downloaders.WitnessFile{OriginalID: "x", Files: []downloaders.FileWitness{downloaders.FileWitness(fi)}}
	if err := WriteWitnessFile(dir, next); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWitnessFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 1 || got.Files[0].Checksum != fake || got.Files[0].SourceChecksum != "md5:abc" {
		t.Errorf("files = %+v, want prev's checksums kept", got.Files)
	}
}
