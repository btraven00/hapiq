package validators

import (
	"testing"
)

func TestURLValidator_ValidateURL(t *testing.T) {
	validator := NewURLValidator()

	tests := []struct {
		name      string
		input     string
		wantType  string
		wantMsg   string
		wantValid bool
	}{
		{
			name:      "valid zenodo URL",
			input:     "https://zenodo.org/record/123456",
			wantValid: true,
			wantType:  "zenodo_record",
			wantMsg:   "Valid URL",
		},
		{
			name:      "valid figshare URL",
			input:     "https://figshare.com/articles/dataset/example/123456",
			wantValid: true,
			wantType:  "figshare_article",
			wantMsg:   "Valid URL",
		},
		{
			name:      "valid github release URL",
			input:     "https://github.com/user/repo/releases/tag/v1.0.0",
			wantValid: true,
			wantType:  "github_release",
			wantMsg:   "Valid URL",
		},
		{
			name:      "valid generic URL",
			input:     "https://example.com/data.zip",
			wantValid: true,
			wantType:  "generic",
			wantMsg:   "Valid URL",
		},
		{
			name:      "URL without scheme",
			input:     "zenodo.org/record/123456",
			wantValid: false,
			wantMsg:   "URL missing scheme (http/https)",
		},
		{
			name:      "URL with invalid scheme",
			input:     "ftp://example.com/data.zip",
			wantValid: false,
			wantMsg:   "Unsupported URL scheme: ftp",
		},
		{
			name:      "malformed URL",
			input:     "not-a-url",
			wantValid: false,
		},
		{
			name:      "empty URL",
			input:     "",
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.ValidateURL(tt.input)

			if result.Valid != tt.wantValid {
				t.Errorf("ValidateURL() valid = %v, want %v", result.Valid, tt.wantValid)
			}

			if tt.wantValid && result.Type != tt.wantType {
				t.Errorf("ValidateURL() type = %v, want %v", result.Type, tt.wantType)
			}

			if tt.wantMsg != "" && result.Message != tt.wantMsg {
				t.Errorf("ValidateURL() message = %v, want %v", result.Message, tt.wantMsg)
			}
		})
	}
}

func TestDOIValidator_ValidateDOI(t *testing.T) {
	validator := NewDOIValidator()

	tests := []struct {
		name      string
		input     string
		wantType  string
		wantValid bool
	}{
		{
			name:      "valid zenodo DOI",
			input:     "10.5281/zenodo.123456",
			wantValid: true,
			wantType:  "zenodo_doi",
		},
		{
			name:      "valid figshare DOI",
			input:     "10.6084/m9.figshare.123456",
			wantValid: true,
			wantType:  "figshare_doi",
		},
		{
			name:      "valid dryad DOI",
			input:     "10.5061/dryad.123abc",
			wantValid: true,
			wantType:  "dryad_doi",
		},
		{
			name:      "valid generic DOI",
			input:     "10.1234/example.doi.123",
			wantValid: true,
			wantType:  "generic_doi",
		},
		{
			name:      "DOI with doi: prefix",
			input:     "doi:10.5281/zenodo.123456",
			wantValid: true,
			wantType:  "zenodo_doi",
		},
		{
			name:      "DOI with URL prefix",
			input:     "https://doi.org/10.5281/zenodo.123456",
			wantValid: true,
			wantType:  "zenodo_doi",
		},
		{
			name:      "invalid DOI - wrong prefix",
			input:     "11.5281/zenodo.123456",
			wantValid: false,
		},
		{
			name:      "invalid DOI - too short registrant",
			input:     "10.123/test",
			wantValid: false,
		},
		{
			name:      "invalid DOI - missing suffix",
			input:     "10.5281/",
			wantValid: false,
		},
		{
			name:      "invalid DOI - no slash",
			input:     "10.5281.zenodo.123456",
			wantValid: false,
		},
		{
			name:      "empty DOI",
			input:     "",
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.ValidateDOI(tt.input)

			if result.Valid != tt.wantValid {
				t.Errorf("ValidateDOI() valid = %v, want %v", result.Valid, tt.wantValid)
			}

			if tt.wantValid && result.Type != tt.wantType {
				t.Errorf("ValidateDOI() type = %v, want %v", result.Type, tt.wantType)
			}
		})
	}
}

func TestValidateIdentifier(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantType  string
		wantValid bool
	}{
		{
			name:      "valid URL",
			input:     "https://zenodo.org/record/123456",
			wantValid: true,
			wantType:  "zenodo_record",
		},
		{
			name:      "valid DOI",
			input:     "10.5281/zenodo.123456",
			wantValid: true,
			wantType:  "zenodo_doi",
		},
		{
			name:      "invalid identifier",
			input:     "not-valid-url-or-doi",
			wantValid: false,
			wantType:  "unknown",
		},
		{
			name:      "empty input",
			input:     "",
			wantValid: false,
			wantType:  "unknown",
		},
		{
			name:      "whitespace input",
			input:     "   ",
			wantValid: false,
			wantType:  "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateIdentifier(tt.input)

			if result.Valid != tt.wantValid {
				t.Errorf("ValidateIdentifier() valid = %v, want %v", result.Valid, tt.wantValid)
			}

			if result.Type != tt.wantType {
				t.Errorf("ValidateIdentifier() type = %v, want %v", result.Type, tt.wantType)
			}
		})
	}
}

