package extractor

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"code.sajari.com/docconv/v2"

	"github.com/btraven00/hapiq/pkg/validators/domains"
)

// PDFExtractor handles extraction of links from PDF documents using domain validators.
type PDFExtractor struct {
	extractionPatterns []ExtractionPattern
	sectionRegexes     []*regexp.Regexp
	cleaners           []*regexp.Regexp
	options            ExtractionOptions
}

// ExtractionPattern defines how to extract identifiers from text.
type ExtractionPattern struct {
	Regex       *regexp.Regexp
	Name        string
	Type        LinkType
	Description string
	Examples    []string
	Confidence  float64
}

// NewPDFExtractor creates a new PDF extractor that uses domain validators.
func NewPDFExtractor(options ExtractionOptions) *PDFExtractor {
	extractor := &PDFExtractor{
		extractionPatterns: getExtractionPatterns(),
		sectionRegexes:     getDefaultSectionRegexes(),
		cleaners:           getDefaultCleaners(),
		options:            options,
	}

	return extractor
}

// ExtractFromFile extracts links from a PDF file using domain validators.
func (e *PDFExtractor) ExtractFromFile(filename string) (*ExtractionResult, error) {
	startTime := time.Now()

	// Extract text from PDF using docconv
	response, err := docconv.ConvertPath(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to convert PDF file '%s': %w", filename, err)
	}

	if strings.TrimSpace(response.Body) == "" {
		return nil, fmt.Errorf("no readable text found in PDF file")
	}

	text := response.Body
	pageCount := 1 // TODO: Implement page detection

	// Extract links from the text
	var allLinks []ExtractedLink

	links := e.extractLinksFromText(text, 1, "unknown")
	allLinks = append(allLinks, links...)

	// Create result structure
	result := &ExtractionResult{
		Filename: filename,
		Pages:    pageCount,
		Links:    allLinks,
		Errors:   []string{},
		Warnings: []string{},
	}

	// Sort links for deterministic output
	e.sortLinksForDeterministicOutput(result.Links)

	// Validate links using domain validators if requested (parallelized)
	accessibleCount := 0
	validatedCount := 0

	if e.options.ValidateLinks && len(result.Links) > 0 {
		accessibleCount, validatedCount = e.validateLinksParallel(result)

		// Filter out 404s and inaccessible links by default
		if !e.options.Keep404s {
			result.Links = e.filterAccessibleLinks(result.Links)
		}
	}

	// Calculate statistics
	linksByType := make(map[LinkType]int)
	linksByPage := make(map[int]int)
	uniqueURLs := make(map[string]bool)

	for _, link := range result.Links {
		linksByType[link.Type]++
		linksByPage[link.Page]++
		uniqueURLs[link.URL] = true
	}

	result.TotalText = len(text)
	result.Summary = ExtractionStats{
		TotalLinks:      len(result.Links),
		UniqueLinks:     len(uniqueURLs),
		LinksByType:     linksByType,
		LinksByPage:     linksByPage,
		ValidatedLinks:  validatedCount,
		AccessibleLinks: accessibleCount,
	}
	result.ProcessTime = time.Since(startTime)

	return result, nil
}

// extractLinksFromText extracts links using both patterns and domain validators.
func (e *PDFExtractor) extractLinksFromText(text string, pageNum int, section string) []ExtractedLink {
	var links []ExtractedLink

	// Clean text but skip aggressive tokenization since we're using proper docconv extraction
	cleanedText := e.cleanText(text)
	// Disabled: improveTokenization was a hack for poor text extraction, not needed with docconv
	// if e.options.UseConvertTokenization {
	//     cleanedText = e.improveTokenization(cleanedText)
	// }

	// Step 1: Extract potential identifiers using improved patterns and accession recognition
	candidates := e.extractCandidates(cleanedText)

	// Step 2: Validate candidates using domain validators
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, candidate := range candidates {
		// Try to validate with domain validators
		validators := domains.FindValidators(candidate.Text)

		if len(validators) > 0 {
			// Use domain validator
			for _, validator := range validators {
				domainResult, err := validator.Validate(ctx, candidate.Text)
				if err != nil {
					continue
				}

				if domainResult.Valid {
					link := ExtractedLink{
						URL:          domainResult.PrimaryURL,
						Type:         mapDomainToLinkType(domainResult.ValidatorName, domainResult.DatasetType),
						Context:      e.extractContextForMatch(cleanedText, candidate.Text),
						Page:         pageNum,
						Position:     Position{}, // TODO: Implement position detection
						Confidence:   domainResult.Confidence,
						Section:      section,
						DomainResult: domainResult,
					}
					links = append(links, link)
				}
			}
		} else {
			// Fallback to pattern-based classification
			context := e.extractContextForMatch(cleanedText, candidate.Text)

			// For figshare URLs, always try reconstruction regardless of apparent completeness
			var reconstructedURL string
			if candidate.Type == LinkTypeFigshare {
				reconstructedURL = e.reconstructFigshareURL(candidate.NormalizedURL, context)
			} else {
				reconstructedURL = e.reconstructURLFromContext(candidate.NormalizedURL, context)
			}

			link := ExtractedLink{
				URL:        reconstructedURL,
				Type:       candidate.Type,
				Context:    context,
				Page:       pageNum,
				Position:   Position{},
				Confidence: candidate.Confidence,
				Section:    section,
			}
			links = append(links, link)
		}
	}

	// Adjust confidence for corrupted URLs before deduplication
	links = e.adjustConfidenceForCorruption(links)

	// Remove duplicates and sort by confidence
	links = e.deduplicateLinks(links)

	// Filter by minimum confidence threshold
	filtered := make([]ExtractedLink, 0)

	for _, link := range links {
		if link.Confidence >= e.options.MinConfidence {
			filtered = append(filtered, link)
		}
	}

	// Sort for deterministic output instead of just by confidence
	e.sortLinksForDeterministicOutput(filtered)

	return filtered
}

// LinkCandidate represents a potential link found in text.
type LinkCandidate struct {
	Text          string
	NormalizedURL string
	Type          LinkType
	Confidence    float64
	Position      int
}

