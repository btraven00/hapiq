// Package cmd provides command-line interface commands for the hapiq tool.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/btraven00/hapiq/pkg/downloaders"
)

var (
	downloadersJSON    bool
	downloadersVerbose bool
)

// downloadersCmd represents the downloaders command.
var downloadersCmd = &cobra.Command{
	Use:   "downloaders",
	Short: "List and explore available dataset downloaders",
	Long: `The downloaders command allows you to explore the available downloaders
that can fetch datasets from various scientific data repositories.

Each downloader supports specific repository types and provides metadata
extraction and download capabilities with provenance tracking.

Examples:
  hapiq downloaders                       # List all available downloaders
  hapiq downloaders --verbose             # Show detailed information
  hapiq downloaders --output json         # Output as JSON`,
	RunE: runDownloaders,
}

func init() {
	rootCmd.AddCommand(downloadersCmd)

	downloadersCmd.Flags().BoolVarP(&downloadersJSON, "output", "o", false, "Output as JSON")
	downloadersCmd.Flags().BoolVarP(&downloadersVerbose, "verbose", "v", false, "Show verbose information")
}

func runDownloaders(_ *cobra.Command, _ []string) error {
	// Initialize downloaders to ensure they're registered
	if err := initializeDownloaders(); err != nil {
		return fmt.Errorf("failed to initialize downloaders: %w", err)
	}

	if downloadersJSON {
		return outputDownloadersJSON()
	}

	return listAvailableDownloaders()
}

// listAvailableDownloaders displays all registered downloaders in a human-readable format.
func listAvailableDownloaders() error {
	downloaderTypes := downloaders.List()
	if len(downloaderTypes) == 0 {
		fmt.Println("No downloaders are currently registered.")
		return nil
	}

	aliasMap := downloaders.DefaultRegistry.ListWithAliases()

	fmt.Printf("📥 Available Dataset Downloaders (%d total)\n\n", len(downloaderTypes))

	// Sort downloaders for consistent output
	sort.Strings(downloaderTypes)

	for _, sourceType := range downloaderTypes {
		aliases := aliasMap[sourceType]

		fmt.Printf("🔹 %s", sourceType)
		if len(aliases) > 0 {
			fmt.Printf(" (aliases: %s)", strings.Join(aliases, ", "))
		}
		fmt.Println()

		if downloadersVerbose {
			description := getDownloaderDescription(sourceType)
			if description != "" {
				fmt.Printf("   %s\n", description)
			}

			examples := getDownloaderExamples(sourceType)
			if len(examples) > 0 {
				fmt.Printf("   Examples:\n")
				for _, example := range examples {
					fmt.Printf("     %s\n", example)
				}
			}
			fmt.Println()
		}
	}

	if !downloadersVerbose {
		fmt.Println("\nUse --verbose for detailed information about each downloader.")
	}

	return nil
}

// outputDownloadersJSON outputs downloader information as JSON.
func outputDownloadersJSON() error {
	downloaderTypes := downloaders.List()
	aliasMap := downloaders.DefaultRegistry.ListWithAliases()

	type DownloaderInfo struct {
		Type        string   `json:"type"`
		Aliases     []string `json:"aliases,omitempty"`
		Description string   `json:"description,omitempty"`
		Examples    []string `json:"examples,omitempty"`
	}

	var result struct {
		Downloaders []DownloaderInfo `json:"downloaders"`
		Total       int              `json:"total"`
	}

	result.Total = len(downloaderTypes)
	result.Downloaders = make([]DownloaderInfo, 0, len(downloaderTypes))

	// Sort for consistent output
	sort.Strings(downloaderTypes)

	for _, sourceType := range downloaderTypes {
		info := DownloaderInfo{
			Type:        sourceType,
			Aliases:     aliasMap[sourceType],
			Description: getDownloaderDescription(sourceType),
			Examples:    getDownloaderExamples(sourceType),
		}
		result.Downloaders = append(result.Downloaders, info)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// getDownloaderDescription returns a description for the given downloader type.
func getDownloaderDescription(sourceType string) string {
	descriptions := map[string]string{
		"geo":      "NCBI Gene Expression Omnibus - Download gene expression datasets, series, samples, and platforms",
		"figshare": "Figshare repository - Download articles, collections, and projects with comprehensive metadata",
		"ensembl":  "Ensembl Genomes databases - Download genomic data from bacteria, fungi, metazoa, plants, and protists",
	}

	if desc, exists := descriptions[sourceType]; exists {
		return desc
	}
	return fmt.Sprintf("%s downloader", strings.Title(sourceType))
}

// getDownloaderExamples returns usage examples for the given downloader type.
func getDownloaderExamples(sourceType string) []string {
	examples := map[string][]string{
		"geo": {
			"hapiq download geo GSE123456",
			"hapiq download geo GSM456789",
			"hapiq download geo GPL570",
		},
		"figshare": {
			"hapiq download figshare 12345678",
			"hapiq download figshare 87654321 --exclude-raw",
		},
		"ensembl": {
			"hapiq download ensembl bacteria:47:pep",
			"hapiq download ensembl fungi:47:gff3",
			"hapiq download ensembl plants:50:dna:arabidopsis_thaliana",
		},
	}

	if exs, exists := examples[sourceType]; exists {
		return exs
	}
	return []string{fmt.Sprintf("hapiq download %s <identifier>", sourceType)}
}
