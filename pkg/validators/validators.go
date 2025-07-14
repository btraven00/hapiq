package validators

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// ValidationResult represents the result of a validation check
type ValidationResult struct {
	Valid   bool
	Type    string
	Message string
	Details map[string]interface{}
}

// URLValidator validates various types of URLs
type URLValidator struct{}

// DOIValidator validates DOI identifiers
type DOIValidator struct{}

// NewURLValidator creates a new URL validator
func NewURLValidator() *URLValidator {
	return &URLValidator{}
}

// NewDOIValidator creates a new DOI validator
func NewDOIValidator() *DOIValidator {
	return &DOIValidator{}
}

// ValidateURL validates a URL and determines its type
func (v *URLValidator) ValidateURL(input string) ValidationResult {
	result := ValidationResult{
		Details: make(map[string]interface{}),
	}

	// Parse the URL
	u, err := url.Parse(input)
	if err != nil {
		result.Valid = false
		result.Message = fmt.Sprintf("Invalid URL format: %v", err)
		return result
	}

	// Check if scheme is present
	if u.Scheme == "" {
		result.Valid = false
		result.Message = "URL missing scheme (http/https)"
		return result
	}

	// Check if host is present
	if u.Host == "" {
		result.Valid = false
		result.Message = "URL missing host"
		return result
	}

	// Validate scheme
	if u.Scheme != "http" && u.Scheme != "https" {
		result.Valid = false
		result.Message = fmt.Sprintf("Unsupported URL scheme: %s", u.Scheme)
		return result
	}

	result.Valid = true
	result.Type = v.classifyURLType(u)
	result.Message = "Valid URL"
	result.Details["scheme"] = u.Scheme
	result.Details["host"] = u.Host
	result.Details["path"] = u.Path

	return result
}

// classifyURLType determines the type of repository/service from URL
func (v *URLValidator) classifyURLType(u *url.URL) string {
	host := strings.ToLower(u.Host)
	path := strings.ToLower(u.Path)

	switch {
	case strings.Contains(host, "zenodo.org"):
		if strings.Contains(path, "/record/") {
			return "zenodo_record"
		}
		return "zenodo"
	case strings.Contains(host, "figshare.com"):
		if strings.Contains(path, "/articles/") {
			return "figshare_article"
		}
		return "figshare"
	case strings.Contains(host, "dryad.org"):
		if strings.Contains(path, "/stash/dataset/") {
			return "dryad_dataset"
		}
		return "dryad"
	case strings.Contains(host, "github.com"):
		if strings.Contains(path, "/releases/") {
			return "github_release"
		}
		return "github"
	case strings.Contains(host, "doi.org"):
		return "doi_resolver"
	case strings.Contains(host, "osf.io"):
		return "osf"
	case strings.Contains(host, "mendeley.com"):
		return "mendeley"
	case strings.Contains(host, "dataverse."):
		return "dataverse"
	default:
		return "generic"
	}
}

// ValidateDOI validates a DOI identifier
func (v *DOIValidator) ValidateDOI(input string) ValidationResult {
	result := ValidationResult{
		Details: make(map[string]interface{}),
	}

	// Remove common prefixes
	doi := strings.TrimSpace(input)
	doi = strings.TrimPrefix(doi, "doi:")
	doi = strings.TrimPrefix(doi, "DOI:")
	doi = strings.TrimPrefix(doi, "https://doi.org/")
	doi = strings.TrimPrefix(doi, "http://doi.org/")
	doi = strings.TrimPrefix(doi, "https://dx.doi.org/")
	doi = strings.TrimPrefix(doi, "http://dx.doi.org/")

	// DOI regex pattern - matches the standard DOI format
	// DOIs start with "10." followed by a registrant code and a suffix
	doiPattern := regexp.MustCompile(`^10\.\d{4,}(?:\.\d+)*\/[^\s]+$`)

	if !doiPattern.MatchString(doi) {
		result.Valid = false
		result.Message = "Invalid DOI format"
		result.Details["pattern"] = "DOI must start with '10.' followed by registrant code and suffix"
		return result
	}

	// Split DOI into prefix and suffix
	parts := strings.SplitN(doi, "/", 2)
	if len(parts) != 2 {
		result.Valid = false
		result.Message = "DOI missing suffix after '/'"
		return result
	}

	prefix := parts[0]
	suffix := parts[1]

	// Validate prefix (must start with 10. and have at least 4 digits)
	prefixPattern := regexp.MustCompile(`^10\.\d{4,}(?:\.\d+)*$`)
	if !prefixPattern.MatchString(prefix) {
		result.Valid = false
		result.Message = "Invalid DOI prefix format"
		result.Details["prefix"] = prefix
		return result
	}

	// Check for common issues in suffix
	if strings.TrimSpace(suffix) == "" {
		result.Valid = false
		result.Message = "DOI suffix cannot be empty"
		return result
	}

	result.Valid = true
	result.Type = v.classifyDOIType(prefix, suffix)
	result.Message = "Valid DOI"
	result.Details["prefix"] = prefix
	result.Details["suffix"] = suffix
	result.Details["full_doi"] = doi

	return result
}

