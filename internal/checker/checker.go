package checker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/btraven00/hapiq/pkg/validators/domains"
	_ "github.com/btraven00/hapiq/pkg/validators/domains/bio" // Import for side effects (validator registration)
)

// Config holds configuration for the checker.
type Config struct {
	OutputFormat   string
	TimeoutSeconds int
	Verbose        bool
	Download       bool
}

// Result represents the result of checking a dataset URL.
type Result struct {
	Metadata        map[string]string                 `json:"metadata,omitempty"`
	FileStructure   *FileStructure                    `json:"file_structure,omitempty"`
	Target          string                            `json:"target"`
	ContentType     string                            `json:"content_type,omitempty"`
	DatasetType     string                            `json:"dataset_type,omitempty"`
	Error           string                            `json:"error,omitempty"`
	DomainResults   []*domains.DomainValidationResult `json:"domain_results,omitempty"`
	HTTPStatus      int                               `json:"http_status,omitempty"`
	ContentLength   int64                             `json:"content_length,omitempty"`
	ResponseTime    time.Duration                     `json:"response_time,omitempty"`
	LikelihoodScore float64                           `json:"likelihood_score"`
	Valid           bool                              `json:"valid"`
}

// FileStructure represents the structure of downloaded files.
type FileStructure struct {
	FileTypes  map[string]int `json:"file_types"`
	Extensions map[string]int `json:"extensions"`
	Archives   []string       `json:"archives,omitempty"`
	TotalFiles int            `json:"total_files"`
	TotalSize  int64          `json:"total_size"`
}

// Checker performs dataset validation and analysis.
type Checker struct {
	client *http.Client
	config Config
}

// New creates a new Checker instance.
func New(config Config) *Checker {
	client := &http.Client{
		Timeout: time.Duration(config.TimeoutSeconds) * time.Second,
	}

	// Initialize domain validators
	initializeDomainValidators()

	return &Checker{
		config: config,
		client: client,
	}
}

// initializeDomainValidators sets up the domain-specific validators.
func initializeDomainValidators() {
	// Domain validators are now registered via init() functions in their packages
	// This function is kept for any additional runtime configuration if needed
}

// Check validates and analyzes a dataset URL or identifier.
func (c *Checker) Check(target string) (*Result, error) {
	result := &Result{
		Target:   target,
		Metadata: make(map[string]string),
	}

	// Try domain-specific validation first
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.config.TimeoutSeconds)*time.Second)
	defer cancel()

	domainResults, domainErr := c.tryDomainValidation(ctx, target)
	if domainErr == nil && len(domainResults) > 0 {
		result.DomainResults = domainResults
		// Use the best domain result for primary classification
		bestResult := c.selectBestDomainResult(domainResults)
		if bestResult != nil {
			// Always use domain result if available, even if invalid
			result.Valid = bestResult.Valid
			result.DatasetType = bestResult.DatasetType
			result.LikelihoodScore = bestResult.Likelihood
			// Add domain metadata to main result
			for key, value := range bestResult.Metadata {
				result.Metadata["domain_"+key] = value
			}

			// If domain validation failed due to HTTP issues, set appropriate status
			if !bestResult.Valid && strings.Contains(bestResult.Error, "HTTP") {
				// Extract HTTP status if available in error message
				if strings.Contains(bestResult.Error, "404") {
					result.HTTPStatus = 404
				} else if strings.Contains(bestResult.Error, "403") {
					result.HTTPStatus = 403
				} else if strings.Contains(bestResult.Error, "500") {
					result.HTTPStatus = 500
				}

				result.Error = bestResult.Error

				return result, nil
			}

			if bestResult.PrimaryURL != "" {
				// Use domain-specific URL for HTTP validation
				target = bestResult.PrimaryURL
			}
		}
	}

	// Normalize and validate the target (fallback to generic validation)
	normalizedURL, datasetType, err := c.normalizeTarget(target)
	if err != nil {
		// If domain validation succeeded but URL normalization failed, don't override
		if result.Valid {
			return result, nil
		}

		result.Error = err.Error()

		return result, nil
	}

	// Only update dataset type if domain validation didn't provide one
	if result.DatasetType == "" {
		result.DatasetType = datasetType
	}

	if c.config.Verbose {
		fmt.Printf("Normalized URL: %s\n", normalizedURL)
		fmt.Printf("Dataset type: %s\n", datasetType)
	}

	// Perform HTTP check
	start := time.Now()

	resp, err := c.client.Head(normalizedURL)
	if err != nil {
		// Try GET request if HEAD fails
		resp, err = c.client.Get(normalizedURL)
	}

	result.ResponseTime = time.Since(start)

	if err != nil {
		result.Error = fmt.Sprintf("HTTP request failed: %v", err)
		return result, nil
	}

	defer resp.Body.Close()

	result.HTTPStatus = resp.StatusCode
	result.ContentType = resp.Header.Get("Content-Type")
	result.ContentLength = resp.ContentLength
	result.Valid = resp.StatusCode >= 200 && resp.StatusCode < 300

	// Extract metadata from headers
	c.extractMetadata(resp, result)

	// Calculate likelihood score (only if not set by domain validation)
	if result.LikelihoodScore == 0 {
		result.LikelihoodScore = c.calculateLikelihood(result)
	}

	// Attempt download if requested and the URL seems valid
	if c.config.Download && result.Valid && result.LikelihoodScore > 0.3 {
		if err := c.attemptDownload(normalizedURL, result); err != nil {
			if c.config.Verbose {
				fmt.Printf("Download failed: %v\n", err)
			}
		}
	}

	return result, nil
}

