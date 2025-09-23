// Package zenodo provides comprehensive tests for Zenodo downloader functionality
// including validation, metadata retrieval, and download operations.
package zenodo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

// createTestRecord creates a test record with proper server URLs.
func createTestRecord(serverURL string) ZenodoRecord {
	return ZenodoRecord{
		ID:           123456,
		DOI:          "10.5281/zenodo.123456",
		ConceptDOI:   "10.5281/zenodo.123455",
		ConceptRecID: 123455,
		Created:      "2023-01-01T12:00:00.000000+00:00",
		Modified:     "2023-01-02T12:00:00.000000+00:00",
		State:        "done",
		Submitted:    true,
		Title:        "Test Dataset",
		Metadata: ZenodoMetadata{
			Title:           "Test Dataset",
			Description:     "A test dataset for unit testing",
			PublicationDate: "2023-01-01",
			AccessRight:     "open",
			License: ZenodoLicense{
				ID:    "cc-by-4.0",
				Title: "Creative Commons Attribution 4.0 International",
				URL:   "https://creativecommons.org/licenses/by/4.0/",
			},
			Creators: []ZenodoCreator{
				{
					Name:        "Test Author",
					Affiliation: "Test University",
					ORCID:       "0000-0000-0000-0000",
				},
			},
			Keywords: []string{"test", "dataset", "example"},
			ResourceType: ZenodoResourceType{
				Type:    "dataset",
				Subtype: "other",
				Title:   "Dataset",
			},
			Version: "1.0.0",
		},
		Files: []ZenodoFile{
			{
				ID:       "file1",
				Key:      "data.csv",
				Size:     900, // "test,data\n" * 100 = 9 * 100 = 900 bytes
				Checksum: "d41d8cd98f00b204e9800998ecf8427e",
				Type:     "csv",
				Links: ZenodoFileLinks{
					Self: serverURL + "/api/files/bucket1/data.csv",
				},
			},
			{
				ID:       "file2",
				Key:      "readme.txt",
				Size:     560, // "This is a test readme file.\n" * 20 = 28 * 20 = 560 bytes
				Checksum: "098f6bcd4621d373cade4e832627b4f6",
				Type:     "txt",
				Links: ZenodoFileLinks{
					Self: serverURL + "/api/files/bucket1/readme.txt",
				},
			},
		},
	}
}