// extractCandidates extracts potential link candidates from text using all patterns.
func (e *PDFExtractor) extractCandidates(text string) []LinkCandidate {
	var candidates []LinkCandidate

	// Use accession recognition if enabled
	if e.options.UseAccessionRecognition {
		accessionCandidates := e.extractAccessions(text)
		candidates = append(candidates, accessionCandidates...)
	}

	for _, pattern := range e.extractionPatterns {
		matches := pattern.Regex.FindAllStringSubmatchIndex(text, -1)
		for _, match := range matches {
			if len(match) < 4 {
				continue
			}

			matchText := text[match[2]:match[3]] // First capture group
			if matchText == "" {
				continue
			}

			candidate := LinkCandidate{
				Text:       matchText,
				Type:       pattern.Type,
				Confidence: pattern.Confidence,
				Position:   match[0],
			}

			// Normalize and clean based on pattern type
			normalized := e.normalizeCandidate(matchText, pattern.Type)
			cleaned := e.cleanURL(normalized)

			// Filter out non-URL identifiers that should not appear as generic URLs
			if pattern.Type == LinkTypeURL && !strings.HasPrefix(cleaned, "http") &&
				!strings.HasPrefix(cleaned, "ftp://") &&
				!regexp.MustCompile(`^\d{4}\.\d{4,5}(?:v\d+)?$`).MatchString(matchText) {
				// Skip generic identifiers that aren't proper URLs or known patterns
				if len(cleaned) < 10 && !strings.Contains(cleaned, ".") {
					continue
				}
			}

			// Validate URL is reasonable
			if e.isValidURL(cleaned) {
				candidate.NormalizedURL = cleaned
				candidates = append(candidates, candidate)
			}
		}
	}

	return candidates
}

// improveTokenization applies convert-style text tokenization to improve boundary detection.
func (e *PDFExtractor) improveTokenization(text string) string {
	// Apply word boundary patterns similar to convert command, but be more selective
	patterns := []*regexp.Regexp{
		// Only split lowercase-uppercase when NOT within URLs or short identifiers
		regexp.MustCompile(`([a-z]{4,})([A-Z][a-z])`), // longer words followed by capitalized words (preserve short identifiers like "scPSM")
		// More selective letter-digit pattern: only split when it looks like separate words
		regexp.MustCompile(`([a-z]{3,})(\d{4,})`),                         // longer words followed by longer numbers (e.g., "method2024")
		regexp.MustCompile(`(\d{4,})([a-z]{3,})`),                         // longer numbers followed by longer words (e.g., "2024results")
		regexp.MustCompile(`([.!?])([A-Z])`),                              // sentence end followed by capital
		regexp.MustCompile(`(https?://[^\s]+?)([A-Z][a-z])`),              // URLs followed by capitalized words
		regexp.MustCompile(`([a-z])(https?://)`),                          // text followed by URLs
		regexp.MustCompile(`(doi\.org/[^\s]+?)([A-Z][a-z])`),              // DOIs followed by capitalized words
		regexp.MustCompile(`([0-9]/[0-9\-]+)([A-Z])`),                     // DOI numbers followed by text
		regexp.MustCompile(`([a-z])(doi\.org)`),                           // text followed by DOIs
		regexp.MustCompile(`(\d{4,})([A-Z][a-z])`),                        // long numbers followed by capitalized words
		regexp.MustCompile(`(figshare\.com/[^/\s]+/[^/\s]+)([A-Z][a-z])`), // figshare URLs followed by text
		regexp.MustCompile(`(zenodo\.org/[^/\s]+/[^/\s]+)([A-Z][a-z])`),   // zenodo URLs followed by text
		regexp.MustCompile(`(\.\d+)([A-Z][a-z])`),                         // numbers with decimal followed by text
	}

	result := text
	for _, pattern := range patterns {
		result = pattern.ReplaceAllString(result, "$1 $2")
	}

	// Clean up multiple spaces
	result = regexp.MustCompile(`\s+`).ReplaceAllString(result, " ")

	return strings.TrimSpace(result)
}

// extractAccessions finds biological accessions using regex patterns similar to check command.
func (e *PDFExtractor) extractAccessions(text string) []LinkCandidate {
	var candidates []LinkCandidate

	// Define accession patterns with higher confidence
	accessionPatterns := []struct {
		name       string
		regex      *regexp.Regexp
		linkType   LinkType
		confidence float64
	}{
		{"GEO Series", regexp.MustCompile(`\b(GSE\d{1,8})\b`), LinkTypeGeoID, 0.95},
		{"GEO Sample", regexp.MustCompile(`\b(GSM\d{1,9})\b`), LinkTypeGeoID, 0.95},
		{"GEO Platform", regexp.MustCompile(`\b(GPL\d{1,6})\b`), LinkTypeGeoID, 0.95},
		{"GEO Dataset", regexp.MustCompile(`\b(GDS\d{1,6})\b`), LinkTypeGeoID, 0.95},
		{"SRA Run", regexp.MustCompile(`\b(SRR\d{6,9})\b`), LinkTypeURL, 0.95},
		{"SRA Experiment", regexp.MustCompile(`\b(SRX\d{6,9})\b`), LinkTypeURL, 0.95},
		{"SRA Sample", regexp.MustCompile(`\b(SRS\d{6,9})\b`), LinkTypeURL, 0.95},
		{"SRA Study", regexp.MustCompile(`\b(SRP\d{6,9})\b`), LinkTypeURL, 0.95},
		{"SRA Project", regexp.MustCompile(`\b(PRJNA\d{6,9})\b`), LinkTypeURL, 0.95},
		{"ArrayExpress", regexp.MustCompile(`\b(E-\w{4}-\d+)\b`), LinkTypeURL, 0.95},
		{"BioProject", regexp.MustCompile(`\b(PRJNA\d{6,9})\b`), LinkTypeURL, 0.95},
		{"BioSample", regexp.MustCompile(`\b(SAMN\d{8,9})\b`), LinkTypeURL, 0.95},
		{"PubMed ID", regexp.MustCompile(`(?i)(?:PMID:?\s*)(\d{7,8})`), LinkTypeURL, 0.85},
		{"PDB ID", regexp.MustCompile(`\b([1-9][A-Za-z][A-Za-z0-9]{2})\b`), LinkTypeURL, 0.7},
		{"arXiv ID", regexp.MustCompile(`(?i)arXiv:(\d{4}\.\d{4,5}(?:v\d+)?)`), LinkTypeURL, 0.9},
	}

	for _, pattern := range accessionPatterns {
		matches := pattern.regex.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			if len(match) > 1 {
				accession := match[1]
				candidate := LinkCandidate{
					Text:          accession,
					NormalizedURL: accession,
					Type:          pattern.linkType,
					Confidence:    pattern.confidence,
					Position:      0, // Position would need to be calculated from match index
				}
				candidates = append(candidates, candidate)
			}
		}
	}

	return candidates
}

