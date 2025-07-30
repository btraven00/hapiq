package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"code.sajari.com/docconv/v2"
	"github.com/spf13/cobra"
)

var (
	outputFile     string
	preserveLayout bool
	includePages   bool
	extractHeaders bool
)

// textCmd represents the text command
var textCmd = &cobra.Command{
	Use:   "text [pdf-file]",
	Short: "Convert PDF to structured text with Markdown formatting",
	Long: `Convert PDF documents to text with proper tokenization and Markdown formatting.

This command extracts text from PDF files while preserving document structure,
identifying headers, sections, and paragraphs. The output is formatted as
Markdown with proper heading hierarchy and text organization.

Features:
- Automatic header detection and Markdown formatting
- Paragraph separation and text flow preservation
- Optional page number annotations
- Clean text tokenization with proper spacing
- Section structure preservation

Examples:
  hapiq text paper.pdf
  hapiq text --output paper.md --headers paper.pdf
  hapiq text --preserve-layout --include-pages paper.pdf`,
	Args: cobra.ExactArgs(1),
	RunE: runText,
}

func init() {
	rootCmd.AddCommand(textCmd)

	textCmd.Flags().StringVarP(&outputFile, "output", "o", "", "output file (default: stdout)")
	textCmd.Flags().BoolVar(&preserveLayout, "preserve-layout", false, "preserve original text layout and spacing")
	textCmd.Flags().BoolVar(&includePages, "include-pages", false, "include page numbers in output")
	textCmd.Flags().BoolVar(&extractHeaders, "headers", true, "detect and format headers as Markdown")
}

func runText(cmd *cobra.Command, args []string) error {
	filename := args[0]

	if !quiet {
		fmt.Fprintf(os.Stderr, "Converting %s to text...\n", filename)
	}

	// Check if file exists and is readable
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", filename)
	}

	text, err := convertPDFToMarkdown(filename)
	if err != nil {
		return fmt.Errorf("failed to convert PDF: %w", err)
	}

	// Output to file or stdout
	if outputFile != "" {
		if err := os.WriteFile(outputFile, []byte(text), 0644); err != nil {
			return fmt.Errorf("failed to write output file: %w", err)
		}
		if !quiet {
			fmt.Fprintf(os.Stderr, "Converted text written to %s\n", outputFile)
		}
	} else {
		fmt.Print(text)
	}

	return nil
}

// PDFConverter handles PDF to Markdown conversion
type PDFConverter struct {
	preserveLayout bool
	includePages   bool
	extractHeaders bool

	// Regex patterns for text processing
	headerPatterns  []*regexp.Regexp
	bulletPatterns  []*regexp.Regexp
	numberPatterns  []*regexp.Regexp
	whitespaceRegex *regexp.Regexp
	paragraphRegex  *regexp.Regexp
}

// NewPDFConverter creates a new PDF converter with specified options
func NewPDFConverter(preserveLayout, includePages, extractHeaders bool) *PDFConverter {
	return &PDFConverter{
		preserveLayout: preserveLayout,
		includePages:   includePages,
		extractHeaders: extractHeaders,
		headerPatterns: []*regexp.Regexp{
			// Common academic paper header patterns
			regexp.MustCompile(`^[A-Z][A-Z\s]{5,50}$`),                  // ALL CAPS headers
			regexp.MustCompile(`^\d+\.?\s+[A-Z][A-Za-z\s]{5,80}$`),      // Numbered sections
			regexp.MustCompile(`^[A-Z]\w+(?:\s+[A-Z]\w+){1,8}$`),        // Title Case Headers
			regexp.MustCompile(`^\d+\.\d+\.?\s+[A-Z][A-Za-z\s]{3,60}$`), // Subsections
		},
		bulletPatterns: []*regexp.Regexp{
			regexp.MustCompile(`^[•·▪▫‣⁃]\s+`), // Unicode bullets
			regexp.MustCompile(`^[-*+]\s+`),    // ASCII bullets
		},
		numberPatterns: []*regexp.Regexp{
			regexp.MustCompile(`^\d+\.\s+`),   // Numbered lists
			regexp.MustCompile(`^\(\d+\)\s+`), // Parenthetical numbers
			regexp.MustCompile(`^[a-z]\)\s+`), // Lettered lists
		},
		whitespaceRegex: regexp.MustCompile(`\s+`),
		paragraphRegex:  regexp.MustCompile(`\n\s*\n`),
	}
}

