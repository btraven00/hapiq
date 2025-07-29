package cmd

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/btraven00/hapiq/internal/checker"
	"github.com/spf13/cobra"
)

var (
	downloadFlag bool
	timeoutFlag  int
	inputFile    string
)

// checkCmd represents the check command
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

func runCheck(cmd *cobra.Command, args []string) error {
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
	} else {
		return processSingleTarget(c, args[0])
	}
}

func processSingleTarget(c *checker.Checker, target string) error {
	if !quiet {
		fmt.Printf("Checking: %s\n", target)
		fmt.Printf("Download enabled: %t\n", downloadFlag)
		fmt.Printf("Timeout: %ds\n", timeoutFlag)
		fmt.Printf("Output format: %s\n", output)
	}

	// Clean and normalize the target
	cleanTarget := cleanupIdentifier(target)
	if !quiet && cleanTarget != target {
		fmt.Printf("Normalized: %s -> %s\n", target, cleanTarget)
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
		fmt.Printf("Processing batch file: %s\n", filename)
		fmt.Printf("Download enabled: %t\n", downloadFlag)
		fmt.Printf("Timeout: %ds\n", timeoutFlag)
		fmt.Printf("Output format: %s\n", output)
	}

	// Open input file
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open input file: %w", err)
	}
	defer file.Close()

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
				fmt.Printf("Line %d: Skipping empty after cleanup: %s\n", lineNumber, line)
			}
			continue
		}

		processedCount++
		if !quiet {
			fmt.Printf("\n[%d/%d] Processing: %s", processedCount, lineNumber, cleanLine)
			if cleanLine != line {
				fmt.Printf(" (normalized from: %s)", line)
			}
			fmt.Println()
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
		fmt.Printf("\nBatch processing completed: %d processed, %d errors\n", processedCount, errorCount)
	}

	return nil
}

// cleanupIdentifier removes unwanted characters and normalizes DOIs/URLs
func cleanupIdentifier(identifier string) string {
	if identifier == "" {
		return ""
	}

	// Remove leading/trailing whitespace
	cleaned := strings.TrimSpace(identifier)

	// Enhanced URL boundary detection patterns
	var cleanupPatterns = []*regexp.Regexp{
		regexp.MustCompile(`[\(\)\[\]\{\}].*$`),   // Remove everything from first bracket onwards
		regexp.MustCompile(`[,;].*$`),             // Remove everything from first comma/semicolon onwards
		regexp.MustCompile(`\s+`),                 // Normalize whitespace
		regexp.MustCompile(`\.[A-Z][a-z].*$`),     // Remove text starting with period followed by capitalized word
		regexp.MustCompile(`[0-9]+[A-Z][a-z].*$`), // Remove text starting with number followed by capitalized word
		regexp.MustCompile(`[a-z]+[A-Z][a-z].*$`), // Remove concatenated text starting with lowercase-uppercase pattern
		regexp.MustCompile(`\.\d+.*$`),            // Remove figshare version numbers (e.g., .2.3MouselungdatasetThis...)
	}

	// Apply cleanup patterns
	for _, pattern := range cleanupPatterns {
		if pattern.MatchString(cleaned) {
			if strings.Contains(pattern.String(), `.*$`) {
				// For patterns that remove everything after a character
				cleaned = pattern.ReplaceAllString(cleaned, "")
			} else {
				// For patterns that normalize (like whitespace)
				cleaned = pattern.ReplaceAllString(cleaned, " ")
			}
		}
	}

	// Final cleanup
	cleaned = strings.TrimSpace(cleaned)

	// Remove trailing punctuation that's not part of the identifier
	cleaned = strings.TrimRight(cleaned, ".,;:!?()[]{})")

	return cleaned
}

func init() {
	rootCmd.AddCommand(checkCmd)

	// Local flags for the check command
	checkCmd.Flags().BoolVarP(&downloadFlag, "download", "d", false, "attempt to download the dataset")
	checkCmd.Flags().IntVarP(&timeoutFlag, "timeout", "t", 30, "timeout in seconds for HTTP requests")
	checkCmd.Flags().StringVarP(&inputFile, "input", "i", "", "input file with DOIs/links (one per line)")
}
