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

// GSAValidator validates Genome Sequence Archive (GSA) identifiers and URLs
// Supports the Chinese National Genomics Data Center (NGDC) GSA database
type GSAValidator struct {
	*BaseAccessionValidator
	metadataCache map[string]*GSAMetadata
}

// GSAMetadata contains metadata extracted from GSA APIs
type GSAMetadata struct {
	Title          string    `json:"title,omitempty"`
	Organism       string    `json:"organism,omitempty"`
	Platform       string    `json:"platform,omitempty"`
	LibraryLayout  string    `json:"library_layout,omitempty"`
	LibrarySource  string    `json:"library_source,omitempty"`
	StudyType      string    `json:"study_type,omitempty"`
	DataSize       int64     `json:"data_size,omitempty"`
	FileCount      int       `json:"file_count,omitempty"`
	SubmissionDate string    `json:"submission_date,omitempty"`
	LastUpdate     string    `json:"last_update,omitempty"`
	Institution    string    `json:"institution,omitempty"`
	Country        string    `json:"country,omitempty"`
	CachedAt       time.Time `json:"cached_at"`
}

// NewGSAValidator creates a new GSA validator
func NewGSAValidator() *GSAValidator {
	base := NewBaseAccessionValidator(
		"gsa",
		"Genome Sequence Archive (GSA) - China's national genomic data repository",
		88, // High priority for Asian genomic data
	)

	validator := &GSAValidator{
		BaseAccessionValidator: base,
		metadataCache:          make(map[string]*GSAMetadata),
	}

	// Add GSA-specific patterns
	validator.initializeGSAPatterns()

	return validator
}

// initializeGSAPatterns sets up GSA-specific accession patterns
func (v *GSAValidator) initializeGSAPatterns() {
	// Filter global patterns for GSA-related types
	gsaPatterns := []AccessionPattern{}

	for _, pattern := range AccessionPatterns {
		switch pattern.Type {
		case ProjectGSA, StudyGSA, BioSampleGSA, ExperimentGSA, RunGSA:
			gsaPatterns = append(gsaPatterns, pattern)
		}
	}

	v.AddAccessionPatterns(gsaPatterns)
}

// CanValidate checks if this validator can handle the given input
func (v *GSAValidator) CanValidate(input string) bool {
	// Use base implementation
	if v.BaseAccessionValidator.CanValidate(input) {
		return true
	}

	// Additional GSA-specific URL checks
	if v.isGSARelatedURL(input) {
		return true
	}

	return false
}

// Validate performs comprehensive GSA validation
func (v *GSAValidator) Validate(ctx context.Context, input string) (*domains.DomainValidationResult, error) {
	start := time.Now()

	// Start with base validation
	result, err := v.BaseAccessionValidator.Validate(ctx, input)
	if err != nil {
		return result, err
	}

	if !result.Valid {
		return result, nil
	}

	accessionID := result.NormalizedID
	pattern := v.findMatchingPattern(accessionID)
	if pattern == nil {
		result.Valid = false
		result.Error = "no matching GSA pattern found"
		result.ValidationTime = time.Since(start)
		return result, nil
	}

	// Generate GSA-specific URLs
	v.generateGSASpecificURLs(result, accessionID, pattern)

	// Perform HTTP validation
	httpResult, err := v.ValidateHTTPAccess(ctx, result.PrimaryURL)
	if err == nil {
		v.addHTTPMetadata(result, httpResult)
	}

	// Try to fetch metadata from GSA APIs
	if metadata := v.fetchGSAMetadata(ctx, accessionID, pattern.Type); metadata != nil {
		v.addGSAMetadata(result, metadata)
	}

	// Note: Keep format validation separate from HTTP accessibility
	// A properly formatted accession should remain Valid=true even if temporarily inaccessible
	if httpResult != nil && !httpResult.Accessible {
		// Add HTTP error to metadata, but don't invalidate the accession format
		if result.Metadata == nil {
			result.Metadata = make(map[string]string)
		}
		result.Metadata["http_error"] = fmt.Sprintf("HTTP %d", httpResult.StatusCode)
		result.Metadata["accessibility"] = "false"

		// Lower confidence due to inaccessibility, but keep some base confidence for format validity
		result.Confidence = 0.3
		result.Likelihood = 0.3
	} else if httpResult != nil {
		// HTTP accessible
		if result.Metadata == nil {
			result.Metadata = make(map[string]string)
		}
		result.Metadata["accessibility"] = "true"
	}

	// Calculate final scores and add metadata only if accessible
	if httpResult != nil && httpResult.Accessible {
		result.Confidence = v.calculateGSAConfidence(result, httpResult)
		result.Likelihood = result.Confidence // Remove redundancy - likelihood = confidence

		// Add GSA-specific tags and metadata (only if accessible)
		v.enhanceGSAResult(result, pattern, httpResult)
	}

	// Add availability tags based on HTTP status (always)
	v.addAvailabilityTags(result, httpResult)

	result.ValidationTime = time.Since(start)
	return result, nil
}