func convertPDFToMarkdown(filename string) (string, error) {
	converter := NewPDFConverter(preserveLayout, includePages, extractHeaders)

	// Use docconv to extract text from PDF
	response, err := docconv.ConvertPath(filename)
	if err != nil {
		return "", fmt.Errorf("failed to convert PDF file '%s': %w", filename, err)
	}

	if strings.TrimSpace(response.Body) == "" {
		return "", fmt.Errorf("no readable text found in PDF file")
	}

	var result strings.Builder

	// Add document header
	baseName := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	result.WriteString(fmt.Sprintf("# %s\n\n", strings.Title(strings.ReplaceAll(baseName, "_", " "))))
	result.WriteString(fmt.Sprintf("*Converted from PDF: %s*\n", filename))
	result.WriteString(fmt.Sprintf("*Conversion date: %s*\n\n", time.Now().Format("2006-01-02 15:04:05")))

	// Add metadata if available
	if response.Meta != nil && len(response.Meta) > 0 {
		result.WriteString("## Document Metadata\n\n")
		for key, value := range response.Meta {
			if value != "" {
				result.WriteString(fmt.Sprintf("- **%s**: %s\n", strings.Title(key), value))
			}
		}
		result.WriteString("\n")
	}

	// Process the extracted text
	processedText := converter.processDocumentText(response.Body)
	result.WriteString(processedText)

	return result.String(), nil
}

// processPageText processes raw page text into structured Markdown
func (c *PDFConverter) processPageText(text string) string {
	// Clean up the text
	cleaned := c.cleanText(text)

	// Split into lines for processing
	lines := strings.Split(cleaned, "\n")
	var result strings.Builder

	inParagraph := false
	lastWasHeader := false

	for i, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines unless preserving layout
		if line == "" {
			if c.preserveLayout {
				result.WriteString("\n")
			} else if inParagraph {
				result.WriteString("\n\n")
				inParagraph = false
			}
			continue
		}

		// Check if this line is a header
		if c.extractHeaders && c.isHeader(line) {
			if inParagraph {
				result.WriteString("\n")
				inParagraph = false
			}

			headerLevel := c.determineHeaderLevel(line)
			cleanHeader := c.cleanHeaderText(line)
			result.WriteString(fmt.Sprintf("%s %s\n\n", strings.Repeat("#", headerLevel), cleanHeader))
			lastWasHeader = true
			continue
		}

		// Check if this line is a list item
		if c.isListItem(line) {
			if inParagraph {
				result.WriteString("\n")
				inParagraph = false
			}

			formattedItem := c.formatListItem(line)
			result.WriteString(formattedItem + "\n")
			continue
		}

		// Regular paragraph text
		if !inParagraph && !lastWasHeader {
			if result.Len() > 0 && !strings.HasSuffix(result.String(), "\n\n") {
				result.WriteString("\n")
			}
		}

		// Add the line with proper spacing
		if inParagraph && !c.preserveLayout {
			result.WriteString(" " + line)
		} else {
			result.WriteString(line)
			inParagraph = true
		}

		lastWasHeader = false

		// Look ahead to see if next line should be connected
		if i+1 < len(lines) {
			nextLine := strings.TrimSpace(lines[i+1])
			if nextLine != "" && !c.isHeader(nextLine) && !c.isListItem(nextLine) && !c.preserveLayout {
				// Continue paragraph on next iteration
				continue
			}
		}

		result.WriteString("\n")
		if inParagraph {
			result.WriteString("\n")
			inParagraph = false
		}
	}

	return result.String()
}

