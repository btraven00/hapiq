package bio

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/btraven00/hapiq/pkg/validators/domains"
)

// GEOValidator validates Gene Expression Omnibus (GEO) identifiers and URLs.
type GEOValidator struct {
	*BioDomainValidator
	idRegexes map[string]*regexp.Regexp
}

// NewGEOValidator creates a new GEO validator.
func NewGEOValidator() *GEOValidator {
	base := NewBioDomainValidator(
		"geo",
		"Gene Expression Omnibus (GEO) - NCBI repository for gene expression and genomics data",
		90, // High priority for bioinformatics
	)

	validator := &GEOValidator{
		BioDomainValidator: base,
		idRegexes:          make(map[string]*regexp.Regexp),
	}

	// Initialize regex patterns for different GEO ID types
	validator.initializePatterns()

	return validator
}

// initializePatterns sets up regex patterns and URL patterns for GEO.
func (v *GEOValidator) initializePatterns() {
	// GEO ID patterns
	patterns := map[string]string{
		"GSE": `^GSE\d+$`,       // GEO Series (experiments)
		"GSM": `^GSM\d+$`,       // GEO Samples
		"GPL": `^GPL\d+$`,       // GEO Platforms
		"GDS": `^GDS\d+$`,       // GEO Datasets (curated)
		"GSC": `^GSC\d+$`,       // GEO SuperSeries Collections
		"GCF": `^GCF_\d+\.\d+$`, // GenBank Complete genome Format
		"GCA": `^GCA_\d+\.\d+$`, // GenBank Complete genome Assembly
	}

	// Compile regex patterns
	for idType, pattern := range patterns {
		compiled, err := regexp.Compile(pattern)
		if err == nil {
			v.idRegexes[idType] = compiled
		}
	}

	// Add patterns to the base validator for documentation
	v.AddPattern(
		domains.PatternTypeRegex,
		`GSE\d+`,
		"GEO Series ID - represents a study or experiment",
		[]string{"GSE185917", "GSE123456", "GSE000001"},
	)

	v.AddPattern(
		domains.PatternTypeRegex,
		`GSM\d+`,
		"GEO Sample ID - represents individual samples",
		[]string{"GSM1234567", "GSM987654", "GSM000001"},
	)

	v.AddPattern(
		domains.PatternTypeRegex,
		`GPL\d+`,
		"GEO Platform ID - represents array/sequencing platforms",
		[]string{"GPL570", "GPL96", "GPL21185"},
	)

	v.AddPattern(
		domains.PatternTypeRegex,
		`GDS\d+`,
		"GEO Dataset ID - curated gene expression datasets",
		[]string{"GDS1234", "GDS5678"},
	)

	v.AddPattern(
		domains.PatternTypeURL,
		`https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=*`,
		"GEO accession page URLs",
		[]string{
			"https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSE185917",
			"https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GSM1234567",
			"https://www.ncbi.nlm.nih.gov/geo/query/acc.cgi?acc=GPL570",
		},
	)

	v.AddPattern(
		domains.PatternTypeURL,
		`https://www.ncbi.nlm.nih.gov/geo/browse/?view=*`,
		"GEO browse URLs",
		[]string{
			"https://www.ncbi.nlm.nih.gov/geo/browse/?view=series&acc=GSE185917",
		},
	)
}

// CanValidate checks if this validator can handle the given input.
func (v *GEOValidator) CanValidate(input string) bool {
	input = strings.TrimSpace(input)

	// Check for direct GEO ID patterns
	for _, regex := range v.idRegexes {
		if regex.MatchString(input) {
			return true
		}
	}

	// Check for GEO URLs
	if v.isGEOURL(input) {
		return true
	}

	// Check if we can extract a GEO ID from the input
	if extractedID := v.extractGEOID(input); extractedID != "" {
		return true
	}

	return false
}

