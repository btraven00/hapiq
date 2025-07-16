package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/btraven00/hapiq/pkg/validators/domains"
	_ "github.com/btraven00/hapiq/pkg/validators/domains/bio" // Import for side effects
)

var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "Debug information about validators and patterns",
	Long:  `Display debug information about available validators, patterns, and system state.`,
	Run:   runDebug,
}

var debugListValidators bool
var debugTestPattern string

func init() {
	rootCmd.AddCommand(debugCmd)
	debugCmd.Flags().BoolVarP(&debugListValidators, "list-validators", "l", false, "List all registered validators")
	debugCmd.Flags().StringVarP(&debugTestPattern, "test-pattern", "t", "", "Test pattern matching for a specific input")
}

func runDebug(cmd *cobra.Command, args []string) {
	if debugListValidators {
		listValidators()
		return
	}

	if debugTestPattern != "" {
		testPattern(debugTestPattern)
		return
	}

	// Default: show general debug info
	showGeneralDebug()
}

func listValidators() {
	fmt.Println("=== Registered Domain Validators ===")

	registry := domains.DefaultRegistry
	validators := registry.GetAll()

	if len(validators) == 0 {
		fmt.Println("❌ No validators registered!")
		return
	}

	fmt.Printf("Found %d validator(s):\n\n", len(validators))

	// Group by domain
	domainGroups := make(map[string][]domains.DomainValidator)
	for _, validator := range validators {
		domain := validator.Domain()
		domainGroups[domain] = append(domainGroups[domain], validator)
	}

	// Sort domains for consistent output
	var sortedDomains []string
	for domain := range domainGroups {
		sortedDomains = append(sortedDomains, domain)
	}
	sort.Strings(sortedDomains)

	for _, domain := range sortedDomains {
		fmt.Printf("📁 Domain: %s\n", domain)

		validators := domainGroups[domain]
		// Sort validators by priority (descending)
		sort.Slice(validators, func(i, j int) bool {
			return validators[i].Priority() > validators[j].Priority()
		})

		for _, validator := range validators {
			fmt.Printf("  ✅ %s (priority: %d)\n", validator.Name(), validator.Priority())
			fmt.Printf("     Description: %s\n", validator.Description())

			patterns := validator.GetPatterns()
			if len(patterns) > 0 {
				fmt.Printf("     Patterns: %d\n", len(patterns))
				for i, pattern := range patterns {
					if i < 3 { // Show first 3 patterns
						fmt.Printf("       - %s: %s\n", pattern.Type, pattern.Pattern)
					} else if i == 3 {
						fmt.Printf("       - ... and %d more\n", len(patterns)-3)
						break
					}
				}
			}
			fmt.Println()
		}
	}

	// Test some example inputs
	fmt.Println("=== Validator Test Examples ===")
	testInputs := []string{
		"SRR123456",
		"ERR1234567",
		"GSE123456",
		"CRR123456",
		"PRJNA123456",
		"https://zenodo.org/record/123456",
		"10.5281/zenodo.123456",
	}

	for _, input := range testInputs {
		candidates := registry.FindValidators(input)
		fmt.Printf("%s: ", input)
		if len(candidates) > 0 {
			var names []string
			for _, v := range candidates {
				names = append(names, v.Name())
			}
			fmt.Printf("✅ %v\n", names)
		} else {
			fmt.Printf("❌ No validators\n")
		}
	}
}

func testPattern(input string) {
	fmt.Printf("=== Testing Pattern Matching for: %q ===\n\n", input)

	registry := domains.DefaultRegistry
	candidates := registry.FindValidators(input)

	fmt.Printf("Found %d matching validator(s):\n", len(candidates))

	if len(candidates) == 0 {
		fmt.Println("❌ No validators can handle this input")
		return
	}

	for i, validator := range candidates {
		fmt.Printf("\n%d. Validator: %s (%s)\n", i+1, validator.Name(), validator.Domain())
		fmt.Printf("   Priority: %d\n", validator.Priority())
		fmt.Printf("   Description: %s\n", validator.Description())

		// Test CanValidate
		canValidate := validator.CanValidate(input)
		fmt.Printf("   CanValidate: %t\n", canValidate)

		if canValidate {
			fmt.Printf("   ✅ This validator can handle the input\n")
		} else {
			fmt.Printf("   ❌ This validator cannot handle the input\n")
		}
	}

	// Try actual validation with the best validator
	if len(candidates) > 0 {
		fmt.Printf("\n=== Testing Validation with Best Validator ===\n")

		best := candidates[0]
		fmt.Printf("Using: %s\n", best.Name())

		// Note: We don't actually run validation here to avoid network calls
		// In a real scenario, you would do:
		// ctx := context.Background()
		// result, err := best.Validate(ctx, input)
		fmt.Printf("(Validation skipped to avoid network calls)\n")
	}
}

func showGeneralDebug() {
	fmt.Println("=== Hapiq Debug Information ===")
	fmt.Println()

	registry := domains.DefaultRegistry
	validators := registry.GetAll()
	domains := registry.ListDomains()

	fmt.Printf("Validators: %d registered\n", len(validators))
	fmt.Printf("Domains: %d (%v)\n", len(domains), domains)
	fmt.Println()

	fmt.Println("Use --list-validators to see detailed validator information")
	fmt.Println("Use --test-pattern <input> to test pattern matching")
	fmt.Println()

	fmt.Println("Example commands:")
	fmt.Println("  hapiq debug --list-validators")
	fmt.Println("  hapiq debug --test-pattern SRR123456")
	fmt.Println("  hapiq debug --test-pattern 'https://zenodo.org/record/123456'")
}
