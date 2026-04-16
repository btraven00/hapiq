package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/btraven00/hapiq/pkg/downloaders"
	"github.com/btraven00/hapiq/pkg/downloaders/geo"
)

var (
	searchLimit    int
	searchOrganism string
	searchType     string
)

var searchCmd = &cobra.Command{
	Use:   "search <source> <query>",
	Short: "Search for datasets in scientific repositories",
	Long: `Search for datasets using a repository's native query API.

Results are printed one accession per line when --quiet is set, making
them easy to pipe into 'hapiq download':

  hapiq search geo "ATAC-seq human liver" -q | \
    xargs -I{} hapiq download geo {} --out ./data --dry-run

Supported sources:
  geo  - NCBI Gene Expression Omnibus (uses eutils esearch/esummary)

Examples:
  hapiq search geo "ATAC-seq human liver" --limit 20
  hapiq search geo "single-cell RNA-seq pancreas" --organism "Mus musculus"
  hapiq search geo "ChIP-seq H3K27ac" --type GSE --limit 5 --output json
  hapiq search geo "bulk RNA-seq" -q | head -5 | xargs -I{} hapiq download geo {} --out ./data`,
	Args: cobra.ExactArgs(2),
	RunE: runSearch,
}

func runSearch(_ *cobra.Command, args []string) error {
	sourceType := args[0]
	query := args[1]

	if strings.ToLower(sourceType) != "geo" {
		return fmt.Errorf("search is currently only supported for 'geo'; got %q", sourceType)
	}

	apiKey := os.Getenv("NCBI_API_KEY")
	geoOpts := []geo.Option{
		geo.WithVerbose(false), // search output managed here, not by downloader
		geo.WithTimeout(time.Duration(defaultCheckTimeoutSec) * time.Second),
	}
	if apiKey != "" {
		geoOpts = append(geoOpts, geo.WithAPIKey(apiKey))
	}

	d := geo.NewGEODownloader(geoOpts...)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	opts := downloaders.SearchOptions{
		Organism:  searchOrganism,
		EntryType: searchType,
		Limit:     searchLimit,
	}

	if !quiet {
		_, _ = fmt.Fprintf(os.Stderr, "Searching GEO for: %s\n", query)
		if searchOrganism != "" {
			_, _ = fmt.Fprintf(os.Stderr, "  Organism filter: %s\n", searchOrganism)
		}
		if searchType != "" {
			_, _ = fmt.Fprintf(os.Stderr, "  Type filter: %s\n", searchType)
		}
	}

	results, err := d.Search(ctx, query, opts)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	if len(results) == 0 {
		_, _ = fmt.Fprintln(os.Stderr, "No results found.")
		return nil
	}

	switch output {
	case outputFormatJSON:
		return printSearchJSON(results)
	default:
		if quiet {
			return printSearchQuiet(results)
		}
		return printSearchTable(results)
	}
}

// printSearchQuiet prints only the accession IDs, one per line — ideal for piping.
func printSearchQuiet(results []downloaders.SearchResult) error {
	for _, r := range results {
		fmt.Println(r.Accession)
	}
	return nil
}

// printSearchTable prints a formatted table to stderr and accessions to stdout.
func printSearchTable(results []downloaders.SearchResult) error {
	w := tabwriter.NewWriter(os.Stderr, 0, 0, tabWriterPadding, ' ', 0)
	_, _ = fmt.Fprintln(w, "ACCESSION\tTITLE\tORGANISM\tTYPE\tSAMPLES\tDATE")
	_, _ = fmt.Fprintln(w, "---------\t-----\t--------\t----\t-------\t----")

	for _, r := range results {
		title := r.Title
		if len(title) > maxDescriptionChars {
			title = title[:maxDescriptionChars-truncationSuffix] + "..."
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n",
			r.Accession, title, r.Organism, r.EntryType, r.SampleCount, r.Date)
	}

	_ = w.Flush()

	// Also print accessions to stdout for easy piping even in human mode.
	_, _ = fmt.Fprintf(os.Stderr, "\nAccessions (stdout):\n")
	for _, r := range results {
		fmt.Println(r.Accession)
	}

	return nil
}

// printSearchJSON prints results as a JSON array to stdout.
func printSearchJSON(results []downloaders.SearchResult) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}

func init() {
	rootCmd.AddCommand(searchCmd)

	searchCmd.Flags().IntVar(&searchLimit, "limit", 10, "maximum number of results to return")
	searchCmd.Flags().StringVar(&searchOrganism, "organism", "", "filter by organism (e.g. 'Homo sapiens')")
	searchCmd.Flags().StringVar(&searchType, "type", "GSE", "filter by entry type: GSE, GSM, GPL, GDS (default: GSE)")
}
