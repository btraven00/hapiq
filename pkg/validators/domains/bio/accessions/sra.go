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

// SRAValidator validates Sequence Read Archive (SRA) identifiers and URLs
// Supports NCBI SRA, EBI ENA, and DDBJ databases
type SRAValidator struct {
	*BaseAccessionValidator
	metadataCache map[string]*SRAMetadata
}

// SRAMetadata contains metadata extracted from SRA APIs
type SRAMetadata struct {
	Title           string    `json:"title,omitempty"`
	Organism        string    `json:"organism,omitempty"`
	Platform        string    `json:"platform,omitempty"`
	LibraryStrategy string    `json:"library_strategy,omitempty"`
	LibrarySource   string    `json:"library_source,omitempty"`
	StudyType       string    `json:"study_type,omitempty"`
	DataSize        int64     `json:"data_size,omitempty"`
	SubmissionDate  string    `json:"submission_date,omitempty"`
	LastUpdate      string    `json:"last_update,omitempty"`
	CachedAt        time.Time `json:"cached_at"`
}

// NewSRAValidator creates a new SRA validator
func NewSRAValidator() *SRAValidator {
	base := NewBaseAccessionValidator(
		"sra",
		"Sequence Read Archive (SRA/ENA/DDBJ) - International repositories for high-throughput sequencing data",
		95, // High priority for sequence data
	)

	validator := &SRAValidator{
		BaseAccessionValidator: base,
		metadataCache:          make(map[string]*SRAMetadata),
	}

	// Add SRA-specific patterns
	validator.initializeSRAPatterns()

	return validator
}

// initializeSRAPatterns sets up SRA-specific accession patterns
func (v *SRAValidator) initializeSRAPatterns() {
	// Filter global patterns for SRA-related types
	sraPatterns := []AccessionPattern{}

	for _, pattern := range AccessionPatterns {
		switch pattern.Type {
		case StudySRA, SampleSRA, ExperimentSRA, RunSRA, ProjectBioProject:
			sraPatterns = append(sraPatterns, pattern)
		}
	}

	v.AddAccessionPatterns(sraPatterns)
}

// CanValidate checks if this validator can handle the given input
func (v *SRAValidator) CanValidate(input string) bool {
	// Use base implementation
	if v.BaseAccessionValidator.CanValidate(input) {
		return true
	}

	// Additional SRA-specific URL checks
	if v.isSRARelatedURL(input) {
		return true
	}

	return false
}

// Validate performs comprehensive SRA validation
func (v *SRAValidator) Validate(ctx context.Context, input string) (*domains.DomainValidationResult, error) {
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
		result.Error = "no matching SRA pattern found"
		result.ValidationTime = time.Since(start)
		return result, nil
	}

	// Generate SRA-specific URLs
	v.generateSRASpecificURLs(result, accessionID, pattern)

	// Perform HTTP validation
	httpResult, err := v.ValidateHTTPAccess(ctx, result.PrimaryURL)
	if err == nil {
		v.addHTTPMetadata(result, httpResult)
	}

	// Check if HTTP validation failed (404, etc.)
	if httpResult != nil && !httpResult.Accessible {
		result.Valid = false
		result.Error = fmt.Sprintf("accession not accessible (HTTP %d)", httpResult.StatusCode)
		result.Confidence = 0.0
		result.Likelihood = 0.0
		result.ValidationTime = time.Since(start)
		return result, nil
	}

	// Try to fetch metadata from SRA APIs
	if metadata := v.fetchSRAMetadata(ctx, accessionID, pattern.Type); metadata != nil {
		v.addSRAMetadata(result, metadata)
	}

	// Calculate final scores (only if accessible)
	result.Confidence = v.calculateSRAConfidence(result, httpResult)
	result.Likelihood = result.Confidence // Remove redundancy - likelihood = confidence

	// Add availability tags based on HTTP status
	v.addAvailabilityTags(result, httpResult)

	// Add SRA-specific tags and metadata (only if accessible)
	v.enhanceSRAResult(result, pattern, httpResult)

	result.ValidationTime = time.Since(start)
	return result, nil
}

