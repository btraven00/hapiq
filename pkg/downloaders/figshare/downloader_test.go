// Package figshare provides unit tests for the Figshare downloader implementation
// testing article metadata extraction, validation, and download functionality.
package figshare

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

func TestFigshareDownloader_GetSourceType(t *testing.T) {
	downloader := NewFigshareDownloader()
	if got := downloader.GetSourceType(); got != "figshare" {
		t.Errorf("GetSourceType() = %v, want %v", got, "figshare")
	}
}

func TestFigshareDownloader_Validate(t *testing.T) {
	downloader := NewFigshareDownloader()
	ctx := context.Background()

	tests := []struct {
		name      string
		id        string
		wantValid bool
		wantError bool
	}{
		{
			name:      "valid numeric ID",
			id:        "12345678",
			wantValid: true,
		},
		{
			name:      "valid small ID",
			id:        "123",
			wantValid: true,
		},
		{
			name:      "invalid empty string",
			id:        "",
			wantValid: false,
		},
		{
			name:      "valid with cleanup - extracts number",
			id:        "abc123",
			wantValid: true,
		},
		{
			name:      "valid with cleanup - removes negative sign",
			id:        "-123",
			wantValid: true,
		},
		{
			name:      "valid with URL cleanup",
			id:        "https://figshare.com/articles/12345678",
			wantValid: true,
		},
		{
			name:      "zero ID",
			id:        "0",
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := downloader.Validate(ctx, tt.id)
			if tt.wantError && err == nil {
				t.Errorf("Validate() expected error but got none")
				return
			}
			if !tt.wantError && err != nil {
				t.Errorf("Validate() unexpected error: %v", err)
				return
			}
			if result.Valid != tt.wantValid {
				t.Errorf("Validate() Valid = %v, want %v", result.Valid, tt.wantValid)
			}
		})
	}
}

func TestFigshareDownloader_cleanFigshareID(t *testing.T) {
	downloader := NewFigshareDownloader()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple numeric ID",
			input:    "12345678",
			expected: "12345678",
		},
		{
			name:     "ID with whitespace",
			input:    "  12345678  ",
			expected: "12345678",
		},
		{
			name:     "figshare URL",
			input:    "https://figshare.com/articles/dataset/test/12345678",
			expected: "12345678",
		},
		{
			name:     "figshare URL with version",
			input:    "https://figshare.com/articles/dataset/test/12345678/1",
			expected: "12345678",
		},
		{
			name:     "URL with prefix",
			input:    "figshare:12345678",
			expected: "12345678",
		},
		{
			name:     "mixed alphanumeric",
			input:    "abc12345def",
			expected: "12345",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := downloader.cleanFigshareID(tt.input)
			if result != tt.expected {
				t.Errorf("cleanFigshareID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFigshareDownloader_isSharingURL(t *testing.T) {
	downloader := NewFigshareDownloader()

	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{
			name:     "valid sharing URL",
			url:      "https://figshare.com/s/865e694ad06d5857db4b",
			expected: true,
		},
		{
			name:     "valid sharing URL without https",
			url:      "figshare.com/s/abc123def456",
			expected: true,
		},
		{
			name:     "regular article URL",
			url:      "https://figshare.com/articles/dataset/title/12345678",
			expected: false,
		},
		{
			name:     "collection URL",
			url:      "https://figshare.com/collections/title/12345678",
			expected: false,
		},
		{
			name:     "simple ID",
			url:      "12345678",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := downloader.isSharingURL(tt.url)
			if result != tt.expected {
				t.Errorf("isSharingURL(%q) = %v, want %v", tt.url, result, tt.expected)
			}
		})
	}
}

func TestFigshareDownloader_resolveSharingURL(t *testing.T) {
	// Create a mock server that simulates the sharing page
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a figshare sharing page with article ID embedded
		html := `
		<html>
		<body>
			<div>MCA DGE Data</div>
			<a href="https://figshare.com/ndownloader/articles/5435866/versions/8">Download all (1.27 GB)</a>
			<script>
			window.__APOLLO_STATE__ = {
				"PrivateLink:75151": {
					"article": {"id": 5435866},
					"title": "MCA DGE Data"
				}
			};
			</script>
		</body>
		</html>`
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	}))
	defer server.Close()

	// Create downloader with custom client that redirects to our mock
	client := &http.Client{
		Transport: &mockSharingTransport{server: server},
	}
	downloader := NewFigshareDownloader(WithHTTPClient(client), WithVerbose(true))

	// Test resolving a sharing URL
	articleID, err := downloader.resolveSharingURL("https://figshare.com/s/865e694ad06d5857db4b")
	if err != nil {
		t.Fatalf("resolveSharingURL() error = %v", err)
	}

	expected := "5435866"
	if articleID != expected {
		t.Errorf("resolveSharingURL() = %q, want %q", articleID, expected)
	}
}

// mockSharingTransport redirects sharing URL requests to our test server
type mockSharingTransport struct {
	server *httptest.Server
}

