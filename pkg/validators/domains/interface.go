package domains

import (
	"context"
	"time"
)

// DomainValidator defines the interface for domain-specific validators
type DomainValidator interface {
	// Name returns the unique name of this validator (e.g., "geo", "sra", "pubchem")
	Name() string

	// Domain returns the scientific domain this validator covers (e.g., "bioinformatics", "chemistry")
	Domain() string

	// Description returns a human-readable description of what this validator handles
	Description() string

	// CanValidate checks if this validator can handle the given input without performing validation
	CanValidate(input string) bool

	// Validate performs the actual validation and returns detailed results
	Validate(ctx context.Context, input string) (*DomainValidationResult, error)

	// GetPatterns returns the patterns this validator recognizes (for documentation/help)
	GetPatterns() []Pattern

	// Priority returns the priority of this validator (higher = checked first)
	Priority() int
}

// DomainValidationResult represents the result of domain-specific validation
type DomainValidationResult struct {
	// Basic validation info
	Valid         bool   `json:"valid"`
	Input         string `json:"input"`
	ValidatorName string `json:"validator_name"`
	Domain        string `json:"domain"`

	// Normalized identifier and URLs
	NormalizedID  string   `json:"normalized_id,omitempty"`
	PrimaryURL    string   `json:"primary_url,omitempty"`
	AlternateURLs []string `json:"alternate_urls,omitempty"`

	// Classification
	DatasetType string `json:"dataset_type"`
	Subtype     string `json:"subtype,omitempty"`

	// Confidence and likelihood
	Confidence float64 `json:"confidence"` // How confident we are in the validation
	Likelihood float64 `json:"likelihood"` // How likely this is a valid dataset

	// Metadata extracted from the identifier
	Metadata map[string]string `json:"metadata,omitempty"`
	Tags     []string          `json:"tags,omitempty"`

	// Error information
	Error    string   `json:"error,omitempty"`
	Warnings []string `json:"warnings,omitempty"`

	// Timing
	ValidationTime time.Duration `json:"validation_time"`
}

// Pattern represents a pattern this validator can recognize
type Pattern struct {
	Type        PatternType `json:"type"`
	Pattern     string      `json:"pattern"`
	Description string      `json:"description"`
	Examples    []string    `json:"examples"`
}

// PatternType defines the type of pattern
type PatternType string

const (
	PatternTypeRegex PatternType = "regex"
	PatternTypeURL   PatternType = "url"
	PatternTypeGlob  PatternType = "glob"
)

// ValidatorRegistry manages domain-specific validators
type ValidatorRegistry struct {
	validators map[string]DomainValidator
	domains    map[string][]DomainValidator
	sorted     []DomainValidator // Sorted by priority
}

// NewValidatorRegistry creates a new validator registry
func NewValidatorRegistry() *ValidatorRegistry {
	return &ValidatorRegistry{
		validators: make(map[string]DomainValidator),
		domains:    make(map[string][]DomainValidator),
		sorted:     make([]DomainValidator, 0),
	}
}

// Register adds a validator to the registry
func (r *ValidatorRegistry) Register(validator DomainValidator) error {
	name := validator.Name()
	domain := validator.Domain()

	// Check for duplicate names
	if _, exists := r.validators[name]; exists {
		return &RegistryError{
			Type:    ErrorTypeDuplicate,
			Message: "validator with name '" + name + "' already exists",
		}
	}

	// Add to registry
	r.validators[name] = validator

	// Add to domain grouping
	if r.domains[domain] == nil {
		r.domains[domain] = make([]DomainValidator, 0)
	}
	r.domains[domain] = append(r.domains[domain], validator)

	// Rebuild sorted list
	r.rebuildSorted()

	return nil
}

// Unregister removes a validator from the registry
func (r *ValidatorRegistry) Unregister(name string) error {
	validator, exists := r.validators[name]
	if !exists {
		return &RegistryError{
			Type:    ErrorTypeNotFound,
			Message: "validator with name '" + name + "' not found",
		}
	}

	// Remove from main registry
	delete(r.validators, name)

	// Remove from domain grouping
	domain := validator.Domain()
	if domainValidators := r.domains[domain]; domainValidators != nil {
		for i, v := range domainValidators {
			if v.Name() == name {
				r.domains[domain] = append(domainValidators[:i], domainValidators[i+1:]...)
				break
			}
		}
		// Clean up empty domain
		if len(r.domains[domain]) == 0 {
			delete(r.domains, domain)
		}
	}

	// Rebuild sorted list
	r.rebuildSorted()

	return nil
}

// Get retrieves a validator by name
func (r *ValidatorRegistry) Get(name string) (DomainValidator, bool) {
	validator, exists := r.validators[name]
	return validator, exists
}