// tryDomainValidation attempts validation using domain-specific validators.
func (c *Checker) tryDomainValidation(ctx context.Context, target string) ([]*domains.DomainValidationResult, error) {
	validators := domains.FindValidators(target)
	if len(validators) == 0 {
		return nil, nil
	}

	results := make([]*domains.DomainValidationResult, 0, len(validators))

	for _, validator := range validators {
		if c.config.Verbose {
			fmt.Printf("Trying domain validator: %s (%s)\n", validator.Name(), validator.Domain())
		}

		result, err := validator.Validate(ctx, target)
		if err != nil {
			if c.config.Verbose {
				fmt.Printf("Domain validator %s failed: %v\n", validator.Name(), err)
			}

			continue
		}

		if result != nil {
			results = append(results, result)

			if c.config.Verbose {
				fmt.Printf("Domain validator %s: valid=%t, confidence=%.2f, likelihood=%.2f\n",
					validator.Name(), result.Valid, result.Confidence, result.Likelihood)
			}
		}
	}

	return results, nil
}

// selectBestDomainResult chooses the best domain validation result.
func (c *Checker) selectBestDomainResult(results []*domains.DomainValidationResult) *domains.DomainValidationResult {
	if len(results) == 0 {
		return nil
	}

	var best *domains.DomainValidationResult

	bestScore := -1.0

	for _, result := range results {
		// Score based on confidence and likelihood
		// Give valid results higher base score, but still consider invalid results
		score := result.Confidence*0.6 + result.Likelihood*0.4
		if result.Valid {
			score += 1.0 // Bonus for valid results
		}

		if score > bestScore {
			bestScore = score
			best = result
		}
	}

	return best
}

// normalizeTarget converts various identifier formats to URLs.
func (c *Checker) normalizeTarget(target string) (string, string, error) {
	// Check if it's already a valid URL
	if u, err := url.Parse(target); err == nil && u.Scheme != "" {
		return c.classifyURL(target)
	}

	// Check for DOI pattern
	doiPattern := regexp.MustCompile(`^10\.\d+/.+`)
	if doiPattern.MatchString(target) {
		return "https://doi.org/" + target, "doi", nil
	}

	// Check for Zenodo record ID
	zenodoPattern := regexp.MustCompile(`^\d+$`)
	if zenodoPattern.MatchString(target) {
		return fmt.Sprintf("https://zenodo.org/record/%s", target), "zenodo", nil
	}

	return "", "", fmt.Errorf("unrecognized target format: %s", target)
}

// classifyURL determines the type of dataset repository from URL.
func (c *Checker) classifyURL(target string) (string, string, error) {
	u, err := url.Parse(target)
	if err != nil {
		return "", "", err
	}

	host := strings.ToLower(u.Host)

	switch {
	case strings.Contains(host, "zenodo.org"):
		return target, "zenodo", nil
	case strings.Contains(host, "figshare.com"):
		return target, "figshare", nil
	case strings.Contains(host, "dryad.org"):
		return target, "dryad", nil
	case strings.Contains(host, "github.com"):
		return target, "github", nil
	case strings.Contains(host, "doi.org"):
		return target, "doi", nil
	default:
		return target, "generic", nil
	}
}

// extractMetadata extracts useful metadata from HTTP response headers.
func (c *Checker) extractMetadata(resp *http.Response, result *Result) {
	// Common headers that might contain useful information
	headers := map[string]string{
		"server":           resp.Header.Get("Server"),
		"last-modified":    resp.Header.Get("Last-Modified"),
		"etag":             resp.Header.Get("ETag"),
		"content-encoding": resp.Header.Get("Content-Encoding"),
		"content-language": resp.Header.Get("Content-Language"),
	}

	for key, value := range headers {
		if value != "" {
			result.Metadata[key] = value
		}
	}
}

