//go:build ignore
// +build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/btraven00/hapiq/pkg/validators/domains"
	"github.com/btraven00/hapiq/pkg/validators/domains/bio/accessions"
)

func main() {
	fmt.Println("=== Hapiq Accession Validation Example ===\n")

	// Initialize context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Example accessions from different databases
	examples := []string{
		// SRA (Sequence Read Archive) accessions
		"SRR123456",   // Run
		"SRX789012",   // Experiment
		"SRS345678",   // Sample
		"SRP901234",   // Study
		"PRJNA123456", // BioProject
		"ERR1234567",  // ENA Run
		"DRR567890",   // DDBJ Run

		// GSA (Genome Sequence Archive) accessions
		"CRR123456",   // GSA Run
		"CRX789012",   // GSA Experiment
		"CRA345678",   // GSA Study
		"PRJCA123456", // GSA Project

		// BioSample accessions
		"SAMN12345678", // NCBI BioSample
		"SAME87654321", // EBI BioSample
		"SAMD11111111", // DDBJ BioSample
		"SAMC22222222", // GSA BioSample

		// GEO accessions
		"GSE123456", // GEO Series
		"GSM789012", // GEO Sample

		// URLs containing accessions
		"https://www.ncbi.nlm.nih.gov/sra/SRR123456",
		"https://www.ebi.ac.uk/ena/browser/view/ERR1234567",
		"https://ngdc.cncb.ac.cn/gsa/browse/CRA123456",

		// Invalid examples
		"INVALID123",
		"GSE123", // too short
		"",       // empty
	}

	fmt.Println("1. Pattern Matching Examples")
	fmt.Println(strings.Repeat("=", 40))
	demonstratePatternMatching(examples)

	fmt.Println("\n2. Comprehensive Validation Examples")
	fmt.Println(strings.Repeat("=", 40))
	demonstrateComprehensiveValidation(ctx, examples[:10]) // Use subset for detailed validation

	fmt.Println("\n3. Registry-based Validation")
	fmt.Println(strings.Repeat("=", 40))
	demonstrateRegistryValidation(ctx, examples[:5])

	fmt.Println("\n4. Text Extraction Examples")
	fmt.Println(strings.Repeat("=", 40))
	demonstrateTextExtraction()

	fmt.Println("\n5. Database and Regional Information")
	fmt.Println(strings.Repeat("=", 40))
	demonstrateDatabaseInfo()

	fmt.Println("\n6. Accession Hierarchy and Relationships")
	fmt.Println(strings.Repeat("=", 40))
	demonstrateHierarchy()

	fmt.Println("\n7. Performance Analysis")
	fmt.Println(strings.Repeat("=", 40))
	demonstratePerformance(examples)

	fmt.Println("\n8. Error Handling and Edge Cases")
	fmt.Println(strings.Repeat("=", 40))
	demonstrateErrorHandling(ctx)

	fmt.Println("\n=== Example Complete ===")
}

// demonstratePatternMatching shows basic pattern matching capabilities
func demonstratePatternMatching(examples []string) {
	for _, example := range examples {
		pattern, matched := accessions.MatchAccession(example)

		if matched {
			fmt.Printf("✅ %s\n", example)
			fmt.Printf("   Type: %s\n", pattern.Type)
			fmt.Printf("   Database: %s\n", pattern.Database)
			fmt.Printf("   Description: %s\n", pattern.Description)
			fmt.Printf("   Priority: %d\n", pattern.Priority)
		} else {
			fmt.Printf("❌ %s - No pattern match\n", example)
		}
		fmt.Println()
	}
}

