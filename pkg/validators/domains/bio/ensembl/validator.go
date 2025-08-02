// Package ensembl provides domain validation for Ensembl genome database identifiers.
package ensembl

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/btraven00/hapiq/pkg/downloaders/ensembl"
	"github.com/btraven00/hapiq/pkg/validators/domains"
)

// EnsemblValidator validates Ensembl genome database identifiers.
type EnsemblValidator struct {
	downloader *ensembl.EnsemblDownloader
	patterns   []domains.Pattern
}

// NewEnsemblValidator creates a new Ensembl validator.
func NewEnsemblValidator() *EnsemblValidator {
	validator := &EnsemblValidator{
		downloader: ensembl.NewEnsemblDownloader(
			ensembl.WithTimeout(30*time.Second),
			ensembl.WithVerbose(false),
		),
		patterns: make([]domains.Pattern, 0),
	}

	// Initialize patterns
	validator.initializePatterns()

	return validator
}

// initializePatterns sets up regex patterns for Ensembl identifiers.
func (v *EnsemblValidator) initializePatterns() {
	v.patterns = append(v.patterns, domains.Pattern{
		Type:        domains.PatternTypeRegex,
		Pattern:     `^(bacteria|fungi|metazoa|plants|protists):\d+:(pep|cds|gff3|dna)(?::[a-z_]+)?$`,
		Description: "Ensembl database identifier format: database:version:content[:species]",
		Examples: []string{
			"bacteria:47:pep",
			"fungi:47:gff3",
			"plants:50:dna:triticum_turgidum",
			"metazoa:47:cds",
		},
	})

	v.patterns = append(v.patterns, domains.Pattern{
		Type:        domains.PatternTypeRegex,
		Pattern:     `^ensembl:(bacteria|fungi|metazoa|plants|protists):\d+:(pep|cds|gff3|dna)(?::[a-z_]+)?$`,
		Description: "Ensembl identifier with explicit 'ensembl:' prefix",
		Examples: []string{
			"ensembl:bacteria:47:pep",
			"ensembl:fungi:47:gff3",
		},
	})
}

// Name returns the name of this validator.
func (v *EnsemblValidator) Name() string {
	return "ensembl"
}

// Domain returns the scientific domain this validator covers.
func (v *EnsemblValidator) Domain() string {
	return "bioinformatics"
}

// Description returns a human-readable description.
func (v *EnsemblValidator) Description() string {
	return "Validates Ensembl genome database identifiers for bacteria, fungi, metazoa, plants, and protists"
}

// Priority returns the priority of this validator.
func (v *EnsemblValidator) Priority() int {
	return 80 // High priority for specific biological databases
}

// GetPatterns returns the patterns this validator recognizes.
func (v *EnsemblValidator) GetPatterns() []domains.Pattern {
	return v.patterns
}

// CanValidate checks if this validator can handle the given input.
func (v *EnsemblValidator) CanValidate(input string) bool {
	// Clean the input first
	cleaned := v.cleanInput(input)

	// Check for direct Ensembl FTP URLs
	if v.isEnsemblFTPURL(input) {
		return true
	}

	// Check for Ensembl identifier patterns
	patterns := []string{
		`^(bacteria|fungi|metazoa|plants|protists):\d+:(pep|cds|gff3|dna)(?::[a-z_]+)?$`,
		`^ensembl:(bacteria|fungi|metazoa|plants|protists):\d+:(pep|cds|gff3|dna)(?::[a-z_]+)?$`,
	}

	for _, pattern := range patterns {
		if matched, _ := regexp.MatchString(pattern, cleaned); matched {
			return true
		}
	}

	return false
}