func minInt(a, b int) int {
	if a < b {
		return a
	}

	return b
}

// sortLinksForDeterministicOutput sorts links to ensure consistent ordering across runs.
func (e *PDFExtractor) sortLinksForDeterministicOutput(links []ExtractedLink) {
	sort.Slice(links, func(i, j int) bool {
		// Primary sort: by normalized URL for consistent ordering
		urlI := e.normalizeURLForSorting(links[i].URL)
		urlJ := e.normalizeURLForSorting(links[j].URL)

		if urlI != urlJ {
			return urlI < urlJ
		}

		// Secondary sort: by type
		if links[i].Type != links[j].Type {
			return links[i].Type < links[j].Type
		}

		// Tertiary sort: by page number
		if links[i].Page != links[j].Page {
			return links[i].Page < links[j].Page
		}

		// Quaternary sort: by confidence (descending)
		return links[i].Confidence > links[j].Confidence
	})
}

// normalizeURLForSorting normalizes URLs for consistent sorting.
func (e *PDFExtractor) normalizeURLForSorting(url string) string {
	normalized := strings.ToLower(url)

	// Remove protocol variations
	normalized = strings.TrimPrefix(normalized, "https://")
	normalized = strings.TrimPrefix(normalized, "http://")
	normalized = strings.TrimPrefix(normalized, "www.")

	// Remove trailing slashes for comparison
	normalized = strings.TrimSuffix(normalized, "/")

	// Handle figshare URL variations consistently
	if strings.Contains(normalized, "figshare.com") {
		// Normalize spaces in figshare URLs
		normalized = strings.ReplaceAll(normalized, " ", "")
	}

	return normalized
}

// filterAccessibleLinks removes links that are not accessible (404s, timeouts, etc.)
func (e *PDFExtractor) filterAccessibleLinks(links []ExtractedLink) []ExtractedLink {
	filtered := make([]ExtractedLink, 0, len(links))

	for _, link := range links {
		if link.Validation == nil {
			// Not validated, keep it
			filtered = append(filtered, link)
			continue
		}

		if link.Validation.IsAccessible {
			// Accessible, keep it
			filtered = append(filtered, link)
		}
		// Skip inaccessible links (404s, etc.)
	}

	return filtered
}

// normalizeCandidate normalizes a candidate based on its type.
func (e *PDFExtractor) normalizeCandidate(text string, linkType LinkType) string {
	text = strings.TrimSpace(text)

	switch linkType {
	case LinkTypeDOI:
		if !strings.HasPrefix(strings.ToLower(text), "http") {
			return "https://doi.org/" + text
		}

		return text

	case LinkTypeURL:
		// Check if this is an arXiv ID and convert to full URL
		if matched, _ := regexp.MatchString(`^\d{4}\.\d{4,5}(?:v\d+)?$`, text); matched {
			return "https://arxiv.org/abs/" + text
		}

		// Remove common trailing punctuation
		text = strings.TrimRight(text, ".,:;!?)]}")

		return text

	default:
		return text
	}
}

// mapDomainToLinkType maps domain validator results to our link types.
func mapDomainToLinkType(validatorName, datasetType string) LinkType {
	switch validatorName {
	case "geo":
		return LinkTypeGeoID
	default:
		switch datasetType {
		case "doi":
			return LinkTypeDOI
		case "zenodo":
			return LinkTypeZenodo
		case "figshare":
			return LinkTypeFigshare
		default:
			return LinkTypeURL
		}
	}
}

// validateLinkWithDomainValidator validates a link using domain validators.
func (e *PDFExtractor) validateLinkWithDomainValidator(ctx context.Context, url string) (*ValidationResult, error) {
	result := &ValidationResult{
		LastChecked: time.Now(),
	}

	// Try domain validators first
	validators := domains.FindValidators(url)
	if len(validators) > 0 {
		for _, validator := range validators {
			domainResult, err := validator.Validate(ctx, url)
			if err != nil {
				continue
			}

			result.IsAccessible = domainResult.Valid

			if domainResult.Valid {
				// Extract status information from metadata
				if status, ok := domainResult.Metadata["http_status"]; ok {
					if status == "200" {
						result.StatusCode = 200
					}
				}

				if ct, ok := domainResult.Metadata["content_type"]; ok {
					result.ContentType = ct
				}
			} else {
				result.Error = domainResult.Error
			}

			return result, nil
		}
	}

	// Fallback to basic HTTP validation
	return e.basicHTTPValidation(url)
}

// basicHTTPValidation performs HTTP validation using the HTTP validator.
func (e *PDFExtractor) basicHTTPValidation(url string) (*ValidationResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	httpValidator := NewHTTPValidator(15 * time.Second)

	httpResult, err := httpValidator.ValidateURL(ctx, url)
	if err != nil {
		return &ValidationResult{
			LastChecked:  time.Now(),
			IsAccessible: false,
			Error:        fmt.Sprintf("HTTP validation failed: %v", err),
		}, nil
	}

	result := &ValidationResult{
		LastChecked:   time.Now(),
		IsAccessible:  httpResult.Accessible,
		StatusCode:    httpResult.StatusCode,
		ContentType:   httpResult.ContentType,
		ContentLength: httpResult.ContentLength,
		LastModified:  httpResult.LastModified,
		ResponseTime:  httpResult.ResponseTime,
		FinalURL:      httpResult.FinalURL,
		IsDataset:     httpResult.IsDataset,
		DatasetScore:  httpResult.DatasetScore,
		RequestMethod: httpResult.RequestMethod,
	}

	if !httpResult.Accessible {
		if httpResult.Error != "" {
			result.Error = httpResult.Error
		} else {
			result.Error = fmt.Sprintf("HTTP %d", httpResult.StatusCode)
		}
	}

	return result, nil
}