// demonstrateComprehensiveValidation shows full validation with HTTP checks
func demonstrateComprehensiveValidation(ctx context.Context, examples []string) {
	// Create validators
	sraValidator := accessions.NewSRAValidator()
	gsaValidator := accessions.NewGSAValidator()

	validators := map[string]domains.DomainValidator{
		"SRA": sraValidator,
		"GSA": gsaValidator,
	}

	for _, example := range examples {
		fmt.Printf("Validating: %s\n", example)

		for name, validator := range validators {
			if validator.CanValidate(example) {
				fmt.Printf("  Using %s validator...\n", name)

				result, err := validator.Validate(ctx, example)
				if err != nil {
					fmt.Printf("    ❌ Error: %v\n", err)
					continue
				}

				printValidationResult(result)
				break
			}
		}
		fmt.Println()
	}
}

// demonstrateRegistryValidation shows validation using the global registry
func demonstrateRegistryValidation(ctx context.Context, examples []string) {
	for _, example := range examples {
		fmt.Printf("Registry validation: %s\n", example)

		// Find suitable validators
		validators := domains.FindValidators(example)
		fmt.Printf("  Found %d validator(s)\n", len(validators))

		if len(validators) > 0 {
			// Use the first (highest priority) validator
			result, err := validators[0].Validate(ctx, example)
			if err != nil {
				fmt.Printf("    ❌ Error: %v\n", err)
			} else {
				fmt.Printf("    ✅ Valid: %t\n", result.Valid)
				fmt.Printf("    Validator: %s\n", result.ValidatorName)
				fmt.Printf("    Type: %s\n", result.DatasetType)
				fmt.Printf("    Confidence: %.3f\n", result.Confidence)
			}
		} else {
			fmt.Printf("    ❌ No suitable validator found\n")
		}
		fmt.Println()
	}
}

// demonstrateTextExtraction shows extracting accessions from text
func demonstrateTextExtraction() {
	textExamples := []string{
		"This study used RNA-seq data from SRR123456 and protein data from SRX789012.",
		"Genomic data available at GSE123456 (GEO) and CRA345678 (GSA).",
		"Samples SAMN12345678, SAME87654321 were sequenced on Illumina platform.",
		"See accession: PRJNA123456 for complete project information.",
		"No accessions in this text.",
		"Mixed case srr123456 and ERR1234567 should be found.",
	}

	for i, text := range textExamples {
		fmt.Printf("Text %d: %s\n", i+1, text)

		extracted := accessions.ExtractAccessionFromText(text)
		if len(extracted) > 0 {
			fmt.Printf("  Found accessions: %v\n", extracted)

			// Classify each found accession
			for _, acc := range extracted {
				if pattern, matched := accessions.MatchAccession(acc); matched {
					fmt.Printf("    %s: %s (%s)\n", acc, pattern.Type, pattern.Database)
				}
			}
		} else {
			fmt.Printf("  No accessions found\n")
		}
		fmt.Println()
	}
}

// demonstrateDatabaseInfo shows database and regional information
func demonstrateDatabaseInfo() {
	// Show all known databases
	fmt.Println("Known Databases:")
	for name, db := range accessions.KnownDatabases {
		fmt.Printf("  %s: %s (%s)\n", name, db.FullName, db.Region)
		fmt.Printf("    URL: %s\n", db.URL)
		fmt.Printf("    Description: %s\n", db.Description)
		fmt.Println()
	}

	// Show regional mirrors for international databases
	fmt.Println("Regional Mirrors:")
	internationalDBs := []string{"sra", "ena", "ddbj"}
	for _, db := range internationalDBs {
		mirrors := accessions.GetRegionalMirrors(db)
		fmt.Printf("  %s mirrors:\n", db)
		for _, mirror := range mirrors {
			fmt.Printf("    - %s (%s): %s\n", mirror.Name, mirror.Region, mirror.URL)
		}
		fmt.Println()
	}
}

// demonstrateHierarchy shows accession hierarchies and relationships
func demonstrateHierarchy() {
	examples := []accessions.AccessionType{
		accessions.RunSRA,
		accessions.RunGSA,
		accessions.ExperimentSRA,
		accessions.SampleGEO,
		accessions.BioSampleNCBI,
	}

	for _, accType := range examples {
		fmt.Printf("Accession Type: %s\n", accType)

		hierarchy := accessions.GetAccessionHierarchy(accType)
		fmt.Printf("  Hierarchy: %v\n", hierarchy)

		isData := accessions.IsDataLevel(accType)
		fmt.Printf("  Data Level: %t\n", isData)

		preferredDB := accessions.GetPreferredDatabase(accType)
		fmt.Printf("  Preferred Database: %s\n", preferredDB)

		fmt.Println()
	}
}

