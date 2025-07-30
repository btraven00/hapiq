package extractor

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// HTTPValidator handles HTTP validation of URLs with browser spoofing.
type HTTPValidator struct {
	client    *http.Client
	userAgent string
}

// HTTPValidationResult contains detailed HTTP validation information.
type HTTPValidationResult struct {
	Headers       map[string]string `json:"headers,omitempty"`
	Error         string            `json:"error,omitempty"`
	FinalURL      string            `json:"final_url,omitempty"`
	RequestMethod string            `json:"request_method"`
	ContentType   string            `json:"content_type,omitempty"`
	LastModified  string            `json:"last_modified,omitempty"`
	ETag          string            `json:"etag,omitempty"`
	Server        string            `json:"server,omitempty"`
	URL           string            `json:"url"`
	RedirectChain []string          `json:"redirect_chain,omitempty"`
	ResponseTime  time.Duration     `json:"response_time"`
	ContentLength int64             `json:"content_length,omitempty"`
	DatasetScore  float64           `json:"dataset_score"`
	StatusCode    int               `json:"status_code"`
	Accessible    bool              `json:"accessible"`
	IsDataset     bool              `json:"is_dataset"`
}

// NewHTTPValidator creates a new HTTP validator with browser-like configuration.
func NewHTTPValidator(timeout time.Duration) *HTTPValidator {
	if timeout == 0 {
		timeout = 15 * time.Second
	}

	// Create HTTP client with browser-like configuration
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  false,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: false},
			MaxIdleConnsPerHost: 10,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Allow up to 10 redirects (some repositories have long redirect chains)
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects: %d", len(via))
			}
			// Preserve headers through redirects
			if len(via) > 0 {
				req.Header = via[0].Header.Clone()
			}
			return nil
		},
	}

	return &HTTPValidator{
		client:    client,
		userAgent: getRandomUserAgent(),
	}
}

// ValidateURL validates a URL using HEAD first, then GET if necessary.
func (v *HTTPValidator) ValidateURL(ctx context.Context, targetURL string) (*HTTPValidationResult, error) {
	start := time.Now()

	result := &HTTPValidationResult{
		URL:          targetURL,
		ResponseTime: 0,
		Headers:      make(map[string]string),
	}

	// Parse and validate URL
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		result.Error = fmt.Sprintf("invalid URL: %v", err)
		result.ResponseTime = time.Since(start)

		return result, nil
	}

	// Try HEAD request first (lightweight, doesn't download content)
	headResult, headErr := v.performRequest(ctx, "HEAD", parsedURL.String())
	if headErr == nil && headResult.StatusCode < 400 {
		result = headResult
		result.RequestMethod = "HEAD"
		result.ResponseTime = time.Since(start)
		result.Accessible = result.StatusCode >= 200 && result.StatusCode < 400
		v.analyzeDatasetLikelihood(result)

		return result, nil
	}

	// If HEAD fails or returns client/server error, try GET with Range header
	// This allows us to get headers without downloading the full content
	getResult, getErr := v.performRequestWithRange(ctx, parsedURL.String())
	if getErr == nil {
		result = getResult
		result.RequestMethod = "GET (Range)"
		result.ResponseTime = time.Since(start)
		result.Accessible = result.StatusCode >= 200 && result.StatusCode < 400
		v.analyzeDatasetLikelihood(result)

		return result, nil
	}

	// If both fail, try a simple GET request (last resort)
	simpleResult, simpleErr := v.performRequest(ctx, "GET", parsedURL.String())
	if simpleErr != nil {
		result.Error = fmt.Sprintf("all requests failed - HEAD: %v, GET: %v", headErr, simpleErr)
		result.ResponseTime = time.Since(start)

		return result, nil
	}

	result = simpleResult
	result.RequestMethod = "GET"
	result.ResponseTime = time.Since(start)
	result.Accessible = result.StatusCode >= 200 && result.StatusCode < 400
	v.analyzeDatasetLikelihood(result)

	return result, nil
}

