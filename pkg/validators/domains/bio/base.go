package bio

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/btraven00/hapiq/pkg/validators/domains"
)

// BioDomainValidator provides common functionality for bioinformatics validators
type BioDomainValidator struct {
	name        string
	description string
	patterns    []domains.Pattern
	priority    int
	client      *http.Client
}

// NewBioDomainValidator creates a new bioinformatics domain validator
func NewBioDomainValidator(name, description string, priority int) *BioDomainValidator {
	return &BioDomainValidator{
		name:        name,
		description: description,
		priority:    priority,
		patterns:    make([]domains.Pattern, 0),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Name returns the validator name
func (v *BioDomainValidator) Name() string {
	return v.name
}

// Domain returns the scientific domain
func (v *BioDomainValidator) Domain() string {
	return "bioinformatics"
}

// Description returns the validator description
func (v *BioDomainValidator) Description() string {
	return v.description
}

// Priority returns the validator priority
func (v *BioDomainValidator) Priority() int {
	return v.priority
}

// GetPatterns returns the patterns this validator recognizes
func (v *BioDomainValidator) GetPatterns() []domains.Pattern {
	return v.patterns
}

// AddPattern adds a new pattern to the validator
func (v *BioDomainValidator) AddPattern(patternType domains.PatternType, pattern, description string, examples []string) {
	v.patterns = append(v.patterns, domains.Pattern{
		Type:        patternType,
		Pattern:     pattern,
		Description: description,
		Examples:    examples,
	})
}

// ExtractIDFromURL attempts to extract an ID from a URL using common bioinformatics patterns
func (v *BioDomainValidator) ExtractIDFromURL(input string) (string, bool) {
	u, err := url.Parse(input)
	if err != nil {
		return "", false
	}

	// Check query parameters for common ID patterns
	query := u.Query()
	for _, param := range []string{"acc", "id", "accession", "term", "query"} {
		if value := query.Get(param); value != "" {
			return value, true
		}
	}

	// Check path for ID patterns
	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for _, part := range pathParts {
		// Skip common path segments
		if v.isCommonPathSegment(part) {
			continue
		}
		// Look for ID-like patterns
		if v.looksLikeID(part) {
			return part, true
		}
	}

	return "", false
}

// isCommonPathSegment checks if a path segment is a common non-ID segment
func (v *BioDomainValidator) isCommonPathSegment(segment string) bool {
	commonSegments := map[string]bool{
		"query":     true,
		"search":    true,
		"browse":    true,
		"view":      true,
		"show":      true,
		"display":   true,
		"acc":       true,
		"accession": true,
		"entry":     true,
		"record":    true,
		"dataset":   true,
		"data":      true,
		"download":  true,
		"file":      true,
		"api":       true,
		"v1":        true,
		"v2":        true,
	}
	return commonSegments[strings.ToLower(segment)]
}

// looksLikeID checks if a string looks like a biological database ID
func (v *BioDomainValidator) looksLikeID(s string) bool {
	// Common patterns for biological IDs
	patterns := []string{
		`^[A-Z]{2,4}\d+(\.\d+)?$`, // GenBank, RefSeq (e.g., NM_000001, XM_123456.1)
		`^[A-Z]{3}\d{6,}$`,        // GEO, SRA (e.g., GSE123456, SRR123456)
		`^[A-Z]\d{5,}$`,           // Simple format (e.g., P12345)
		`^[A-Z]{2}_\d+$`,          // Underscore format (e.g., NP_000001)
		`^[A-Z]+[0-9]+[A-Z]*$`,    // Mixed alphanumeric
		`^\d{4,}-\d{2}-\d{4}$`,    // Date-like format
		`^[A-Z0-9]{8,}$`,          // Long alphanumeric codes
	}

	for _, pattern := range patterns {
		if matched, _ := regexp.MatchString(pattern, s); matched {
			return true
		}
	}

	return false
}

// ValidateHTTPAccess performs HTTP validation for a URL
func (v *BioDomainValidator) ValidateHTTPAccess(ctx context.Context, url string) (*HTTPValidationResult, error) {
	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return &HTTPValidationResult{
			URL:            url,
			Accessible:     false,
			Error:          err.Error(),
			ValidationTime: time.Since(start),
		}, nil
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return &HTTPValidationResult{
			URL:            url,
			Accessible:     false,
			Error:          err.Error(),
			ValidationTime: time.Since(start),
		}, nil
	}
	defer resp.Body.Close()

	result := &HTTPValidationResult{
		URL:            url,
		Accessible:     resp.StatusCode >= 200 && resp.StatusCode < 400,
		StatusCode:     resp.StatusCode,
		ContentType:    resp.Header.Get("Content-Type"),
		ContentLength:  resp.ContentLength,
		LastModified:   resp.Header.Get("Last-Modified"),
		ValidationTime: time.Since(start),
	}

	// Extract additional metadata
	result.Headers = make(map[string]string)
	for key, values := range resp.Header {
		if len(values) > 0 {
			result.Headers[key] = values[0]
		}
	}

	return result, nil
}

// CalculateBioLikelihood calculates likelihood based on bioinformatics-specific factors
func (v *BioDomainValidator) CalculateBioLikelihood(result *domains.DomainValidationResult, httpResult *HTTPValidationResult) float64 {
	score := 0.0

	// Base score for valid identification
	if result.Valid {
		score += 0.4
	}

	// HTTP accessibility score
	if httpResult != nil && httpResult.Accessible {
		score += 0.3

		// Content type bonus
		contentType := strings.ToLower(httpResult.ContentType)
		switch {
		case strings.Contains(contentType, "text/html"):
			score += 0.1 // Landing page
		case strings.Contains(contentType, "application/json"):
			score += 0.15 // API response
		case strings.Contains(contentType, "text/xml"):
			score += 0.15 // XML data
		case strings.Contains(contentType, "text/plain"):
			score += 0.05 // Plain text data
		}

		// Size considerations for bio data
		if httpResult.ContentLength > 0 {
			if httpResult.ContentLength > 1024*1024 { // > 1MB
				score += 0.1
			} else if httpResult.ContentLength > 1024 { // > 1KB
				score += 0.05
			}
		}
	}

	// Domain-specific bonuses
	if result.DatasetType != "" {
		switch result.DatasetType {
		case "expression_data", "sequence_data", "genomic_data":
			score += 0.1
		case "experimental_data", "computational_data":
			score += 0.08
		case "metadata", "annotation":
			score += 0.05
		}
	}

	// Cap at 1.0
	if score > 1.0 {
		score = 1.0
	}

	return score
}

// HTTPValidationResult contains HTTP validation results
type HTTPValidationResult struct {
	URL            string            `json:"url"`
	Accessible     bool              `json:"accessible"`
	StatusCode     int               `json:"status_code"`
	ContentType    string            `json:"content_type,omitempty"`
	ContentLength  int64             `json:"content_length,omitempty"`
	LastModified   string            `json:"last_modified,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Error          string            `json:"error,omitempty"`
	ValidationTime time.Duration     `json:"validation_time"`
}

// Common biological database URL patterns
var CommonBioURLPatterns = map[string]string{
	"ncbi":         `^https?://www\.ncbi\.nlm\.nih\.gov/`,
	"ebi":          `^https?://www\.ebi\.ac\.uk/`,
	"ensembl":      `^https?://.*\.ensembl\.org/`,
	"ucsc":         `^https?://genome\.ucsc\.edu/`,
	"uniprot":      `^https?://www\.uniprot\.org/`,
	"rcsb":         `^https?://www\.rcsb\.org/`,
	"arrayexpress": `^https?://www\.ebi\.ac\.uk/arrayexpress/`,
	"pride":        `^https?://www\.ebi\.ac\.uk/pride/`,
	"metabolights": `^https?://www\.ebi\.ac\.uk/metabolights/`,
}

// IsKnownBioDomain checks if a URL belongs to a known bioinformatics domain
func IsKnownBioDomain(url string) (string, bool) {
	for domain, pattern := range CommonBioURLPatterns {
		if matched, _ := regexp.MatchString(pattern, url); matched {
			return domain, true
		}
	}
	return "", false
}

// ExtractDatabaseFromURL attempts to extract the database name from a URL
func ExtractDatabaseFromURL(inputURL string) string {
	if domain, found := IsKnownBioDomain(inputURL); found {
		return domain
	}

	// Try to extract from hostname
	u, err := url.Parse(inputURL)
	if err != nil {
		return "unknown"
	}

	host := strings.ToLower(u.Host)
	parts := strings.Split(host, ".")

	// Look for database indicators in hostname
	for _, part := range parts {
		switch part {
		case "ncbi", "ebi", "ensembl", "ucsc", "uniprot", "rcsb":
			return part
		}
	}

	return "unknown"
}