// isGSARelatedURL checks if URL is GSA-related
func (v *GSAValidator) isGSARelatedURL(input string) bool {
	u, err := url.Parse(input)
	if err != nil {
		return false
	}

	host := strings.ToLower(u.Host)
	path := strings.ToLower(u.Path)

	// GSA-related hosts
	gsaHosts := map[string]bool{
		"ngdc.cncb.ac.cn":     true,
		"bigd.big.ac.cn":      true,
		"download.cncb.ac.cn": true,
		"download.big.ac.cn":  true,
		"ftp.cncb.ac.cn":      true,
		"ftp.big.ac.cn":       true,
		"gsa.big.ac.cn":       true,
		"www.biosino.org":     true,
		"biosino.org":         true,
	}

	if !gsaHosts[host] {
		return false
	}

	// GSA-related path patterns
	gsaPathPatterns := []string{
		"/gsa", "/bioproject", "/biosample", "/gsr",
		"/browse", "/search", "/download",
	}

	for _, pattern := range gsaPathPatterns {
		if strings.Contains(path, pattern) {
			return true
		}
	}

	// Check for GSA accessions in query parameters
	query := u.Query()
	for _, param := range []string{"acc", "term", "query", "accession", "searchTerm"} {
		if value := query.Get(param); value != "" {
			if pattern, matched := MatchAccession(strings.ToUpper(value)); matched {
				switch pattern.Type {
				case ProjectGSA, StudyGSA, BioSampleGSA, ExperimentGSA, RunGSA:
					return true
				}
			}
		}
	}

	return false
}

// generateGSASpecificURLs creates comprehensive URL set for GSA accessions
func (v *GSAValidator) generateGSASpecificURLs(result *domains.DomainValidationResult, accessionID string, pattern *AccessionPattern) {
	var primary string
	var alternates []string

	switch pattern.Type {
	case RunGSA:
		primary, alternates = v.generateGSARunURLs(accessionID)
	case ExperimentGSA:
		primary, alternates = v.generateGSAExperimentURLs(accessionID)
	case BioSampleGSA:
		primary, alternates = v.generateGSABioSampleURLs(accessionID)
	case StudyGSA:
		primary, alternates = v.generateGSAStudyURLs(accessionID)
	case ProjectGSA:
		primary, alternates = v.generateGSAProjectURLs(accessionID)
	default:
		primary, alternates = v.GenerateURLs(accessionID, pattern)
	}

	result.PrimaryURL = primary
	result.AlternateURLs = alternates
}

// generateGSARunURLs creates URLs for GSA run accessions (CRR)
func (v *GSAValidator) generateGSARunURLs(runID string) (string, []string) {
	// Extract potential CRA from CRR for browse URLs
	// GSA runs are usually CRR followed by numbers
	baseURL := "https://ngdc.cncb.ac.cn/gsa"

	// Primary: GSA run search page
	primary := fmt.Sprintf("%s/search?searchTerm=%s", baseURL, runID)

	alternates := []string{
		// Browse by run (requires knowing the CRA, but we'll try generic)
		fmt.Sprintf("%s/browse/%s", baseURL, runID),
		// API endpoint for run metadata
		fmt.Sprintf("%s/api/run/%s", baseURL, runID),
		// Download page
		fmt.Sprintf("https://download.cncb.ac.cn/gsa/%s", runID),
	}

	// Add potential FTP download URLs
	// GSA FTP structure: ftp://download.big.ac.cn/gsa{series}/CRA{series}/CRR{id}/
	if len(runID) >= 6 && strings.HasPrefix(runID, "CRR") {
		// Try to infer series number for FTP structure
		series := "001" // Default series, would need better logic to determine actual series
		ftpPath := fmt.Sprintf("ftp://download.big.ac.cn/gsa%s/CRA%s/%s/",
			series, series, runID)
		alternates = append(alternates, ftpPath)
	}

	return primary, alternates
}

