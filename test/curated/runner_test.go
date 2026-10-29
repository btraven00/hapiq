//go:build curated

// Curated end-to-end suite: real downloads from real repositories, checked
// against facts we know independently (checksums published by the source,
// file lists, exit codes). Not run in CI. See README.md.
package curated

import (
	"crypto/md5" // #nosec G501 -- comparing to checksums published by repositories
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type Case struct {
	Name  string `yaml:"name"`
	Why   string `yaml:"why"`
	Steps []Step `yaml:"steps"`
}

// Step runs a shell line ($OUT is the case's empty directory, `hapiq` is the
// binary under test) and then checks what it left behind.
type Step struct {
	Run           string         `yaml:"run"`
	Exit          int            `yaml:"exit"`
	Fails         bool           `yaml:"fails"` // any nonzero exit
	Contains      []string       `yaml:"output_contains"`
	NotContains   []string       `yaml:"output_not_contains"`
	Files         []FileCheck    `yaml:"files"`
	Only          *[]string      `yaml:"only"` // every file under $OUT matches one of these; [] means empty
	Witness       map[string]any `yaml:"witness"`
	SkipInvariant bool           `yaml:"skip_witness_invariant"`
}

// FileCheck matches exactly one file (or none, with absent). A pattern without
// "/" matches the basename anywhere under $OUT.
type FileCheck struct {
	Path    string `yaml:"path"`
	Absent  bool   `yaml:"absent"`
	Size    *int64 `yaml:"size"`
	MinSize int64  `yaml:"min_size"`
	MD5     string `yaml:"md5"`
	SHA256  string `yaml:"sha256"`
}

var bin string

func TestMain(m *testing.M) {
	// Build from this tree, so a stale installed hapiq is never what gets tested.
	dir, err := os.MkdirTemp("", "hapiq-curated-bin")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "hapiq")
	if b := os.Getenv("HAPIQ_BIN"); b != "" {
		if bin, err = filepath.Abs(b); err != nil {
			panic(err)
		}
	} else {
		build := exec.Command("go", "build", "-o", bin, "../..")
		build.Stdout, build.Stderr = os.Stderr, os.Stderr
		if err := build.Run(); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestCurated(t *testing.T) {
	// Load and check every ID before running anything, so a typo can't waste an hour of downloads.
	var all []Case
	seen := map[string]bool{}
	suites, _ := filepath.Glob("*.yaml")
	for _, s := range suites {
		raw, err := os.ReadFile(s) // #nosec G304 -- suite files in this directory
		if err != nil {
			t.Fatal(err)
		}
		var cases []Case
		dec := yaml.NewDecoder(strings.NewReader(string(raw)))
		dec.KnownFields(true)
		if err := dec.Decode(&cases); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		prefix := strings.TrimSuffix(s, ".yaml") + "-"
		for _, c := range cases {
			if !idPattern.MatchString(c.Name) || !strings.HasPrefix(c.Name, prefix) {
				t.Fatalf("%s: case %q: name must be %sNNN-description", s, c.Name, prefix)
			}
			id := c.Name[:len(prefix)+3]
			if seen[id] {
				t.Fatalf("%s: case ID %s used twice", s, id)
			}
			seen[id] = true
		}
		all = append(all, cases...)
	}
	for _, c := range all {
		// ponytail: sequential on purpose; parallel cases would hammer the same repositories.
		t.Run(c.Name, func(t *testing.T) { runCase(t, c) })
	}
}

var idPattern = regexp.MustCompile(`^[a-z0-9]+-[0-9]{3}-[a-z0-9-]+$`)

func runCase(t *testing.T, c Case) {
	out, home := t.TempDir(), t.TempDir()
	// Fresh HOME: no ~/.hapiqrc, and the cache off so every byte comes from the source.
	if err := os.WriteFile(filepath.Join(home, ".hapiqrc"), []byte("[cache]\nmode = \"off\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "OUT="+out, "HOME="+home,
		"PATH="+filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))

	for i, s := range c.Steps {
		cmd := exec.Command("sh", "-c", s.Run)
		cmd.Env, cmd.Dir = env, out
		outb, _ := cmd.CombinedOutput()
		output := string(outb)
		code := cmd.ProcessState.ExitCode()
		fail := func(format string, a ...any) {
			t.Errorf("step %d `%s`: %s", i+1, s.Run, fmt.Sprintf(format, a...))
		}

		if s.Fails && code == 0 {
			fail("exit 0, want failure")
		} else if !s.Fails && code != s.Exit {
			fail("exit %d, want %d", code, s.Exit)
		}
		for _, want := range s.Contains {
			if !strings.Contains(output, want) {
				fail("output lacks %q", want)
			}
		}
		for _, bad := range s.NotContains {
			if strings.Contains(output, bad) {
				fail("output contains %q", bad)
			}
		}

		all := listFiles(t, out)
		for _, f := range s.Files {
			checkFile(fail, out, all, f)
		}
		if s.Only != nil {
			for _, rel := range all {
				if !matchesAny(rel, *s.Only) {
					fail("unexpected file %s", rel)
				}
			}
		}
		witnesses := matching(all, "hapiq.json")
		if len(s.Witness) > 0 {
			if len(witnesses) != 1 {
				fail("want one hapiq.json, found %d", len(witnesses))
			} else {
				checkWitness(fail, filepath.Join(out, witnesses[0]), s.Witness)
			}
		}
		if !s.SkipInvariant {
			for _, w := range witnesses {
				witnessInvariant(fail, filepath.Join(out, w))
			}
		}
		if t.Failed() {
			t.Logf("output of step %d:\n%s", i+1, tail(output, 60))
			return
		}
	}
}

// witnessInvariant: every file hapiq.json lists is on disk with the recorded
// size and sha256. This is the promise that refetch relies on.
func witnessInvariant(fail func(string, ...any), witness string) {
	var w struct {
		Files []struct {
			Path         string `json:"path"`
			Size         int64  `json:"size"`
			Checksum     string `json:"checksum"`
			ChecksumType string `json:"checksum_type"`
		} `json:"files"`
	}
	raw, err := os.ReadFile(witness) // #nosec G304 -- written by the run under test
	if err == nil {
		err = json.Unmarshal(raw, &w)
	}
	if err != nil {
		fail("read %s: %v", witness, err)
		return
	}
	dir := filepath.Dir(witness)
	for _, f := range w.Files {
		// ponytail: some sources record absolute paths; accept both until hapiq.json settles on relative.
		p := f.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		st, err := os.Stat(p)
		switch {
		case err != nil:
			fail("hapiq.json lists %s, not on disk", f.Path)
		case st.Size() != f.Size:
			fail("%s: %d bytes on disk, hapiq.json says %d", f.Path, st.Size(), f.Size)
		case f.ChecksumType == "":
			fail("%s: no checksum in hapiq.json", f.Path)
		case f.ChecksumType == "sha256" && sum(p, sha256.New()) != f.Checksum:
			fail("%s: sha256 on disk differs from hapiq.json", f.Path)
		}
	}
}

func checkFile(fail func(string, ...any), out string, all []string, f FileCheck) {
	got := matching(all, f.Path)
	if f.Absent {
		if len(got) > 0 {
			fail("%s should be absent, found %v", f.Path, got)
		}
		return
	}
	if len(got) != 1 {
		fail("%s: want exactly one match, found %v", f.Path, got)
		return
	}
	p := filepath.Join(out, got[0])
	st, err := os.Stat(p)
	if err != nil {
		fail("%v", err)
		return
	}
	if f.Size != nil && st.Size() != *f.Size {
		fail("%s: size %d, want %d", got[0], st.Size(), *f.Size)
	}
	if st.Size() < f.MinSize {
		fail("%s: size %d, want at least %d", got[0], st.Size(), f.MinSize)
	}
	if f.MD5 != "" && sum(p, md5.New()) != f.MD5 { // #nosec G401
		fail("%s: md5 mismatch", got[0])
	}
	if f.SHA256 != "" && sum(p, sha256.New()) != f.SHA256 {
		fail("%s: sha256 mismatch", got[0])
	}
}

// checkWitness compares dotted paths into hapiq.json; "files.#" is a length.
func checkWitness(fail func(string, ...any), witness string, want map[string]any) {
	raw, err := os.ReadFile(witness) // #nosec G304 -- written by the run under test
	var doc any
	if err == nil {
		err = json.Unmarshal(raw, &doc)
	}
	if err != nil {
		fail("read %s: %v", witness, err)
		return
	}
	for key, exp := range want {
		got, ok := lookup(doc, key)
		if !ok {
			fail("hapiq.json: no %s", key)
		} else if fmt.Sprint(got) != fmt.Sprint(exp) {
			fail("hapiq.json %s = %v, want %v", key, got, exp)
		}
	}
}

func lookup(v any, key string) (any, bool) {
	for _, part := range strings.Split(key, ".") {
		switch x := v.(type) {
		case map[string]any:
			if v = x[part]; v == nil {
				return nil, false
			}
		case []any:
			if part == "#" {
				return len(x), true
			}
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			v = x[i]
		default:
			return nil, false
		}
	}
	return v, true
}

func listFiles(t *testing.T, root string) []string {
	var files []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func matching(all []string, pattern string) []string {
	var got []string
	for _, rel := range all {
		if matchesAny(rel, []string{pattern}) {
			got = append(got, rel)
		}
	}
	return got
}

func matchesAny(rel string, patterns []string) bool {
	for _, p := range patterns {
		target := rel
		if !strings.Contains(p, "/") {
			target = path.Base(rel)
		}
		if ok, _ := path.Match(p, target); ok {
			return true
		}
	}
	return false
}

func sum(p string, h hash.Hash) string {
	f, err := os.Open(p) // #nosec G304 -- files under the case's temp dir
	if err != nil {
		return ""
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func tail(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