// Validate performs GEO-specific validation.
func (v *GEOValidator) Validate(ctx context.Context, input string) (*domains.DomainValidationResult, error) {
	start := time.Now()
	input = strings.TrimSpace(input)

	result := &domains.DomainValidationResult{
		Input:         input,
		ValidatorName: v.Name(),
		Domain:        v.Domain(),
		Metadata:      make(map[string]string),
		Tags:          make([]string, 0),
	}

	// Extract and normalize GEO ID
	geoID := v.extractGEOID(input)
	if geoID == "" {
		result.Valid = false
		result.Error = "no valid GEO identifier found in input"
		result.ValidationTime = time.Since(start)

		return result, nil
	}

	result.NormalizedID = geoID
	result.Valid = true

	// Classify the GEO ID type and set metadata
	v.classifyGEOID(geoID, result)

	// Generate URLs
	v.generateGEOURLs(geoID, result)

	// Perform HTTP validation if we have URLs
	if result.PrimaryURL != "" {
		httpResult, err := v.ValidateHTTPAccess(ctx, result.PrimaryURL)
		if err == nil {
			v.addHTTPMetadata(httpResult, result)
		}
	}

	// Calculate confidence and likelihood
	result.Confidence = v.calculateConfidence(result)
	result.Likelihood = v.CalculateBioLikelihood(result, nil)

	result.ValidationTime = time.Since(start)

	return result, nil
}

// extractGEOID extracts a GEO ID from various input formats.
func (v *GEOValidator) extractGEOID(input string) string {
	// Direct ID match
	for _, regex := range v.idRegexes {
		if regex.MatchString(input) {
			return input
		}
	}

	// Extract from URL
	if v.isGEOURL(input) {
		if id, found := v.ExtractIDFromURL(input); found {
			// Validate the extracted ID
			for _, regex := range v.idRegexes {
				if regex.MatchString(id) {
					return id
				}
			}
		}
	}

	// Try to find GEO ID pattern anywhere in the string
	geoPattern := regexp.MustCompile(`\b(G[SCM][ESLP]?\d+(?:\.\d+)?)\b`)

	matches := geoPattern.FindStringSubmatch(input)
	if len(matches) > 1 {
		candidate := matches[1]
		for _, regex := range v.idRegexes {
			if regex.MatchString(candidate) {
				return candidate
			}
		}
	}

	return ""
}

// isGEOURL checks if the input is a GEO-related URL.
func (v *GEOValidator) isGEOURL(input string) bool {
	u, err := url.Parse(input)
	if err != nil {
		return false
	}

	host := strings.ToLower(u.Host)
	path := strings.ToLower(u.Path)

	// Check for NCBI GEO URLs
	if host == "www.ncbi.nlm.nih.gov" {
		return strings.Contains(path, "/geo/") ||
			strings.Contains(u.RawQuery, "acc=G") ||
			strings.Contains(u.RawQuery, "term=G")
	}

	// Check for alternative GEO URLs
	geoHosts := []string{
		"ftp.ncbi.nlm.nih.gov",
		"ftp-trace.ncbi.nlm.nih.gov",
	}

	for _, geoHost := range geoHosts {
		if host == geoHost && strings.Contains(path, "geo") {
			return true
		}
	}

	return false
}

