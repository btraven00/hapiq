package accessions

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/btraven00/hapiq/pkg/validators/domains"
)

// BaseAccessionValidator provides common functionality for all accession validators
type BaseAccessionValidator struct {
	name        string
	description string
	priority    int
	patterns    []AccessionPattern
	client      *http.Client
}

// NewBaseAccessionValidator creates a new base accession validator
func NewBaseAccessionValidator(name, description string, priority int) *BaseAccessionValidator {
	return &BaseAccessionValidator{
		name:        name,
		description: description,
		priority:    priority,
		patterns:    make([]AccessionPattern, 0),
		client: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:       10,
				IdleConnTimeout:    30 * time.Second,
				DisableCompression: false,
				DisableKeepAlives:  false,
			},
		},
	}
}

// Name returns the validator name
func (v *BaseAccessionValidator) Name() string {
	return v.name
}

// Domain returns the scientific domain
func (v *BaseAccessionValidator) Domain() string {
	return "bioinformatics"
}

// Description returns the validator description
func (v *BaseAccessionValidator) Description() string {
	return v.description
}

// Priority returns the validator priority
func (v *BaseAccessionValidator) Priority() int {
	return v.priority
}

// GetPatterns returns the patterns as domain patterns
func (v *BaseAccessionValidator) GetPatterns() []domains.Pattern {
	result := make([]domains.Pattern, len(v.patterns))
	for i, pattern := range v.patterns {
		result[i] = domains.Pattern{
			Type:        domains.PatternTypeRegex,
			Pattern:     pattern.Regex.String(),
			Description: pattern.Description,
			Examples:    pattern.Examples,
		}
	}
	return result
}

// AddAccessionPattern adds an accession pattern to this validator
func (v *BaseAccessionValidator) AddAccessionPattern(pattern AccessionPattern) {
	v.patterns = append(v.patterns, pattern)
}

// AddAccessionPatterns adds multiple accession patterns to this validator
func (v *BaseAccessionValidator) AddAccessionPatterns(patterns []AccessionPattern) {
	v.patterns = append(v.patterns, patterns...)
}

// CanValidate checks if this validator can handle the given input
func (v *BaseAccessionValidator) CanValidate(input string) bool {
	normalized := strings.TrimSpace(strings.ToUpper(input))

	// Check against our specific patterns
	for _, pattern := range v.patterns {
		if pattern.Regex.MatchString(normalized) {
			return true
		}
	}

	// Check if input looks like an accession and extract it
	if extracted := v.extractAccessionFromInput(input); extracted != "" {
		for _, pattern := range v.patterns {
			if pattern.Regex.MatchString(extracted) {
				return true
			}
		}
	}

	return false
}

// Validate performs base validation common to all accession validators
func (v *BaseAccessionValidator) Validate(ctx context.Context, input string) (*domains.DomainValidationResult, error) {
	start := time.Now()

	result := &domains.DomainValidationResult{
		Input:         input,
		ValidatorName: v.Name(),
		Domain:        v.Domain(),
		Metadata:      make(map[string]string),
		Tags:          make([]string, 0),
	}

	// Extract and normalize accession ID
	accessionID := v.extractAccessionFromInput(input)
	if accessionID == "" {
		result.Valid = false
		result.Error = "no valid accession identifier found in input"
		result.ValidationTime = time.Since(start)
		return result, nil
	}

	// Find matching pattern
	matchedPattern := v.findMatchingPattern(accessionID)
	if matchedPattern == nil {
		result.Valid = false
		result.Error = "accession format not recognized by this validator"
		result.ValidationTime = time.Since(start)
		return result, nil
	}

	// Basic validation passed
	result.Valid = true
	result.NormalizedID = accessionID

	// Set type and metadata based on matched pattern
	v.populateResultFromPattern(result, matchedPattern)

	// Validate format
	if valid, issues := ValidateAccessionFormat(accessionID); !valid {
		result.Warnings = append(result.Warnings, issues...)
	}

	result.ValidationTime = time.Since(start)
	return result, nil
}