// isSRARelatedURL checks if URL is SRA-related
func (v *SRAValidator) isSRARelatedURL(input string) bool {
	u, err := url.Parse(input)
	if err != nil {
		return false
	}

	host := strings.ToLower(u.Host)
	path := strings.ToLower(u.Path)

	// SRA-related hosts
	sraHosts := map[string]bool{
		"www.ncbi.nlm.nih.gov":       true,
		"trace.ncbi.nlm.nih.gov":     true,
		"ftp-trace.ncbi.nlm.nih.gov": true,
		"www.ebi.ac.uk":              true,
		"ftp.sra.ebi.ac.uk":          true,
		"www.ddbj.nig.ac.jp":         true,
		"trace.ddbj.nig.ac.jp":       true,
	}

	if !sraHosts[host] {
		return false
	}

	// SRA-related path patterns
	sraPathPatterns := []string{
		"/sra", "/traces/sra", "/ena", "/ddbj", "/bioproject", "/biosample",
	}

	for _, pattern := range sraPathPatterns {
		if strings.Contains(path, pattern) {
			return true
		}
	}

	// Check for SRA accessions in query parameters
	query := u.Query()
	for _, param := range []string{"acc", "term", "query", "accession"} {
		if value := query.Get(param); value != "" {
			if pattern, matched := MatchAccession(strings.ToUpper(value)); matched {
				switch pattern.Type {
				case StudySRA, SampleSRA, ExperimentSRA, RunSRA, ProjectBioProject:
					return true
				}
			}
		}
	}

	return false
}

// generateSRASpecificURLs creates comprehensive URL set for SRA accessions
func (v *SRAValidator) generateSRASpecificURLs(result *domains.DomainValidationResult, accessionID string, pattern *AccessionPattern) {
	var primary string
	var alternates []string

	switch pattern.Type {
	case RunSRA:
		primary, alternates = v.generateRunURLs(accessionID)
	case ExperimentSRA:
		primary, alternates = v.generateExperimentURLs(accessionID)
	case SampleSRA:
		primary, alternates = v.generateSampleURLs(accessionID)
	case StudySRA:
		primary, alternates = v.generateStudyURLs(accessionID)
	case ProjectBioProject:
		primary, alternates = v.generateProjectURLs(accessionID)
	default:
		primary, alternates = v.GenerateURLs(accessionID, pattern)
	}

	result.PrimaryURL = primary
	result.AlternateURLs = alternates
}

// generateRunURLs creates URLs for SRA run accessions
func (v *SRAValidator) generateRunURLs(runID string) (string, []string) {
	// Primary: NCBI SRA run page
	primary := fmt.Sprintf("https://www.ncbi.nlm.nih.gov/sra/%s", runID)

	alternates := []string{
		// ENA browser
		fmt.Sprintf("https://www.ebi.ac.uk/ena/browser/view/%s", runID),
		// NCBI Trace archive
		fmt.Sprintf("https://trace.ncbi.nlm.nih.gov/Traces/sra/?run=%s", runID),
		// ENA API for run info
		fmt.Sprintf("https://www.ebi.ac.uk/ena/portal/api/filereport?accession=%s&result=read_run&fields=all", runID),
		// DDBJ (if applicable)
		fmt.Sprintf("https://ddbj.nig.ac.jp/resource/sra-run/%s", runID),
	}

	// Add FTP URLs for data download
	if len(runID) >= 9 {
		ftpPath := fmt.Sprintf("ftp://ftp.sra.ebi.ac.uk/vol1/fastq/%s/%s/%s",
			runID[:6], runID[:9], runID)
		alternates = append(alternates, ftpPath)
	}

	return primary, alternates
}

// generateExperimentURLs creates URLs for SRA experiment accessions
func (v *SRAValidator) generateExperimentURLs(expID string) (string, []string) {
	primary := fmt.Sprintf("https://www.ncbi.nlm.nih.gov/sra?term=%s", expID)

	alternates := []string{
		fmt.Sprintf("https://www.ebi.ac.uk/ena/browser/view/%s", expID),
		fmt.Sprintf("https://trace.ncbi.nlm.nih.gov/Traces/sra/?exp=%s", expID),
		fmt.Sprintf("https://www.ebi.ac.uk/ena/portal/api/search?result=read_experiment&query=experiment_accession=%s", expID),
	}

	return primary, alternates
}

