package common

import (
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