// extractAccessionFromInput extracts accession ID from various input formats
func (v *BaseAccessionValidator) extractAccessionFromInput(input string) string {
	normalized := strings.TrimSpace(input)

	// Direct match - most common case
	upper := strings.ToUpper(normalized)
	if pattern, matched := MatchAccession(upper); matched {
		// Verify this validator can handle this pattern type
		for _, validatorPattern := range v.patterns {
			if validatorPattern.Type == pattern.Type {
				return upper
			}
		}
	}

	// Try to extract from URL
	if strings.HasPrefix(normalized, "http") {
		if id := v.extractFromURL(normalized); id != "" {
			return id
		}
	}

	// Try to extract from text (e.g., "accession: SRR123456")
	extracted := ExtractAccessionFromText(normalized)
	for _, candidate := range extracted {
		for _, pattern := range v.patterns {
			if pattern.Regex.MatchString(candidate) {
				return candidate
			}
		}
	}

	return ""
}

// extractFromURL attempts to extract accession ID from URL
func (v *BaseAccessionValidator) extractFromURL(inputURL string) string {
	u, err := url.Parse(inputURL)
	if err != nil {
		return ""
	}

	// Check query parameters
	query := u.Query()
	for _, param := range []string{"acc", "accession", "id", "term", "query"} {
		if value := query.Get(param); value != "" {
			if v.isValidAccessionForValidator(value) {
				return strings.ToUpper(value)
			}
		}
	}

	// Check path segments
	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for _, part := range pathParts {
		if v.isValidAccessionForValidator(part) {
			return strings.ToUpper(part)
		}
	}

	return ""
}

// isValidAccessionForValidator checks if an accession is valid for this validator
func (v *BaseAccessionValidator) isValidAccessionForValidator(accession string) bool {
	upper := strings.ToUpper(strings.TrimSpace(accession))
	for _, pattern := range v.patterns {
		if pattern.Regex.MatchString(upper) {
			return true
		}
	}
	return false
}

// findMatchingPattern finds the pattern that matches the given accession
func (v *BaseAccessionValidator) findMatchingPattern(accession string) *AccessionPattern {
	for _, pattern := range v.patterns {
		if pattern.Regex.MatchString(accession) {
			return &pattern
		}
	}
	return nil
}

// populateResultFromPattern fills result fields based on matched pattern
func (v *BaseAccessionValidator) populateResultFromPattern(result *domains.DomainValidationResult, pattern *AccessionPattern) {
	result.DatasetType = v.getDatasetTypeFromAccessionType(pattern.Type)
	result.Subtype = string(pattern.Type)
	result.Metadata["accession_type"] = string(pattern.Type)
	result.Metadata["database"] = pattern.Database
	result.Metadata["pattern_description"] = pattern.Description

	// Add database information
	if db, exists := KnownDatabases[pattern.Database]; exists {
		result.Metadata["database_full_name"] = db.FullName
		result.Metadata["database_url"] = db.URL
		result.Metadata["database_region"] = db.Region
	}

	// Add tags based on accession type
	result.Tags = append(result.Tags, pattern.Database)
	result.Tags = append(result.Tags, v.getTagsFromAccessionType(pattern.Type)...)
}

// getDatasetTypeFromAccessionType maps accession types to dataset types
func (v *BaseAccessionValidator) getDatasetTypeFromAccessionType(accType AccessionType) string {
	switch accType {
	case RunSRA, RunGSA:
		return "sequence_data"
	case ExperimentSRA, ExperimentGSA:
		return "experimental_data"
	case SampleSRA, SampleGEO:
		return "sample_data"
	case BioSampleNCBI, BioSampleEBI, BioSampleDDBJ, BioSampleGSA:
		return "sample_metadata"
	case StudySRA, StudyGSA:
		return "study_metadata"
	case ProjectBioProject, ProjectGSA, ProjectGEO:
		return "project_metadata"
	default:
		return "biological_data"
	}
}