// generateSampleURLs creates URLs for SRA sample accessions
func (v *SRAValidator) generateSampleURLs(sampleID string) (string, []string) {
	primary := fmt.Sprintf("https://www.ncbi.nlm.nih.gov/sra?term=%s", sampleID)

	alternates := []string{
		fmt.Sprintf("https://www.ebi.ac.uk/ena/browser/view/%s", sampleID),
		fmt.Sprintf("https://www.ebi.ac.uk/ena/portal/api/search?result=sample&query=sample_accession=%s", sampleID),
	}

	return primary, alternates
}

// generateStudyURLs creates URLs for SRA study accessions
func (v *SRAValidator) generateStudyURLs(studyID string) (string, []string) {
	primary := fmt.Sprintf("https://www.ncbi.nlm.nih.gov/sra?term=%s", studyID)

	alternates := []string{
		fmt.Sprintf("https://www.ebi.ac.uk/ena/browser/view/%s", studyID),
		fmt.Sprintf("https://www.ebi.ac.uk/ena/portal/api/search?result=study&query=study_accession=%s", studyID),
	}

	return primary, alternates
}

// generateProjectURLs creates URLs for BioProject accessions
func (v *SRAValidator) generateProjectURLs(projectID string) (string, []string) {
	primary := fmt.Sprintf("https://www.ncbi.nlm.nih.gov/bioproject/%s", projectID)

	alternates := []string{
		fmt.Sprintf("https://www.ncbi.nlm.nih.gov/sra?term=%s", projectID),
	}

	// Add region-specific URLs
	if strings.HasPrefix(projectID, "PRJEB") {
		alternates = append(alternates,
			fmt.Sprintf("https://www.ebi.ac.uk/ena/browser/view/%s", projectID))
	} else if strings.HasPrefix(projectID, "PRJDB") {
		alternates = append(alternates,
			fmt.Sprintf("https://ddbj.nig.ac.jp/resource/bioproject/%s", projectID))
	}

	return primary, alternates
}

// fetchSRAMetadata attempts to fetch metadata from SRA APIs
func (v *SRAValidator) fetchSRAMetadata(ctx context.Context, accessionID string, accType AccessionType) *SRAMetadata {
	// Check cache first
	if cached, exists := v.metadataCache[accessionID]; exists {
		if time.Since(cached.CachedAt) < 24*time.Hour { // Cache for 24 hours
			return cached
		}
	}

	// Try ENA API first (usually faster and more reliable)
	if metadata := v.fetchFromENA(ctx, accessionID, accType); metadata != nil {
		metadata.CachedAt = time.Now()
		v.metadataCache[accessionID] = metadata
		return metadata
	}

	// Fallback to NCBI if ENA fails
	if metadata := v.fetchFromNCBI(ctx, accessionID, accType); metadata != nil {
		metadata.CachedAt = time.Now()
		v.metadataCache[accessionID] = metadata
		return metadata
	}

	return nil
}

// fetchFromENA fetches metadata from ENA API
func (v *SRAValidator) fetchFromENA(ctx context.Context, accessionID string, accType AccessionType) *SRAMetadata {
	// Determine the appropriate ENA API endpoint
	var apiURL string
	switch accType {
	case RunSRA:
		apiURL = fmt.Sprintf("https://www.ebi.ac.uk/ena/portal/api/filereport?accession=%s&result=read_run&fields=run_accession,scientific_name,instrument_platform,library_strategy,library_source,base_count,first_created,last_updated", accessionID)
	case ExperimentSRA:
		apiURL = fmt.Sprintf("https://www.ebi.ac.uk/ena/portal/api/search?result=read_experiment&query=experiment_accession=%s&fields=experiment_accession,scientific_name,instrument_platform,library_strategy,library_source,first_created,last_updated", accessionID)
	case StudySRA:
		apiURL = fmt.Sprintf("https://www.ebi.ac.uk/ena/portal/api/search?result=study&query=study_accession=%s&fields=study_accession,scientific_name,study_title,study_type,first_created,last_updated", accessionID)
	default:
		return nil
	}

	// Make HTTP request with timeout
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil
	}

	req.Header.Set("Accept", "text/plain")
	req.Header.Set("User-Agent", "Hapiq/1.0 (SRA Validator)")

	resp, err := v.client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	defer resp.Body.Close()

	// Parse response (simplified - would need proper TSV/JSON parsing)
	metadata := &SRAMetadata{}
	// Implementation would parse the TSV response and populate metadata
	// This is a placeholder for the actual parsing logic

	return metadata
}