// Validate performs the actual validation.
func (v *EnsemblValidator) Validate(ctx context.Context, input string) (*domains.DomainValidationResult, error) {
	start := time.Now()

	result := &domains.DomainValidationResult{
		Input:         input,
		Domain:        v.Domain(),
		ValidatorName: v.Name(),
		DatasetType:   "genomic_data",
		Metadata:      make(map[string]string),
		Tags:          []string{},
		Warnings:      []string{},
	}

	// Handle direct FTP URLs
	if v.isEnsemblFTPURL(input) {
		return v.validateDirectURL(ctx, input, result, start)
	}

	// Clean and normalize the input
	cleaned := v.cleanInput(input)
	if cleaned != input {
		result.NormalizedID = cleaned
		result.Warnings = append(result.Warnings, fmt.Sprintf("Normalized input from '%s' to '%s'", input, cleaned))
	}

	// Validate using the downloader
	validation, err := v.downloader.Validate(ctx, cleaned)
	if err != nil {
		result.Valid = false
		result.Error = fmt.Sprintf("Validation failed: %v", err)
		result.Confidence = 0.0
		result.Likelihood = 0.0
		result.ValidationTime = time.Since(start)
		return result, nil
	}

	result.Valid = validation.Valid
	result.Confidence = 0.9 // High confidence for Ensembl validator

	if !validation.Valid {
		result.Error = strings.Join(validation.Errors, "; ")
		result.Likelihood = 0.0
		result.ValidationTime = time.Since(start)
		return result, nil
	}

	// Add warnings from validation
	result.Warnings = append(result.Warnings, validation.Warnings...)

	// Get metadata to enhance the result
	metadata, err := v.downloader.GetMetadata(ctx, cleaned)
	if err != nil {
		// Don't fail validation if metadata retrieval fails
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to retrieve metadata: %v", err))
		result.Likelihood = 0.7 // Still likely valid, but couldn't get full info
	} else {
		// Extract information from metadata
		result.Likelihood = 0.95 // Very high likelihood if we got metadata
		result.Subtype = v.extractSubtype(cleaned)
		result.Tags = metadata.Tags
		result.PrimaryURL = v.constructDownloadURL(cleaned)

		// Add metadata
		result.Metadata["title"] = metadata.Title
		result.Metadata["description"] = metadata.Description
		result.Metadata["version"] = metadata.Version
		result.Metadata["file_count"] = fmt.Sprintf("%d", metadata.FileCount)
		result.Metadata["total_size"] = fmt.Sprintf("%d", metadata.TotalSize)
		result.Metadata["source"] = metadata.Source

		if metadata.Custom != nil {
			if dbType, ok := metadata.Custom["database_type"].(string); ok {
				result.Metadata["database_type"] = dbType
			}
			if contentType, ok := metadata.Custom["content_type"].(string); ok {
				result.Metadata["content_type"] = contentType
			}
			if ftpURL, ok := metadata.Custom["ftp_base_url"].(string); ok {
				result.Metadata["ftp_base_url"] = ftpURL
			}
		}
	}

	result.ValidationTime = time.Since(start)
	return result, nil
}

// cleanInput normalizes the input for validation.
func (v *EnsemblValidator) cleanInput(input string) string {
	// Remove whitespace
	cleaned := strings.TrimSpace(input)

	// Convert to lowercase
	cleaned = strings.ToLower(cleaned)

	// Remove common prefixes
	prefixes := []string{"ensembl:", "http://", "https://"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(cleaned, prefix) {
			cleaned = cleaned[len(prefix):]
		}
	}

	return cleaned
}

// extractSubtype determines the subtype based on the identifier.
func (v *EnsemblValidator) extractSubtype(id string) string {
	parts := strings.Split(id, ":")
	if len(parts) >= 3 {
		database := parts[0]
		content := parts[2]
		return fmt.Sprintf("%s_%s", database, content)
	}
	return ""
}

// constructDownloadURL builds a representative download URL.
func (v *EnsemblValidator) constructDownloadURL(id string) string {
	parts := strings.Split(id, ":")
	if len(parts) >= 3 {
		database := parts[0]
		version := parts[1]

		// Return the HTTPS species list URL for HTTP validation compatibility
		return fmt.Sprintf("https://ftp.ensemblgenomes.ebi.ac.uk/pub/release-%s/%s/species_Ensembl%s.txt",
			version, database, strings.Title(database))
	}
	return ""
}

