// Package cmd provides command-line interface commands for the hapiq tool.
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/btraven00/hapiq/internal/checker"
)

var (
	downloadFlag bool
	timeoutFlag  int
	inputFile    string
)

// checkCmd represents the check command.
var checkCmd = &cobra.Command{
	Use:   "check [<url-or-identifier>]",
	Short: "Check and validate a dataset URL or identifier",
	Long: `Check validates URLs and identifiers (e.g. Zenodo, Figshare),
attempts dataset download (with fallback for archives), inspects structure,
and estimates the likelihood of a valid dataset.

Examples:
  hapiq check https://zenodo.org/record/123456
  hapiq check 10.5281/zenodo.123456
  hapiq check https://figshare.com/articles/dataset/example/123456
  hapiq check -i links.txt --download`,
	Args: cobra.RangeArgs(0, 1),
	RunE: runCheck,
}

func runCheck(_ *cobra.Command, args []string) error {
	// Validate arguments
	if inputFile == "" && len(args) == 0 {
		return fmt.Errorf("either provide a URL/identifier as argument or use -i flag with input file")
	}

	if inputFile != "" && len(args) > 0 {
		return fmt.Errorf("cannot use both input file (-i) and command line argument")
	}

	// Create checker configuration
	config := checker.Config{
		Verbose:        !quiet,
		Download:       downloadFlag,
		TimeoutSeconds: timeoutFlag,
		OutputFormat:   output,
	}

	// Initialize checker
	c := checker.New(config)

	// Process single target or batch file
	if inputFile != "" {
		return processBatchFile(c, inputFile)
	}

	return processSingleTarget(c, args[0])
}

func processSingleTarget(c *checker.Checker, target string) error {
	if !quiet {
		_, _ = fmt.Fprintf(os.Stderr, "Checking: %s\n", target)
		_, _ = fmt.Fprintf(os.Stderr, "Download enabled: %t\n", downloadFlag)
		_, _ = fmt.Fprintf(os.Stderr, "Timeout: %ds\n", timeoutFlag)
		_, _ = fmt.Fprintf(os.Stderr, "Output format: %s\n", output)
	}

	// Clean and normalize the target
	cleanTarget := cleanupIdentifier(target)
	if !quiet && cleanTarget != target {
		_, _ = fmt.Fprintf(os.Stderr, "Normalized: %s -> %s\n", target, cleanTarget)
	}

	// Perform the check
	result, err := c.Check(cleanTarget)
	if err != nil {
		return fmt.Errorf("failed to check %s: %w", cleanTarget, err)
	}

	// Output results
	if err := c.OutputResult(result); err != nil {
		return fmt.Errorf("failed to output result: %w", err)
	}

	return nil
}

func processBatchFile(c *checker.Checker, filename string) error {
	if !quiet {
		_, _ = fmt.Fprintf(os.Stderr, "Processing batch file: %s\n", filename)
		_, _ = fmt.Fprintf(os.Stderr, "Download enabled: %t\n", downloadFlag)
		_, _ = fmt.Fprintf(os.Stderr, "Timeout: %ds\n", timeoutFlag)
		_, _ = fmt.Fprintf(os.Stderr, "Output format: %s\n", output)
	}

	// Open input file
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open input file: %w", err)
	}

	defer func() {
		if err := file.Close(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error closing file: %v\n", err)
		}
	}()

	// Read and process each line
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	processedCount := 0
	errorCount := 0

	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Clean and normalize the identifier
		cleanLine := cleanupIdentifier(line)
		if cleanLine == "" {
			if !quiet {
				_, _ = fmt.Fprintf(os.Stderr, "Line %d: Skipping empty after cleanup: %s\n", lineNumber, line)
			}

			continue
		}

		processedCount++
		if !quiet {
			_, _ = fmt.Fprintf(os.Stderr, "\n[%d/%d] Processing: %s", processedCount, lineNumber, cleanLine)

			if cleanLine != line {
				_, _ = fmt.Fprintf(os.Stderr, " (normalized from: %s)", line)
			}

			_, _ = fmt.Fprintln(os.Stderr)
		}

		// Perform the check
		result, err := c.Check(cleanLine)
		if err != nil {
			errorCount++

			fmt.Fprintf(os.Stderr, "Error checking %s: %v\n", cleanLine, err)

			continue
		}

		// Output results
		if err := c.OutputResult(result); err != nil {
			errorCount++

			fmt.Fprintf(os.Stderr, "Error outputting result for %s: %v\n", cleanLine, err)

			continue
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	if !quiet {
		_, _ = fmt.Fprintf(os.Stderr, "\nBatch processing completed: %d processed, %d errors\n", processedCount, errorCount)
	}

	return nil
}