// fetchFromNCBI fetches metadata from NCBI APIs
func (v *SRAValidator) fetchFromNCBI(ctx context.Context, accessionID string, accType AccessionType) *SRAMetadata {
	// Implementation would use NCBI E-utilities
	// This is a placeholder for the actual implementation
	return nil
}

// addSRAMetadata adds fetched metadata to the validation result
func (v *SRAValidator) addSRAMetadata(result *domains.DomainValidationResult, metadata *SRAMetadata) {
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
	if metadata.LibraryStrategy != "" {
		result.Metadata["library_strategy"] = metadata.LibraryStrategy
		result.Tags = append(result.Tags, "strategy:"+strings.ToLower(metadata.LibraryStrategy))
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
	if metadata.SubmissionDate != "" {
		result.Metadata["submission_date"] = metadata.SubmissionDate
	}
	if metadata.LastUpdate != "" {
		result.Metadata["last_updated"] = metadata.LastUpdate
	}
}

// addHTTPMetadata adds HTTP validation results to the domain result
func (v *SRAValidator) addHTTPMetadata(result *domains.DomainValidationResult, httpResult *HTTPValidationResult) {
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
	}
}

// enhanceSRAResult adds SRA-specific enhancements
func (v *SRAValidator) enhanceSRAResult(result *domains.DomainValidationResult, pattern *AccessionPattern, httpResult *HTTPValidationResult) {
	// Add SRA-specific tags
	result.Tags = append(result.Tags, "sequence_archive", "high_throughput_sequencing")

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
	database := pattern.Database
	if db, exists := KnownDatabases[database]; exists {
		result.Tags = append(result.Tags, "region:"+db.Region)
	}

	// Data availability indicators (only add if data is accessible)
	if httpResult != nil && httpResult.Accessible && IsDataLevel(pattern.Type) {
		result.Tags = append(result.Tags, "downloadable_data")
		if pattern.Type == RunSRA {
			result.Tags = append(result.Tags, "raw_reads", "fastq_available")
		}
	}
}

// calculateSRAConfidence determines confidence score for SRA validation
func (v *SRAValidator) calculateSRAConfidence(result *domains.DomainValidationResult, httpResult *HTTPValidationResult) float64 {
	if !result.Valid {
		return 0.0
	}

	// If HTTP validation failed, return 0
	if httpResult != nil && !httpResult.Accessible {
		return 0.0
	}

	confidence := 0.85 // Base confidence for valid SRA accession

	// HTTP accessibility bonus
	if httpResult != nil && httpResult.Accessible {
		confidence += 0.10
		if httpResult.StatusCode == 200 {
			confidence += 0.03
		}
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
		case RunSRA:
			confidence += 0.02 // Runs are most specific and reliable
		case ProjectBioProject:
			confidence += 0.01 // Projects are stable but high-level
		}
	}

	// Cap at 1.0
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// ClearCache clears the metadata cache
func (v *SRAValidator) ClearCache() {
	v.metadataCache = make(map[string]*SRAMetadata)
}

// GetCacheSize returns the number of cached entries
func (v *SRAValidator) GetCacheSize() int {
	return len(v.metadataCache)
}

// GetSupportedAccessionTypes returns the types of accessions this validator supports
func (v *SRAValidator) GetSupportedAccessionTypes() []AccessionType {
	return []AccessionType{
		ProjectBioProject,
		StudySRA,
		SampleSRA,
		ExperimentSRA,
		RunSRA,
	}
}