// cleanText performs basic text cleaning and normalization
func (c *PDFConverter) cleanText(text string) string {
	// Normalize whitespace but preserve line structure
	lines := strings.Split(text, "\n")
	var cleanedLines []string

	for _, line := range lines {
		// Normalize whitespace within each line
		cleaned := c.whitespaceRegex.ReplaceAllString(line, " ")
		cleaned = strings.TrimSpace(cleaned)

		// Remove common PDF artifacts
		cleaned = strings.ReplaceAll(cleaned, "\u00a0", " ") // Non-breaking space
		cleaned = strings.ReplaceAll(cleaned, "\u2010", "-") // Hyphen variants
		cleaned = strings.ReplaceAll(cleaned, "\u2011", "-")
		cleaned = strings.ReplaceAll(cleaned, "\u2012", "-")
		cleaned = strings.ReplaceAll(cleaned, "\u2013", "-")
		cleaned = strings.ReplaceAll(cleaned, "\u2014", "--")

		// Normalize quotes
		cleaned = strings.ReplaceAll(cleaned, "\u201c", "\"") // Left double quote
		cleaned = strings.ReplaceAll(cleaned, "\u201d", "\"") // Right double quote
		cleaned = strings.ReplaceAll(cleaned, "\u2018", "'")  // Left single quote
		cleaned = strings.ReplaceAll(cleaned, "\u2019", "'")  // Right single quote

		// Remove control characters but preserve meaningful content
		cleaned = regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]`).ReplaceAllString(cleaned, "")

		cleanedLines = append(cleanedLines, cleaned)
	}

	// Rejoin lines and normalize paragraph breaks
	result := strings.Join(cleanedLines, "\n")
	result = regexp.MustCompile(`\n{3,}`).ReplaceAllString(result, "\n\n")

	return strings.TrimSpace(result)
}

// isHeader determines if a line is likely a header
func (c *PDFConverter) isHeader(line string) bool {
	if len(line) < 3 || len(line) > 120 {
		return false
	}

	// Skip lines that are clearly not headers
	if strings.Contains(line, "  ") || // Multiple spaces suggest body text
		strings.HasSuffix(line, ".") || // Sentences usually end with periods
		strings.Contains(line, ",") { // Headers rarely contain commas

		// Exception: numbered sections can have periods
		if !regexp.MustCompile(`^\d+(\.\d+)*\.?\s+`).MatchString(line) {
			return false
		}
	}

	for _, pattern := range c.headerPatterns {
		if pattern.MatchString(line) {
			return true
		}
	}

	// Additional heuristics
	words := strings.Fields(line)
	if len(words) >= 2 && len(words) <= 15 {
		// Check if it looks like a title (most words capitalized)
		capitalizedCount := 0
		for _, word := range words {
			if len(word) > 0 && word[0] >= 'A' && word[0] <= 'Z' {
				capitalizedCount++
			}
		}
		// More lenient threshold for shorter lines
		threshold := 0.6
		if len(words) <= 4 {
			threshold = 0.5
		}
		if float64(capitalizedCount)/float64(len(words)) > threshold {
			return true
		}
	}

	return false
}

// determineHeaderLevel determines the appropriate Markdown header level
func (c *PDFConverter) determineHeaderLevel(line string) int {
	// Check for numbered sections
	if regexp.MustCompile(`^\d+\.\s+`).MatchString(line) {
		return 2 // ## for main sections
	}
	if regexp.MustCompile(`^\d+\.\d+\.\s+`).MatchString(line) {
		return 3 // ### for subsections
	}
	if regexp.MustCompile(`^\d+\.\d+\.\d+\.\s+`).MatchString(line) {
		return 4 // #### for sub-subsections
	}

	// All caps likely indicates a major section
	if strings.ToUpper(line) == line && len(strings.Fields(line)) <= 6 {
		return 2
	}

	// Default to level 3 for other headers
	return 3
}

// cleanHeaderText removes section numbers and cleans header text
func (c *PDFConverter) cleanHeaderText(line string) string {
	// Remove leading numbers and dots
	cleaned := regexp.MustCompile(`^\d+(\.\d+)*\.?\s*`).ReplaceAllString(line, "")

	// Normalize case for all-caps headers
	if strings.ToUpper(cleaned) == cleaned && len(cleaned) > 5 {
		cleaned = strings.Title(strings.ToLower(cleaned))
	}

	return strings.TrimSpace(cleaned)
}

// isListItem determines if a line is a list item
func (c *PDFConverter) isListItem(line string) bool {
	for _, pattern := range c.bulletPatterns {
		if pattern.MatchString(line) {
			return true
		}
	}
	for _, pattern := range c.numberPatterns {
		if pattern.MatchString(line) {
			return true
		}
	}
	return false
}

// formatListItem formats a line as a Markdown list item
func (c *PDFConverter) formatListItem(line string) string {
	// Convert various bullet types to Markdown
	for _, pattern := range c.bulletPatterns {
		if pattern.MatchString(line) {
			return pattern.ReplaceAllString(line, "- ")
		}
	}

	// Handle numbered lists
	if regexp.MustCompile(`^\d+\.\s+`).MatchString(line) {
		return line // Keep numbered lists as-is
	}

	// Convert other number patterns to regular bullets
	for _, pattern := range c.numberPatterns {
		if pattern.MatchString(line) {
			return pattern.ReplaceAllString(line, "- ")
		}
	}

	return "- " + line
}

// processDocumentText processes the entire document text extracted by docconv
func (c *PDFConverter) processDocumentText(text string) string {
	// Clean up the text first
	cleaned := c.cleanText(text)

	// Split into paragraphs for processing
	paragraphs := c.paragraphRegex.Split(cleaned, -1)
	var result strings.Builder

	for i, paragraph := range paragraphs {
		if strings.TrimSpace(paragraph) == "" {
			continue
		}

		// Process each paragraph
		processedParagraph := c.processParagraph(paragraph)
		result.WriteString(processedParagraph)

		// Add spacing between paragraphs
		if i < len(paragraphs)-1 && strings.TrimSpace(paragraphs[i+1]) != "" {
			result.WriteString("\n\n")
		}
	}

	return result.String()
}

// processParagraph processes a single paragraph of text
func (c *PDFConverter) processParagraph(paragraph string) string {
	lines := strings.Split(strings.TrimSpace(paragraph), "\n")
	var result strings.Builder

	inParagraph := false
	lastWasHeader := false

	for i, line := range lines {
		line = strings.TrimSpace(line)

		if line == "" {
			if c.preserveLayout {
				result.WriteString("\n")
			} else if inParagraph {
				result.WriteString("\n\n")
				inParagraph = false
			}
			continue
		}

		// Check if this line is a header
		if c.extractHeaders && c.isHeader(line) {
			if inParagraph {
				result.WriteString("\n")
				inParagraph = false
			}

			headerLevel := c.determineHeaderLevel(line)
			cleanHeader := c.cleanHeaderText(line)
			result.WriteString(fmt.Sprintf("%s %s\n\n", strings.Repeat("#", headerLevel), cleanHeader))
			lastWasHeader = true
			continue
		}

		// Check if this line is a list item
		if c.isListItem(line) {
			if inParagraph {
				result.WriteString("\n")
				inParagraph = false
			}

			formattedItem := c.formatListItem(line)
			result.WriteString(formattedItem + "\n")
			continue
		}

		// Regular paragraph text
		if !inParagraph && !lastWasHeader {
			if result.Len() > 0 && !strings.HasSuffix(result.String(), "\n\n") {
				result.WriteString("\n")
			}
		}

		// Add the line with proper spacing
		if inParagraph && !c.preserveLayout {
			result.WriteString(" " + line)
		} else {
			result.WriteString(line)
			inParagraph = true
		}

		lastWasHeader = false

		// Look ahead to see if next line should be connected
		if i+1 < len(lines) {
			nextLine := strings.TrimSpace(lines[i+1])
			if nextLine != "" && !c.isHeader(nextLine) && !c.isListItem(nextLine) && !c.preserveLayout {
				// Continue paragraph on next iteration
				continue
			}
		}

		result.WriteString("\n")
		if inParagraph {
			result.WriteString("\n")
			inParagraph = false
		}
	}

	return result.String()
}

// addBasicSpacing applies sophisticated word segmentation for proper tokenization
func (c *PDFConverter) addBasicSpacing(text string) string {
	// First apply basic pattern-based separation
	result := c.applyBasicPatterns(text)

	// Then apply dictionary-based word segmentation for remaining concatenated text
	result = c.segmentWords(result)

	// Clean up multiple spaces
	result = regexp.MustCompile(`\s+`).ReplaceAllString(result, " ")

	return strings.TrimSpace(result)
}

// AddBasicSpacing is a public wrapper for addBasicSpacing for external testing
func (c *PDFConverter) AddBasicSpacing(text string) string {
	return c.addBasicSpacing(text)
}

// applyBasicPatterns applies simple regex patterns for obvious word boundaries
func (c *PDFConverter) applyBasicPatterns(text string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`([a-z])([A-Z])`),           // lowercase followed by uppercase
		regexp.MustCompile(`([a-zA-Z])(\d)`),           // letter followed by digit
		regexp.MustCompile(`(\d)([a-zA-Z])`),           // digit followed by letter
		regexp.MustCompile(`([.!?])([A-Z])`),           // sentence end followed by capital
		regexp.MustCompile(`(https?://[^\s]+)([A-Z])`), // URLs followed by text
		regexp.MustCompile(`([a-z])(https?://)`),       // text followed by URLs
		regexp.MustCompile(`(doi\.org/[^\s]+)([A-Z])`), // DOIs followed by text
		regexp.MustCompile(`([0-9]/[0-9\-]+)([A-Z])`),  // DOI numbers followed by text
		regexp.MustCompile(`([a-z])(doi\.org)`),        // text followed by DOIs
		regexp.MustCompile(`([0-9])([A-Z])`),           // numbers followed by uppercase
	}

	result := text
	for _, pattern := range patterns {
		result = pattern.ReplaceAllString(result, "$1 $2")
	}

	return result
}

