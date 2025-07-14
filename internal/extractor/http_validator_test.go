package extractor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewHTTPValidator(t *testing.T) {
	validator := NewHTTPValidator(10 * time.Second)

	if validator == nil {
		t.Fatal("NewHTTPValidator returned nil")
	}

	if validator.client == nil {
		t.Error("HTTP client not initialized")
	}

	if validator.userAgent == "" {
		t.Error("User agent not set")
	}

	// Test that user agent looks like a real browser
	if !strings.Contains(validator.userAgent, "Mozilla") {
		t.Error("User agent doesn't look like a browser")
	}
}

func TestHTTPValidator_ValidateURL_Success(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify browser-like headers are present
		if !strings.Contains(r.Header.Get("User-Agent"), "Mozilla") {
			t.Error("Missing or invalid User-Agent header")
		}

		if r.Header.Get("Accept") == "" {
			t.Error("Missing Accept header")
		}

		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Length", "1048576") // 1MB
		w.Header().Set("Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test,data\n1,2\n"))
	}))
	defer server.Close()

	validator := NewHTTPValidator(5 * time.Second)
	ctx := context.Background()

	result, err := validator.ValidateURL(ctx, server.URL)

	if err != nil {
		t.Fatalf("ValidateURL failed: %v", err)
	}

	if !result.Accessible {
		t.Error("Expected URL to be accessible")
	}

	if result.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", result.StatusCode)
	}

	if result.ContentType != "text/csv" {
		t.Errorf("Expected content type text/csv, got %s", result.ContentType)
	}

	if result.ContentLength != 1048576 {
		t.Errorf("Expected content length 1048576, got %d", result.ContentLength)
	}

	if !result.IsDataset {
		t.Error("Expected CSV to be classified as dataset")
	}

	if result.DatasetScore < 0.8 {
		t.Errorf("Expected high dataset score for CSV, got %.2f", result.DatasetScore)
	}

	if result.LastModified == "" {
		t.Error("Expected Last-Modified header to be captured")
	}
}

func TestHTTPValidator_ValidateURL_Redirects(t *testing.T) {
	// Create redirect chain: /start -> /middle -> /final
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/middle", http.StatusFound)
		case "/middle":
			http.Redirect(w, r, "/final", http.StatusFound)
		case "/final":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"data": "final"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	validator := NewHTTPValidator(5 * time.Second)
	ctx := context.Background()

	result, err := validator.ValidateURL(ctx, server.URL+"/start")

	if err != nil {
		t.Fatalf("ValidateURL failed: %v", err)
	}

	if !result.Accessible {
		t.Error("Expected redirected URL to be accessible")
	}

	if result.FinalURL != server.URL+"/final" {
		t.Errorf("Expected final URL %s/final, got %s", server.URL, result.FinalURL)
	}

	if len(result.RedirectChain) != 2 {
		t.Errorf("Expected redirect chain length 2, got %d", len(result.RedirectChain))
	}
}

func TestHTTPValidator_ValidateURL_HeadFirst(t *testing.T) {
	methodUsed := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methodUsed = r.Method

		if r.Method == "HEAD" {
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Length", "52428800") // 50MB
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	validator := NewHTTPValidator(5 * time.Second)
	ctx := context.Background()

	result, err := validator.ValidateURL(ctx, server.URL)

	if err != nil {
		t.Fatalf("ValidateURL failed: %v", err)
	}

	if methodUsed != "HEAD" {
		t.Errorf("Expected HEAD request, but server received %s", methodUsed)
	}

	if result.RequestMethod != "HEAD" {
		t.Errorf("Expected result to show HEAD method, got %s", result.RequestMethod)
	}

	if !result.Accessible {
		t.Error("Expected HEAD request to succeed")
	}

	if result.ContentType != "application/zip" {
		t.Errorf("Expected application/zip, got %s", result.ContentType)
	}
}

func TestHTTPValidator_ValidateURL_HeadFailsGetSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if r.Method == "GET" {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("test data"))
			return
		}

		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer server.Close()

	validator := NewHTTPValidator(5 * time.Second)
	ctx := context.Background()

	result, err := validator.ValidateURL(ctx, server.URL)

	if err != nil {
		t.Fatalf("ValidateURL failed: %v", err)
	}

	if !result.Accessible {
		t.Error("Expected GET fallback to succeed")
	}

	if !strings.Contains(result.RequestMethod, "GET") {
		t.Errorf("Expected GET method in result, got %s", result.RequestMethod)
	}
}

