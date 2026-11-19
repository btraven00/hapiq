package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// --connections is on every downloading command, and each refuses a value
// below 1 before doing any work.
func TestConnectionsFlag(t *testing.T) {
	for _, c := range []*cobra.Command{downloadCmd, manifestGetCmd, refetchCmd} {
		if c.Flags().Lookup("connections") == nil {
			t.Errorf("%s has no --connections flag", c.Name())
		}
	}

	defer func(n int) { downloadConnections = n }(downloadConnections)
	downloadConnections = 0
	if _, err := withConnections(context.Background()); err == nil {
		t.Error("--connections 0 accepted")
	}
	runs := map[string]func() error{
		"download":     func() error { return runDownload(nil, []string{"url", "https://example.org/x"}) },
		"manifest get": func() error { return runManifestGet(nil, []string{"no-such-manifest.yaml"}) },
		"refetch":      func() error { return runRefetch(nil, []string{"no-such-dir"}) },
	}
	for name, run := range runs {
		if err := run(); err == nil || !strings.Contains(err.Error(), "--connections") {
			t.Errorf("%s with --connections 0: err=%v", name, err)
		}
	}

	downloadConnections = 3
	if _, err := withConnections(context.Background()); err != nil {
		t.Errorf("--connections 3: %v", err)
	}
}