// demonstratePerformance shows performance characteristics
func demonstratePerformance(examples []string) {
	start := time.Now()

	successCount := 0
	totalValidations := 0

	for _, example := range examples {
		pattern, matched := accessions.MatchAccession(example)
		totalValidations++

		if matched {
			successCount++
			_ = pattern // Use the pattern to avoid optimization
		}
	}

	duration := time.Since(start)

	fmt.Printf("Performance Analysis:\n")
	fmt.Printf("  Total validations: %d\n", totalValidations)
	fmt.Printf("  Successful matches: %d\n", successCount)
	fmt.Printf("  Success rate: %.2f%%\n", float64(successCount)/float64(totalValidations)*100)
	fmt.Printf("  Total time: %v\n", duration)
	fmt.Printf("  Average time per validation: %v\n", duration/time.Duration(totalValidations))

	// Test format validation performance
	start = time.Now()
	formatTests := 1000
	for i := 0; i < formatTests; i++ {
		for _, example := range examples {
			accessions.ValidateAccessionFormat(example)
		}
	}
	formatDuration := time.Since(start)
	fmt.Printf("  Format validation (1000x): %v\n", formatDuration)
	fmt.Printf("  Avg format validation: %v\n", formatDuration/time.Duration(formatTests*len(examples)))
}

// demonstrateErrorHandling shows error handling and edge cases
func demonstrateErrorHandling(ctx context.Context) {
	validator := accessions.NewSRAValidator()

	edgeCases := []string{
		"",              // Empty string
		"   ",           // Whitespace only
		"SRR123",        // Too short
		"SRR123@456",    // Invalid characters
		"srr123456",     // Lowercase
		"  SRR123456  ", // Whitespace padding
		"SRR123456.1",   // With version (invalid for SRA)
		"INVALID123456", // Wrong prefix
		"123456",        // Numbers only
		"SRR",           // Prefix only
	}

	fmt.Println("Edge Case Testing:")
	for _, testCase := range edgeCases {
		fmt.Printf("Testing: %q\n", testCase)

		// Test pattern matching
		_, matched := accessions.MatchAccession(testCase)
		fmt.Printf("  Pattern match: %t\n", matched)

		// Test format validation
		valid, issues := accessions.ValidateAccessionFormat(testCase)
		fmt.Printf("  Format valid: %t\n", valid)
		if len(issues) > 0 {
			fmt.Printf("  Issues: %v\n", issues)
		}

		// Test validator
		canValidate := validator.CanValidate(testCase)
		fmt.Printf("  Validator can handle: %t\n", canValidate)

		if canValidate {
			result, err := validator.Validate(ctx, testCase)
			if err != nil {
				fmt.Printf("  Validation error: %v\n", err)
			} else {
				fmt.Printf("  Validation result: %t\n", result.Valid)
			}
		}

		fmt.Println()
	}

	// Test timeout handling
	fmt.Println("Timeout Testing:")
	shortCtx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	result, err := validator.Validate(shortCtx, "SRR123456")
	fmt.Printf("  Timeout handling - Error: %v\n", err)
	if result != nil {
		fmt.Printf("  Timeout handling - Valid: %t\n", result.Valid)
		fmt.Printf("  Timeout handling - Duration: %v\n", result.ValidationTime)
	}
}