func TestHTTPValidator_ValidateURL_404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not Found"))
	}))
	defer server.Close()

	validator := NewHTTPValidator(5 * time.Second)
	ctx := context.Background()

	result, err := validator.ValidateURL(ctx, server.URL)

	if err != nil {
		t.Fatalf("ValidateURL failed: %v", err)
	}

	if result.Accessible {
		t.Error("Expected 404 to be marked as not accessible")
	}

	if result.StatusCode != 404 {
		t.Errorf("Expected status 404, got %d", result.StatusCode)
	}
}

func TestHTTPValidator_ValidateURL_InvalidURL(t *testing.T) {
	validator := NewHTTPValidator(5 * time.Second)
	ctx := context.Background()

	result, err := validator.ValidateURL(ctx, "not-a-valid-url")

	if err != nil {
		t.Fatalf("ValidateURL should not return error for invalid URL, got %v", err)
	}

	if result.Error == "" {
		t.Error("Expected error message for invalid URL")
	}

	if result.Accessible {
		t.Error("Invalid URL should not be accessible")
	}
}

func TestHTTPValidator_ValidateURL_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	validator := NewHTTPValidator(500 * time.Millisecond) // Short timeout
	ctx := context.Background()

	start := time.Now()
	result, err := validator.ValidateURL(ctx, server.URL)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("ValidateURL failed: %v", err)
	}

	if result.Accessible {
		t.Error("Expected timeout to make URL inaccessible")
	}

	// Allow some margin for timeout handling - the client tries multiple methods
	if duration > 2*time.Second {
		t.Errorf("Expected timeout within 2 seconds, took %v", duration)
	}
}

func TestAnalyzeDatasetLikelihood(t *testing.T) {
	validator := NewHTTPValidator(5 * time.Second)

	testCases := []struct {
		name          string
		contentType   string
		url           string
		contentLength int64
		expectedScore float64
		expectDataset bool
	}{
		{
			name:          "CSV file",
			contentType:   "text/csv",
			url:           "https://example.com/data.csv",
			contentLength: 1024 * 1024, // 1MB
			expectedScore: 0.9,
			expectDataset: true,
		},
		{
			name:          "JSON data",
			contentType:   "application/json",
			url:           "https://api.example.com/dataset.json",
			contentLength: 500 * 1024, // 500KB
			expectedScore: 0.7,
			expectDataset: true,
		},
		{
			name:          "Zenodo archive",
			contentType:   "application/zip",
			url:           "https://zenodo.org/record/123456/files/data.zip",
			contentLength: 50 * 1024 * 1024, // 50MB
			expectedScore: 0.8,
			expectDataset: true,
		},
		{
			name:          "HTML page",
			contentType:   "text/html",
			url:           "https://example.com/about.html",
			contentLength: 10 * 1024, // 10KB
			expectedScore: 0.3,
			expectDataset: false,
		},
		{
			name:          "GitHub repository",
			contentType:   "text/html",
			url:           "https://github.com/user/dataset-repo",
			contentLength: 20 * 1024, // 20KB
			expectedScore: 0.9,
			expectDataset: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := &HTTPValidationResult{
				URL:           tc.url,
				ContentType:   tc.contentType,
				ContentLength: tc.contentLength,
			}

			validator.analyzeDatasetLikelihood(result)

			if result.DatasetScore < tc.expectedScore-0.1 || result.DatasetScore > tc.expectedScore+0.3 {
				t.Errorf("Expected dataset score around %.1f, got %.2f", tc.expectedScore, result.DatasetScore)
			}

			if result.IsDataset != tc.expectDataset {
				t.Errorf("Expected IsDataset=%v, got %v", tc.expectDataset, result.IsDataset)
			}
		})
	}
}

