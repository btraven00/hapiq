package accessions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewSRAValidator(t *testing.T) {
	validator := NewSRAValidator()

	if validator == nil {
		t.Fatal("NewSRAValidator() returned nil")
	}

	if validator.Name() != "sra" {
		t.Errorf("Name() = %q, expected %q", validator.Name(), "sra")
	}

	if validator.Domain() != "bioinformatics" {
		t.Errorf("Domain() = %q, expected %q", validator.Domain(), "bioinformatics")
	}

	if validator.Priority() != 95 {
		t.Errorf("Priority() = %d, expected %d", validator.Priority(), 95)
	}

	// Check that patterns are loaded
	patterns := validator.GetPatterns()
	if len(patterns) == 0 {
		t.Error("No patterns loaded in SRA validator")
	}

	// Verify supported accession types
	supportedTypes := validator.GetSupportedAccessionTypes()
	expectedTypes := []AccessionType{
		ProjectBioProject,
		StudySRA,
		SampleSRA,
		ExperimentSRA,
		RunSRA,
	}

	if len(supportedTypes) != len(expectedTypes) {
		t.Errorf("GetSupportedAccessionTypes() returned %d types, expected %d", len(supportedTypes), len(expectedTypes))
	}

	for _, expectedType := range expectedTypes {
		found := false
		for _, supportedType := range supportedTypes {
			if supportedType == expectedType {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected accession type %q not found in supported types", expectedType)
		}
	}
}

func TestSRAValidator_CanValidate(t *testing.T) {
	validator := NewSRAValidator()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		// Valid SRA accessions
		{
			name:     "Valid SRR accession",
			input:    "SRR123456",
			expected: true,
		},
		{
			name:     "Valid ERR accession",
			input:    "ERR1234567",
			expected: true,
		},
		{
			name:     "Valid DRR accession",
			input:    "DRR123456",
			expected: true,
		},
		{
			name:     "Valid SRX accession",
			input:    "SRX123456",
			expected: true,
		},
		{
			name:     "Valid ERX accession",
			input:    "ERX1234567",
			expected: true,
		},
		{
			name:     "Valid SRS accession",
			input:    "SRS123456",
			expected: true,
		},
		{
			name:     "Valid ERS accession",
			input:    "ERS1234567",
			expected: true,
		},
		{
			name:     "Valid SRP accession",
			input:    "SRP123456",
			expected: true,
		},
		{
			name:     "Valid ERP accession",
			input:    "ERP123456",
			expected: true,
		},
		{
			name:     "Valid PRJNA accession",
			input:    "PRJNA123456",
			expected: true,
		},
		{
			name:     "Valid PRJEB accession",
			input:    "PRJEB123456",
			expected: true,
		},
		{
			name:     "Valid PRJDB accession",
			input:    "PRJDB123456",
			expected: true,
		},

		// SRA URLs
		{
			name:     "NCBI SRA URL",
			input:    "https://www.ncbi.nlm.nih.gov/sra/SRR123456",
			expected: true,
		},
		{
			name:     "ENA browser URL",
			input:    "https://www.ebi.ac.uk/ena/browser/view/ERR123456",
			expected: true,
		},
		{
			name:     "NCBI SRA search URL",
			input:    "https://www.ncbi.nlm.nih.gov/sra?term=SRP123456",
			expected: true,
		},
		{
			name:     "Trace archive URL",
			input:    "https://trace.ncbi.nlm.nih.gov/Traces/sra/?run=SRR123456",
			expected: true,
		},

		// Case insensitive
		{
			name:     "Lowercase SRR",
			input:    "srr123456",
			expected: true,
		},

		// Invalid inputs
		{
			name:     "GSA accession (not SRA)",
			input:    "CRR123456",
			expected: false,
		},
		{
			name:     "GEO accession (not SRA)",
			input:    "GSE123456",
			expected: false,
		},
		{
			name:     "Invalid accession",
			input:    "INVALID123",
			expected: false,
		},
		{
			name:     "Empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "Non-SRA URL",
			input:    "https://www.example.com/data",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.CanValidate(tt.input)
			if result != tt.expected {
				t.Errorf("CanValidate(%q) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSRAValidator_Validate(t *testing.T) {
	validator := NewSRAValidator()
	ctx := context.Background()

	tests := []struct {
		name        string
		input       string
		expectType  AccessionType
		expectValid bool
		expectURLs  bool
		expectError bool
	}{
		{
			name:        "Valid SRR accession",
			input:       "SRR123456",
			expectValid: true,
			expectType:  RunSRA,
			expectURLs:  true,
			expectError: false,
		},
		{
			name:        "Valid ERX accession",
			input:       "ERX1234567",
			expectValid: true,
			expectType:  ExperimentSRA,
			expectURLs:  true,
			expectError: false,
		},
		{
			name:        "Valid SRP accession",
			input:       "SRP123456",
			expectValid: true,
			expectType:  StudySRA,
			expectURLs:  true,
			expectError: false,
		},
		{
			name:        "Valid PRJNA accession",
			input:       "PRJNA123456",
			expectValid: true,
			expectType:  ProjectBioProject,
			expectURLs:  true,
			expectError: false,
		},
		{
			name:        "SRA URL with accession",
			input:       "https://www.ncbi.nlm.nih.gov/sra/SRR123456",
			expectValid: true,
			expectType:  RunSRA,
			expectURLs:  true,
			expectError: false,
		},
		{
			name:        "Invalid accession",
			input:       "INVALID123",
			expectValid: false,
			expectURLs:  false,
			expectError: false,
		},
		{
			name:        "Empty input",
			input:       "",
			expectValid: false,
			expectURLs:  false,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validator.Validate(ctx, tt.input)

			if (err != nil) != tt.expectError {
				t.Errorf("Validate() error = %v, expectError %v", err, tt.expectError)
				return
			}

			if result == nil {
				t.Fatal("Validate() returned nil result")
			}

			if result.Valid != tt.expectValid {
				t.Errorf("Validate() Valid = %v, expected %v", result.Valid, tt.expectValid)
			}

			if tt.expectValid {
				if result.NormalizedID == "" {
					t.Error("Expected NormalizedID to be set for valid accession")
				}

				if result.DatasetType == "" {
					t.Error("Expected DatasetType to be set for valid accession")
				}

				if accType, exists := result.Metadata["accession_type"]; exists {
					if AccessionType(accType) != tt.expectType {
						t.Errorf("Expected accession_type %q, got %q", tt.expectType, accType)
					}
				} else {
					t.Error("Expected accession_type in metadata")
				}

				if tt.expectURLs {
					if result.PrimaryURL == "" {
						t.Error("Expected PrimaryURL to be set")
					}

					if len(result.AlternateURLs) == 0 {
						t.Error("Expected AlternateURLs to be set")
					}
				}

				if result.Confidence <= 0 || result.Confidence > 1 {
					t.Errorf("Confidence should be between 0 and 1, got %f", result.Confidence)
				}

				if result.Likelihood <= 0 || result.Likelihood > 1 {
					t.Errorf("Likelihood should be between 0 and 1, got %f", result.Likelihood)
				}
			}

			// Check that timing information is recorded
			if result.ValidationTime <= 0 {
				t.Error("Expected ValidationTime to be positive")
			}

			// Verify basic metadata
			if result.ValidatorName != "sra" {
				t.Errorf("ValidatorName = %q, expected %q", result.ValidatorName, "sra")
			}

			if result.Domain != "bioinformatics" {
				t.Errorf("Domain = %q, expected %q", result.Domain, "bioinformatics")
			}
		})
	}
}

func TestSRAValidator_URLGeneration(t *testing.T) {
	validator := NewSRAValidator()

	tests := []struct {
		name          string
		accessionID   string
		accType       AccessionType
		expectPrimary bool
		expectAlts    bool
	}{
		{
			name:          "SRR run URLs",
			accessionID:   "SRR123456",
			accType:       RunSRA,
			expectPrimary: true,
			expectAlts:    true,
		},
		{
			name:          "SRX experiment URLs",
			accessionID:   "SRX123456",
			accType:       ExperimentSRA,
			expectPrimary: true,
			expectAlts:    true,
		},
		{
			name:          "SRS sample URLs",
			accessionID:   "SRS123456",
			accType:       SampleSRA,
			expectPrimary: true,
			expectAlts:    true,
		},
		{
			name:          "SRP study URLs",
			accessionID:   "SRP123456",
			accType:       StudySRA,
			expectPrimary: true,
			expectAlts:    true,
		},
		{
			name:          "PRJNA project URLs",
			accessionID:   "PRJNA123456",
			accType:       ProjectBioProject,
			expectPrimary: true,
			expectAlts:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Find the pattern for this accession type
			var pattern *AccessionPattern
			for _, p := range AccessionPatterns {
				if p.Type == tt.accType {
					pattern = &p
					break
				}
			}

			if pattern == nil {
				t.Fatalf("Could not find pattern for accession type %q", tt.accType)
			}

			primary, alternates := validator.GenerateURLs(tt.accessionID, pattern)

			if tt.expectPrimary && primary == "" {
				t.Error("Expected primary URL to be generated")
			}

			if tt.expectAlts && len(alternates) == 0 {
				t.Error("Expected alternate URLs to be generated")
			}

			// Verify URLs are valid
			if primary != "" && !isValidURL(primary) {
				t.Errorf("Primary URL is not valid: %q", primary)
			}

			for _, alt := range alternates {
				if !isValidURL(alt) {
					t.Errorf("Alternate URL is not valid: %q", alt)
				}
			}

			// Verify URLs contain the accession ID
			if primary != "" && !containsAccessionID(primary, tt.accessionID) {
				t.Errorf("Primary URL does not contain accession ID: %q", primary)
			}
		})
	}
}

func TestSRAValidator_HTTPValidation(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/success":
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Content-Length", "1024")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("<html><body>Test page</body></html>"))
		case "/not-found":
			w.WriteHeader(http.StatusNotFound)
		case "/server-error":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	validator := NewSRAValidator()
	ctx := context.Background()

	tests := []struct {
		name             string
		url              string
		expectAccessible bool
		expectStatus     int
	}{
		{
			name:             "Successful request",
			url:              server.URL + "/success",
			expectAccessible: true,
			expectStatus:     200,
		},
		{
			name:             "Not found",
			url:              server.URL + "/not-found",
			expectAccessible: false,
			expectStatus:     404,
		},
		{
			name:             "Server error",
			url:              server.URL + "/server-error",
			expectAccessible: false,
			expectStatus:     500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validator.ValidateHTTPAccess(ctx, tt.url)
			if err != nil {
				t.Errorf("ValidateHTTPAccess() unexpected error: %v", err)
				return
			}

			if result == nil {
				t.Fatal("ValidateHTTPAccess() returned nil result")
			}

			if result.Accessible != tt.expectAccessible {
				t.Errorf("Accessible = %v, expected %v", result.Accessible, tt.expectAccessible)
			}

			if result.StatusCode != tt.expectStatus {
				t.Errorf("StatusCode = %d, expected %d", result.StatusCode, tt.expectStatus)
			}

			if result.URL != tt.url {
				t.Errorf("URL = %q, expected %q", result.URL, tt.url)
			}

			if result.ValidationTime <= 0 {
				t.Error("Expected ValidationTime to be positive")
			}
		})
	}
}

func TestSRAValidator_Cache(t *testing.T) {
	validator := NewSRAValidator()

	// Test cache operations
	initialSize := validator.GetCacheSize()
	if initialSize != 0 {
		t.Errorf("Initial cache size should be 0, got %d", initialSize)
	}

	// Clear cache (should not error on empty cache)
	validator.ClearCache()

	// Verify cache is still empty
	if validator.GetCacheSize() != 0 {
		t.Error("Cache size should be 0 after clearing empty cache")
	}
}

func TestSRAValidator_SpecificAccessionTypes(t *testing.T) {
	validator := NewSRAValidator()
	ctx := context.Background()

	// Test specific URL generation for different accession types
	accessionTests := map[AccessionType]string{
		RunSRA:            "SRR123456",
		ExperimentSRA:     "SRX123456",
		SampleSRA:         "SRS123456",
		StudySRA:          "SRP123456",
		ProjectBioProject: "PRJNA123456",
	}

	for accType, accession := range accessionTests {
		t.Run(string(accType), func(t *testing.T) {
			result, err := validator.Validate(ctx, accession)
			if err != nil {
				t.Errorf("Validate() error = %v", err)
				return
			}

			if !result.Valid {
				t.Error("Expected validation to succeed")
				return
			}

			if result.NormalizedID != accession {
				t.Errorf("NormalizedID = %q, expected %q", result.NormalizedID, accession)
			}

			// Check that the correct accession type is identified
			if metaType, exists := result.Metadata["accession_type"]; exists {
				if AccessionType(metaType) != accType {
					t.Errorf("Metadata accession_type = %q, expected %q", metaType, accType)
				}
			} else {
				t.Error("Expected accession_type in metadata")
			}

			// Check that appropriate tags are added
			hasExpectedTag := false
			expectedTags := map[AccessionType]string{
				RunSRA:            "run_level",
				ExperimentSRA:     "experiment_level",
				SampleSRA:         "sample",
				StudySRA:          "study_level",
				ProjectBioProject: "project_level",
			}

			if expectedTag, exists := expectedTags[accType]; exists {
				for _, tag := range result.Tags {
					if tag == expectedTag {
						hasExpectedTag = true
						break
					}
				}
				if !hasExpectedTag {
					t.Errorf("Expected tag %q not found in result tags", expectedTag)
				}
			}
		})
	}
}

// Helper functions

func isValidURL(url string) bool {
	return url != "" && (url[:7] == "http://" || url[:8] == "https://" || url[:6] == "ftp://")
}

func containsAccessionID(url, accessionID string) bool {
	return url != "" && accessionID != "" &&
		(strings.Contains(url, accessionID) ||
			strings.Contains(url, strings.ToLower(accessionID)))
}

// Benchmark tests.
func BenchmarkSRAValidator_CanValidate(b *testing.B) {
	validator := NewSRAValidator()
	testInputs := []string{
		"SRR123456",
		"ERX1234567",
		"PRJNA123456",
		"https://www.ncbi.nlm.nih.gov/sra/SRR123456",
		"INVALID123",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, input := range testInputs {
			validator.CanValidate(input)
		}
	}
}

func BenchmarkSRAValidator_Validate(b *testing.B) {
	validator := NewSRAValidator()
	ctx := context.Background()
	testInputs := []string{
		"SRR123456",
		"ERX1234567",
		"PRJNA123456",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, input := range testInputs {
			validator.Validate(ctx, input)
		}
	}
}

func TestSRAValidator_EdgeCases(t *testing.T) {
	validator := NewSRAValidator()
	ctx := context.Background()

	edgeCases := []struct {
		name        string
		input       string
		expectValid bool
	}{
		{
			name:        "Whitespace around accession",
			input:       "  SRR123456  ",
			expectValid: true,
		},
		{
			name:        "Mixed case accession",
			input:       "sRr123456",
			expectValid: true,
		},
		{
			name:        "Very long valid accession",
			input:       "SRR12345678901234",
			expectValid: true,
		},
		{
			name:        "Minimum length valid accession",
			input:       "SRR123",
			expectValid: false, // Too short for SRA pattern
		},
		{
			name:        "Accession with version",
			input:       "SRR123456.1",
			expectValid: false, // SRA doesn't use versions
		},
		{
			name:        "URL with query parameters",
			input:       "https://www.ncbi.nlm.nih.gov/sra?term=SRR123456&db=sra",
			expectValid: true,
		},
	}

	for _, tt := range edgeCases {
		t.Run(tt.name, func(t *testing.T) {
			result, err := validator.Validate(ctx, tt.input)
			if err != nil {
				t.Errorf("Validate() unexpected error: %v", err)
				return
			}

			if result.Valid != tt.expectValid {
				t.Errorf("Validate(%q) Valid = %v, expected %v", tt.input, result.Valid, tt.expectValid)
			}
		})
	}
}

func TestSRAValidator_Timeout(t *testing.T) {
	validator := NewSRAValidator()

	// Create a context with a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// This should handle the timeout gracefully
	result, err := validator.Validate(ctx, "SRR123456")
	// Should not error, but HTTP validation might fail due to timeout
	if err != nil {
		t.Errorf("Validate() should handle timeout gracefully, got error: %v", err)
	}

	if result == nil {
		t.Fatal("Validate() returned nil result")
	}

	// Basic validation should still work even with timeout
	if !result.Valid {
		t.Error("Basic validation should succeed even with timeout")
	}
}
