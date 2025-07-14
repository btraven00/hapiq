package cmd

import (
	"fmt"

	"github.com/btraven00/hapiq/internal/checker"
	"github.com/spf13/cobra"
)

var (
	downloadFlag bool
	timeoutFlag  int
)

// checkCmd represents the check command
var checkCmd = &cobra.Command{
	Use:   "check <url-or-identifier>",
	Short: "Check and validate a dataset URL or identifier",
	Long: `Check validates URLs and identifiers (e.g. Zenodo, Figshare),
attempts dataset download (with fallback for archives), inspects structure,
and estimates the likelihood of a valid dataset.

Examples:
  hapiq check https://zenodo.org/record/123456
  hapiq check 10.5281/zenodo.123456
  hapiq check https://figshare.com/articles/dataset/example/123456`,
	Args: cobra.ExactArgs(1),
	RunE: runCheck,
}

func runCheck(cmd *cobra.Command, args []string) error {
	target := args[0]

	if verbose {
		fmt.Printf("Checking: %s\n", target)
		fmt.Printf("Download enabled: %t\n", downloadFlag)
		fmt.Printf("Timeout: %ds\n", timeoutFlag)
		fmt.Printf("Output format: %s\n", output)
	}

	// Create checker configuration
	config := checker.Config{
		Verbose:        verbose,
		Download:       downloadFlag,
		TimeoutSeconds: timeoutFlag,
		OutputFormat:   output,
	}

	// Initialize checker
	c := checker.New(config)

	// Perform the check
	result, err := c.Check(target)
	if err != nil {
		return fmt.Errorf("failed to check %s: %w", target, err)
	}

	// Output results
	if err := c.OutputResult(result); err != nil {
		return fmt.Errorf("failed to output result: %w", err)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(checkCmd)

	// Local flags for the check command
	checkCmd.Flags().BoolVarP(&downloadFlag, "download", "d", false, "attempt to download the dataset")
	checkCmd.Flags().IntVarP(&timeoutFlag, "timeout", "t", 30, "timeout in seconds for HTTP requests")
}