// generateGSAExperimentURLs creates URLs for GSA experiment accessions (CRX)
func (v *GSAValidator) generateGSAExperimentURLs(expID string) (string, []string) {
	baseURL := "https://ngdc.cncb.ac.cn/gsa"

	primary := fmt.Sprintf("%s/search?searchTerm=%s", baseURL, expID)

	alternates := []string{
		fmt.Sprintf("%s/browse/%s", baseURL, expID),
		fmt.Sprintf("%s/api/experiment/%s", baseURL, expID),
	}

	return primary, alternates
}

// generateGSABioSampleURLs creates URLs for GSA biosample accessions (SAMC)
func (v *GSAValidator) generateGSABioSampleURLs(sampleID string) (string, []string) {
	primary := fmt.Sprintf("https://ngdc.cncb.ac.cn/biosample/browse/%s", sampleID)

	alternates := []string{
		fmt.Sprintf("https://ngdc.cncb.ac.cn/gsa/search?searchTerm=%s", sampleID),
		fmt.Sprintf("https://ngdc.cncb.ac.cn/biosample/api/%s", sampleID),
	}

	return primary, alternates
}

// generateGSAStudyURLs creates URLs for GSA study accessions (CRA)
func (v *GSAValidator) generateGSAStudyURLs(studyID string) (string, []string) {
	baseURL := "https://ngdc.cncb.ac.cn/gsa"

	primary := fmt.Sprintf("%s/browse/%s", baseURL, studyID)

	alternates := []string{
		fmt.Sprintf("%s/search?searchTerm=%s", baseURL, studyID),
		// API for getting run info by CRA
		fmt.Sprintf("%s/search/getRunInfoByCra", baseURL),
		// Metadata download
		fmt.Sprintf("%s/file/exportExcelFile", baseURL),
	}

	return primary, alternates
}

// generateGSAProjectURLs creates URLs for GSA project accessions (PRJC)
func (v *GSAValidator) generateGSAProjectURLs(projectID string) (string, []string) {
	primary := fmt.Sprintf("https://ngdc.cncb.ac.cn/bioproject/browse/%s", projectID)

	alternates := []string{
		fmt.Sprintf("https://ngdc.cncb.ac.cn/gsa/search?searchTerm=%s", projectID),
		fmt.Sprintf("https://ngdc.cncb.ac.cn/bioproject/api/%s", projectID),
	}

	return primary, alternates
}

// fetchGSAMetadata attempts to fetch metadata from GSA APIs
func (v *GSAValidator) fetchGSAMetadata(ctx context.Context, accessionID string, accType AccessionType) *GSAMetadata {
	// Check cache first
	if cached, exists := v.metadataCache[accessionID]; exists {
		if time.Since(cached.CachedAt) < 24*time.Hour { // Cache for 24 hours
			return cached
		}
	}

	// Try GSA API (implementation would require actual API calls)
	if metadata := v.fetchFromGSA(ctx, accessionID, accType); metadata != nil {
		metadata.CachedAt = time.Now()
		v.metadataCache[accessionID] = metadata
		return metadata
	}

	return nil
}

// fetchFromGSA fetches metadata from GSA API
func (v *GSAValidator) fetchFromGSA(ctx context.Context, accessionID string, accType AccessionType) *GSAMetadata {
	// Determine the appropriate GSA API endpoint
	var apiURL string
	switch accType {
	case RunGSA:
		// GSA uses POST requests for run info retrieval
		apiURL = "https://ngdc.cncb.ac.cn/gsa/search/getRunInfo"
	case StudyGSA:
		apiURL = "https://ngdc.cncb.ac.cn/gsa/search/getRunInfoByCra"
	case ProjectGSA:
		apiURL = fmt.Sprintf("https://ngdc.cncb.ac.cn/bioproject/api/%s", accessionID)
	case BioSampleGSA:
		apiURL = fmt.Sprintf("https://ngdc.cncb.ac.cn/biosample/api/%s", accessionID)
	default:
		return nil
	}

	// Make HTTP request with timeout
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// GSA APIs often require POST requests with specific parameters
	// This is a simplified implementation - actual implementation would need
	// to handle the specific POST data requirements for each endpoint
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Hapiq/1.0 (GSA Validator)")

	resp, err := v.client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	defer resp.Body.Close()

	// Parse response (simplified - would need proper JSON parsing)
	metadata := &GSAMetadata{}
	// Implementation would parse the JSON response and populate metadata
	// GSA responses are typically in Chinese and English

	return metadata
}

