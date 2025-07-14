package extractor

import (
	"strings"
	"testing"

	"github.com/btraven00/hapiq/pkg/validators/domains"
	_ "github.com/btraven00/hapiq/pkg/validators/domains/bio" // Import for side effects
)

func TestNewPDFExtractor(t *testing.T) {
	options := DefaultExtractionOptions()
	extractor := NewPDFExtractor(options)

	if extractor == nil {
		t.Fatal("NewPDFExtractor returned nil")
	}

	if len(extractor.extractionPatterns) == 0 {
		t.Error("Expected extraction patterns to be initialized")
	}

	if len(extractor.sectionRegexes) == 0 {
		t.Error("Expected section regexes to be initialized")
	}

	if len(extractor.cleaners) == 0 {
		t.Error("Expected cleaners to be initialized")
	}
}

func TestExtractCandidates(t *testing.T) {
	extractor := NewPDFExtractor(DefaultExtractionOptions())

	testCases := []struct {
		name     string
		text     string
		expected []string // Expected candidate texts
	}{
		{
			name: "DOI patterns",
			text: "This study is available at doi:10.1234/example.dataset.2024 and also at https://doi.org/10.5678/another.dataset",
			expected: []string{
				"10.1234/example.dataset.2024",
				"10.5678/another.dataset",
			},
		},
		{
			name: "GEO patterns",
			text: "Gene expression data is available in GEO under accession GSE123456. Sample GSM789012 was processed using platform GPL570.",
			expected: []string{
				"GSE123456",
				"GSM789012",
				"GPL570",
			},
		},
		{
			name: "Repository URLs",
			text: "Data available at https://zenodo.org/record/123456 and https://figshare.com/articles/dataset/title/789012",
			expected: []string{
				"https://zenodo.org/record/123456",
				"https://figshare.com/articles/dataset/title/789012",
			},
		},
		{
			name: "Mixed identifiers",
			text: "See PMID: 12345678, arXiv:2024.12345, and data at https://github.com/user/dataset-repo",
			expected: []string{
				"12345678",
				"2024.12345",
				"https://github.com/user/dataset-repo",
			},
		},
		{
			name: "Bioinformatics identifiers",
			text: "Sequences were deposited as SRR123456 under BioProject PRJNA789012. Protein structure 1ABC was used.",
			expected: []string{
				"SRR123456",
				"PRJNA789012",
				"1ABC",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			candidates := extractor.extractCandidates(tc.text)

			if len(candidates) < len(tc.expected) {
				t.Errorf("Expected at least %d candidates, got %d", len(tc.expected), len(candidates))
			}

			// Check that all expected texts are found
			found := make(map[string]bool)
			for _, candidate := range candidates {
				found[candidate.Text] = true
			}

			for _, expected := range tc.expected {
				if !found[expected] {
					t.Errorf("Expected to find candidate '%s' in text '%s'", expected, tc.text)
				}
			}
		})
	}
}

func TestNormalizeCandidate(t *testing.T) {
	extractor := NewPDFExtractor(DefaultExtractionOptions())

	testCases := []struct {
		text     string
		linkType LinkType
		expected string
	}{
		{
			text:     "10.1234/example",
			linkType: LinkTypeDOI,
			expected: "https://doi.org/10.1234/example",
		},
		{
			text:     "https://doi.org/10.1234/example",
			linkType: LinkTypeDOI,
			expected: "https://doi.org/10.1234/example",
		},
		{
			text:     "https://example.com/data.csv;",
			linkType: LinkTypeURL,
			expected: "https://example.com/data.csv",
		},
		{
			text:     "GSE123456",
			linkType: LinkTypeGeoID,
			expected: "GSE123456",
		},
	}

	for _, tc := range testCases {
		result := extractor.normalizeCandidate(tc.text, tc.linkType)
		if result != tc.expected {
			t.Errorf("normalizeCandidate(%q, %v) = %q, expected %q",
				tc.text, tc.linkType, result, tc.expected)
		}
	}
}

func TestMapDomainToLinkType(t *testing.T) {
	testCases := []struct {
		validatorName string
		datasetType   string
		expected      LinkType
	}{
		{"geo", "expression_data", LinkTypeGeoID},
		{"unknown", "zenodo", LinkTypeZenodo},
		{"unknown", "figshare", LinkTypeFigshare},
		{"unknown", "doi", LinkTypeDOI},
		{"unknown", "unknown", LinkTypeURL},
	}

	for _, tc := range testCases {
		result := mapDomainToLinkType(tc.validatorName, tc.datasetType)
		if result != tc.expected {
			t.Errorf("mapDomainToLinkType(%q, %q) = %v, expected %v",
				tc.validatorName, tc.datasetType, result, tc.expected)
		}
	}
}