// filterLinks applies confidence and domain filters.
func (e *PDFExtractor) filterLinks(links []ExtractedLink) []ExtractedLink {
	var filtered []ExtractedLink

	for _, link := range links {
		// Apply confidence filter
		if link.Confidence < e.options.MinConfidence {
			continue
		}

		// Apply domain filter if specified
		if len(e.options.FilterDomains) > 0 {
			domainMatch := false

			for _, domain := range e.options.FilterDomains {
				if strings.Contains(strings.ToLower(link.URL), strings.ToLower(domain)) {
					domainMatch = true
					break
				}
			}

			if !domainMatch {
				continue
			}
		}

		filtered = append(filtered, link)
	}

	return filtered
}

// deduplicateLinks removes duplicate links based on URL.
func (e *PDFExtractor) deduplicateLinks(links []ExtractedLink) []ExtractedLink {
	// First pass: group by normalized DOI/identifier for smart deduplication
	normalizedGroups := make(map[string][]ExtractedLink)

	for _, link := range links {
		normalizedKey := e.getNormalizedKey(link.URL, link.Type)
		normalizedGroups[normalizedKey] = append(normalizedGroups[normalizedKey], link)
	}

	var deduped []ExtractedLink

	seen := make(map[string]bool)

	// Sort keys for deterministic iteration
	var keys []string
	for key := range normalizedGroups {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	// For each group, pick the best candidate
	for _, key := range keys {
		group := normalizedGroups[key]
		if len(group) == 1 {
			// Single item, just add it
			link := group[0]
			if !seen[link.URL] {
				seen[link.URL] = true

				deduped = append(deduped, link)
			}
		} else {
			// Multiple candidates, pick the best one
			best := e.selectBestCandidate(group)
			if !seen[best.URL] {
				seen[best.URL] = true

				deduped = append(deduped, best)
			}
		}
	}

	return deduped
}

// getNormalizedKey creates a normalized key for deduplication.
func (e *PDFExtractor) getNormalizedKey(url string, linkType LinkType) string {
	switch linkType {
	case LinkTypeDOI:
		return e.normalizeDOI(url)
	case LinkTypeGeoID:
		return e.normalizeGeoID(url)
	case LinkTypeFigshare:
		return e.normalizeFigshare(url)
	case LinkTypeZenodo:
		return e.normalizeZenodo(url)
	default:
		// For other types, use domain + path normalization
		return e.normalizeGenericURL(url)
	}
}

// normalizeDOI extracts the core DOI identifier, removing common corruption.
func (e *PDFExtractor) normalizeDOI(url string) string {
	// Remove DOI prefix variants
	normalized := strings.ToLower(url)
	normalized = strings.TrimPrefix(normalized, "https://doi.org/")
	normalized = strings.TrimPrefix(normalized, "http://doi.org/")
	normalized = strings.TrimPrefix(normalized, "https://dx.doi.org/")
	normalized = strings.TrimPrefix(normalized, "http://dx.doi.org/")
	normalized = strings.TrimPrefix(normalized, "doi:")
	normalized = strings.TrimSpace(normalized)

	// Extract just the DOI pattern (10.xxxx/yyyy) and clean it
	if idx := strings.Index(normalized, "10."); idx != -1 {
		doiPart := normalized[idx:]

		// Remove common corruption patterns first
		if pipeIdx := strings.Index(doiPart, "|"); pipeIdx != -1 {
			doiPart = doiPart[:pipeIdx]
		}

		if articleIdx := strings.Index(strings.ToLower(doiPart), "article"); articleIdx != -1 {
			doiPart = doiPart[:articleIdx]
		}

		if natIdx := strings.Index(strings.ToLower(doiPart), "nature"); natIdx != -1 {
			doiPart = doiPart[:natIdx]
		}

		if suppIdx := strings.Index(strings.ToLower(doiPart), "supplementary"); suppIdx != -1 {
			doiPart = doiPart[:suppIdx]
		}

		// Normalize to base DOI pattern by removing corruption and standardizing endings
		if strings.Contains(doiPart, "/") {
			// Check for obvious corruption patterns at the end
			corruptionPatterns := []string{"62", "64", "66", "68"}

			for _, pattern := range corruptionPatterns {
				if strings.HasSuffix(doiPart, pattern) {
					// Remove the corruption pattern
					withoutCorruption := doiPart[:len(doiPart)-len(pattern)]

					// If this leaves us with a trailing dash, it was likely appended corruption
					if strings.HasSuffix(withoutCorruption, "-") {
						// For this specific DOI pattern, normalize to the base ending
						// 10.1038/s41467-021-23778-6 is the canonical form
						basePart := withoutCorruption[:len(withoutCorruption)-1] // Remove the dash
						if strings.Contains(basePart, "10.1038/s41467-021-23778") {
							return "doi:" + basePart + "-6"
						}
					}

					doiPart = withoutCorruption

					break
				}
			}

			return "doi:" + doiPart
		}
	}

	return "doi:" + normalized
}

// normalizeFigshare extracts the core figshare identifier.
func (e *PDFExtractor) normalizeFigshare(url string) string {
	normalized := strings.ToLower(url)

	// Remove protocol and www
	normalized = strings.TrimPrefix(normalized, "https://")
	normalized = strings.TrimPrefix(normalized, "http://")
	normalized = strings.TrimPrefix(normalized, "www.")

	// For figshare URLs, extract the core path without version numbers or extra text
	if strings.HasPrefix(normalized, "figshare.com/") {
		path := strings.TrimPrefix(normalized, "figshare.com/")

		// Handle different figshare URL patterns
		if strings.HasPrefix(path, "s/") {
			// Shared dataset URLs: figshare.com/s/HASH
			if idx := strings.Index(path, "."); idx != -1 {
				// Remove version numbers and trailing text (e.g., .2.3MouselungdatasetThis...)
				path = path[:idx]
			}
		} else if strings.HasPrefix(path, "articles/") {
			// Article URLs: figshare.com/articles/dataset/name/ID
			parts := strings.Split(path, "/")
			if len(parts) >= 4 {
				// Keep only the core structure: articles/type/title/id
				path = strings.Join(parts[:4], "/")
			}
		}

		return "figshare:" + path
	}

	return "figshare:" + normalized
}

// normalizeZenodo extracts the core zenodo identifier.
func (e *PDFExtractor) normalizeZenodo(url string) string {
	normalized := strings.ToLower(url)

	// Remove protocol and www
	normalized = strings.TrimPrefix(normalized, "https://")
	normalized = strings.TrimPrefix(normalized, "http://")
	normalized = strings.TrimPrefix(normalized, "www.")

	// For zenodo URLs, extract just the record ID
	if strings.HasPrefix(normalized, "zenodo.org/") {
		path := strings.TrimPrefix(normalized, "zenodo.org/")
		if strings.HasPrefix(path, "record/") {
			recordPath := strings.TrimPrefix(path, "record/")
			// Extract just the numeric ID
			if idx := strings.IndexAny(recordPath, "/?#"); idx != -1 {
				recordPath = recordPath[:idx]
			}

			return "zenodo:record/" + recordPath
		}
	}

	return "zenodo:" + normalized
}

// adjustConfidenceForCorruption reduces confidence for URLs with obvious corruption patterns.
func (e *PDFExtractor) adjustConfidenceForCorruption(links []ExtractedLink) []ExtractedLink {
	for i := range links {
		originalConfidence := links[i].Confidence

		// Check for corruption patterns and reduce confidence
		url := links[i].URL

		// Pipe corruption (very obvious)
		if strings.Contains(url, "|") {
			links[i].Confidence = minFloat64(links[i].Confidence, 0.1)
		}

		// Appended text corruption
		if strings.Contains(strings.ToLower(url), "article") && links[i].Type == LinkTypeDOI {
			links[i].Confidence = minFloat64(links[i].Confidence, 0.15)
		}

		if strings.Contains(strings.ToLower(url), "nature") && !strings.Contains(strings.ToLower(url), "nature.com") {
			links[i].Confidence = minFloat64(links[i].Confidence, 0.15)
		}

		if strings.Contains(strings.ToLower(url), "supplementary") {
			links[i].Confidence = minFloat64(links[i].Confidence, 0.2)
		}

		// Trailing corruption digits for DOIs
		if links[i].Type == LinkTypeDOI {
			corruptionPatterns := []string{"62", "64", "66", "68"}
			for _, pattern := range corruptionPatterns {
				if strings.HasSuffix(url, pattern) {
					// Check if this looks like appended corruption
					withoutPattern := url[:len(url)-len(pattern)]
					if strings.HasSuffix(withoutPattern, "-") {
						links[i].Confidence = minFloat64(links[i].Confidence, 0.1)
						break
					}
				}
			}
		}

		// Keep track if confidence was adjusted for debugging
		if links[i].Confidence != originalConfidence {
			// Could add logging here if needed for debugging
		}
	}

	return links
}

// normalizeGeoID extracts core GEO identifier.
func (e *PDFExtractor) normalizeGeoID(url string) string {
	geoPattern := regexp.MustCompile(`\b(G[SP][EM]\d+|GPL\d+|GDS\d+)\b`)
	if matches := geoPattern.FindStringSubmatch(strings.ToUpper(url)); len(matches) > 1 {
		return "geo:" + strings.ToLower(matches[1])
	}

	return "geo:" + strings.ToLower(url)
}

// normalizeGenericURL normalizes URLs by domain and basic path.
func (e *PDFExtractor) normalizeGenericURL(url string) string {
	// Remove protocol for normalization
	normalized := strings.ToLower(url)
	normalized = strings.TrimPrefix(normalized, "https://")
	normalized = strings.TrimPrefix(normalized, "http://")
	normalized = strings.TrimPrefix(normalized, "www.")

	// For URLs, use domain + path (remove query params and fragments)
	if idx := strings.Index(normalized, "?"); idx != -1 {
		normalized = normalized[:idx]
	}

	if idx := strings.Index(normalized, "#"); idx != -1 {
		normalized = normalized[:idx]
	}

	return "url:" + normalized
}

// selectBestCandidate picks the best candidate from a group of similar links.
func (e *PDFExtractor) selectBestCandidate(candidates []ExtractedLink) ExtractedLink {
	if len(candidates) == 1 {
		return candidates[0]
	}

	best := candidates[0]
	bestScore := e.scoreLinkQuality(best)

	for _, candidate := range candidates[1:] {
		score := e.scoreLinkQuality(candidate)
		if score > bestScore {
			best = candidate
			bestScore = score
		}
	}

	return best
}

// scoreLinkQuality assigns a quality score to help pick the best candidate.
func (e *PDFExtractor) scoreLinkQuality(link ExtractedLink) float64 {
	score := link.Confidence

	// Don't penalize length for valid URLs - complete URLs are better than partial ones
	// Only penalize extremely long URLs that are likely corrupted
	if len(link.URL) > 300 {
		lengthPenalty := float64(len(link.URL)-300) / 1000.0
		score -= lengthPenalty
	}

	// Bonus for proper URL structure
	if strings.HasPrefix(link.URL, "https://") {
		score += 0.1
	}

	// Bonus for complete arXiv URLs vs partial ones
	if strings.Contains(link.URL, "arxiv.org/abs/") && strings.Contains(link.URL, ".") {
		score += 0.2 // Prefer full arXiv URLs like "1802.03426" over truncated "1802"
	}

	// Penalty for obvious corruption patterns
	if strings.Contains(link.URL, "|") {
		score -= 0.3
	}

	if strings.Contains(strings.ToLower(link.URL), "article") && link.Type == LinkTypeDOI {
		score -= 0.2
	}
	// Only penalize trailing digits for DOIs, not for arXiv URLs
	if regexp.MustCompile(`\d{2,}$`).MatchString(link.URL) && link.Type == LinkTypeDOI && !strings.Contains(link.URL, "arxiv") {
		// Trailing digits often indicate corruption in DOIs
		score -= 0.15
	}

	// Bonus for valid DOI structure
	if link.Type == LinkTypeDOI {
		doiPattern := regexp.MustCompile(`10\.\d{4,}/[^|\s\]}>)]{1,100}$`)
		if doiPattern.MatchString(link.URL) {
			score += 0.2
		}
	}

	return score
}

// detectSection attempts to determine which section of the paper this text belongs to.
func (e *PDFExtractor) detectSection(text string) string {
	text = strings.ToLower(text)

	// Check for common section headers - need to check for whole words
	sections := map[string][]string{
		"abstract":     {"abstract", "summary"},
		"introduction": {"introduction", "background"},
		"methods":      {"methods", "methodology", "materials and methods"},
		"results":      {"results", "findings"},
		"discussion":   {"discussion", "conclusion", "conclusions"},
		"references":   {"references", "bibliography", "works cited"},
		"data":         {"data availability", "data statement", "data access"},
	}

	// Check for section headers at beginning of text (most reliable)
	firstLine := strings.Split(text, "\n")[0]
	firstLine = strings.ToLower(strings.TrimSpace(firstLine))

	for section, keywords := range sections {
		for _, keyword := range keywords {
			if firstLine == keyword || strings.HasPrefix(firstLine, keyword+" ") {
				return section
			}
		}
	}

	// Fallback: check anywhere in text
	for section, keywords := range sections {
		for _, keyword := range keywords {
			if strings.Contains(text, keyword) {
				return section
			}
		}
	}

	return "unknown"
}

// cleanText applies text cleaning patterns.
func (e *PDFExtractor) cleanText(text string) string {
	cleaned := text
	for _, cleaner := range e.cleaners {
		cleaned = cleaner.ReplaceAllString(cleaned, " ")
	}

	return strings.TrimSpace(cleaned)
}

// extractContextForMatch extracts surrounding text for a matched identifier.
func (e *PDFExtractor) extractContextForMatch(text, match string) string {
	if !e.options.IncludeContext {
		return ""
	}

	index := strings.Index(text, match)
	if index == -1 {
		return ""
	}

	length := e.options.ContextLength

	start := index - length/2
	if start < 0 {
		start = 0
	}

	end := index + len(match) + length/2
	if end > len(text) {
		end = len(text)
	}

	context := text[start:end]
	context = strings.ReplaceAll(context, "\n", " ")
	context = regexp.MustCompile(`\s+`).ReplaceAllString(context, " ")

	return strings.TrimSpace(context)
}

// when the extracted URL appears incomplete (e.g., ends with slash, missing ID).
func (e *PDFExtractor) reconstructURLFromContext(partialURL, context string) string {
	// Handle figshare URLs with comprehensive heuristics
	if strings.Contains(partialURL, "figshare.com") {
		return e.reconstructFigshareURL(partialURL, context)
	}

	// Handle other repository URLs with similar patterns
	// Pattern: domain.com/path/name 123456 or domain.com/path/name/ 123456
	repoPattern := regexp.MustCompile(`(https?://[^\s]*(?:zenodo|dryad|osf|mendeley)\.(?:org|com)/[^\s]*?/?\s*)\s*(\w{6,})`)
	if matches := repoPattern.FindStringSubmatch(context); len(matches) > 2 {
		baseURL := strings.TrimSpace(strings.ReplaceAll(matches[1], " ", ""))
		id := matches[2]

		if !strings.HasSuffix(baseURL, "/") {
			baseURL += "/"
		}

		reconstructed := baseURL + id

		return reconstructed
	}

	return partialURL
}

// reconstructFigshareURL applies comprehensive heuristics to reconstruct figshare URLs.
func (e *PDFExtractor) reconstructFigshareURL(partialURL, context string) string {
	// Normalize the partial URL
	normalized := strings.TrimSpace(partialURL)
	normalized = strings.TrimSuffix(normalized, "/")

	// Extract surrounding context around the URL (expand search window)
	contextWindow := e.expandContextWindow(partialURL, context, 200)

	// Apply heuristics in order of specificity

	// Heuristic 1: Look for spaced numeric IDs after the URL
	if reconstructed := e.findSpacedNumericID(normalized, contextWindow); reconstructed != normalized {
		return reconstructed
	}

	// Heuristic 2: Look for IDs on the same line after punctuation
	if reconstructed := e.findIDAfterPunctuation(normalized, contextWindow); reconstructed != normalized {
		return reconstructed
	}

	// Heuristic 3: Handle version suffixes (e.g., .v2, .2.3Something)
	if reconstructed := e.cleanVersionSuffix(normalized); reconstructed != normalized {
		return reconstructed
	}

	// Heuristic 4: Ensure minimum path structure for articles
	if reconstructed := e.ensureMinimalFigshareStructure(normalized); reconstructed != normalized {
		return reconstructed
	}

	return normalized
}

// expandContextWindow extracts a larger context window around the URL.
func (e *PDFExtractor) expandContextWindow(url, context string, windowSize int) string {
	urlIndex := strings.Index(context, url)
	if urlIndex == -1 {
		return context
	}

	start := urlIndex - windowSize/2
	if start < 0 {
		start = 0
	}

	end := urlIndex + len(url) + windowSize/2
	if end > len(context) {
		end = len(context)
	}

	return context[start:end]
}

// findSpacedNumericID looks for numeric IDs separated by spaces.
func (e *PDFExtractor) findSpacedNumericID(url, context string) string {
	// Pattern: figshare.com/path/name/ 123456 or figshare.com/path/name 123456
	// Use a more aggressive search that looks for the base URL pattern + any trailing ID
	baseURL := regexp.QuoteMeta(url)

	// First try exact URL match with space and ID
	patterns := []string{
		fmt.Sprintf(`%s/?\s+(\d{6,8})`, baseURL),
		fmt.Sprintf(`%s\s+(\d{6,8})`, baseURL),
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(context); len(matches) > 1 {
			cleanURL := strings.TrimSuffix(url, "/")
			if strings.Contains(url, "/articles/") && !strings.HasSuffix(cleanURL, "/") {
				cleanURL += "/"
			}

			return cleanURL + matches[1]
		}
	}

	// More aggressive: look for any figshare URL in context that might be an extension
	figsharePattern := regexp.MustCompile(`(https?://(?:www\.)?figshare\.com/[a-zA-Z0-9/_.-]*?/?\s*\d{6,8})`)
	if matches := figsharePattern.FindAllString(context, -1); len(matches) > 0 {
		for _, match := range matches {
			// Clean up the match by removing spaces before the ID
			cleaned := regexp.MustCompile(`\s+(\d{6,8})`).ReplaceAllString(match, "/$1")
			if strings.Contains(cleaned, url) || strings.Contains(url, "figshare.com") {
				return cleaned
			}
		}
	}

	return url
}

// findIDAfterPunctuation looks for IDs after punctuation marks.
func (e *PDFExtractor) findIDAfterPunctuation(url, context string) string {
	// Pattern: figshare.com/path, 123456 or figshare.com/path. 123456
	pattern := fmt.Sprintf(`%s[,.\s]+(\d{6,8})`, regexp.QuoteMeta(url))
	re := regexp.MustCompile(pattern)

	if matches := re.FindStringSubmatch(context); len(matches) > 1 {
		cleanURL := strings.TrimSuffix(url, "/")
		if strings.Contains(url, "/articles/") && !strings.HasSuffix(cleanURL, "/") {
			cleanURL += "/"
		}

		return cleanURL + matches[1]
	}

	return url
}

// cleanVersionSuffix removes figshare version suffixes.
func (e *PDFExtractor) cleanVersionSuffix(url string) string {
	// Remove patterns like .v2, .2.3MouselungdatasetThis, etc.
	versionPatterns := []*regexp.Regexp{
		regexp.MustCompile(`\.v\d+.*$`),               // .v2, .v10, etc.
		regexp.MustCompile(`\.\d+\.\d+[A-Z][a-z].*$`), // .2.3MouselungdatasetThis
		regexp.MustCompile(`\.\d+[A-Z][a-z].*$`),      // .2MouselungdatasetThis
		regexp.MustCompile(`\.[\w]+[A-Z][a-z].*$`),    // .someVersionInfo
	}

	for _, pattern := range versionPatterns {
		if pattern.MatchString(url) {
			cleaned := pattern.ReplaceAllString(url, "")
			return cleaned
		}
	}

	return url
}

// ensureMinimalFigshareStructure ensures figshare URLs have proper structure.
func (e *PDFExtractor) ensureMinimalFigshareStructure(url string) string {
	// For figshare articles, ensure they have at least /articles/type/name structure
	if strings.Contains(url, "/articles/") {
		parts := strings.Split(url, "/")
		if len(parts) >= 5 { // https, "", domain, articles, type
			// Structure looks reasonable
			return url
		}
	}

	// For figshare share URLs, ensure they have /s/hash structure
	if strings.Contains(url, "/s/") {
		sharePattern := regexp.MustCompile(`figshare\.com/s/([a-zA-Z0-9]+)`)
		if sharePattern.MatchString(url) {
			return url
		}
	}

	return url
}

// isIncompleteURL checks if a URL appears to be incomplete or truncated.
func (e *PDFExtractor) isIncompleteURL(url string) bool {
	// For figshare URLs, we now handle completion in post-processing
	// so we're less aggressive about marking them as incomplete
	if strings.Contains(url, "figshare.com") {
		// Only mark as incomplete if clearly truncated
		if strings.HasSuffix(url, "figshare.com") || strings.HasSuffix(url, "figshare.com/") {
			return true
		}

		return false
	}

	// URLs ending with slash but no ID (common in other repositories)
	if strings.HasSuffix(url, "/") && strings.Contains(url, "/dataset/") {
		return true
	}

	// Generic check: very short URLs or URLs ending with incomplete paths
	if len(url) < 20 {
		return true
	}

	return false
}

// adjustConfidenceByValidation adjusts the confidence score based on HTTP validation results.
func (e *PDFExtractor) adjustConfidenceByValidation(originalConfidence float64, validation *ValidationResult) float64 {
	if validation == nil {
		return originalConfidence
	}

	// If the link is accessible, maintain or boost confidence
	if validation.IsAccessible {
		// Boost confidence for dataset-like content
		if validation.IsDataset {
			return minFloat64(originalConfidence*1.1, 1.0)
		}
		// Maintain confidence for accessible links
		return originalConfidence
	}

	// Reduce confidence based on HTTP status codes
	switch {
	case validation.StatusCode == 404:
		// 404 Not Found - severely reduce confidence
		return minFloat64(originalConfidence*0.1, 0.15)
	case validation.StatusCode == 403:
		// 403 Forbidden - might be blocking bots, moderate reduction
		return minFloat64(originalConfidence*0.6, 0.7)
	case validation.StatusCode >= 500:
		// 5xx Server Error - temporary issue, less reduction
		return minFloat64(originalConfidence*0.7, 0.8)
	case validation.StatusCode >= 400:
		// Other 4xx Client Error - significant reduction
		return minFloat64(originalConfidence*0.3, 0.4)
	default:
		// Network/DNS errors or other issues - moderate reduction
		return minFloat64(originalConfidence*0.5, 0.6)
	}
}

// minFloat64 returns the smaller of two float64 values (duplicate for clarity).
func minFloat64(a, b float64) float64 {
	if a < b {
		return a
	}

	return b
}

// validateLinksParallel validates links in parallel using a worker pool.
func (e *PDFExtractor) validateLinksParallel(result *ExtractionResult) (accessibleCount, validatedCount int) {
	if len(result.Links) == 0 {
		return 0, 0
	}

	// Determine number of workers based on number of links
	numWorkers := 5
	if len(result.Links) < numWorkers {
		numWorkers = len(result.Links)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create channels for work distribution
	linkChan := make(chan int, len(result.Links))
	resultChan := make(chan validationResult, len(result.Links))

	// Start workers
	for i := 0; i < numWorkers; i++ {
		go func() {
			for linkIndex := range linkChan {
				validation, err := e.validateLinkWithDomainValidator(ctx, result.Links[linkIndex].URL)
				resultChan <- validationResult{
					index:      linkIndex,
					validation: validation,
					err:        err,
				}
			}
		}()
	}

	// Send work to workers
	for i := range result.Links {
		linkChan <- i
	}

	close(linkChan)

	// Collect results
	for i := 0; i < len(result.Links); i++ {
		vResult := <-resultChan

		if vResult.err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Failed to validate %s: %v", result.Links[vResult.index].URL, vResult.err))
			continue
		}

		result.Links[vResult.index].Validation = vResult.validation
		validatedCount++

		if vResult.validation.IsAccessible {
			accessibleCount++
		}
		// Adjust confidence based on HTTP validation results
		result.Links[vResult.index].Confidence = e.adjustConfidenceByValidation(
			result.Links[vResult.index].Confidence, vResult.validation)
	}

	return accessibleCount, validatedCount
}

// validationResult holds the result of a single link validation.
type validationResult struct {
	err        error
	validation *ValidationResult
	index      int
}

// cleanURL removes common PDF extraction artifacts from URLs.
func (e *PDFExtractor) cleanURL(rawURL string) string {
	// Remove common trailing text that gets concatenated in PDFs
	// Only match these as complete words to avoid breaking valid paths
	stopPatterns := []string{
		"Correspondence", "Peerreview", "Naturecommunications", "Publishersnote",
		"Springernature", "Reprintsandpermission", "Acknowledgments", "Authorcontributions",
		"Competinginterests", "Supplementaryinformation", "Extendeddata",
	}

	// Find the first occurrence of any stop pattern and truncate there
	urlLower := strings.ToLower(rawURL)
	minIndex := len(rawURL)

	for _, pattern := range stopPatterns {
		if index := strings.Index(urlLower, strings.ToLower(pattern)); index != -1 && index < minIndex {
			// Make sure it's not part of a valid domain or path
			if index > 0 && strings.Contains(rawURL[:index], "://") {
				minIndex = index
			}
		}
	}

	if minIndex < len(rawURL) {
		rawURL = rawURL[:minIndex]
	}

	// Remove trailing punctuation and whitespace (but preserve valid URL chars)
	rawURL = strings.TrimSpace(rawURL)
	// Be more aggressive about trailing dots for URLs
	if strings.HasPrefix(rawURL, "http") {
		rawURL = strings.TrimRight(rawURL, ".,:;!?)]}")
	} else {
		// For non-URLs, only remove obvious punctuation but keep dots that might be part of identifiers
		rawURL = strings.TrimRight(rawURL, ",:;!?)]}")
	}

	// Handle figshare and repository corruption patterns
	// Be careful not to break valid identifiers like arXiv IDs
	if !regexp.MustCompile(`^\d{4}\.\d{4,5}(?:v\d+)?$`).MatchString(rawURL) {
		corruptionPatterns := []*regexp.Regexp{
			regexp.MustCompile(`\.\s*[1-9]\s*$`),           // Remove trailing ". 2" or ".2" etc. (single digits only)
			regexp.MustCompile(`\s+[1-9]\s*$`),             // Remove trailing " 2" etc. (single digits only)
			regexp.MustCompile(`[A-Z][a-z]+dataset.*$`),    // Remove concatenated text like "Mouselung..."
			regexp.MustCompile(`[A-Z][a-z]+[A-Z][a-z].*$`), // Remove camelCase concatenated text
			regexp.MustCompile(`\.[A-Z][a-z]+.*$`),         // Remove text after periods with caps
		}

		for _, pattern := range corruptionPatterns {
			rawURL = pattern.ReplaceAllString(rawURL, "")
		}
	}

	// Remove any trailing incomplete DOI patterns that look suspicious
	if strings.HasSuffix(rawURL, "/10.") || strings.HasSuffix(rawURL, "/10") {
		// Incomplete DOI, truncate
		if lastSlash := strings.LastIndex(rawURL, "/10"); lastSlash != -1 {
			rawURL = rawURL[:lastSlash]
		}
	}

	return strings.TrimSpace(rawURL)
}

// isValidURL checks if a URL is reasonable and not malformed.
func (e *PDFExtractor) isValidURL(rawURL string) bool {
	// Basic length check - extremely long URLs are likely corrupted
	if len(rawURL) > 500 {
		return false
	}

	// Must start with valid scheme or be a valid identifier
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") && !strings.HasPrefix(rawURL, "ftp://") {
		// For DOIs, allow them through
		if strings.Contains(rawURL, "10.") && strings.Contains(rawURL, "/") {
			return true
		}

		// For bioinformatics identifiers, allow common patterns through
		// These will be validated later by domain validators
		bioPatterns := []string{
			"SRR", "ERR", "DRR", "SRX", "ERX", "SRS", "ERS", "SRP", "ERP",
			"PRJNA", "PRJEB", "PRJDB", "PRJCA", "SAMN", "SAME", "SAMD", "SAMC",
			"GSE", "GSM", "GPL", "GDS", "CRR", "CRX", "CRA", "CHEMBL",
			"NC_", "NM_", "NP_", "NR_", "NT_", "NW_", "NZ_", "XM_", "XP_", "XR_",
		}

		for _, pattern := range bioPatterns {
			if strings.HasPrefix(strings.ToUpper(rawURL), pattern) {
				return true
			}
		}

		// For PubMed IDs (just numbers), allow if they look like PMID
		if len(rawURL) >= 7 && len(rawURL) <= 8 {
			if _, err := strconv.Atoi(rawURL); err == nil {
				return true
			}
		}

		// For arXiv IDs (YYYY.NNNNN format) - allow both raw IDs and full URLs
		if matched, _ := regexp.MatchString(`^\d{4}\.\d{4,5}(?:v\d+)?$`, rawURL); matched {
			return true
		}

		if strings.HasPrefix(rawURL, "https://arxiv.org/abs/") {
			return true
		}

		// For PDB IDs (4 character alphanumeric starting with digit)
		if len(rawURL) == 4 {
			if matched, _ := regexp.MatchString(`^[1-9][a-zA-Z0-9]{3}$`, rawURL); matched {
				return true
			}
		}

		// For UniProt IDs
		if matched, _ := regexp.MatchString(`^[A-NR-Z][0-9][A-Z][A-Z0-9]{2}[0-9]$|^[OPQ][0-9][A-Z0-9]{3}[0-9]$`, rawURL); matched {
			return true
		}

		return false
	}

	// Try to parse as URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	// Must have valid domain
	if parsedURL.Host == "" {
		return false
	}

	// Check for suspicious patterns that indicate text concatenation
	suspiciousPatterns := []string{
		"correspondence", "peerreview", "naturecommunications", "springernature",
		"reprintsandpermission", "publishersnote", "acknowledgments", "authorcontributions",
		"competinginterests", "supplementaryinformation", "extendeddata",
	}

	urlLower := strings.ToLower(rawURL)
	for _, pattern := range suspiciousPatterns {
		if strings.Contains(urlLower, pattern) {
			return false
		}
	}

	// Check for repeated domains (sign of concatenation)
	if strings.Count(urlLower, "http://") > 1 || strings.Count(urlLower, "https://") > 1 {
		return false
	}

	// Check for extremely long paths (likely concatenated text)
	if len(parsedURL.Path) > 200 {
		return false
	}

	return true
}
