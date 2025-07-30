// Package geo provides unit tests for the GEO downloader implementation
// using E-utilities XML responses instead of HTML scraping.
package geo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

func TestGEODownloader_GetSourceType(t *testing.T) {
	downloader := NewGEODownloader()

	expected := "geo"
	actual := downloader.GetSourceType()

	if actual != expected {
		t.Fatalf("Expected source type '%s', got '%s'", expected, actual)
	}
}

func TestGEODownloader_Validate(t *testing.T) {
	downloader := NewGEODownloader()
	ctx := context.Background()

	tests := []struct {
		name        string
		id          string
		description string
		expectValid bool
		expectError bool
	}{
		{
			name:        "valid GSE ID",
			id:          "GSE123456",
			expectValid: true,
			expectError: false,
			description: "Standard GSE format should be valid",
		},
		{
			name:        "valid GSM ID",
			id:          "GSM789012",
			expectValid: true,
			expectError: false,
			description: "Standard GSM format should be valid",
		},
		{
			name:        "valid GPL ID",
			id:          "GPL570",
			expectValid: true,
			expectError: false,
			description: "Standard GPL format should be valid",
		},
		{
			name:        "valid GDS ID",
			id:          "GDS5678",
			expectValid: true,
			expectError: false,
			description: "Standard GDS format should be valid",
		},
		{
			name:        "lowercase GSE",
			id:          "gse123456",
			expectValid: true,
			expectError: false,
			description: "Lowercase should be normalized to uppercase",
		},
		{
			name:        "GSE with URL prefix",
			id:          "https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE123456",
			expectValid: true,
			expectError: false,
			description: "URL should be cleaned to extract ID",
		},
		{
			name:        "invalid format - no digits",
			id:          "GSE",
			expectValid: false,
			expectError: false,
			description: "ID without digits should be invalid",
		},
		{
			name:        "invalid format - wrong prefix",
			id:          "XYZ123456",
			expectValid: false,
			expectError: false,
			description: "Non-GEO prefix should be invalid",
		},
		{
			name:        "invalid format - negative number",
			id:          "GSE-123",
			expectValid: false,
			expectError: false,
			description: "Negative numbers should be invalid",
		},
		{
			name:        "invalid format - zero",
			id:          "GSE0",
			expectValid: false,
			expectError: false,
			description: "Zero should be invalid",
		},
		{
			name:        "very low number with warning",
			id:          "GSE50",
			expectValid: true,
			expectError: false,
			description: "Very low numbers should be valid but generate warning",
		},
		{
			name:        "empty string",
			id:          "",
			expectValid: false,
			expectError: false,
			description: "Empty string should be invalid",
		},
		{
			name:        "whitespace only",
			id:          "   ",
			expectValid: false,
			expectError: false,
			description: "Whitespace should be invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := downloader.Validate(ctx, tt.id)

			if tt.expectError && err == nil {
				t.Fatalf("Expected error for test '%s', got nil", tt.name)
			}

			if !tt.expectError && err != nil {
				t.Fatalf("Unexpected error for test '%s': %v", tt.name, err)
			}

			if result.Valid != tt.expectValid {
				t.Fatalf("Expected valid=%t for test '%s', got valid=%t", tt.expectValid, tt.name, result.Valid)
			}

			if result.SourceType != "geo" {
				t.Fatalf("Expected source type 'geo', got '%s'", result.SourceType)
			}

			// Check that errors are present when invalid
			if !tt.expectValid && len(result.Errors) == 0 {
				t.Fatalf("Expected errors for invalid ID '%s', got none", tt.id)
			}

			// Check warnings for edge cases
			if tt.id == "GSE50" && len(result.Warnings) == 0 {
				t.Fatal("Expected warning for very low GSE number")
			}

			if strings.Contains(tt.id, "GSM") && len(result.Warnings) == 0 {
				t.Fatal("Expected warning for non-GSE type")
			}
		})
	}
}

