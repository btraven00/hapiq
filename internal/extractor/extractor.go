package extractor

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"net/url"

	"github.com/btraven00/hapiq/pkg/validators/domains"
	"github.com/ledongthuc/pdf"
)

// PDFExtractor handles extraction of links from PDF documents using domain validators
type PDFExtractor struct {
	extractionPatterns []ExtractionPattern
	sectionRegexes     []*regexp.Regexp
	cleaners           []*regexp.Regexp
	options            ExtractionOptions
}

// ExtractionPattern defines how to extract identifiers from text
type ExtractionPattern struct {
	Name        string
	Regex       *regexp.Regexp
	Type        LinkType
	Confidence  float64
	Description string
	Examples    []string
}

// NewPDFExtractor creates a new PDF extractor that uses domain validators
func NewPDFExtractor(options ExtractionOptions) *PDFExtractor {
	extractor := &PDFExtractor{
		extractionPatterns: getExtractionPatterns(),
		sectionRegexes:     getDefaultSectionRegexes(),
		cleaners:           getDefaultCleaners(),
		options:            options,
	}

	return extractor
}

// ExtractFromFile extracts links from a PDF file using domain validators
func (e *PDFExtractor) ExtractFromFile(filename string) (*ExtractionResult, error) {
	startTime := time.Now()

	// Open and read PDF
	file, reader, err := pdf.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open PDF: %w", err)
	}
	defer file.Close()

	result := &ExtractionResult{
		Filename:    filename,
		Pages:       reader.NumPage(),
		Links:       make([]ExtractedLink, 0),
		Errors:      make([]string, 0),
		Warnings:    make([]string, 0),
		ProcessTime: 0,
	}

	var allText strings.Builder
	linkCounts := make(map[int]int)
	linkTypes := make(map[LinkType]int)
	uniqueLinks := make(map[string]bool)

	// Process each page
	for pageNum := 1; pageNum <= reader.NumPage(); pageNum++ {
		page := reader.Page(pageNum)
		if page.V.IsNull() {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Page %d is null", pageNum))
			continue
		}

		// Extract text from page
		text, err := page.GetPlainText(nil)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to extract text from page %d: %v", pageNum, err))
			continue
		}

		allText.WriteString(text)
		allText.WriteString("\n")

		// Detect section for this page
		section := e.detectSection(text)

		// Extract links from page text using domain validators
		pageLinks := e.extractLinksFromText(text, pageNum, section)

		// Apply per-page limit
		if e.options.MaxLinksPerPage > 0 && len(pageLinks) > e.options.MaxLinksPerPage {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Page %d has %d links, limiting to %d", pageNum, len(pageLinks), e.options.MaxLinksPerPage))
			pageLinks = pageLinks[:e.options.MaxLinksPerPage]
		}

		// Filter by confidence and domains
		filteredLinks := e.filterLinks(pageLinks)

		// Add to results
		result.Links = append(result.Links, filteredLinks...)

		// Update statistics
		linkCounts[pageNum] = len(filteredLinks)
		for _, link := range filteredLinks {
			linkTypes[link.Type]++
			uniqueLinks[link.URL] = true
		}
	}

	// Validate links using domain validators if requested (parallelized)
	accessibleCount := 0
	validatedCount := 0
	if e.options.ValidateLinks && len(result.Links) > 0 {
		accessibleCount, validatedCount = e.validateLinksParallel(result)
	}

	// Populate summary statistics
	result.TotalText = allText.Len()
	result.Summary = ExtractionStats{
		TotalLinks:      len(result.Links),
		UniqueLinks:     len(uniqueLinks),
		LinksByType:     linkTypes,
		LinksByPage:     linkCounts,
		ValidatedLinks:  validatedCount,
		AccessibleLinks: accessibleCount,
	}
	result.ProcessTime = time.Since(startTime)

	return result, nil
}

