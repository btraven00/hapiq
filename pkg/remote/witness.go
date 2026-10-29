package remote

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/btraven00/hapiq/pkg/downloaders/common"
	"github.com/btraven00/hapiq/pkg/manifest"
)

// EntryFromDir builds an unpushed lock entry from a download folder's
// hapiq.json: accession, filters, and the file set with sha256 per file.
func EntryFromDir(dir string) (*Entry, error) {
	w, err := common.LoadWitnessFile(dir)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	if len(w.Datasets) > 0 {
		return nil, fmt.Errorf("%s holds more than one accession; download each into its own folder", dir)
	}
	e := &Entry{
		Accession: w.Source + ":" + w.OriginalID,
		Filters:   manifest.FiltersFromDownload(w.Options),
	}
	for _, f := range w.Files {
		if f.ChecksumType != "sha256" || f.Checksum == "" {
			return nil, fmt.Errorf("%s: %s has no sha256 in hapiq.json", dir, f.Path)
		}
		if filepath.IsAbs(f.Path) {
			return nil, fmt.Errorf("%s: %s is absolute in hapiq.json; re-download with this hapiq version", dir, f.Path)
		}
		local := filepath.Join(dir, f.Path)
		st, err := os.Stat(local)
		if err != nil {
			return nil, err
		}
		if st.Size() != f.Size {
			return nil, fmt.Errorf("%s: size %d on disk, %d in hapiq.json", local, st.Size(), f.Size)
		}
		e.Files = append(e.Files, File{Path: filepath.ToSlash(f.Path), Size: f.Size, SHA256: f.Checksum, Local: local})
	}
	if e.Digest, err = Digest(e.Files); err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	return e, nil
}