// addGSAMetadata adds fetched metadata to the validation result
func (v *GSAValidator) addGSAMetadata(result *domains.DomainValidationResult, metadata *GSAMetadata) {
	if metadata.Title != "" {
		result.Metadata["title"] = metadata.Title
	}
	if metadata.Organism != "" {
		result.Metadata["organism"] = metadata.Organism
		result.Tags = append(result.Tags, "organism:"+strings.ToLower(metadata.Organism))
	}
	if metadata.Platform != "" {
		result.Metadata["sequencing_platform"] = metadata.Platform
		result.Tags = append(result.Tags, "platform:"+strings.ToLower(metadata.Platform))
	}
	if metadata.LibraryLayout != "" {
		result.Metadata["library_layout"] = metadata.LibraryLayout
	}
	if metadata.LibrarySource != "" {
		result.Metadata["library_source"] = metadata.LibrarySource
	}
	if metadata.StudyType != "" {
		result.Metadata["study_type"] = metadata.StudyType
	}
	if metadata.DataSize > 0 {
		result.Metadata["data_size_bytes"] = fmt.Sprintf("%d", metadata.DataSize)
	}
	if metadata.FileCount > 0 {
		result.Metadata["file_count"] = fmt.Sprintf("%d", metadata.FileCount)
	}
	if metadata.SubmissionDate != "" {
		result.Metadata["submission_date"] = metadata.SubmissionDate
	}
	if metadata.LastUpdate != "" {
		result.Metadata["last_updated"] = metadata.LastUpdate
	}
	if metadata.Institution != "" {
		result.Metadata["institution"] = metadata.Institution
	}
	if metadata.Country != "" {
		result.Metadata["country"] = metadata.Country
		result.Tags = append(result.Tags, "country:"+strings.ToLower(metadata.Country))
	}
}

// addHTTPMetadata adds HTTP validation results to the domain result
func (v *GSAValidator) addHTTPMetadata(result *domains.DomainValidationResult, httpResult *HTTPValidationResult) {
	if httpResult.Accessible {
		result.Metadata["http_status"] = fmt.Sprintf("%d", httpResult.StatusCode)
		result.Metadata["content_type"] = httpResult.ContentType
		if httpResult.LastModified != "" {
			result.Metadata["last_modified"] = httpResult.LastModified
		}
		if httpResult.ContentLength > 0 {
			result.Metadata["content_length"] = fmt.Sprintf("%d", httpResult.ContentLength)
		}
	} else {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("HTTP access failed: %s", httpResult.Error))

		// GSA specific warning for international access
		if strings.Contains(httpResult.Error, "timeout") || strings.Contains(httpResult.Error, "refused") {
			result.Warnings = append(result.Warnings,
				"GSA servers may have limited international access")
		}
	}
}

// enhanceGSAResult adds GSA-specific enhancements
func (v *GSAValidator) enhanceGSAResult(result *domains.DomainValidationResult, pattern *AccessionPattern, httpResult *HTTPValidationResult) {
	// Add GSA-specific tags
	result.Tags = append(result.Tags, "chinese_database", "ngdc", "asia_pacific")

	// Add hierarchy information
	hierarchy := GetAccessionHierarchy(pattern.Type)
	if len(hierarchy) > 1 {
		result.Metadata["hierarchical_level"] = fmt.Sprintf("%d of %d", len(hierarchy), len(hierarchy))

		var levelNames []string
		for _, level := range hierarchy {
			levelNames = append(levelNames, string(level))
		}
		result.Metadata["hierarchy"] = strings.Join(levelNames, " > ")
	}

	// Add regional information
	result.Tags = append(result.Tags, "region:asia")
	result.Metadata["primary_region"] = "China"
	result.Metadata["data_policy"] = "Chinese national data sharing policies apply"

	// Data availability indicators (only add if data is accessible)
	if httpResult != nil && httpResult.Accessible && IsDataLevel(pattern.Type) {
		result.Tags = append(result.Tags, "downloadable_data")
		if pattern.Type == RunGSA {
			result.Tags = append(result.Tags, "raw_reads", "fastq_available")
		}
	}

	// Language support
	result.Tags = append(result.Tags, "chinese_interface", "english_interface")
	result.Metadata["interface_languages"] = "Chinese, English"

	// Download capabilities
	switch pattern.Type {
	case RunGSA:
		result.Tags = append(result.Tags, "aspera_support", "ftp_download", "http_download")
		result.Metadata["download_methods"] = "Aspera, FTP, HTTP"
	case StudyGSA:
		result.Tags = append(result.Tags, "batch_download", "metadata_export")
	}
}