// getTagsFromAccessionType returns relevant tags for an accession type
func (v *BaseAccessionValidator) getTagsFromAccessionType(accType AccessionType) []string {
	var tags []string

	switch accType {
	case RunSRA, RunGSA:
		tags = append(tags, "sequencing", "run", "raw_data")
	case ExperimentSRA, ExperimentGSA:
		tags = append(tags, "experiment", "sequencing")
	case SampleSRA, SampleGEO:
		tags = append(tags, "sample", "biological_sample")
	case BioSampleNCBI, BioSampleEBI, BioSampleDDBJ, BioSampleGSA:
		tags = append(tags, "biosample", "metadata", "sample_metadata")
	case StudySRA, StudyGSA:
		tags = append(tags, "study", "metadata")
	case ProjectBioProject, ProjectGSA, ProjectGEO:
		tags = append(tags, "project", "metadata")
	}

	// Add hierarchical tags
	hierarchy := GetAccessionHierarchy(accType)
	for _, level := range hierarchy {
		switch level {
		case ProjectBioProject, ProjectGSA, ProjectGEO:
			tags = append(tags, "project_level")
		case StudySRA, StudyGSA:
			tags = append(tags, "study_level")
		case ExperimentSRA, ExperimentGSA:
			tags = append(tags, "experiment_level")
		case RunSRA, RunGSA:
			tags = append(tags, "run_level")
		}
	}

	// Add data availability tag
	// Note: data_available/metadata_only tags will be added later based on HTTP accessibility
	// Don't add availability tags here since we don't know if data is actually accessible

	return tags
}

// GenerateURLs creates relevant URLs for an accession
func (v *BaseAccessionValidator) GenerateURLs(accessionID string, pattern *AccessionPattern) (primary string, alternates []string) {
	database := pattern.Database
	accType := pattern.Type

	switch database {
	case "sra":
		primary, alternates = v.generateSRAURLs(accessionID, accType)
	case "ena":
		primary, alternates = v.generateENAURLs(accessionID, accType)
	case "ddbj":
		primary, alternates = v.generateDDBJURLs(accessionID, accType)
	case "gsa":
		primary, alternates = v.generateGSAURLs(accessionID, accType)
	case "geo":
		primary, alternates = v.generateGEOURLs(accessionID, accType)
	case "biosample":
		primary, alternates = v.generateBioSampleURLs(accessionID, accType)
	case "bioproject":
		primary, alternates = v.generateBioProjectURLs(accessionID, accType)
	}

	return primary, alternates
}

// generateSRAURLs creates SRA-specific URLs
func (v *BaseAccessionValidator) generateSRAURLs(accessionID string, accType AccessionType) (string, []string) {
	baseURL := "https://www.ncbi.nlm.nih.gov/sra"
	var primary string
	var alternates []string

	switch accType {
	case RunSRA:
		primary = fmt.Sprintf("%s/%s", baseURL, accessionID)
		alternates = append(alternates,
			fmt.Sprintf("https://www.ebi.ac.uk/ena/browser/view/%s", accessionID),
			fmt.Sprintf("https://trace.ncbi.nlm.nih.gov/Traces/sra/?run=%s", accessionID),
		)
	case ExperimentSRA:
		primary = fmt.Sprintf("%s?term=%s", baseURL, accessionID)
		alternates = append(alternates,
			fmt.Sprintf("https://www.ebi.ac.uk/ena/browser/view/%s", accessionID),
		)
	case SampleSRA:
		primary = fmt.Sprintf("%s?term=%s", baseURL, accessionID)
		alternates = append(alternates,
			fmt.Sprintf("https://www.ebi.ac.uk/ena/browser/view/%s", accessionID),
		)
	case StudySRA:
		primary = fmt.Sprintf("%s?term=%s", baseURL, accessionID)
		alternates = append(alternates,
			fmt.Sprintf("https://www.ebi.ac.uk/ena/browser/view/%s", accessionID),
		)
	}

	return primary, alternates
}

