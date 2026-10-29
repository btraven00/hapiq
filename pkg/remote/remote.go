// Package remote pushes fetched datasets to stores other than their upstream
// repository (thoa's data lake first) and fetches them back, verified against
// the sha256 recorded at push time. Design: docs/mirrors.md.
package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"golang.org/x/text/unicode/norm"
)

// File is one file of a set: its path inside the dataset and its hashes.
type File struct {
	Path   string `yaml:"path"` // relative to the folder holding hapiq.json, slash-separated
	Size   int64  `yaml:"size"`
	SHA256 string `yaml:"sha256"`
	MD5    string `yaml:"md5,omitempty"`
	Local  string `yaml:"-"` // file to read when pushing
}

// Location is where a pushed file set landed.
type Location struct {
	Type     string `yaml:"type"`     // adaptor type, e.g. "thoa"
	Endpoint string `yaml:"endpoint"` // store API base URL
	ID       string `yaml:"dataset"`  // store-native id
	// Files maps dataset path -> the store's file id, for stores that keep no
	// path map of their own (a thoa import).
	Files map[string]string `yaml:"files,omitempty"`
}

func (l Location) String() string { return l.Type + ":" + l.ID }

// Remote is implemented once per store type.
type Remote interface {
	// Push uploads files, keeping each File.Path as its path in the store.
	Push(ctx context.Context, files []File) (Location, error)
	// Stat lists the paths the store holds at loc; ErrNotFound if it is gone.
	Stat(ctx context.Context, loc Location) ([]string, error)
	// Fetch downloads loc into dest, preserving paths. The caller verifies.
	Fetch(ctx context.Context, loc Location, dest string) error
}

var (
	ErrNotFound  = errors.New("remote: location not found")
	ErrForbidden = errors.New("remote: no access")
)

// Factory builds a Remote from the environment (credentials, endpoint).
type Factory func() (Remote, error)

var (
	mu        sync.Mutex
	factories = map[string]Factory{}
)

// Register makes a store type available to Open.
func Register(typ string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	factories[typ] = f
}

// Open builds the Remote for a store type.
// ponytail: one remote per type, configured from env; named [remotes.<name>]
// config sections when someone needs two remotes of the same type.
func Open(typ string) (Remote, error) {
	mu.Lock()
	f, ok := factories[typ]
	mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown remote type %q", typ)
	}
	return f()
}

// Digest identifies a file set: sha256 over sorted "<sha256>  <path>\n"
// lines (sha256sum format). See docs/mirrors.md for the exact definition.
func Digest(files []File) (string, error) {
	if len(files) == 0 {
		return "", errors.New("empty file set")
	}
	lines := make([]string, 0, len(files))
	seen := map[string]string{}
	for _, f := range files {
		if err := checkPath(f.Path); err != nil {
			return "", err
		}
		fold := strings.ToLower(f.Path)
		if prev, ok := seen[fold]; ok {
			return "", fmt.Errorf("paths %q and %q collide", prev, f.Path)
		}
		seen[fold] = f.Path
		if len(f.SHA256) != 64 {
			return "", fmt.Errorf("%s: no sha256", f.Path)
		}
		lines = append(lines, strings.ToLower(f.SHA256)+"  "+f.Path+"\n")
	}
	// Sorting whole lines would order by hash; the definition orders by path.
	sort.Slice(lines, func(i, j int) bool { return lines[i][66:] < lines[j][66:] })
	h := sha256.New()
	for _, l := range lines {
		h.Write([]byte(l))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func checkPath(p string) error {
	switch {
	case p == "" || strings.HasPrefix(p, "/") || path.Clean(p) != p || p == "." || p == ".." || strings.HasPrefix(p, "../"):
		return fmt.Errorf("path %q: must be relative and clean", p)
	case strings.ContainsAny(p, "\n\r\\"):
		return fmt.Errorf("path %q: contains a newline or backslash", p)
	case !norm.NFC.IsNormalString(p):
		return fmt.Errorf("path %q: not NFC-normalised UTF-8", p)
	}
	return nil
}

// Verify checks that every file is in dir with its recorded sha256.
func Verify(dir string, files []File) error {
	for _, f := range files {
		got, err := sha256File(filepath.Join(dir, filepath.FromSlash(f.Path)))
		if err != nil {
			return err
		}
		if got != strings.ToLower(f.SHA256) {
			return fmt.Errorf("%s: sha256 %s, recorded %s", f.Path, got, f.SHA256)
		}
	}
	return nil
}

func sha256File(p string) (string, error) {
	fh, err := os.Open(p) // #nosec G304 -- dataset file under the output dir
	if err != nil {
		return "", err
	}
	defer func() { _ = fh.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, fh); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
