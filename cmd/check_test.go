package cmd

import (
	"testing"
)

func TestCleanupIdentifier(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "clean DOI",
			input:    "10.5281/zenodo.123456",
			expected: "10.5281/zenodo.123456",
		},
		{
			name:     "DOI with parentheses",
			input:    "10.5281/zenodo.123456 (accessed 2023)",
			expected: "10.5281/zenodo.123456",
		},
		{
			name:     "DOI with square brackets",
			input:    "10.5281/zenodo.123456 [supplementary]",
			expected: "10.5281/zenodo.123456",
		},
		{
			name:     "DOI with curly braces",
			input:    "10.5281/zenodo.123456 {dataset}",
			expected: "10.5281/zenodo.123456",
		},
		{
			name:     "URL with comma",
			input:    "https://zenodo.org/record/123456, accessed March 2023",
			expected: "https://zenodo.org/record/123456",
		},
		{
			name:     "URL with semicolon",
			input:    "https://figshare.com/articles/123456; supplementary data",
			expected: "https://figshare.com/articles/123456",
		},
		{
			name:     "DOI with trailing punctuation",
			input:    "10.5281/zenodo.123456.",
			expected: "10.5281/zenodo.123456",
		},
		{
			name:     "URL with multiple trailing punctuation",
			input:    "https://dryad.org/resource/123456...",
			expected: "https://dryad.org/resource/123456",
		},
		{
			name:     "DOI with excess whitespace",
			input:    "  10.5281/zenodo.123456  ",
			expected: "10.5281/zenodo.123456",
		},
		{
			name:     "complex case with multiple cleanup needed",
			input:    "  https://zenodo.org/record/123456  (dataset, accessed 2023-01-01).",
			expected: "https://zenodo.org/record/123456",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only whitespace",
			input:    "   ",
			expected: "",
		},
		{
			name:     "only punctuation after cleanup",
			input:    "(accessed 2023)",
			expected: "",
		},
		{
			name:     "figshare URL with description",
			input:    "https://figshare.com/articles/dataset/Example_Dataset/12345678 [Data file]",
			expected: "https://figshare.com/articles/dataset/Example_Dataset/12345678",
		},
		{
			name:     "dryad DOI with citation info",
			input:    "10.5061/dryad.abc123, John Doe et al. (2023)",
			expected: "10.5061/dryad.abc123",
		},
		{
			name:     "nature URL with access date",
			input:    "https://www.nature.com/articles/s41467-021-23778-6 (accessed on 15 March 2023)",
			expected: "https://www.nature.com/articles/s41467-021-23778-6",
		},
		{
			name:     "DOI with internal spaces normalized",
			input:    "10.5281/  zenodo.123456",
			expected: "10.5281/ zenodo.123456",
		},
		{
			name:     "URL with query parameters kept",
			input:    "https://zenodo.org/record/123456?version=2",
			expected: "https://zenodo.org/record/123456?version=2",
		},
		{
			name:     "URL with fragment kept",
			input:    "https://github.com/user/repo#readme",
			expected: "https://github.com/user/repo#readme",
		},
		{
			name:     "DOI with version info in brackets",
			input:    "10.5281/zenodo.123456 [version 2.0]",
			expected: "10.5281/zenodo.123456",
		},
		{
			name:     "mixed brackets and punctuation",
			input:    "https://example.com/dataset/123 (primary), [backup: https://backup.com]",
			expected: "https://example.com/dataset/123",
		},
		{
			name:     "trailing single parenthesis",
			input:    "https://github.com/comprna/METEORE)",
			expected: "https://github.com/comprna/METEORE",
		},
		{
			name:     "trailing single bracket",
			input:    "https://example.com/dataset]",
			expected: "https://example.com/dataset",
		},
		{
			name:     "trailing single brace",
			input:    "https://example.com/dataset}",
			expected: "https://example.com/dataset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanupIdentifier(tt.input)
			if result != tt.expected {
				t.Errorf("cleanupIdentifier(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCleanupIdentifier_EdgeCases(t *testing.T) {
	// Test cases that might break the cleanup logic
	edgeCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "brackets at beginning",
			input:    "[Dataset] 10.5281/zenodo.123456",
			expected: "",
		},
		{
			name:     "comma at beginning",
			input:    ", 10.5281/zenodo.123456",
			expected: "",
		},
		{
			name:     "multiple brackets",
			input:    "10.5281/zenodo.123456 (version 1) [backup] {mirror}",
			expected: "10.5281/zenodo.123456",
		},
		{
			name:     "nested brackets",
			input:    "10.5281/zenodo.123456 (version [1.0] stable)",
			expected: "10.5281/zenodo.123456",
		},
		{
			name:     "URL with brackets in path",
			input:    "https://example.com/path[with]brackets/file.pdf",
			expected: "https://example.com/path",
		},
		{
			name:     "special characters",
			input:    "10.5281/zenodo.123456 © 2023",
			expected: "10.5281/zenodo.123456 © 2023",
		},
	}

	for _, tt := range edgeCases {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanupIdentifier(tt.input)
			if result != tt.expected {
				t.Errorf("cleanupIdentifier(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCleanupIdentifier_RealWorldExamples(t *testing.T) {
	// Real-world examples from academic papers
	realWorld := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "typical academic citation",
			input:    "https://doi.org/10.1038/s41467-021-23778-6 (Nature Communications, 2021)",
			expected: "https://doi.org/10.1038/s41467-021-23778-6",
		},
		{
			name:     "zenodo with version",
			input:    "10.5281/zenodo.4321098 (version 1.2.0, accessed March 15, 2023)",
			expected: "10.5281/zenodo.4321098",
		},
		{
			name:     "figshare with file description",
			input:    "https://figshare.com/articles/dataset/RNA_seq_data/12345678 [RNA sequencing data files]",
			expected: "https://figshare.com/articles/dataset/RNA_seq_data/12345678",
		},
		{
			name:     "dryad with paper info",
			input:    "10.5061/dryad.j1fd7, Smith et al. 2023",
			expected: "10.5061/dryad.j1fd7",
		},
		{
			name:     "github release",
			input:    "https://github.com/user/project/releases/tag/v1.0.0 (source code)",
			expected: "https://github.com/user/project/releases/tag/v1.0.0",
		},
		{
			name:     "bioproject with description",
			input:    "PRJNA123456 (BioProject, whole genome sequencing)",
			expected: "PRJNA123456",
		},
		{
			name:     "complex URL with multiple elements",
			input:    "https://www.ebi.ac.uk/ena/browser/view/PRJEB12345 (European Nucleotide Archive, [raw reads])",
			expected: "https://www.ebi.ac.uk/ena/browser/view/PRJEB12345",
		},
	}

	for _, tt := range realWorld {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanupIdentifier(tt.input)
			if result != tt.expected {
				t.Errorf("cleanupIdentifier(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
