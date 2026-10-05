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
