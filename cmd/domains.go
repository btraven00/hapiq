// Package cmd provides command-line interface commands for the hapiq tool.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/btraven00/hapiq/pkg/validators/domains"
)

var (
	listDomains  bool
	showPatterns bool
	domainFilter string
	listJSON     bool
)

// listCmd represents the list command.
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List and explore domain-specific validators",
	Long: `The list command allows you to explore the available domain-specific
validators that can recognize and validate identifiers from scientific databases
and repositories.

Each domain represents a scientific field (e.g., bioinformatics, chemistry)
and contains validators for that field's specific databases and formats.

Examples:
  hapiq list                              # List all available validators
  hapiq list --domain bio                 # Show validators in bioinformatics domain
  hapiq list --patterns                   # Show recognition patterns
  hapiq list --output json                # Output as JSON`,
	RunE: runList,
}

func runList(_ *cobra.Command, _ []string) error {
	if listJSON {
		return outputDomainsJSON()
	}

	if listDomains {
		return listAvailableDomains()
	}

	if domainFilter != "" {
		return showDomainValidators(domainFilter)
	}

	// Default: show all validators
	return showAllValidators()
}

func outputDomainsJSON() error {
	info := domains.DefaultRegistry.ListValidators()

	output := struct {
		Validators []domains.ValidatorInfo `json:"validators"`
		Domains    []string                `json:"domains"`
		Count      int                     `json:"count"`
	}{
		Validators: info,
		Domains:    domains.DefaultRegistry.ListDomains(),
		Count:      len(info),
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	return encoder.Encode(output)
}

func listAvailableDomains() error {
	domainList := domains.DefaultRegistry.ListDomains()
	if len(domainList) == 0 {
		_, _ = fmt.Fprintln(os.Stderr, "No domain validators are currently registered.")
		return nil
	}

	sort.Strings(domainList)

	_, _ = fmt.Fprintf(os.Stderr, "Available Domains (%d):\n\n", len(domainList))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "DOMAIN\tVALIDATORS\tDESCRIPTION")
	_, _ = fmt.Fprintln(w, "------\t----------\t-----------")

	for _, domain := range domainList {
		validators := domains.DefaultRegistry.GetByDomain(domain)
		description := getDomainDescription(domain)

		_, _ = fmt.Fprintf(w, "%s\t%d\t%s\n", domain, len(validators), description)
	}

	return w.Flush()
}

func showDomainValidators(domain string) error {
	validators := domains.DefaultRegistry.GetByDomain(domain)
	if len(validators) == 0 {
		return fmt.Errorf("no validators found for domain: %s", domain)
	}

	_, _ = fmt.Fprintf(os.Stderr, "Validators in domain '%s' (%d):\n\n", domain, len(validators))

	for _, validator := range validators {
		_, _ = fmt.Fprintf(os.Stderr, "📊 %s (priority: %d)\n", validator.Name(), validator.Priority())
		_, _ = fmt.Fprintf(os.Stderr, "   %s\n\n", validator.Description())

		if showPatterns {
			patterns := validator.GetPatterns()
			if len(patterns) > 0 {
				_, _ = fmt.Fprintf(os.Stderr, "   Patterns:\n")

				for _, pattern := range patterns {
					_, _ = fmt.Fprintf(os.Stderr, "   • %s: %s\n", pattern.Type, pattern.Pattern)
					_, _ = fmt.Fprintf(os.Stderr, "     %s\n", pattern.Description)

					if len(pattern.Examples) > 0 {
						_, _ = fmt.Fprintf(os.Stderr, "     Examples: %s\n", strings.Join(pattern.Examples, ", "))
					}

					_, _ = fmt.Fprintln(os.Stderr)
				}
			}
		}
	}

	return nil
}

