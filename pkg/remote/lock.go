package remote

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/btraven00/hapiq/pkg/manifest"
)

// Entry is one locked dataset: the key (accession + filters), the pinned file
// set, and where copies of it live.
type Entry struct {
	Accession string            `yaml:"accession"` // canonical source:id
	Filters   *manifest.Options `yaml:"filters,omitempty"`
	Digest    string            `yaml:"digest"`
	Files     []File            `yaml:"files"`
	Remotes   []Location        `yaml:"remotes,omitempty"`
}

// Key identifies an entry: accession plus canonical filters.
func (e *Entry) Key() string { return Key(e.Accession, e.Filters) }

// Key canonicalises filters (lists sorted, empty filters = none) so two
// spellings of the same selection give the same key.
func Key(accession string, f *manifest.Options) string {
	if f == nil {
		return accession
	}
	c := *f
	for _, l := range []*[]string{&c.IncludeExts, &c.ExcludeExts, &c.Subset} {
		*l = slices.Sorted(slices.Values(*l))
	}
	b, _ := yaml.Marshal(c)
	if s := string(bytes.TrimSpace(b)); s != "{}" {
		return accession + "\x00" + s
	}
	return accession
}

// Lock is the hapiq.lock file: entries sorted by key.
type Lock struct {
	Entries []Entry
}

// LoadLock reads a lockfile. A missing file is an error: a lock is only used
// when named explicitly, so a wrong path should fail loudly.
func LoadLock(path string) (*Lock, error) {
	b, err := os.ReadFile(path) // #nosec G304 -- user-named lockfile
	if err != nil {
		return nil, err
	}
	l := &Lock{}
	if err := yaml.Unmarshal(b, &l.Entries); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return l, nil
}

// LoadOrNewLock reads path, or returns an empty lock when it doesn't exist yet.
func LoadOrNewLock(path string) (*Lock, error) {
	l, err := LoadLock(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Lock{}, nil
	}
	return l, err
}

// Find returns the entry for a key, or nil.
func (l *Lock) Find(accession string, f *manifest.Options) *Entry {
	k := Key(accession, f)
	for i := range l.Entries {
		if l.Entries[i].Key() == k {
			return &l.Entries[i]
		}
	}
	return nil
}

// Put adds or replaces the entry with e's key.
func (l *Lock) Put(e Entry) {
	if old := l.Find(e.Accession, e.Filters); old != nil {
		*old = e
		return
	}
	l.Entries = append(l.Entries, e)
}

// Save writes the lock deterministically (entries by key, files by path),
// via a temp file so a crash never leaves it half-written.
func (l *Lock) Save(path string) error {
	sort.Slice(l.Entries, func(i, j int) bool { return l.Entries[i].Key() < l.Entries[j].Key() })
	for i := range l.Entries {
		fs := l.Entries[i].Files
		sort.Slice(fs, func(a, b int) bool { return fs[a].Path < fs[b].Path })
	}
	var buf bytes.Buffer
	buf.WriteString("# hapiq.lock: written by `hapiq lock add`; commit it with the plan.\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(l.Entries); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil { // #nosec G306 -- committed, not secret
		return err
	}
	return os.Rename(tmp, path)
}