func TestGEODownloader_cleanGEOID(t *testing.T) {
	downloader := NewGEODownloader()

	tests := []struct {
		input    string
		expected string
	}{
		{"GSE123456", "GSE123456"},
		{"gse123456", "GSE123456"},
		{"  GSE123456  ", "GSE123456"},
		{"https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE123456", "GSE123456"},
		{"GEO:GSE123456", "GSE123456"},
		{"NCBI:GSE123456", "GSE123456"},
		{"http://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE123456&view=full", "GSE123456"},
		{"GSE123456&other=params", "GSE123456"},
		{"prefix_GSE123456_suffix", "GSE123456"},
		{"", ""},
		{"invalid", ""},
		{"123456", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := downloader.cleanGEOID(tt.input)
			if result != tt.expected {
				t.Fatalf("cleanGEOID('%s') = '%s', expected '%s'", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGEODownloader_buildGEOURL(t *testing.T) {
	downloader := NewGEODownloader()

	id := "GSE123456"
	expected := "https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE123456"
	actual := downloader.buildGEOURL(id)

	if actual != expected {
		t.Fatalf("buildGEOURL('%s') = '%s', expected '%s'", id, actual, expected)
	}
}

func TestGEODownloader_GetMetadata_MockEUtils(t *testing.T) {
	// Mock E-utilities responses
	mockESearchResponse := `<?xml version="1.0" encoding="UTF-8"?>
<eSearchResult>
	<Count>1</Count>
	<RetMax>1</RetMax>
	<RetStart>0</RetStart>
	<IdList>
		<Id>200123456</Id>
	</IdList>
</eSearchResult>`

	mockESummaryResponse := `<?xml version="1.0" encoding="UTF-8"?>
<eSummaryResult>
	<DocSum>
		<Id>200123456</Id>
		<Item Name="title" Type="String">Test Series Title</Item>
		<Item Name="summary" Type="String">This is a test series description for functional genomics analysis</Item>
		<Item Name="GPL" Type="String">GPL570</Item>
		<Item Name="taxon" Type="String">Homo sapiens</Item>
		<Item Name="entryType" Type="String">GSE</Item>
		<Item Name="gdsType" Type="String">Expression profiling by array</Item>
		<Item Name="ptechType" Type="String">high throughput sequencing</Item>
		<Item Name="valType" Type="String">log2 ratio</Item>
		<Item Name="PDAT" Type="String">2020/01/15</Item>
		<Item Name="SSInfo" Type="String">
			<Item Name="samples" Type="Integer">24</Item>
			<Item Name="subsets" Type="Integer">4</Item>
		</Item>
		<Item Name="suppFile" Type="String">RAW.tar;processed_data.txt.gz</Item>
		<Item Name="Accession" Type="String">GSE123456</Item>
	</DocSum>
</eSummaryResult>`

	// Create a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")

		// Route based on URL path and query
		if strings.Contains(r.URL.Path, "esearch") || strings.Contains(r.URL.RawQuery, "esearch") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(mockESearchResponse))
		} else if strings.Contains(r.URL.Path, "esummary") || strings.Contains(r.URL.RawQuery, "esummary") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(mockESummaryResponse))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Create downloader with custom client that redirects E-utilities calls to our mock
	client := &http.Client{
		Transport: &mockEUtilsTransport{server: server},
		Timeout:   30 * time.Second,
	}

	downloader := NewGEODownloader(WithHTTPClient(client))

	ctx := context.Background()
	metadata, err := downloader.getSeriesMetadata(ctx, "GSE123456")
	if err != nil {
		t.Fatalf("Expected successful metadata retrieval, got error: %v", err)
	}

	if metadata.Source != "geo" {
		t.Fatalf("Expected source 'geo', got '%s'", metadata.Source)
	}

	if metadata.ID != "GSE123456" {
		t.Fatalf("Expected ID 'GSE123456', got '%s'", metadata.ID)
	}

	if metadata.Title != "Test Series Title" {
		t.Fatalf("Expected title 'Test Series Title', got '%s'", metadata.Title)
	}

	if metadata.Description != "This is a test series description for functional genomics analysis" {
		t.Fatalf("Expected correct description, got '%s'", metadata.Description)
	}

	// Check custom fields
	if organism, ok := metadata.Custom["organism"]; !ok || organism != "Homo sapiens" {
		t.Fatalf("Expected organism 'Homo sapiens', got %v", organism)
	}

	if platform, ok := metadata.Custom["platform"]; !ok || platform != "GPL570" {
		t.Fatalf("Expected platform 'GPL570', got %v", platform)
	}

	if entryType, ok := metadata.Custom["entry_type"]; !ok || entryType != "GSE" {
		t.Fatalf("Expected entry type 'GSE', got %v", entryType)
	}

	// Check created date parsing
	if metadata.Created.IsZero() {
		t.Fatal("Expected created date to be parsed")
	}

	expectedDate := time.Date(2020, 1, 15, 0, 0, 0, 0, time.UTC)
	if !metadata.Created.Equal(expectedDate) {
		t.Fatalf("Expected created date %v, got %v", expectedDate, metadata.Created)
	}
}

// mockEUtilsTransport redirects E-utilities requests to our test server.
type mockEUtilsTransport struct {
	server *httptest.Server
}

func (t *mockEUtilsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Redirect E-utilities calls to our mock server
	if strings.Contains(req.URL.Host, "eutils.ncbi.nlm.nih.gov") {
		// Parse the mock server URL to get host and port
		mockURL := strings.TrimPrefix(t.server.URL, "http://")
		req.URL.Scheme = "http"
		req.URL.Host = mockURL
	}

	return http.DefaultTransport.RoundTrip(req)
}

func TestGEODownloader_parseEUtilsDate(t *testing.T) {
	downloader := NewGEODownloader()

	tests := []struct {
		input       string
		expectError bool
		description string
	}{
		{"2020/01/15", false, "E-utilities format should parse"},
		{"2020-01-15", false, "ISO format should parse"},
		{"2020/1/2", false, "Single digit format should parse"},
		{"Jan 15, 2020", false, "Text format should parse"},
		{"Jan 1, 2020", false, "Single digit day should parse"},
		{"", true, "Empty string should fail"},
		{"invalid date", true, "Invalid format should fail"},
		{"2020-13-45", true, "Invalid date values should fail"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := downloader.parseEUtilsDate(tt.input)

			if tt.expectError && err == nil {
				t.Fatalf("Expected error for input '%s', got nil", tt.input)
			}

			if !tt.expectError && err != nil {
				t.Fatalf("Unexpected error for input '%s': %v", tt.input, err)
			}

			if !tt.expectError && result.IsZero() {
				t.Fatalf("Expected valid time for input '%s', got zero time", tt.input)
			}
		})
	}
}

func TestGEODownloader_extractSampleIDsFromSOFT(t *testing.T) {
	downloader := NewGEODownloader()

	softContent := `
^SERIES = GSE123456
!Series_title = Test Series
!Series_summary = Test description
^SAMPLE = GSM1000001
!Sample_title = Test sample 1
!Sample_source_name_ch1 = Cell line A
^SAMPLE = GSM1000002
!Sample_title = Test sample 2
!Sample_source_name_ch1 = Cell line B
^SAMPLE = GSM1000001
!Sample_title = Duplicate sample
^PLATFORM = GPL570
!Platform_title = Test platform
	`

	samples := downloader.extractSampleIDsFromSOFT(softContent)

	expectedSamples := []string{"GSM1000001", "GSM1000002"}
	if len(samples) != len(expectedSamples) {
		t.Fatalf("Expected %d unique samples, got %d", len(expectedSamples), len(samples))
	}

	sampleSet := make(map[string]bool)
	for _, sample := range samples {
		sampleSet[sample] = true
	}

	for _, expected := range expectedSamples {
		if !sampleSet[expected] {
			t.Fatalf("Expected to find sample '%s' in results", expected)
		}
	}
}

func TestGEODownloader_generateSampleFileURLs(t *testing.T) {
	downloader := NewGEODownloader()

	metadata := &downloaders.Metadata{
		Custom: map[string]any{
			"supplementary_files": []string{
				"GSM123456_data.txt.gz",
				"GSM123456_processed.cel.gz",
			},
		},
	}

	urls := downloader.generateSampleFileURLs("GSM123456", metadata)

	// Should contain both generated patterns and metadata-based files
	if len(urls) == 0 {
		t.Fatal("Expected some URLs to be generated")
	}

	// Check for metadata-based files
	expectedFiles := []string{
		"GSM123456_data.txt.gz",
		"GSM123456_processed.cel.gz",
	}

	for _, file := range expectedFiles {
		if _, exists := urls[file]; !exists {
			t.Fatalf("Expected URL for file '%s' to be generated", file)
		}
	}

	// Check URL format
	for filename, url := range urls {
		expectedPattern := "https://ftp.ncbi.nlm.nih.gov/geo/samples/"
		if !strings.HasPrefix(url, expectedPattern) {
			t.Fatalf("Expected URL for '%s' to start with '%s', got '%s'", filename, expectedPattern, url)
		}
	}
}

func TestGEODownloader_Options(t *testing.T) {
	// Test WithTimeout option
	timeout := 60 * time.Second
	downloader := NewGEODownloader(WithTimeout(timeout))

	if downloader.timeout != timeout {
		t.Fatalf("Expected timeout %v, got %v", timeout, downloader.timeout)
	}

	if downloader.client.Timeout != timeout {
		t.Fatalf("Expected client timeout %v, got %v", timeout, downloader.client.Timeout)
	}

	// Test WithVerbose option
	downloader = NewGEODownloader(WithVerbose(true))

	if !downloader.verbose {
		t.Fatal("Expected verbose to be true")
	}

	// Test WithHTTPClient option
	customClient := &http.Client{Timeout: 120 * time.Second}
	downloader = NewGEODownloader(WithHTTPClient(customClient))

	if downloader.client != customClient {
		t.Fatal("Expected custom HTTP client to be set")
	}
}

func TestGEODownloader_getGSESubdir(t *testing.T) {
	downloader := NewGEODownloader()

	tests := []struct {
		input    string
		expected string
	}{
		{"GSE123456", "GSE123nnn"},
		{"GSE12345", "GSE12nnn"},
		{"GSE1234", "GSE1nnn"},
		{"GSE123", "GSE000nnn"},
		{"GSE12", "GSE000nnn"},
		{"GSE1", "GSE000nnn"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := downloader.getGSESubdir(tt.input)
			if result != tt.expected {
				t.Fatalf("getGSESubdir('%s') = '%s', expected '%s'", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGEODownloader_getGSMSubdir(t *testing.T) {
	downloader := NewGEODownloader()

	tests := []struct {
		input    string
		expected string
	}{
		{"GSM123456", "GSM123nnn"},
		{"GSM12345", "GSM12nnn"},
		{"GSM1234", "GSM1nnn"},
		{"GSM123", "GSM000nnn"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := downloader.getGSMSubdir(tt.input)
			if result != tt.expected {
				t.Fatalf("getGSMSubdir('%s') = '%s', expected '%s'", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGEODownloader_shouldDownloadFile(t *testing.T) {
	downloader := NewGEODownloader()

	tests := []struct {
		options  *downloaders.DownloadOptions
		filename string
		expected bool
	}{
		{nil, "test.txt", true},
		{&downloaders.DownloadOptions{IncludeRaw: true}, "raw_data.fastq", true},
		{&downloaders.DownloadOptions{IncludeRaw: false}, "raw_data.fastq", false},
		{&downloaders.DownloadOptions{ExcludeSupplementary: true}, "supplementary.pdf", false},
		{&downloaders.DownloadOptions{ExcludeSupplementary: false}, "supplementary.pdf", true},
		{&downloaders.DownloadOptions{CustomFilters: map[string]string{"extension": ".txt"}}, "data.txt", true},
		{&downloaders.DownloadOptions{CustomFilters: map[string]string{"extension": ".txt"}}, "data.csv", false},
		{&downloaders.DownloadOptions{CustomFilters: map[string]string{"contains": "experiment"}}, "experiment_data.txt", true},
		{&downloaders.DownloadOptions{CustomFilters: map[string]string{"contains": "experiment"}}, "control_data.txt", false},
		{&downloaders.DownloadOptions{CustomFilters: map[string]string{"excludes": "temp"}}, "temp_file.txt", false},
		{&downloaders.DownloadOptions{CustomFilters: map[string]string{"excludes": "temp"}}, "final_file.txt", true},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			result := downloader.shouldDownloadFile(tt.filename, tt.options)
			if result != tt.expected {
				t.Fatalf("shouldDownloadFile('%s', %+v) = %t, expected %t",
					tt.filename, tt.options, result, tt.expected)
			}
		})
	}
}

func TestGEODownloader_isRetryableError(t *testing.T) {
	tests := []struct {
		errorMsg string
		expected bool
	}{
		{"connection timeout", true},
		{"connection reset by peer", true},
		{"temporary failure", true},
		{"network is unreachable", true},
		{"no such host", true},
		{"file not found", false},
		{"permission denied", false},
		{"invalid format", false},
	}

	for _, tt := range tests {
		t.Run(tt.errorMsg, func(t *testing.T) {
			err := &testError{msg: tt.errorMsg}
			result := isRetryableError(err)
			if result != tt.expected {
				t.Fatalf("isRetryableError('%s') = %t, expected %t", tt.errorMsg, result, tt.expected)
			}
		})
	}
}

// testError implements error interface for testing.
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
