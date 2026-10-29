package remote

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/btraven00/hapiq/pkg/manifest"
)

const (
	shaAAA   = "9834876dcfb05cb167a5c24953eba58c4ac89b1adf57f28f2f9d09af107ee8f0" // sha256("aaa")
	shaEmpty = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

// The vector was computed with coreutils, not with this code:
//
//	printf '%s  %s\n' $EMPTY b/empty $AAA a.txt | sort -k2 | sha256sum
func TestDigestVector(t *testing.T) {
	files := []File{{Path: "b/empty", SHA256: shaEmpty}, {Path: "a.txt", SHA256: shaAAA}}
	got, err := Digest(files)
	if err != nil {
		t.Fatal(err)
	}
	if want := "7e351181d09fd33ebec455358b025a612e1cae0b8cf291dc3a3e41b12f1fe9be"; got != want {
		t.Fatalf("digest = %s, want %s", got, want)
	}
}

func TestDigestRejects(t *testing.T) {
	for name, files := range map[string][]File{
		"empty set":      nil,
		"absolute":       {{Path: "/a", SHA256: shaAAA}},
		"dotdot":         {{Path: "../a", SHA256: shaAAA}},
		"unclean":        {{Path: "a//b", SHA256: shaAAA}},
		"newline":        {{Path: "a\nb", SHA256: shaAAA}},
		"case collision": {{Path: "A", SHA256: shaAAA}, {Path: "a", SHA256: shaAAA}},
		"no sha256":      {{Path: "a"}},
		"NFD":            {{Path: "café", SHA256: shaAAA}},
	} {
		if _, err := Digest(files); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestKeyCanonical(t *testing.T) {
	a := Key("geo:GSE1", &manifest.Options{IncludeExts: []string{".h5ad", ".csv"}})
	b := Key("geo:GSE1", &manifest.Options{IncludeExts: []string{".csv", ".h5ad"}})
	if a != b {
		t.Errorf("list order changes the key: %q vs %q", a, b)
	}
	if Key("geo:GSE1", &manifest.Options{}) != Key("geo:GSE1", nil) {
		t.Error("empty filters and no filters give different keys")
	}
	if a == Key("geo:GSE1", nil) {
		t.Error("filters don't change the key")
	}
}

func TestLockRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hapiq.lock")
	l, err := LoadOrNewLock(p)
	if err != nil {
		t.Fatal(err)
	}
	f := &manifest.Options{IncludeExts: []string{".h5ad"}}
	l.Put(Entry{Accession: "geo:GSE2", Digest: "d2", Files: []File{{Path: "x", SHA256: shaAAA}}})
	l.Put(Entry{Accession: "geo:GSE1", Filters: f, Digest: "d1", Remotes: []Location{{Type: "thoa", Endpoint: "https://api.thoa.io", ID: "ds-1"}}})
	l.Put(Entry{Accession: "geo:GSE1", Filters: f, Digest: "d1b"}) // replaces
	if err := l.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := LoadLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 2 || got.Entries[0].Accession != "geo:GSE1" {
		t.Fatalf("entries = %+v", got.Entries)
	}
	if e := got.Find("geo:GSE1", &manifest.Options{IncludeExts: []string{".h5ad"}}); e == nil || e.Digest != "d1b" {
		t.Fatalf("Find = %+v", e)
	}
	if got.Find("geo:GSE1", nil) != nil {
		t.Fatal("found an entry under different filters")
	}
	if _, err := LoadLock(filepath.Join(t.TempDir(), "missing.lock")); err == nil {
		t.Fatal("a missing named lockfile must be an error")
	}
}

func TestEntryFromDirAndVerify(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aaa"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "b", "empty"), nil, 0o644)
	witness := `{"source":"zenodo","original_id":"123","options":{"include_raw":true,"filename_glob":"*.txt"},
	  "files":[{"path":"a.txt","size":3,"checksum":"` + shaAAA + `","checksum_type":"sha256"},
	           {"path":"b/empty","size":0,"checksum":"` + shaEmpty + `","checksum_type":"sha256"}]}`
	_ = os.WriteFile(filepath.Join(dir, "hapiq.json"), []byte(witness), 0o644)

	e, err := EntryFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if e.Accession != "zenodo:123" || e.Filters == nil || e.Filters.FilenameGlob != "*.txt" {
		t.Errorf("entry = %+v filters %+v", e, e.Filters)
	}
	if e.Digest != "7e351181d09fd33ebec455358b025a612e1cae0b8cf291dc3a3e41b12f1fe9be" {
		t.Errorf("digest = %s", e.Digest)
	}

	// Fetch into a fresh folder through a fake remote, then verify.
	var r Remote = fakeRemote{src: dir}
	loc, _ := r.Push(context.Background(), e.Files)
	dest := t.TempDir()
	if err := r.Fetch(context.Background(), loc, dest); err != nil {
		t.Fatal(err)
	}
	if err := Verify(dest, e.Files); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dest, "a.txt"), []byte("aab"), 0o644)
	if err := Verify(dest, e.Files); err == nil {
		t.Fatal("a changed file passed verification")
	}
}

// fakeRemote "stores" files by copying them back from the folder they came from.
type fakeRemote struct{ src string }

func (f fakeRemote) Push(_ context.Context, _ []File) (Location, error) {
	return Location{Type: "fake", ID: "1"}, nil
}

func (f fakeRemote) Stat(context.Context, Location) ([]string, error) { return nil, nil }

func (f fakeRemote) Fetch(_ context.Context, _ Location, dest string) error {
	return filepath.Walk(f.src, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || info.Name() == "hapiq.json" {
			return err
		}
		rel, _ := filepath.Rel(f.src, p)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out := filepath.Join(dest, rel)
		_ = os.MkdirAll(filepath.Dir(out), 0o755)
		return os.WriteFile(out, b, 0o644)
	})
}