// generateENAURLs creates ENA-specific URLs
func (v *BaseAccessionValidator) generateENAURLs(accessionID string, accType AccessionType) (string, []string) {
	primary := fmt.Sprintf("https://www.ebi.ac.uk/ena/browser/view/%s", accessionID)
	var alternates []string

	// Add API URLs
	alternates = append(alternates,
		fmt.Sprintf("https://www.ebi.ac.uk/ena/portal/api/filereport?accession=%s&result=read_run&fields=all", accessionID),
	)

	return primary, alternates
}

// generateDDBJURLs creates DDBJ-specific URLs
func (v *BaseAccessionValidator) generateDDBJURLs(accessionID string, accType AccessionType) (string, []string) {
	primary := fmt.Sprintf("https://ddbj.nig.ac.jp/resource/sra-run/%s", accessionID)
	var alternates []string

	return primary, alternates
}

// generateGSAURLs creates GSA-specific URLs
func (v *BaseAccessionValidator) generateGSAURLs(accessionID string, accType AccessionType) (string, []string) {
	var primary string
	var alternates []string

	switch accType {
	case RunGSA:
		primary = fmt.Sprintf("https://ngdc.cncb.ac.cn/gsa/browse/%s", accessionID)
	case ExperimentGSA:
		primary = fmt.Sprintf("https://ngdc.cncb.ac.cn/gsa/browse/%s", accessionID)
	case StudyGSA:
		primary = fmt.Sprintf("https://ngdc.cncb.ac.cn/gsa/browse/%s", accessionID)
	case ProjectGSA:
		primary = fmt.Sprintf("https://ngdc.cncb.ac.cn/gsa/browse/%s", accessionID)
	}

	return primary, alternates
}

// generateGEOURLs creates GEO-specific URLs
func (v *BaseAccessionValidator) generateGEOURLs(accessionID string, accType AccessionType) (string, []string) {
	primary := fmt.Sprintf("https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=%s", accessionID)
	var alternates []string

	return primary, alternates
}

// generateBioSampleURLs creates BioSample-specific URLs
func (v *BaseAccessionValidator) generateBioSampleURLs(accessionID string, accType AccessionType) (string, []string) {
	var primary string
	var alternates []string

	switch accType {
	case BioSampleNCBI:
		primary = fmt.Sprintf("https://www.ncbi.nlm.nih.gov/biosample/%s", accessionID)
	case BioSampleEBI:
		primary = fmt.Sprintf("https://www.ebi.ac.uk/biosamples/samples/%s", accessionID)
	case BioSampleDDBJ:
		primary = fmt.Sprintf("https://ddbj.nig.ac.jp/resource/biosample/%s", accessionID)
	case BioSampleGSA:
		primary = fmt.Sprintf("https://ngdc.cncb.ac.cn/biosample/browse/%s", accessionID)
	}

	return primary, alternates
}

// generateBioProjectURLs creates BioProject-specific URLs
func (v *BaseAccessionValidator) generateBioProjectURLs(accessionID string, accType AccessionType) (string, []string) {
	primary := fmt.Sprintf("https://www.ncbi.nlm.nih.gov/bioproject/%s", accessionID)
	var alternates []string

	// Always add SRA search as alternate
	alternates = append(alternates,
		fmt.Sprintf("https://www.ncbi.nlm.nih.gov/sra?term=%s", accessionID))

	if strings.HasPrefix(accessionID, "PRJEB") {
		alternates = append(alternates,
			fmt.Sprintf("https://www.ebi.ac.uk/ena/browser/view/%s", accessionID))
	} else if strings.HasPrefix(accessionID, "PRJDB") {
		alternates = append(alternates,
			fmt.Sprintf("https://ddbj.nig.ac.jp/resource/bioproject/%s", accessionID))
	}

	return primary, alternates
}