func TestIsDatasetRepository(t *testing.T) {
	tests := []struct {
		urlType  string
		expected bool
	}{
		{"zenodo_record", true},
		{"figshare_article", true},
		{"dryad_dataset", true},
		{"zenodo_doi", true},
		{"osf", true},
		{"dataverse", true},
		{"github", false},
		{"generic", false},
		{"unknown", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.urlType, func(t *testing.T) {
			result := IsDatasetRepository(tt.urlType)
			if result != tt.expected {
				t.Errorf("IsDatasetRepository(%s) = %v, want %v", tt.urlType, result, tt.expected)
			}
		})
	}
}

func TestGetRepositoryScore(t *testing.T) {
	tests := []struct {
		name           string
		validationType string
		valid          bool
		expectedScore  float64
	}{
		{
			name:           "invalid result",
			validationType: "zenodo_record",
			valid:          false,
			expectedScore:  0.0,
		},
		{
			name:           "zenodo record",
			validationType: "zenodo_record",
			valid:          true,
			expectedScore:  0.95,
		},
		{
			name:           "figshare article",
			validationType: "figshare_article",
			valid:          true,
			expectedScore:  0.95,
		},
		{
			name:           "zenodo DOI",
			validationType: "zenodo_doi",
			valid:          true,
			expectedScore:  0.90,
		},
		{
			name:           "github release",
			validationType: "github_release",
			valid:          true,
			expectedScore:  0.60,
		},
		{
			name:           "generic URL",
			validationType: "generic",
			valid:          true,
			expectedScore:  0.10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidationResult{
				Valid: tt.valid,
				Type:  tt.validationType,
			}

			score := GetRepositoryScore(result)
			if score != tt.expectedScore {
				t.Errorf("GetRepositoryScore() = %v, want %v", score, tt.expectedScore)
			}
		})
	}
}

func TestURLValidator_classifyURLType(t *testing.T) {
	validator := NewURLValidator()

	tests := []struct {
		url      string
		expected string
	}{
		{"https://zenodo.org/record/123456", "zenodo_record"},
		{"https://zenodo.org/", "zenodo"},
		{"https://figshare.com/articles/dataset/test/123", "figshare_article"},
		{"https://figshare.com/", "figshare"},
		{"https://github.com/user/repo/releases/tag/v1.0", "github_release"},
		{"https://github.com/user/repo", "github"},
		{"https://doi.org/10.5281/zenodo.123", "doi_resolver"},
		{"https://example.com/data.zip", "generic"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			result := validator.ValidateURL(tt.url)
			if result.Type != tt.expected {
				t.Errorf("classifyURLType(%s) = %v, want %v", tt.url, result.Type, tt.expected)
			}
		})
	}
}

func TestDOIValidator_classifyDOIType(t *testing.T) {
	validator := NewDOIValidator()

	tests := []struct {
		prefix   string
		suffix   string
		expected string
	}{
		{"10.5281", "zenodo.123456", "zenodo_doi"},
		{"10.6084", "m9.figshare.123456", "figshare_doi"},
		{"10.5061", "dryad.123abc", "dryad_doi"},
		{"10.1371", "journal.pone.123456", "plos_doi"},
		{"10.1038", "nature.123456", "nature_doi"},
		{"10.9999", "test.zenodo.456", "zenodo_doi"},
		{"10.9999", "regular.paper.123", "generic_doi"},
	}

	for _, tt := range tests {
		t.Run(tt.prefix+"/"+tt.suffix, func(t *testing.T) {
			result := validator.classifyDOIType(tt.prefix, tt.suffix)
			if result != tt.expected {
				t.Errorf("classifyDOIType(%s, %s) = %v, want %v", tt.prefix, tt.suffix, result, tt.expected)
			}
		})
	}
}

// Benchmark tests.
func BenchmarkValidateURL(b *testing.B) {
	validator := NewURLValidator()
	url := "https://zenodo.org/record/123456"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.ValidateURL(url)
	}
}

func BenchmarkValidateDOI(b *testing.B) {
	validator := NewDOIValidator()
	doi := "10.5281/zenodo.123456"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.ValidateDOI(doi)
	}
}

func BenchmarkValidateIdentifier(b *testing.B) {
	identifier := "https://zenodo.org/record/123456"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ValidateIdentifier(identifier)
	}
}

// Edge case tests.
func TestEdgeCases(t *testing.T) {
	t.Run("DOI with special characters", func(t *testing.T) {
		validator := NewDOIValidator()
		result := validator.ValidateDOI("10.1234/test-data_2024.v1")
		if !result.Valid {
			t.Errorf("Expected valid DOI with special characters, got invalid")
		}
	})

	t.Run("URL with query parameters", func(t *testing.T) {
		validator := NewURLValidator()
		result := validator.ValidateURL("https://zenodo.org/record/123456?token=abc123")
		if !result.Valid {
			t.Errorf("Expected valid URL with query parameters, got invalid")
		}
	})

	t.Run("Case insensitive DOI prefix", func(t *testing.T) {
		validator := NewDOIValidator()
		result := validator.ValidateDOI("DOI:10.5281/zenodo.123456")
		if !result.Valid {
			t.Errorf("Expected valid DOI with uppercase prefix, got invalid")
		}
	})

	t.Run("Very long DOI", func(t *testing.T) {
		validator := NewDOIValidator()
		longSuffix := "very.long.doi.with.many.segments.and.identifiers.123456789"
		doi := "10.5281/" + longSuffix
		result := validator.ValidateDOI(doi)
		if !result.Valid {
			t.Errorf("Expected valid long DOI, got invalid")
		}
	})
}
