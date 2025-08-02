// Package ensembl provides domain validation for Ensembl genome database identifiers.
package ensembl

import (
	"fmt"

	"github.com/btraven00/hapiq/pkg/validators/domains"
)

// init registers the Ensembl validator with the domain registry.
func init() {
	validator := NewEnsemblValidator()

	if err := domains.Register(validator); err != nil {
		// Log error but don't panic - this allows the application to continue
		// even if the validator fails to register
		fmt.Printf("Warning: failed to register %s validator: %v\n", validator.Name(), err)
	}
}
