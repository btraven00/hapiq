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
	"github.com/btraven00/hapiq/pkg/downloaders/common"
	"github.com/btraven00/hapiq/pkg/downloaders/experimenthub"
	"github.com/btraven00/hapiq/pkg/downloaders/geo"
	"github.com/btraven00/hapiq/pkg/downloaders/scperturb"
	"github.com/btraven00/hapiq/pkg/downloaders/vcp"
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
  vcp  - CZI Virtual Cell Platform (VCP); set VCP_TOKEN for private datasets
  experimenthub - Bioconductor ExperimentHub (uses cached metadata sqlite)

Examples:
  hapiq search geo "ATAC-seq human liver" --limit 20
  hapiq search geo "scRNA-seq pancreas" --organism "Mus musculus"
  hapiq search vcp "Perturb-Seq" --limit 10
  hapiq search vcp "Perturb-Seq" --assay "Perturb-Seq" --organism "Homo sapiens"
  hapiq search vcp "Perturb-Seq" -q | xargs -I{} hapiq download vcp {} --out ./data`,
	Args: cobra.ExactArgs(2),
	RunE: runSearch,
}

func runSearch(_ *cobra.Command, args []string) error {
	sourceType := args[0]
	query := args[1]

	var (
		d   downloaders.Searcher
		src = strings.ToLower(sourceType)
	)

	switch src {
	case "geo":
		// Default GEO searches to series (GSE) when no type is specified.
		if searchType == "" {
			searchType = "GSE"
		}
		apiKey := os.Getenv("NCBI_API_KEY")
		geoOpts := []geo.Option{
			geo.WithVerbose(false),
			geo.WithTimeout(time.Duration(defaultCheckTimeoutSec) * time.Second),
		}
		if apiKey != "" {
			geoOpts = append(geoOpts, geo.WithAPIKey(apiKey))
		}
		d = geo.NewGEODownloader(geoOpts...)

	case "vcp":
		cziOpts := []vcp.Option{
			vcp.WithVerbose(false),
			vcp.WithTimeout(time.Duration(defaultCheckTimeoutSec) * time.Second),
		}
		if token := os.Getenv("VCP_TOKEN"); token != "" {
			cziOpts = append(cziOpts, vcp.WithToken(token))
		}
		d = vcp.NewVCPDownloader(cziOpts...)

	case "scperturb":
		d = scperturb.NewScPerturbDownloader(
			scperturb.WithVerbose(false),
			scperturb.WithTimeout(time.Duration(defaultCheckTimeoutSec) * time.Second),
		)

	case "experimenthub", "eh":
		d = experimenthub.NewExperimentHubDownloader(
			experimenthub.WithVerbose(!quiet),
			experimenthub.WithTimeout(time.Duration(defaultCheckTimeoutSec) * time.Second),
		)

	default:
		return fmt.Errorf("search is supported for 'geo', 'vcp', 'scperturb', 'experimenthub'; got %q", sourceType)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	opts := downloaders.SearchOptions{
		Organism:  searchOrganism,
		EntryType: searchType,
		Limit:     searchLimit,
	}

	if !quiet {
		_, _ = fmt.Fprintf(os.Stderr, "Searching %s for: %s\n", strings.ToUpper(src), query)
		if searchOrganism != "" {
			_, _ = fmt.Fprintf(os.Stderr, "  Organism filter: %s\n", searchOrganism)
		}
		if searchType != "" {
			_, _ = fmt.Fprintf(os.Stderr, "  Type/assay filter: %s\n", searchType)
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
		return printSearchTable(results, src)
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
// Column layout differs by source:
//   - geo: ACCESSION  TITLE  ORGANISM  TYPE  SAMPLES  DATE
//   - vcp: ACCESSION  TITLE  ORGANISM  ASSAY  SIZE
func printSearchTable(results []downloaders.SearchResult, src string) error {
	w := tabwriter.NewWriter(os.Stderr, 0, 0, tabWriterPadding, ' ', 0)

	if src == "vcp" || src == "scperturb" {
		_, _ = fmt.Fprintln(w, "ACCESSION\tTITLE\tORGANISM\tASSAY\tSIZE")
		_, _ = fmt.Fprintln(w, "---------\t-----\t--------\t-----\t----")
		for _, r := range results {
			title := r.Title
			if len(title) > maxDescriptionChars {
				title = title[:maxDescriptionChars-truncationSuffix] + "..."
			}
			size := "-"
			if r.FileSize > 0 {
				size = common.FormatBytes(r.FileSize)
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				r.Accession, title, r.Organism, r.EntryType, size)
		}
	} else {
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
	}

	_ = w.Flush()
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
	searchCmd.Flags().StringVar(&searchType, "type", "",
		"GEO: entry type to filter (GSE/GSM/GPL/GDS, default GSE); CZI: assay filter (e.g. 'Perturb-Seq')")
}