func TestDeduplicateLinks(t *testing.T) {
	extractor := NewPDFExtractor(DefaultExtractionOptions())

	links := []ExtractedLink{
		{URL: "https://example.com/1", Type: LinkTypeURL, Confidence: 0.8},
		{URL: "https://example.com/2", Type: LinkTypeURL, Confidence: 0.9},
		{URL: "https://example.com/1", Type: LinkTypeURL, Confidence: 0.7}, // Duplicate
		{URL: "https://example.com/3", Type: LinkTypeDOI, Confidence: 0.95},
	}

	deduped := extractor.deduplicateLinks(links)

	if len(deduped) != 3 {
		t.Errorf("Expected 3 unique links, got %d", len(deduped))
	}

	// Check that the first occurrence is kept (higher confidence)
	found := false
	for _, link := range deduped {
		if link.URL == "https://example.com/1" {
			if link.Confidence != 0.8 {
				t.Errorf("Expected first occurrence to be kept (confidence 0.8), got %f", link.Confidence)
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected to find https://example.com/1 in deduplicated results")
	}
}

func TestDetectSection(t *testing.T) {
	extractor := NewPDFExtractor(DefaultExtractionOptions())

	testCases := []struct {
		text     string
		expected string
	}{
		{"Abstract\nThis study investigates...", "abstract"},
		{"INTRODUCTION\nGene expression analysis...", "introduction"},
		{"Methods and Materials\nSamples were collected...", "methods"},
		{"RESULTS\nWe found that...", "results"},
		{"Discussion and Conclusions\nOur findings suggest...", "discussion"},
		{"References\n1. Smith et al...", "references"},
		{"Data Availability Statement\nData is available at...", "data"},
		{"Random text without section headers", "unknown"},
	}

	for _, tc := range testCases {
		result := extractor.detectSection(tc.text)
		if result != tc.expected {
			t.Errorf("detectSection(%q) = %q, expected %q", tc.text, result, tc.expected)
		}
	}
}

func TestExtractContextForMatch(t *testing.T) {
	options := DefaultExtractionOptions()
	options.IncludeContext = true
	options.ContextLength = 40
	extractor := NewPDFExtractor(options)

	text := "This is a long text with a DOI 10.1234/example.dataset in the middle of the sentence for testing context extraction."
	match := "10.1234/example.dataset"

	context := extractor.extractContextForMatch(text, match)

	if context == "" {
		t.Error("Expected non-empty context")
	}

	if !strings.Contains(context, match) {
		t.Errorf("Expected context to contain the match '%s', got '%s'", match, context)
	}

	// Test with context disabled
	options.IncludeContext = false
	extractor2 := NewPDFExtractor(options)
	context2 := extractor2.extractContextForMatch(text, match)

	if context2 != "" {
		t.Errorf("Expected empty context when disabled, got '%s'", context2)
	}
}

func TestFilterLinks(t *testing.T) {
	options := ExtractionOptions{
		MinConfidence: 0.7,
		FilterDomains: []string{"example.com", "zenodo.org"},
	}
	extractor := NewPDFExtractor(options)

	links := []ExtractedLink{
		{URL: "https://example.com/data", Confidence: 0.8},      // Should pass
		{URL: "https://zenodo.org/record/123", Confidence: 0.9}, // Should pass
		{URL: "https://other.com/data", Confidence: 0.8},        // Should fail domain filter
		{URL: "https://example.com/low", Confidence: 0.5},       // Should fail confidence filter
		{URL: "https://good.com/data", Confidence: 0.9},         // Should fail domain filter
	}

	filtered := extractor.filterLinks(links)

	if len(filtered) != 2 {
		t.Errorf("Expected 2 filtered links, got %d", len(filtered))
	}

	for _, link := range filtered {
		if link.Confidence < 0.7 {
			t.Errorf("Filtered link has confidence %f below threshold 0.7", link.Confidence)
		}

		domainMatch := false
		for _, domain := range options.FilterDomains {
			if strings.Contains(link.URL, domain) {
				domainMatch = true
				break
			}
		}
		if !domainMatch {
			t.Errorf("Filtered link %s doesn't match any allowed domains", link.URL)
		}
	}
}

func TestDomainValidatorIntegration(t *testing.T) {
	// Test that domain validators are available
	geoValidators := domains.FindValidators("GSE123456")
	if len(geoValidators) == 0 {
		t.Skip("Bio domain validators not loaded, skipping integration test")
	}

	// Test that GEO validator can handle GSE IDs
	found := false
	for _, validator := range geoValidators {
		if validator.Name() == "geo" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected to find 'geo' validator for GSE123456")
	}
}

func TestExtractionPatterns(t *testing.T) {
	patterns := getExtractionPatterns()

	if len(patterns) == 0 {
		t.Fatal("No extraction patterns found")
	}

	// Test that each pattern has required fields
	for i, pattern := range patterns {
		if pattern.Name == "" {
			t.Errorf("Pattern %d has empty name", i)
		}
		if pattern.Regex == nil {
			t.Errorf("Pattern %d (%s) has nil regex", i, pattern.Name)
		}
		if pattern.Confidence <= 0 || pattern.Confidence > 1 {
			t.Errorf("Pattern %d (%s) has invalid confidence %f", i, pattern.Name, pattern.Confidence)
		}
		if pattern.Description == "" {
			t.Errorf("Pattern %d (%s) has empty description", i, pattern.Name)
		}
	}

	// Test that patterns can match their examples
	for _, pattern := range patterns {
		if len(pattern.Examples) == 0 {
			continue // Skip patterns without examples
		}

		for _, example := range pattern.Examples {
			matches := pattern.Regex.FindStringSubmatch(example)
			if len(matches) == 0 {
				t.Errorf("Pattern %s failed to match its example: %s", pattern.Name, example)
			}
		}
	}
}

func TestCleanText(t *testing.T) {
	extractor := NewPDFExtractor(DefaultExtractionOptions())

	testCases := []struct {
		input    string
		expected string
	}{
		{"Normal text", "Normal text"},
		{"Text   with    multiple   spaces", "Text with multiple spaces"},
		{"Text\nwith\r\nline\nbreaks", "Text with line breaks"},
		{"Text with () empty [] brackets {}", "Text with empty brackets"},
		{"Text with null\x00bytes", "Text with null bytes"},
	}

	for _, tc := range testCases {
		result := extractor.cleanText(tc.input)
		if result != tc.expected {
			t.Errorf("cleanText(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}