// isEnsemblFTPURL checks if the input is a direct Ensembl FTP URL.
func (v *EnsemblValidator) isEnsemblFTPURL(input string) bool {
	// Check for Ensembl FTP URLs
	ensemblFTPPatterns := []string{
		`^ftp://ftp\.ensemblgenomes\.org/pub/`,
		`^https://ftp\.ensemblgenomes\.ebi\.ac\.uk/pub/`,
	}

	for _, pattern := range ensemblFTPPatterns {
		if matched, _ := regexp.MatchString(pattern, input); matched {
			return true
		}
	}

	return false
}

// validateDirectURL validates a direct Ensembl FTP URL.
func (v *EnsemblValidator) validateDirectURL(ctx context.Context, url string, result *domains.DomainValidationResult, start time.Time) (*domains.DomainValidationResult, error) {
	// Convert FTP URL to HTTPS for checker validation
	httpsURL := v.convertFTPToHTTPS(url)
	result.PrimaryURL = httpsURL
	result.NormalizedID = url

	// Extract metadata from URL path
	urlInfo := v.parseEnsemblURL(url)
	if urlInfo != nil {
		result.Subtype = fmt.Sprintf("%s_%s", urlInfo.Database, urlInfo.Content)
		result.Tags = []string{"genomics", "bioinformatics", "ensembl", urlInfo.Database, urlInfo.Content}

		result.Metadata["database_type"] = urlInfo.Database
		result.Metadata["content_type"] = urlInfo.Content
		result.Metadata["version"] = urlInfo.Version
		result.Metadata["species"] = urlInfo.Species
		result.Metadata["direct_url"] = "true"
	}

	// Test URL accessibility
	resp, err := v.downloader.GetProtocolClient().Head(ctx, url)
	if err != nil {
		result.Valid = false
		result.Error = fmt.Sprintf("URL not accessible: %v", err)
		result.Confidence = 0.0
		result.Likelihood = 0.0
	} else {
		result.Valid = resp.StatusCode == 200
		result.Confidence = 0.95 // High confidence for direct URLs
		result.Likelihood = 0.9  // High likelihood if accessible

		if resp.Size > 0 {
			result.Metadata["file_size"] = fmt.Sprintf("%d", resp.Size)
		}
	}

	result.ValidationTime = time.Since(start)
	return result, nil
}

// EnsemblURLInfo contains parsed information from an Ensembl URL.
type EnsemblURLInfo struct {
	Database string
	Version  string
	Content  string
	Species  string
	Filename string
}

// parseEnsemblURL extracts information from an Ensembl FTP URL.
func (v *EnsemblValidator) parseEnsemblURL(url string) *EnsemblURLInfo {
	// Pattern: /pub/release-VERSION/DATABASE/...
	releasePattern := regexp.MustCompile(`/release-(\d+)/(\w+)/`)
	matches := releasePattern.FindStringSubmatch(url)
	if len(matches) < 3 {
		return nil
	}

	info := &EnsemblURLInfo{
		Version:  matches[1],
		Database: matches[2],
	}

	// Extract content type from path
	if strings.Contains(url, "/pep/") {
		info.Content = "pep"
	} else if strings.Contains(url, "/cds/") {
		info.Content = "cds"
	} else if strings.Contains(url, "/gff3/") {
		info.Content = "gff3"
	} else if strings.Contains(url, "/dna/") {
		info.Content = "dna"
	}

	// Extract filename and species from URL
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		filename := parts[len(parts)-1]
		info.Filename = filename

		// Try to extract species from filename
		if strings.Contains(filename, ".") {
			speciesPart := strings.Split(filename, ".")[0]
			info.Species = speciesPart
		}
	}

	return info
}

// convertFTPToHTTPS converts an Ensembl FTP URL to HTTPS for HTTP validation.
func (v *EnsemblValidator) convertFTPToHTTPS(ftpURL string) string {
	// Convert ftp://ftp.ensemblgenomes.org to https://ftp.ensemblgenomes.ebi.ac.uk
	httpsURL := strings.Replace(ftpURL, "ftp://ftp.ensemblgenomes.org", "https://ftp.ensemblgenomes.ebi.ac.uk", 1)
	return httpsURL
}
