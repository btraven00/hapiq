package bio

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/btraven00/hapiq/pkg/validators/domains"
)

func TestGEOValidator_CanValidate(t *testing.T) {
	validator := NewGEOValidator()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		// Valid GEO Series IDs
		{
			name:     "valid GSE ID",
			input:    "GSE185917",
			expected: true,
		},
		{
			name:     "valid GSE ID with leading/trailing spaces",
			input:    "  GSE123456  ",
			expected: true,
		},
		{
			name:     "valid GSE ID - single digit",
			input:    "GSE1",
			expected: true,
		},
		{
			name:     "valid GSE ID - large number",
			input:    "GSE999999999",
			expected: true,
		},

		// Valid GEO Sample IDs
		{
			name:     "valid GSM ID",
			input:    "GSM1234567",
			expected: true,
		},
		{
			name:     "valid GSM ID - small number",
			input:    "GSM1",
			expected: true,
		},

		// Valid GEO Platform IDs
		{
			name:     "valid GPL ID",
			input:    "GPL570",
			expected: true,
		},
		{
			name:     "valid GPL ID - large number",
			input:    "GPL123456",
			expected: true,
		},

		// Valid GEO Dataset IDs
		{
			name:     "valid GDS ID",
			input:    "GDS1234",
			expected: true,
		},

		// Valid GEO SuperSeries Collection IDs
		{
			name:     "valid GSC ID",
			input:    "GSC1234",
			expected: true,
		},

		// Valid Genome Assembly IDs
		{
			name:     "valid GCF ID",
			input:    "GCF_000001405.39",
			expected: true,
		},
		{
			name:     "valid GCA ID",
			input:    "GCA_000001405.28",
			expected: true,
		},

		// Valid GEO URLs
		{
			name:     "GEO accession URL",
			input:    "https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE185917",
			expected: true,
		},
		{
			name:     "GEO browse URL",
			input:    "https://www.ncbi.nlm.nih.gov/geo/browse/?view=series&acc=GSE185917",
			expected: true,
		},
		{
			name:     "GEO URL with mixed case",
			input:    "https://www.ncbi.nlm.nih.gov/GEO/query/acc.cgi?acc=GSM1234567",
			expected: true,
		},

		// Invalid inputs
		{
			name:     "invalid prefix",
			input:    "XSE123456",
			expected: false,
		},
		{
			name:     "no numbers",
			input:    "GSE",
			expected: false,
		},
		{
			name:     "invalid characters",
			input:    "GSE123abc",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "non-GEO URL",
			input:    "https://www.example.com/data",
			expected: false,
		},
		{
			name:     "random text",
			input:    "this is not a GEO identifier",
			expected: false,
		},

		// Edge cases
		{
			name:     "text containing GEO ID",
			input:    "The dataset GSE185917 contains expression data",
			expected: true,
		},
		{
			name:     "URL with additional parameters",
			input:    "https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE185917&view=quick",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.CanValidate(tt.input)
			if result != tt.expected {
				t.Errorf("CanValidate(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGEOValidator_Validate(t *testing.T) {
	validator := NewGEOValidator()
	ctx := context.Background()

	tests := []struct {
		name           string
		input          string
		expectType     string
		expectSubtype  string
		expectGeoType  string
		expectTags     []string
		expectValid    bool
		expectWarnings bool
	}{
		{
			name:          "GSE Series ID",
			input:         "GSE185917",
			expectValid:   true,
			expectType:    "expression_data",
			expectSubtype: "series",
			expectGeoType: "Series",
			expectTags:    []string{"experiment", "series", "study", "ncbi", "geo", "gene_expression"},
		},
		{
			name:          "GSM Sample ID",
			input:         "GSM1234567",
			expectValid:   true,
			expectType:    "expression_data",
			expectSubtype: "sample",
			expectGeoType: "Sample",
			expectTags:    []string{"sample", "biological_sample", "ncbi", "geo", "gene_expression"},
		},
		{
			name:          "GPL Platform ID",
			input:         "GPL570",
			expectValid:   true,
			expectType:    "metadata",
			expectSubtype: "platform",
			expectGeoType: "Platform",
			expectTags:    []string{"platform", "array", "technology", "ncbi", "geo", "gene_expression"},
		},
		{
			name:          "GDS Dataset ID",
			input:         "GDS1234",
			expectValid:   true,
			expectType:    "expression_data",
			expectSubtype: "dataset",
			expectGeoType: "Dataset",
			expectTags:    []string{"curated", "dataset", "processed", "ncbi", "geo", "gene_expression"},
		},
		{
			name:          "GSC Collection ID",
			input:         "GSC1234",
			expectValid:   true,
			expectType:    "expression_data",
			expectSubtype: "collection",
			expectGeoType: "SuperSeries Collection",
			expectTags:    []string{"collection", "superseries", "ncbi", "geo", "gene_expression"},
		},
		{
			name:          "GCF Assembly ID",
			input:         "GCF_000001405.39",
			expectValid:   true,
			expectType:    "genomic_data",
			expectSubtype: "assembly",
			expectGeoType: "Genome Assembly",
			expectTags:    []string{"genome", "assembly", "reference", "ncbi", "geo", "gene_expression"},
		},
		{
			name:          "GEO URL",
			input:         "https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE185917",
			expectValid:   true,
			expectType:    "expression_data",
			expectSubtype: "series",
			expectGeoType: "Series",
			expectTags:    []string{"experiment", "series", "study", "ncbi", "geo", "gene_expression"},
		},
		{
			name:        "Invalid input",
			input:       "invalid-input",
			expectValid: false,
		},
		{
			name:        "Empty input",
			input:       "",
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validator.Validate(ctx, tt.input)
			if err != nil {
				t.Fatalf("Validate() returned error: %v", err)
			}

			if result == nil {
				t.Fatal("Validate() returned nil result")
			}

			if result.Valid != tt.expectValid {
				t.Errorf("Validate() valid = %v, want %v", result.Valid, tt.expectValid)
			}

			if !tt.expectValid {
				return // No need to check other fields for invalid results
			}

			if result.DatasetType != tt.expectType {
				t.Errorf("Validate() dataset_type = %v, want %v", result.DatasetType, tt.expectType)
			}

			if result.Subtype != tt.expectSubtype {
				t.Errorf("Validate() subtype = %v, want %v", result.Subtype, tt.expectSubtype)
			}

			if geoType, exists := result.Metadata["geo_type"]; !exists || geoType != tt.expectGeoType {
				t.Errorf("Validate() geo_type = %v, want %v", geoType, tt.expectGeoType)
			}

			// Check that all expected tags are present
			tagMap := make(map[string]bool)
			for _, tag := range result.Tags {
				tagMap[tag] = true
			}

			for _, expectedTag := range tt.expectTags {
				if !tagMap[expectedTag] {
					t.Errorf("Validate() missing expected tag: %v", expectedTag)
				}
			}

			// Check that URLs are generated
			if result.PrimaryURL == "" {
				t.Error("Validate() did not generate primary URL")
			}

			// Check confidence and likelihood
			if result.Confidence <= 0 || result.Confidence > 1 {
				t.Errorf("Validate() confidence = %v, want > 0 and <= 1", result.Confidence)
			}

			if result.Likelihood <= 0 || result.Likelihood > 1 {
				t.Errorf("Validate() likelihood = %v, want > 0 and <= 1", result.Likelihood)
			}

			// Check validation time
			if result.ValidationTime <= 0 {
				t.Error("Validate() validation_time should be > 0")
			}
		})
	}
}

func TestGEOValidator_extractGEOID(t *testing.T) {
	validator := NewGEOValidator()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "direct GSE ID",
			input:    "GSE185917",
			expected: "GSE185917",
		},
		{
			name:     "GSE ID with spaces",
			input:    "  GSE123456  ",
			expected: "GSE123456",
		},
		{
			name:     "URL with GSE ID",
			input:    "https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE185917",
			expected: "GSE185917",
		},
		{
			name:     "text containing GSE ID",
			input:    "Dataset GSE185917 contains expression profiles",
			expected: "GSE185917",
		},
		{
			name:     "GSM ID",
			input:    "GSM1234567",
			expected: "GSM1234567",
		},
		{
			name:     "GPL ID",
			input:    "GPL570",
			expected: "GPL570",
		},
		{
			name:     "GDS ID",
			input:    "GDS1234",
			expected: "GDS1234",
		},
		{
			name:     "GCF assembly ID",
			input:    "GCF_000001405.39",
			expected: "GCF_000001405.39",
		},
		{
			name:     "invalid input",
			input:    "not-a-geo-id",
			expected: "",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.extractGEOID(tt.input)
			if result != tt.expected {
				t.Errorf("extractGEOID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGEOValidator_isGEOURL(t *testing.T) {
	validator := NewGEOValidator()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid GEO accession URL",
			input:    "https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE185917",
			expected: true,
		},
		{
			name:     "valid GEO browse URL",
			input:    "https://www.ncbi.nlm.nih.gov/geo/browse/?view=series",
			expected: true,
		},
		{
			name:     "GEO FTP URL",
			input:    "ftp://ftp.ncbi.nlm.nih.gov/geo/series/GSE185nnn/GSE185917/",
			expected: true,
		},
		{
			name:     "non-GEO NCBI URL",
			input:    "https://www.ncbi.nlm.nih.gov/pubmed/12345",
			expected: false,
		},
		{
			name:     "non-NCBI URL",
			input:    "https://www.example.com/geo/data",
			expected: false,
		},
		{
			name:     "invalid URL",
			input:    "not-a-url",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.isGEOURL(tt.input)
			if result != tt.expected {
				t.Errorf("isGEOURL(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGEOValidator_generateGEOURLs(t *testing.T) {
	validator := NewGEOValidator()

	tests := []struct {
		name             string
		geoID            string
		expectPrimaryURL string
		expectAlternates int
		expectFTPURL     bool
	}{
		{
			name:             "GSE Series",
			geoID:            "GSE185917",
			expectPrimaryURL: "https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE185917",
			expectAlternates: 3, // browse series, browse samples, FTP
			expectFTPURL:     true,
		},
		{
			name:             "GSM Sample",
			geoID:            "GSM1234567",
			expectPrimaryURL: "https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSM1234567",
			expectAlternates: 2, // browse samples, FTP
			expectFTPURL:     true,
		},
		{
			name:             "GPL Platform",
			geoID:            "GPL570",
			expectPrimaryURL: "https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GPL570",
			expectAlternates: 1, // browse platforms
			expectFTPURL:     false,
		},
		{
			name:             "GDS Dataset",
			geoID:            "GDS1234",
			expectPrimaryURL: "https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GDS1234",
			expectAlternates: 0, // no specific alternates for GDS
			expectFTPURL:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &domains.DomainValidationResult{}
			validator.generateGEOURLs(tt.geoID, result)

			if result.PrimaryURL != tt.expectPrimaryURL {
				t.Errorf("generateGEOURLs() primary URL = %q, want %q",
					result.PrimaryURL, tt.expectPrimaryURL)
			}

			if len(result.AlternateURLs) != tt.expectAlternates {
				t.Errorf("generateGEOURLs() alternate URLs count = %d, want %d",
					len(result.AlternateURLs), tt.expectAlternates)
			}

			hasFTP := false
			for _, url := range result.AlternateURLs {
				if strings.HasPrefix(url, "ftp://") {
					hasFTP = true
					break
				}
			}

			if hasFTP != tt.expectFTPURL {
				t.Errorf("generateGEOURLs() has FTP URL = %v, want %v", hasFTP, tt.expectFTPURL)
			}
		})
	}
}

func TestGEOValidator_Performance(t *testing.T) {
	validator := NewGEOValidator()
	ctx := context.Background()

	// Test validation performance for non-network operations
	testCases := []struct {
		input       string
		description string
		maxDuration time.Duration
	}{
		{
			input:       "invalid-input",
			maxDuration: 100 * time.Millisecond,
			description: "invalid input should be fast",
		},
		{
			input:       "GSE185917",
			maxDuration: 5 * time.Second, // Allow time for HTTP request
			description: "valid GEO ID with HTTP check",
		},
		{
			input:       "not-a-geo-id-at-all",
			maxDuration: 50 * time.Millisecond,
			description: "clearly invalid input should be very fast",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			start := time.Now()
			_, err := validator.Validate(ctx, tc.input)
			duration := time.Since(start)

			if err != nil {
				t.Errorf("Validate(%q) returned error: %v", tc.input, err)
			}

			if duration > tc.maxDuration {
				t.Errorf("Validate(%q) took %v, expected < %v", tc.input, duration, tc.maxDuration)
			}
		})
	}
}

func TestGEOValidator_GetPatterns(t *testing.T) {
	validator := NewGEOValidator()
	patterns := validator.GetPatterns()

	if len(patterns) == 0 {
		t.Error("GetPatterns() returned no patterns")
	}

	// Check that we have the expected pattern types
	hasRegex := false
	hasURL := false

	for _, pattern := range patterns {
		switch pattern.Type {
		case domains.PatternTypeRegex:
			hasRegex = true
		case domains.PatternTypeURL:
			hasURL = true
		}

		// Each pattern should have description and examples
		if pattern.Description == "" {
			t.Errorf("Pattern %q has no description", pattern.Pattern)
		}

		if len(pattern.Examples) == 0 {
			t.Errorf("Pattern %q has no examples", pattern.Pattern)
		}
	}

	if !hasRegex {
		t.Error("GetPatterns() should include regex patterns")
	}

	if !hasURL {
		t.Error("GetPatterns() should include URL patterns")
	}
}

// Test metadata extraction.
func TestGEOValidator_Metadata(t *testing.T) {
	validator := NewGEOValidator()
	ctx := context.Background()

	result, err := validator.Validate(ctx, "GSE185917")
	if err != nil {
		t.Fatalf("Validate() returned error: %v", err)
	}

	if !result.Valid {
		t.Fatal("Expected valid result")
	}

	// Check required metadata fields
	requiredFields := []string{"geo_type", "database", "provider", "data_domain", "description"}
	for _, field := range requiredFields {
		if _, exists := result.Metadata[field]; !exists {
			t.Errorf("Missing required metadata field: %s", field)
		}
	}

	// Check that normalized ID matches input
	if result.NormalizedID != "GSE185917" {
		t.Errorf("NormalizedID = %q, want %q", result.NormalizedID, "GSE185917")
	}

	// Check that validator name and domain are correct
	if result.ValidatorName != "geo" {
		t.Errorf("ValidatorName = %q, want %q", result.ValidatorName, "geo")
	}

	if result.Domain != "bioinformatics" {
		t.Errorf("Domain = %q, want %q", result.Domain, "bioinformatics")
	}
}
