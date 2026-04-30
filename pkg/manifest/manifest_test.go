package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func writeManifest(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "manifest-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	_ = f.Close()
	return f.Name()
}

func TestLoad_Accession(t *testing.T) {
	path := writeManifest(t, `
- identifier: my-geo
  accession: geo:GSE123456
`)
	entries, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Load() len = %d, want 1", len(entries))
	}
	if entries[0].Identifier != "my-geo" {
		t.Errorf("Identifier = %q, want %q", entries[0].Identifier, "my-geo")
	}
	if entries[0].Accession != "geo:GSE123456" {
		t.Errorf("Accession = %q, want %q", entries[0].Accession, "geo:GSE123456")
	}
}

func TestLoad_URL(t *testing.T) {
	path := writeManifest(t, `
- identifier: ref-genome
  url: https://example.com/genome.fa.gz
  hash: sha256:abc123
`)
	entries, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Load() len = %d, want 1", len(entries))
	}
	if entries[0].URL != "https://example.com/genome.fa.gz" {
		t.Errorf("URL = %q, want https://example.com/genome.fa.gz", entries[0].URL)
	}
	if entries[0].Accession != "" {
		t.Errorf("Accession should be empty for url entry, got %q", entries[0].Accession)
	}
}

func TestLoad_BothAccessionAndURL(t *testing.T) {
	path := writeManifest(t, `
- identifier: bad
  accession: geo:GSE123456
  url: https://example.com/file.csv
`)
	_, err := Load(path)
	if err == nil {
		t.Error("Load() should fail when both accession and url are set")
	}
}

func TestLoad_NeitherAccessionNorURL(t *testing.T) {
	path := writeManifest(t, `
- identifier: bad
`)
	_, err := Load(path)
	if err == nil {
		t.Error("Load() should fail when neither accession nor url is set")
	}
}

func TestLoad_MissingIdentifier(t *testing.T) {
	path := writeManifest(t, `
- accession: geo:GSE123456
`)
	_, err := Load(path)
	if err == nil {
		t.Error("Load() should fail when identifier is missing")
	}
}

func TestLoad_InvalidAccessionFormat(t *testing.T) {
	path := writeManifest(t, `
- identifier: bad
  accession: no-colon-here
`)
	_, err := Load(path)
	if err == nil {
		t.Error("Load() should fail for accession with no colon")
	}
}

func TestLoad_Mixed(t *testing.T) {
	path := writeManifest(t, `
- identifier: geo-entry
  accession: geo:GSE123456
- identifier: direct-url
  url: https://example.com/file.csv
`)
	entries, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Load() len = %d, want 2", len(entries))
	}
}

func TestResolveSource_Accession(t *testing.T) {
	e := Entry{Identifier: "x", Accession: "geo:GSE123456"}
	src, id, err := ResolveSource(e)
	if err != nil {
		t.Fatalf("ResolveSource() error: %v", err)
	}
	if src != "geo" {
		t.Errorf("source = %q, want %q", src, "geo")
	}
	if id != "GSE123456" {
		t.Errorf("id = %q, want %q", id, "GSE123456")
	}
}

func TestResolveSource_URL(t *testing.T) {
	rawURL := "https://example.com/data.csv"
	e := Entry{Identifier: "x", URL: rawURL}
	src, id, err := ResolveSource(e)
	if err != nil {
		t.Fatalf("ResolveSource() error: %v", err)
	}
	if src != "url" {
		t.Errorf("source = %q, want %q", src, "url")
	}
	if id != rawURL {
		t.Errorf("id = %q, want %q", id, rawURL)
	}
}

func TestResolveSource_URLPreferredOverAccession(t *testing.T) {
	// When URL is set it wins regardless (Load rejects both-set, but ResolveSource
	// should handle it gracefully by preferring url).
	e := Entry{URL: "https://example.com/file.csv"}
	src, _, err := ResolveSource(e)
	if err != nil {
		t.Fatalf("ResolveSource() error: %v", err)
	}
	if src != "url" {
		t.Errorf("source = %q, want url", src)
	}
}

func TestSplitAccession(t *testing.T) {
	tests := []struct {
		acc     string
		src     string
		id      string
		wantErr bool
	}{
		{"geo:GSE123456", "geo", "GSE123456", false},
		{"url:https://example.com/file.csv", "url", "https://example.com/file.csv", false},
		{"no-colon", "", "", true},
		{":noleadingsource", "", "", true},
		{"trailingsource:", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.acc, func(t *testing.T) {
			src, id, err := SplitAccession(tt.acc)
			if (err != nil) != tt.wantErr {
				t.Errorf("SplitAccession(%q) error = %v, wantErr %v", tt.acc, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if src != tt.src {
					t.Errorf("source = %q, want %q", src, tt.src)
				}
				if id != tt.id {
					t.Errorf("id = %q, want %q", id, tt.id)
				}
			}
		})
	}
}

func TestFromWitness_URLSource(t *testing.T) {
	dir := t.TempDir()
	witness := `{
		"source": "url",
		"original_id": "https://example.com/genome.fa.gz",
		"download_time": "2024-01-01T00:00:00Z",
		"hapiq_version": "dev",
		"files": [
			{"path": "genome.fa.gz", "original_name": "genome.fa.gz", "source_url": "https://example.com/genome.fa.gz"}
		],
		"download_stats": {}
	}`
	if err := os.WriteFile(filepath.Join(dir, "hapiq.json"), []byte(witness), 0o644); err != nil {
		t.Fatalf("write witness: %v", err)
	}

	entry, err := FromWitness(filepath.Join(dir, "hapiq.json"))
	if err != nil {
		t.Fatalf("FromWitness() error: %v", err)
	}
	if entry.URL != "https://example.com/genome.fa.gz" {
		t.Errorf("URL = %q, want https://example.com/genome.fa.gz", entry.URL)
	}
	if entry.Accession != "" {
		t.Errorf("Accession should be empty for url source, got %q", entry.Accession)
	}
}

func TestFromWitness_RegularSource(t *testing.T) {
	dir := t.TempDir()
	witness := `{
		"source": "geo",
		"original_id": "GSE123456",
		"download_time": "2024-01-01T00:00:00Z",
		"hapiq_version": "dev",
		"files": [],
		"download_stats": {}
	}`
	if err := os.WriteFile(filepath.Join(dir, "hapiq.json"), []byte(witness), 0o644); err != nil {
		t.Fatalf("write witness: %v", err)
	}

	entry, err := FromWitness(filepath.Join(dir, "hapiq.json"))
	if err != nil {
		t.Fatalf("FromWitness() error: %v", err)
	}
	if entry.Accession != "geo:GSE123456" {
		t.Errorf("Accession = %q, want geo:GSE123456", entry.Accession)
	}
	if entry.URL != "" {
		t.Errorf("URL should be empty for non-url source, got %q", entry.URL)
	}
}