// segmentWords uses dictionary-based segmentation for concatenated words
func (c *PDFConverter) segmentWords(text string) string {
	words := strings.Fields(text)
	var result []string

	for _, word := range words {
		// Skip if word is already properly spaced or contains punctuation
		if len(word) < 6 || strings.ContainsAny(word, ".,!?;:()[]{}\"'") {
			result = append(result, word)
			continue
		}

		// Skip if word contains spaces (already segmented)
		if strings.Contains(word, " ") {
			result = append(result, word)
			continue
		}

		// Apply word segmentation to long concatenated strings
		segmented := c.dynamicWordSegmentation(strings.ToLower(word))
		if segmented != strings.ToLower(word) && !strings.Contains(segmented, "  ") {
			// Preserve original case as much as possible
			segmented = c.preserveCase(word, segmented)
			result = append(result, segmented)
		} else {
			result = append(result, word)
		}
	}

	return strings.Join(result, " ")
}

// dynamicWordSegmentation uses dynamic programming to find optimal word boundaries
func (c *PDFConverter) dynamicWordSegmentation(text string) string {
	if len(text) == 0 {
		return text
	}

	// Don't segment if already contains spaces
	if strings.Contains(text, " ") {
		return text
	}

	n := len(text)
	dp := make([]int, n+1)     // dp[i] = max score for segmenting text[0:i]
	parent := make([]int, n+1) // parent[i] = optimal split point before position i

	dictionary := c.getCommonWords()

	// Initialize: empty string has score 0
	dp[0] = 0
	for i := 1; i <= n; i++ {
		dp[i] = -1000 // Initialize with very low score
	}

	for i := 1; i <= n; i++ {
		// Try all possible last words ending at position i
		for j := 0; j < i; j++ {
			if dp[j] < 0 {
				continue // Skip invalid segmentations
			}

			substr := text[j:i]
			if c.isValidWord(substr, dictionary) {
				score := dp[j] + c.getWordScore(substr, dictionary)
				if score > dp[i] {
					dp[i] = score
					parent[i] = j
				}
			}
		}

		// If no valid segmentation found, allow single character with heavy penalty
		if dp[i] < 0 && i > 0 {
			dp[i] = dp[i-1] - 20 // Heavy penalty for single chars
			parent[i] = i - 1
		}
	}

	// Only return segmentation if it has positive score (better than original)
	if dp[n] <= 0 {
		return text
	}

	var words []string
	pos := n
	for pos > 0 {
		start := parent[pos]
		word := text[start:pos]
		if len(word) > 0 {
			words = append([]string{word}, words...)
		}
		pos = start
	}

	segmented := strings.Join(words, " ")

	// Don't return segmentation with too many single characters
	singleCharCount := 0
	for _, word := range words {
		if len(word) == 1 {
			singleCharCount++
		}
	}

	if singleCharCount > len(words)/3 {
		return text
	}

	return segmented
}