// classifyDOIType attempts to determine the type of DOI based on prefix patterns
func (v *DOIValidator) classifyDOIType(prefix, suffix string) string {
	switch {
	case strings.HasPrefix(prefix, "10.5281"):
		return "zenodo_doi"
	case strings.HasPrefix(prefix, "10.6084"):
		return "figshare_doi"
	case strings.HasPrefix(prefix, "10.5061"):
		return "dryad_doi"
	case strings.HasPrefix(prefix, "10.1371"):
		return "plos_doi"
	case strings.HasPrefix(prefix, "10.1038"):
		return "nature_doi"
	case strings.HasPrefix(prefix, "10.1126"):
		return "science_doi"
	case strings.Contains(suffix, "zenodo"):
		return "zenodo_doi"
	case strings.Contains(suffix, "figshare"):
		return "figshare_doi"
	case strings.Contains(suffix, "dryad"):
		return "dryad_doi"
	default:
		return "generic_doi"
	}
}

// ValidateIdentifier is a convenience function that attempts to validate
// an input as either a URL or DOI
func ValidateIdentifier(input string) ValidationResult {
	input = strings.TrimSpace(input)

	// Try URL validation first
	urlValidator := NewURLValidator()
	urlResult := urlValidator.ValidateURL(input)
	if urlResult.Valid {
		return urlResult
	}

	// If URL validation fails, try DOI validation
	doiValidator := NewDOIValidator()
	doiResult := doiValidator.ValidateDOI(input)
	if doiResult.Valid {
		return doiResult
	}

	// If both fail, return the URL error (more informative)
	return ValidationResult{
		Valid:   false,
		Type:    "unknown",
		Message: fmt.Sprintf("Invalid URL or DOI: %s", urlResult.Message),
		Details: map[string]interface{}{
			"url_error": urlResult.Message,
			"doi_error": doiResult.Message,
		},
	}
}

// IsDatasetRepository checks if a URL belongs to a known dataset repository
func IsDatasetRepository(urlType string) bool {
	datasetTypes := map[string]bool{
		"zenodo":           true,
		"zenodo_record":    true,
		"figshare":         true,
		"figshare_article": true,
		"dryad":            true,
		"dryad_dataset":    true,
		"osf":              true,
		"dataverse":        true,
		"mendeley":         true,
		"zenodo_doi":       true,
		"figshare_doi":     true,
		"dryad_doi":        true,
	}
	return datasetTypes[urlType]
}

// GetRepositoryScore returns a confidence score for how likely
// the identifier points to a dataset repository
func GetRepositoryScore(validationResult ValidationResult) float64 {
	if !validationResult.Valid {
		return 0.0
	}

	switch validationResult.Type {
	case "zenodo_record", "figshare_article", "dryad_dataset":
		return 0.95
	case "zenodo_doi", "figshare_doi", "dryad_doi":
		return 0.90
	case "zenodo", "figshare", "dryad", "osf", "dataverse":
		return 0.80
	case "mendeley":
		return 0.70
	case "github_release":
		return 0.60
	case "github":
		return 0.40
	case "doi_resolver":
		return 0.50
	case "generic_doi":
		return 0.30
	default:
		return 0.10
	}
}