// performRequest executes an HTTP request with browser spoofing.
func (v *HTTPValidator) performRequest(ctx context.Context, method, url string) (*HTTPValidationResult, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, http.NoBody)
	if err != nil {
		return nil, err
	}

	// Add browser-like headers
	v.addBrowserHeaders(req)

	// Track redirects
	var redirectChain []string

	originalURL := url

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Build redirect chain if redirects occurred
	if resp.Request.URL.String() != originalURL {
		redirectChain = append(redirectChain, originalURL)
		redirectChain = append(redirectChain, resp.Request.URL.String())
	}

	result := &HTTPValidationResult{
		URL:           originalURL,
		FinalURL:      resp.Request.URL.String(),
		StatusCode:    resp.StatusCode,
		RedirectChain: redirectChain,
		Headers:       make(map[string]string),
	}

	// Extract useful headers
	v.extractHeaders(resp, result)

	return result, nil
}

// performRequestWithRange tries to get headers without downloading full content.
func (v *HTTPValidator) performRequestWithRange(ctx context.Context, url string) (*HTTPValidationResult, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, err
	}

	// Add browser-like headers
	v.addBrowserHeaders(req)

	// Request only the first few bytes to avoid downloading large files
	req.Header.Set("Range", "bytes=0-1023")

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	result := &HTTPValidationResult{
		URL:        url,
		FinalURL:   resp.Request.URL.String(),
		StatusCode: resp.StatusCode,
		Headers:    make(map[string]string),
	}

	// Extract headers
	v.extractHeaders(resp, result)

	return result, nil
}