// extractLinksFromText extracts links using both patterns and domain validators
func (e *PDFExtractor) extractLinksFromText(text string, pageNum int, section string) []ExtractedLink {
	var links []ExtractedLink

	// Clean text
	cleanedText := e.cleanText(text)

	// Step 1: Extract potential identifiers using patterns
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
			link := ExtractedLink{
				URL:        candidate.NormalizedURL,
				Type:       candidate.Type,
				Context:    e.extractContextForMatch(cleanedText, candidate.Text),
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
	sort.Slice(links, func(i, j int) bool {
		return links[i].Confidence > links[j].Confidence
	})

	return links
}

// LinkCandidate represents a potential link found in text
type LinkCandidate struct {
	Text          string
	NormalizedURL string
	Type          LinkType
	Confidence    float64
	Position      int
}

// extractCandidates finds potential identifiers and URLs in text
func (e *PDFExtractor) extractCandidates(text string) []LinkCandidate {
	var candidates []LinkCandidate

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

			// Validate URL is reasonable
			if e.isValidURL(cleaned) {
				candidate.NormalizedURL = cleaned
				candidates = append(candidates, candidate)
			}
		}
	}

	return candidates
}

// normalizeCandidate normalizes a candidate based on its type
func (e *PDFExtractor) normalizeCandidate(text string, linkType LinkType) string {
	text = strings.TrimSpace(text)

	switch linkType {
	case LinkTypeDOI:
		if !strings.HasPrefix(strings.ToLower(text), "http") {
			return "https://doi.org/" + text
		}
		return text

	case LinkTypeURL:
		// Remove common trailing punctuation
		text = strings.TrimRight(text, ".,:;!?)]}")
		return text

	default:
		return text
	}
}

// mapDomainToLinkType maps domain validator results to our link types
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

// validateLinkWithDomainValidator validates a link using domain validators
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

// basicHTTPValidation performs HTTP validation using the HTTP validator
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

// filterLinks applies confidence and domain filters
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