func TestHTTPValidator_BatchValidation(t *testing.T) {
	// Create test server that responds to different paths
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/good":
			w.Header().Set("Content-Type", "text/csv")
			w.WriteHeader(http.StatusOK)
		case "/bad":
			w.WriteHeader(http.StatusNotFound)
		case "/slow":
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	validator := NewHTTPValidator(1 * time.Second)
	ctx := context.Background()

	urls := []string{
		server.URL + "/good",
		server.URL + "/bad",
		server.URL + "/slow",
	}

	start := time.Now()
	results := validator.ValidateLinkBatch(ctx, urls, 2)
	duration := time.Since(start)

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	// Check that concurrent processing is actually faster
	if duration > 500*time.Millisecond {
		t.Errorf("Batch processing took too long: %v", duration)
	}

	// Check individual results
	goodResult := results[server.URL+"/good"]
	if goodResult == nil || !goodResult.Accessible {
		t.Error("Expected /good to be accessible")
	}

	badResult := results[server.URL+"/bad"]
	if badResult == nil || badResult.Accessible {
		t.Error("Expected /bad to be inaccessible")
	}

	slowResult := results[server.URL+"/slow"]
	if slowResult == nil || !slowResult.Accessible {
		t.Error("Expected /slow to be accessible")
	}
}

func TestGetRandomUserAgent(t *testing.T) {
	ua1 := getRandomUserAgent()

	if ua1 == "" {
		t.Error("User agent should not be empty")
	}

	if !strings.Contains(ua1, "Mozilla") {
		t.Error("User agent should contain Mozilla")
	}

	// Test that we get different user agents (though this might occasionally fail)
	// Run multiple times to increase chances of getting different ones
	different := false
	for i := 0; i < 10; i++ {
		if getRandomUserAgent() != ua1 {
			different = true
			break
		}
	}
	if !different {
		t.Log("Warning: Got same user agent multiple times (this is probabilistically unlikely but possible)")
	}
}

func TestGetContentTypeCategory(t *testing.T) {
	testCases := []struct {
		contentType string
		expected    string
	}{
		{"text/csv", "structured_data"},
		{"application/json", "structured_data"},
		{"text/xml", "structured_data"},
		{"application/zip", "archive"},
		{"application/x-gzip", "archive"},
		{"application/vnd.ms-excel", "spreadsheet"},
		{"application/pdf", "document"},
		{"text/html", "webpage"},
		{"application/octet-stream", "binary"},
		{"image/png", "unknown"},
	}

	for _, tc := range testCases {
		result := GetContentTypeCategory(tc.contentType)
		if result != tc.expected {
			t.Errorf("GetContentTypeCategory(%q) = %q, expected %q",
				tc.contentType, result, tc.expected)
		}
	}
}

func TestIsHealthyResponse(t *testing.T) {
	testCases := []struct {
		statusCode int
		expected   bool
	}{
		{200, true},
		{201, true},
		{301, true},
		{302, true},
		{304, true},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{500, false},
		{503, false},
	}

	for _, tc := range testCases {
		result := IsHealthyResponse(tc.statusCode)
		if result != tc.expected {
			t.Errorf("IsHealthyResponse(%d) = %v, expected %v",
				tc.statusCode, result, tc.expected)
		}
	}
}

func TestHTTPValidator_RangeRequest(t *testing.T) {
	rangeRequested := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if r.Header.Get("Range") != "" {
			rangeRequested = true
			w.Header().Set("Content-Range", "bytes 0-1023/1048576")
			w.WriteHeader(http.StatusPartialContent)
			w.Write(make([]byte, 1024))
			return
		}

		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("full content"))
	}))
	defer server.Close()

	validator := NewHTTPValidator(5 * time.Second)
	ctx := context.Background()

	result, err := validator.ValidateURL(ctx, server.URL)

	if err != nil {
		t.Fatalf("ValidateURL failed: %v", err)
	}

	if !rangeRequested {
		t.Error("Expected Range request to be made")
	}

	if !result.Accessible {
		t.Error("Expected range request to succeed")
	}

	if !strings.Contains(result.RequestMethod, "Range") {
		t.Errorf("Expected method to indicate Range request, got %s", result.RequestMethod)
	}
}
