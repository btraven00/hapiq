package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/btraven00/hapiq/pkg/validators/domains"
	"github.com/spf13/cobra"
)

var (
	listDomains  bool
	showPatterns bool
	domainFilter string
	listJSON     bool
)

// domainsCmd represents the domains command
var domainsCmd = &cobra.Command{
	Use:   "domains",
	Short: "List and explore domain-specific validators",
	Long: `The domains command allows you to explore the available domain-specific
validators that can recognize and validate identifiers from scientific databases
and repositories.

Each domain represents a scientific field (e.g., bioinformatics, chemistry)
and contains validators for specific databases within that domain.

Examples:
  hapiq domains --list                    # List all available validators
  hapiq domains --domains                 # List available domains
  hapiq domains --domain bio              # Show validators in bioinformatics domain
  hapiq domains --patterns                # Show recognition patterns
  hapiq domains --output json             # Output as JSON`,
	RunE: runDomains,
}

func runDomains(cmd *cobra.Command, args []string) error {
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
		fmt.Println("No domain validators are currently registered.")
		return nil
	}

	sort.Strings(domainList)

	fmt.Printf("Available Domains (%d):\n\n", len(domainList))

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "DOMAIN\tVALIDATORS\tDESCRIPTION")
	fmt.Fprintln(w, "------\t----------\t-----------")

	for _, domain := range domainList {
		validators := domains.DefaultRegistry.GetByDomain(domain)
		description := getDomainDescription(domain)

		fmt.Fprintf(w, "%s\t%d\t%s\n", domain, len(validators), description)
	}

	return w.Flush()
}

func showDomainValidators(domain string) error {
	validators := domains.DefaultRegistry.GetByDomain(domain)
	if len(validators) == 0 {
		return fmt.Errorf("no validators found for domain: %s", domain)
	}

	fmt.Printf("Validators in domain '%s' (%d):\n\n", domain, len(validators))

	for _, validator := range validators {
		fmt.Printf("📊 %s (priority: %d)\n", validator.Name(), validator.Priority())
		fmt.Printf("   %s\n\n", validator.Description())

		if showPatterns {
			patterns := validator.GetPatterns()
			if len(patterns) > 0 {
				fmt.Printf("   Patterns:\n")
				for _, pattern := range patterns {
					fmt.Printf("   • %s: %s\n", pattern.Type, pattern.Pattern)
					fmt.Printf("     %s\n", pattern.Description)
					if len(pattern.Examples) > 0 {
						fmt.Printf("     Examples: %s\n", strings.Join(pattern.Examples, ", "))
					}
					fmt.Println()
				}
			}
		}
	}

	return nil
}

func showAllValidators() error {
	info := domains.DefaultRegistry.ListValidators()
	if len(info) == 0 {
		fmt.Println("No domain validators are currently registered.")
		fmt.Println("\nTo see how to add validators, run: hapiq domains --help")
		return nil
	}

	fmt.Printf("Available Domain Validators (%d):\n\n", len(info))

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

		fmt.Printf("🔬 %s\n", strings.ToUpper(domain))
		fmt.Printf("   %s\n\n", getDomainDescription(domain))

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "   NAME\tPRIORITY\tDESCRIPTION")
		fmt.Fprintln(w, "   ----\t--------\t-----------")

		for _, validator := range validators {
			desc := validator.Description
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			fmt.Fprintf(w, "   %s\t%d\t%s\n", validator.Name, validator.Priority, desc)
		}

		w.Flush()
		fmt.Println()

		if showPatterns {
			fmt.Printf("   Supported Patterns:\n")
			for _, validator := range validators {
				if len(validator.Patterns) > 0 {
					fmt.Printf("   • %s:\n", validator.Name)
					for _, pattern := range validator.Patterns {
						fmt.Printf("     - %s patterns: %s\n", pattern.Type, pattern.Pattern)
						if len(pattern.Examples) > 0 && len(pattern.Examples[0]) < 50 {
							fmt.Printf("       Example: %s\n", pattern.Examples[0])
						}
					}
				}
			}
			fmt.Println()
		}
	}

	fmt.Printf("💡 Tips:\n")
	fmt.Printf("   • Use --domain <name> to focus on a specific domain\n")
	fmt.Printf("   • Use --patterns to see recognition patterns\n")
	fmt.Printf("   • Use --output json for machine-readable output\n")
	fmt.Printf("   • Run 'hapiq check <identifier>' to test validation\n")

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
	rootCmd.AddCommand(domainsCmd)

	// Flags for the domains command
	domainsCmd.Flags().BoolVar(&listDomains, "domains", false, "list available scientific domains")
	domainsCmd.Flags().BoolVar(&showPatterns, "patterns", false, "show recognition patterns for validators")
	domainsCmd.Flags().StringVar(&domainFilter, "domain", "", "filter by specific domain (e.g., 'bio', 'chemistry')")
	domainsCmd.Flags().BoolVar(&listJSON, "json", false, "output as JSON")

	// Mark flags as mutually exclusive where appropriate
	domainsCmd.MarkFlagsMutuallyExclusive("domains", "domain")
}
