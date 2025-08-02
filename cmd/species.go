// Package cmd provides command-line interface commands for the hapiq tool.
package cmd

import (
	"bufio"
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/btraven00/hapiq/pkg/downloaders/ensembl"
)

var (
	showAll       bool
	showExamples  bool
	filterPattern string
)

// speciesCmd represents the species command.
var speciesCmd = &cobra.Command{
	Use:   "species [database] [version]",
	Short: "Browse available species in Ensembl databases",
	Long: `Browse and search available species in Ensembl genome databases.

This command downloads and parses the species list for a given database and version,
showing available organisms for targeted downloads.

Examples:
  hapiq species fungi                    # Browse fungi database (default version 47)
  hapiq species bacteria 47              # Browse bacteria database version 47
  hapiq species plants 50                # Browse plants database version 50
  hapiq species --filter "cerevisiae"    # Show only species matching pattern
  hapiq species --examples fungi         # Show download examples
  hapiq species --all fungi              # Show all details (assembly, taxon, etc.)

Supported databases: bacteria, fungi, metazoa, plants, protists`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runSpecies,
}

func runSpecies(cmd *cobra.Command, args []string) error {
	database := strings.ToLower(args[0])
	version := "47" // Default version
	if len(args) > 1 {
		version = args[1]
	}

	// Validate database
	validDatabases := []string{"bacteria", "fungi", "metazoa", "plants", "protists"}
	validDB := false
	for _, db := range validDatabases {
		if database == db {
			validDB = true
			break
		}
	}

	if !validDB {
		return fmt.Errorf("invalid database '%s'. Valid databases: %s", database, strings.Join(validDatabases, ", "))
	}

	if !quiet {
		fmt.Printf("🔬 Browsing Ensembl %s Database (Release %s)\n", strings.Title(database), version)
		fmt.Println("=" + strings.Repeat("=", 50))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Download and parse species list
	speciesURL := fmt.Sprintf("ftp://ftp.ensemblgenomes.org/pub/release-%s/%s/species_Ensembl%s.txt",
		version, database, strings.Title(database))

	if !quiet {
		fmt.Printf("📡 Downloading species list from FTP server...\n\n")
	}

	// Use protocol client to download species list
	client := ensembl.NewMultiProtocolClient(nil, 60*time.Second, false, 1)
	defer client.Close()

	resp, err := client.Get(ctx, speciesURL)
	if err != nil {
		return fmt.Errorf("failed to download species list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to download species list: status %d", resp.StatusCode)
	}

	// Parse species list
	species, err := parseSpeciesList(bufio.NewReader(resp.Body))
	if err != nil {
		return fmt.Errorf("failed to parse species list: %w", err)
	}

	// Apply filtering
	if filterPattern != "" {
		species = filterSpecies(species, filterPattern)
	}

	// Sort species by name for better display
	sort.Slice(species, func(i, j int) bool {
		return species[i].SpeciesName < species[j].SpeciesName
	})

	if !quiet {
		fmt.Printf("📊 Found %d species", len(species))
		if filterPattern != "" {
			fmt.Printf(" matching '%s'", filterPattern)
		}
		fmt.Printf(" in %s database:\n\n", database)
	}

	// Display results based on format
	switch output {
	case "json":
		return outputSpeciesJSON(species)
	default:
		return outputSpeciesHuman(species, database, version)
	}
}

// SpeciesInfo represents information about a species from the Ensembl species list.
type SpeciesInfo struct {
	Name        string `json:"name"`
	SpeciesName string `json:"species_name"`
	TaxonID     string `json:"taxon_id"`
	Assembly    string `json:"assembly"`
	Source      string `json:"source"`
	CoreDB      string `json:"core_db"`
}

// parseSpeciesList parses the Ensembl species list format.
func parseSpeciesList(reader *bufio.Reader) ([]SpeciesInfo, error) {
	var species []SpeciesInfo
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := scanner.Text()

		// Skip comments and empty lines
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}

		// Replace empty values with "NA" to handle parsing
		line = normalizeSpeciesLine(line)

		// Parse tab-separated values
		fields := strings.Split(line, "\t")
		if len(fields) < 14 {
			continue // Skip malformed lines
		}

		speciesInfo := SpeciesInfo{
			Name:        fields[0],
			SpeciesName: fields[1],
			TaxonID:     fields[3],
			Assembly:    fields[4],
			Source:      fields[5],
			CoreDB:      fields[13],
		}

		species = append(species, speciesInfo)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return species, nil
}

// normalizeSpeciesLine handles empty fields in the species list.
func normalizeSpeciesLine(line string) string {
	// Replace empty tabs with NA (similar to sed command)
	line = regexp.MustCompile(`\t\t`).ReplaceAllString(line, "\tNA\t")
	if strings.HasPrefix(line, "\t") {
		line = "NA" + line
	}
	if strings.HasSuffix(line, "\t") {
		line = line + "NA"
	}
	return line
}