// preserveCase attempts to preserve the original case when applying segmentation
func (c *PDFConverter) preserveCase(original, segmented string) string {
	if len(original) == 0 || len(segmented) == 0 {
		return segmented
	}

	// If the original starts with uppercase, capitalize the first word
	result := segmented
	if len(original) > 0 && original[0] >= 'A' && original[0] <= 'Z' {
		if len(result) > 0 && result[0] >= 'a' && result[0] <= 'z' {
			result = strings.ToUpper(result[:1]) + result[1:]
		}
	}

	return result
}

// isValidWord checks if a word is valid using multiple criteria
func (c *PDFConverter) isValidWord(word string, dictionary map[string]bool) bool {
	if len(word) < 1 {
		return false
	}

	// Single characters are valid only if they're meaningful
	if len(word) == 1 {
		return strings.ContainsAny(word, "aioAIO")
	}

	// Check common dictionary words
	if dictionary[word] {
		return true
	}

	// Check scientific/academic terms
	if c.isScientificTerm(word) {
		return true
	}

	// Check if it's a reasonable English word pattern
	return c.isReasonableWord(word)
}

// getWordScore assigns scores to words for optimal segmentation
func (c *PDFConverter) getWordScore(word string, dictionary map[string]bool) int {
	if len(word) == 0 {
		return -100
	}

	// Single characters get low scores
	if len(word) == 1 {
		return -5
	}

	// Dictionary words get high scores
	if dictionary[word] {
		return len(word) * 10 // Longer dictionary words preferred
	}

	// Scientific terms get good scores
	if c.isScientificTerm(word) {
		return len(word) * 8
	}

	// Reasonable words get moderate scores
	if c.isReasonableWord(word) {
		return len(word) * 5
	}

	// Unknown words get penalty
	return -2
}