// printValidationResult prints a formatted validation result
func printValidationResult(result *domains.DomainValidationResult) {
	if result.Valid {
		fmt.Printf("    ✅ Valid: %s\n", result.NormalizedID)
		fmt.Printf("       Type: %s\n", result.DatasetType)
		if result.Subtype != "" {
			fmt.Printf("       Subtype: %s\n", result.Subtype)
		}
		fmt.Printf("       Confidence: %.3f\n", result.Confidence)
		fmt.Printf("       Likelihood: %.3f\n", result.Likelihood)

		if result.PrimaryURL != "" {
			fmt.Printf("       Primary URL: %s\n", result.PrimaryURL)
		}

		if len(result.AlternateURLs) > 0 {
			fmt.Printf("       Alternate URLs: %d\n", len(result.AlternateURLs))
		}

		if len(result.Tags) > 0 {
			fmt.Printf("       Tags: %v\n", result.Tags)
		}

		if len(result.Metadata) > 0 {
			fmt.Printf("       Metadata:\n")
			for key, value := range result.Metadata {
				fmt.Printf("         %s: %s\n", key, value)
			}
		}

		if len(result.Warnings) > 0 {
			fmt.Printf("       Warnings: %v\n", result.Warnings)
		}

		fmt.Printf("       Validation time: %v\n", result.ValidationTime)
	} else {
		fmt.Printf("    ❌ Invalid")
		if result.Error != "" {
			fmt.Printf(": %s", result.Error)
		}
		fmt.Println()
	}
}

// Example of using the validation results in a practical application
func practicalExample() {
	fmt.Println("\n9. Practical Application Example")
	fmt.Println(strings.Repeat("=", 40))

	// Simulate processing a research paper with embedded accessions
	paperText := `
	This study presents a comprehensive analysis of RNA-seq data from multiple sources.
	We analyzed publicly available datasets including:
	- Single-cell RNA-seq data from SRR12345678 (human lung tissue)
	- Bulk RNA-seq samples from GSE123456 (mouse brain development)
	- Chinese population genomics data from CRA345678
	- Proteomics data linked to SAMN87654321

	All data processing scripts and intermediate files are available at:
	https://www.ncbi.nlm.nih.gov/sra/SRP901234

	For replication, see also ERR1234567 and associated metadata.
	`

	fmt.Println("Processing research paper text...")

	// Extract all accessions
	accessions := accessions.ExtractAccessionFromText(paperText)
	fmt.Printf("Found %d accessions: %v\n\n", len(accessions), accessions)

	// Validate and categorize each accession
	ctx := context.Background()
	results := make(map[string]*domains.DomainValidationResult)

	for _, acc := range accessions {
		validators := domains.FindValidators(acc)
		if len(validators) > 0 {
			result, err := validators[0].Validate(ctx, acc)
			if err == nil && result.Valid {
				results[acc] = result
			}
		}
	}

	// Generate a summary report
	fmt.Println("Validation Summary:")
	fmt.Printf("%s\n", strings.Repeat("━", 40))

	dataTypes := make(map[string][]string)
	databases := make(map[string][]string)

	for acc, result := range results {
		dataTypes[result.DatasetType] = append(dataTypes[result.DatasetType], acc)
		if db, exists := result.Metadata["database"]; exists {
			databases[db] = append(databases[db], acc)
		}
	}

	fmt.Println("By Data Type:")
	for dataType, accs := range dataTypes {
		fmt.Printf("  %s: %v\n", dataType, accs)
	}

	fmt.Println("\nBy Database:")
	for db, accs := range databases {
		fmt.Printf("  %s: %v\n", db, accs)
	}

	// Generate download URLs
	fmt.Println("\nDownload URLs:")
	for acc, result := range results {
		fmt.Printf("  %s:\n", acc)
		fmt.Printf("    Primary: %s\n", result.PrimaryURL)
		if len(result.AlternateURLs) > 0 {
			fmt.Printf("    Alternates: %d URLs available\n", len(result.AlternateURLs))
		}
	}

	// Export as JSON for further processing
	fmt.Println("\nJSON Export:")
	jsonData, err := json.MarshalIndent(results, "", "  ")
	if err == nil {
		fmt.Printf("Results exported (%d bytes)\n", len(jsonData))
		// In a real application, you would save this to a file
		// fmt.Println(string(jsonData))
	}
}