// calculateLikelihood estimates the likelihood that this is a valid dataset.
func (c *Checker) calculateLikelihood(result *Result) float64 {
	score := 0.0

	// Base score for valid HTTP response
	if result.Valid {
		score += 0.3
	}

	// Score based on dataset type
	switch result.DatasetType {
	case "zenodo", "figshare", "dryad":
		score += 0.4
	case "github":
		score += 0.2
	case "doi":
		score += 0.3
	default:
		score += 0.1
	}

	// Score based on content type
	contentType := strings.ToLower(result.ContentType)

	switch {
	case strings.Contains(contentType, "application/zip"),
		strings.Contains(contentType, "application/x-tar"),
		strings.Contains(contentType, "application/gzip"):
		score += 0.2
	case strings.Contains(contentType, "text/html"):
		score += 0.1
	case strings.Contains(contentType, "application/json"):
		score += 0.15
	}

	// Score based on content length (datasets are usually substantial)
	if result.ContentLength > 1024*1024 { // > 1MB
		score += 0.1
	}

	// Cap the score at 1.0
	if score > 1.0 {
		score = 1.0
	}

	return score
}

// attemptDownload tries to download and analyze the dataset structure.
func (c *Checker) attemptDownload(url string, result *Result) error {
	// This is a placeholder for download functionality
	// In a real implementation, this would:
	// 1. Create a temporary directory
	// 2. Download the file(s)
	// 3. Extract archives if needed
	// 4. Analyze file structure
	// 5. Clean up temporary files
	if c.config.Verbose {
		fmt.Printf("Download functionality not yet implemented for: %s\n", url)
	}

	// Mock file structure for demonstration
	result.FileStructure = &FileStructure{
		TotalFiles: 0,
		TotalSize:  0,
		FileTypes:  make(map[string]int),
		Extensions: make(map[string]int),
	}

	return nil
}

// OutputResult outputs the result in the specified format.
func (c *Checker) OutputResult(result *Result) error {
	switch strings.ToLower(c.config.OutputFormat) {
	case "json":
		return c.outputJSON(result)
	case "human", "":
		return c.outputHuman(result)
	default:
		return fmt.Errorf("unsupported output format: %s", c.config.OutputFormat)
	}
}

// outputJSON outputs the result as JSON.
func (c *Checker) outputJSON(result *Result) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	return encoder.Encode(result)
}

// outputHuman outputs the result in human-readable format.
func (c *Checker) outputHuman(result *Result) error {
	fmt.Printf("Target: %s\n", result.Target)

	if result.Error != "" {
		fmt.Printf("❌ Error: %s\n", result.Error)
		return nil
	}

	if result.Valid {
		fmt.Printf("✅ Status: Valid (HTTP %d)\n", result.HTTPStatus)
	} else {
		fmt.Printf("❌ Status: Invalid (HTTP %d)\n", result.HTTPStatus)
	}

	fmt.Printf("📂 Dataset Type: %s\n", result.DatasetType)
	fmt.Printf("🔗 Content Type: %s\n", result.ContentType)

	if result.ContentLength > 0 {
		fmt.Printf("📏 Size: %d bytes\n", result.ContentLength)
	}

	fmt.Printf("⏱️  Response Time: %v\n", result.ResponseTime)
	fmt.Printf("🧠 Dataset Likelihood: %.2f\n", result.LikelihoodScore)

	// Display domain-specific results
	if len(result.DomainResults) > 0 {
		fmt.Printf("🔬 Domain Analysis:\n")

		for _, domainResult := range result.DomainResults {
			status := "❌"
			if domainResult.Valid {
				status = "✅"
			}

			fmt.Printf("   %s %s (%s): confidence=%.2f, likelihood=%.2f\n",
				status, domainResult.ValidatorName, domainResult.Domain,
				domainResult.Confidence, domainResult.Likelihood)

			if domainResult.Valid && domainResult.DatasetType != "" {
				fmt.Printf("      Type: %s", domainResult.DatasetType)

				if domainResult.Subtype != "" {
					fmt.Printf(" (%s)", domainResult.Subtype)
				}

				fmt.Printf("\n")
			}

			if len(domainResult.Tags) > 0 && c.config.Verbose {
				fmt.Printf("      Tags: %s\n", strings.Join(domainResult.Tags, ", "))
			}
		}
	}

	if len(result.Metadata) > 0 && c.config.Verbose {
		fmt.Printf("📋 Metadata:\n")

		for key, value := range result.Metadata {
			fmt.Printf("   %s: %s\n", key, value)
		}
	}

	return nil
}