// classifyGEOID determines the type and characteristics of a GEO ID.
func (v *GEOValidator) classifyGEOID(geoID string, result *domains.DomainValidationResult) {
	switch {
	case strings.HasPrefix(geoID, "GSE"):
		result.DatasetType = "expression_data"
		result.Subtype = "series"
		result.Metadata["geo_type"] = "Series"
		result.Metadata["description"] = "Gene expression experiment or study"
		result.Tags = append(result.Tags, "experiment", "series", "study")

	case strings.HasPrefix(geoID, "GSM"):
		result.DatasetType = "expression_data"
		result.Subtype = "sample"
		result.Metadata["geo_type"] = "Sample"
		result.Metadata["description"] = "Individual biological sample"
		result.Tags = append(result.Tags, "sample", "biological_sample")

	case strings.HasPrefix(geoID, "GPL"):
		result.DatasetType = "metadata"
		result.Subtype = "platform"
		result.Metadata["geo_type"] = "Platform"
		result.Metadata["description"] = "Array or sequencing platform information"
		result.Tags = append(result.Tags, "platform", "array", "technology")

	case strings.HasPrefix(geoID, "GDS"):
		result.DatasetType = "expression_data"
		result.Subtype = "dataset"
		result.Metadata["geo_type"] = "Dataset"
		result.Metadata["description"] = "Curated gene expression dataset"
		result.Tags = append(result.Tags, "curated", "dataset", "processed")

	case strings.HasPrefix(geoID, "GSC"):
		result.DatasetType = "expression_data"
		result.Subtype = "collection"
		result.Metadata["geo_type"] = "SuperSeries Collection"
		result.Metadata["description"] = "Collection of related series"
		result.Tags = append(result.Tags, "collection", "superseries")

	case strings.HasPrefix(geoID, "GCF") || strings.HasPrefix(geoID, "GCA"):
		result.DatasetType = "genomic_data"
		result.Subtype = "assembly"
		result.Metadata["geo_type"] = "Genome Assembly"
		result.Metadata["description"] = "Complete genome assembly"
		result.Tags = append(result.Tags, "genome", "assembly", "reference")
	}

	// Add common metadata
	result.Metadata["database"] = "GEO"
	result.Metadata["provider"] = "NCBI"
	result.Metadata["data_domain"] = "gene_expression"
	result.Tags = append(result.Tags, "ncbi", "geo", "gene_expression")
}

// generateGEOURLs creates relevant URLs for the GEO ID.
func (v *GEOValidator) generateGEOURLs(geoID string, result *domains.DomainValidationResult) {
	baseURL := "https://www.ncbi.nlm.nih.gov/geo"

	// Primary URL (accession page)
	result.PrimaryURL = fmt.Sprintf("%s/query/acc.cgi?acc=%s", baseURL, geoID)

	// Generate alternate URLs
	alternates := []string{}

	switch {
	case strings.HasPrefix(geoID, "GSE"):
		alternates = append(alternates,
			fmt.Sprintf("%s/browse/?view=series&acc=%s", baseURL, geoID),
			fmt.Sprintf("%s/browse/?view=samples&series=%s", baseURL, geoID),
		)

	case strings.HasPrefix(geoID, "GSM"):
		alternates = append(alternates,
			fmt.Sprintf("%s/browse/?view=samples&acc=%s", baseURL, geoID),
		)

	case strings.HasPrefix(geoID, "GPL"):
		alternates = append(alternates,
			fmt.Sprintf("%s/browse/?view=platforms&acc=%s", baseURL, geoID),
		)
	}

	// Add FTP URLs for data download
	if strings.HasPrefix(geoID, "GSE") || strings.HasPrefix(geoID, "GSM") {
		seriesID := geoID
		if strings.HasPrefix(geoID, "GSM") {
			// For samples, we can't directly determine the series, but we can provide the pattern
			seriesID = "GSExxx" // Placeholder
		}

		ftpURL := fmt.Sprintf("ftp://ftp.ncbi.nlm.nih.gov/geo/series/%snnn/%s/",
			seriesID[:len(seriesID)-3], seriesID)
		alternates = append(alternates, ftpURL)
	}

	result.AlternateURLs = alternates
}

// addHTTPMetadata adds HTTP validation results to the domain result.
func (v *GEOValidator) addHTTPMetadata(httpResult *HTTPValidationResult, result *domains.DomainValidationResult) {
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

// calculateConfidence determines how confident we are in the validation.
func (v *GEOValidator) calculateConfidence(result *domains.DomainValidationResult) float64 {
	if !result.Valid {
		return 0.0
	}

	confidence := 0.8 // Base confidence for valid GEO ID

	// Increase confidence based on successful HTTP access
	if result.Metadata["http_status"] == "200" {
		confidence += 0.15
	}

	// Increase confidence based on ID type (some are more reliable)
	switch result.Subtype {
	case "series", "dataset":
		confidence += 0.05 // These are primary data objects
	case "platform":
		confidence += 0.03 // Platforms are stable but metadata
	}

	// Cap at 1.0
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}