// isScientificTerm checks for scientific and academic terminology
func (c *PDFConverter) isScientificTerm(word string) bool {
	scientificTerms := map[string]bool{
		"supplementary": true, "information": true, "version": true, "material": true,
		"available": true, "correspondence": true, "requests": true, "addressed": true,
		"peer": true, "review": true, "nature": true, "communications": true,
		"anonymous": true, "reviewers": true, "contribution": true, "reports": true,
		"reprints": true, "permission": true, "publisher": true, "springer": true,
		"neutral": true, "jurisdictional": true, "claims": true, "published": true,
		"institutional": true, "affiliations": true, "research": true, "analysis": true,
		"methodology": true, "results": true, "discussion": true, "conclusion": true,
		"abstract": true, "introduction": true, "methods": true, "data": true,
		"statistical": true, "significant": true, "experiment": true, "study": true,
		"online": true, "contains": true, "thanks": true, "regard": true, "remains": true,
		"reviewer": true,
	}

	return scientificTerms[word]
}

// isReasonableWord uses heuristics to determine if a word is reasonable
func (c *PDFConverter) isReasonableWord(word string) bool {
	if len(word) < 2 || len(word) > 20 {
		return false
	}

	// Check for reasonable vowel/consonant distribution
	vowels := 0
	for _, r := range word {
		if strings.ContainsRune("aeiouAEIOU", r) {
			vowels++
		}
	}

	vowelRatio := float64(vowels) / float64(len(word))

	// More lenient vowel ratio for shorter words
	minRatio := 0.15
	maxRatio := 0.75
	if len(word) <= 4 {
		minRatio = 0.1
		maxRatio = 0.8
	}

	return vowelRatio >= minRatio && vowelRatio <= maxRatio
}