// mockZenodoServer creates a mock Zenodo API server for testing.
func mockZenodoServer() *httptest.Server {
	var server *httptest.Server
	mux := http.NewServeMux()

	// Mock record endpoint
	mux.HandleFunc("/api/records/", func(w http.ResponseWriter, r *http.Request) {
		recordID := strings.TrimPrefix(r.URL.Path, "/api/records/")

		switch recordID {
		case "123456":
			record := createTestRecord(server.URL)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(record)

		case "404":
			http.NotFound(w, r)

		case "403":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message": "Access denied"}`))

		default:
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"message": "Internal server error"}`))
		}
	})

	// Mock file download endpoints
	mux.HandleFunc("/api/files/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "data.csv") {
			content := strings.Repeat("test,data\n", 100)
			w.Header().Set("Content-Type", "text/csv")
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			w.Write([]byte(content))
		} else if strings.Contains(r.URL.Path, "readme.txt") {
			content := strings.Repeat("This is a test readme file.\n", 20)
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			w.Write([]byte(content))
		} else {
			http.NotFound(w, r)
		}
	})

	server = httptest.NewServer(mux)
	return server
}

func TestNewZenodoDownloader(t *testing.T) {
	tests := []struct {
		name    string
		options []Option
		want    string
	}{
		{
			name:    "default configuration",
			options: nil,
			want:    "zenodo",
		},
		{
			name:    "with verbose option",
			options: []Option{WithVerbose(true)},
			want:    "zenodo",
		},
		{
			name:    "with timeout option",
			options: []Option{WithTimeout(60 * time.Second)},
			want:    "zenodo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			downloader := NewZenodoDownloader(tt.options...)
			if downloader.GetSourceType() != tt.want {
				t.Errorf("GetSourceType() = %v, want %v", downloader.GetSourceType(), tt.want)
			}
		})
	}
}

func TestZenodoDownloader_CleanZenodoID(t *testing.T) {
	downloader := NewZenodoDownloader()

	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "direct record ID",
			input:    "123456",
			expected: "123456",
			wantErr:  false,
		},
		{
			name:     "Zenodo URL",
			input:    "https://zenodo.org/record/123456",
			expected: "123456",
			wantErr:  false,
		},
		{
			name:     "Zenodo DOI",
			input:    "10.5281/zenodo.123456",
			expected: "123456",
			wantErr:  false,
		},
		{
			name:     "DOI URL",
			input:    "https://doi.org/10.5281/zenodo.123456",
			expected: "123456",
			wantErr:  false,
		},
		{
			name:     "versioned DOI",
			input:    "10.5281/zenodo.123456.v1",
			expected: "123456",
			wantErr:  false,
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "invalid format",
			input:    "not-a-zenodo-id",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "non-zenodo DOI",
			input:    "10.1234/example.123",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "deposit URL (should fail)",
			input:    "https://zenodo.org/deposit/123456",
			expected: "",
			wantErr:  true,
		},
		{
			name:     "community URL (should fail)",
			input:    "https://zenodo.org/communities/test-community",
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := downloader.cleanZenodoID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("cleanZenodoID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if result != tt.expected {
				t.Errorf("cleanZenodoID() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestZenodoDownloader_ParseZenodoIdentifier(t *testing.T) {
	downloader := NewZenodoDownloader()

	tests := []struct {
		name         string
		input        string
		expectedID   string
		expectedType ZenodoArtifactType
		isVersioned  bool
		wantErr      bool
	}{
		{
			name:         "direct record ID",
			input:        "123456",
			expectedID:   "123456",
			expectedType: ArtifactTypeRecord,
			isVersioned:  false,
			wantErr:      false,
		},
		{
			name:         "record URL",
			input:        "https://zenodo.org/record/123456",
			expectedID:   "123456",
			expectedType: ArtifactTypeRecord,
			isVersioned:  false,
			wantErr:      false,
		},
		{
			name:         "deposit URL",
			input:        "https://zenodo.org/deposit/789012",
			expectedID:   "789012",
			expectedType: ArtifactTypeDeposit,
			isVersioned:  false,
			wantErr:      false,
		},
		{
			name:         "community URL",
			input:        "https://zenodo.org/communities/test-community",
			expectedID:   "test-community",
			expectedType: ArtifactTypeCommunity,
			isVersioned:  false,
			wantErr:      false,
		},
		{
			name:         "versioned DOI",
			input:        "10.5281/zenodo.123456.v1",
			expectedID:   "123456",
			expectedType: ArtifactTypeConcept,
			isVersioned:  true,
			wantErr:      false,
		},
		{
			name:         "regular DOI",
			input:        "10.5281/zenodo.123456",
			expectedID:   "123456",
			expectedType: ArtifactTypeRecord,
			isVersioned:  false,
			wantErr:      false,
		},
		{
			name:         "invalid format",
			input:        "not-a-zenodo-id",
			expectedID:   "",
			expectedType: ArtifactTypeUnknown,
			isVersioned:  false,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := downloader.parseZenodoIdentifier(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseZenodoIdentifier() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if result.ID != tt.expectedID {
					t.Errorf("parseZenodoIdentifier() ID = %v, want %v", result.ID, tt.expectedID)
				}
				if result.Type != tt.expectedType {
					t.Errorf("parseZenodoIdentifier() Type = %v, want %v", result.Type, tt.expectedType)
				}
				if result.IsVersioned != tt.isVersioned {
					t.Errorf("parseZenodoIdentifier() IsVersioned = %v, want %v", result.IsVersioned, tt.isVersioned)
				}
				if result.OriginalText != tt.input {
					t.Errorf("parseZenodoIdentifier() OriginalText = %v, want %v", result.OriginalText, tt.input)
				}
			}
		})
	}
}

func TestZenodoDownloader_Validate(t *testing.T) {
	server := mockZenodoServer()
	defer server.Close()

	downloader := NewZenodoDownloader()
	downloader.apiURL = server.URL + "/api/records"

	tests := []struct {
		name        string
		id          string
		wantValid   bool
		wantErr     bool
		wantWarning bool
	}{
		{
			name:      "valid record ID",
			id:        "123456",
			wantValid: true,
			wantErr:   false,
		},
		{
			name:      "valid Zenodo URL",
			id:        "https://zenodo.org/record/123456",
			wantValid: true,
			wantErr:   false,
		},
		{
			name:      "not found record",
			id:        "404",
			wantValid: false,
			wantErr:   false,
		},
		{
			name:      "access denied record",
			id:        "403",
			wantValid: false,
			wantErr:   false,
		},
		{
			name:      "invalid format",
			id:        "invalid-id",
			wantValid: false,
			wantErr:   false,
		},
		{
			name:        "deposit URL",
			id:          "https://zenodo.org/deposit/123456",
			wantValid:   false,
			wantErr:     false,
			wantWarning: true,
		},
		{
			name:        "community URL",
			id:          "https://zenodo.org/communities/test-community",
			wantValid:   false,
			wantErr:     false,
			wantWarning: true,
		},
		{
			name:        "versioned DOI",
			id:          "10.5281/zenodo.123456.v1",
			wantValid:   true,
			wantErr:     false,
			wantWarning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := downloader.Validate(ctx, tt.id)

			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if result.Valid != tt.wantValid {
				t.Errorf("Validate() valid = %v, want %v", result.Valid, tt.wantValid)
			}

			if result.SourceType != "zenodo" {
				t.Errorf("Validate() source type = %v, want zenodo", result.SourceType)
			}

			if tt.wantWarning && len(result.Warnings) == 0 {
				t.Errorf("Validate() expected warnings but got none")
			}
		})
	}
}

func TestZenodoDownloader_GetMetadata(t *testing.T) {
	server := mockZenodoServer()
	defer server.Close()

	downloader := NewZenodoDownloader()
	downloader.apiURL = server.URL + "/api/records"

	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{
			name:    "valid record ID",
			id:      "123456",
			wantErr: false,
		},
		{
			name:    "not found record",
			id:      "404",
			wantErr: true,
		},
		{
			name:    "invalid format",
			id:      "invalid-id",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			metadata, err := downloader.GetMetadata(ctx, tt.id)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetMetadata() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if metadata.Source != "zenodo" {
					t.Errorf("GetMetadata() source = %v, want zenodo", metadata.Source)
				}
				if metadata.Title != "Test Dataset" {
					t.Errorf("GetMetadata() title = %v, want Test Dataset", metadata.Title)
				}
				if metadata.DOI != "10.5281/zenodo.123456" {
					t.Errorf("GetMetadata() DOI = %v, want 10.5281/zenodo.123456", metadata.DOI)
				}
				if len(metadata.Authors) != 1 || metadata.Authors[0] != "Test Author" {
					t.Errorf("GetMetadata() authors = %v, want [Test Author]", metadata.Authors)
				}
				if metadata.FileCount != 2 {
					t.Errorf("GetMetadata() file count = %v, want 2", metadata.FileCount)
				}
				if metadata.TotalSize != 1460 { // 900 + 560
					t.Errorf("GetMetadata() total size = %v, want 1460", metadata.TotalSize)
				}
			}
		})
	}
}

func TestZenodoDownloader_Download(t *testing.T) {
	server := mockZenodoServer()
	defer server.Close()

	downloader := NewZenodoDownloader()
	downloader.apiURL = server.URL + "/api/records"
	downloader.baseURL = server.URL

	// Create temporary directory for test downloads
	tempDir := t.TempDir()

	tests := []struct {
		name          string
		request       *downloaders.DownloadRequest
		wantSuccess   bool
		wantFileCount int
	}{
		{
			name: "successful download",
			request: &downloaders.DownloadRequest{
				ID:        "123456",
				OutputDir: tempDir,
				Options: &downloaders.DownloadOptions{
					IncludeRaw:           true,
					ExcludeSupplementary: false,
					MaxConcurrent:        1,
				},
			},
			wantSuccess:   true,
			wantFileCount: 2,
		},
		{
			name: "exclude supplementary files",
			request: &downloaders.DownloadRequest{
				ID:        "123456",
				OutputDir: filepath.Join(tempDir, "no-supp"),
				Options: &downloaders.DownloadOptions{
					IncludeRaw:           true,
					ExcludeSupplementary: true,
					MaxConcurrent:        1,
				},
			},
			wantSuccess:   true,
			wantFileCount: 1, // readme.txt should be excluded
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := downloader.Download(ctx, tt.request)

			if err != nil {
				t.Errorf("Download() error = %v", err)
				return
			}

			if result.Success != tt.wantSuccess {
				t.Errorf("Download() success = %v, want %v", result.Success, tt.wantSuccess)
				if len(result.Errors) > 0 {
					t.Logf("Download errors: %v", result.Errors)
				}
			}

			if len(result.Files) != tt.wantFileCount {
				t.Errorf("Download() file count = %v, want %v", len(result.Files), tt.wantFileCount)
				if len(result.Errors) > 0 {
					t.Logf("Download errors: %v", result.Errors)
				}
			}

			if tt.wantSuccess {
				// Check that files were actually downloaded
				for _, file := range result.Files {
					if _, err := os.Stat(file.Path); os.IsNotExist(err) {
						t.Errorf("Downloaded file does not exist: %s", file.Path)
					}
				}

				// Check witness file
				witnessPath := filepath.Join(tt.request.OutputDir, "hapiq.json")
				if _, err := os.Stat(witnessPath); os.IsNotExist(err) {
					t.Errorf("Witness file was not created: %s", witnessPath)
				}
			}
		})
	}
}

func TestZenodoDownloader_ShouldDownloadFile(t *testing.T) {
	downloader := NewZenodoDownloader()

	tests := []struct {
		name     string
		file     ZenodoFile
		options  *downloaders.DownloadOptions
		expected bool
	}{
		{
			name: "include all files",
			file: ZenodoFile{
				Key:  "data.csv",
				Size: 1024,
			},
			options:  nil,
			expected: true,
		},
		{
			name: "exclude raw data",
			file: ZenodoFile{
				Key:  "raw_data.fastq",
				Size: 1024,
			},
			options: &downloaders.DownloadOptions{
				IncludeRaw: false,
			},
			expected: false,
		},
		{
			name: "exclude supplementary files",
			file: ZenodoFile{
				Key:  "readme.txt",
				Size: 512,
			},
			options: &downloaders.DownloadOptions{
				ExcludeSupplementary: true,
			},
			expected: false,
		},
		{
			name: "custom filter - extension",
			file: ZenodoFile{
				Key:  "data.csv",
				Size: 1024,
			},
			options: &downloaders.DownloadOptions{
				CustomFilters: map[string]string{
					"extension": ".csv",
				},
			},
			expected: true,
		},
		{
			name: "custom filter - max size",
			file: ZenodoFile{
				Key:  "large_file.dat",
				Size: 2048,
			},
			options: &downloaders.DownloadOptions{
				CustomFilters: map[string]string{
					"max_size": "1024",
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

func TestZenodoDownloader_FilterFiles(t *testing.T) {
	downloader := NewZenodoDownloader()

	files := []ZenodoFile{
		{Key: "data.csv", Size: 1024, Type: "csv"},
		{Key: "raw_data.fastq", Size: 2048, Type: "fastq"},
		{Key: "readme.txt", Size: 512, Type: "txt"},
		{Key: "metadata.json", Size: 256, Type: "json"},
	}

	tests := []struct {
		name     string
		options  *downloaders.DownloadOptions
		expected int
	}{
		{
			name:     "no filters",
			options:  nil,
			expected: 4,
		},
		{
			name: "exclude raw data",
			options: &downloaders.DownloadOptions{
				IncludeRaw: false,
			},
			expected: 3, // excludes raw_data.fastq
		},
		{
			name: "exclude supplementary",
			options: &downloaders.DownloadOptions{
				ExcludeSupplementary: true,
			},
			expected: 1, // excludes readme.txt and metadata.json
		},
		{
			name: "both exclusions",
			options: &downloaders.DownloadOptions{
				IncludeRaw:           false,
				ExcludeSupplementary: true,
			},
			expected: 1, // only data.csv remains
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := downloader.filterFiles(files, tt.options)
			if len(filtered) != tt.expected {
				t.Errorf("filterFiles() returned %d files, want %d", len(filtered), tt.expected)
			}
		})
	}
}

func TestZenodoDownloader_ConvertToMetadata(t *testing.T) {
	downloader := NewZenodoDownloader()

	record := &ZenodoRecord{
		ID:           123456,
		DOI:          "10.5281/zenodo.123456",
		ConceptDOI:   "10.5281/zenodo.123455",
		ConceptRecID: 123455,
		Created:      "2023-01-01T12:00:00.000000+00:00",
		Modified:     "2023-01-02T12:00:00.000000+00:00",
		State:        "done",
		Submitted:    true,
		Metadata: ZenodoMetadata{
			Title:           "Test Dataset",
			Description:     "A test dataset",
			PublicationDate: "2023-01-01",
			Version:         "1.0.0",
			License: ZenodoLicense{
				ID:    "cc-by-4.0",
				Title: "Creative Commons Attribution 4.0",
			},
			Creators: []ZenodoCreator{
				{Name: "Test Author"},
			},
			Keywords: []string{"test", "dataset"},
			ResourceType: ZenodoResourceType{
				Type: "dataset",
			},
		},
		Files: []ZenodoFile{
			{Key: "data.csv", Size: 900},
		},
	}

	metadata := downloader.convertToMetadata(record, "123456")

	if metadata.Source != "zenodo" {
		t.Errorf("Source = %v, want zenodo", metadata.Source)
	}
	if metadata.ID != "123456" {
		t.Errorf("ID = %v, want 123456", metadata.ID)
	}
	if metadata.Title != "Test Dataset" {
		t.Errorf("Title = %v, want Test Dataset", metadata.Title)
	}
	if metadata.DOI != "10.5281/zenodo.123456" {
		t.Errorf("DOI = %v, want 10.5281/zenodo.123456", metadata.DOI)
	}
	if metadata.FileCount != 1 {
		t.Errorf("FileCount = %v, want 1", metadata.FileCount)
	}
	if metadata.TotalSize != 900 {
		t.Errorf("TotalSize = %v, want 900", metadata.TotalSize)
	}
	if len(metadata.Authors) != 1 || metadata.Authors[0] != "Test Author" {
		t.Errorf("Authors = %v, want [Test Author]", metadata.Authors)
	}
}

func BenchmarkZenodoDownloader_CleanZenodoID(b *testing.B) {
	downloader := NewZenodoDownloader()
	testCases := []string{
		"123456",
		"https://zenodo.org/record/123456",
		"10.5281/zenodo.123456",
		"https://doi.org/10.5281/zenodo.123456",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, tc := range testCases {
			_, _ = downloader.cleanZenodoID(tc)
		}
	}
}

func BenchmarkZenodoDownloader_ParseZenodoIdentifier(b *testing.B) {
	downloader := NewZenodoDownloader()
	testCases := []string{
		"123456",
		"https://zenodo.org/record/123456",
		"https://zenodo.org/deposit/789012",
		"https://zenodo.org/communities/test-community",
		"10.5281/zenodo.123456",
		"10.5281/zenodo.123456.v1",
		"https://doi.org/10.5281/zenodo.123456",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, tc := range testCases {
			_, _ = downloader.parseZenodoIdentifier(tc)
		}
	}
}

func BenchmarkZenodoDownloader_ShouldDownloadFile(b *testing.B) {
	downloader := NewZenodoDownloader()
	file := ZenodoFile{
		Key:  "test_file.csv",
		Size: 1024,
	}
	options := &downloaders.DownloadOptions{
		IncludeRaw:           true,
		ExcludeSupplementary: false,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = downloader.shouldDownloadFile(file, options)
	}
}
