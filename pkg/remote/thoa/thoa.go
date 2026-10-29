// Package thoa is the remote adaptor for thoa's data lake, a thin wrapper
// over github.com/btraven00/thoa-go. Credentials and endpoint come from
// thoa-go's own resolution (THOA_API_KEY, THOA_API_URL, config files).
package thoa

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	thoago "github.com/btraven00/thoa-go"

	"github.com/btraven00/hapiq/pkg/remote"
)

func init() {
	remote.Register("thoa", func() (remote.Remote, error) {
		c, err := thoago.NewClient()
		if err != nil {
			return nil, err
		}
		return &store{c: c}, nil
	})
}

type store struct{ c *thoago.Client }

// ponytail: thoa-go takes no context; a cancelled run waits for the call to return.

func (s *store) Push(_ context.Context, files []remote.File) (remote.Location, error) {
	m := make(map[string]string, len(files))
	for _, f := range files {
		m[f.Path] = f.Local
	}
	// An imported dataset keeps no path map, and filenames come from whichever
	// upload first stored the content, so the lock keeps path -> file id.
	id, ctx, err := s.c.UploadContext(m)
	if err != nil {
		return remote.Location{}, mapErr(err)
	}
	if len(ctx) == 0 {
		return remote.Location{}, fmt.Errorf("thoa dataset %s: import returned no input_context", id)
	}
	return remote.Location{Type: "thoa", Endpoint: s.c.BaseURL, ID: id, Files: ctx}, nil
}

// Stat lists the recorded paths whose file is still in the dataset.
func (s *store) Stat(_ context.Context, loc remote.Location) ([]string, error) {
	if len(loc.Files) == 0 {
		return nil, fmt.Errorf("thoa dataset %s: no file ids recorded", loc.ID)
	}
	list, err := s.c.DatasetFiles(loc.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("%w: thoa dataset %s lists no files", remote.ErrNotFound, loc.ID)
	}
	have := make(map[string]bool, len(list))
	for _, f := range list {
		have[f.ID] = true
	}
	var paths []string
	for p, id := range loc.Files {
		if have[id] {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func (s *store) Fetch(_ context.Context, loc remote.Location, dest string) error {
	res, err := s.c.DownloadFiles(loc.ID, loc.Files, dest)
	if err != nil {
		return mapErr(err)
	}
	for p, e := range res.Failed {
		return fmt.Errorf("%d of the dataset's files failed, e.g. %s: %w", len(res.Failed), p, e)
	}
	return nil
}

func mapErr(err error) error {
	var api *thoago.APIError
	switch {
	case errors.As(err, &api) && api.Status == http.StatusNotFound:
		return fmt.Errorf("%w: %v", remote.ErrNotFound, err)
	case errors.As(err, &api) && (api.Status == http.StatusUnauthorized || api.Status == http.StatusForbidden):
		return fmt.Errorf("%w: %v", remote.ErrForbidden, err)
	case strings.HasSuffix(err.Error(), " not found"): // thoa-go's "dataset <id> not found" on an empty lookup
		return fmt.Errorf("%w: %v", remote.ErrNotFound, err)
	}
	return err
}