// calculateGSAConfidence determines confidence score for GSA validation
func (v *GSAValidator) calculateGSAConfidence(result *domains.DomainValidationResult, httpResult *HTTPValidationResult) float64 {
	if !result.Valid {
		return 0.0
	}

	// If HTTP validation failed, return 0
	if httpResult != nil && !httpResult.Accessible {
		return 0.0
	}

	confidence := 0.80 // Base confidence for valid GSA accession (slightly lower due to international access issues)

	// HTTP accessibility bonus (adjusted for GSA international access challenges)
	if httpResult != nil && httpResult.Accessible {
		confidence += 0.15
		if httpResult.StatusCode == 200 {
			confidence += 0.03
		}
	} else if httpResult != nil {
		// Don't penalize too much for access issues with Chinese servers
		confidence += 0.05 // Partial credit for attempting validation
	}

	// Metadata availability bonus
	if _, hasTitle := result.Metadata["title"]; hasTitle {
		confidence += 0.05
	}
	if _, hasOrganism := result.Metadata["organism"]; hasOrganism {
		confidence += 0.03
	}

	// Accession type specific adjustments
	if accType, exists := result.Metadata["accession_type"]; exists {
		switch AccessionType(accType) {
		case RunGSA:
			confidence += 0.03 // Runs are most specific
		case StudyGSA:
			confidence += 0.02 // Studies are well-structured in GSA
		case ProjectGSA:
			confidence += 0.01 // Projects are stable but high-level
		}
	}

	// Regional bonus for Chinese data
	if country, exists := result.Metadata["country"]; exists {
		if strings.ToLower(country) == "china" || strings.ToLower(country) == "中国" {
			confidence += 0.02
		}
	}

	// Cap at 1.0
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// ClearCache clears the metadata cache
func (v *GSAValidator) ClearCache() {
	v.metadataCache = make(map[string]*GSAMetadata)
}

// GetCacheSize returns the number of cached entries
func (v *GSAValidator) GetCacheSize() int {
	return len(v.metadataCache)
}

// GetSupportedAccessionTypes returns the types of accessions this validator supports
func (v *GSAValidator) GetSupportedAccessionTypes() []AccessionType {
	return []AccessionType{
		ProjectGSA,
		StudyGSA,
		BioSampleGSA,
		ExperimentGSA,
		RunGSA,
	}
}

// GetDownloadCapabilities returns information about GSA download capabilities
func (v *GSAValidator) GetDownloadCapabilities(accessionType AccessionType) map[string]interface{} {
	capabilities := make(map[string]interface{})

	switch accessionType {
	case RunGSA:
		capabilities["formats"] = []string{"FASTQ", "SRA"}
		capabilities["compression"] = []string{"gzip", "bzip2"}
		capabilities["methods"] = []string{"HTTP", "FTP", "Aspera"}
		capabilities["batch_download"] = true
		capabilities["api_access"] = true
	case StudyGSA:
		capabilities["metadata_formats"] = []string{"CSV", "Excel", "JSON"}
		capabilities["batch_operations"] = true
		capabilities["run_list"] = true
	case ProjectGSA:
		capabilities["metadata_export"] = true
		capabilities["study_list"] = true
	default:
		capabilities["metadata_only"] = true
	}

	capabilities["region_optimized"] = "Asia-Pacific"
	capabilities["international_access"] = "Limited"

	return capabilities
}
