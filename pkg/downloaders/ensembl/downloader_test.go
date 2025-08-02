// Package ensembl provides test coverage for the Ensembl downloader implementation.
package ensembl

import (
	"context"
	"testing"
	"time"
)

func TestNewEnsemblDownloader(t *testing.T) {
	// Test default configuration
	d := NewEnsemblDownloader()
	if d == nil {
		t.Fatal("expected non-nil downloader")
	}

	if d.GetSourceType() != "ensembl" {
		t.Errorf("expected source type 'ensembl', got '%s'", d.GetSourceType())
	}

	if d.timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", d.timeout)
	}

	// Test with options
	d2 := NewEnsemblDownloader(
		WithTimeout(60*time.Second),
		WithVerbose(true),
	)

	if d2.timeout != 60*time.Second {
		t.Errorf("expected timeout 60s, got %v", d2.timeout)
	}

	if !d2.verbose {
		t.Error("expected verbose to be true")
	}
}

func TestEnsemblDownloader_Validate(t *testing.T) {
	d := NewEnsemblDownloader()
	ctx := context.Background()

	tests := []struct {
		name      string
		id        string
		wantValid bool
		wantError bool
	}{
		{
			name:      "valid bacteria peptides",
			id:        "bacteria:47:pep",
			wantValid: true,
			wantError: false,
		},
		{
			name:      "valid fungi with species",
			id:        "fungi:47:gff3:saccharomyces_cerevisiae",
			wantValid: true,
			wantError: false,
		},
		{
			name:      "valid plants DNA",
			id:        "plants:50:dna",
			wantValid: true,
			wantError: false,
		},
		{
			name:      "invalid database",
			id:        "invalid:47:pep",
			wantValid: false,
			wantError: false,
		},
		{
			name:      "invalid version",
			id:        "bacteria:abc:pep",
			wantValid: false,
			wantError: false,
		},
		{
			name:      "invalid content type",
			id:        "bacteria:47:invalid",
			wantValid: false,
			wantError: false,
		},
		{
			name:      "missing parts",
			id:        "bacteria:47",
			wantValid: false,
			wantError: false,
		},
		{
			name:      "empty string",
			id:        "",
			wantValid: false,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := d.Validate(ctx, tt.id)

			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if result == nil {
				t.Fatal("expected non-nil result")
			}

			if result.Valid != tt.wantValid {
				t.Errorf("Validate() valid = %v, want %v", result.Valid, tt.wantValid)
			}

			if result.SourceType != "ensembl" {
				t.Errorf("expected source type 'ensembl', got '%s'", result.SourceType)
			}

			// Check that errors are properly populated for invalid cases
			if !tt.wantValid && len(result.Errors) == 0 {
				t.Error("expected validation errors for invalid input")
			}
		})
	}
}