func (t *mockSharingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Redirect sharing URL calls to our mock server
	if strings.Contains(req.URL.Path, "/s/") {
		// Parse the mock server URL to get host and port
		mockURL := strings.TrimPrefix(t.server.URL, "http://")
		req.URL.Scheme = "http"
		req.URL.Host = mockURL
		req.URL.Path = "/"
	}

	return http.DefaultTransport.RoundTrip(req)
}

func TestFigshareDownloader_buildFigshareURL(t *testing.T) {
	downloader := NewFigshareDownloader()

	tests := []struct {
		id       string
		expected string
	}{
		{"12345678", "https://figshare.com/articles/12345678"},
		{"123", "https://figshare.com/articles/123"},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			result := downloader.buildFigshareURL(tt.id)
			if result != tt.expected {
				t.Errorf("buildFigshareURL(%q) = %q, want %q", tt.id, result, tt.expected)
			}
		})
	}
}

func TestFigshareDownloader_GetMetadata_MockAPI(t *testing.T) {
	// Mock Figshare API response for article
	mockArticleResponse := `{
		"id": 12978275,
		"title": "Test Dataset",
		"description": "This is a test dataset for unit testing",
		"doi": "10.6084/m9.figshare.12978275.v3",
		"license": {
			"value": 1,
			"name": "CC BY 4.0",
			"url": "https://creativecommons.org/licenses/by/4.0/"
		},
		"authors": [
			{
				"id": 123456,
				"full_name": "Test Author",
				"first_name": "Test",
				"last_name": "Author",
				"email": "",
				"orcid_id": ""
			}
		],
		"categories": [
			{
				"id": 26488,
				"title": "Computer Science",
				"parent_id": 26485
			}
		],
		"tags": ["test", "dataset"],
		"keywords": ["testing", "unit test"],
		"references": [],
		"files": [
			{
				"id": 24726800,
				"name": "test_data.csv",
				"size": 18182,
				"download_url": "https://ndownloader.figshare.com/files/24726800",
				"computed_md5": "c1dbd3309b951bf49535bf627ee67e82",
				"mimetype": "text/plain",
				"is_link_only": false
			}
		],
		"created_date": "2020-09-19T09:12:21Z",
		"modified_date": "2022-02-02T10:11:27Z",
		"published_date": "2020-09-19T09:12:21Z",
		"version": 3,
		"resource_title": "",
		"resource_doi": "",
		"custom_fields": []
	}`

	// Create a mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "articles") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(mockArticleResponse))
		} else {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message": "Entity not found", "code": "EntityNotFound"}`))
		}
	}))
	defer server.Close()

	// Create downloader with custom client that redirects to our mock
	client := &http.Client{
		Transport: &mockFigshareTransport{server: server},
		Timeout:   30 * time.Second,
	}

	downloader := NewFigshareDownloader(
		WithHTTPClient(client),
		WithVerbose(false),
	)

	// Test metadata retrieval
	ctx := context.Background()
	metadata, err := downloader.GetMetadata(ctx, "12978275")
	if err != nil {
		t.Fatalf("GetMetadata() failed: %v", err)
	}

	// Verify metadata content
	if metadata.Source != "figshare" {
		t.Errorf("Source = %q, want %q", metadata.Source, "figshare")
	}
	if metadata.ID != "12978275" {
		t.Errorf("ID = %q, want %q", metadata.ID, "12978275")
	}
	if metadata.Title != "Test Dataset" {
		t.Errorf("Title = %q, want %q", metadata.Title, "Test Dataset")
	}
	if metadata.DOI != "10.6084/m9.figshare.12978275.v3" {
		t.Errorf("DOI = %q, want %q", metadata.DOI, "10.6084/m9.figshare.12978275.v3")
	}
	if metadata.License != "CC BY 4.0" {
		t.Errorf("License = %q, want %q", metadata.License, "CC BY 4.0")
	}
	if len(metadata.Authors) != 1 || metadata.Authors[0] != "Test Author" {
		t.Errorf("Authors = %v, want %v", metadata.Authors, []string{"Test Author"})
	}
	if metadata.FileCount != 1 {
		t.Errorf("FileCount = %d, want %d", metadata.FileCount, 1)
	}
	if metadata.TotalSize != 18182 {
		t.Errorf("TotalSize = %d, want %d", metadata.TotalSize, 18182)
	}
	if len(metadata.Tags) != 2 {
		t.Errorf("Tags count = %d, want %d", len(metadata.Tags), 2)
	}
	if metadata.Version != "3" {
		t.Errorf("Version = %q, want %q", metadata.Version, "3")
	}
}

func TestFigshareDownloader_parseFigshareDate(t *testing.T) {
	downloader := NewFigshareDownloader()

	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{
			name:      "ISO 8601 with Z",
			input:     "2020-09-19T09:12:21Z",
			wantError: false,
		},
		{
			name:      "ISO 8601 with milliseconds",
			input:     "2020-09-19T09:12:21.123Z",
			wantError: false,
		},
		{
			name:      "ISO 8601 with timezone",
			input:     "2020-09-19T09:12:21-05:00",
			wantError: false,
		},
		{
			name:      "simple format",
			input:     "2020-09-19 09:12:21",
			wantError: false,
		},
		{
			name:      "date only",
			input:     "2020-09-19",
			wantError: false,
		},
		{
			name:      "empty string",
			input:     "",
			wantError: true,
		},
		{
			name:      "invalid date",
			input:     "invalid-date",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := downloader.parseFigshareDate(tt.input)
			if tt.wantError && err == nil {
				t.Errorf("parseFigshareDate(%q) expected error but got none", tt.input)
			}
			if !tt.wantError && err != nil {
				t.Errorf("parseFigshareDate(%q) unexpected error: %v", tt.input, err)
			}
		})
	}
}

