package accessions

import (
	"github.com/btraven00/hapiq/pkg/validators/domains"
)

// init registers all accession validators with the global registry
func init() {
	// Register SRA validator
	sraValidator := NewSRAValidator()
	if err := domains.Register(sraValidator); err != nil {
		// Log error but don't panic during initialization
		// In production, you might want to use a proper logger
		_ = err
	}

	// Register GSA validator
	gsaValidator := NewGSAValidator()
	if err := domains.Register(gsaValidator); err != nil {
		_ = err
	}
}

// RegisterAccessionValidators manually registers all accession validators
// This can be called explicitly if needed, e.g., for testing or custom registries
func RegisterAccessionValidators(registry *domains.ValidatorRegistry) error {
	validators := []domains.DomainValidator{
		NewSRAValidator(),
		NewGSAValidator(),
	}

	for _, validator := range validators {
		if err := registry.Register(validator); err != nil {
			return err
		}
	}

	return nil
}

// GetAllAccessionValidators returns instances of all accession validators
// Useful for testing and introspection
func GetAllAccessionValidators() []domains.DomainValidator {
	return []domains.DomainValidator{
		NewSRAValidator(),
		NewGSAValidator(),
	}
}

// GetAccessionValidatorByName returns a specific accession validator by name
func GetAccessionValidatorByName(name string) domains.DomainValidator {
	switch name {
	case "sra":
		return NewSRAValidator()
	case "gsa":
		return NewGSAValidator()
	default:
		return nil
	}
}

// GetSupportedAccessionTypes returns all accession types supported by registered validators
func GetSupportedAccessionTypes() []AccessionType {
	var allTypes []AccessionType
	seen := make(map[AccessionType]bool)

	// Collect types from all validators
	validators := GetAllAccessionValidators()
	for _, validator := range validators {
		var validatorTypes []AccessionType

		switch v := validator.(type) {
		case *SRAValidator:
			validatorTypes = v.GetSupportedAccessionTypes()
		case *GSAValidator:
			validatorTypes = v.GetSupportedAccessionTypes()
		}

		for _, accType := range validatorTypes {
			if !seen[accType] {
				allTypes = append(allTypes, accType)
				seen[accType] = true
			}
		}
	}

	return allTypes
}

// GetAccessionStats returns statistics about supported accession patterns
func GetAccessionStats() map[string]interface{} {
	stats := make(map[string]interface{})

	// Count patterns by database
	dbCounts := make(map[string]int)
	typeCounts := make(map[AccessionType]int)

	for _, pattern := range AccessionPatterns {
		dbCounts[pattern.Database]++
		typeCounts[pattern.Type]++
	}

	stats["total_patterns"] = len(AccessionPatterns)
	stats["databases"] = dbCounts
	stats["accession_types"] = typeCounts
	stats["supported_databases"] = []string{"sra", "ena", "ddbj", "gsa", "geo", "biosample", "bioproject"}
	stats["validators_count"] = len(GetAllAccessionValidators())

	return stats
}