// cleanupIdentifier removes unwanted characters and normalizes DOIs/URLs.
func cleanupIdentifier(identifier string) string {
	if identifier == "" {
		return ""
	}

	// Remove leading/trailing whitespace
	cleaned := strings.TrimSpace(identifier)
	if cleaned == "" {
		return ""
	}

	// Handle edge case: brackets at beginning (e.g., "[Dataset] 10.5281/zenodo.123456")
	// These should return empty string as they're not valid identifiers
	if strings.HasPrefix(cleaned, "[") || strings.HasPrefix(cleaned, "(") || strings.HasPrefix(cleaned, ",") {
		return ""
	}

	// Check for edge case: only brackets/punctuation with no valid identifier
	if !containsValidIdentifier(cleaned) {
		return ""
	}

	// Normalize internal whitespace, but preserve single spaces in DOIs
	if strings.HasPrefix(cleaned, "10.") {
		// For DOIs, be more careful with whitespace normalization
		// Convert multiple spaces to single space, but preserve existing single spaces
		cleaned = regexp.MustCompile(`\s{2,}`).ReplaceAllString(cleaned, " ")
	} else {
		cleaned = regexp.MustCompile(`\s+`).ReplaceAllString(cleaned, " ")
	}

	// Remove content in brackets/parentheses that comes after the main identifier
	// This preserves brackets that are part of URLs (like query parameters)
	cleaned = regexp.MustCompile(`\s+[\(\[\{].*$`).ReplaceAllString(cleaned, "")

	// Remove content after comma or semicolon (citation info, access dates, etc.)
	cleaned = regexp.MustCompile(`\s*[,;].*$`).ReplaceAllString(cleaned, "")

	// Special handling for URLs with brackets in path - truncate at brackets
	if strings.HasPrefix(cleaned, "http") && strings.Contains(cleaned, "[") && !strings.Contains(cleaned, " ") {
		bracketIdx := strings.Index(cleaned, "[")
		if bracketIdx > 0 {
			cleaned = cleaned[:bracketIdx]
		}
	}

	// Handle special characters - preserve copyright and similar symbols
	cleaned = handleSpecialCharacters(cleaned)

	// Only remove basic trailing punctuation if no special characters present
	if !regexp.MustCompile(`[©®™]`).MatchString(cleaned) {
		cleaned = strings.TrimRight(cleaned, ".,;:!?")
	}

	// Remove unmatched trailing brackets
	cleaned = strings.TrimRight(cleaned, "()[]{}")

	cleaned = strings.TrimSpace(cleaned)

	// Final check: if we ended up with something that doesn't look like an identifier, return empty
	if !containsValidIdentifier(cleaned) {
		return ""
	}

	return cleaned
}

// containsValidIdentifier checks if a string contains a valid identifier.
func containsValidIdentifier(s string) bool {
	// Check for URLs
	if strings.Contains(s, "http://") || strings.Contains(s, "https://") {
		return true
	}
	// Check for DOI patterns anywhere in string
	if regexp.MustCompile(`10\.\d+/`).MatchString(s) {
		return true
	}
	// Check for other identifier patterns
	if regexp.MustCompile(`(PRJNA|PRJEB|GSE|SRA|ERP|DRP)`).MatchString(s) {
		return true
	}

	return false
}