func TestEnsemblDownloader_cleanEnsemblID(t *testing.T) {
	d := NewEnsemblDownloader()

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "bacteria:47:pep",
			expected: "bacteria:47:pep",
		},
		{
			input:    "  FUNGI:47:GFF3  ",
			expected: "fungi:47:gff3",
		},
		{
			input:    "ensembl:plants:50:dna",
			expected: "plants:50:dna",
		},
		{
			input:    "https://bacteria:47:pep",
			expected: "bacteria:47:pep",
		},
		{
			input:    "HTTP://PROTISTS:45:CDS",
			expected: "protists:45:cds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := d.cleanEnsemblID(tt.input)
			if result != tt.expected {
				t.Errorf("cleanEnsemblID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestEnsemblDownloader_parseEnsemblID(t *testing.T) {
	d := NewEnsemblDownloader()

	tests := []struct {
		name     string
		id       string
		expected *EnsemblRequest
		wantErr  bool
	}{
		{
			name: "basic format",
			id:   "bacteria:47:pep",
			expected: &EnsemblRequest{
				Database: DatabaseBacteria,
				Version:  "47",
				Content:  ContentPeptides,
				Species:  "",
			},
			wantErr: false,
		},
		{
			name: "with species",
			id:   "fungi:47:gff3:saccharomyces_cerevisiae",
			expected: &EnsemblRequest{
				Database: DatabaseFungi,
				Version:  "47",
				Content:  ContentGFF3,
				Species:  "saccharomyces_cerevisiae",
			},
			wantErr: false,
		},
		{
			name:     "insufficient parts",
			id:       "bacteria:47",
			expected: nil,
			wantErr:  true,
		},
		{
			name:     "empty string",
			id:       "",
			expected: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := d.parseEnsemblID(tt.id)

			if (err != nil) != tt.wantErr {
				t.Errorf("parseEnsemblID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if result == nil {
				t.Fatal("expected non-nil result")
			}

			if result.Database != tt.expected.Database {
				t.Errorf("Database = %v, want %v", result.Database, tt.expected.Database)
			}

			if result.Version != tt.expected.Version {
				t.Errorf("Version = %v, want %v", result.Version, tt.expected.Version)
			}

			if result.Content != tt.expected.Content {
				t.Errorf("Content = %v, want %v", result.Content, tt.expected.Content)
			}

			if result.Species != tt.expected.Species {
				t.Errorf("Species = %v, want %v", result.Species, tt.expected.Species)
			}
		})
	}
}

func TestEnsemblDownloader_getDefaultSpeciesCount(t *testing.T) {
	d := NewEnsemblDownloader()

	tests := []struct {
		database DatabaseType
		expected int
	}{
		{DatabaseBacteria, 50000},
		{DatabaseFungi, 1000},
		{DatabaseMetazoa, 500},
		{DatabasePlants, 100},
		{DatabaseProtists, 200},
		{DatabaseType("unknown"), 100}, // default fallback
	}

	for _, tt := range tests {
		t.Run(string(tt.database), func(t *testing.T) {
			result := d.getDefaultSpeciesCount(tt.database)
			if result != tt.expected {
				t.Errorf("getDefaultSpeciesCount(%v) = %v, want %v", tt.database, result, tt.expected)
			}
		})
	}
}

func TestDatabaseType_String(t *testing.T) {
	tests := []struct {
		db       DatabaseType
		expected string
	}{
		{DatabaseBacteria, "bacteria"},
		{DatabaseFungi, "fungi"},
		{DatabaseMetazoa, "metazoa"},
		{DatabasePlants, "plants"},
		{DatabaseProtists, "protists"},
	}

	for _, tt := range tests {
		t.Run(string(tt.db), func(t *testing.T) {
			if string(tt.db) != tt.expected {
				t.Errorf("DatabaseType string = %v, want %v", string(tt.db), tt.expected)
			}
		})
	}
}

func TestContentType_String(t *testing.T) {
	tests := []struct {
		ct       ContentType
		expected string
	}{
		{ContentPeptides, "pep"},
		{ContentCDS, "cds"},
		{ContentGFF3, "gff3"},
		{ContentDNA, "dna"},
	}

	for _, tt := range tests {
		t.Run(string(tt.ct), func(t *testing.T) {
			if string(tt.ct) != tt.expected {
				t.Errorf("ContentType string = %v, want %v", string(tt.ct), tt.expected)
			}
		})
	}
}

func TestEnsemblDownloader_buildTitle(t *testing.T) {
	d := NewEnsemblDownloader()

	tests := []struct {
		name     string
		req      *EnsemblRequest
		expected string
	}{
		{
			name: "bacteria peptides",
			req: &EnsemblRequest{
				Database: DatabaseBacteria,
				Version:  "47",
				Content:  ContentPeptides,
			},
			expected: "Ensembl Bacteria Release 47 - Protein Sequences",
		},
		{
			name: "fungi with species",
			req: &EnsemblRequest{
				Database: DatabaseFungi,
				Version:  "47",
				Content:  ContentGFF3,
				Species:  "saccharomyces_cerevisiae",
			},
			expected: "Ensembl Fungi Release 47 - Genome Annotations (saccharomyces_cerevisiae)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := d.buildTitle(tt.req)
			if result != tt.expected {
				t.Errorf("buildTitle() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestEnsemblDownloader_calculateEstimates(t *testing.T) {
	d := NewEnsemblDownloader()

	tests := []struct {
		name         string
		req          *EnsemblRequest
		speciesCount int
		wantFiles    int
		wantSize     int64
	}{
		{
			name: "bacteria peptides",
			req: &EnsemblRequest{
				Database: DatabaseBacteria,
				Content:  ContentPeptides,
			},
			speciesCount: 100,
			wantFiles:    100,
			wantSize:     100 * 5 * 1024 * 1024, // 100 species * 5MB each
		},
		{
			name: "plants DNA",
			req: &EnsemblRequest{
				Database: DatabasePlants,
				Content:  ContentDNA,
			},
			speciesCount: 50,
			wantFiles:    50,
			wantSize:     50 * 500 * 1024 * 1024, // 50 species * 500MB each
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fileCount, size := d.calculateEstimates(tt.req, tt.speciesCount)

			if fileCount != tt.wantFiles {
				t.Errorf("calculateEstimates() fileCount = %v, want %v", fileCount, tt.wantFiles)
			}

			if size != tt.wantSize {
				t.Errorf("calculateEstimates() size = %v, want %v", size, tt.wantSize)
			}
		})
	}
}

// Integration test for GetMetadata (requires network access, can be skipped)
func TestEnsemblDownloader_GetMetadata_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	d := NewEnsemblDownloader(WithTimeout(10 * time.Second))
	ctx := context.Background()

	// Test with a known invalid version to avoid hitting real servers
	_, err := d.GetMetadata(ctx, "bacteria:999:pep")
	if err == nil {
		t.Error("expected error for non-existent version")
	}

	// Check that error is properly typed
	if err != nil {
		// Just check that we got an error, don't worry about the specific type
		// since it could be a network error or other wrapped error
		t.Logf("Got expected error: %v", err)
	}
}

// Benchmark tests
func BenchmarkEnsemblDownloader_Validate(b *testing.B) {
	d := NewEnsemblDownloader()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = d.Validate(ctx, "bacteria:47:pep")
	}
}

func BenchmarkEnsemblDownloader_parseEnsemblID(b *testing.B) {
	d := NewEnsemblDownloader()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = d.parseEnsemblID("bacteria:47:pep:escherichia_coli")
	}
}

func BenchmarkEnsemblDownloader_cleanEnsemblID(b *testing.B) {
	d := NewEnsemblDownloader()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = d.cleanEnsemblID("  ENSEMBL:BACTERIA:47:PEP  ")
	}
}