// addBrowserHeaders adds realistic browser headers to avoid detection.
func (v *HTTPValidator) addBrowserHeaders(req *http.Request) {
	req.Header.Set("User-Agent", v.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("DNT", "1")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Cache-Control", "max-age=0")
}

// extractHeaders extracts useful headers from the response.
func (v *HTTPValidator) extractHeaders(resp *http.Response, result *HTTPValidationResult) {
	// Primary headers
	result.ContentType = resp.Header.Get("Content-Type")
	result.LastModified = resp.Header.Get("Last-Modified")
	result.ETag = resp.Header.Get("ETag")
	result.Server = resp.Header.Get("Server")

	// Parse content length
	if contentLengthStr := resp.Header.Get("Content-Length"); contentLengthStr != "" {
		if contentLength, err := strconv.ParseInt(contentLengthStr, 10, 64); err == nil {
			result.ContentLength = contentLength
		}
	}

	// Store interesting headers
	interestingHeaders := []string{
		"Content-Disposition", "Content-Encoding", "Content-Language",
		"Access-Control-Allow-Origin", "X-Powered-By", "X-Frame-Options",
		"X-Content-Type-Options", "Strict-Transport-Security",
		"Location", "Refresh", "Retry-After",
	}

	for _, header := range interestingHeaders {
		if value := resp.Header.Get(header); value != "" {
			result.Headers[strings.ToLower(header)] = value
		}
	}
}

// analyzeDatasetLikelihood determines if the URL likely points to a dataset.
func (v *HTTPValidator) analyzeDatasetLikelihood(result *HTTPValidationResult) {
	score := 0.0

	// Check content type for dataset indicators
	contentType := strings.ToLower(result.ContentType)

	switch {
	case strings.Contains(contentType, "text/csv"):
		score += 0.9
	case strings.Contains(contentType, "application/json"):
		score += 0.7
	case strings.Contains(contentType, "application/xml"):
		score += 0.6
	case strings.Contains(contentType, "application/zip"):
		score += 0.8
	case strings.Contains(contentType, "application/x-tar"):
		score += 0.8
	case strings.Contains(contentType, "application/gzip"):
		score += 0.7
	case strings.Contains(contentType, "application/octet-stream"):
		score += 0.5
	case strings.Contains(contentType, "application/vnd.ms-excel"):
		score += 0.8
	case strings.Contains(contentType, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"):
		score += 0.8
	case strings.Contains(contentType, "text/html"):
		score += 0.3 // Could be a dataset landing page
	}

	// Check URL patterns
	urlLower := strings.ToLower(result.URL)
	datasetURLPatterns := []string{
		"download", "data", "dataset", "file", "archive", "export",
		".csv", ".tsv", ".json", ".xml", ".zip", ".tar", ".gz",
		".xlsx", ".xls", ".h5", ".hdf5", ".parquet", ".feather",
	}

	for _, pattern := range datasetURLPatterns {
		if strings.Contains(urlLower, pattern) {
			score += 0.2
		}
	}

	// Check known data repository domains
	datasetDomains := []string{
		"zenodo.org", "figshare.com", "dryad.org", "osf.io",
		"data.mendeley.com", "kaggle.com", "dataverse.org",
		"ncbi.nlm.nih.gov", "ebi.ac.uk", "github.com",
	}

	for _, domain := range datasetDomains {
		if strings.Contains(urlLower, domain) {
			score += 0.4
			break
		}
	}

	// Check content disposition header for file downloads
	if disposition, exists := result.Headers["content-disposition"]; exists {
		if strings.Contains(strings.ToLower(disposition), "attachment") {
			score += 0.3
		}
	}

	// Check file size indicators (large files more likely to be datasets)
	if result.ContentLength > 0 {
		switch {
		case result.ContentLength > 100*1024*1024: // > 100MB
			score += 0.3
		case result.ContentLength > 10*1024*1024: // > 10MB
			score += 0.2
		case result.ContentLength > 1024*1024: // > 1MB
			score += 0.1
		}
	}

	// Normalize score to [0, 1]
	if score > 1.0 {
		score = 1.0
	}

	result.DatasetScore = score
	result.IsDataset = score >= 0.5
}

// getRandomUserAgent returns a realistic browser user agent.
func getRandomUserAgent() string {
	userAgents := []string{
		// Chrome on Windows
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		// Chrome on macOS
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		// Firefox on Windows
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
		// Firefox on macOS
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:121.0) Gecko/20100101 Firefox/121.0",
		// Safari on macOS
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.1 Safari/605.1.15",
		// Edge on Windows
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
		// Chrome on Linux (common for academic/research environments)
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	}

	// Use a simple rotation based on current nanosecond
	// In production, you might want a more sophisticated approach
	return userAgents[int(time.Now().UnixNano())%len(userAgents)]
}

// ValidateLinkBatch validates multiple URLs concurrently.
func (v *HTTPValidator) ValidateLinkBatch(ctx context.Context, urls []string, maxConcurrency int) map[string]*HTTPValidationResult {
	if maxConcurrency <= 0 {
		maxConcurrency = 5 // Conservative default to avoid overwhelming servers
	}

	results := make(map[string]*HTTPValidationResult)
	urlChan := make(chan string, len(urls))
	resultChan := make(chan struct {
		result *HTTPValidationResult
		url    string
	}, len(urls))

	// Start workers
	for i := 0; i < maxConcurrency; i++ {
		go func() {
			for url := range urlChan {
				result, _ := v.ValidateURL(ctx, url)
				resultChan <- struct {
					result *HTTPValidationResult
					url    string
				}{result, url}
			}
		}()
	}

	// Send URLs to workers
	for _, url := range urls {
		urlChan <- url
	}

	close(urlChan)

	// Collect results
	for i := 0; i < len(urls); i++ {
		result := <-resultChan
		results[result.url] = result.result
	}

	return results
}

// IsHealthyResponse checks if the HTTP response indicates a healthy endpoint.
func IsHealthyResponse(statusCode int) bool {
	return statusCode >= 200 && statusCode < 400
}

// GetContentTypeCategory categorizes content types for analysis.
func GetContentTypeCategory(contentType string) string {
	contentType = strings.ToLower(contentType)

	switch {
	case strings.Contains(contentType, "csv"):
		return "structured_data"
	case strings.Contains(contentType, "json"):
		return "structured_data"
	case strings.Contains(contentType, "xml"):
		return "structured_data"
	case strings.Contains(contentType, "zip"), strings.Contains(contentType, "tar"), strings.Contains(contentType, "gzip"):
		return "archive"
	case strings.Contains(contentType, "excel"), strings.Contains(contentType, "spreadsheet"):
		return "spreadsheet"
	case strings.Contains(contentType, "pdf"):
		return "document"
	case strings.Contains(contentType, "html"):
		return "webpage"
	case strings.Contains(contentType, "octet-stream"):
		return "binary"
	default:
		return "unknown"
	}
}