// ValidateHTTPAccess performs HTTP validation for a URL with accession-specific logic
func (v *BaseAccessionValidator) ValidateHTTPAccess(ctx context.Context, url string) (*HTTPValidationResult, error) {
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

	// Add user agent for better compatibility with biological databases
	req.Header.Set("User-Agent", "Hapiq/1.0 (Biological Database Validator)")

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
		Accessible:     v.isHTTPStatusSuccessful(resp.StatusCode),
		StatusCode:     resp.StatusCode,
		ContentType:    resp.Header.Get("Content-Type"),
		ContentLength:  resp.ContentLength,
		LastModified:   resp.Header.Get("Last-Modified"),
		ValidationTime: time.Since(start),
		Headers:        make(map[string]string),
	}

	// Extract relevant headers
	relevantHeaders := []string{
		"Server", "X-Powered-By", "Content-Encoding", "Cache-Control",
		"ETag", "Expires", "Location", "X-RateLimit-Remaining",
	}

	for _, header := range relevantHeaders {
		if value := resp.Header.Get(header); value != "" {
			result.Headers[header] = value
		}
	}

	return result, nil
}

// isHTTPStatusSuccessful checks if HTTP status indicates success
func (v *BaseAccessionValidator) isHTTPStatusSuccessful(statusCode int) bool {
	// Accept 2xx and 3xx status codes as successful
	return statusCode >= 200 && statusCode < 400
}

// CalculateAccessionLikelihood calculates likelihood score for accession validation
func (v *BaseAccessionValidator) CalculateAccessionLikelihood(result *domains.DomainValidationResult, httpResult *HTTPValidationResult) float64 {
	if !result.Valid {
		return 0.0
	}

	// If HTTP validation failed, return 0
	if httpResult != nil && !httpResult.Accessible {
		return 0.0
	}

	score := 0.6 // Base score for valid accession format

	// HTTP accessibility bonus
	if httpResult != nil && httpResult.Accessible {
		score += 0.25

		// Content type considerations
		contentType := strings.ToLower(httpResult.ContentType)
		switch {
		case strings.Contains(contentType, "text/html"):
			score += 0.10 // Landing page available
		case strings.Contains(contentType, "application/json"):
			score += 0.15 // API response
		case strings.Contains(contentType, "text/xml"):
			score += 0.15 // XML metadata
		case strings.Contains(contentType, "application/xml"):
			score += 0.15 // XML metadata
		}

		// Status code specific bonuses
		if httpResult.StatusCode == 200 {
			score += 0.05
		}
	}

	// Data level bonus (actual data vs metadata)
	if accType, exists := result.Metadata["accession_type"]; exists {
		if IsDataLevel(AccessionType(accType)) {
			score += 0.10
		}
	}

	// Database reputation bonus
	if database, exists := result.Metadata["database"]; exists {
		switch database {
		case "sra", "ena", "ddbj", "geo":
			score += 0.05 // Well-established international databases
		case "gsa":
			score += 0.03 // Newer but official database
		}
	}

	// Cap at 1.0
	if score > 1.0 {
		score = 1.0
	}

	return score
}

// addAvailabilityTags adds data availability tags based on HTTP accessibility
func (v *BaseAccessionValidator) addAvailabilityTags(result *domains.DomainValidationResult, httpResult *HTTPValidationResult) {
	if !result.Valid {
		return
	}

	accType := AccessionType(result.Metadata["accession_type"])

	if httpResult != nil && httpResult.Accessible {
		// Data is accessible via HTTP
		if IsDataLevel(accType) {
			result.Tags = append(result.Tags, "data_available")
		} else {
			result.Tags = append(result.Tags, "metadata_available")
		}
	} else {
		// Data is not accessible (404, timeout, etc.)
		if IsDataLevel(accType) {
			result.Tags = append(result.Tags, "data_unavailable")
		} else {
			result.Tags = append(result.Tags, "metadata_unavailable")
		}
	}
}

// HTTPValidationResult contains HTTP validation results for accessions
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