// isValidIdentifierStart checks if a string starts like a valid identifier.
func isValidIdentifierStart(s string) bool {
	// Check for URLs
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return true
	}
	// Check for DOI patterns
	if regexp.MustCompile(`^10\.\d+/`).MatchString(s) {
		return true
	}
	// Check for other identifier patterns
	if regexp.MustCompile(`^(PRJNA|PRJEB|GSE|SRA|ERP|DRP)`).MatchString(s) {
		return true
	}

	return false
}

// extractURL extracts a clean URL from text that may contain trailing description.
func extractURL(text string) string {
	// Find the URL part before any obvious descriptive text
	// Look for patterns that indicate the end of a URL
	// Special handling for URLs with brackets in the path
	// If we have brackets without spaces, they're likely part of the path and should truncate the URL
	if strings.Contains(text, "[") {
		bracketIdx := strings.Index(text, "[")
		spaceIdx := strings.Index(text, " ")

		// If brackets appear in URL path (no space before them), truncate at brackets
		if spaceIdx == -1 || bracketIdx < spaceIdx {
			return strings.TrimSpace(text[:bracketIdx])
		}
	}

	// Remove text after patterns that indicate trailing description
	patterns := []string{
		` \(`,       // space + opening parenthesis
		` \[`,       // space + opening bracket
		` \{`,       // space + opening brace
		` accessed`, // access date info
		` version`,  // version info
		` dataset`,  // dataset description
		` data`,     // data description
	}

	for _, pattern := range patterns {
		if idx := strings.Index(text, pattern); idx != -1 {
			text = text[:idx]
		}
	}

	return strings.TrimSpace(text)
}

// extractDOI extracts a clean DOI from text that may contain trailing description.
func extractDOI(text string) string {
	// For DOIs with internal spaces, handle them specially
	if strings.HasPrefix(text, "10.") && strings.Contains(text, " ") {
		// Check if this looks like "10.5281/ zenodo.123456" pattern
		parts := strings.Fields(text)
		if len(parts) >= 2 && strings.HasSuffix(parts[0], "/") {
			// This is likely a DOI split by space - preserve it
			return text
		}
	}

	// For regular DOIs, be more conservative and look for the DOI pattern
	doiPattern := regexp.MustCompile(`^(10\.\d+/[^\s\(\[\{,;]+)`)
	if match := doiPattern.FindString(text); match != "" {
		return match
	}

	// For other identifiers, take the first word if it looks like an identifier
	parts := strings.Fields(text)
	if len(parts) > 0 && isValidIdentifierStart(parts[0]) {
		// Check if first part is complete identifier or if we need more parts
		firstPart := parts[0]

		// For patterns like "PRJNA123456", one part is enough
		if regexp.MustCompile(`^(PRJNA|PRJEB|GSE|SRA|ERP|DRP)\d+`).MatchString(firstPart) {
			return firstPart
		}

		// For other cases, might need to be more careful
		return firstPart
	}

	return text
}

// handleSpecialCharacters processes text with spaces, handling special characters appropriately.
func handleSpecialCharacters(cleaned string) string {
	if !strings.Contains(cleaned, " ") {
		return cleaned
	}

	parts := strings.Fields(cleaned)
	if len(parts) < 2 {
		return cleaned
	}

	// Check if any part contains special characters that should be preserved
	for _, part := range parts {
		if regexp.MustCompile(`[©®™]`).MatchString(part) {
			return cleaned // Keep text as-is if special characters found
		}
	}

	// Normal processing for other cases
	if isValidIdentifierStart(parts[0]) {
		if strings.HasPrefix(parts[0], "http") {
			return extractURL(cleaned)
		}

		return extractDOI(cleaned)
	}

	return cleaned
}

func init() {
	rootCmd.AddCommand(checkCmd)

	// Local flags for the check command
	checkCmd.Flags().BoolVarP(&downloadFlag, "download", "d", false, "attempt to download the dataset")
	checkCmd.Flags().IntVarP(&timeoutFlag, "timeout", "t", 30, "timeout in seconds for HTTP requests")
	checkCmd.Flags().StringVarP(&inputFile, "input", "i", "", "input file with DOIs/links (one per line)")
}