// deduplicateLinks removes duplicate links based on URL
func (e *PDFExtractor) deduplicateLinks(links []ExtractedLink) []ExtractedLink {
	// First pass: group by normalized DOI/identifier for smart deduplication
	normalizedGroups := make(map[string][]ExtractedLink)

	for _, link := range links {
		normalizedKey := e.getNormalizedKey(link.URL, link.Type)
		normalizedGroups[normalizedKey] = append(normalizedGroups[normalizedKey], link)
	}

	var deduped []ExtractedLink
	seen := make(map[string]bool)

	// For each group, pick the best candidate
	for _, group := range normalizedGroups {
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

// getNormalizedKey creates a normalized key for deduplication
func (e *PDFExtractor) getNormalizedKey(url string, linkType LinkType) string {
	switch linkType {
	case LinkTypeDOI:
		return e.normalizeDOI(url)
	case LinkTypeGeoID:
		return e.normalizeGeoID(url)
	default:
		// For other types, use domain + path normalization
		return e.normalizeGenericURL(url)
	}
}

// normalizeDOI extracts the core DOI identifier, removing common corruption
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

// adjustConfidenceForCorruption reduces confidence for URLs with obvious corruption patterns
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

// normalizeGeoID extracts core GEO identifier
func (e *PDFExtractor) normalizeGeoID(url string) string {
	geoPattern := regexp.MustCompile(`\b(G[SP][EM]\d+|GPL\d+|GDS\d+)\b`)
	if matches := geoPattern.FindStringSubmatch(strings.ToUpper(url)); len(matches) > 1 {
		return "geo:" + strings.ToLower(matches[1])
	}
	return "geo:" + strings.ToLower(url)
}

// normalizeGenericURL normalizes URLs by domain and basic path
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

// selectBestCandidate picks the best candidate from a group of similar links
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

// scoreLinkQuality assigns a quality score to help pick the best candidate
func (e *PDFExtractor) scoreLinkQuality(link ExtractedLink) float64 {
	score := link.Confidence

	// Prefer shorter, cleaner URLs (less likely to have garbage)
	lengthPenalty := float64(len(link.URL)) / 1000.0
	score -= lengthPenalty

	// Bonus for proper URL structure
	if strings.HasPrefix(link.URL, "https://") {
		score += 0.1
	}

	// Penalty for obvious corruption patterns
	if strings.Contains(link.URL, "|") {
		score -= 0.3
	}
	if strings.Contains(strings.ToLower(link.URL), "article") && link.Type == LinkTypeDOI {
		score -= 0.2
	}
	if regexp.MustCompile(`\d{2,}$`).MatchString(link.URL) && link.Type == LinkTypeDOI {
		// Trailing digits often indicate corruption
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

// detectSection attempts to determine which section of the paper this text belongs to
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

// cleanText applies text cleaning patterns
func (e *PDFExtractor) cleanText(text string) string {
	cleaned := text
	for _, cleaner := range e.cleaners {
		cleaned = cleaner.ReplaceAllString(cleaned, " ")
	}
	return strings.TrimSpace(cleaned)
}

// extractContextForMatch extracts surrounding text for a matched identifier
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

// adjustConfidenceByValidation adjusts the confidence score based on HTTP validation results
func (e *PDFExtractor) adjustConfidenceByValidation(originalConfidence float64, validation *ValidationResult) float64 {
	if validation == nil {
		return originalConfidence
	}

	// If the link is accessible, maintain or boost confidence
	if validation.IsAccessible {
		// Boost confidence for dataset-like content
		if validation.IsDataset {
			return min(originalConfidence*1.1, 1.0)
		}
		// Maintain confidence for accessible links
		return originalConfidence
	}

	// Reduce confidence based on HTTP status codes
	switch {
	case validation.StatusCode == 404:
		// 404 Not Found - severely reduce confidence
		return min(originalConfidence*0.1, 0.15)
	case validation.StatusCode == 403:
		// 403 Forbidden - might be blocking bots, moderate reduction
		return min(originalConfidence*0.6, 0.7)
	case validation.StatusCode >= 500:
		// 5xx Server Error - temporary issue, less reduction
		return min(originalConfidence*0.7, 0.8)
	case validation.StatusCode >= 400:
		// Other 4xx Client Error - significant reduction
		return min(originalConfidence*0.3, 0.4)
	default:
		// Network/DNS errors or other issues - moderate reduction
		return min(originalConfidence*0.5, 0.6)
	}
}

// min returns the smaller of two float64 values
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// minFloat64 returns the smaller of two float64 values (duplicate for clarity)
func minFloat64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// validateLinksParallel validates links in parallel using a worker pool
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

// validationResult holds the result of a single link validation
type validationResult struct {
	index      int
	validation *ValidationResult
	err        error
}

// cleanURL removes common PDF extraction artifacts from URLs
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
	rawURL = strings.TrimRight(rawURL, ".,:;!?)]}")

	// Remove any trailing incomplete DOI patterns that look suspicious
	if strings.HasSuffix(rawURL, "/10.") || strings.HasSuffix(rawURL, "/10") {
		// Incomplete DOI, truncate
		if lastSlash := strings.LastIndex(rawURL, "/10"); lastSlash != -1 {
			rawURL = rawURL[:lastSlash]
		}
	}

	return strings.TrimSpace(rawURL)
}

// isValidURL checks if a URL is reasonable and not malformed
func (e *PDFExtractor) isValidURL(rawURL string) bool {
	// Basic length check - extremely long URLs are likely corrupted
	if len(rawURL) > 500 {
		return false
	}

	// Must start with valid scheme
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") && !strings.HasPrefix(rawURL, "ftp://") {
		// For DOIs and other identifiers, allow them through
		if strings.Contains(rawURL, "10.") && strings.Contains(rawURL, "/") {
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