// GetByDomain returns all validators for a specific domain
func (r *ValidatorRegistry) GetByDomain(domain string) []DomainValidator {
	return r.domains[domain]
}

// GetAll returns all registered validators sorted by priority
func (r *ValidatorRegistry) GetAll() []DomainValidator {
	return r.sorted
}

// FindValidators returns validators that can handle the given input
func (r *ValidatorRegistry) FindValidators(input string) []DomainValidator {
	var candidates []DomainValidator

	for _, validator := range r.sorted {
		if validator.CanValidate(input) {
			candidates = append(candidates, validator)
		}
	}

	return candidates
}

// ValidateWithBest attempts validation with the best matching validator
func (r *ValidatorRegistry) ValidateWithBest(ctx context.Context, input string) (*DomainValidationResult, error) {
	candidates := r.FindValidators(input)
	if len(candidates) == 0 {
		return nil, &RegistryError{
			Type:    ErrorTypeNoValidator,
			Message: "no validator found for input: " + input,
		}
	}

	// Try the highest priority validator first
	return candidates[0].Validate(ctx, input)
}

// ValidateWithAll attempts validation with all matching validators
func (r *ValidatorRegistry) ValidateWithAll(ctx context.Context, input string) ([]*DomainValidationResult, error) {
	candidates := r.FindValidators(input)
	if len(candidates) == 0 {
		return nil, &RegistryError{
			Type:    ErrorTypeNoValidator,
			Message: "no validator found for input: " + input,
		}
	}

	results := make([]*DomainValidationResult, 0, len(candidates))

	for _, validator := range candidates {
		result, err := validator.Validate(ctx, input)
		if err != nil {
			// Create error result instead of failing completely
			result = &DomainValidationResult{
				Valid:         false,
				Input:         input,
				ValidatorName: validator.Name(),
				Domain:        validator.Domain(),
				Error:         err.Error(),
			}
		}
		results = append(results, result)
	}

	return results, nil
}

// ListDomains returns all registered domains
func (r *ValidatorRegistry) ListDomains() []string {
	domains := make([]string, 0, len(r.domains))
	for domain := range r.domains {
		domains = append(domains, domain)
	}
	return domains
}

// ListValidators returns metadata about all registered validators
func (r *ValidatorRegistry) ListValidators() []ValidatorInfo {
	info := make([]ValidatorInfo, 0, len(r.validators))

	for _, validator := range r.sorted {
		info = append(info, ValidatorInfo{
			Name:        validator.Name(),
			Domain:      validator.Domain(),
			Description: validator.Description(),
			Priority:    validator.Priority(),
			Patterns:    validator.GetPatterns(),
		})
	}

	return info
}

// rebuildSorted rebuilds the priority-sorted validator list
func (r *ValidatorRegistry) rebuildSorted() {
	r.sorted = make([]DomainValidator, 0, len(r.validators))

	for _, validator := range r.validators {
		r.sorted = append(r.sorted, validator)
	}

	// Sort by priority (descending) then by name (ascending) for deterministic order
	for i := 0; i < len(r.sorted)-1; i++ {
		for j := i + 1; j < len(r.sorted); j++ {
			iPriority := r.sorted[i].Priority()
			jPriority := r.sorted[j].Priority()

			if jPriority > iPriority ||
				(jPriority == iPriority && r.sorted[j].Name() < r.sorted[i].Name()) {
				r.sorted[i], r.sorted[j] = r.sorted[j], r.sorted[i]
			}
		}
	}
}

// ValidatorInfo contains metadata about a validator
type ValidatorInfo struct {
	Name        string    `json:"name"`
	Domain      string    `json:"domain"`
	Description string    `json:"description"`
	Priority    int       `json:"priority"`
	Patterns    []Pattern `json:"patterns"`
}

// RegistryError represents errors from the validator registry
type RegistryError struct {
	Type    ErrorType `json:"type"`
	Message string    `json:"message"`
}

func (e *RegistryError) Error() string {
	return e.Message
}

// ErrorType defines types of registry errors
type ErrorType string

const (
	ErrorTypeDuplicate   ErrorType = "duplicate"
	ErrorTypeNotFound    ErrorType = "not_found"
	ErrorTypeNoValidator ErrorType = "no_validator"
)

// DefaultRegistry is the global registry instance
var DefaultRegistry = NewValidatorRegistry()

// Register registers a validator with the default registry
func Register(validator DomainValidator) error {
	return DefaultRegistry.Register(validator)
}

// Validate validates input using the default registry
func Validate(ctx context.Context, input string) (*DomainValidationResult, error) {
	return DefaultRegistry.ValidateWithBest(ctx, input)
}

// FindValidators finds validators using the default registry
func FindValidators(input string) []DomainValidator {
	return DefaultRegistry.FindValidators(input)
}