func TestFigshareDownloader_shouldDownloadFile(t *testing.T) {
	downloader := NewFigshareDownloader()

	file := FigshareFile{
		Name:       "test_data.csv",
		Size:       1000,
		MimeType:   "text/csv",
		IsLinkOnly: false,
	}

	tests := []struct {
		name     string
		file     FigshareFile
		options  *downloaders.DownloadOptions
		expected bool
	}{
		{
			name:     "no options",
			file:     file,
			options:  nil,
			expected: true,
		},
		{
			name: "exclude supplementary - not supplementary",
			file: file,
			options: &downloaders.DownloadOptions{
				ExcludeSupplementary: true,
			},
			expected: true,
		},
		{
			name: "exclude supplementary - is supplementary",
			file: FigshareFile{
				Name:       "supplementary_data.csv",
				Size:       1000,
				MimeType:   "text/csv",
				IsLinkOnly: false,
			},
			options: &downloaders.DownloadOptions{
				ExcludeSupplementary: true,
			},
			expected: false,
		},
		{
			name: "link only file",
			file: FigshareFile{
				Name:       "external_link.txt",
				Size:       1000,
				MimeType:   "text/plain",
				IsLinkOnly: true,
			},
			options:  nil,
			expected: false,
		},
		{
			name: "custom filter - extension match",
			file: file,
			options: &downloaders.DownloadOptions{
				CustomFilters: map[string]string{
					"extension": ".csv",
				},
			},
			expected: true,
		},
		{
			name: "custom filter - extension no match",
			file: file,
			options: &downloaders.DownloadOptions{
				CustomFilters: map[string]string{
					"extension": ".txt",
				},
			},
			expected: false,
		},
		{
			name: "custom filter - max size",
			file: file,
			options: &downloaders.DownloadOptions{
				CustomFilters: map[string]string{
					"max_size": "2000",
				},
			},
			expected: true,
		},
		{
			name: "custom filter - max size exceeded",
			file: file,
			options: &downloaders.DownloadOptions{
				CustomFilters: map[string]string{
					"max_size": "500",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := downloader.shouldDownloadFile(tt.file, tt.options)
			if result != tt.expected {
				t.Errorf("shouldDownloadFile() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestFigshareDownloader_Options(t *testing.T) {
	// Test timeout option
	timeout := 60 * time.Second
	downloader := NewFigshareDownloader(WithTimeout(timeout))
	if downloader.timeout != timeout {
		t.Errorf("WithTimeout() timeout = %v, want %v", downloader.timeout, timeout)
	}

	// Test verbose option
	downloader = NewFigshareDownloader(WithVerbose(true))
	if !downloader.verbose {
		t.Errorf("WithVerbose(true) verbose = %v, want %v", downloader.verbose, true)
	}

	// Test custom client option
	customClient := &http.Client{Timeout: 120 * time.Second}
	downloader = NewFigshareDownloader(WithHTTPClient(customClient))
	if downloader.client != customClient {
		t.Errorf("WithHTTPClient() client mismatch")
	}
}

func TestFigshareDownloader_determineDatasetType(t *testing.T) {
	downloader := NewFigshareDownloader()

	tests := []struct {
		name     string
		metadata *downloaders.Metadata
		expected string
	}{
		{
			name: "article - no collections",
			metadata: &downloaders.Metadata{
				Collections: []downloaders.Collection{},
			},
			expected: "article",
		},
		{
			name: "collection type",
			metadata: &downloaders.Metadata{
				Collections: []downloaders.Collection{
					{Type: "figshare_collection"},
				},
			},
			expected: "collection",
		},
		{
			name: "project type",
			metadata: &downloaders.Metadata{
				Collections: []downloaders.Collection{
					{Type: "figshare_project"},
				},
			},
			expected: "project",
		},
		{
			name: "unknown collection type",
			metadata: &downloaders.Metadata{
				Collections: []downloaders.Collection{
					{Type: "unknown_type"},
				},
			},
			expected: "article",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := downloader.determineDatasetType(tt.metadata)
			if result != tt.expected {
				t.Errorf("determineDatasetType() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// mockFigshareTransport redirects Figshare API requests to our test server
type mockFigshareTransport struct {
	server *httptest.Server
}

func (t *mockFigshareTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Redirect Figshare API calls to our mock server
	if strings.Contains(req.URL.Host, "api.figshare.com") {
		// Parse the mock server URL to get host and port
		mockURL := strings.TrimPrefix(t.server.URL, "http://")
		req.URL.Scheme = "http"
		req.URL.Host = mockURL
	}

	return http.DefaultTransport.RoundTrip(req)
}