// filterSpecies filters species list by pattern.
func filterSpecies(species []SpeciesInfo, pattern string) []SpeciesInfo {
	var filtered []SpeciesInfo
	lowerPattern := strings.ToLower(pattern)

	for _, sp := range species {
		if strings.Contains(strings.ToLower(sp.SpeciesName), lowerPattern) ||
			strings.Contains(strings.ToLower(sp.Name), lowerPattern) ||
			strings.Contains(strings.ToLower(sp.Assembly), lowerPattern) {
			filtered = append(filtered, sp)
		}
	}

	return filtered
}

// outputSpeciesHuman displays species in human-readable format.
func outputSpeciesHuman(species []SpeciesInfo, database, version string) error {
	if showAll {
		// Full details table
		fmt.Printf("%-4s %-35s %-15s %-20s %-15s\n", "ID", "Species Name", "Taxon ID", "Assembly", "Source")
		fmt.Println(strings.Repeat("-", 95))

		for i, sp := range species {
			fmt.Printf("%-4d %-35s %-15s %-20s %-15s\n",
				i+1,
				truncate(sp.SpeciesName, 34),
				sp.TaxonID,
				truncate(sp.Assembly, 19),
				truncate(sp.Source, 14))
		}
	} else {
		// Compact list
		fmt.Printf("%-4s %-50s %-15s\n", "ID", "Species Name", "Assembly")
		fmt.Println(strings.Repeat("-", 70))

		for i, sp := range species {
			fmt.Printf("%-4d %-50s %-15s\n",
				i+1,
				truncate(sp.SpeciesName, 49),
				truncate(sp.Assembly, 14))
		}
	}

	// Show usage examples
	if showExamples || len(species) <= 10 {
		fmt.Printf("\n📝 Download Examples:\n")
		fmt.Println("Format: DATABASE:VERSION:CONTENT:SPECIES_NAME")
		fmt.Println()

		examples := []string{"gff3", "pep", "cds", "dna"}
		maxExamples := 5
		if len(species) < maxExamples {
			maxExamples = len(species)
		}

		for i := 0; i < maxExamples; i++ {
			sp := species[i]
			content := examples[i%len(examples)]
			fmt.Printf("  hapiq check \"%s:%s:%s:%s\"\n", database, version, content, sp.SpeciesName)
		}

		fmt.Printf("\n🔍 Search Examples:\n")
		fmt.Printf("  hapiq species %s --filter cerevisiae\n", database)
		fmt.Printf("  hapiq species %s --filter coli\n", database)
	}

	fmt.Printf("\n💡 Usage Tips:\n")
	fmt.Println("  • Use --filter to search for specific species")
	fmt.Println("  • Use --all to show detailed information")
	fmt.Println("  • Use --examples to see more download examples")
	fmt.Println("  • Species names support partial matching in downloads")

	fmt.Printf("\n📁 Content Types:\n")
	fmt.Println("  gff3 - Genome annotations")
	fmt.Println("  pep  - Protein sequences")
	fmt.Println("  cds  - Coding DNA sequences")
	fmt.Println("  dna  - Genomic DNA sequences")

	return nil
}

// outputSpeciesJSON displays species in JSON format.
func outputSpeciesJSON(species []SpeciesInfo) error {
	// Simple JSON output - could be enhanced with proper JSON marshaling
	fmt.Println("[")
	for i, sp := range species {
		fmt.Printf("  {\n")
		fmt.Printf("    \"id\": %d,\n", i+1)
		fmt.Printf("    \"species_name\": \"%s\",\n", sp.SpeciesName)
		fmt.Printf("    \"name\": \"%s\",\n", sp.Name)
		fmt.Printf("    \"taxon_id\": \"%s\",\n", sp.TaxonID)
		fmt.Printf("    \"assembly\": \"%s\",\n", sp.Assembly)
		fmt.Printf("    \"source\": \"%s\",\n", sp.Source)
		fmt.Printf("    \"core_db\": \"%s\"\n", sp.CoreDB)
		if i < len(species)-1 {
			fmt.Printf("  },\n")
		} else {
			fmt.Printf("  }\n")
		}
	}
	fmt.Println("]")
	return nil
}

// truncate truncates a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func init() {
	rootCmd.AddCommand(speciesCmd)

	// Local flags for the species command
	speciesCmd.Flags().BoolVar(&showAll, "all", false, "show all details (assembly, taxon, source)")
	speciesCmd.Flags().BoolVar(&showExamples, "examples", false, "show download examples for species")
	speciesCmd.Flags().StringVar(&filterPattern, "filter", "", "filter species by name pattern")
}
