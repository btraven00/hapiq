// Package manifest defines a YAML-based DSL for declaring datasets to download
// and verify in bulk. A manifest lists entries; each entry maps a canonical
// accession (compact "source:id" form) to a folder identifier and an optional
// list of expected files with hashes for post-download verification.
package manifest

import (
	"bytes"
	"crypto/md5"  // #nosec G501 -- used only for upstream checksum verification
	"crypto/sha1" // #nosec G505 -- used only for upstream checksum verification
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

// Entry declares one dataset to download into <parent>/<identifier>.
type Entry struct {
	Identifier string   `yaml:"identifier"`
	Accession  string   `yaml:"accession,omitempty"`
	URL        string   `yaml:"url,omitempty"`
	Hash       string   `yaml:"hash,omitempty"`
	Files      []File   `yaml:"files,omitempty"`
	Options    *Options `yaml:"options,omitempty"`
}

// File names a specific downloaded file (relative to the entry folder) and
// its expected hash in "<algo>:<hex>" form (e.g. md5:abc123).
type File struct {
	Name string `yaml:"name"`
	Hash string `yaml:"hash,omitempty"`
}

// Options is a per-entry subset of downloader options.
type Options struct {
	IncludeExts          []string `yaml:"include_ext,omitempty"`
	ExcludeExts          []string `yaml:"exclude_ext,omitempty"`
	MaxFileSize          string   `yaml:"max_file_size,omitempty"`
	FilenameGlob         string   `yaml:"filename_pattern,omitempty"`
	Subset               []string `yaml:"subset,omitempty"`
	Organism             string   `yaml:"organism,omitempty"`
	ExcludeRaw           bool     `yaml:"exclude_raw,omitempty"`
	ExcludeSupplementary bool     `yaml:"exclude_supplementary,omitempty"`
	IncludeSRA           bool     `yaml:"include_sra,omitempty"`
	LimitFiles           int      `yaml:"limit_files,omitempty"`
}

// Load parses a manifest YAML file from disk. The top-level document is a
// sequence of entries. Unknown fields are rejected to keep the schema flat
// and explicit.
func Load(path string) ([]Entry, error) {
	data, err := os.ReadFile(filepath.Clean(path)) // #nosec G304 -- path is user-supplied manifest file
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var entries []Entry
	if err := dec.Decode(&entries); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	for i, e := range entries {
		if e.Identifier == "" {
			return nil, fmt.Errorf("entry[%d]: missing identifier", i)
		}
		if e.Accession != "" && e.URL != "" {
			return nil, fmt.Errorf("entry[%d] %q: set either 'accession' or 'url', not both", i, e.Identifier)
		}
		if e.Accession == "" && e.URL == "" {
			return nil, fmt.Errorf("entry[%d] %q: missing accession or url", i, e.Identifier)
		}
		if e.Accession != "" {
			if _, _, err := SplitAccession(e.Accession); err != nil {
				return nil, fmt.Errorf("entry[%d] %q: %w", i, e.Identifier, err)
			}
		}
	}
	return entries, nil
}

// ResolveSource returns (source, id) for an entry, handling both the
// "accession: source:id" form and the "url: https://..." shorthand.
func ResolveSource(e Entry) (source, id string, err error) {
	if e.URL != "" {
		return "url", e.URL, nil
	}
	return SplitAccession(e.Accession)
}

// SplitAccession parses "source:id" into its parts.
func SplitAccession(acc string) (source, id string, err error) {
	idx := strings.Index(acc, ":")
	if idx <= 0 || idx == len(acc)-1 {
		return "", "", fmt.Errorf("accession %q must be in form source:id", acc)
	}
	return acc[:idx], acc[idx+1:], nil
}

// FromWitness builds a manifest entry from a hapiq.json witness file. The
// identifier defaults to the basename of the directory containing the witness.
func FromWitness(witnessPath string) (*Entry, error) {
	dir := filepath.Dir(witnessPath)
	w, err := readWitness(witnessPath)
	if err != nil {
		return nil, err
	}
	if w.Source == "" || w.OriginalID == "" {
		return nil, fmt.Errorf("witness file missing source/original_id")
	}
	entry := &Entry{
		Identifier: filepath.Base(dir),
	}
	if w.Source == "url" {
		entry.URL = w.OriginalID
	} else {
		entry.Accession = fmt.Sprintf("%s:%s", w.Source, w.OriginalID)
	}
	for _, f := range w.Files {
		name := f.Path
		if name == "" {
			name = f.OriginalName
		}
		name = strings.TrimPrefix(name, dir+string(filepath.Separator))
		entry.Files = append(entry.Files, File{
			Name: name,
			Hash: combineHash(f.ChecksumType, f.Checksum),
		})
	}
	return entry, nil
}

// RenderEntry emits a YAML snippet for a single entry as one item of a
// top-level sequence (suitable for appending to a manifest file).
func RenderEntry(e *Entry) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode([]Entry{*e}); err != nil {
		return "", err
	}
	_ = enc.Close()
	return buf.String(), nil
}

// VerifyFile checks a file at path against an "<algo>:<hex>" hash spec. An
// empty spec is a no-op (returns nil). Unknown algorithms return an error.
func VerifyFile(path, spec string) error {
	if spec == "" {
		return nil
	}
	algo, want, ok := strings.Cut(spec, ":")
	if !ok || want == "" {
		return fmt.Errorf("invalid hash spec %q (want algo:hex)", spec)
	}
	var h hash.Hash
	switch strings.ToLower(algo) {
	case "md5":
		h = md5.New() // #nosec G401 -- checksum verification, not security
	case "sha1":
		h = sha1.New() // #nosec G401 -- checksum verification, not security
	case "sha256":
		h = sha256.New()
	default:
		return fmt.Errorf("unsupported hash algorithm %q", algo)
	}
	f, err := os.Open(filepath.Clean(path)) // #nosec G304 -- user-specified file to verify
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("hash mismatch for %s: want %s:%s, got %s:%s",
			path, algo, want, algo, got)
	}
	return nil
}

func combineHash(algo, sum string) string {
	if sum == "" {
		return ""
	}
	if algo == "" {
		algo = "md5"
	}
	return fmt.Sprintf("%s:%s", strings.ToLower(algo), sum)
}

func readWitness(path string) (*downloaders.WitnessFile, error) {
	data, err := os.ReadFile(filepath.Clean(path)) // #nosec G304 -- user-specified witness file
	if err != nil {
		return nil, fmt.Errorf("read witness: %w", err)
	}
	var w downloaders.WitnessFile
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("parse witness: %w", err)
	}
	return &w, nil
}