// getCommonWords returns a dictionary of common English words
func (c *PDFConverter) getCommonWords() map[string]bool {
	return map[string]bool{
		// Articles and determiners
		"a": true, "an": true, "the": true, "this": true, "that": true, "these": true, "those": true,

		// Prepositions
		"in": true, "on": true, "at": true, "by": true, "for": true, "with": true, "to": true,
		"of": true, "from": true, "up": true, "down": true, "over": true, "under": true,

		// Conjunctions
		"and": true, "or": true, "but": true, "nor": true, "yet": true, "so": true,

		// Common verbs
		"is": true, "are": true, "was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true, "did": true, "will": true,
		"would": true, "could": true, "should": true, "may": true, "might": true, "can": true,
		"get": true, "got": true, "go": true, "went": true, "come": true, "came": true,
		"see": true, "saw": true, "know": true, "knew": true, "think": true, "thought": true,

		// Common nouns
		"time": true, "person": true, "year": true, "way": true, "day": true, "thing": true,
		"man": true, "woman": true, "child": true, "world": true, "life": true, "hand": true,
		"part": true, "place": true, "case": true, "point": true, "group": true, "problem": true,
		"fact": true, "work": true, "week": true, "month": true, "end": true, "number": true,

		// Common adjectives
		"good": true, "new": true, "last": true, "long": true, "great": true,
		"little": true, "own": true, "other": true, "old": true, "right": true, "big": true,
		"high": true, "different": true, "small": true, "large": true, "next": true, "early": true,

		// Academic/Scientific common words
		"study": true, "research": true, "analysis": true, "data": true, "method": true, "result": true,
		"conclusion": true, "figure": true, "table": true, "paper": true, "article": true, "journal": true,
		"university": true, "college": true, "department": true, "professor": true, "student": true,
		"experiment": true, "test": true, "sample": true, "control": true, "statistical": true,
		"significant": true, "hypothesis": true, "theory": true, "model": true, "framework": true,
		"approach": true, "technique": true, "procedure": true, "protocol": true, "methodology": true,

		// Numbers and quantifiers
		"one": true, "two": true, "three": true, "four": true, "five": true, "six": true,
		"seven": true, "eight": true, "nine": true, "ten": true, "first": true, "second": true,
		"third": true, "many": true, "much": true, "more": true, "most": true, "few": true,
		"several": true, "some": true, "any": true, "all": true, "both": true, "each": true,

		// Common academic phrases components
		"online": true, "version": true, "contains": true, "material": true, "available": true,
		"information": true, "supplementary": true, "correspondence": true, "requests": true,
		"addressed": true, "review": true, "peer": true, "thanks": true,
		"anonymous": true, "reviewers": true, "their": true, "contribution": true, "reports": true,
		"reprints": true, "permission": true, "publisher": true, "note": true, "remains": true,
		"neutral": true, "regard": true, "claims": true, "published": true, "maps": true,
		"institutional": true, "affiliations": true, "nature": true, "communications": true,
		"reviewer": true,
	}
}
