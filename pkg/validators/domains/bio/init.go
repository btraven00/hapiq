package bio

import (
	"fmt"

	"github.com/btraven00/hapiq/pkg/validators/domains"
	_ "github.com/btraven00/hapiq/pkg/validators/domains/bio/accessions"
)

// init registers all bioinformatics domain validators.
func init() {
	registerBioValidators()
}

// registerBioValidators registers all validators in the bio domain.
func registerBioValidators() {
	validators := []domains.DomainValidator{
		NewGEOValidator(),
		// Add more bio validators here as they are implemented
		// NewSRAValidator(),
		// NewEnsemblValidator(),
		// NewUniProtValidator(),
	}

	for _, validator := range validators {
		if err := domains.Register(validator); err != nil {
			// Log error but don't panic - this allows the application to continue
			// even if some validators fail to register
			fmt.Printf("Warning: failed to register %s validator: %v\n", validator.Name(), err)
		}
	}
}

// GetRegisteredValidators returns the list of registered bio validators.
func GetRegisteredValidators() []string {
	bioValidators := domains.DefaultRegistry.GetByDomain("bioinformatics")
	names := make([]string, 0, len(bioValidators))

	for _, validator := range bioValidators {
		names = append(names, validator.Name())
	}

	return names
}