func showAllValidators() error {
	info := domains.DefaultRegistry.ListValidators()
	if len(info) == 0 {
		_, _ = fmt.Fprintln(os.Stderr, "No domain validators are currently registered.")
		_, _ = fmt.Fprintln(os.Stderr, "\nTo see how to add validators, run: hapiq domains --help")

		return nil
	}

	_, _ = fmt.Fprintf(os.Stderr, "Available Domain Validators (%d):\n\n", len(info))

	// Group by domain
	domainGroups := make(map[string][]domains.ValidatorInfo)

	for _, validator := range info {
		domain := validator.Domain
		if domainGroups[domain] == nil {
			domainGroups[domain] = make([]domains.ValidatorInfo, 0)
		}

		domainGroups[domain] = append(domainGroups[domain], validator)
	}

	// Sort domains
	domainList := make([]string, 0, len(domainGroups))
	for domain := range domainGroups {
		domainList = append(domainList, domain)
	}

	sort.Strings(domainList)

	for _, domain := range domainList {
		validators := domainGroups[domain]

		_, _ = fmt.Fprintf(os.Stderr, "🔬 %s\n", strings.ToUpper(domain))
		_, _ = fmt.Fprintf(os.Stderr, "   %s\n\n", getDomainDescription(domain))

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "   NAME\tPRIORITY\tDESCRIPTION")
		_, _ = fmt.Fprintln(w, "   ----\t--------\t-----------")

		for _, validator := range validators {
			desc := validator.Description
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}

			_, _ = fmt.Fprintf(w, "   %s\t%d\t%s\n", validator.Name, validator.Priority, desc)
		}

		_ = w.Flush()
		_, _ = fmt.Fprintln(os.Stderr)

		if showPatterns {
			_, _ = fmt.Fprintf(os.Stderr, "   Supported Patterns:\n")

			for _, validator := range validators {
				if len(validator.Patterns) > 0 {
					_, _ = fmt.Fprintf(os.Stderr, "   • %s:\n", validator.Name)

					for _, pattern := range validator.Patterns {
						_, _ = fmt.Fprintf(os.Stderr, "     - %s patterns: %s\n", pattern.Type, pattern.Pattern)

						if len(pattern.Examples) > 0 && len(pattern.Examples[0]) < 50 {
							_, _ = fmt.Fprintf(os.Stderr, "       Example: %s\n", pattern.Examples[0])
						}
					}
				}
			}

			_, _ = fmt.Fprintln(os.Stderr)
		}
	}

	_, _ = fmt.Fprintf(os.Stderr, "💡 Tips:\n")
	_, _ = fmt.Fprintf(os.Stderr, "   • Use --domain <name> to focus on a specific domain\n")
	_, _ = fmt.Fprintf(os.Stderr, "   • Use --patterns to see recognition patterns\n")
	_, _ = fmt.Fprintf(os.Stderr, "   • Use --output json for machine-readable output\n")
	_, _ = fmt.Fprintf(os.Stderr, "   • Run 'hapiq check <identifier>' to test validation\n")

	return nil
}

func getDomainDescription(domain string) string {
	descriptions := map[string]string{
		"bioinformatics": "Biological databases and genomics repositories",
		"chemistry":      "Chemical databases and molecular repositories",
		"physics":        "Physics databases and experimental data",
		"astronomy":      "Astronomical databases and observational data",
		"materials":      "Materials science databases and properties",
		"climate":        "Climate and environmental data repositories",
		"social":         "Social science and survey data",
		"economics":      "Economic databases and financial data",
	}

	if desc, exists := descriptions[domain]; exists {
		return desc
	}

	return "Specialized scientific database validators"
}

func init() {
	rootCmd.AddCommand(listCmd)

	// Flags for the list command
	listCmd.Flags().BoolVar(&listDomains, "domains", false, "list available scientific domains")
	listCmd.Flags().BoolVar(&showPatterns, "patterns", false, "show recognition patterns for validators")
	listCmd.Flags().StringVar(&domainFilter, "domain", "", "filter by specific domain (e.g., 'bio', 'chem')")
	listCmd.Flags().BoolVar(&listJSON, "json", false, "output as JSON")

	// Mark flags as mutually exclusive where appropriate
	listCmd.MarkFlagsMutuallyExclusive("domains", "domain")
}
